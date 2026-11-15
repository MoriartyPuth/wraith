import { useEffect, useState } from "react";
import { api } from "../../services/api";
import { useApi } from "../../hooks/useApi";
import { Button, Panel } from "../ui";
import { CLASSIC_TX_WARNING } from "../../utils/bluetooth";
import { timeAgo } from "../../utils/format";
import type { BTSchedule } from "../../types/api";

const PRESETS = [
  { every: 60, dur: 300, label: "5 min / hour" },
  { every: 30, dur: 300, label: "5 min / 30 min" },
  { every: 15, dur: 120, label: "2 min / 15 min" },
  { every: 360, dur: 600, label: "10 min / 6 h" },
];

/** Unattended Bluetooth sessions: Core switches the radio to Bluetooth on a
 * schedule and back. Each session pauses Wi-Fi monitoring, so the control
 * says so and Classic (which transmits) is an explicit extra step. */
export function ScheduleControl() {
  const state = useApi((s) => api.btSchedule(s), 30000);
  const [draft, setDraft] = useState<BTSchedule | null>(null);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ t: "good" | "bad"; text: string } | null>(null);
  const d = state.data?.data;

  // Seed the editable draft from the server once loaded / after a save.
  useEffect(() => { if (d && draft === null) setDraft(d.schedule); }, [d, draft]);

  if (!d || draft === null) {
    return <Panel title="Scheduled sessions" className="mt-4"><p className="text-xs text-ink-3">Loading…</p></Panel>;
  }

  const lim = d.limits;
  const durTooLong = draft.duration_seconds >= draft.every_minutes * 60;
  const invalid = draft.every_minutes < lim.min_every_minutes || draft.every_minutes > lim.max_every_minutes
    || draft.duration_seconds < lim.min_duration_seconds || draft.duration_seconds > lim.max_duration_seconds || durTooLong;
  const dirty = JSON.stringify(draft) !== JSON.stringify(d.schedule);

  const save = (next: BTSchedule) => {
    setBusy(true); setMsg(null);
    api.btSetSchedule(next)
      .then((r) => { setDraft(r.data.schedule); state.reload(); setMsg({ t: "good", text: "Saved." }); })
      .catch((e: Error) => setMsg({ t: "bad", text: e.message }))
      .finally(() => setBusy(false));
  };
  const field = (k: "every_minutes" | "duration_seconds", v: string) => {
    const n = Math.trunc(Number(v));
    if (Number.isFinite(n)) setDraft({ ...draft, [k]: n });
  };

  return (
    <Panel title="Scheduled sessions" className="mt-4"
      action={<span className="text-xs text-ink-3">{d.schedule.enabled ? (d.in_session ? "session running" : d.next_run ? `next ${timeAgo(d.next_run)}` : "on") : "off"}</span>}>
      <p className="mb-3 text-xs text-ink-3">
        Core switches this sensor to Bluetooth on a schedule and back to Wi-Fi after each session. Wi-Fi monitoring is
        paused for the session's duration (one radio). A session you start by hand is left alone.
      </p>

      {!d.schedule.enabled ? (
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm text-ink-2">Off.</span>
          <Button variant="primary" disabled={busy} onClick={() => save({ ...draft, enabled: true })}>Enable…</Button>
          <span className="text-xs text-ink-3">Starts a {draft.duration_seconds}s {draft.classic ? "BLE + Classic" : "BLE"} session every {draft.every_minutes} min.</span>
        </div>
      ) : (
        <div className="space-y-3">
          <div className="flex flex-wrap items-end gap-4">
            <label className="text-xs text-ink-3">Every (min)
              <input type="number" min={lim.min_every_minutes} max={lim.max_every_minutes} value={draft.every_minutes}
                onChange={(e) => field("every_minutes", e.target.value)}
                className="mt-1 block w-24 rounded border border-line bg-raised px-2 py-1 text-sm text-ink" />
            </label>
            <label className="text-xs text-ink-3">For (sec)
              <input type="number" min={lim.min_duration_seconds} max={lim.max_duration_seconds} value={draft.duration_seconds}
                onChange={(e) => field("duration_seconds", e.target.value)}
                className="mt-1 block w-24 rounded border border-line bg-raised px-2 py-1 text-sm text-ink" />
            </label>
            <label className="flex items-center gap-2 text-sm text-ink-2">
              <input type="checkbox" checked={draft.classic} onChange={(e) => setDraft({ ...draft, classic: e.target.checked })} />
              Include Classic inquiry
            </label>
          </div>
          {draft.classic && <p className="text-xs text-warn">{CLASSIC_TX_WARNING}</p>}
          {durTooLong && <p className="text-xs text-bad">The session must be shorter than the interval ({draft.every_minutes * 60}s).</p>}
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-xs text-ink-3">Presets:</span>
            {PRESETS.map((p) => (
              <button key={p.label} disabled={busy}
                onClick={() => setDraft({ ...draft, every_minutes: p.every, duration_seconds: p.dur })}
                className="rounded border border-line px-2 py-0.5 text-xs hover:bg-raised">{p.label}</button>
            ))}
          </div>
          <div className="flex flex-wrap items-center gap-2 border-t border-line pt-3">
            <Button variant="primary" disabled={busy || invalid || !dirty} onClick={() => save(draft)}>Save</Button>
            <Button disabled={busy} onClick={() => save({ ...d.schedule, enabled: false })}>Turn off</Button>
            {dirty && <span className="text-xs text-warn">Unsaved changes</span>}
          </div>
        </div>
      )}
      {msg && <p role="status" className={`mt-2 text-xs ${msg.t === "bad" ? "text-bad" : "text-ink-3"}`}>{msg.text}</p>}
    </Panel>
  );
}
