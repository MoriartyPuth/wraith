# WRAITH Event Protocol — v1

Status: **IMPLEMENTED — TESTED** (Go validator + firmware serializer host tests).
Not yet validated on ESP32 hardware.

All WRAITH components exchange events as JSON objects. The machine-readable
schema is `protocol/schemas/event.v1.schema.json`; canonical valid/invalid
examples live in `protocol/fixtures/`. The Go validator
(`core/internal/events`) is the reference implementation and is tested against
those fixtures. The firmware serializer is checked against the Go validator by
`scripts/check-protocol.sh`.

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
| `version`    | integer | yes      | Must equal `1`. |
| `event_id`   | string  | yes      | 1–64 chars, `[A-Za-z0-9._:-]`. Unique per sensor. |
| `source`     | string  | yes      | `wraith-nano`, `wraith-core`. (`tshark`/`kismet` were removed: WRAITH has no host capture.) |
| `sensor_id`  | string  | yes      | 1–64 chars, `[A-Za-z0-9._:-]`. |
| `event_type` | string  | yes      | One of the types below. |
| `timestamp`  | integer | yes      | Unix seconds. `0` = sensor wall clock unknown (see below). |
| `seq`        | integer | yes      | Per-boot monotonic counter, starts at 0, ≥ 0. |
| `channel`    | integer | no       | 1–233. Nano only reports 1–14. |
| `bssid`      | string  | no       | `AA:BB:CC:DD:EE:FF`, uppercase hex. |
| `metadata`   | object  | yes      | Type-specific; see below. `{}` if empty. |

Unknown top-level fields are **rejected** (strict). Unknown metadata fields are
**rejected** too, so firmware and core cannot silently drift.

### Timestamps

The ESP32 has no battery-backed clock and WRAITH Nano does not use NTP (it
never joins a network). Nano therefore sends `timestamp: 0` and includes
`uptime_ms` in heartbeats. WRAITH Core records its own `received_at` for every
event and uses that for ordering when `timestamp` is `0`.

### Event IDs, duplicates and reconnects

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
informational; a structured form is planned for protocol v2.
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
| `hello` | `{"protocol": 1}` on connect |
| `event` | stored event row (`id`, `event_id`, `event_type`, `payload`, …), after commit |
| `access_point` | AP inventory row after an AP-related event |
| `finding` | new detection finding |
| `sensor` | sensor row after heartbeat/status/restart |
| `provider` | capture provider status change |

Clients send nothing (read limit 512 bytes; ping/pong keepalive). A client
that falls 256 messages behind is disconnected and should reconnect and
re-fetch via REST. Browser `Origin` must be a loopback host (or listed in
`allowed_hosts`); other origins get HTTP 403.

## Versioning

Additive changes to metadata require a version bump because validation is
strict. Core will support multiple versions side by side when v2 exists.
