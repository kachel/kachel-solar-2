package battery

import (
	"context"
	"log"
	"sync"
	"time"

	"kachel.solar/internal/sensor"
)

// Sampler polls the sensor on a fixed interval and keeps the most recent
// Reading in memory. HTTP handlers read from the cache and never touch I2C
// directly, so a slow or wedged sensor can't stall a request.
type Sampler struct {
	sensor   sensor.Sensor
	cfg      Config
	interval time.Duration
	now      func() time.Time

	// onSample, if set, is called for every successful reading (used to
	// append to the stats log). It must not block for long.
	onSample func(Reading)

	mu       sync.RWMutex
	last     Reading
	lastOK   time.Time
	haveRead bool
	fails    int
}

// NewSampler builds a Sampler. interval <= 0 defaults to 10s.
func NewSampler(s sensor.Sensor, cfg Config, interval time.Duration, onSample func(Reading)) *Sampler {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	return &Sampler{
		sensor:   s,
		cfg:      cfg.WithDefaults(),
		interval: interval,
		now:      time.Now,
		onSample: onSample,
	}
}

// Run samples once immediately, then every interval until ctx is cancelled.
func (s *Sampler) Run(ctx context.Context) {
	s.sample()
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.sample()
		}
	}
}

// sample performs one read and updates the cache. Exported behaviour is via
// Current; this is separate so tests can drive it deterministically.
func (s *Sampler) sample() {
	raw, err := s.sensor.Read()
	now := s.now()
	if err != nil {
		s.mu.Lock()
		s.fails++
		fails := s.fails
		s.mu.Unlock()
		// Log the first failure and then only occasionally, so a sensor
		// that's out overnight doesn't flood the journal.
		if fails == 1 || fails%30 == 0 {
			log.Printf("sensor read failed (%d in a row): %v", fails, err)
		}
		return
	}

	r := s.cfg.Interpret(raw, now)
	s.mu.Lock()
	if s.fails > 0 {
		log.Printf("sensor recovered after %d failed reads", s.fails)
	}
	s.fails = 0
	s.last = r
	s.lastOK = now
	s.haveRead = true
	cb := s.onSample
	s.mu.Unlock()

	if cb != nil {
		cb(r)
	}
}

// Current returns the latest reading and whether one exists yet. Stale is
// computed against the wall clock at call time.
func (s *Sampler) Current() (Reading, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.haveRead {
		return Reading{}, false
	}
	r := s.last
	r.Stale = s.now().Sub(s.lastOK) > s.cfg.StaleAfter
	return r, true
}
