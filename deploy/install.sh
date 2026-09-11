#!/usr/bin/env bash
# Install / update kacheld on a Raspberry Pi OS box. Run as root on the Pi
# after copying the repo (or at least deploy/ + dist/kacheld + web/) across.
#
#   sudo deploy/install.sh
#
# Idempotent: safe to re-run to push a new binary or site build.
set -euo pipefail

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BIN_SRC="${REPO_DIR}/dist/kacheld"
WEB_SRC="${REPO_DIR}/web"

BIN_DST="/usr/local/bin/kacheld"
WEB_DST="/var/www/kachel.solar"
DATA_DST="/var/lib/kachel-solar"
SVC_DST="/etc/systemd/system/kachel-solar.service"

if [[ $EUID -ne 0 ]]; then
  echo "run as root (sudo $0)" >&2
  exit 1
fi

if [[ ! -x "${BIN_SRC}" ]]; then
  echo "missing ${BIN_SRC} - build it first: deploy/build.sh" >&2
  exit 1
fi

echo "==> enabling I2C"
if command -v raspi-config >/dev/null 2>&1; then
  raspi-config nonint do_i2c 0 || echo "   (raspi-config do_i2c returned non-zero, continuing)"
else
  echo "   raspi-config not found; ensure 'dtparam=i2c_arm=on' is in /boot/firmware/config.txt"
fi

echo "==> creating service user 'kachel'"
if ! id kachel >/dev/null 2>&1; then
  useradd --system --no-create-home --shell /usr/sbin/nologin kachel
fi
# Grant I2C access.
if getent group i2c >/dev/null 2>&1; then
  usermod -aG i2c kachel
else
  echo "   group 'i2c' missing; udev rules from raspi-config normally create it"
fi

echo "==> installing binary -> ${BIN_DST}"
install -m 0755 "${BIN_SRC}" "${BIN_DST}"

echo "==> installing site -> ${WEB_DST}"
mkdir -p "${WEB_DST}"
if [[ -d "${WEB_SRC}" ]] && [[ -n "$(ls -A "${WEB_SRC}" 2>/dev/null)" ]]; then
  rsync -a --delete "${WEB_SRC}/" "${WEB_DST}/"
else
  echo "   ${WEB_SRC} empty - leaving ${WEB_DST} as-is (server shows a placeholder until you deploy the site)"
fi

echo "==> data dir -> ${DATA_DST}"
mkdir -p "${DATA_DST}"
chown -R kachel:kachel "${DATA_DST}"

echo "==> installing systemd unit"
install -m 0644 "${REPO_DIR}/deploy/kachel-solar.service" "${SVC_DST}"
systemctl daemon-reload
systemctl enable --now kachel-solar.service

echo
echo "done. check status with:"
echo "  systemctl status kachel-solar.service"
echo "  curl -s localhost:8080/api/health"
echo
echo "cloudflared is separate - see deploy/cloudflared/config.yml and deploy/cloudflared.service"
