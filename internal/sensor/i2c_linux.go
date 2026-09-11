//go:build linux

package sensor

import (
	"fmt"
	"log"
	"math"
	"os"
	"sync"
	"syscall"
)

// i2cSlave is the ioctl request number for selecting the target device
// address on a Linux i2c-dev bus (from <linux/i2c-dev.h>).
const i2cSlave = 0x0703

// i2cDev is a wordBus backed by a real /dev/i2c-* character device. It uses the
// simple write-register-then-read pattern, which the INA219 supports; every
// value on this chip is a big-endian 16-bit word.
type i2cDev struct {
	mu   sync.Mutex
	f    *os.File
	addr int
}

func openI2C(bus string, addr int) (*i2cDev, error) {
	f, err := os.OpenFile(bus, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", bus, err)
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), i2cSlave, uintptr(addr)); errno != 0 {
		f.Close()
		return nil, fmt.Errorf("select i2c address %#02x on %s: %w", addr, bus, errno)
	}
	return &i2cDev{f: f, addr: addr}, nil
}

func (d *i2cDev) writeReg(reg uint8, val uint16) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	buf := [3]byte{reg, byte(val >> 8), byte(val)}
	n, err := d.f.Write(buf[:])
	if err != nil {
		return err
	}
	if n != len(buf) {
		return fmt.Errorf("short i2c write: %d/%d bytes", n, len(buf))
	}
	return nil
}

func (d *i2cDev) readReg(reg uint8) (uint16, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, err := d.f.Write([]byte{reg}); err != nil {
		return 0, fmt.Errorf("set register pointer %#02x: %w", reg, err)
	}
	var buf [2]byte
	n, err := d.f.Read(buf[:])
	if err != nil {
		return 0, err
	}
	if n != len(buf) {
		return 0, fmt.Errorf("short i2c read: %d/%d bytes", n, len(buf))
	}
	return uint16(buf[0])<<8 | uint16(buf[1]), nil
}

func (d *i2cDev) close() error { return d.f.Close() }

// newHardware is the Linux constructor selected by New.
func newHardware(cfg Config) (Sensor, error) {
	if _, err := os.Stat(cfg.Bus); err != nil {
		return nil, fmt.Errorf("sensor: i2c bus %s not available: %w (run with -mock for local dev)", cfg.Bus, err)
	}
	if math.Abs(cfg.ShuntOhms-0.1) > 1e-9 {
		log.Printf("sensor: shunt %.3f ohm requested but calibration is fixed for the 0.1 ohm Adafruit breakout; current/power will be off", cfg.ShuntOhms)
	}
	bus, err := openI2C(cfg.Bus, cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("sensor: %w", err)
	}
	dev, err := newINA219(bus)
	if err != nil {
		bus.close()
		return nil, err
	}
	return dev, nil
}
