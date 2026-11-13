# Bluetooth testing

What is tested, how, and what is not. Everything below runs without
hardware; **no Bluetooth feature has been validated on an ESP32 yet** (the
Bluetooth firmware build is a later phase).

`scripts/check-all.sh` runs all of it (Go, protocol, firmware host tests,
both firmware builds, dashboard unit tests, typecheck, build).

## Protocol (BT-1)

- `protocol/fixtures/{valid,invalid}/bt_*.json` against the Go validator,
  each invalid fixture with its exact rejection reason
  (`core/internal/events/validate_test.go`).
- The v4 JSON schema agrees with the Go validator on every `bt_*` fixture
  (checked with Python `jsonschema`, Draft 2020-12).

## Core (BT-2, BT-3)

| Area | Tests |
|------|-------|
| Class of Device decoding, merge rules (never invent, empty names ignored, additive counts, RSSI average, service union cap) | `core/internal/bluetooth/bluetooth_test.go` |
| Stream throttle: per device, global rate, bounded memory | `core/internal/bluetooth/throttle_test.go` |
| All 11 event types through the real ingest pipeline; Wi-Fi tables untouched; GATT address-type resolution; status/error tally | `TestBluetoothInventoryThroughPipeline`, `TestBluetoothEventsStoredWithoutTouchingWifi` |
| Retention: old addresses pruned, observations cascade | `TestBluetoothRetention` |
| Live stream: raw advertisements not streamed, device updates throttled, status streamed | `TestBluetoothStreamThrottled` |
| Every REST endpoint, filters, id encodings, 400/404, host guard | `core/internal/api/bluetooth_test.go` |

Mutation checks: for BT-2 and BT-3, eleven deliberate breakages of the rules
above (implicit counting, missing address-type resolution, disabled pruning,
empty-name overwrite, raw advertisements streamed, throttle removed, token
bucket removed, unbounded throttle map, unrestricted event history, GATT
duplicates, unvalidated device id) each make a test fail. `go test -race`
is clean.

Migration 0005 was applied to a copy of a live database (23 k events):
schema 1–4 → 1–5, every Wi-Fi table's row count unchanged,
`integrity_check` ok, `foreign_key_check` clean.

## Dashboard (BT-4)

- Unit tests (`dashboard/src/utils/bluetooth.test.ts`, Vitest): id encoding,
  company/UUID lookups (unknown ids never guessed), histogram bucket
  boundaries and a 100 000-address run, the observed / derived / inferred /
  unavailable labelling, coexistence wording, search, a description for every
  `bluetooth.*` type (and no crash on empty metadata), demo-data markers.
  Three mutations (bucket boundary, inferred shown as observed, guessed
  vendor) each fail a test.
- Rendering: the built dashboard served by a real Core over a database
  seeded through the real ingest pipeline, driven in headless Firefox over
  WebDriver BiDi: overview, both lists, three device pages (BLE with GATT,
  BLE with inferred class, unnamed Classic), unknown device (404 state),
  Bluetooth events, Wi-Fi access points and the general history, and the demo
  pages — expected content present and no console errors on every page.
  Screenshots were inspected; one defect found and fixed (a screen-reader
  table caption painted over the histogram note).
- The dashboard has no DOM test library; logic lives in pure modules so it can
  be unit-tested without adding one.

## Firmware switch mode (BT-5)

- **Wi-Fi builds unchanged, proven.** Before any firmware edit, all 463
  objects of the default and `tx` builds were fingerprinted (a clean rebuild
  reproduced every hash). After BT-5, both builds were rebuilt and compared
  with a build of the previous commit in a separate worktree: all 462 compiled
  objects are identical in code and data (`objcopy --strip-debug`; only debug
  line/column records of the three hooked files differ), and the linked
  `firmware.elf` sections `.iram0.vectors`, `.iram0.text`, `.dram0.data`,
  `.flash.text`, `.flash.rodata` are identical; `.flash.appdesc` differs by
  the 3 bytes of the compile time.
- Host tests (`firmware/test/host/test_bt.c`, second binary at protocol v4,
  ASan/UBSan): command parsing (MODE vs TX/PING/other input, malformed MODE
  commands), switch order (Wi-Fi fully stopped before Bluetooth starts and
  vice versa), idempotent requests, `MODE STATUS`, return to Wi-Fi on host
  loss and on the session limit (including across the 32-bit millisecond
  wrap), every failure path (Bluetooth start/stop failure, Wi-Fi stop/start
  failure with retry), truncation of over-long strings.
- Every `bluetooth.system.*` line the firmware code emits passes the Go
  validator with checksums (`scripts/check-protocol.sh`).
- `pio run -e bt` is part of `scripts/check-all.sh`.
- Core: `POST /bluetooth/mode` — capability gate, connected-sensor gate,
  CSRF content-type guard, strict body, command per mode, send failure
  (`TestBluetoothModeEndpoint`; the gate and guard are mutation-checked).

## Not tested yet

- Anything on hardware: Bluedroid start/stop on the ESP32, the radio-mode
  switch both ways, repeated switching, heap and stack high-water marks, the
  Bluetooth build's Wi-Fi capture rate vs the Wi-Fi build (IRAM trade-off),
  BLE scanning and Classic inquiry (not implemented yet).
- Live WebSocket updates in the browser with real Bluetooth traffic (the
  stream kinds and throttle are tested in Core; the pages' re-fetch on
  `bt_device` / `bt_status` reuses the existing, tested subscription hook).
