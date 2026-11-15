# Scheduled Bluetooth sessions

Status: **IMPLEMENTED — TESTED** (Go unit + API tests; end-to-end on hardware
2026-11-16). Code: `core/internal/btschedule`, API in
`core/internal/api/overview.go`, control in
`dashboard/src/components/bluetooth/ScheduleControl.tsx`.

A switch-mode sensor has one radio, so Bluetooth data otherwise exists only
during a session an operator starts by hand. A schedule lets Core run those
sessions unattended: it sends the same `MODE BLUETOOTH [CLASSIC]` / `MODE
WIFI` commands as the dashboard button, on a timer. **Each session pauses
Wi-Fi monitoring for its duration** — a deliberate trade the operator opts
into; it is off by default.

## Behaviour

- One global schedule: `enabled`, `every_minutes`, `duration_seconds`,
  `classic`. Stored in `app_config` (`bt_schedule`), so it survives restarts.
- Starts are aligned to the wall clock from the Unix epoch: a 60-minute
  schedule fires on the hour, a 15-minute one at :00/:15/:30/:45.
- Core drives it (`btschedule.Runner`, a 2 s tick). The firmware's own
  safety limits still apply: a session also ends on host-link loss and at the
  15-minute firmware cap.
- **Classic inquiry transmits**, so it is a separate opt-in (`classic`), with
  the same warning as the manual control. If the sensor's firmware predates
  Classic support, a Classic schedule falls back to BLE-only.
- **The operator always wins.** If the radio is already in Bluetooth mode
  when a scheduled start comes due (a manual session), the schedule skips that
  start and never force-returns a manual session to Wi-Fi. Turning the
  schedule off ends a *scheduled* session in progress immediately.
- A start is skipped (not counted as a running session) when no
  switch-capable sensor is connected, so it never later sends a stray `MODE
  WIFI`.

## Limits

`every_minutes` 1–1440, `duration_seconds` 10–600, and the duration must be
shorter than the interval. Validated in Core (`Schedule.Validate`) and
mirrored in the dashboard form from the `limits` the API returns.

## API

- `GET /api/v1/bluetooth/schedule` → `{schedule, in_session, next_run?,
  limits}` (`next_run` only when enabled).
- `PUT /api/v1/bluetooth/schedule` with the schedule JSON. JSON-only (the CSRF
  guard, as `/tx` and `/bluetooth/mode`): a scheduled switch pauses Wi-Fi, so
  a cross-site page must not set it. `400` on an out-of-range or malformed
  body, `415` on a non-JSON content type.

The dashboard control is on the Bluetooth overview, with presets and the
next-run time.
