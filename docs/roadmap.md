# Roadmap & checklist

Labels: `NOT IMPLEMENTED` · `IMPLEMENTED — NOT HARDWARE VALIDATED` ·
`IMPLEMENTED — TESTED` · `BLOCKED — HARDWARE REQUIRED`

## Phase 0 — Discovery ✅ (2026-11-11)

- [x] Fedora 44 / kernel 7.2.11 inspected
- [x] Wi-Fi: MT7922 / mt7921e; monitor supported, **not concurrent** with managed
- [x] Tools: go, node, npm, tshark, sqlite3 present; **pio, kismet absent**
- [x] ESP32 board: ESP32-D0WD-V3 rev 3.1, 4 MB, CP2102 → `esp32dev`
- [x] ESP-IDF 6.1.0 (espressif32 8.0.0, PlatformIO 6.2.0)
- [x] Previous project backed up (verified full-flash image); `scripts/esp32-switch.sh`
- [ ] LCD electrical requirements — `BLOCKED — HARDWARE REQUIRED`

## Phase 1 — Foundation ✅ (awaiting approval)

| Item | Status |
|------|--------|
| Monorepo, README, docs | IMPLEMENTED |
| Protocol v1 (doc, JSON Schema, fixtures) | IMPLEMENTED — TESTED |
| Go validator / framing / dedup | IMPLEMENTED — TESTED |
| Go config, SQLite + migrations | IMPLEMENTED — TESTED |
| REST API (all listed GET endpoints) | IMPLEMENTED — TESTED (empty data until ingestion) |
| Capture provider interface + availability probes | IMPLEMENTED — TESTED (stubs; no capture) |
| Wireless interface discovery | IMPLEMENTED — TESTED (on this host) |
| Firmware JSON writer, protocol builders, 802.11 header parser | IMPLEMENTED — TESTED (host) |
| Firmware transport, status/heartbeat, main | IMPLEMENTED — TESTED on hardware (nano-70b2a8) |
| Firmware Wi-Fi / beacon / mgmt / detection / LCD | NOT IMPLEMENTED (APIs defined) |
| Dashboard shell, 10 routes, API client, WS backoff hook | IMPLEMENTED — typecheck + build pass |
| WebSocket endpoint | NOT IMPLEMENTED (returns 501) |

## Phase 2 — WRAITH Nano ✅ (2026-11-11, awaiting approval for Phase 3)

| Item | Status |
|------|--------|
| PlatformIO + first build, board identified (`esp32dev`) | IMPLEMENTED — TESTED |
| Flash; boot status + 5 s heartbeats validated by Core | IMPLEMENTED — TESTED on hardware |
| Promiscuous mode in `WIFI_MODE_NULL` (never transmits), mgmt filter, ring buffer → processing task | IMPLEMENTED — TESTED on hardware |
| Channel hopping 1–13, 250 ms dwell, rejected channels dropped | IMPLEMENTED — TESTED on hardware |
| Beacon parser: SSID, DS channel, RSN (ciphers/AKM/MFPC/MFPR), WPA vendor IE, auth mode | IMPLEMENTED — TESTED (host, ASan, fuzz, mutation) + hardware |
| AP tracker: `ap_observed` / `pmf_observed` on first sight and identity/security change; 10 s aggregated `beacon_observed` | IMPLEMENTED — TESTED (host) + hardware |
| Deauth/disassoc aggregation per BSSID/window with reason codes, protected bit | IMPLEMENTED — TESTED (host); **not yet seen on hardware** (no such frames observed) |
| Capture statistics in `sensor.status` message | IMPLEMENTED — TESTED on hardware |
| Interrupt-driven console UART | IMPLEMENTED — TESTED on hardware |
| On-device detection (`detection.c`) | Deferred: authoritative detection is in Core (Phase 3) |
| LCD | Dropped from scope for now (user decision); stays disabled |
| Probe responses (reveal hidden SSIDs) | NOT IMPLEMENTED |
| Host→device commands (ping, channel config) | NOT IMPLEMENTED |

Open item: validate the deauth/disassoc path on hardware with an owned
client (e.g. disconnect a phone from your own 2.4 GHz network).

## Phase 3 — WRAITH Core ✅ (2026-11-11, awaiting approval for Phase 4)

| Item | Status |
|------|--------|
| ESP32 serial provider: raw termios, stale-input flush, hot-plug reconnect with backoff | IMPLEMENTED — TESTED (pty unplug/replug) + hardware |
| Ingestion: validate → dedup (LRU + DB unique) → persist; AP inventory, RSSI history | IMPLEMENTED — TESTED + hardware (123 events, 0 dup/gap/failure) |
| Sensor registry: heartbeat, status, boot id, `connected`, rejected/dup/gap counters | IMPLEMENTED — TESTED + hardware |
| Detection engine: deauth bursts, security/PMF downgrade, SSID change, rogue-AP indicator, invalid PMF | IMPLEMENTED — TESTED (unit + integration); not triggered on hardware |
| WebSocket hub: Origin check, bounded queues, ping/pong, write deadlines, shutdown | IMPLEMENTED — TESTED (-race) + hardware |
| `/events` filters (type, bssid, sensor, since, until) | IMPLEMENTED — TESTED |
| `GET /access-points/{bssid}/observations` | IMPLEMENTED — TESTED |
| `PATCH /detections/{id}` (open/reviewed/dismissed; JSON-only as CSRF guard) | IMPLEMENTED — TESTED |
| Graceful shutdown order | IMPLEMENTED — TESTED on hardware |
| Firmware: AP identity refresh every 60 s (late-attach fix found in hardware test) | IMPLEMENTED — TESTED (host + hardware) |
| `wraith tail` live-stream CLI | IMPLEMENTED |
| TShark / Kismet providers | DROPPED (2026-11-11, user decision: only the ESP32 is available as capture hardware) |
| Signal/channel anomaly detectors | NOT IMPLEMENTED |
| systemd unit | NOT IMPLEMENTED (Phase 5) |

## Phase 4 — WRAITH Web ✅ (2026-11-11, awaiting approval for Phase 5)

| Item | Status |
|------|--------|
| Shared live stream context; pages refresh on relevant messages | IMPLEMENTED — TESTED on hardware (screenshots) |
| Wireless Monitor: live AP table, filters (channel, security, recency), sorting, update highlight | IMPLEMENTED — TESTED on hardware |
| AP inventory + AP detail (config, RSN, RSSI history chart, findings, events) | IMPLEMENTED — TESTED on hardware |
| Security Events: deauth/disassoc/PMF observations, reason-code names, linked findings | IMPLEMENTED — TESTED (demo) |
| Detection Center: filters, evidence drill-down (event_id lookup), review/dismiss/reopen | IMPLEMENTED — TESTED on hardware (real finding) |
| PMF Inspector with state legend and raw MFPC/MFPR | IMPLEMENTED — TESTED |
| Channel Analysis with per-channel table | IMPLEMENTED — TESTED |
| Event History: date/type/BSSID/sensor filters, paging, JSON/CSV export (CSV formula-injection safe) | IMPLEMENTED — TESTED (unit + demo) |
| Sensors: live connected flag, serial-link counters | IMPLEMENTED — TESTED on hardware |
| Demo mode: opt-in, banner on every page, 02:DE BSSIDs, labelled exports | IMPLEMENTED — TESTED |
| Code splitting (per page, charts lazy) — initial JS 272 kB | IMPLEMENTED |
| Degraded-link banner | IMPLEMENTED — TESTED (pty) |
| Core: `event_id` filter; CRC-32 line framing; open-burst grace; link health | IMPLEMENTED — TESTED + hardware |
| Firmware: CRC-32 suffix | IMPLEMENTED — TESTED (host + hardware) |
| Detector fix: rogue indicator on "auth became known", graded severity | IMPLEMENTED — TESTED + first real finding |

## Phase 5 — Integration ✅ (2026-11-11, awaiting approval for Phase 6)

| Item | Status |
|------|--------|
| LCD driver (PCF8574/HD44780, address auto-detect, 4 screens, own task) | IMPLEMENTED — TESTED on hardware |
| Host → device PING; LCD host status; status re-announce on connect | IMPLEMENTED — TESTED on hardware |
| systemd user service + installer (by-id device, sg fallback, hardening after re-login) | IMPLEMENTED — TESTED |
| Stable device name | /dev/serial/by-id (udev default; no custom rule needed) |
| Hot-plug reconnect (backoff cap 5 s) + stall watchdog (15 s) | IMPLEMENTED — TESTED (pty + 2 hardware replugs) |
| esp32-switch.sh pauses the service | IMPLEMENTED |
| Deauth/disassoc path on hardware | TESTED (phone leaving own network) |

## Phase 6 — Validation

- [ ] Long soak (hours): memory, gaps, link health, DB growth
- [ ] False-positive review of detectors on a day of real traffic
- [ ] Data retention / pruning policy for events and observations
- [ ] Documentation review
