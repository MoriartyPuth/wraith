import { useState, type FormEvent } from "react";
import { api, ApiError } from "../services/api";
import type { Client } from "../types/api";
import { clientIsPrivate } from "../utils/clients";

const MAX_LABEL = 48;

/**
 * "Device" cell for a client row: the operator's name (editable inline), the
 * manufacturer from the address registry and a device-type guess. Labels are
 * per address, so renaming here renames the device everywhere it appears.
 */
export function ClientDevice({ c, onChanged }: { c: Client; onChanged: () => void }) {
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState(c.label ?? "");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const priv = clientIsPrivate(c);

  const finish = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setErr(null);
    try {
      await fn();
      setEditing(false);
      onChanged();
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : "could not save");
    } finally {
      setBusy(false);
    }
  };
  const save = (e: FormEvent) => {
    e.preventDefault();
    const v = text.trim();
    if (!v) return void (c.label ? finish(() => api.deleteDeviceLabel(c.client)) : setEditing(false));
    if (v === c.label) return setEditing(false);
    void finish(() => api.setDeviceLabel(c.client, v));
  };

  if (editing) {
    return (
      <form onSubmit={save} className="flex min-w-56 flex-wrap items-center gap-1.5">
        <input autoFocus value={text} maxLength={MAX_LABEL} disabled={busy} aria-label={`Name for ${c.client}`}
          placeholder="e.g. my iPhone" onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Escape") { setEditing(false); setText(c.label ?? ""); } }}
          className="w-40 rounded border border-line bg-raised px-1.5 py-0.5 text-sm" />
        <button type="submit" disabled={busy} className="rounded border border-line px-1.5 py-0.5 text-xs hover:bg-raised">Save</button>
        {c.label && (
          <button type="button" disabled={busy} onClick={() => void finish(() => api.deleteDeviceLabel(c.client))}
            className="rounded border border-line px-1.5 py-0.5 text-xs text-ink-3 hover:bg-raised">Remove</button>
        )}
        <button type="button" disabled={busy} onClick={() => { setEditing(false); setText(c.label ?? ""); }}
          className="px-1 text-xs text-ink-3 hover:text-ink">Cancel</button>
        {err && <span className="w-full text-xs text-bad">{err}</span>}
      </form>
    );
  }

  const title = c.label || c.vendor || (priv ? "Private address" : "Unknown maker");
  const sub = c.label
    ? [c.vendor, c.device_hint && (c.vendor ? `likely ${c.device_hint}` : c.device_hint)].filter(Boolean).join(" · ")
    : c.vendor ? (c.device_hint ? `likely ${c.device_hint}` : "") : priv ? "usually a phone, tablet or laptop" : "";
  return (
    <div className="group min-w-40" title={c.vendor_full ? `Registered maker: ${c.vendor_full}` : undefined}>
      <div className="flex items-center gap-1.5">
        <span className={c.label || c.vendor ? (c.label ? "font-medium" : "") : "text-ink-3"}>{title}</span>
        <button type="button" onClick={() => { setText(c.label ?? ""); setEditing(true); }}
          className="text-xs text-ink-3 opacity-60 hover:text-accent group-hover:opacity-100 focus:opacity-100"
          aria-label={c.label ? `Rename ${c.client}` : `Name ${c.client}`}>
          {c.label ? "rename" : "name it"}
        </button>
      </div>
      {sub && <div className="text-xs text-ink-3">{sub}</div>}
    </div>
  );
}
