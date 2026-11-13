# Bluetooth event protocol (protocol v4)

Status: **IMPLEMENTED — TESTED (Core validator, schema, fixtures, ingest).**
No firmware emits these events yet (the Bluetooth firmware build is a later
phase), so nothing here is hardware-validated.

Protocol v4 is v3 plus the `bluetooth.*` event types below. Everything else —
framing (`<json>\t<crc32>\n`), the 1024-byte line limit, event IDs, sequence
numbers, duplicate and gap detection — is unchanged (see
[../protocol.md](../protocol.md)).

## Compatibility

- Core accepts v1–v4 side by side (`specsByVersion` in `core/internal/events`).
- `bluetooth.*` types **require** `version: 4`; the same event sent as v3 is
  rejected as `unknown_event_type` (fixture `bt_wrong_version.json`).
- The Wi-Fi-only firmware build (`pio run`, the default) keeps emitting v3 and
  is unaffected. Only the Bluetooth-enabled build will emit v4.
- An older Core (v1–v3 only) rejects v4 events with `unsupported_version` and
  counts them per sensor — visible, never silently stored.

## Envelope rules for Bluetooth events

The envelope is unchanged. Bluetooth specifics live **only in metadata**:

- `bssid` and `channel` are Wi-Fi fields and are **not** used by Bluetooth
  events (a Bluetooth device is not a BSSID; BLE advertising channels 37–39
  and the Classic hop sequence do not map onto Wi-Fi channels).
- The device address is metadata `address`, a 48-bit BD_ADDR written like a
  MAC: `AA:BB:CC:DD:EE:FF`, uppercase.
- Unknown metadata fields are rejected, as for every other type.

## Address semantics

BLE advertisers use `address_type` `public` or `random`. A random address may
be static or private (resolvable / non-resolvable) and may rotate every few
minutes. **An address is an observation, not a device identity**: WRAITH never
merges two addresses into one device based on similar advertising data, and a
rotating address is not treated as suspicious.

## Event types

`?` marks optional fields.

### `bluetooth.classic.device_discovered` / `bluetooth.classic.device_updated`
A BR/EDR device answered an inquiry (`discovered` on first sight in this
boot, `updated` when its metadata changes or on an aggregation window).
```
address        BD_ADDR
name?          string ≤ 248 bytes   only if the device's name was obtained
cod?           integer 0–16777215   24-bit Class of Device, raw
device_class?  "audio" | "phone" | "computer" | "peripheral" | "input" |
               "wearable" | "imaging" | "network" | "health" | "toy" |
               "uncategorized" | "unknown"    derived from cod (major class)
rssi?          integer -127–0       when the inquiry result carries it
count?         integer ≥ 1          inquiry responses aggregated
window_ms?     integer ≥ 1
```
`device_class` is decoded from the Class of Device field the device itself
advertises; it is never guessed from the address.

### `bluetooth.ble.advertisement_observed`
Aggregated advertising reports from one advertiser.
```
address            BD_ADDR
address_type       "public" | "random"
rssi               integer -127–0    latest (or strongest in window)
adv_type?          "ind" | "direct_ind" | "scan_ind" | "nonconn_ind" | "scan_rsp"
payload_len?       integer 0–255     AD payload bytes
flags?             integer 0–255     AD Flags
name?              string ≤ 248      Complete or Shortened Local Name
tx_power?          integer -127–127  TX Power Level AD
services?          array of UUID strings (≤ 16, each ≤ 36 chars)
manufacturer_id?   integer 0–65535   Company Identifier
manufacturer_data? hex, ≤ 32 bytes   bytes after the company id
window_ms?, count? integers ≥ 1
```
UUIDs are lowercase hex: 4 chars (16-bit), 8 chars (32-bit) or the canonical
36-char 128-bit form.

### `bluetooth.ble.device_discovered` / `bluetooth.ble.device_updated`
Per-address device record kept by the sensor's BLE tracker.
```
address, address_type        as above
name?, rssi?, services?, manufacturer_id?
classification?  "beacon" | "wearable" | "phone" | "computer" | "peripheral" |
                 "sensor" | "tag" | "medical" | "unknown"     INFERRED
confidence?      number 0–1   confidence of the inferred classification
count?           integer ≥ 1
```

### `bluetooth.ble.connection_observed` / `bluetooth.ble.connection_lost`
Only produced by operator-initiated GATT discovery (disabled by default).
```
address, address_type?
reason?  string ≤ 128   (connection_lost only)
```

### `bluetooth.ble.gatt_service_discovered`
```
address, uuid, start_handle?, end_handle?, primary?
```

### `bluetooth.ble.gatt_characteristic_discovered`
```
address, service_uuid?, uuid, handle?,
properties?  array (≤ 8) e.g. "read", "write", "write_no_response", "notify",
             "indicate", "broadcast", "signed_write", "extended"
```
WRAITH never writes characteristics; discovery only.

### `bluetooth.system.status`
```
state             "idle" | "scanning_ble" | "scanning_classic" | "scanning_dual" | "error"
mode              "wifi" | "bluetooth"     the sensor's radio mode (see architecture.md)
classic_devices?  integer ≥ 0
ble_devices?      integer ≥ 0
message?          string ≤ 128
```

### `bluetooth.system.error`
```
code     string ≤ 64    e.g. "controller_init_failed", "inquiry_failed"
message  string ≤ 256
```

## Fixtures and tests

- Valid: `protocol/fixtures/valid/bt_*.json` (Classic device, BLE
  advertisement, BLE device, GATT characteristic, system status).
- Invalid: `protocol/fixtures/invalid/bt_*.json` (bad address, missing
  address, unknown metadata field, bad address type, wrong version) — each
  asserted with its exact rejection reason in `validate_test.go`.
- Schema: `protocol/schemas/event.v4.schema.json`; it agrees with the Go
  validator on every `bt_*` fixture.
- Ingest: `TestBluetoothEventsStoredWithoutTouchingWifi` feeds v4 events
  through the real pipeline: stored as events, 0 failures, no AP rows, no
  Wi-Fi findings, no sequence gaps.
