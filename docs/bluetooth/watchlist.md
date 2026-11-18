# Bluetooth watchlist and presence sessions

Status: **IMPLEMENTED — TESTED** (Go unit tests for session building and the
presence evaluator, storage/API tests, dashboard typecheck/build). **No
hardware is involved**: presence is derived in Core from the `bt_observations`
rows already stored from `bluetooth.*` events, so it works with past data and
with the demo dataset, and needs no firmware change.

## What it does

Mark Bluetooth addresses you care about ("my keys", "my band"). Core then:
- tracks **presence**: a device is *present* until 5 minutes after its last
  sighting;
- builds **sessions**: a session is a run of sightings with no gap longer
  than 5 minutes; an ongoing session's duration counts up to now;
- reports dwell time and session counts over the last 24 hours;
- raises findings when a watched address arrives or goes missing.

An address is an observation, not a physical device. BLE random addresses
rotate, so a device that rotates its address will look new; WRAITH does not
link addresses. Presence is a heuristic: a device that is silent but nearby
looks away, and the sensor scans one radio mode at a time.

## Data

Migration `0010_bt_watchlist` adds `bt_watchlist`, keyed by
`(transport, address, address_type)` (the same key as `bt_devices`). It keeps
the operator label, `added_at`, and the last evaluated `present` /
`state_since`, so arrival/departure findings neither repeat nor get lost when
Core restarts. Watchlist rows are user data and are never pruned; the derived
sessions depend on stored sightings and therefore on the retention window.

Adding a watch seeds `present` from the device's current `last_seen`, so
watching a device that is already here does not raise a false "arrived".

## Findings

`core/internal/presence` runs an evaluator every 60 s (started from
`cmd/wraith` after the stream hub):

| Category | Severity | When |
|----------|----------|------|
| `bt_watch_arrived` | `info` | a watched address is heard again after being away |
| `bt_watch_left` | `low` | a watched address has not been heard for 5 minutes |

Both use `classification: "observed"`, carry the device id in `bt_device`, and
state the limitations (silence is not proof of absence; MACs rotate). They
appear in the Detection Center and are published to the live stream. The state
is edge-triggered: one finding per transition, not per evaluation.

## API

| Endpoint | Purpose |
|----------|---------|
| `GET /api/v1/bluetooth/watchlist` | watched addresses with device snapshot, `present`, `sessions_24h`, `dwell_ms_24h` |
| `PUT /api/v1/bluetooth/watchlist/{id}` | add/rename; body `{"label":"..."}` (may be empty); the device must have been observed |
| `DELETE /api/v1/bluetooth/watchlist/{id}` | remove (`204`, `404` when not watched) |
| `GET /api/v1/bluetooth/watchlist/{id}/sessions` | presence + sessions + dwell (default last 24 h, `?since=<unix ms>`) |
| `GET /api/v1/bluetooth/devices/{id}/sessions` | the same for any observed device |

`{id}` is the device id (`classic:AA:..`, `ble:public:AA:..`,
`ble:random:AA:..`; colons may be `%3A`-encoded). `PUT` is JSON-only (CSRF
guard). The sessions response:

```json
{"data":{"now":1794424613312,"since":1794338213312,"present":true,
  "sessions":[{"start":1794420000000,"end":1794424400000,
               "duration_ms":4400000,"sightings":37,"ongoing":true}],
  "dwell_ms":4400000}}
```

## Dashboard

- **Bluetooth → Watchlist** (`/bluetooth/watchlist`): add an observed device
  (with an optional name), see present/away, last seen, sessions and dwell
  over 24 h, remove.
- **Device page**: a **Watch / Unwatch** button and a *Presence · last 24 h*
  panel with the session table.
- **Bluetooth overview**: a "Watchlist · presence now" card.
- Demo mode includes a labelled watchlist so the pages can be seen without a
  board.

## Not included

No location estimate (one sensor gives presence, not position), no
cross-address identity merging, and no notifications outside the findings
feed.
