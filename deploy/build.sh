#!/usr/bin/env bash
# Cross-compile kacheld for the Raspberry Pi Zero (original: ARMv6, 32-bit).
# Run this on any machine with Go installed; copy dist/kacheld to the Pi.
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
OUT="dist/kacheld"

mkdir -p dist

echo "building kacheld ${VERSION} for linux/arm (GOARM=6)"
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=6 \
  go build -trimpath \
  -ldflags "-s -w -X main.version=${VERSION}" \
  -o "${OUT}" ./cmd/kacheld

echo "wrote ${OUT}"
file "${OUT}" 2>/dev/null || true
ls -lh "${OUT}"
