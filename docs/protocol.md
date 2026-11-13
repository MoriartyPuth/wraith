# WRAITH Event Protocol — v4

Status: **IMPLEMENTED — TESTED** (Go validator + firmware serializer host tests).
v1 events are extensively hardware-validated (phases 2–5); the v2 (`tx.*`) and
v3 (`wifi.client_observed`) additions were validated on ESP32 hardware
2026-11-12.

All WRAITH components exchange events as JSON objects. The machine-readable
schema is `protocol/schemas/event.v4.schema.json`; canonical valid/invalid
examples live in `protocol/fixtures/`. The Go validator
(`core/internal/events`) is the reference implementation and is tested against
those fixtures. The firmware serializer is checked against the Go validator by
`scripts/check-protocol.sh`.

v4 is **v3 plus the `bluetooth.*` event types** (Bluetooth Classic + BLE,
emitted only by the Bluetooth-enabled switch-mode firmware build; see
[docs/bluetooth/event-protocol.md](bluetooth/event-protocol.md)). The
Wi-Fi-only firmware build still emits v3. v3 was v2 plus one event type:
`wifi.client_observed` (station sightings from data frames). v2 was v1 plus `tx.ack`/`tx.report` (deauth test
transmission, docs/tx.md). Core accepts v1–v4 envelopes; sensors built
for older versions keep working.

## Framing (USB serial)

- UTF-8, one event per line: `<json>` `\t` `<crc32>` `\n`, where `<crc32>` is
  the CRC-32 (IEEE 802.3 / zlib, as Go's `crc32.ChecksumIEEE`) of the JSON
  bytes, as 8 lowercase hex digits. JSON never contains a raw tab, so the last
  tab is the separator. A preceding `\r` before `\n` is tolerated and stripped.
- Core's serial provider **requires** the checksum: lines with a missing or
  wrong checksum are rejected (`missing_checksum`, `checksum_mismatch`) and
  counted, never parsed. This exists because USB-serial adapters can corrupt
  lines in ways that still yield valid JSON (see docs/hardware.md, "Serial
  link"). `wraith validate` checks the checksum when present;
  `--require-checksum` makes it mandatory. Shared fixtures are bare JSON.
- **Maximum line length: 1024 bytes** including checksum suffix and newline. Longer lines are
  discarded by the receiver up to the next `\n` and counted as `oversize`.
- Lines that do not start with `{` are treated as non-protocol output (e.g.
  ESP-IDF boot log) and ignored, but counted. The firmware sends ESP-IDF logs to
  UART0 alongside events, so receivers **must** tolerate interleaved log lines.
- The receiver must not assume a single `read()` returns a complete line.

## Envelope

| Field        | Type    | Required | Rules |
|--------------|---------|----------|-------|
| `version`    | integer | yes      | `1`–`4`. `tx.*` require v2; `wifi.client_observed` requires v3; `bluetooth.*` require v4. |
| `event_id`   | string  | yes      | 1–64 chars, `[A-Za-z0-9._:-]`. Unique per sensor. |
| `source`     | string  | yes      | `wraith-nano`, `wraith-core`. (`tshark`/`kismet` were removed: WRAITH has no host capture.) |
| `sensor_id`  | string  | yes      | 1–64 chars, `[A-Za-z0-9._:-]`. |
| `event_type` | string  | yes      | One of the types below. |
| `timestamp`  | integer | yes      | Unix seconds. `0` = sensor wall clock unknown (see below). |
| `seq`        | integer | yes      | Per-boot monotonic counter, starts at 0, ≥ 0. |
| `channel`    | integer | no       | 1–233. Nano only reports 1–14. Required for `tx.report` (the pinned transmit channel). |
| `bssid`      | string  | no       | `AA:BB:CC:DD:EE:FF`, uppercase hex. Required for `tx.report`; optional for `tx.ack`. |
| `metadata`   | object  | yes      | Type-specific; see below. `{}` if empty. |

Unknown top-level fields are **rejected** (strict). Unknown metadata fields are
**rejected** too, so firmware and core cannot silently drift.

### Timestamps

The ESP32 has no battery-backed clock and WRAITH Nano does not use NTP (it
never joins a network). Nano therefore sends `timestamp: 0` and includes
`uptime_ms` in heartbeats. WRAITH Core records its own `received_at` for every
event and uses that for ordering when `timestamp` is `0`.

### Host → device

The link is two-way. Core writes the ASCII line `PING\n` every 2 s while it
reads the port. Nano treats the host as connected while pings arrive (timeout
6 s; shown on the LCD) and, when a host (re)connects, sends a `sensor.status`
within one heartbeat interval so a late-attaching Core learns state and
capabilities immediately.

All other input is ignored **unless** the firmware was built with
`WRAITH_TX_ENABLED=1` (docs/tx.md), in which case these lines are accepted
(and silently dropped otherwise):

```
TX_DEAUTH   <bssid> <client> <count> <interval_ms> <reason> <channel>
TX_DISASSOC <bssid> <client> <count> <interval_ms> <reason> <channel>
TX_STATUS
TX_STOP
```

Lines are ≤ 96 chars, no tabs/newlines; invalid or out-of-limit commands are
answered with a `rejected` `tx.ack` event rather than applied. The sensor's
hard limits (≤ 10 frames, ≥ 100 ms spacing, 30 s cooldown, unicast client)
re-validate every parameter; Core's checks exist to fail fast at the API.

## Event IDs, duplicates and reconnects

Nano event IDs are `<boot_id>-<seq>`, where `boot_id` is 8 hex chars drawn from
the hardware RNG at boot. Core:

- drops an event whose `(sensor_id, event_id)` was seen recently
  (bounded LRU) and counts it as `duplicate`;
- treats a new `boot_id` as a sensor restart, and a gap in `seq` within the same
  boot as **reported loss** (`seq_gap`), never as silent loss.

## Event types and metadata

`?` marks optional fields.

### `sensor.heartbeat`
Sent every 5 s.
```
uptime_ms      integer ≥ 0
free_heap      integer ≥ 0
channel?       integer 1–14     current monitor channel
queue_drops    integer ≥ 0      events dropped on-device since boot
```

### `sensor.status`
Sent at boot, on state change, and every 60 s. While monitoring, Nano's
`message` carries capture statistics (≤ 5 s old), e.g.
`monitoring 2.4GHz; aps=10 rx=767 drop=0 bad=0 deauth=0 disassoc=0`, where
`drop` counts frames lost because the processing task fell behind. These are
informational; the counters are carried in the free-text `message` rather than
as structured fields.
```
state             "booting" | "online" | "degraded" | "error"
firmware_version  string (semver)
board?            string
capabilities      array of string  e.g. ["wifi-2.4ghz","promiscuous","lcd"]
message?          string ≤ 128
```

### `wifi.ap_observed`
Sent when an AP (BSSID) is first seen, whenever its SSID bytes, channel,
auth mode, RSN suites or PMF bits change, and as a refresh every 60 s while
the AP is still being heard (so a late-attaching Core learns auth mode and
PMF within a minute). Nano derives it from
beacons (probe responses are not used). Requires `bssid`, `channel`
(the AP's DS Parameter Set channel when present, else the receive channel).
```
ssid        string ≤ 32 bytes UTF-8 (may be "")   display form (see below)
ssid_hex    string, 0–64 lowercase hex chars      authoritative raw bytes
rssi        integer -127–0
auth_mode?  string  one of: "open", "wep", "wpa-psk", "wpa-eap", "wpa2-psk",
                    "wpa-wpa2-psk", "wpa2-eap", "wpa-wpa2-eap", "wpa2-wpa3-psk",
                    "wpa3-sae", "wpa3-eap-192", "owe", "rsn-other", "rsn-malformed"
hidden?     boolean
```

SSIDs are arbitrary bytes on air. If the raw SSID is valid UTF-8 it is sent
as-is in `ssid` (control characters JSON-escaped); otherwise every byte ≥ 0x80
is replaced by `?`. Use `ssid_hex` whenever exact bytes matter (e.g. comparing
look-alike SSIDs in rogue-AP detection).

### `wifi.beacon_observed`
Aggregated beacon sighting. Nano sends one immediately with every
`wifi.ap_observed`, then at most one per BSSID per 10 s with `count` = beacons
received since the previous report.
Requires `bssid`, `channel`.
```
ssid, ssid_hex, rssi       as above
beacon_interval?           integer (TU)
capability?                integer (16-bit capability info field)
rsn?                       object (see RSN object), absent if no RSN IE
count?                     integer ≥ 1  beacons aggregated in this report
```

### `wifi.deauth_observed` / `wifi.disassoc_observed`
Aggregated over a window. Requires `bssid` (BSSID field of the frame).
`channel` is the channel the radio was tuned to: these frames carry no
channel field, and the ESP32 also receives adjacent channels, so it may
differ by 1–2 from the AP's channel.
```
count       integer ≥ 1
window_ms   integer ≥ 1
src?        MAC   transmitter address (addr2) of the last frame
dst?        MAC   receiver address (addr1); FF:FF:FF:FF:FF:FF = broadcast
reason_codes? array of integer 0–65535 (distinct, ≤ 8)
rssi?       integer -127–0
protected?  boolean   true if Protected Frame bit was set (PMF-protected)
```

### `wifi.pmf_observed`
Requires `bssid`.
```
pmf          "required" | "capable" | "not_advertised" | "unavailable"
rsn_present  boolean
```
This reflects what the AP **advertises** in its RSN IE. It says nothing about
what any individual client negotiated. Nano sends it with every
`wifi.ap_observed` (first sight, change, 60 s refresh). `unavailable` is also used for an RSN element
that is malformed or advertises MFPR without MFPC (invalid); the raw bits are
in the `rsn` object of `wifi.beacon_observed`.

### `detection.finding`
Emitted by Core (and possibly Nano later).
```
finding_id      string
category        string  e.g. "deauth_activity"
severity        "info" | "low" | "medium" | "high"
confidence      number 0–1
classification  "observed" | "suspicious" | "potential_attack" | "confirmed"
explanation     string ≤ 1024
evidence?       array of event_id strings (≤ 64)
```

### `capture.provider_status`
```
provider   string
state      "unavailable" | "stopped" | "starting" | "running" | "error"
message?   string ≤ 256
```

### `wifi.client_observed` (v3 only)
Emitted when a station is seen exchanging data with an AP at this BSSID.
Requires `bssid` and `channel`. This is a **sighting**, not authoritative
association state: the sensor only sees the channel it is tuned to, idle
clients transmit nothing, and station MACs may be randomised.
```
client      MAC        station address from the 802.11 header
frames      integer ≥ 1  data frames aggregated in this report
window_ms   integer ≥ 1  aggregation window
rssi?       integer -127–0
```
Direction is derived from the ToDS/FromDS bits (station→AP, AP→station, or
IBSS); WDS frames, multicast destinations and frames where client == BSSID
are ignored. The sensor reports a station's first sighting immediately, then
aggregates further frames into ≥ 10 s windows.

### `tx.ack` (v2 only)
Sent by Nano in response to a `TX_*` command. Envelope `bssid` is optional
(the command itself carries the target).
```
command     "deauth" | "disassoc" | "status"
status      "accepted" | "rejected"
detail?     string ≤ 128   why rejected (limits, disabled, malformed…)
client?     MAC            echoed target client for accepted bursts
```

### `tx.report` (v2 only)
Sent once per burst when it ends. Requires envelope `bssid` (the target)
and `channel` (the channel the radio was pinned to).
```
command     "deauth" | "disassoc" | "status"
client      MAC
reason      integer 0–65535
sent        integer ≥ 0    frames actually transmitted
requested   integer ≥ 1    frames the burst asked for
outcome     "complete" | "aborted_host_lost" | "aborted_radio"
```
`sent < requested` means the burst was cut short; `aborted_host_lost` fires
when the USB link (PING) dropped mid-burst, `aborted_radio` when a transmit
call failed. The burst aborts within one frame spacing of the trigger.

### RSN object
```
version          integer
group_cipher     string  e.g. "ccmp-128"; "absent" if the element ends after version
pairwise         array of string
akm              array of string  e.g. "psk","sae","802.1x","owe","ft-psk"
mfpc             boolean   Management Frame Protection Capable
mfpr             boolean   Management Frame Protection Required
```
`mfpr` without `mfpc` is an invalid advertisement; it is still reported as
observed and flagged by detection. Nano stores at most 4 pairwise and 4 AKM
suites per element. Unknown suites are written as `"oo-oo-oo:type"`.

## Errors

Core rejects invalid events with a reason (`unsupported_version`,
`unknown_field`, `missing_field`, `invalid_field`, `unknown_event_type`,
`oversize`, `malformed_json`). Rejections are counted per sensor and visible via
`/api/v1/sensors/{id}`; they are never silently discarded.

## Live stream (Core → dashboard)

`GET /api/v1/ws` (WebSocket). Each text message is
`{"type": T, "time": <unix ms>, "data": ...}` where `T` is one of:

| type | data |
|------|------|
| `hello` | `{"protocol": N}` on connect, where `N` is Core's newest protocol version (currently 4) |
| `event` | stored event row (`id`, `event_id`, `event_type`, `payload`, …), after commit; `bluetooth.ble.advertisement_observed` is not streamed raw (see `bt_device`) |
| `access_point` | AP inventory row after an AP-related event |
| `client` | client sighting row after a `wifi.client_observed` event (v3) |
| `finding` | new detection finding |
| `sensor` | sensor row after heartbeat/status/restart |
| `provider` | capture provider status change |
| `bt_device` | Bluetooth device row after a device event, throttled (v4; docs/bluetooth/api.md) |
| `bt_status` | sensor's Bluetooth subsystem state after `bluetooth.system.status`/`.error` (v4) |

Clients send nothing (read limit 512 bytes; ping/pong keepalive). A client
that falls 256 messages behind is disconnected and should reconnect and
re-fetch via REST. Browser `Origin` must be a loopback host (or listed in
`allowed_hosts`); other origins get HTTP 403.

## Versioning

Additive changes to metadata require a version bump because validation is
strict. v2 added the `tx.*` event types (2026-11-11) and v3 added
`wifi.client_observed` (2026-11-12), and v4 added the `bluetooth.*` event
types (2026-11-13). Core supports v1–v4 side by side
(`specsByVersion` in `core/internal/events`), and the live stream's `hello`
message carries the Core's protocol version. Sensors report their protocol
version implicitly through the events they send.
