package stats

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func mustOpen(t *testing.T, retention time.Duration) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir, retention)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s, dir
}

// writeSeries appends count records starting at start, step apart, each with
// the given signed power (mW), percent and charging flag.
func writeSeries(t *testing.T, s *Store, start time.Time, step time.Duration, count int, powerMW, pct float64, chg bool) {
	t.Helper()
	for i := 0; i < count; i++ {
		ts := start.Add(time.Duration(i) * step)
		if err := s.Append(Record{
			T: ts.Unix(), V: 3.9, I: powerMW / 3.9, P: powerMW, Pct: pct, Chg: chg,
		}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
}

func approxEq(t *testing.T, got, want, tol float64, label string) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s: got %.4f, want %.4f (tol %.4f)", label, got, want, tol)
	}
}

func TestStatsEnergyAndCharging(t *testing.T) {
	s, _ := mustOpen(t, 0)
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	// 25 samples, 300s apart => 2h of coverage at a constant 3.6 W charge.
	writeSeries(t, s, base, 5*time.Minute, 25, 3600, 50, true)
	s.now = func() time.Time { return base.Add(3 * time.Hour) }

	rep, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}

	approxEq(t, rep.Day.EnergyGeneratedWh, 7.2, 0.05, "day energy generated")
	approxEq(t, rep.Day.EnergyConsumedWh, 0, 0.001, "day energy consumed")
	approxEq(t, rep.Day.AvgPercent, 50, 0.01, "day avg percent")
	approxEq(t, rep.Day.CoverageSeconds, 7200, 1, "day coverage seconds")
	approxEq(t, rep.Day.ChargingSeconds, 7200, 1, "day charging seconds")
	approxEq(t, rep.Day.ChargingFraction, 1, 0.001, "day charging fraction")
	if rep.Day.Samples != 25 {
		t.Errorf("day samples = %d, want 25", rep.Day.Samples)
	}
	if rep.Since.IsZero() || !rep.Since.Equal(base.UTC()) {
		t.Errorf("since = %v, want %v", rep.Since, base.UTC())
	}
}

func TestStatsDischargeCountsAsConsumed(t *testing.T) {
	s, _ := mustOpen(t, 0)
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	writeSeries(t, s, base, 5*time.Minute, 13, -2000, 40, false) // 1h at -2 W
	s.now = func() time.Time { return base.Add(2 * time.Hour) }

	rep, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	approxEq(t, rep.Day.EnergyConsumedWh, 2.0, 0.05, "consumed")
	approxEq(t, rep.Day.EnergyGeneratedWh, 0, 0.001, "generated")
	approxEq(t, rep.Day.ChargingSeconds, 0, 0.001, "charging seconds")
}

func TestMaintainRollsUpAndPrunes(t *testing.T) {
	s, dir := mustOpen(t, 24*time.Hour)
	old := time.Date(2026, 2, 26, 0, 0, 0, 0, time.UTC)
	writeSeries(t, s, old, 5*time.Minute, 13, 3600, 60, true) // 1h, 3.6 W

	today := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	writeSeries(t, s, today, 5*time.Minute, 5, 1000, 70, true)
	s.now = func() time.Time { return today }

	if err := s.Maintain(); err != nil {
		t.Fatalf("Maintain: %v", err)
	}

	dailyPath := filepath.Join(dir, "daily-2026-02-26.json")
	d, err := readDaily(dailyPath)
	if err != nil {
		t.Fatalf("expected rollup file: %v", err)
	}
	approxEq(t, d.EnergyGeneratedWh, 3.6, 0.05, "rollup energy")
	if d.Samples != 13 {
		t.Errorf("rollup samples = %d, want 13", d.Samples)
	}

	if _, err := os.Stat(filepath.Join(dir, "raw-2026-02-26.jsonl")); !os.IsNotExist(err) {
		t.Errorf("old raw file should have been pruned, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "raw-2026-03-01.jsonl")); err != nil {
		t.Errorf("today's raw file should remain: %v", err)
	}

	// All-time must still include the pruned day via its rollup.
	rep, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if rep.All.EnergyGeneratedWh < 3.6 {
		t.Errorf("all-time energy %.3f should include rolled-up day (>=3.6)", rep.All.EnergyGeneratedWh)
	}
	if rep.All.Samples != 18 {
		t.Errorf("all-time samples = %d, want 18 (13 rolled + 5 raw)", rep.All.Samples)
	}
}

func TestMaintainKeepsRawWithinRetention(t *testing.T) {
	s, dir := mustOpen(t, 30*24*time.Hour)
	old := time.Date(2026, 2, 26, 0, 0, 0, 0, time.UTC)
	writeSeries(t, s, old, 5*time.Minute, 4, 3600, 60, true)
	s.now = func() time.Time { return time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC) }

	if err := s.Maintain(); err != nil {
		t.Fatalf("Maintain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "daily-2026-02-26.json")); err != nil {
		t.Errorf("rollup should exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "raw-2026-02-26.jsonl")); err != nil {
		t.Errorf("raw within retention should be kept: %v", err)
	}
}

func TestReadRawToleratesTornLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "raw-2026-03-01.jsonl")
	content := `{"t":1,"v":3.9,"i":10,"p":39,"pct":50,"chg":true}
{"t":2,"v":3.9,"i":10,"p":39,"pct":50,"chg":true}
{"t":3,"v":3.9,"i":10,"p":39,"pct":5` // crash mid-write
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	recs, err := readRaw(path)
	if err != nil {
		t.Fatalf("readRaw: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2 (torn line skipped)", len(recs))
	}
}

func TestStatsEmpty(t *testing.T) {
	s, _ := mustOpen(t, 0)
	s.now = func() time.Time { return time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC) }
	rep, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats on empty store: %v", err)
	}
	if rep.Day.Samples != 0 || rep.All.Samples != 0 {
		t.Errorf("expected zero samples, got day=%d all=%d", rep.Day.Samples, rep.All.Samples)
	}
	if !rep.Since.IsZero() {
		t.Errorf("since should be zero on empty store, got %v", rep.Since)
	}
}
