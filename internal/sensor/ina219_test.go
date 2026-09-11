package sensor

import (
	"math"
	"testing"
)

// fakeBus is an in-memory wordBus for exercising the register math without
// hardware.
type fakeBus struct {
	regs    map[uint8]uint16
	writes  []uint8
	failOn  uint8
	failErr error
}

func newFakeBus() *fakeBus { return &fakeBus{regs: map[uint8]uint16{}} }

func (b *fakeBus) writeReg(reg uint8, val uint16) error {
	if b.failErr != nil && reg == b.failOn {
		return b.failErr
	}
	b.regs[reg] = val
	b.writes = append(b.writes, reg)
	return nil
}

func (b *fakeBus) readReg(reg uint8) (uint16, error) {
	if b.failErr != nil && reg == b.failOn {
		return 0, b.failErr
	}
	return b.regs[reg], nil
}

func (b *fakeBus) close() error { return nil }

func approx(t *testing.T, got, want, tol float64, label string) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s: got %.4f, want %.4f (+-%.4f)", label, got, want, tol)
	}
}

func TestINA219ConfiguresChip(t *testing.T) {
	b := newFakeBus()
	if _, err := newINA219(b); err != nil {
		t.Fatalf("newINA219: %v", err)
	}
	if b.regs[regCalibration] != calValue32V2A {
		t.Errorf("calibration = %#04x, want %#04x", b.regs[regCalibration], calValue32V2A)
	}
	if b.regs[regConfig] != configValue {
		t.Errorf("config = %#04x, want %#04x", b.regs[regConfig], configValue)
	}
}

func TestINA219ReadScaling(t *testing.T) {
	b := newFakeBus()
	d, err := newINA219(b)
	if err != nil {
		t.Fatalf("newINA219: %v", err)
	}

	// Bus voltage: value lives in bits 3..15, LSB 4 mV. 4.200 V -> 1050 counts.
	b.regs[regBusVoltage] = uint16(1050) << 3
	// Shunt voltage: LSB 10 uV. +3200 counts -> 32 mV.
	b.regs[regShuntVoltage] = 3200
	// Current: LSB 0.1 mA. +4500 counts -> 450 mA (charging).
	b.regs[regCurrent] = 4500

	s, err := d.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	approx(t, s.BusVoltage, 4.200, 0.001, "bus voltage")
	approx(t, s.ShuntVoltage, 0.032, 0.0001, "shunt voltage")
	approx(t, s.CurrentMA, 450, 0.01, "current mA")
	approx(t, s.PowerMW, 4.200*450, 0.5, "power mW")
}

func TestINA219NegativeCurrent(t *testing.T) {
	b := newFakeBus()
	d, _ := newINA219(b)
	b.regs[regBusVoltage] = uint16(925) << 3 // 3.700 V
	current := int16(-1600)                  // -160.0 mA (discharging)
	b.regs[regCurrent] = uint16(current)

	s, err := d.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	approx(t, s.CurrentMA, -160, 0.01, "current mA")
	if s.PowerMW >= 0 {
		t.Errorf("power should be negative while discharging, got %.2f", s.PowerMW)
	}
}

func TestINA219OverflowFlag(t *testing.T) {
	b := newFakeBus()
	d, _ := newINA219(b)
	b.regs[regBusVoltage] = 0x0001 // OVF set
	if _, err := d.Read(); err == nil {
		t.Fatal("expected error when overflow flag is set")
	}
}
