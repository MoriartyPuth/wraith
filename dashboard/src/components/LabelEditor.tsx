import { useState, type FormEvent } from "react";
import { api, ApiError } from "../services/api";

const MAX_LABEL = 48;

/**
 * "name it" / "rename" button that turns into an inline form. Names are kept
 * per address by Core (device_labels) and shown wherever that address appears,
 * Wi-Fi client or Bluetooth device.
 */
export function LabelEditor({ address, label, onChanged, placeholder = "e.g. my iPhone" }: {
  address: string; label: string | undefined; onChanged: () => void; placeholder?: string;
}) {
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState(label ?? "");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

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
  const cancel = () => { setEditing(false); setText(label ?? ""); setErr(null); };
  const save = (e: FormEvent) => {
    e.preventDefault();
    const v = text.trim();
    if (!v) return void (label ? finish(() => api.deleteDeviceLabel(address)) : cancel());
    if (v === label) return cancel();
    void finish(() => api.setDeviceLabel(address, v));
  };

  if (!editing) {
    return (
      <button type="button" onClick={() => { setText(label ?? ""); setEditing(true); }}
        className="whitespace-nowrap text-xs text-ink-3 opacity-60 hover:text-accent group-hover:opacity-100 focus:opacity-100"
        aria-label={label ? `Rename ${address}` : `Name ${address}`}>
        {label ? "rename" : "name it"}
      </button>
    );
  }
  return (
    <form onSubmit={save} className="flex min-w-56 flex-wrap items-center gap-1.5">
      <input autoFocus value={text} maxLength={MAX_LABEL} disabled={busy} aria-label={`Name for ${address}`}
        placeholder={placeholder} onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => { if (e.key === "Escape") cancel(); }}
        className="w-40 rounded border border-line bg-raised px-1.5 py-0.5 text-sm" />
      <button type="submit" disabled={busy} className="rounded border border-line px-1.5 py-0.5 text-xs hover:bg-raised">Save</button>
      {label && (
        <button type="button" disabled={busy} onClick={() => void finish(() => api.deleteDeviceLabel(address))}
          className="rounded border border-line px-1.5 py-0.5 text-xs text-ink-3 hover:bg-raised">Remove</button>
      )}
      <button type="button" disabled={busy} onClick={cancel} className="px-1 text-xs text-ink-3 hover:text-ink">Cancel</button>
      {err && <span className="w-full text-xs text-bad">{err}</span>}
    </form>
  );
}
