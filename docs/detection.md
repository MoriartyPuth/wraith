# Detection

Status: **IMPLEMENTED — TESTED** (unit + pipeline integration tests) for the
detectors marked ✅ below. No finding has been triggered on hardware yet
(nothing suspicious occurred during tests). Code: `core/internal/detection`.

## Classification ladder

| Level | Meaning |
|-------|---------|
| `observed` | A frame or configuration was seen. No judgment. |
| `suspicious` | Pattern unusual for this environment's baseline. |
| `potential_attack` | Pattern consistent with a known attack and inconsistent with benign causes we can check. |
| `confirmed` | Analyst-confirmed only. Detectors never emit this. |

Every finding carries: id, category, severity, confidence (0–1),
classification, BSSID, source sensor, evidence (event IDs), explanation, and the
limitations that apply.

## Implemented detectors

| ✅ | Category | Trigger | Classification / severity |
|----|----------|---------|---------------------------|
| ✅ | `deauth_activity` | ≥ 30 deauth+disassoc frames for one BSSID within 60 s (sum of Nano's 1 s aggregates) | suspicious / medium (conf 0.5); ≥ 100 → high (0.6); ≥ 100 **and** AP advertises PMF required **and** frames unprotected → potential_attack / high (0.7). One finding per BSSID per 5 min. |
| ✅ | `security_downgrade` | `ap_observed` with weaker auth mode than before (rank: open < wep < owe/wpa-psk < … < wpa3-sae < wpa3-eap-192) | suspicious / medium; to open or WEP → high (0.6). Upgrades are not reported. |
| ✅ | `pmf_downgrade` | advertised PMF required → capable/not_advertised, or capable → not_advertised | suspicious / medium (0.6). `unavailable` is never compared. |
| ✅ | `ap_identity_change` | same BSSID, different SSID bytes | observed / low (0.9) |
| ✅ | `rogue_ap_indicator` | when a BSSID's auth mode becomes known, any BSSID with the exact same SSID bytes that advertises **weaker** security than a sibling (checked both ways) | suspicious; one rank weaker (e.g. WPA/WPA2-mixed vs WPA2) → low (0.3); ≥ 2 ranks → medium (0.4); open/WEP twin → high (0.5). Same-security siblings (mesh) and hidden SSIDs are never flagged. |
| ✅ | `pmf_invalid_advertisement` | RSN with MFPR=1, MFPC=0 | observed / info (0.9), once per BSSID per run |
| — | `mgmt_burst` (non-deauth), `signal_anomaly`, channel anomalies | | NOT IMPLEMENTED |

First real-traffic finding (2026-11-11): two BSSIDs of a neighbouring network
named identically, one WPA2-only and one WPA/WPA2-mixed → `low` indicator. The
original rule missed it because a beacon report created the AP row before the
auth mode was known; the rule now triggers on "auth mode became known".

The rogue-AP detector waits out a 90 s warm-up after Core starts before
evaluating any AP, and evaluates each BSSID exactly once afterwards, so it
compares against a populated inventory rather than one still filling from
beacon reports and the 60 s identity refresh. This removes a class of
startup false positives (a legitimate multi-BSSID network flagged before all
its siblings were seen). Comprehensive false-positive tuning is deferred to
the Phase 6 soak review, where real-traffic data drives threshold choices.

Findings are also de-duplicated at the database level: a new finding with the same category and BSSID as one stored within the last 6 h is suppressed (the `findings_deduped` runtime stat). This keeps one record of an ongoing condition across Core restarts, which reset the detectors' in-memory state.

Detector state (windows, cooldowns, warm-up) is in memory and resets when Core
restarts. All thresholds are constants in the code; they are starting points
and need tuning against real environments (Phase 6 false-positive evaluation).

## Bluetooth detectors (BT-10)

Status: **IMPLEMENTED — TESTED** (unit, pipeline integration, mutation
checks). Live check 2026-11-15: a 10-minute BLE + Classic session on real
traffic produced no Bluetooth finding (no false positives; none expected). Code:
`core/internal/detection/bluetooth.go`. A finding about one Bluetooth device
carries `bt_device` (the `bt_devices` row id, e.g. `classic:AA:BB:…`,
`ble:random:AA:BB:…`; migration `0006_bt_findings`) instead of a BSSID and
links to the device page; the 6 h database de-duplication keys on category +
BSSID + `bt_device`.

An address is an observation, not a device: BLE random-address rotation is
never a finding on its own, and no rule merges addresses.

| ✅ | Category | Trigger | Classification / severity |
|----|----------|---------|---------------------------|
| ✅ | `ble_advertising_flood` | ≥ 30 **new** BLE addresses (`ble.device_discovered`) at one sensor within 60 s | suspicious / medium (0.5). If ≥ 10 of those addresses sent **pairing pop-up** adverts — Apple Continuity Proximity Pairing (`004C`, type `07`) or Nearby Action (`0F`), Microsoft Swift Pair (`0006`, `03 00 80`), Google Fast Pair (service `fe2c`) — → potential_attack / high (0.7): the BLE pop-up-spam pattern. One per sensor per 10 min. |
| ✅ | `bt_tracker_separated` | a BLE address broadcasting Apple Find My's **separated-from-owner** advert (`004C`, `12 19…`) reported ≥ 3 times over ≥ 30 min (gaps of up to 2 h tolerated: Bluetooth sessions last ≤ 15 min). The near-owner form (`12 02…`, common on real data) is ignored. | suspicious / medium (0.5), once per address. Tile / SmartTag / Google Find My Device trackers are not recognised. |
| ✅ | `bt_identity_change` | a stable address (Classic, or BLE `public`) changes its **name** (both known; one being a prefix of the other — shortened vs complete name — is ignored) or its Classic **device class** | name: observed / low (0.6); device class: suspicious / low (0.4). Once per device and kind per hour. |

Baseline from the BT-6/BT-7 hardware runs: 15–24 BLE addresses over 8
minutes in a residential setting, so 30 new per minute is far above it; busy
public places were not measured and may need a higher threshold. Each finding
states its limitations (crowds and address rotation for floods; neighbours'
items for trackers; renames and spoofing for identity changes).

## Design notes (original contract)

| Category | Evidence | Benign explanations it must account for |
|----------|----------|------------------------------------------|
| `deauth_activity` | count/window per BSSID, broadcast vs unicast, reason codes, `protected` bit | AP reboot, roaming, client leaving, band steering, idle timeouts |
| `disassoc_activity` | same | same |
| `mgmt_burst` | rate vs per-BSSID baseline | busy networks, sensor channel dwell bias |
| `rogue_ap_indicator` | same SSID (by `ssid_hex`) with new BSSID + different security/vendor OUI | mesh / multi-AP networks, repeaters, guest networks |
| `ap_identity_change` | BSSID's SSID, channel or capabilities change | firmware updates, DFS channel changes |
| `security_downgrade` | RSN removed, AKM/cipher weakened, MFPR dropped | admin reconfiguration |
| `pmf_observation` | MFPC/MFPR advertised; MFPR without MFPC flagged as malformed | — (informational) |
| `signal_anomaly` | sudden RSSI jump for fixed AP across sensors | sensor moved, obstruction |

Unprotected deauth frames with `protected: false` from an AP that advertises
MFPR is a stronger indicator (a PMF-required AP should send protected
deauths), but still not proof: the ESP32 may mis-capture, and clients without
PMF may be legitimately deauthenticated in mixed (MFPC-only) networks.

## Limitations to state in every explanation

- A single 2.4 GHz sensor hopping channels sees a fraction of frames.
- Transmitter addresses in management frames are trivially spoofed; a "from the
  AP" deauth is only *addressed* from the AP.
- AP-advertised PMF says nothing about what a client negotiated.
