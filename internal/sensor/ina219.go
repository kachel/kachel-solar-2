package sensor

import (
	"fmt"
	"time"
)

// INA219 register addresses.
const (
	regConfig       = 0x00
	regShuntVoltage = 0x01
	regBusVoltage   = 0x02
	regPower        = 0x03
	regCurrent      = 0x04
	regCalibration  = 0x05
)

// Fixed-point scale factors for the "32V / 2A" calibration used below. This is
// the standard Adafruit profile and is comfortably wide for a 1-cell LiPo that
// never exceeds ~4.3 V or ~1 A in either direction.
const (
	currentLSB_mA  = 0.1  // milliamps per current-register count
	powerLSB_mW    = 2.0  // milliwatts per power-register count (= 20 * currentLSB)
	busVoltageLSB  = 4.0  // millivolts per bus-voltage count (bits 3..15)
	shuntVoltageuV = 10.0 // microvolts per shunt-voltage count
	calValue32V2A  = 4096 // calibration register value for the profile above
)

// Config register value: 32 V bus range (PGA /8, +-320 mV shunt), 12-bit ADC
// with 128-sample averaging on both channels, continuous shunt+bus mode.
//   BRNG=1 (bit13), PG=11 (bits11-12), BADC=1111 (bits7-10), SADC=1111 (bits3-6),
//   MODE=111 (bits0-2)
const configValue = 0x3FFF

// wordBus is the 16-bit big-endian register transport an INA219 needs. The
// Linux implementation talks to /dev/i2c-*; tests supply a fake.
type wordBus interface {
	writeReg(reg uint8, val uint16) error
	readReg(reg uint8) (uint16, error)
	close() error
}

type ina219 struct {
	bus wordBus
}

// newINA219 configures the chip and returns a ready sensor. It is used by the
// platform-specific newHardware constructors and by tests (with a fake bus).
func newINA219(bus wordBus) (*ina219, error) {
	d := &ina219{bus: bus}
	if err := d.bus.writeReg(regCalibration, calValue32V2A); err != nil {
		return nil, fmt.Errorf("ina219: write calibration: %w", err)
	}
	if err := d.bus.writeReg(regConfig, configValue); err != nil {
		return nil, fmt.Errorf("ina219: write config: %w", err)
	}
	// The first conversion after (re)configuring can take a few ms with
	// 128-sample averaging; give it a moment before the caller reads.
	time.Sleep(2 * time.Millisecond)
	return d, nil
}

func (d *ina219) Read() (Sample, error) {
	// Re-write calibration each cycle: it is the one register that is lost if
	// the chip briefly browns out with the rest of the Pi, and a zero
	// calibration silently yields zero current/power.
	if err := d.bus.writeReg(regCalibration, calValue32V2A); err != nil {
		return Sample{}, fmt.Errorf("ina219: refresh calibration: %w", err)
	}

	busRaw, err := d.bus.readReg(regBusVoltage)
	if err != nil {
		return Sample{}, fmt.Errorf("ina219: read bus voltage: %w", err)
	}
	// Bit 1 (CNVR) ready flag, bit 0 (OVF) overflow. Value is bits 3..15.
	if busRaw&0x0001 != 0 {
		return Sample{}, fmt.Errorf("ina219: math overflow flag set (raw=%#04x)", busRaw)
	}
	busMV := float64(busRaw>>3) * busVoltageLSB

	shuntRaw, err := d.bus.readReg(regShuntVoltage)
	if err != nil {
		return Sample{}, fmt.Errorf("ina219: read shunt voltage: %w", err)
	}
	shuntUV := float64(int16(shuntRaw)) * shuntVoltageuV

	currentRaw, err := d.bus.readReg(regCurrent)
	if err != nil {
		return Sample{}, fmt.Errorf("ina219: read current: %w", err)
	}
	currentMA := float64(int16(currentRaw)) * currentLSB_mA

	busVoltage := busMV / 1000

	// The chip's POWER register is unsigned and only valid while current is
	// positive, so derive power ourselves: it then carries the same sign as
	// the current (negative == discharging).
	powerMW := busVoltage * currentMA

	return Sample{
		BusVoltage:   busVoltage,
		ShuntVoltage: shuntUV / 1_000_000,
		CurrentMA:    currentMA,
		PowerMW:      powerMW,
	}, nil
}

func (d *ina219) Close() error { return d.bus.close() }
