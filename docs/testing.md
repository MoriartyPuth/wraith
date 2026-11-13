# Testing

Run everything that does not need hardware:

```bash
scripts/check-all.sh
```

| Check | Command | Covers |
|-------|---------|--------|
| Go unit + integration | `cd core && go test -race ./...` | validator vs fixtures, framing (1-byte reads, oversize, partial), dedup/gaps/restarts, config precedence, SQLite migrations + pagination, all REST endpoints, error format, DNS-rebinding guard, `iw` parsing (real MT7922 output) |
| Firmware host tests | `scripts/firmware-host-tests.sh` | JSON escaping, UTF-8 validation, overflow latching, identity, heartbeat; 802.11 header parsing incl. every truncated length; beacon/RSN parsing for open/WEP/WPA/WPA2/WPA3/transition/OWE, hidden and oversized SSIDs, malformed RSN, MFPR-without-MFPC; every prefix of a beacon in exact-size heap buffers; 20 000 random bodies; AP tracker rate limiting, change detection, eviction; deauth/disassoc windows, reason codes, protected bucket, table overflow |
| Protocol cross-check | `scripts/check-protocol.sh` | firmware C serializer output → Go validator; all valid fixtures accepted; all invalid fixtures rejected (`--strict`) |
| Firmware build | `cd firmware && pio run` | ESP-IDF 6.1.0 build, -Wall -Wextra clean |
| Dashboard | `cd dashboard && npm test && npm run typecheck && npm run build` | unit tests, types, production bundle |

Firmware host tests run under ASan/UBSan (gcc if `libasan`/`libubsan` are
installed, otherwise clang). Mutation check (2026-11-11): swapping the
MFPC/MFPR bits fails 4 checks; removing the IE bounds check triggers an ASan
heap-buffer-overflow in the truncation sweep.

## Hardware smoke test (2026-11-11, nano-70b2a8)

```bash
scripts/nano-capture.py /dev/ttyUSB0 22 > nano.raw   # resets board via RTS, captures
cd core && go run ./cmd/wraith validate nano.raw
```

Result: `valid=6 invalid=0 non-protocol=231` — status `booting` → `degraded`
(Wi-Fi monitor not implemented yet), heartbeats every 5000 ms, seq 0–5 with no
gaps, free heap steady at 277 088 B, `queue_drops` 0. ESP-IDF's console emits
CRLF line endings; Core strips the `\r`.

Observed: on opening the port, ~3 KB of repeated line fragments
(`eue_drops":0}}`) arrived *before* the reset, i.e. stale data buffered in
the USB-serial path while no reader was attached. Core's framing discards them
as non-protocol lines. Root cause not investigated (CP2102 / cp210x driver).
`nano-capture.py` is a development helper; Python is not a Core runtime dependency.

## Phase 2 hardware test (2026-11-11, nano-70b2a8, 65–70 s captures)

`valid=103 invalid=0`: 10 APs on channels 1/4/6/8/11 (wpa2-psk ×6,
wpa-wpa2-psk ×4, 1 hidden, all PMF `not_advertised`), 65 aggregated beacon
reports, seq contiguous, `queue_drops` 0, ring-buffer `drop` 0, `rx=767`
frames/min, free heap flat at 165 KB, all 13 channels visited.
In one capture the ROM boot banner arrived garbled into a 2.5 KB line and was
discarded as oversize by Core's framing (USB-serial artifact around reset).

## Phase 3 end-to-end (2026-11-11, nano-70b2a8 → Core → SQLite → WebSocket)

Core started ~20 s after the board booted (late attach), 80 s run:
12 APs, all with auth mode and PMF (identity refresh), 123 events ingested,
0 duplicates, 0 sequence gaps, 0 ingest failures, 0 rejected for the sensor;
sensor `connected: true`; `wraith tail` received the live stream; SIGINT
shut down in the documented order. Opening the port does not reset the board.
One 2968-byte garbled burst arrived right after opening the port despite the
input flush and was rejected as oversize (unattributable, provider stats only).

Timing-sensitive tests (`capture/serial`, `stream`, `ingest`) pass 10/10 runs
under `-race`.

## Phase 4 (2026-11-11)

- Dashboard unit tests: `cd dashboard && npm test` (Vitest: CSV quoting and
  formula-injection guard, event descriptions, reason codes).
- Visual check: pages rendered in headless Firefox driven over WebDriver BiDi
  (waits for data, full-page capture), in demo mode and on live board data.
- Serial link: after 60 s idle, reopening produced 7 534 replayed lines; with
  the grace period the provider stayed `running`, ingested valid events
  throughout, 0 checksum failures, 0 gaps, no false degraded alarm.
- Real finding: `rogue_ap_indicator` (low) on a neighbouring network; review
  via PATCH succeeded; form-encoded PATCH refused (415).

## Phase 5 (2026-11-11, hardware)

- LCD: detected at 0x27, WRAITH screens confirmed by the user.
- systemd user service: Core main PID with dialout, by-id device, dashboard
  on :8088; status announce on host connect (`online` within 12 s).
- Deauth/disassoc on hardware: phone leaving own network "Bo" →
  `wifi.disassoc_observed`, reason 8, unprotected, −47…−65 dBm; below the
  burst threshold, so correctly no finding.
- Unplug/replug ×2: link error at once; reopen ≤ 5 s after the device
  reappeared; read stalled after the replay burst both times and the stall
  watchdog reopened it; sensor online again 16–19 s after replugging.
- An earlier cable disturbance left the CP2102 failing every transfer
  (kernel `urb status -71`) until physically replugged; not recoverable in
  software.

## Accelerated load test

Feeding 40 000 beacon events (~7 h of real capture) straight through the
pipeline into an on-disk DB (`WRAITH_LOAD=40000`, env-gated, not in check-all):
ingest ~7 000 events/s, heap delta 0.3 MB (no per-event leak), DB ~31 MB.

This surfaced an O(n^2) prune: `observations.event_row` had no index, so the
cascade delete scanned the table per event — 1m13s to prune 20 k events.
Adding `idx_obs_event_row` (migration 0003) cut it to 170 ms (~430x).

## Soak (started 2026-11-11, ongoing)

~2 h sample (live service, `~/.local/share/wraith/soak.log`): link stable,
sensor connected, no stalls/reconnects/checksum failures in steady state.
Memory rose ~4 MB during warm-up then held flat (26.1 → 26.5 MB over 80 min)
— no leak. DB grows ~3.5 MB/h linearly; bounded by the 7-day retention.

Finding: ~0.25 % of events are lost as whole dropped lines in the USB path
(seen as `gap_count`, not silent — the board's `queue_drops` stays 0, so the
loss is in the flaky CP2102 link, not the firmware). Harmless for passive
monitoring (each AP is re-observed within seconds); it could slightly
undercount a deauth burst, already a documented detector limitation. A longer
soak is still worthwhile before calling Phase 6 complete.

## Deauthentication burst detection

The *detection* burst path is validated end to end by
`TestDeauthBurstEndToEndOverSerial` (core/internal/ingest): a realistic burst
of `wifi.deauth_observed` lines, framed and checksummed exactly as the
firmware emits them, is fed through a pseudo-terminal into the real serial
reader → framing/CRC → parse → ingest → detection → stored finding. This
exercises every link except the ESP32 radio physically receiving a high
volume of frames.

By default WRAITH never transmits anything, so a burst is never generated
against any network for testing. The optional deauth *test* transmission
(docs/tx.md) has its own host coverage: `firmware/test/host/test_tx.c` (frame
builder, command parsing, limits, burst lifecycle, host-lost/radio aborts,
gated build) and the Core API tests (`core/internal/api/tx_test.go`: every
gate and error path, with a fake `Commander` provider). The `pio -e tx` build
and `esp_wifi_80211_tx` behaviour were validated on hardware 2026-11-12: a
5-frame deauth burst to an owned client reported `sent=5/requested=5 complete`
with an observable disconnect (docs/tx.md, docs/hardware.md).

## Client sightings (protocol v3)

`firmware/test/host/test_client.c` covers the data-header parser (QoS/HT
lengths, non-data, bad version), direction mapping (station→AP, AP→station,
IBSS), rejection of WDS/multicast/zero/client==BSSID, first-sight emission and
windowed aggregation. Core coverage: validator fixtures
(`protocol/fixtures/*client_*`), `TestClientSighting` (ingest upsert,
accumulation, SSID join, live stream), and `TestClientsEndpoints` (list, per-AP,
encoded path, filters). `check-protocol.sh` validates firmware-emitted
`wifi.client_observed` samples against the Go validator. Hardware: the ESP32
captures data-frame headers when tuned to the AP's channel (best-effort; see
docs/hardware.md).

## Retention

`PruneEvents` / `RunPruner` are covered by `core/internal/storage`:
pruning by age (observations cascade, findings kept), idempotency, the
initial backlog sweep, and the keep-forever (retention 0) case.

## Not yet tested

- Deauthentication *burst* findings from frames an ESP32 actually received
  (would require generating frames WRAITH deliberately does not emit).
- Multi-hour soak (in progress; see docs/roadmap.md Phase 6).
- Dashboard rendering with populated data was not screenshotted (headless
  Firefox captures before fetches resolve); empty/loading states were inspected.

## Controlled lab tests (Phase 6)

Use only access points and clients you own. Deauthentication frames for
detector validation should come from your own AP's normal behavior (reboot,
client kick via its admin UI, roaming) — by default WRAITH does not generate
them. The gated TX build (`pio -e tx`, docs/tx.md) exists for authorized
test bursts and must never be used on third-party networks.
