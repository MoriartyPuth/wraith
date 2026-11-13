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

## Hardware: switch mode (BT-5, 2026-11-13, nano-70b2a8)

Board ESP32-D0WD-V3 rev 3.1, Core with protocol v4 / schema 5, live 2.4 GHz
environment (11–13 APs, ~28 clients). Measured through Core's stored events
(`/api/v1/events`).

**Wi-Fi capture: Bluetooth build (Wi-Fi mode) vs the build it replaced (tx)**

| | tx build (10 min) | bt build, Wi-Fi mode (5 min) |
|---|---|---|
| frames received / min | 850 | 920 |
| Wi-Fi events / min | 101.6 | 109.5 |
| ring-buffer drops, queue drops | 0, 0 | 0, 0 |
| sequence gaps | 0 | 0 |
| free heap | 154.8 KB | **87.2 KB** (flat) |

No capture loss is measurable from the IRAM trade-off (the windows are not
simultaneous, so the 850 vs 920 difference is the environment, not the
firmware). The real cost is heap: the reserved Bluetooth controller memory
takes 67.6 KB in Wi-Fi mode.

**Switching (9 cycles over 3 boots, 20–30 s in Bluetooth each)**

| | result |
|---|---|
| Wi-Fi → Bluetooth (command to confirming status) | 1.73–2.42 s |
| Bluetooth → Wi-Fi | 1.35–1.73 s |
| first Wi-Fi event after the Wi-Fi command | 1.33–1.54 s |
| Wi-Fi events while in Bluetooth mode | 0 in every cycle |
| errors, resets (boot id unchanged) | none |
| free heap, Bluetooth mode | 117.0–117.7 KB |
| free heap after returning to Wi-Fi | identical every cycle within a boot (87,092 / 86,840 B): no leak |
| lowest free heap since boot | 80,192 B (constant across cycles) |
| radio task stack (runs Bluedroid init/deinit) | ≥ 3,608 of 6,144 B always free |

**Host-link loss.** With the sensor in Bluetooth mode, Core was stopped for
25 s: the sensor returned to Wi-Fi by itself without a reset, Wi-Fi events
flowed as soon as Core was back, and Core showed the correct mode with no
command sent. Events emitted while Core was down are counted as sequence
gaps (24), as designed.

**Defects found on hardware and fixed (all re-verified on the board):**
1. `system_status` silently ignored capabilities beyond 8, dropping
   `radio-switch`, so Core refused mode commands. The limit is now
   `WRAITH_MAX_CAPS` (8 in the Wi-Fi builds — objects unchanged — 12 in the
   Bluetooth build), and `radio-switch` is registered first.
2. Heartbeats kept the last Wi-Fi channel in Bluetooth mode (they read
   `system_status`' copy). `wifi_monitor_stop()` now clears it.
3. Core showed a stale mode after a reconnect: the sensor's status about the
   automatic return to Wi-Fi was emitted while Core was down. The state
   machine now re-announces the mode on every host (re)connect (host test
   added and mutation-checked); this also delivers the boot announcement when
   Core attaches late.
4. Measurements added to the switch status message: `heap`, `min` (lowest
   since boot), `stack` (radio task high-water mark).

Not exercised on hardware: the 15-minute session limit (host-tested,
including the 32-bit millisecond wrap); the LCD in Bluetooth mode (shows the
Wi-Fi screens with no channel until BT-11).

## BLE passive scanning (BT-6)

Host (`firmware/test/host/test_ble.c`, ASan/UBSan): parser on real frame
layouts (iBeacon, Eddystone service data, Nordic UART 128-bit UUID, 32-bit
UUID), padding, a length past the payload, name precedence, duplicate and
excess UUIDs, odd-length lists, capped manufacturer data, TX power 0x80,
NULL/empty input; every prefix of a full payload in an exact-size heap buffer;
20 000 random payloads. Tracker: discovery and windowed reports, the additive
count contract (sum of emitted counts = adverts received), address types
never merged, RSSI clamping, unknown PDU types, sticky classification and
`device_updated`, a 40-address burst under the serial budget (nothing lost),
eviction accounting, expiry and rediscovery, flush before stop. Mutation
checks: parser bounds check, first-advert double count, serial budget,
address-type merging — each caught. Every BLE event the firmware emits
passes the Go validator (this caught TX power −128 being out of range; fixed).

**Hardware (2026-11-14, nano-70b2a8, real environment):**

| scan duty | adverts received / 90 s | rate | controller discards, qdrop, deferred, seq gaps |
|-----------|------------------------|------|------------------------------------------------|
| 60 % (window 30 of 50 ms) | 230 | 2.56/s | 0, 0, 0, 0 |
| 100 % (window = interval) | 1 492 | **16.58/s** | 0, 0, 0, 0 |

Partial-duty scanning lost far more than its idle share (6.5× fewer adverts
for 1.7× less airtime); the default is continuous. In every run the counts
reconcile exactly: adverts received − qdrop − evicted = the observations
Core stored (35 = 35, 230 = 230, 1 492 = 1 492). 9–17 addresses per run,
including Apple (0x004C), Microsoft (0x0006) and Samsung (0x0075)
manufacturer data and names; free heap while scanning 102 KB; no resets;
Wi-Fi events: none after the Bluetooth confirmation; Wi-Fi capture resumed
within ~1.5 s of switching back. No beacon/wearable signal was present, so
classification was not exercised on hardware (host-tested only).

## Not tested yet

- Classic inquiry (BT-7), and BLE + Classic scheduled together.
- BLE classification on real beacons/wearables (no such device was nearby).
- Stack high-water marks of the Bluedroid-internal tasks (only the radio
  task's is reported).
- Live WebSocket updates in the browser with real Bluetooth traffic (the
  stream kinds and throttle are tested in Core; the pages' re-fetch on
  `bt_device` / `bt_status` reuses the existing, tested subscription hook).
