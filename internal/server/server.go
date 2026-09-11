// Package server wires the battery cache and the stats store to a tiny HTTP
// surface: the JSON API plus static file serving for the website. No router,
// no middleware stack - just net/http.
package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"kachel.solar/internal/battery"
	"kachel.solar/internal/stats"
)

// BatterySource is the part of *battery.Sampler the API depends on.
type BatterySource interface {
	Current() (battery.Reading, bool)
}

// StatsSource is the part of *stats.Store the API depends on.
type StatsSource interface {
	Stats() (stats.Report, error)
}

// Options configures New.
type Options struct {
	WebDir   string        // directory of static site files
	Battery  BatterySource // required
	Stats    StatsSource   // required
	StatsTTL time.Duration // how long to cache the /api/sensor report (default 60s)
	Version  string        // reported by /api/health
}

type srv struct {
	opts     Options
	static   http.Handler
	statsTTL time.Duration

	mu       sync.Mutex
	report   *stats.Report
	reportAt time.Time
}

// New returns the fully assembled HTTP handler.
func New(opts Options) http.Handler {
	if opts.StatsTTL <= 0 {
		opts.StatsTTL = 60 * time.Second
	}
	s := &srv{
		opts:     opts,
		static:   newStaticHandler(opts.WebDir),
		statsTTL: opts.StatsTTL,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/battery", s.handleBattery)
	mux.HandleFunc("GET /api/sensor", s.handleSensor)
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.Handle("GET /", s.static)
	return logRequests(mux)
}

func (s *srv) handleBattery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	rd, ok := s.opts.Battery.Current()
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "battery reading not available yet",
		})
		return
	}
	writeJSON(w, http.StatusOK, rd)
}

func (s *srv) handleSensor(w http.ResponseWriter, r *http.Request) {
	rep, err := s.cachedReport()
	if err != nil {
		log.Printf("stats report failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "sensor stats unavailable",
		})
		return
	}
	w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(int(s.statsTTL.Seconds())))
	writeJSON(w, http.StatusOK, rep)
}

func (s *srv) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	_, haveReading := s.opts.Battery.Current()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "ok",
		"version":      s.opts.Version,
		"time":         time.Now().UTC(),
		"have_reading": haveReading,
	})
}

func (s *srv) cachedReport() (stats.Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.report != nil && time.Since(s.reportAt) < s.statsTTL {
		return *s.report, nil
	}
	rep, err := s.opts.Stats.Stats()
	if err != nil {
		return stats.Report{}, err
	}
	s.report = &rep
	s.reportAt = time.Now()
	return rep, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		log.Printf("encode response: %v", err)
	}
}

// logRequests writes a one-line access log to stdout (the systemd journal).
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s -> %d (%s)", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.wroteHeader = true
	}
	return r.ResponseWriter.Write(b)
}
