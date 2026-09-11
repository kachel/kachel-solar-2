//go:build !linux

package sensor

// newHardware is the fallback constructor on platforms without Linux i2c-dev.
// The program still builds and runs there; callers must pass -mock.
func newHardware(cfg Config) (Sensor, error) {
	return nil, ErrUnsupported
}
