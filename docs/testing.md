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

## Not yet tested

- Deauth/disassoc reporting on hardware (no such frames occurred).
- Any detector firing on real traffic.
- LCD (disabled pending electrical verification).
- Dashboard rendering with populated data was not screenshotted (headless
  Firefox captures before fetches resolve); empty/loading states were inspected.

## Controlled lab tests (Phase 6)

Use only access points and clients you own. Deauthentication frames for
detector validation should come from your own AP's normal behavior (reboot,
client kick via its admin UI, roaming) — WRAITH does not generate them.
