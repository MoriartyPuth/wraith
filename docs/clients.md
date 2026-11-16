# Wi-Fi clients: what WRAITH can and cannot say about a device

WRAITH Nano lists a **client** when it hears an 802.11 data frame between that
station and an access point (protocol v3, `wifi.client_observed`). Only the MAC
header is read; payloads stay encrypted and are never decoded.

## What is shown

| Field | Source | Status |
|-------|--------|--------|
| Address, AP, channel, RSSI, frames, first/last seen | data-frame headers | observed |
| **Active** (dashboard) | heard in the last 10 min (`client_window_ms`) | observed |
| **Private** | locally administered bit of the address (randomised MAC) | derived |
| **Maker** (`vendor`, `vendor_full`) | IEEE MA-L registry lookup of the first 3 bytes | derived |
| **Device guess** (`device_hint`) | fixed maker → device-type table (`core/internal/devid`) | inferred, a guess |
| **Name** (`label`) | set by you, stored in Core (`device_labels`) | operator-supplied |

## Why "iPhone" or "iPad" is not shown

- Device names (DHCP host name, mDNS, "Beau's iPhone") travel **inside
  encrypted traffic**, which WRAITH does not and cannot read.
- iPhones, iPads, Android phones and recent laptops use **private
  (randomised) addresses** per network by default. These carry no maker, so
  they appear as "Private address".
- Even a real Apple address only says "Apple". The registry does not
  distinguish iPhone from iPad or Mac.

The reliable way to identify **your own** devices is to name them: compare
the address in the phone's Wi-Fi settings (iOS: Settings → Wi-Fi → (i) →
"Private Wi-Fi Address"; Android: Wi-Fi → network → "MAC address") with the
Clients page and use **name it**. A label is per address, so if a phone rotates
its private address (iOS "Rotating", Android per-connection), the old label
stops matching and the new address appears unnamed.

## Maker database

`core/internal/devid/oui.tsv.gz` (≈ 40 000 prefixes, embedded in the Core
binary, no network lookups) is generated from the IEEE MA-L registry by
`scripts/gen-oui.sh` (default source: Fedora `hwdata` `/usr/share/hwdata/oui.txt`;
pass an IEEE `oui.txt` download to use another copy). Re-run it to refresh.
Only 24-bit MA-L blocks are covered; addresses from MA-M/MA-S blocks show the
large-block owner or nothing.

## API

- Client rows (`GET /clients`, `GET /access-points/{bssid}/clients`, live
  `client` stream messages) carry `vendor`, `vendor_full`, `device_hint`,
  `private`, `label`.
- `GET /device-labels` → `{"data":[{"address","label","updated_at"}]}`
- `PUT /device-labels/{address}` with `Content-Type: application/json` and
  `{"label":"..."}` (trimmed, 1–48 characters, no control characters) → 200.
- `DELETE /device-labels/{address}` → 204, or 404 if there was no label.
- `GET /search` also matches client labels and shows the maker.

Labels are user data: never pruned by retention, kept in the Core database.
