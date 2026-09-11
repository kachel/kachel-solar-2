.PHONY: test vet build run clean

test:
	go test ./...

vet:
	go vet ./...

# Cross-compile for the Pi Zero (ARMv6).
build:
	./deploy/build.sh

# Run locally against the simulated sensor.
run:
	go run ./cmd/kacheld -mock -web ./web -data ./data

clean:
	rm -rf dist
