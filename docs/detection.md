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
