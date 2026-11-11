# Architecture

```
 WRAITH Nano (ESP32)          WRAITH Core (Go, Fedora)                 WRAITH Web
 ───────────────────          ────────────────────────                 ──────────
 promiscuous RX cb            esp32-serial provider                    React + TS
   │ (copy ≤512 B, ring buf)    │ framing, CRC-32 check                Vite, Tailwind
 processing task                ▼                                      Recharts
   │ frame/beacon parse       validate (events.Parse)
   │ AP tracker, mgmt windows   dedup / seq gaps
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
                          │   detection.Engine (sees AP before/after) · findings
                          └─▶ after commit: stream.Hub.Publish → WebSocket clients
```

Rejected lines are counted per provider (`/capture/status` stats) and, when
the sensor id is recoverable, per sensor (`rejected_count`). Shutdown order:
stop providers → drain ingestion (≤ 5 s) → close WebSocket clients → stop
HTTP → close the database.

## Principles

- **One schema.** `docs/protocol.md` + `protocol/schemas/event.v1.schema.json`.
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
| `firmware/src/{lcd,lcd_screens,host_link}.c` | LCD display, host-link ping listener |
| `firmware/src/detection.c` | On-device stub; detection lives in Core |
| `core/internal/events` | Event model + strict v1 validator |
| `core/internal/transport` | Line framing (bounded, fragment-safe), dedup/seq tracking |
| `core/internal/storage` | SQLite (modernc, no CGO), embedded migrations, queries |
| `core/internal/capture` | Provider interface and manager (WRAITH's only source is the ESP32) |
| `core/internal/capture/serial` | ESP32 serial provider: raw termios, framing, validation, reconnect |
| `core/internal/ingest` | Ingestion pipeline (single writer) |
| `core/internal/detection` | Detection engine and detectors |
| `core/internal/stream` | WebSocket hub: bounded per-client queues, Origin check, keepalive |
| `core/internal/system` | Host info, wireless interface discovery (sysfs + `iw`) |
| `core/internal/api` | Chi router, pagination, error format, host guard |
| `dashboard/src/services/api.ts` | Typed, centralized API client |
| `dashboard/src/hooks/useLiveStream.ts` | WebSocket with exponential backoff + jitter |
| `dashboard/src/services/live.tsx` | One shared stream per tab: live feed, AP updates, debounced page refresh |
| `dashboard/src/services/demo.ts` | Labelled demo dataset (02:DE BSSIDs), only when demo mode is on |

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
