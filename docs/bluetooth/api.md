# Bluetooth API (REST + live stream)

Status: **IMPLEMENTED — TESTED** (endpoint tests through the real ingest
pipeline, throttle tests, real-binary smoke test) and in use with the `bt`
firmware build on hardware. The GATT endpoints stay empty: GATT discovery
(BT-9) is not implemented.

All endpoints are read-only except `POST /bluetooth/mode` (below) and
`PUT /bluetooth/schedule` ([schedule.md](schedule.md)). They live under `/api/v1`, on Core's existing router: same
loopback host guard (DNS-rebinding protection), 15 s request timeout,
pagination (`limit` 1–500, `offset`) and error format
(`{"error":{"code","message"}}`) as every other endpoint. No separate server.

## Device identifiers

A device row is one observed address (an address is not a physical device;
BLE random addresses rotate and are never merged). Its `id`:

| Transport | `id` |
|-----------|------|
| Classic | `classic:AA:BB:CC:DD:EE:FF` |
| BLE | `ble:public:AA:BB:CC:DD:EE:FF` or `ble:random:AA:BB:CC:DD:EE:FF` |

Colons may be sent `%3A`-encoded; the address may be lowercase. Anything else
is `400 invalid_parameter`; a well-formed id that was never observed is `404`.

## Endpoints

| Endpoint | Returns |
|----------|---------|
| `GET /bluetooth/status` | `sensors`: per-sensor radio `mode`, scan `state`, device counts the sensor reported, `error_count` and last error; `devices`: stored `classic`, `ble`, `total`, and `recent_classic` / `recent_ble` (seen in the last `recent_window_ms`, 10 min) |
| `GET /bluetooth/devices` | All devices, newest `last_seen` first. Filters: `transport=classic\|ble`, `since=<unix ms>` |
| `GET /bluetooth/classic/devices` | Classic only (`since` filter) |
| `GET /bluetooth/ble/devices` | BLE only (`since` filter) |
| `GET /bluetooth/devices/{id}` | One device |
| `GET /bluetooth/devices/{id}/observations` | RSSI history / per-event observations, newest first; `limit` 1–1000 (default 200) |
| `GET /bluetooth/devices/{id}/events` | The stored events for this device, newest first, paginated |
| `GET /bluetooth/ble/devices/{id}/services` | GATT services found by operator-initiated discovery (newest per UUID) |
| `GET /bluetooth/ble/devices/{id}/characteristics` | GATT characteristics (newest per service/UUID/handle); `truncated: true` if more than 1000 GATT events exist |
| `GET /bluetooth/events` | Event history restricted to `bluetooth.*`; same filters as `/events` (`type`, `sensor`, `since`, `until`, `event_id`) except `bssid`; a non-Bluetooth `type` is `400` |

Device fields (see [event-protocol.md](event-protocol.md), "Core inventory"):
`id, transport, address, address_type, name, cod, device_class,
classification, confidence, manufacturer_id, tx_power, services, last_rssi,
rssi_avg, observations, first_seen, last_seen, last_sensor`. A field the
sensor never reported is `null` — never filled in. `device_class` (Classic) is
decoded from the device's own Class of Device; `classification` (BLE) is the
sensor's inference and comes with `confidence`.

GATT lists are empty unless an operator ran GATT discovery on that device
(off by default; a later phase).

## Radio mode (switch-mode sensors)

`POST /api/v1/bluetooth/mode`, body `{"mode": "bluetooth" | "wifi" | "status"}`,
optionally `"classic": true | false` with `"mode": "bluetooth"`.

Sends `MODE BLUETOOTH` / `MODE WIFI` / `MODE STATUS` to the sensor
(`"classic": true` → `MODE BLUETOOTH CLASSIC`). Plain `bluetooth` is BLE
passive scanning only — and turns Classic inquiry off if it was on. **Classic
inquiry transmits** (bounded slots, [architecture.md](architecture.md#firmware-bounded-classic-inquiry-bt-7)),
so it is only ever sent on an explicit `"classic": true`. The sensor
has one radio: **`bluetooth` pauses Wi-Fi monitoring** on it until `wifi`, a
lost host link, or the sensor's session limit (15 min by default). The answer
arrives asynchronously as `bluetooth.system.status` (or `.error`) — the
`bt_status` stream message and `GET /bluetooth/status`.

| Response | When |
|----------|------|
| `202` | command sent (`data.command` and `data.classic` echo it) |
| `400 invalid_body` | not `{"mode": ...}` with one of the three values (plus optional boolean `classic`, only with `bluetooth`) |
| `415` | not `application/json` — the CSRF guard: cross-site pages cannot switch the radio |
| `409 radio_switch_unsupported` | no sensor announced the `radio-switch` capability (flash `pio run -e bt`) |
| `409 classic_unsupported` | `"classic": true` but no sensor announced `classic-inquiry` (firmware older than BT-7) |
| `409 sensor_not_connected` | no capture provider running |
| `502 command_send_failed` | the serial write failed |

The dashboard's Bluetooth overview has the control on each sensor card, with a
confirmation step that states Wi-Fi monitoring will pause. In Bluetooth mode
it offers *Classic inquiry…* (a second confirmation that says it transmits)
and *Stop Classic inquiry*.

## Live stream (WebSocket `/api/v1/ws`)

Two message kinds, alongside the existing ones (docs/protocol.md):

| `type` | `data` | When |
|--------|--------|------|
| `bt_device` | device row (as `GET /bluetooth/devices/{id}`) | after a device-describing event, **throttled** |
| `bt_status` | the sensor's row from `/bluetooth/status` `sensors` | after `bluetooth.system.status` or `.error` |

Bluetooth events other than advertisement reports are also sent as ordinary
`event` messages.

### Why and how it is throttled

The hub disconnects a client that falls 256 messages behind, and Wi-Fi and
Bluetooth messages share that queue. A crowded BLE environment (hundreds of
advertisers) must not be able to push Wi-Fi messages out. Therefore:

- `bluetooth.ble.advertisement_observed` — the highest-rate type — is stored
  and appears in the event history, but is **not** streamed as a raw `event`;
  its effect reaches the dashboard as a `bt_device` update.
- `bt_device` updates are limited to **one per device per 2 s** and **20 per
  second overall** (burst 40), in `core/internal/bluetooth/throttle.go`.
- Suppressed updates are still stored; the dashboard re-fetches over REST.
  The count is `bt_stream_suppressed` in `/api/v1/statistics` → `runtime`.

The limit is leading-edge: when an update is suppressed and no further event
arrives for that device, the live view keeps the earlier values until the
next update or REST refresh.
