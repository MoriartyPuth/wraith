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
scripts/install-user-service.sh --uninstall
journalctl --user -u wraith-core -f        # logs (JSON)
```

Installs `~/.local/bin/wraith`, the dashboard in
`~/.local/share/wraith/dashboard`, data in `~/.local/share/wraith/wraith.db`,
and `~/.config/systemd/user/wraith-core.service`. The serial device is the
stable `/dev/serial/by-id/…` link (no udev rule needed). If your login session
predates joining `dialout`, the unit starts Core via `sg dialout`; re-run the
installer after your next login to switch to a direct start with
`NoNewPrivileges=yes`. The service runs while you are logged in.

`scripts/esp32-switch.sh` stops the service while it uses the port and
restarts it afterwards; flash with `pio run -t upload` only after
`systemctl --user stop wraith-core`.
