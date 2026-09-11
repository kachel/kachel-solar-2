// Command kacheld is the kachel.solar server: it serves the static website and
// a small JSON API (/api/battery, /api/sensor, /api/health) backed by an
// Adafruit INA219 on the Pi's I2C bus. Single binary, no framework, no
// database.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"kachel.solar/internal/battery"
	"kachel.solar/internal/sensor"
	"kachel.solar/internal/server"
	"kachel.solar/internal/stats"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	cfg := parseFlags()
	log.SetFlags(log.LstdFlags | log.LUTC)

	if cfg.showVersion {
		fmt.Printf("kacheld %s\n", version)
		return
	}

	if err := run(cfg); err != nil {
		log.Fatalf("kacheld: %v", err)
	}
}

type config struct {
	addr             string
	webDir           string
	dataDir          string
	i2cBus           string
	i2cAddr          int
	shuntOhms        float64
	sampleInterval   time.Duration
	staleAfter       time.Duration
	chargeThreshold  float64
	invertCurrent    bool
	mock             bool
	rawRetention     time.Duration
	statsTTL         time.Duration
	maintainInterval time.Duration
	showVersion      bool
}

func parseFlags() config {
	var c config
	var i2cAddr string

	flag.StringVar(&c.addr, "addr", envOr("KACHEL_ADDR", "127.0.0.1:8080"), "listen address")
	flag.StringVar(&c.webDir, "web", envOr("KACHEL_WEB", "./web"), "directory of static website files")
	flag.StringVar(&c.dataDir, "data", envOr("KACHEL_DATA", "./data"), "directory for the reading log and rollups")
	flag.StringVar(&c.i2cBus, "i2c-bus", "/dev/i2c-1", "I2C bus device for the INA219")
	flag.StringVar(&i2cAddr, "i2c-addr", "0x40", "INA219 I2C address (accepts 0x prefix)")
	flag.Float64Var(&c.shuntOhms, "i2c-shunt-ohms", 0.1, "INA219 shunt resistor value in ohms")
	flag.DurationVar(&c.sampleInterval, "sample-interval", 10*time.Second, "how often to poll the sensor")
	flag.DurationVar(&c.staleAfter, "stale-after", 45*time.Second, "flag a reading stale after this long without a fresh sample")
	flag.Float64Var(&c.chargeThreshold, "charge-threshold-ma", 15, "current magnitude (mA) above which the pack counts as charging/discharging")
	flag.BoolVar(&c.invertCurrent, "invert-current", false, "flip the sign of measured current (wrong-way INA219 wiring)")
	flag.BoolVar(&c.mock, "mock", false, "use a simulated sensor instead of real hardware")
	flag.DurationVar(&c.rawRetention, "raw-retention", 14*24*time.Hour, "how long to keep raw per-sample logs before pruning to daily rollups")
	flag.DurationVar(&c.statsTTL, "stats-ttl", 60*time.Second, "server-side cache lifetime for the /api/sensor report")
	flag.DurationVar(&c.maintainInterval, "maintain-interval", time.Hour, "how often to roll up and prune the reading log")
	flag.BoolVar(&c.showVersion, "version", false, "print version and exit")
	flag.Parse()

	n, err := strconv.ParseInt(i2cAddr, 0, 32)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid -i2c-addr %q: %v\n", i2cAddr, err)
		os.Exit(2)
	}
	c.i2cAddr = int(n)
	return c
}

func run(cfg config) error {
	sens, err := sensor.New(sensor.Config{
		Mock:      cfg.mock,
		Bus:       cfg.i2cBus,
		Addr:      cfg.i2cAddr,
		ShuntOhms: cfg.shuntOhms,
	})
	if err != nil {
		if errors.Is(err, sensor.ErrUnsupported) {
			return fmt.Errorf("%w (pass -mock to run without hardware)", err)
		}
		return err
	}
	defer sens.Close()

	if cfg.mock {
		log.Printf("sensor: using simulated INA219")
	} else {
		log.Printf("sensor: INA219 on %s @ %#02x", cfg.i2cBus, cfg.i2cAddr)
	}

	store, err := stats.Open(cfg.dataDir, cfg.rawRetention)
	if err != nil {
		return err
	}
	defer store.Close()

	batCfg := battery.Config{
		ChargeThresholdMA: cfg.chargeThreshold,
		InvertCurrent:     cfg.invertCurrent,
		StaleAfter:        cfg.staleAfter,
	}

	var logFails int
	sampler := battery.NewSampler(sens, batCfg, cfg.sampleInterval, func(r battery.Reading) {
		if err := store.Append(recordFrom(r)); err != nil {
			logFails++
			if logFails == 1 || logFails%30 == 0 {
				log.Printf("stats append failed (%d): %v", logFails, err)
			}
			return
		}
		logFails = 0
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go sampler.Run(ctx)
	go maintainLoop(ctx, store, cfg.maintainInterval)

	handler := server.New(server.Options{
		WebDir:   cfg.webDir,
		Battery:  sampler,
		Stats:    store,
		StatsTTL: cfg.statsTTL,
		Version:  version,
	})

	httpSrv := &http.Server{
		Addr:              cfg.addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		log.Printf("kacheld %s listening on %s (web=%s data=%s)", version, cfg.addr, cfg.webDir, cfg.dataDir)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	select {
	case err := <-errc:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		log.Printf("shutdown signal received, draining...")
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
	if err := store.Maintain(); err != nil {
		log.Printf("final maintain failed: %v", err)
	}
	return nil
}

func maintainLoop(ctx context.Context, store *stats.Store, every time.Duration) {
	if every <= 0 {
		every = time.Hour
	}
	if err := store.Maintain(); err != nil {
		log.Printf("initial maintain failed: %v", err)
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := store.Maintain(); err != nil {
				log.Printf("maintain failed: %v", err)
			}
		}
	}
}

func recordFrom(r battery.Reading) stats.Record {
	return stats.Record{
		T:   r.UpdatedAt.Unix(),
		V:   r.Voltage,
		I:   r.CurrentMA,
		P:   r.PowerMW,
		Pct: r.Percent,
		Chg: r.Charging,
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
