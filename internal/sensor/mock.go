package sensor

import (
	"math"
	"sync"
	"time"
)

// Mock is a simulated INA219. It models a single 3000 mAh LiPo cell on a small
// solar panel: the panel charges during local daylight, the Pi draws a roughly
// constant load, and the state of charge drifts accordingly. It exists so the
// server (and the website that talks to it) can run on a development machine.
type Mock struct {
	mu       sync.Mutex
	percent  float64
	last     time.Time
	capacity float64 // mAh
	loadMA   float64 // constant discharge current
	now      func() time.Time
}

// NewMock returns a Mock starting at a plausible mid-morning state of charge.
func NewMock() *Mock {
	return &Mock{
		percent:  68,
		capacity: 3000,
		loadMA:   160,
		now:      time.Now,
	}
}

// solarMA returns the panel's charge current for the given local time: a clean
// half-sine between roughly 06:00 and 18:00, peaking near solar noon. The
// Voltaic P105 is a 5 W / 6 V panel, so ~600 mA into the charger at its best.
func solarMA(t time.Time) float64 {
	hour := float64(t.Hour()) + float64(t.Minute())/60
	const sunrise, sunset = 6.0, 18.0
	if hour < sunrise || hour > sunset {
		return 0
	}
	frac := (hour - sunrise) / (sunset - sunrise) // 0..1 across the day
	return 600 * math.Sin(frac*math.Pi)
}

// voltageFor maps a state of charge back to an approximate resting cell
// voltage (inverse of the discharge curve used in package battery).
func voltageFor(percent float64) float64 {
	switch {
	case percent >= 100:
		return 4.20
	case percent <= 0:
		return 3.27
	}
	// Piecewise-linear, matching battery.socCurve reasonably closely.
	pts := [][2]float64{
		{0, 3.27}, {5, 3.61}, {10, 3.69}, {20, 3.73}, {35, 3.79},
		{50, 3.84}, {65, 3.91}, {80, 4.02}, {90, 4.11}, {100, 4.20},
	}
	for i := 1; i < len(pts); i++ {
		if percent <= pts[i][0] {
			lo, hi := pts[i-1], pts[i]
			f := (percent - lo[0]) / (hi[0] - lo[0])
			return lo[1] + f*(hi[1]-lo[1])
		}
	}
	return 4.20
}

func (m *Mock) Read() (Sample, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	t := m.now()
	if !m.last.IsZero() {
		dt := t.Sub(m.last)
		if dt < 0 {
			dt = 0
		}
		if dt > time.Hour {
			dt = time.Hour // don't fast-forward wildly after a long pause
		}
		netMA := solarMA(t) - m.loadMA
		deltaMAh := netMA * dt.Hours()
		m.percent += deltaMAh / m.capacity * 100
		m.percent = math.Max(2, math.Min(100, m.percent))
	}
	m.last = t

	current := solarMA(t) - m.loadMA // signed: + charging, - discharging
	bus := voltageFor(m.percent)
	if current > 0 {
		bus += 0.15 * (current / 600) // charging pushes terminal voltage up
	}
	shunt := current / 1000 * 0.1 // 0.1 ohm shunt, volts

	return Sample{
		BusVoltage:   bus,
		ShuntVoltage: shunt,
		CurrentMA:    current,
		PowerMW:      bus * current,
	}, nil
}

func (m *Mock) Close() error { return nil }
