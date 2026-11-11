# WRAITH — Wireless Security Research Toolkit

*Observe. Analyze. Understand.*

A local-first platform for passive Wi-Fi observation and defensive monitoring:

- **WRAITH Nano** — ESP32 sensor (C, ESP-IDF via PlatformIO) streaming
  newline-delimited JSON events over USB serial.
- **WRAITH Core** — Go service on Fedora: ingestion, SQLite, detection, REST + WebSocket.
- **WRAITH Web** — React/TypeScript dashboard.

> **Status: Phase 5 done** (runs as a systemd user service; LCD active). WRAITH Nano passively observes 2.4 GHz beacons
> and deauth/disassoc frames; WRAITH Core ingests them over a checksummed USB
> serial link, keeps an AP inventory with RSSI history, runs explainable
> detectors and streams live updates; WRAITH Web shows all of it live, with
> evidence drill-down, finding review and JSON/CSV export. Capture hardware is
> the ESP32 only. See [docs/roadmap.md](docs/roadmap.md).
>
> **Client visibility (protocol v3):** Nano also captures 802.11 data-frame
> headers (never payloads) to list stations seen talking to each AP; the
> dashboard has a Clients page and each AP page lists its observed clients.
>
> **Optional active feature:** a heavily gated deauth *test* transmission
> path exists (firmware build flag + Core `-tx` + explicit confirm; see
> [docs/tx.md](docs/tx.md)). It is off by default everywhere and has been
> validated on hardware (2026-11-12) with an owned client.

## Layout

```
firmware/   PlatformIO ESP-IDF project (WRAITH Nano)
core/       Go module (WRAITH Core)
dashboard/  Vite + React + Tailwind (WRAITH Web)
protocol/   JSON Schema + shared valid/invalid fixtures
docs/       architecture, hardware, protocol, detection, testing, installation, tx, roadmap
scripts/    check-all.sh, firmware-host-tests.sh, check-protocol.sh
```

## Requirements

Hardware: ESP32 dev board (original ESP32: 2.4 GHz only), optional 16×2 I2C LCD
(PCF8574 backpack, auto-detected; see the 5 V note in [docs/hardware.md](docs/hardware.md)), Linux host.
Software: Go ≥ 1.26, Node ≥ 22, PlatformIO Core. SQLite is bundled (pure-Go
driver). Python is not a runtime dependency.

## Build & test

```bash
scripts/check-all.sh                 # everything that runs without hardware
scripts/install-user-service.sh      # run Core as a service: http://127.0.0.1:8088
cd core && go run ./cmd/wraith -serial /dev/ttyUSB0  # or run it by hand
cd dashboard && npm install && npm run dev
cd firmware && pio run               # requires PlatformIO
```

Details: [docs/installation.md](docs/installation.md), [docs/testing.md](docs/testing.md).

## Limitations

- ESP32: 2.4 GHz only, one channel at a time, best-effort capture, no decryption.
  It is a sensor, not a professional capture adapter.
- PMF information is what APs *advertise*, not what clients negotiate.
- Channel views show observed Wi-Fi activity, not RF spectrum.
- The ESP32 is the only capture source (no host monitor-mode capture).
- This board's USB-serial adapter replays packets after idle periods; WRAITH
  detects and discards corrupted lines (CRC-32) — see docs/hardware.md.

## Scope

Defensive and research use on networks you own or are authorized to assess.
WRAITH **observes**: AP discovery, beacons, management frames, deauth/disassoc
detection, PMF inspection, **client/station sightings from data-frame headers**
(no payload decryption), rogue-AP indicators, evidence export. By default it
transmits nothing. There is one exception, strictly for authorized testing:
an opt-in, triple-gated deauth *test* burst (≤ 10 frames, ≥ 100 ms apart,
30 s cooldown, no broadcast clients) described in [docs/tx.md](docs/tx.md) —
compiled out of the firmware unless `WRAITH_TX_ENABLED=1`, hidden unless Core
runs with `-tx`, and never accepted without an explicit `confirm` flag. WRAITH
does not run evil twins or captive portals and does not recover credentials.
Findings state their evidence, confidence and limitations; a deauth frame
alone is never called an attack.

## License

MIT — see [LICENSE](LICENSE).
