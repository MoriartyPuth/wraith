# Bluetooth dashboard

Status: **IMPLEMENTED — TESTED** (unit tests, typecheck, build, rendered in
headless Firefox against a real Core with pipeline-seeded data, in demo mode,
and with live data from the `bt` firmware build since BT-6).

Navigation (2026-11-16): a master **Overview** (`/`) on top, then a **Wi-Fi**
group (Wi-Fi overview at `/wifi` and the Wi-Fi pages), a **Bluetooth** group,
and a **System** group (Detection Center — findings from both radios —
Sensors, Event History, Settings). All earlier URLs are unchanged. The master
overview shows sensor/AP/Bluetooth/finding totals, each sensor's radio mode
and what it means for coverage, one summary card per radio linking to its
section, the latest findings tagged Wi-Fi or Bluetooth, and recent activity
from both radios.

**Radio coverage** (master overview; `GET /api/v1/coverage`): one bar per
sensor over 6 h / 24 h / 7 d showing Wi-Fi monitoring, Bluetooth (BLE
passive), Bluetooth + Classic inquiry (transmits) and *no data* (no heartbeat
for > 20 s: sensor offline, link down or Core stopped), with totals and
hover times. It is built from `bluetooth.system.status` reports and heartbeat
gaps (`core/internal/coverage`), so it shows when an absence of events from a
radio carries no meaning.

**Global search** (header, `/` to focus; `GET /api/v1/search?q=`): access
points by SSID or BSSID, clients by MAC, Bluetooth devices by name or
address; addresses match with any separator or none (`e40cfd`, `E4:0C`,
`e4-0c-fd`) and LIKE wildcards are literal. Up to 8 per kind, newest first;
arrows + Enter or click open the AP page, the Clients list filtered to that
MAC (`/clients?q=`), or the Bluetooth device page. Code: `dashboard/src/pages/bluetooth/`,
`dashboard/src/components/bluetooth/`, logic in `dashboard/src/utils/bluetooth.ts`.

| Route | Page |
|-------|------|
| `/bluetooth` | Overview: address totals (and seen in the last 10 min), Bluetooth error total, one card per sensor (radio mode, scan state, what the mode means for Wi-Fi, last error), signal-strength distribution, most recent addresses |
| `/bluetooth` | …plus **Near now** (devices heard in the last 5 min, strongest signal first) and **Breakdowns** (transport, BLE address type, BLE manufacturer, Classic device class, BLE classification; top 6 + Other), both from the loaded device list |
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
