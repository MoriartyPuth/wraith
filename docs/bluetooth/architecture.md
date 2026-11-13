# Bluetooth subsystem — architecture and coexistence

Status: **DESIGN, PROTOCOL AND CORE INVENTORY.** Protocol v4, Core validation
and the SQLite device inventory exist ([event-protocol.md](event-protocol.md));
no Bluetooth firmware, API or dashboard yet. Nothing here is hardware-validated.

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

## Integration points (no Wi-Fi file changes)

| Layer | Plan |
|-------|------|
| Firmware | New `bluetooth_*` modules; events go through the existing `transport_send` message buffer; radio-mode switch reuses the host-link command path. `transport`, `host_link`, `system_status` and the LCD task keep running across a mode switch. |
| Protocol | v4 = v3 + `bluetooth.*` (done) |
| Core | `core/internal/bluetooth` + migration `0005_bluetooth` (devices, observations, per-sensor status); one call from ingest |
| API / WebSocket | New `/api/v1/bluetooth/*` routes on the existing router and host guard; existing hub |
| Dashboard | New Bluetooth section; Wi-Fi pages untouched |

Files that must not change for Bluetooth: `wifi_monitor.c`, `beacon_parser.c`,
`management_monitor.c`, `frame_parser.c`, `ap_tracker.c`, `client_tracker.c`,
`tx_test.c`, and the Wi-Fi detectors. Any needed hook (e.g. a clean
`wifi_monitor_stop()` for the mode switch) will be added additively and called
out explicitly.
