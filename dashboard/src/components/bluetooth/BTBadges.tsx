import { Link } from "react-router-dom";
import { Badge } from "../ui";
import type { BTDevice } from "../../types/api";
import { BT_ACTIVE_MS, batteryText, btDeviceHref, btDisplayName, type Provenance } from "../../utils/bluetooth";

const provTone = { observed: "good", derived: "accent", inferred: "warn", unavailable: "neutral" } as const;

/** Where a value came from: observed (reported), derived (computed by WRAITH
 * from observed data), inferred (a guess), unavailable (never reported). */
export function ProvenanceBadge({ kind }: { kind: Provenance }) {
  return <Badge t={provTone[kind]}>{kind}</Badge>;
}

export function TransportBadge({ t }: { t: BTDevice["transport"] }) {
  return <Badge t={t === "ble" ? "accent" : "neutral"}>{t === "ble" ? "BLE" : "Classic"}</Badge>;
}

export function ActiveBadge({ lastSeen }: { lastSeen: number }) {
  return Date.now() - lastSeen < BT_ACTIVE_MS ? <Badge t="good">active</Badge> : null;
}

/** Your name, its advertised name or what it was identified as (else "(no
 * name)"), linking to the device page, address underneath. */
export function BTDeviceLink({ d, identityFallback = true }: {
  d: Pick<BTDevice, "id" | "name" | "address" | "label" | "identity">;
  /** false where an "Identified as" column already shows the identity. */
  identityFallback?: boolean;
}) {
  const name = identityFallback ? btDisplayName(d) : d.label || d.name;
  return (
    <Link to={btDeviceHref(d.id)} className="hover:text-accent">
      <span className={`block ${d.label ? "font-medium" : !d.name && name ? "text-ink-2" : ""}`}>
        {name ?? <span className="text-ink-3">(no name)</span>}
      </span>
      <span className="block font-mono text-xs text-ink-3">{d.address}</span>
    </Link>
  );
}

/** Decoded identity: what it is, tracker state and battery, as compact badges. */
export function IdentityCell({ d }: { d: Pick<BTDevice, "identity" | "classification"> }) {
  const id = d.identity;
  if (!id) {
    return d.classification
      ? <span title="Inferred by the sensor from advertising data">{d.classification} <span className="text-xs text-ink-3">(inferred)</span></span>
      : <span className="text-ink-3">—</span>;
  }
  const batt = batteryText(id.battery);
  return (
    <div className="min-w-44" title={`Decoded from ${id.source}`}>
      <div className="flex flex-wrap items-center gap-1.5">
        <span>{id.label}</span>
        {id.kind === "tracker" && <Badge t={id.separated ? "bad" : "warn"}>{id.separated ? "tracker · away from owner" : "tracker"}</Badge>}
      </div>
      {batt && <div className="text-xs text-ink-3">battery {batt}</div>}
    </div>
  );
}
