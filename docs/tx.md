# Deauth test transmission (WRAITH TX)

Status: **IMPLEMENTED — TESTED + HARDWARE VALIDATED** (host tests, protocol
check and unit tests pass; first on-air burst confirmed 2026-11-12: a 5-frame
deauth burst to an owned client produced `tx.report sent=5/requested=5
complete` and a brief, observable client disconnect).

WRAITH can send a small, rate-limited burst of deauthentication or
disassociation frames from WRAITH Nano's radio, for **testing your own
defences**: e.g. confirm your IDS sees the frames, or that a client behaves
the way you expect when management frames are spoofed. This is an
*active* feature — the only one in WRAITH — and it is gated at every layer.

## Authorization

Run this only against networks you own or have written permission to test.
Sending deauthentication frames to networks you are not authorized for is
illegal in most jurisdictions (e.g. computer-misuse and telecom-interference
laws), regardless of intent. WRAITH deliberately does not make this easy:

- firmware: the feature is **not compiled in** unless `WRAITH_TX_ENABLED=1`;
- Core: the API is **not exposed** unless Core runs with `-tx` (or
  `tx_enabled: true` in config, or env `WRAITH_TX_ENABLED`);
- the API request must carry `"confirm": true`;
- the target must be a BSSID WRAITH has actually observed, on a 2.4 GHz
  channel (so the radio can be tuned to it);
- the sensor re-validates everything and applies hard limits.

## What it sends

A burst of identical 802.11 management frames (26 bytes before the FCS the
ESP32 appends):

| Frame | Type/Subtype | Addressing |
|-------|--------------|------------|
| deauth  | `0xC0 0x00` | addr1 = client, addr2 = addr3 = BSSID |
| disassoc| `0xA0 0x00` | same |

BSSID and client MAC are validated (unicast client only; broadcast
"kick-everyone" bursts are not offered through the API). The reason code is
configurable and defaults to 7 ("Class 3 frame received from nonassociated
STA"). Frames are transmitted with `esp_wifi_80211_tx(WIFI_IF_STA, …)`
while the radio is pinned to the target AP's last observed channel for the
duration of the burst (no monitor hop during the burst, forced re-hop
afterwards).

Two driver details matter on real hardware (discovered during validation):

- the interface must be in an **unconfigured STA mode** (`WIFI_MODE_STA`, no
  station config, promiscuous on), not `WIFI_MODE_NULL` — with NULL mode the
  driver returns `ESP_ERR_INVALID_ARG` and no frame leaves the radio;
- stock ESP-IDF rejects deauth/disassoc frames from `esp_wifi_80211_tx` via
  its internal `ieee80211_raw_frame_sanity_check()` (`wifi: unsupport frame
  type: 0c0`). The TX build links `firmware/src/tx_raw_override.c` (with
  `-Wl,--allow-multiple-definition`) to allow exactly these frames. The
  default build does not compile it. Only WRAITH's validated deauth/disassoc
  path can reach the radio.

The sensor does **not** hear its own transmissions (the driver does not loop
raw TX back into promiscuous RX); confirmed on the first hardware burst.

## Limits (mirrored in firmware and Core)

| Limit | Value |
|-------|-------|
| Frames per burst | ≤ 10 (default 1) |
| Frame spacing | ≥ 100 ms, ≤ 10000 ms |
| Cooldown between bursts | 30 s by default; **0 in the `tx` PlatformIO env** (continuous testing). Override with `-D WRAITH_TX_COOLDOWN_MS=…` (a burst already running is still rejected). |
| Broadcast client | never allowed |
| Burst aborted when | host link lost, radio error, or burst complete |

Every burst abort and completion is reported as a `tx.report` event, so the
dashboard shows what actually happened — WRAITH never claims a result it
cannot evidence.

## Request path

```
Dashboard APDetail ── POST /api/v1/tx ──▶ WRAITH Core ── "TX_DEAUTH …" line ──▶ WRAITH Nano
                 ◀── 202 accepted ──                             ◀── tx.ack / tx.report events ──
```

### HTTP: `POST /api/v1/tx`

Requires `Content-Type: application/json` (that requirement doubles as the
CSRF guard: cross-site forms cannot set it). Body:

```json
{
  "kind": "deauth",            // "deauth" | "disassoc"
  "bssid": "AA:BB:CC:DD:EE:01",
  "client": "11:22:33:44:55:66",
  "reason": 7,                 // optional, 0–65535, default 7
  "count": 1,                  // optional, 1–10, default 1
  "interval_ms": 500,          // optional, 100–10000, default 100
  "confirm": true              // required
}
```

Stop a burst in progress: `POST /api/v1/tx/stop` (same gates: `-tx`, JSON
content type, connected sensor). It sends `TX_STOP`; the sensor aborts after
the current frame and emits a `tx.ack` (`command: "status"`) with
`detail` such as `burst aborted by operator; sent 3 of 10`. Harmless when no
burst is running (`no burst running`).

Responses for `/tx`: `202` accepted (echo of parameters + channel), `400` validation
(including missing `confirm`), `403 tx_disabled` (Core started without
`-tx`), `409 target_not_observed` / `target_channel_unknown` /
`sensor_not_connected`, `502 tx_send_failed` (link write failed). The sensor's
acknowledgement arrives asynchronously as a `tx.ack` event (`accepted` /
`rejected` + detail).

### Serial: host → device command lines

Core sends one ASCII line per request (the sensor ignores everything that is
not one of these, and only when compiled with `WRAITH_TX_ENABLED=1`):

```
TX_DEAUTH   <bssid> <client> <count> <interval_ms> <reason> <channel>
TX_DISASSOC <bssid> <client> <count> <interval_ms> <reason> <channel>
TX_STATUS
TX_STOP        abort a running burst (acks how many frames were sent)
```

Example: `TX_DEAUTH AA:BB:CC:DD:EE:01 11:22:33:44:55:66 1 500 7 6`.

The sensor replies on the event stream with `tx.ack` (accepted/rejected) and,
when a burst runs, `tx.report` (final count and outcome:
`complete`, `aborted_host_lost`, `aborted_radio`).

## New protocol v2 event types

Protocol bumped to **v2** (additive; Core accepts v1–v4 side by side — v3 later
added `wifi.client_observed`, see docs/protocol.md):

```
tx.ack      metadata: command ("deauth"|"disassoc"|"status"),
                      status ("accepted"|"rejected"),
                      detail? ≤ 128, client? MAC     (envelope bssid optional)
tx.report   metadata: command, client MAC, reason, sent ≥ 0, requested ≥ 1,
                      outcome ("complete"|"aborted_host_lost"|"aborted_radio")
                                                   (envelope bssid + channel required)
```

Schemas: `protocol/schemas/event.v2.schema.json`; fixtures under
`protocol/fixtures/{valid,invalid}/tx_*.json`.

## Enabling it

```bash
# Firmware (only this build can transmit):
cd firmware && pio run -e tx -t upload   # env 'tx' sets WRAITH_TX_ENABLED=1
# Core (only this config exposes the API):
wraith -tx ...            # or: WRAITH_TX_ENABLED=1, or config "tx_enabled": true
```

Check readiness: `/api/v1/system/info` → `"tx_enabled": true`, and the sensor
status on `/api/v1/sensors` shows the `deauth-tx` capability. The dashboard
shows a "Deauth test transmission" card on an access point's page only when
Core has TX enabled *and* a sensor is connected.

The card can also **repeat a burst automatically** ("Repeat bursts"): it
re-sends at whatever interval you set. Repeating is client-side — it stops
when you press "Stop repeating" or leave the page. The sensor's per-burst
limits still apply: a request is rejected while a previous burst is still
running, and if the firmware keeps a cooldown (the `tx` env sets it to 0) a
too-fast repeat yields a rejected `tx.ack`. Each burst is still capped and
spaced.

## Observing the result

Filter the event history to `tx.ack` / `tx.report` (the dashboard has filter
chips for both), or watch the live feed. Pair with `wifi.deauth_observed`
events from a *second* sensor or from the host's own IDS to confirm frames
were seen on air. A burst that completes only proves the radio sent frames —
not that any client was affected.

## Implementation map

| Layer | File |
|-------|------|
| Firmware TX engine | `firmware/src/tx_test.c`, `firmware/include/tx_test.h` |
| Firmware gating | `firmware/include/wraith_config.h` (`WRAITH_TX_ENABLED`) |
| Radio ops + burst tick | `firmware/src/wifi_monitor.c` (`tx_set_channel`, `tx_send_frame`, proc-task hook) |
| Command parsing | `firmware/src/host_link.c` |
| Protocol v2 + events | `protocol/schemas/event.v2.schema.json`, `core/internal/events` |
| Commander interface | `core/internal/capture/provider.go`, `core/internal/capture/serial/serial.go` |
| HTTP endpoint | `core/internal/api/api.go` (`txSend`) |
| Dashboard card | `dashboard/src/components/TxCard.tsx` |
| Host tests | `firmware/test/host/test_tx.c` |
