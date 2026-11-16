# WRAITH — Wireless Security Research Toolkit

*Observe. Analyze. Understand.*

A local-first platform for passive Wi-Fi and Bluetooth observation and
defensive monitoring:

- **WRAITH Nano**: an ESP32 sensor (C, ESP-IDF via PlatformIO) that streams
  checksummed, newline-delimited JSON events over USB serial, with an optional
  16×2 LCD.
- **WRAITH Core**: a Go service (Linux) for ingestion, SQLite storage,
  explainable detection, REST and WebSocket APIs, and Bluetooth scheduling. It
  also serves the dashboard.
- **WRAITH Web**: a React/TypeScript dashboard, live over WebSocket.

> **Status (2026-11-17):** Wi-Fi phases 0–8 and the Bluetooth track (BT-1 to
> BT-8, BT-10 to BT-12) are implemented and **validated on hardware**. Core runs
> as a systemd user service. Still open: the optional BT-9 (GATT discovery,
> needs a BLE device you own), plus the optional full-day soak and the
> false-positive review on a day of real traffic (Phase 6). Full checklist:
> [docs/roadmap.md](docs/roadmap.md).

## What it does

**Wi-Fi (2.4 GHz, passive by default)**
- Access-point inventory: SSID/BSSID, channel, RSSI history, advertised
  security (RSN) and PMF.
- Management-frame monitoring: beacons, deauthentication/disassociation, PMF.
- **Connected devices (clients):** stations seen exchanging data with an AP,
  from 802.11 data-frame headers only (payloads are never decoded). Each
  shows its network, signal and activity, plus:
  - the **maker**, from the address's IEEE prefix (embedded table, no
    network lookups);
  - a **device-type guess** ("camera", "laptop or PC");
  - a **private-address** flag for randomised MACs;
  - an optional **name** you give it ("my iPhone").

  WRAITH cannot see device names such as "iPhone" or "iPad": they travel inside
  encrypted traffic. See [docs/clients.md](docs/clients.md).
- Detectors: deauth activity, security/PMF downgrade, AP identity change,
  rogue-AP indicator, invalid PMF advertisement
  ([docs/detection.md](docs/detection.md)).
- Optional, heavily gated deauth **test** transmission for networks you own
  ([docs/tx.md](docs/tx.md)).

**Bluetooth (switch mode: the ESP32's one radio runs Wi-Fi *or* Bluetooth)**
- Passive BLE scanning: advertisements, names, manufacturer data, services and
  classification.
- Optional bounded Classic inquiry: device class and EIR names. Inquiry
  transmits, so it is a separate opt-in.
- Detectors: BLE advertising flood and pairing pop-up spam, Find My tracker
  separated from its owner, identity change.
- Manual sessions from the dashboard, or **scheduled sessions** run by Core.
  The sensor always returns to Wi-Fi: on schedule end, after 15 min, or if the
  host disappears. See [docs/bluetooth/](docs/bluetooth/architecture.md).

**Dashboard** (`http://127.0.0.1:8088`)

| Section | Pages |
|---------|-------|
| Overview | system-wide counts, radio coverage timeline (Wi-Fi / Bluetooth / no data), latest findings |
| Wi-Fi | Wi-Fi overview (incl. *Devices on networks*), Wireless Monitor, Access Points (with client counts), AP detail, Clients, Security Events, PMF Inspector, Channel Analysis |
| Bluetooth | Overview (mode control, schedule, *Near now*, breakdowns), Classic, BLE, device detail, Events |
| System | Detection Center (finding review, evidence, export), Sensors, Event History (JSON/CSV export), Settings |

The header search finds access points, clients (by address or your name for
them) and Bluetooth devices. A labelled demo mode works without hardware.

## Layout

```
firmware/   PlatformIO ESP-IDF project (WRAITH Nano): envs wraith-nano, tx, bt, native
core/       Go module (WRAITH Core): cmd/wraith, internal/*, migrations/
dashboard/  Vite + React + Tailwind (WRAITH Web)
protocol/   JSON Schemas (event v1–v4) + shared valid/invalid fixtures
docs/       architecture, hardware, protocol, detection, clients, testing,
            installation, tx, roadmap; bluetooth/ (architecture, event
            protocol, API, dashboard, schedule, testing)
scripts/    check-all.sh, install-user-service.sh, esp32-switch.sh,
            firmware-host-tests.sh, check-protocol.sh, gen-oui.sh
```

## Requirements

Hardware: an original ESP32 dev board (2.4 GHz only; this project uses an
ESP32-D0WD-V3 with a CP2102), optionally a 16×2 I2C LCD (PCF8574 backpack,
auto-detected; see the 5 V note in [docs/hardware.md](docs/hardware.md)), and
a Linux host. Core's serial and system code is Linux-only today.

Software: Go ≥ 1.26, Node ≥ 22, PlatformIO Core. SQLite is bundled (pure-Go
driver). Python is not a runtime dependency.

## Build & run

```bash
scripts/check-all.sh                 # everything that runs without hardware

# Firmware: pick one build (stop the service before flashing)
systemctl --user stop wraith-core
cd firmware && pio run -e wraith-nano -t upload   # passive Wi-Fi (default)
#              pio run -e bt -t upload            # Wi-Fi + Bluetooth switch mode
#              pio run -e tx -t upload            # Wi-Fi + gated deauth test TX

# Core + dashboard as a systemd user service: http://127.0.0.1:8088
scripts/install-user-service.sh                    # add WRAITH_TX_ENABLED=1 to keep -tx

# Development
cd core && go run ./cmd/wraith -serial /dev/ttyUSB0
cd dashboard && npm install && npm run dev        # http://127.0.0.1:5173
```

Details: [docs/installation.md](docs/installation.md),
[docs/testing.md](docs/testing.md). Re-run the installer after every Core or
dashboard change; it rebuilds both and restarts the service.

## Limitations

- ESP32: 2.4 GHz only, one channel at a time, best-effort capture, no
  decryption. It is a sensor, not a professional capture adapter.
- One radio: Wi-Fi monitoring pauses while Bluetooth runs (shown on the
  coverage timeline).
- Clients are "seen exchanging data", not a guaranteed association list. Idle
  devices are invisible, and private (randomised) addresses carry no maker.
- PMF information is what APs *advertise*, not what clients negotiate.
- Channel views show observed Wi-Fi activity, not RF spectrum.
- The ESP32 is the only capture source (no host monitor-mode capture).
- This board's USB-serial adapter replays packets after idle periods. WRAITH
  detects and discards corrupted lines (CRC-32) and counts losses; see
  docs/hardware.md.

## Scope

Defensive and research use on networks and devices you own or are authorized
to assess. WRAITH **observes**:

- AP discovery, beacons and management frames;
- deauth/disassoc detection and PMF inspection;
- client sightings from data-frame headers (no payload decryption), with
  address-derived maker information;
- passive BLE advertisements;
- rogue-AP indicators and evidence export.

By default the Wi-Fi radio transmits nothing. Two features do transmit, and
both are opt-in:

- **Classic Bluetooth inquiry** sends standard inquiry packets, as any phone
  does when you open its Bluetooth settings. It runs only when the operator
  chooses Classic for a session; BLE scanning is passive.
- **Deauth test burst**, strictly for authorized testing. It is limited to
  ≤ 10 frames, ≥ 100 ms apart, with a 30 s cooldown and no broadcast clients
  ([docs/tx.md](docs/tx.md)). It is triple-gated: compiled out of the firmware
  unless `WRAITH_TX_ENABLED=1`, hidden unless Core runs with `-tx`, and never
  accepted without an explicit `confirm` flag. It has no Bluetooth equivalent.

WRAITH does not run evil twins or captive portals, does not connect to or pair
with Bluetooth devices, and does not recover credentials. Findings state their
evidence, confidence and limitations; a deauth frame alone is never called an
attack.

## License

MIT; see [LICENSE](LICENSE).
