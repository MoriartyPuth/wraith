import { Link } from "react-router-dom";
import type { PMF, Severity } from "../types/api";
import { Badge } from "./ui";

export function PmfBadge({ pmf }: { pmf: PMF }) {
  return pmf === "required" ? <Badge t="good">PMF required</Badge>
    : pmf === "capable" ? <Badge t="accent">PMF capable</Badge>
    : pmf === "not_advertised" ? <Badge t="warn">PMF not advertised</Badge>
    : <Badge>PMF unknown</Badge>;
}

export function AuthBadge({ mode }: { mode: string | null }) {
  if (!mode) return <span className="text-ink-3">—</span>;
  const t = mode === "open" || mode === "wep" ? "bad" : mode.startsWith("wpa3") || mode === "wpa2-wpa3-psk" ? "good" : mode.startsWith("wpa-") ? "warn" : "neutral";
  return <Badge t={t}>{mode}</Badge>;
}

export const sevTone: Record<Severity, "neutral" | "accent" | "warn" | "bad"> = { info: "neutral", low: "accent", medium: "warn", high: "bad" };

export function SeverityBadge({ s }: { s: Severity }) {
  return <Badge t={sevTone[s]}>{s}</Badge>;
}

export function BssidLink({ bssid, ssid }: { bssid: string | null; ssid?: string }) {
  if (!bssid) return <span className="text-ink-3">—</span>;
  return (
    <Link to={`/access-points/${encodeURIComponent(bssid)}`} className="whitespace-nowrap hover:text-accent">
      <span className="font-mono text-xs">{bssid}</span>
      {ssid !== undefined && <span className="ml-2 text-ink-2">{ssid || "(hidden)"}</span>}
    </Link>
  );
}
