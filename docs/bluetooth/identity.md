# Bluetooth device identification and trackers

Core decodes what each BLE address **advertises** into an identity
(`core/internal/btident`). Nothing is guessed from the address, no address is
linked to another, and nothing transmits: the decoding runs on the adverts
the sensor already reports (`manufacturer_data`, ≤ 32 bytes, and service
UUIDs). No firmware change is involved.

## What is recognised

| Source | Shown as | Rank |
|--------|----------|------|
| Apple Continuity **Proximity Pairing** (`07`) | AirPods / Beats **model** + left/right/case **battery** and charging | 90 (known model), 70 |
| Cross-platform unwanted-tracking advert, service data `0xFCB2` | Item tracker away from its owner (any brand) | 88 |
| Tile `0xFEED` / `0xFEEC` / `0xFD84`, Chipolo `0xFE33` | Tile / Chipolo tracker | 85 |
| Apple **Find My** separated (`12 19…`) | Find My accessory away from its owner | 80 |
| Apple iBeacon (`02 15`) | iBeacon UUID / major / minor | 70 |
| Apple Magic Switch (`0B`) | Apple Watch | 65 |
| Apple AirPlay Target (`09`), HomeKit (`06`), Instant Hotspot source (`0E`) | AirPlay receiver; HomeKit accessory; iPhone or cellular iPad | 60 |
| Samsung `0xFD5A` (SmartThings Find) | SmartTag **or other Galaxy device** | 55 |
| Apple Handoff (`0C`), AirDrop (`05`); Google Fast Pair `0xFE2C`; Microsoft Swift Pair | iPhone, iPad or Mac; Fast Pair / Swift Pair accessory | 50 |
| Apple Nearby Info (`10`), Nearby Action (`0F`), AirPlay Source (`0A`), hotspot client (`0D`) | iPhone, iPad, Mac or Apple Watch (broad) | 45 |
| Apple Find My near owner (`12 02`) | Find My accessory near its owner | 40 |
| Google `0xFEAA` | Eddystone beacon or Find My Device accessory | 40 |
| Any other Apple manufacturer data | Apple device | 10 |

A device row keeps its **most specific** identity: an iPhone alternating
Nearby Info and Handoff stays "iPhone, iPad or Mac", and AirPods sending Find
My after Proximity Pairing stay "AirPods Pro". An equally specific message
refreshes it, which is how battery levels stay current. Service UUIDs were
checked against the Bluetooth SIG member-UUID registry.

**Not decoded, on purpose:** Nearby Info activity codes (screen on, driving,
on a call) and the AirPlay receiver's IP address. They describe what nearby
people are doing or where their devices are on their network.

**Limits.** Bluetooth cannot reliably tell iPhone from iPad: most Apple
messages only narrow it to "iPhone, iPad or Mac". AirPods report battery only
while the case is open or the buds are in use. Random addresses rotate, so one
device can appear as several rows; WRAITH does not link them.

## Names

"name it" works for Bluetooth rows as for Wi-Fi clients (same
`device_labels` table, by address). It sticks to addresses that do not rotate
(Classic, BLE public, trackers like Tile). A named tracker is treated as yours
and is not reported by `bt_tracker_lingering`.

## Trackers (detectors)

- `bt_tracker_separated`: an address broadcasting a **separated-from-owner**
  advert (Apple Find My `12 19…`, or the cross-platform `0xFCB2` alert) for
  ≥ 30 min with ≥ 3 reports.
- `bt_tracker_lingering`: a recognised tracker of **any** brand, not
  separated and **not named by you**, whose single address has been around
  for ≥ 24 h (first to latest sighting). Reported once per address, also
  across Core restarts. It mainly catches fixed-address trackers such as
  Tile. AirTags rotate their address about every 24 h, which restarts the
  clock, but they are covered by the separated detector.

See [../detection.md](../detection.md) for severities and limitations.

## Existing data

On start-up Core decodes, once, every BLE row stored before this existed,
from up to 200 of its stored advertisements (`bluetooth identity backfill` in
the log). On the first run here, 110 rows took 69 ms and 84 were identified.
Rows with no stored adverts (seen only through device records) fall back
to their stored company id and services, so an Apple one still reads "Apple
device" (migration 0009 re-ran the pass for those).

## API

Device rows (`/bluetooth/devices`, `/bluetooth/ble/devices`, device detail,
live `bt_device`) carry `identity` (`kind`, `label`, `source`, `rank`,
`tracker?`, `separated?`, `battery?`, `at`) or `null`, and `label`.
`GET /bluetooth/devices?kind=tracker` filters by kind: `phone_tablet_computer`,
`headphones`, `watch`, `tv_speaker`, `tracker`, `find_my`, `beacon`,
`smart_home`, `other`. Search matches your names.
