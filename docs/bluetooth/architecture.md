# Bluetooth subsystem — architecture and coexistence

Status: protocol v4, Core inventory ([event-protocol.md](event-protocol.md)),
REST/WebSocket API ([api.md](api.md)), dashboard ([dashboard.md](dashboard.md))
and the firmware **switch-mode foundation** (BT-5, below) exist. BT-5 is built
and host-tested but **not yet hardware-validated**; Bluetooth scanning itself
(BLE, Classic) is not implemented yet ([testing.md](testing.md)).

## Decision: one board, switch mode (2026-11-13)

WRAITH Nano is an ESP32-D0WD-V3 with **one 2.4 GHz transceiver and one
antenna** shared by Wi-Fi and Bluetooth. The Bluetooth subsystem therefore runs
in **switch mode**: the sensor's radio is in exactly one mode at a time.

| Mode | What runs | Wi-Fi behaviour |
|------|-----------|-----------------|
| `wifi` (boot default) | Today's passive Wi-Fi monitor, unchanged | Identical to the Wi-Fi-only build |
| `bluetooth` | Bluedroid dual-mode: BLE passive scan + bounded Classic inquiry | Wi-Fi stopped and de-initialised; its memory released |

Mode changes are explicit operator actions, reported as
`bluetooth.system.status` events and on the LCD. Wi-Fi is never degraded while
it is the active mode; it is cleanly stopped — not silently starved — when the
operator selects Bluetooth.

## Why not Wi-Fi and Bluetooth at the same time

1. **One radio.** Wi-Fi and Bluetooth time-share the transceiver through the
   ESP32 coexistence arbiter. Whenever Bluetooth holds the radio, the Wi-Fi
   receiver hears nothing.
2. **The arbiter cannot protect a sniffer.** Software coexistence is designed
   for Wi-Fi as a connected station (STA/AP), whose timing it can schedule
   around. WRAITH's monitor runs `WIFI_MODE_NULL` + promiscuous with channel
   hopping and wants the receiver on continuously; every slot given to
   Bluetooth is a window of missed frames, and sniffed frames are not
   retransmitted. Promiscuous-mode + Bluetooth coexistence is not a supported
   ESP-IDF configuration.
3. **Memory.** Free heap while Wi-Fi-monitoring is ~165 KB (docs/testing.md),
   and that figure includes Bluetooth controller RAM that ESP-IDF releases when
   Bluetooth is disabled. Bluedroid dual-mode needs on the order of 100+ KB on
   top of the controller. Wi-Fi and Bluedroid together do not fit; one at a
   time does.

Switch mode removes (2) and (3) because the two stacks never co-reside. The
remaining cost is (1): no simultaneous Wi-Fi + Bluetooth capture. A second
ESP32 dedicated to Bluetooth (Core already supports several sensors) would be
the way to get simultaneous coverage.

## Transmission note

**BLE passive scanning transmits nothing.** **Bluetooth Classic inquiry does**:
the inquiring device broadcasts inquiry packets and listens for responses.
Classic discovery is therefore active (it makes the sensor visible to the
devices it discovers) and will be bounded, scheduled and individually
disableable. GATT discovery connects to a device and is operator-initiated
only, off by default. No Bluetooth disruption, pairing, or characteristic
writes are in scope.

## Build gating

Bluetooth follows the TX feature's pattern: a separate PlatformIO environment
with `WRAITH_BT_ENABLED=1` (plus the Bluedroid sdkconfig and a larger app
partition). The default `wraith-nano` build stays Wi-Fi-only, protocol v3, and
byte-for-byte unaffected.

## Firmware: switch-mode foundation (BT-5)

`pio run -e bt` builds the Bluetooth firmware; the default (`pio run`) and
`tx` builds are unchanged — proven byte-identical in code and data
([testing.md](testing.md)).

| Piece | What it does |
|-------|--------------|
| `env:bt` (`platformio.ini`) | `WRAITH_BT_ENABLED=1`, protocol v4, `sdkconfig.bt.defaults` layered over `sdkconfig.defaults`, `partitions_bt.csv`, 4 MB flash. No deauth TX in this build. |
| `sdkconfig.bt.defaults` | Bluedroid + dual-mode (BTDM) controller, Classic + BLE; no GATT server, no SMP/pairing, no Classic profiles (A2DP/SPP/HFP/L2CAP); GATT client kept for later operator-initiated discovery; 1 ACL / 1 BLE connection max. |
| `partitions_bt.csv` | Same nvs/phy_init offsets as the stock single-app table; factory app 3 MB (the image is ~1.25 MB and does not fit the 1 MB default). |
| `radio_mode_core.c` | Pure state machine for `MODE WIFI` / `MODE BLUETOOTH` / `MODE STATUS` (host-tested). |
| `radio_mode.c` | Runs the state machine on its own task (core 1, 100 ms tick), wires it to the real radio, updates `sensor.status`. |
| `bt_manager.c` | Controller init/enable (BTDM) → `esp_bluedroid_init_with_cfg` → enable → **non-connectable, non-discoverable** Classic scan mode; teardown in reverse, tolerant of a partial start. |
| `wifi_monitor_stop()` | Ends the processing task between frames, leaves promiscuous mode, `esp_wifi_stop` + `esp_wifi_deinit` (frees the driver's memory), deletes the ring buffer. Restart = the existing `wifi_monitor_init()` + `wifi_monitor_start()`. |

**Switch sequence.** To Bluetooth: stop Wi-Fi completely, then start
Bluetooth. To Wi-Fi: stop Bluetooth completely, then start Wi-Fi. The two
stacks never run at the same time.

**Wi-Fi stays the baseline.**
- Bluetooth mode returns to Wi-Fi when the host link is lost (no `PING` for
  6 s) and after `WRAITH_BT_SESSION_MAX_MS` (15 min by default).
- A failed switch falls back to Wi-Fi (Bluetooth start fails → it is torn
  down and Wi-Fi restarted; Wi-Fi refuses to stop → it is kept running).
- If Wi-Fi fails to come back, the sensor reports `state: "error"` and
  retries every 5 s.
- Every step is reported: `bluetooth.system.status` on each transition and
  command, `bluetooth.system.error` with a code (`bad_command`,
  `wifi_stop_failed`, `bt_start_failed`, `bt_stop_failed`,
  `wifi_start_failed`, `radio_task_failed`).

**How the Wi-Fi files were touched.** Only additive hooks, in a way that
cannot change the Wi-Fi builds: `ESP_RETURN_ON_ERROR`/`ESP_ERROR_CHECK` embed
`__LINE__` in the machine code, so no line of `wifi_monitor.c`, `host_link.c`
or `main.c` moved. Three existing lines gained a macro (`for (;;)` →
`for (; WIFI_MON_PROC_CONTINUE();)`, the loop end, the task start; the
host-line handler; a post-boot hook) that expands to nothing in the Wi-Fi
builds; the stop function is appended after the last existing line. CMake
filters the Bluetooth sources out of the Wi-Fi builds.

**Trade-offs in the Bluetooth build (not in the Wi-Fi builds):**
- *IRAM.* Wi-Fi + Bluedroid overflow the ESP32's instruction RAM (by 1448
  bytes). The build sets `CONFIG_ESP_WIFI_IRAM_OPT=n` (general Wi-Fi hot
  paths run from flash — ESP-IDF's own default for ESP32+BT+PSRAM); the RX
  path a sniffer depends on keeps `ESP_WIFI_RX_IRAM_OPT`. Possible effect on
  Wi-Fi capture rate in this build's Wi-Fi mode: to be measured on hardware.
- *Heap.* Bluetooth controller memory is not released at boot (switch mode
  must be able to start it later), so this build's Wi-Fi mode has less free
  heap than the Wi-Fi build. To be measured on hardware (heartbeat
  `free_heap`; each switch logs the heap in `sensor.status`).

**Not yet:** BLE scanning, Classic inquiry (BT-6, BT-7), so Bluetooth mode
reports `state: "idle"`; LCD Bluetooth screens (BT-11) — in Bluetooth mode
the LCD still shows the Wi-Fi screens (channel 0).

## Integration points (no Wi-Fi file changes)

| Layer | Plan |
|-------|------|
| Firmware | New `bluetooth_*` modules; events go through the existing `transport_send` message buffer; radio-mode switch reuses the host-link command path. `transport`, `host_link`, `system_status` and the LCD task keep running across a mode switch. |
| Protocol | v4 = v3 + `bluetooth.*` (done) |
| Core | `core/internal/bluetooth` + migration `0005_bluetooth` (devices, observations, per-sensor status); one call from ingest |
| API / WebSocket | `/api/v1/bluetooth/*` on the existing router and host guard; `bt_device` (throttled) and `bt_status` on the existing hub ([api.md](api.md)) |
| Dashboard | Bluetooth navigation group and pages ([dashboard.md](dashboard.md)); Wi-Fi pages untouched |

Files that must not change for Bluetooth: `wifi_monitor.c`, `beacon_parser.c`,
`management_monitor.c`, `frame_parser.c`, `ap_tracker.c`, `client_tracker.c`,
`tx_test.c`, and the Wi-Fi detectors. Any needed hook (e.g. a clean
`wifi_monitor_stop()` for the mode switch) will be added additively and called
out explicitly.
