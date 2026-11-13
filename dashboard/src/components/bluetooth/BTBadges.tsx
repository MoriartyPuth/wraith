import { Link } from "react-router-dom";
import { Badge } from "../ui";
import type { BTDevice } from "../../types/api";
import { BT_ACTIVE_MS, btDeviceHref, type Provenance } from "../../utils/bluetooth";

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

/** Name (or "(no name)") linking to the device page, address underneath. */
export function BTDeviceLink({ d }: { d: Pick<BTDevice, "id" | "name" | "address"> }) {
  return (
    <Link to={btDeviceHref(d.id)} className="hover:text-accent">
      <span className="block">{d.name ?? <span className="text-ink-3">(no name)</span>}</span>
      <span className="block font-mono text-xs text-ink-3">{d.address}</span>
    </Link>
  );
}
