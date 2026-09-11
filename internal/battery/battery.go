// Package battery turns raw INA219 samples into the state the site cares
// about: a charge percentage, a charging flag, and a coarse charge state.
package battery

import (
	"math"
	"time"

	"kachel.solar/internal/sensor"
)

// State is a coarse, human-facing charge state for the widget.
type State string

const (
	StateCharging    State = "charging"
	StateDischarging State = "discharging"
	StateFull        State = "full"
	StateIdle        State = "idle" // on the charger but not moving much current
)

// Reading is the payload behind GET /api/battery.
type Reading struct {
	Percent     float64   `json:"percent"`
	Charging    bool      `json:"charging"`
	ChargeState State     `json:"charge_state"`
	Voltage     float64   `json:"voltage"`
	CurrentMA   float64   `json:"current_ma"`
	PowerMW     float64   `json:"power_mw"`
	UpdatedAt   time.Time `json:"updated_at"`
	Stale       bool      `json:"stale"`
}

// Config tunes how samples are interpreted.
type Config struct {
	// ChargeThresholdMA is the current magnitude above which the pack is
	// considered to be actively charging or discharging rather than idle.
	ChargeThresholdMA float64
	// InvertCurrent flips the sign of the measured current, for the case
	// where the INA219 V+/V- terminals ended up wired the other way round.
	InvertCurrent bool
	// StaleAfter is how long a reading may go without a fresh successful
	// sample before it is flagged stale to callers.
	StaleAfter time.Duration
}

// WithDefaults fills unset fields with sensible values.
func (c Config) WithDefaults() Config {
	if c.ChargeThresholdMA == 0 {
		c.ChargeThresholdMA = 15
	}
	if c.StaleAfter == 0 {
		c.StaleAfter = 45 * time.Second
	}
	return c
}

// socCurve maps resting cell voltage to state of charge for a single LiPo
// cell under a light load. Points are ascending by voltage; Percent
// interpolates linearly between them and clamps at the ends.
var socCurve = []struct{ volts, percent float64 }{
	{3.27, 0}, {3.61, 5}, {3.69, 10}, {3.71, 15}, {3.73, 20},
	{3.75, 25}, {3.77, 30}, {3.79, 35}, {3.82, 40}, {3.84, 50},
	{3.85, 55}, {3.87, 60}, {3.91, 65}, {3.95, 70}, {3.98, 75},
	{4.02, 80}, {4.08, 85}, {4.11, 90}, {4.15, 95}, {4.20, 100},
}

// Percent returns the state of charge (0-100) for a cell voltage.
func Percent(volts float64) float64 {
	if volts <= socCurve[0].volts {
		return 0
	}
	last := socCurve[len(socCurve)-1]
	if volts >= last.volts {
		return 100
	}
	for i := 1; i < len(socCurve); i++ {
		hi := socCurve[i]
		if volts <= hi.volts {
			lo := socCurve[i-1]
			f := (volts - lo.volts) / (hi.volts - lo.volts)
			return lo.percent + f*(hi.percent-lo.percent)
		}
	}
	return 100
}

// Interpret converts a sample into a Reading as of now. Stale is always false
// here; the sampler sets it based on how old the underlying sample is.
func (c Config) Interpret(s sensor.Sample, now time.Time) Reading {
	cfg := c.WithDefaults()

	current := s.CurrentMA
	power := s.PowerMW
	if cfg.InvertCurrent {
		current = -current
		power = -power
	}

	// Estimate the resting voltage: while charging, the terminal voltage is
	// inflated by the pack's internal resistance, so pull it back down a
	// little using the shunt-derived current. ~0.15 ohm effective series
	// resistance is a rough but stable guess for this pack + wiring.
	restVolts := s.BusVoltage
	if current > 0 {
		restVolts -= current / 1000 * 0.15
	}
	percent := Percent(restVolts)

	r := Reading{
		Percent:   round1(percent),
		Voltage:   round3(s.BusVoltage),
		CurrentMA: round1(current),
		PowerMW:   round1(power),
		UpdatedAt: now,
	}

	switch {
	case current > cfg.ChargeThresholdMA:
		r.ChargeState = StateCharging
		r.Charging = true
	case current < -cfg.ChargeThresholdMA:
		r.ChargeState = StateDischarging
	default:
		// Near-zero current. If the bus voltage is up at charger level the
		// pack is topped off and floating; otherwise it is just resting.
		if s.BusVoltage >= 4.15 {
			r.ChargeState = StateFull
		} else {
			r.ChargeState = StateIdle
		}
	}
	return r
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
