package battery

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"kachel.solar/internal/sensor"
)

func TestPercentEndpointsAndClamp(t *testing.T) {
	cases := []struct{ v, want float64 }{
		{2.5, 0},
		{3.27, 0},
		{3.84, 50},
		{4.20, 100},
		{4.5, 100},
	}
	for _, c := range cases {
		if got := Percent(c.v); got != c.want {
			t.Errorf("Percent(%.2f) = %.2f, want %.2f", c.v, got, c.want)
		}
	}
}

func TestPercentMonotonic(t *testing.T) {
	prev := -1.0
	for v := 3.2; v <= 4.25; v += 0.01 {
		p := Percent(v)
		if p < prev {
			t.Fatalf("Percent not monotonic at %.2f: %.2f < %.2f", v, p, prev)
		}
		prev = p
	}
}

func TestInterpretChargeStates(t *testing.T) {
	cfg := Config{ChargeThresholdMA: 15}
	now := time.Unix(1_700_000_000, 0)

	charging := cfg.Interpret(sensor.Sample{BusVoltage: 3.95, CurrentMA: 400}, now)
	if !charging.Charging || charging.ChargeState != StateCharging {
		t.Errorf("expected charging, got %+v", charging)
	}

	discharging := cfg.Interpret(sensor.Sample{BusVoltage: 3.80, CurrentMA: -160}, now)
	if discharging.Charging || discharging.ChargeState != StateDischarging {
		t.Errorf("expected discharging, got %+v", discharging)
	}

	// High terminal voltage but the charger has stopped pushing current:
	// topped off and floating.
	full := cfg.Interpret(sensor.Sample{BusVoltage: 4.19, CurrentMA: 4}, now)
	if full.Charging || full.ChargeState != StateFull {
		t.Errorf("expected full (not charging), got %+v", full)
	}

	// Still bulk-charging at 4.20 V / 500 mA: that's "charging", not "full".
	bulk := cfg.Interpret(sensor.Sample{BusVoltage: 4.20, CurrentMA: 500}, now)
	if !bulk.Charging || bulk.ChargeState != StateCharging {
		t.Errorf("expected charging during CV bulk, got %+v", bulk)
	}

	idle := cfg.Interpret(sensor.Sample{BusVoltage: 3.85, CurrentMA: 2}, now)
	if idle.Charging || idle.ChargeState != StateIdle {
		t.Errorf("expected idle, got %+v", idle)
	}
}

func TestInterpretInvertCurrent(t *testing.T) {
	cfg := Config{ChargeThresholdMA: 15, InvertCurrent: true}
	now := time.Unix(1_700_000_000, 0)
	// Raw says +400 (would be "charging"); inverted it must read as discharging.
	r := cfg.Interpret(sensor.Sample{BusVoltage: 3.9, CurrentMA: 400}, now)
	if r.Charging || r.ChargeState != StateDischarging {
		t.Errorf("invert: expected discharging, got %+v", r)
	}
	if r.CurrentMA != -400 {
		t.Errorf("invert: current = %.1f, want -400", r.CurrentMA)
	}
}

// stubSensor returns a fixed sample or error on demand.
type stubSensor struct {
	mu     sync.Mutex
	sample sensor.Sample
	err    error
}

func (s *stubSensor) Read() (sensor.Sample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sample, s.err
}
func (s *stubSensor) Close() error { return nil }
func (s *stubSensor) set(sm sensor.Sample, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sample, s.err = sm, err
}

func TestSamplerCachesAndGoesStale(t *testing.T) {
	stub := &stubSensor{sample: sensor.Sample{BusVoltage: 3.9, CurrentMA: 100}}
	var appended int
	s := NewSampler(stub, Config{StaleAfter: 30 * time.Second}, time.Minute, func(Reading) { appended++ })

	fake := time.Unix(1_700_000_000, 0)
	s.now = func() time.Time { return fake }

	if _, ok := s.Current(); ok {
		t.Fatal("Current should report no reading before first sample")
	}

	s.sample()
	r, ok := s.Current()
	if !ok || r.Stale {
		t.Fatalf("after first sample: ok=%v stale=%v", ok, r.Stale)
	}
	if appended != 1 {
		t.Fatalf("onSample called %d times, want 1", appended)
	}

	// Sensor starts failing; last good reading must persist and go stale.
	stub.set(sensor.Sample{}, errors.New("i2c timeout"))
	s.sample()
	fake = fake.Add(45 * time.Second)
	r, ok = s.Current()
	if !ok {
		t.Fatal("stale reading should still be returned")
	}
	if !r.Stale {
		t.Fatal("reading should be stale after StaleAfter elapsed with no fresh sample")
	}
	if appended != 1 {
		t.Fatalf("onSample should not fire on failure, got %d", appended)
	}
}

func TestSamplerRunStopsOnContext(t *testing.T) {
	stub := &stubSensor{sample: sensor.Sample{BusVoltage: 3.9}}
	s := NewSampler(stub, Config{}, 10*time.Millisecond, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	time.Sleep(35 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancel")
	}
	if _, ok := s.Current(); !ok {
		t.Fatal("expected at least one reading after Run")
	}
}
