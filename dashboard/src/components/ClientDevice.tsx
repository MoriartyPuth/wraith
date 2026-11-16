import type { Client } from "../types/api";
import { clientIsPrivate } from "../utils/clients";
import { LabelEditor } from "./LabelEditor";

/**
 * "Device" cell for a client row: the operator's name (editable inline), the
 * manufacturer from the address registry and a device-type guess. Labels are
 * per address, so renaming here renames the device everywhere it appears.
 */
export function ClientDevice({ c, onChanged }: { c: Client; onChanged: () => void }) {
  const priv = clientIsPrivate(c);
  const title = c.label || c.vendor || (priv ? "Private address" : "Unknown maker");
  const sub = c.label
    ? [c.vendor, c.device_hint && (c.vendor ? `likely ${c.device_hint}` : c.device_hint)].filter(Boolean).join(" · ")
    : c.vendor ? (c.device_hint ? `likely ${c.device_hint}` : "") : priv ? "usually a phone, tablet or laptop" : "";
  return (
    <div className="group min-w-40" title={c.vendor_full ? `Registered maker: ${c.vendor_full}` : undefined}>
      <div className="flex flex-wrap items-center gap-1.5">
        <span className={c.label || c.vendor ? (c.label ? "font-medium" : "") : "text-ink-3"}>{title}</span>
        <LabelEditor address={c.client} label={c.label} onChanged={onChanged} />
      </div>
      {sub && <div className="text-xs text-ink-3">{sub}</div>}
    </div>
  );
}
