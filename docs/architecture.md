# Architecture

```
 WRAITH Nano (ESP32)          WRAITH Core (Go, Fedora)                 WRAITH Web
 ───────────────────          ────────────────────────                 ──────────
 promiscuous RX cb            esp32-serial provider                    React + TS
   │ (copy ≤512 B, ring buf)    │ framing, CRC-32 check                Vite, Tailwind
 processing task                ▼                                      Recharts
   │ frame/beacon parse       validate (events.Parse)
   │ AP tracker, mgmt + client tracker   dedup / seq gaps
 serializer (json + CRC-32)   persist (SQLite)
   │                          detection ─▶ findings
 transport task ── USB serial ──▶          REST /api/v1 ◀──────────── fetch / poll
 host link (PING) ◀── every 2 s ──        (stall watchdog: reopen after 15 s silence)
 LCD task (I2C 0x27, 4 screens)
   lines: <json>\t<crc32>\n            WS /api/v1/ws ─────────────▶ live updates
```

## Core data flow (Phase 3)

```
serial provider ───▶ ingest.Pipeline.Run ─▶ per event, one SQLite transaction:
 (raw termios, CRC check,  │   dedup (LRU + UNIQUE(sensor_id,event_id)), seq gaps, restarts
  hot-plug retry, link     │
  health / open grace)     │
                          │   events row · sensor state · AP upsert · RSSI observation
                          │   client sighting upsert (wifi.client_observed)
                          │   detection.Engine (sees AP before/after) · findings
                          └─▶ after commit: stream.Hub.Publish → WebSocket clients
```

Bluetooth events (protocol v4) take the same path: the transaction also
updates the Bluetooth inventory (`core/internal/bluetooth`: devices,
observations, sensor radio status) and the Bluetooth detectors. Device updates
reach the live stream throttled (`bt_device`, `bt_status`).

## Radio modes (Bluetooth build)

The ESP32 has one 2.4 GHz radio, so the `bt` firmware build runs **switch
mode**: Wi-Fi monitoring *or* Bluetooth scanning, never both
(docs/bluetooth/architecture.md).

```
 dashboard / btschedule ──▶ POST /bluetooth/mode ──▶ capture.Commander ──▶ "MODE BLUETOOTH [CLASSIC]"
                                                                            │  (host link, serial)
 radio_mode.c: stop + deinit Wi-Fi ─▶ bt_manager (Bluedroid) ─▶ ble_scanner (passive, continuous)
                                                             └▶ classic_scanner (6 s inquiry slots, opt-in)
 back to Wi-Fi on "MODE WIFI", after 15 min, or 6 s after the host goes quiet
```

`core/internal/btschedule` can run these sessions unattended (aligned to the
clock, `GET`/`PUT /bluetooth/schedule`), and `core/internal/coverage` rebuilds
which radio was listening when, from heartbeats and mode events, for the
coverage timeline.

Rejected lines are counted per provider (`/capture/status` stats) and, when
the sensor id is recoverable, per sensor (`rejected_count`). Shutdown order:
stop providers → drain ingestion (≤ 5 s) → close WebSocket clients → stop
HTTP → close the database.

## Principles

- **One schema.** `docs/protocol.md` + `protocol/schemas/event.v4.schema.json`.
  The Go validator is the reference; firmware output is checked against it by
  `scripts/check-protocol.sh`. Strict: unknown fields are rejected.
- **Providers are interchangeable.** Detection consumes `events.Event`
  regardless of source (`core/internal/capture/provider.go`). A missing tool or
  device leaves the provider `unavailable`; Core keeps running.
- **No silent loss.** Nano counts on-device drops (heartbeat `queue_drops`) and
  uses per-boot sequence numbers; Core reports duplicates, gaps and restarts per
  sensor, and counts rejected lines by reason.
- **Read-only host.** Core never changes interface modes or NetworkManager.
  Monitor-mode interfaces are prepared by the operator.
- **Passive by default, gated TX.** Host→device traffic is `PING`, radio-mode
  commands (`MODE …`, operator- or schedule-initiated) and, only behind `-tx` +
  `confirm`, one rate-limited deauth test command (docs/tx.md), all through
  the serial provider (`capture.Commander`). The only radio transmissions are
  that deauth test burst and the opt-in Classic Bluetooth inquiry; BLE
  scanning is passive.
- **Derived ≠ observed.** Anything WRAITH works out rather than sees (client
  maker and device guess from the address, BLE classification, detector
  inferences) is labelled as such in the API and dashboard.
- **Local-first.** Core binds `127.0.0.1:8088` by default and rejects requests
  whose `Host` header is not loopback (DNS-rebinding guard). No cloud services.

## Components

| Path | Responsibility |
|------|----------------|
| `firmware/src/json_writer.c` | Allocation-free JSON writer, UTF-8 validation, escaping |
| `firmware/src/protocol.c` | Event envelope + builders (heartbeat, status) |
| `firmware/src/frame_parser.c` | Bounds-checked 802.11 management header parsing |
| `firmware/src/transport.c` | Message buffer + single writer task on UART0 |
| `firmware/src/system_status.c` | Identity (MAC + boot id), state, heartbeat task |
| `firmware/src/{wifi_monitor,beacon_parser,management_monitor}.c` | Passive monitoring, parsing, aggregation |
| `firmware/src/client_tracker.c` | Station sightings from data-frame headers (protocol v3) |
| `firmware/src/{lcd,lcd_screens,host_link}.c` | LCD display, host-link ping listener, TX command parsing |
| `firmware/src/tx_test.c` | Gated deauth test transmission engine (frame builder, limits, burst state machine) |
| `firmware/src/tx_raw_override.c` | TX build only: allows validated deauth/disassoc frames past the driver's raw-frame check |
| `firmware/src/detection.c` | On-device stub; detection lives in Core |
| `firmware/src/{radio_mode_core,radio_mode}.c` | Bluetooth build: Wi-Fi ⇄ Bluetooth switch (pure state machine + ESP-IDF glue), session limits, host-loss return |
| `firmware/src/{bt_manager,bt_protocol}.c` | Bluedroid bring-up/teardown, v4 `bluetooth.*` event builders |
| `firmware/src/{ble_parser,ble_tracker,ble_scanner}.c` | Passive BLE scan, AD-structure parsing, per-address aggregation |
| `firmware/src/{classic_parser,classic_tracker,classic_scanner}.c` | Bounded Classic inquiry slots, CoD/EIR parsing, Classic device tracking |
| `firmware/src/lcd_bt_screens.c` | LCD screens while in Bluetooth mode |
| `core/internal/events` | Event model + strict v1–v4 validator |
| `core/internal/transport` | Line framing (bounded, fragment-safe), dedup/seq tracking |
| `core/internal/storage` | SQLite (modernc, no CGO), embedded migrations, AP/client inventory, queries |
| `core/internal/capture` | Provider interface and manager (WRAITH's only source is the ESP32) |
| `core/internal/capture/serial` | ESP32 serial provider: raw termios, framing, validation, reconnect |
| `core/internal/ingest` | Ingestion pipeline (single writer) |
| `core/internal/detection` | Detection engine and detectors (Wi-Fi and Bluetooth) |
| `core/internal/bluetooth` | Bluetooth inventory processor, Class of Device decoding, stream throttle |
| `core/internal/btschedule` | Scheduled Bluetooth sessions: pure planner + runner, stored in `app_config` |
| `core/internal/coverage` | Radio coverage timeline (Wi-Fi / Bluetooth / no data) from stored events |
| `core/internal/devid` | Client address → maker (embedded IEEE MA-L table, `scripts/gen-oui.sh`), device-type guess, private-address flag |
| `core/internal/stream` | WebSocket hub: bounded per-client queues, Origin check, keepalive |
| `core/internal/system` | Host info, wireless interface discovery (sysfs + `iw`) |
| `core/internal/api` | Chi router, pagination, error format, host guard; Wi-Fi, Bluetooth, search, coverage, device-label and TX endpoints |
| `core/migrations` | Embedded SQL migrations 0001–0007 (0004 clients, 0005 Bluetooth, 0006 Bluetooth findings, 0007 device labels) |
| `dashboard/src/services/api.ts` | Typed, centralized API client |
| `dashboard/src/hooks/useLiveStream.ts` | WebSocket with exponential backoff + jitter |
| `dashboard/src/services/live.tsx` | One shared stream per tab: live feed, AP updates, debounced page refresh |
| `dashboard/src/services/demo.ts` | Labelled demo dataset (02:DE BSSIDs), only when demo mode is on |
| `dashboard/src/pages/`, `pages/bluetooth/` | Master overview, Wi-Fi pages, Bluetooth pages, System pages (routes in `src/app/App.tsx`) |
| `dashboard/src/components/ClientDevice.tsx` | Client "Device" cell: name (inline edit), maker, device guess |

## Firmware concurrency model (Phase 2 target)

The Wi-Fi promiscuous callback runs in the Wi-Fi driver task. It does only:
filter management frames → copy ≤ 512 bytes + RSSI/channel into a fixed slot →
`xQueueSendFromISR`-style non-blocking post (drops counted). A processing task
parses and aggregates; serialization happens there; `transport_send` copies the
line into a FreeRTOS message buffer; a single writer task does `fwrite` so lines
never interleave. The LCD task reads counter snapshots and never blocks others.

## Why these choices

- **modernc.org/sqlite** — pure Go, so `go build` needs no C toolchain or CGO.
- **Host guard instead of CORS** — the dashboard is same-origin (served by Core or
  proxied by Vite), so no CORS headers are emitted at all.
- **Timestamps** — Nano has no clock; Core's `received_at` orders events.
