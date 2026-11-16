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

# Firmware (see "Firmware builds" below; stop the service first)
cd firmware && pio run -e wraith-nano -t upload && pio device monitor -b 115200
```

## Firmware builds

| PlatformIO env | What it is | Protocol |
|----------------|------------|----------|
| `wraith-nano` (default) | Passive Wi-Fi monitor: APs, management frames, client sightings, LCD | v3 |
| `bt` | Same Wi-Fi monitor + Bluetooth **switch mode** (BLE passive scan, opt-in Classic inquiry); 3 MB app partition; no deauth TX | v4 |
| `tx` | Wi-Fi monitor + gated deauth test transmission (docs/tx.md) | v3 (incl. `tx.ack` / `tx.report`) |
| `native` | Host unit tests only (`scripts/firmware-host-tests.sh`) | — |

Flash with `systemctl --user stop wraith-core` first (the service holds the
port), then `pio run -e <env> -t upload`, then `systemctl --user start
wraith-core`. Use 115200 baud. Always update Core **before** flashing a build
with a newer protocol: an older Core rejects v4 events.
`scripts/esp32-switch.sh` backs up and restores whole-flash images (it stops
and restarts the service on its own).

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

## Upgrading Core

Re-run `scripts/install-user-service.sh` (with `WRAITH_TX_ENABLED=1` if you use
`-tx`; the installer drops the flag otherwise) after pulling changes. Database
migrations apply automatically at start-up and are additive. To keep a
restorable copy first:

```bash
sqlite3 ~/.local/share/wraith/wraith.db ".backup $HOME/.local/share/wraith/wraith.db.bak"
```

(Use `.backup`, not `cp`, because the database runs in WAL mode.)

## Bluetooth (bt build)

Switch the radio from the dashboard (Bluetooth → Overview) or the API:

```bash
curl -X POST -H 'Content-Type: application/json' -d '{"mode":"bluetooth"}' \
  http://127.0.0.1:8088/api/v1/bluetooth/mode            # BLE only (passive)
#  {"mode":"bluetooth","classic":true}  adds Classic inquiry (transmits)
#  {"mode":"wifi"}                      back to Wi-Fi monitoring
```

Wi-Fi monitoring pauses during a Bluetooth session. The sensor returns to
Wi-Fi by itself after 15 minutes or 6 s after Core stops talking to it.
Scheduled sessions (e.g. 2 minutes every hour) are set under Bluetooth →
Overview → Scheduled sessions; see docs/bluetooth/schedule.md.

## Client maker database

The maker shown for Wi-Fi clients comes from an IEEE MA-L table embedded in
Core (`core/internal/devid/oui.tsv.gz`). To refresh it from Fedora's `hwdata`
(or an IEEE `oui.txt` passed as an argument), then rebuild and reinstall Core:

```bash
scripts/gen-oui.sh [path/to/oui.txt]
```

Names you give devices are stored in the `device_labels` table and are never
pruned. See docs/clients.md.

## Data retention

Core prunes events (and the observations linked to them) older than
`event_retention_days` (default **7**). Wi-Fi client sightings and Bluetooth
device rows not seen within that window are removed too. Findings and the
access-point inventory and device names are never auto-pruned. Pruning runs every `prune_interval_minutes`
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
