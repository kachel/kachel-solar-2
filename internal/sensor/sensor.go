// Package sensor reads the Adafruit INA219 high-side current/voltage monitor
// over I2C. The INA219 sits on the battery line of the power chain (battery
// <-> bq24074 charger / PowerBoost split), so a positive shunt current means
// charge flowing into the battery and a negative current means the Pi is
// draining it.
//
// The concrete INA219 driver only builds on Linux (it talks to /dev/i2c-*).
// On every other platform New falls back to an error unless the caller asks
// for the Mock, which lets the rest of the program build and run on a laptop.
package sensor

import "errors"

// Sample is one instantaneous reading from the sensor. All values are already
// converted to physical units.
type Sample struct {
	BusVoltage   float64 // volts, measured battery-side of the shunt
	ShuntVoltage float64 // volts across the shunt resistor
	CurrentMA    float64 // milliamps through the shunt (signed)
	PowerMW      float64 // milliwatts (bus voltage * current, from the chip)
}

// Sensor is the minimal interface the sampler depends on.
type Sensor interface {
	// Read returns a fresh sample or an error if the device could not be
	// reached. It must be safe to call repeatedly from a single goroutine.
	Read() (Sample, error)
	// Close releases the underlying handle.
	Close() error
}

// ErrUnsupported is returned by New on non-Linux platforms when a real device
// was requested.
var ErrUnsupported = errors.New("sensor: hardware I2C is only supported on Linux; run with -mock")

// Config selects and configures a sensor.
type Config struct {
	// Mock forces the simulated sensor regardless of platform.
	Mock bool
	// Bus is the I2C bus device, e.g. "/dev/i2c-1".
	Bus string
	// Addr is the 7-bit I2C address of the INA219 (default 0x40).
	Addr int
	// ShuntOhms is the value of the shunt resistor. The Adafruit INA219
	// breakout ships with 0.1 ohm.
	ShuntOhms float64
}

// New builds a Sensor from cfg. When cfg.Mock is set (or the Linux driver is
// unavailable and the bus device is missing) it returns a Mock.
func New(cfg Config) (Sensor, error) {
	if cfg.Addr == 0 {
		cfg.Addr = 0x40
	}
	if cfg.ShuntOhms == 0 {
		cfg.ShuntOhms = 0.1
	}
	if cfg.Mock {
		return NewMock(), nil
	}
	return newHardware(cfg)
}
