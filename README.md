# kachel.solar — server

The Pi-side program behind [kachel.solar](https://kachel.solar): one small Go
binary that serves the static website **and** a live battery/power API, reading
an Adafruit INA219 over I2C. No framework, no database, no background daemons.

This repo is the *non-website* half of the project. The site's HTML/CSS/JS
(spec in `kachel-solar-design-spec-v3.md`) is dropped into `web/` and served
as-is.

## What it does

- Serves `web/` as a static site with clean URLs and `Cache-Control` headers,
  plus a custom `404.html`.
- Polls the INA219 on a fixed interval in the background and caches the latest
  reading. HTTP handlers never touch I2C, so a wedged sensor can't stall a
  request.
- Logs every sample to an append-only per-day JSON-lines file, rolls finished
  days up into small summaries, and prunes the raw logs past a retention
  window.
- Degrades instead of erroring: if the sensor stops responding, the last good
  reading keeps being served with `"stale": true`. If the battery genuinely
  dies, the whole box goes down — that's the accepted trade-off from the
  project brief, and the 404/backup story lives on `kachel.work`.

## Layout

```
cmd/kacheld/        entrypoint: flags, wiring, graceful shutdown
internal/sensor/    INA219 driver (raw Linux i2c-dev, no cgo) + a simulator
internal/battery/   voltage -> percent, charge-state logic, background sampler
internal/stats/     the reading log, daily rollups, /api/sensor aggregation
internal/server/    net/http surface: JSON API + static file serving
deploy/             build script, systemd units, cloudflared config, installer
web/                the static site (served verbatim)
```

## API

### `GET /api/battery`

Latest reading. `Cache-Control: no-store`. `503` until the first successful
sample.

```json
{
  "percent": 72.4,
  "charging": true,
  "charge_state": "charging",
  "voltage": 3.95,
  "current_ma": 240.5,
  "power_mw": 950.0,
  "updated_at": "2026-09-10T14:03:12Z",
  "stale": false
}
```

`charge_state` is one of `charging`, `discharging`, `full`, `idle`. `stale`
goes `true` when the newest reading is older than `-stale-after`.

### `GET /api/sensor`

Aggregated history for the sensor-stats page. Cached server-side for
`-stats-ttl` (default 60s) and sent with a matching `max-age`.

```json
{
  "day":  { "energy_generated_wh": 3.2, "energy_consumed_wh": 1.1,
            "avg_percent": 78.4, "charging_seconds": 18240,
            "charging_fraction": 0.211, "coverage_seconds": 86400, "samples": 8640 },
  "week": { "...": "same shape, trailing 7 days" },
  "all":  { "...": "same shape, whole history" },
  "since": "2026-09-01T09:12:04Z",
  "generated_at": "2026-09-10T14:03:20Z"
}
```

Plain-language labels for the page: `energy_generated_wh` → "Energy from the
sun", `avg_percent` → "Average charge", `charging_seconds` → "Time spent
charging".

### `GET /api/health`

`{ "status": "ok", "version": "...", "have_reading": true, "time": "..." }` —
for uptime checks.

## How the numbers are derived

- **Percent** is a piecewise-linear map from resting LiPo cell voltage
  (~3.27 V empty → 4.20 V full). While charging, the measured terminal voltage
  is pulled back toward rest using the shunt current and a ~0.15 Ω effective
  series resistance before the lookup.
- **Charging** is decided from the sign and magnitude of the INA219 shunt
  current (`> -charge-threshold-ma` etc.). Wired on the battery line, a
  positive current means charge flowing in. If your INA219 V+/V- ended up
  reversed, run with `-invert-current`.
- **Energy** is a trapezoidal integral of power over time. Gaps between
  samples are capped at 5 minutes so an overnight outage doesn't distort the
  totals. Positive power counts as generated, negative as consumed.
- **Average percent** is time-weighted, not a plain mean of samples.

## Sensor wiring assumption

Adafruit INA219 breakout (PID 904), **0.1 Ω shunt**, on the battery line
between the 3000 mAh cell and the JST split that feeds the bq24074 charger and
the PowerBoost. The driver uses the standard "32 V / 2 A" calibration, which is
wide enough for this pack (never past ~4.3 V or ~1 A either way). A different
shunt value would need a different calibration constant in
`internal/sensor/ina219.go`.

## Build & deploy

The target is a **Raspberry Pi Zero (original)** — ARMv6, 32-bit. Build on any
machine with Go 1.22+, then copy the binary over.

```bash
deploy/build.sh                 # -> dist/kacheld (static, ~a few MB, GOARM=6)
scp dist/kacheld pi@raspberrypi:~/   # plus the repo / deploy dir / web build
```

On the Pi:

```bash
sudo deploy/install.sh          # enables I2C, makes the 'kachel' user,
                                # installs the binary + site + systemd unit
systemctl status kachel-solar.service
curl -s localhost:8080/api/health
```

Then the network path (separate from this server):

```bash
cloudflared tunnel login
cloudflared tunnel create kachel-solar
# fill <TUNNEL_ID> into deploy/cloudflared/config.yml, put the creds JSON at
# /etc/cloudflared/<TUNNEL_ID>.json
cloudflared tunnel route dns kachel-solar kachel.solar
sudo install -m0644 deploy/cloudflared.service /etc/systemd/system/
sudo systemctl enable --now cloudflared.service
```

`cloudflared` is only the tunnel/DNS. It is **not** a CDN and does not host the
static files — those come off the Pi too, on purpose.

## Local development

No Pi, no sensor:

```bash
go run ./cmd/kacheld -mock -web ./web -data ./data
# http://127.0.0.1:8080  — the mock runs a day/night solar charge cycle
```

## Data files (`-data`, default `./data`)

| File | What |
|---|---|
| `raw-YYYY-MM-DD.jsonl` | one JSON object per sample; current day is live |
| `daily-YYYY-MM-DD.json` | rollup of a finished day; kept forever (tiny) |

Raw files are pruned once older than `-raw-retention` (default 14 days); the
rollups keep the all-time stats accurate after that.

## Config flags

| Flag | Default | |
|---|---|---|
| `-addr` | `127.0.0.1:8080` | listen address |
| `-web` | `./web` | static site directory |
| `-data` | `./data` | reading log + rollups |
| `-i2c-bus` | `/dev/i2c-1` | INA219 bus device |
| `-i2c-addr` | `0x40` | INA219 address (accepts `0x`) |
| `-sample-interval` | `10s` | sensor poll period |
| `-stale-after` | `45s` | mark readings stale after this |
| `-charge-threshold-ma` | `15` | charging/discharging deadband |
| `-invert-current` | `false` | flip current sign for reversed wiring |
| `-mock` | `false` | simulated sensor |
| `-raw-retention` | `336h` | keep raw sample logs this long |
| `-stats-ttl` | `60s` | `/api/sensor` server-side cache |
| `-maintain-interval` | `1h` | rollup/prune cadence |

`-addr`, `-web`, `-data` also read `KACHEL_ADDR` / `KACHEL_WEB` / `KACHEL_DATA`.

## Tests

```bash
go test ./...
```

Covers the INA219 register math (against a fake bus), the SoC curve and
charge-state logic, the sampler's cache/stale behaviour, stats integration and
rollup/prune, and the HTTP handlers (including the `/api/sensor` cache and
static clean-URL / 404 handling).
