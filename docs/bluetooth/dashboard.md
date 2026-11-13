# Bluetooth dashboard

Status: **IMPLEMENTED — TESTED** (unit tests, typecheck, build, rendered in
headless Firefox against a real Core with pipeline-seeded data and in demo
mode). No firmware produces Bluetooth events yet, so on a live system the
pages show their empty states until the Bluetooth firmware build exists.

A separate **Bluetooth** group in the navigation; the Wi-Fi pages are
unchanged. Code: `dashboard/src/pages/bluetooth/`,
`dashboard/src/components/bluetooth/`, logic in `dashboard/src/utils/bluetooth.ts`.

| Route | Page |
|-------|------|
| `/bluetooth` | Overview: address totals (and seen in the last 10 min), Bluetooth error total, one card per sensor (radio mode, scan state, what the mode means for Wi-Fi, last error), signal-strength distribution, most recent addresses |
| `/bluetooth/classic` | Classic devices: name, address, class (decoded from the device's own Class of Device), last RSSI, observations, first/last seen; search |
| `/bluetooth/ble` | BLE devices: name, address, address type, last RSSI, manufacturer, services, classification (marked inferred), advertisement count, first/last seen; search across name, address, manufacturer and service |
| `/bluetooth/devices/:id` | Device details: every field with its source, RSSI over time, GATT results (BLE), the address's events |
| `/bluetooth/events` | `bluetooth.*` event history with type groups and paging |

## Observed, derived, inferred, unavailable

The device page labels every value:

| Label | Meaning | Examples |
|-------|---------|----------|
| observed | reported by the device or sensor | name, address, Class of Device, manufacturer id, services, RSSI |
| derived | computed by WRAITH from observed data | Classic device class (decoded CoD), average RSSI |
| inferred | a guess | BLE classification, shown with the sensor's confidence |
| unavailable | never reported | shown as "unavailable" with the reason, never filled in |

Company and UUID names come from a small, deliberately partial table of
Bluetooth SIG assigned numbers; an unknown company id is shown as
"0xNNNN (unlisted company id)", never as a guessed vendor. Notes beside the
values repeat the limits that matter: any device can advertise any company
id, class or name, and a BLE random address may rotate.

## Radio mode (switch mode)

Each sensor card states what its mode means: in **Bluetooth** mode Wi-Fi
monitoring is paused on that sensor (warning colour); in **Wi-Fi** mode
Bluetooth is not being scanned and the Bluetooth data shown is from earlier
sessions; with no status at all the sensor runs the Wi-Fi-only firmware.

## Live updates

Pages subscribe to `bt_device` and `bt_status` and re-fetch (debounced). Core
throttles `bt_device` (docs/bluetooth/api.md), so a crowded BLE area cannot
flood the browser or crowd out Wi-Fi updates. Stored data is complete even
when a live update was skipped.

## Signal-strength chart

Devices per 10 dB bucket of last RSSI: one series, one hue (the dashboard's
accent token, as the Wi-Fi charts), no legend, hover tooltip per bar, and a
screen-reader table with the same numbers. Devices without an RSSI are
counted in the caption, not placed in a bucket. RSSI is labelled as not a
distance measurement.

## Demo mode

Demo mode includes a labelled Bluetooth dataset: `02:DE:…` locally
administered addresses, every name prefixed `Demo-`, the sensor back in Wi-Fi
mode after a Bluetooth session (so the switch-mode state is shown honestly),
and GATT results for one device. A unit test fails if a demo address or name
loses its marker.
