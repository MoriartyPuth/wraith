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
| Firmware Wi-Fi / beacon / mgmt | IMPLEMENTED — TESTED (Phase 2) |
| Firmware LCD / host link | IMPLEMENTED — TESTED on hardware (Phase 5) |
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
| LCD (16x2 I2C) | IMPLEMENTED — TESTED on hardware in Phase 5 (PCF8574 @ 0x27). See docs/hardware.md. |
| Probe responses (reveal hidden SSIDs) | NOT IMPLEMENTED |
| Host→device commands (ping, channel config) | NOT IMPLEMENTED |

Open item: validate the deauth/disassoc path on hardware with an owned
client (e.g. disconnect a phone from your own 2.4 GHz network).
**Done in Phase 5** (2026-11-11): a phone leaving the owned "Bo" network
produced a `wifi.disassoc_observed` (reason 8, unprotected).

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

- [~] Soak/load: ~2 h live clean + accelerated 40k-event load (no leak); found+fixed O(n^2) prune (idx_obs_event_row). Full-day wall-clock soak optional.
- [~] False-positive review on a day of real traffic (soak running; rogue-AP warm-up added to cut startup FPs)
- [x] Data retention / pruning policy (events + cascaded observations; `-retention-days`, default 7)
- [x] Documentation review (2026-11-13: all docs cross-checked against code; fixed
  stale protocol/TX/client hardware-validation status, WS `hello` protocol
  version, and the version-rejection message)

## Phase 7 — Deauth test transmission (2026-11-11)

Authorized-testing-only active feature, fully gated. See docs/tx.md.

| Item | Status |
|------|--------|
| Protocol v2 (`tx.ack`, `tx.report`) — schema, fixtures, Go validator | IMPLEMENTED — TESTED (host) |
| Firmware TX engine: frame builder, command parser, limits, burst state machine, host-lost/radio aborts | IMPLEMENTED — TESTED (host, ASan/UBSan) |
| Firmware wiring: host_link parsing, radio ops + proc-task tick in wifi_monitor, capability `deauth-tx` | IMPLEMENTED — TESTED (host) |
| Firmware build gate `WRAITH_TX_ENABLED` (default off) + `pio -e tx` env | IMPLEMENTED |
| Core: `tx_enabled` config (`-tx`, env), `capture.Commander`, serial `SendCommand` | IMPLEMENTED — TESTED |
| Core: `POST /api/v1/tx` (confirm, limits, observed-BSSID + 2.4 GHz channel, 403/400/409/502) | IMPLEMENTED — TESTED |
| Core: `tx_enabled` in `/system/info`; protocol version 2 | IMPLEMENTED — TESTED |
| Dashboard: APDetail TX card (authorization note, confirm checkbox), Settings status, `tx.*` history filters | IMPLEMENTED — typecheck + build pass |
| Docs: docs/tx.md + scope updates | IMPLEMENTED |
| Hardware validation of `esp_wifi_80211_tx` bursts, real `tx.ack`/`tx.report` on the wire | TESTED 2026-11-12: 5-frame deauth burst to an owned client reported `sent=5/requested=5 complete`, observable ~2 s disconnect; sensor did not echo its own TX |
| Driver requirements found on hardware: unconfigured STA mode + raw-frame sanity-check override (`tx_raw_override.c`) | IMPLEMENTED — TESTED |

## Phase 8 — Client visibility (2026-11-12)

Stations (phones/laptops) seen exchanging data with each AP. See
docs/protocol.md (`wifi.client_observed`) and docs/hardware.md.

| Item | Status |
|------|--------|
| Firmware: data-frame capture (mgmt+data filter), header parser, client tracker (direction, aggregation, first-sight) | IMPLEMENTED — TESTED (host, ASan/UBSan) + hardware |
| Firmware: protocol v3, `wifi.client_observed` builder, `clients=` in status stats | IMPLEMENTED — TESTED |
| Protocol v3 schema + fixtures; Core validator accepts v1–v3 | IMPLEMENTED — TESTED |
| Core: `clients` table + migration, upsert/query/prune | IMPLEMENTED — TESTED |
| Core: ingest upsert + live `client` stream event | IMPLEMENTED — TESTED |
| Core API: `GET /clients`, `GET /access-points/{bssid}/clients` | IMPLEMENTED — TESTED |
| Dashboard: Clients page, per-AP clients panel, deauth client picker, live refresh | IMPLEMENTED — typecheck + build pass |
| Hardware: confirm phones/laptops appear with real MACs on the lab AP | TESTED 2026-11-12 (data-frame headers on channel) |

## Bluetooth track (started 2026-11-13)

Single-board **switch mode** (Wi-Fi OR Bluetooth, never both), Classic + BLE
via Bluedroid, build-gated like TX. Rationale and coexistence limits:
docs/bluetooth/architecture.md.

| Phase | Item | Status |
|-------|------|--------|
| BT-1 | Protocol v4: `bluetooth.*` types, Go validator, v4 JSON schema, fixtures, generic ingest | IMPLEMENTED — TESTED (no hardware involved) |
| BT-2 | Core: Bluetooth device inventory, migration `0005_bluetooth`, processor | IMPLEMENTED — TESTED (no hardware involved) |
| BT-3 | REST `/api/v1/bluetooth/*` + WebSocket kinds, throttled | IMPLEMENTED — TESTED (no hardware involved) |
| BT-4 | Dashboard Bluetooth section | IMPLEMENTED — TESTED (unit + headless-browser render; no hardware involved) |
| BT-5 | Firmware: BT build env, Bluedroid init, custom partition, radio-mode switch (+ Core `POST /bluetooth/mode`, dashboard control) | IMPLEMENTED — NOT HARDWARE VALIDATED (builds; host-tested; Wi-Fi builds proven byte-identical) |
| BT-6 | Firmware: BLE passive scan + AD parser (host-tested) | NOT IMPLEMENTED |
| BT-7 | Firmware: bounded Classic inquiry + CoD/EIR parser | NOT IMPLEMENTED |
| BT-8 | Firmware: device trackers + v4 event emission | NOT IMPLEMENTED |
| BT-9 | Optional operator-initiated GATT discovery (off by default) | NOT IMPLEMENTED |
| BT-10 | Bluetooth detection engine | NOT IMPLEMENTED |
| BT-11 | LCD Bluetooth screens via the existing LCD task | NOT IMPLEMENTED |
| BT-12 | Hardware validation + Wi-Fi regression (mode switch both ways, heap, gaps) | BLOCKED — HARDWARE REQUIRED |
