# Installation (Fedora)

## Already present on this machine

Go 1.26.10, Node 22.23.1 / npm 10.9.8, SQLite 3.51.2, gcc 16.2.1, clang (ASan for firmware host tests).

## Installed for WRAITH / optional

| Tool | Why | Suggested |
|------|-----|-----------|
| PlatformIO Core | firmware build/flash/monitor | done: `pipx install platformio` (6.2.0) |
| `dialout` group | access to `/dev/ttyUSB*` | done; until next login use `sg dialout -c "..."` |
| libasan, libubsan (optional) | sanitizers in firmware host tests | `sudo dnf install libasan libubsan` |

## Run

```bash
# Core (default http://127.0.0.1:8088, DB in ~/.local/share/wraith/)
cd core && go run ./cmd/wraith -serial /dev/ttyUSB0
#   (needs the dialout group; until you log in again: sg dialout -c "...")
#   watch the live stream:  go run ./cmd/wraith tail
#   flags: -serial /dev/ttyUSB0  -db PATH  -listen ADDR  -dashboard-dir DIR
#   env:   WRAITH_SERIAL, WRAITH_DB, WRAITH_LISTEN, WRAITH_DASHBOARD_DIR, ...
# Dashboard (dev, proxies /api to Core)
cd dashboard && npm install && npm run dev     # http://127.0.0.1:5173

# Or serve the built dashboard from Core
cd dashboard && npm run build
cd ../core && go run ./cmd/wraith -dashboard-dir ../dashboard/dist

# Firmware (once PlatformIO is installed and the board identified)
cd firmware && pio run && pio run -t upload && pio device monitor
```

## Run as a service (systemd user unit)

```bash
scripts/install-user-service.sh            # build, install, enable, start
WRAITH_TX_ENABLED=1 scripts/install-user-service.sh   # same, with -tx (docs/tx.md)
scripts/install-user-service.sh --uninstall
```

Installs `~/.local/bin/wraith`, the dashboard in
`~/.local/share/wraith/dashboard`, data in `~/.local/share/wraith/wraith.db`,
and `~/.config/systemd/user/wraith-core.service`. The serial device is the
stable `/dev/serial/by-id/…` link (no udev rule needed). If your login session
predates joining `dialout`, the unit starts Core via `sg dialout`; re-run the
installer after your next login to switch to a direct start with
`NoNewPrivileges=yes`. The service runs while you are logged in.

## Data retention

Core prunes events (and the observations linked to them) older than
`event_retention_days` (default **7**). Findings and the access-point
inventory are never auto-pruned. Pruning runs every `prune_interval_minutes`
(default 60) in batches, so it never holds a long write lock.

```bash
wraith -retention-days 14          # keep two weeks
wraith -retention-days 0           # keep everything (no pruning)
WRAITH_RETENTION_DAYS=3 wraith     # same via environment
```

In a JSON config file: `"event_retention_days": 7, "prune_interval_minutes": 60`.
Pruned counts appear in `/api/v1/statistics` under `runtime`
(`pruned_events`, `pruned_observations`).

## Deauth test transmission (optional)

Off by default. Enabling it exposes `POST /api/v1/tx`, which asks a sensor
built with `WRAITH_TX_ENABLED=1` to send a short, rate-limited deauth/disassoc
burst (authorized networks only — see [docs/tx.md](tx.md)).

```bash
wraith -tx                         # expose the API
WRAITH_TX_ENABLED=1 wraith         # same via environment
```

In a JSON config file: `"tx_enabled": true`. Both ends must opt in: the
firmware is built with `cd firmware && pio run -e tx -t upload`, and Core runs
with `-tx`. `/api/v1/system/info` reports `"tx_enabled"`, and the dashboard
shows the test card on an AP page only when both are true and a sensor is
connected. The feature is **hardware-validated** (2026-11-12; see docs/tx.md,
docs/hardware.md).

`scripts/esp32-switch.sh` stops the service while it uses the port and
restarts it afterwards; flash with `pio run -t upload` only after
`systemctl --user stop wraith-core`.
