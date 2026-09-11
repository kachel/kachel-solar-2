// Package stats keeps a lightweight history of battery readings and rolls it
// up into the numbers behind GET /api/sensor: energy generated, average
// charge, and time spent charging, over a few time windows.
//
// Storage is deliberately dumb: one append-only JSON-lines file per UTC day
// (raw-2006-01-02.jsonl). Finished days are summarised into a small
// daily-2006-01-02.json rollup and the raw file is pruned once it ages past
// the retention window. No database, no daemon.
package stats

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	rawPrefix   = "raw-"
	rawSuffix   = ".jsonl"
	dailyPrefix = "daily-"
	dailySuffix = ".json"
	dayLayout   = "2006-01-02"

	// maxGap caps the time attributed to a single pair of consecutive
	// samples. It bridges brief sensor hiccups without letting a multi-hour
	// outage distort the integrals.
	maxGap = 5 * time.Minute

	syncEvery = 12 // fsync the raw file roughly every 2 minutes at a 10s cadence
)

// Record is one persisted sample.
type Record struct {
	T   int64   `json:"t"`   // unix seconds
	V   float64 `json:"v"`   // bus voltage
	I   float64 `json:"i"`   // current, mA (signed: + charging)
	P   float64 `json:"p"`   // power, mW (signed)
	Pct float64 `json:"pct"` // battery percent
	Chg bool    `json:"chg"` // charging flag
}

// Store owns the data directory and the currently open raw file.
type Store struct {
	dir          string
	rawRetention time.Duration
	now          func() time.Time

	mu        sync.Mutex
	day       string
	f         *os.File
	w         *bufio.Writer
	sinceSync int
}

// Open prepares dir for use. rawRetention <= 0 defaults to 14 days.
func Open(dir string, rawRetention time.Duration) (*Store, error) {
	if rawRetention <= 0 {
		rawRetention = 14 * 24 * time.Hour
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("stats: create %s: %w", dir, err)
	}
	return &Store{dir: dir, rawRetention: rawRetention, now: time.Now}, nil
}

// Append writes one record, rotating to a new day file as needed.
func (s *Store) Append(r Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	day := time.Unix(r.T, 0).UTC().Format(dayLayout)
	if day != s.day || s.w == nil {
		if err := s.rotate(day); err != nil {
			return err
		}
	}

	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if _, err := s.w.Write(append(line, '\n')); err != nil {
		return err
	}
	if err := s.w.Flush(); err != nil {
		return err
	}
	s.sinceSync++
	if s.sinceSync >= syncEvery {
		s.sinceSync = 0
		_ = s.f.Sync()
	}
	return nil
}

func (s *Store) rotate(day string) error {
	if s.w != nil {
		s.w.Flush()
	}
	if s.f != nil {
		s.f.Sync()
		s.f.Close()
	}
	path := filepath.Join(s.dir, rawPrefix+day+rawSuffix)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("stats: open %s: %w", path, err)
	}
	s.f = f
	s.w = bufio.NewWriter(f)
	s.day = day
	s.sinceSync = 0
	return nil
}

// Close flushes and closes the open raw file.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.w != nil {
		s.w.Flush()
	}
	if s.f != nil {
		s.f.Sync()
		err := s.f.Close()
		s.f, s.w = nil, nil
		return err
	}
	return nil
}

// Maintain rolls up completed days and prunes raw files past the retention
// window. Safe to call periodically (e.g. hourly).
func (s *Store) Maintain() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.w != nil {
		s.w.Flush()
	}

	today := s.now().UTC().Format(dayLayout)
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, rawPrefix) || !strings.HasSuffix(name, rawSuffix) {
			continue
		}
		day := strings.TrimSuffix(strings.TrimPrefix(name, rawPrefix), rawSuffix)
		d, perr := time.Parse(dayLayout, day)
		if perr != nil || day >= today {
			continue // skip junk and the still-active day
		}

		dailyPath := filepath.Join(s.dir, dailyPrefix+day+dailySuffix)
		if _, statErr := os.Stat(dailyPath); os.IsNotExist(statErr) {
			recs, rerr := readRaw(filepath.Join(s.dir, name))
			if rerr != nil {
				return fmt.Errorf("stats: rollup %s: %w", name, rerr)
			}
			if err := writeDaily(dailyPath, summarise(day, recs)); err != nil {
				return err
			}
		}

		if s.now().Sub(d) > s.rawRetention {
			if err := os.Remove(filepath.Join(s.dir, name)); err != nil {
				return fmt.Errorf("stats: prune %s: %w", name, err)
			}
		}
	}
	return nil
}

// --- aggregation --------------------------------------------------------------

type agg struct {
	genWh      float64
	consWh     float64
	pctSeconds float64
	chgSeconds float64
	covSeconds float64
	samples    int
	minPct     float64
	maxPct     float64
	firstT     int64
	lastT      int64
}

func newAgg() *agg { return &agg{minPct: math.Inf(1), maxPct: math.Inf(-1)} }

// add folds a run of records (must be time-ordered) into the accumulator.
func (a *agg) add(recs []Record) {
	for _, r := range recs {
		a.samples++
		if r.Pct < a.minPct {
			a.minPct = r.Pct
		}
		if r.Pct > a.maxPct {
			a.maxPct = r.Pct
		}
		if a.firstT == 0 || r.T < a.firstT {
			a.firstT = r.T
		}
		if r.T > a.lastT {
			a.lastT = r.T
		}
	}
	gap := maxGap.Seconds()
	for i := 1; i < len(recs); i++ {
		dt := float64(recs[i].T - recs[i-1].T)
		if dt <= 0 {
			continue
		}
		if dt > gap {
			dt = gap
		}
		avgW := (recs[i].P + recs[i-1].P) / 2 / 1000 // mW -> W
		wh := avgW * dt / 3600
		if wh >= 0 {
			a.genWh += wh
		} else {
			a.consWh += -wh
		}
		a.pctSeconds += (recs[i].Pct + recs[i-1].Pct) / 2 * dt
		if recs[i].Chg && recs[i-1].Chg {
			a.chgSeconds += dt
		}
		a.covSeconds += dt
	}
}

// addDaily folds a stored rollup into the accumulator (used for the all-time
// window once raw files have been pruned).
func (a *agg) addDaily(d Daily) {
	a.genWh += d.EnergyGeneratedWh
	a.consWh += d.EnergyConsumedWh
	a.pctSeconds += d.PctSeconds
	a.chgSeconds += d.ChargingSeconds
	a.covSeconds += d.CoverageSeconds
	a.samples += d.Samples
	if d.MinPercent < a.minPct {
		a.minPct = d.MinPercent
	}
	if d.MaxPercent > a.maxPct {
		a.maxPct = d.MaxPercent
	}
	if a.firstT == 0 || (d.FirstT != 0 && d.FirstT < a.firstT) {
		a.firstT = d.FirstT
	}
	if d.LastT > a.lastT {
		a.lastT = d.LastT
	}
}

// Window is one time window in the report.
type Window struct {
	EnergyGeneratedWh float64 `json:"energy_generated_wh"`
	EnergyConsumedWh  float64 `json:"energy_consumed_wh"`
	AvgPercent        float64 `json:"avg_percent"`
	ChargingSeconds   float64 `json:"charging_seconds"`
	ChargingFraction  float64 `json:"charging_fraction"`
	CoverageSeconds   float64 `json:"coverage_seconds"`
	Samples           int     `json:"samples"`
}

func (a *agg) window() Window {
	w := Window{
		EnergyGeneratedWh: round2(a.genWh),
		EnergyConsumedWh:  round2(a.consWh),
		ChargingSeconds:   math.Round(a.chgSeconds),
		CoverageSeconds:   math.Round(a.covSeconds),
		Samples:           a.samples,
	}
	if a.covSeconds > 0 {
		w.AvgPercent = round1(a.pctSeconds / a.covSeconds)
		w.ChargingFraction = round3(a.chgSeconds / a.covSeconds)
	}
	return w
}

// Daily is the persisted per-day rollup.
type Daily struct {
	Date              string  `json:"date"`
	Samples           int     `json:"samples"`
	EnergyGeneratedWh float64 `json:"energy_generated_wh"`
	EnergyConsumedWh  float64 `json:"energy_consumed_wh"`
	PctSeconds        float64 `json:"pct_seconds"`
	ChargingSeconds   float64 `json:"charging_seconds"`
	CoverageSeconds   float64 `json:"coverage_seconds"`
	MinPercent        float64 `json:"min_percent"`
	MaxPercent        float64 `json:"max_percent"`
	FirstT            int64   `json:"first_t"`
	LastT             int64   `json:"last_t"`
}

func summarise(day string, recs []Record) Daily {
	a := newAgg()
	a.add(recs)
	d := Daily{
		Date:              day,
		Samples:           a.samples,
		EnergyGeneratedWh: a.genWh,
		EnergyConsumedWh:  a.consWh,
		PctSeconds:        a.pctSeconds,
		ChargingSeconds:   a.chgSeconds,
		CoverageSeconds:   a.covSeconds,
		FirstT:            a.firstT,
		LastT:             a.lastT,
	}
	if a.samples > 0 {
		d.MinPercent = a.minPct
		d.MaxPercent = a.maxPct
	}
	return d
}

// --- report -----------------------------------------------------------------

// Report is the payload behind GET /api/sensor.
type Report struct {
	Day         Window    `json:"day"`  // trailing 24h
	Week        Window    `json:"week"` // trailing 7d
	All         Window    `json:"all"`
	Since       time.Time `json:"since"`
	GeneratedAt time.Time `json:"generated_at"`
}

// Stats builds the report from whatever is on disk.
func (s *Store) Stats() (Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.w != nil {
		s.w.Flush()
	}

	now := s.now()
	rawDates, dailyDates, err := s.listData()
	if err != nil {
		return Report{}, err
	}

	// Cache raw file contents; each window reuses them.
	rawCache := map[string][]Record{}
	loadRaw := func(day string) ([]Record, error) {
		if recs, ok := rawCache[day]; ok {
			return recs, nil
		}
		recs, err := readRaw(filepath.Join(s.dir, rawPrefix+day+rawSuffix))
		if err != nil {
			return nil, err
		}
		rawCache[day] = recs
		return recs, nil
	}

	windowAgg := func(since time.Time) (Window, error) {
		a := newAgg()
		cut := since.Unix()
		for _, day := range rawDates {
			recs, err := loadRaw(day)
			if err != nil {
				return Window{}, err
			}
			if len(recs) == 0 || recs[len(recs)-1].T < cut {
				continue // entire file predates the window
			}
			filtered := recs
			if recs[0].T < cut {
				filtered = filterFrom(recs, cut)
			}
			a.add(filtered)
		}
		return a.window(), nil
	}

	dayWin, err := windowAgg(now.Add(-24 * time.Hour))
	if err != nil {
		return Report{}, err
	}
	weekWin, err := windowAgg(now.Add(-7 * 24 * time.Hour))
	if err != nil {
		return Report{}, err
	}

	// All-time: raw days are authoritative; add rollups for days with no raw.
	all := newAgg()
	rawSet := map[string]bool{}
	for _, date := range rawDates {
		rawSet[date] = true
		recs, err := loadRaw(date)
		if err != nil {
			return Report{}, err
		}
		all.add(recs)
	}
	for _, date := range dailyDates {
		if rawSet[date] {
			continue
		}
		d, err := readDaily(filepath.Join(s.dir, dailyPrefix+date+dailySuffix))
		if err != nil {
			return Report{}, err
		}
		all.addDaily(d)
	}

	rep := Report{
		Day:         dayWin,
		Week:        weekWin,
		All:         all.window(),
		GeneratedAt: now,
	}
	if all.firstT != 0 {
		rep.Since = time.Unix(all.firstT, 0).UTC()
	}
	return rep, nil
}

func (s *Store) listData() (rawDates, dailyDates []string, err error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		n := e.Name()
		switch {
		case strings.HasPrefix(n, rawPrefix) && strings.HasSuffix(n, rawSuffix):
			d := strings.TrimSuffix(strings.TrimPrefix(n, rawPrefix), rawSuffix)
			if _, perr := time.Parse(dayLayout, d); perr == nil {
				rawDates = append(rawDates, d)
			}
		case strings.HasPrefix(n, dailyPrefix) && strings.HasSuffix(n, dailySuffix):
			d := strings.TrimSuffix(strings.TrimPrefix(n, dailyPrefix), dailySuffix)
			if _, perr := time.Parse(dayLayout, d); perr == nil {
				dailyDates = append(dailyDates, d)
			}
		}
	}
	sort.Strings(rawDates)
	sort.Strings(dailyDates)
	return rawDates, dailyDates, nil
}

// --- file helpers ---------------------------------------------------------

func readRaw(path string) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var recs []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r Record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			continue // tolerate a torn final line after a crash
		}
		recs = append(recs, r)
	}
	if err := sc.Err(); err != nil {
		return recs, err
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].T < recs[j].T })
	return recs, nil
}

func filterFrom(recs []Record, cut int64) []Record {
	i := sort.Search(len(recs), func(i int) bool { return recs[i].T >= cut })
	if i == 0 {
		return recs
	}
	// Keep one sample before the cut so the first interval isn't lost.
	return recs[i-1:]
}

func writeDaily(path string, d Daily) error {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readDaily(path string) (Daily, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Daily{}, err
	}
	var d Daily
	err = json.Unmarshal(b, &d)
	return d, err
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
func round2(v float64) float64 { return math.Round(v*100) / 100 }
func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
