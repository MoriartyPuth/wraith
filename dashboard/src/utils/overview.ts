// Pure helpers for the master overview's coverage timeline and the global
// search (unit-tested in overview.test.ts).
import type { AccessPoint, BTDevice, Client, CoverageSegment, SearchHit } from "../types/api";
import { btDeviceHref } from "./bluetooth";

export interface Bar { left: number; width: number; seg: CoverageSegment; label: string }

export const KIND_LABEL: Record<CoverageSegment["kind"], string> = { wifi: "Wi-Fi", bluetooth: "Bluetooth", no_data: "No data" };

export function segmentLabel(s: CoverageSegment): string {
  return s.kind === "bluetooth" && s.classic ? "Bluetooth + Classic inquiry" : KIND_LABEL[s.kind];
}

/** "1h 05m", "14m", "45s". */
export function spanText(ms: number): string {
  const sec = Math.max(0, Math.round(ms / 1000));
  const h = Math.floor(sec / 3600), m = Math.floor((sec % 3600) / 60);
  if (h >= 24) return `${Math.floor(h / 24)}d ${h % 24}h`;
  if (h) return `${h}h ${String(m).padStart(2, "0")}m`;
  if (m) return `${m}m`;
  return `${sec}s`;
}

/** Positions (percent of the window) for each segment; zero-width ones dropped. */
export function layoutCoverage(segs: CoverageSegment[], since: number, until: number, fmt: (ms: number) => string): Bar[] {
  const span = until - since;
  if (span <= 0) return [];
  return segs
    .map((seg) => {
      const a = Math.max(since, seg.from), b = Math.min(until, seg.to);
      return { left: ((a - since) / span) * 100, width: ((b - a) / span) * 100, seg,
        label: `${segmentLabel(seg)} · ${fmt(a)}–${fmt(b)} (${spanText(b - a)})` };
    })
    .filter((b) => b.width > 0);
}

/** Axis ticks at whole hours (every `stepH`), as percentages. */
export function hourTicks(since: number, until: number, stepH: number): { at: number; pct: number }[] {
  const step = stepH * 3_600_000;
  const out: { at: number; pct: number }[] = [];
  const first = new Date(since);
  first.setMinutes(0, 0, 0);
  let t = first.getTime();
  if (t < since) t += 3_600_000;
  while (new Date(t).getHours() % stepH !== 0) t += 3_600_000;
  for (; t <= until; t += step) out.push({ at: t, pct: ((t - since) / (until - since)) * 100 });
  return out;
}

export function coverageSummary(totals: Record<string, number>): string {
  const parts = [`Wi-Fi ${spanText(totals.wifi ?? 0)}`, `Bluetooth ${spanText(totals.bluetooth ?? 0)}`];
  if (totals.classic) parts[1] += ` (Classic ${spanText(totals.classic)})`;
  if (totals.no_data) parts.push(`no data ${spanText(totals.no_data)}`);
  return parts.join(" · ");
}

// ---- search ----

export function searchHref(h: SearchHit): string {
  switch (h.kind) {
    case "access_point": return `/access-points/${encodeURIComponent(h.id)}`;
    case "client": return `/clients?q=${encodeURIComponent(h.id)}`;
    default: return btDeviceHref(h.id);
  }
}

export const SEARCH_KIND_LABEL: Record<SearchHit["kind"], string> = { access_point: "AP", client: "Client", bt_device: "Bluetooth" };

/** Same matching rules as Core's /search, for demo mode: text matches names,
 * address queries ignore separators and case. */
export function searchMatcher(q: string): { text: (s: string | null | undefined) => boolean; addr: (a: string) => boolean } {
  const t = q.trim().toLowerCase();
  const hexOnly = /^[0-9a-f:.-]+$/i.test(q.trim());
  const h = q.trim().replace(/[:.-]/g, "").toUpperCase();
  return {
    text: (s) => !!s && s.toLowerCase().includes(t),
    addr: (a) => hexOnly && h.length >= 2 && a.replace(/:/g, "").toUpperCase().includes(h),
  };
}

export function demoSearch(q: string, aps: AccessPoint[], clients: Client[], bt: BTDevice[], perKind = 8): SearchHit[] {
  const m = searchMatcher(q);
  const byNewest = <T extends { last_seen: number }>(a: T[]) => [...a].sort((x, y) => y.last_seen - x.last_seen).slice(0, perKind);
  const ap: SearchHit[] = byNewest(aps.filter((a) => m.text(a.ssid) || m.addr(a.bssid))).map((a) => ({
    kind: "access_point", id: a.bssid, title: a.ssid || "(hidden SSID)", detail: a.bssid + (a.auth_mode ? ` · ${a.auth_mode}` : ""), last_seen: a.last_seen }));
  const seen = new Map<string, Client>();
  for (const c of clients) if (m.addr(c.client) && (!seen.has(c.client) || seen.get(c.client)!.last_seen < c.last_seen)) seen.set(c.client, c);
  const cl: SearchHit[] = byNewest([...seen.values()]).map((c) => ({
    kind: "client", id: c.client, title: c.client, detail: `client · last with ${c.ssid || c.bssid}`, last_seen: c.last_seen }));
  const bd: SearchHit[] = byNewest(bt.filter((d) => m.text(d.name) || m.addr(d.address))).map((d) => ({
    kind: "bt_device", id: d.id, title: d.name || d.address,
    detail: `${d.address} · ${d.transport === "classic" ? "Classic" : "BLE"}${d.address_type ? ` ${d.address_type}` : ""}`, last_seen: d.last_seen }));
  return [...ap, ...cl, ...bd]; // clients only ever match by address
}
