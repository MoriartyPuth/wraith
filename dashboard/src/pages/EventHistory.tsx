import { useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { api, fetchAllEvents } from "../services/api";
import { useApi } from "../hooks/useApi";
import { isDemo } from "../services/mode";
import { Async, Button, Input, Panel, PageHeader, Table, Toolbar, td } from "../components/ui";
import { BssidLink } from "../components/badges";
import type { EventQuery } from "../types/api";
import { download, toCSV } from "../utils/csv";
import { describeEvent } from "../utils/describe";
import { isoTime } from "../utils/format";

const TYPES = ["wifi.ap_observed", "wifi.beacon_observed", "wifi.pmf_observed", "wifi.deauth_observed",
  "wifi.disassoc_observed", "sensor.status", "sensor.heartbeat"];
const PAGE = 100;
const EXPORT_MAX = 10000;

// datetime-local value <-> epoch ms (local time)
const toMs = (v: string | null) => (v ? new Date(v).getTime() || undefined : undefined);

export default function EventHistory() {
  const [params, setParams] = useSearchParams();
  const [offset, setOffset] = useState(0);
  const [exporting, setExporting] = useState<string | null>(null);

  const q: EventQuery = useMemo(() => ({
    types: params.get("type")?.split(",").filter(Boolean),
    bssid: params.get("bssid") || undefined,
    sensor: params.get("sensor") || undefined,
    since: toMs(params.get("from")),
    until: toMs(params.get("to")),
  }), [params]);
  const key = params.toString();
  const ev = useApi((s) => api.events(q, PAGE, offset, s), undefined, [key, offset]);

  const set = (k: string, v: string) => {
    const p = new URLSearchParams(params);
    if (v) p.set(k, v); else p.delete(k);
    setParams(p, { replace: true });
    setOffset(0);
  };
  const types = new Set(q.types ?? []);
  const toggleType = (t: string) => {
    const next = new Set(types);
    if (next.has(t)) next.delete(t); else next.add(t);
    set("type", [...next].join(","));
  };

  const doExport = async (fmt: "json" | "csv") => {
    setExporting(fmt);
    try {
      const { rows, total } = await fetchAllEvents(q, EXPORT_MAX);
      const stamp = new Date().toISOString().replace(/[:.]/g, "-");
      const name = `wraith-events${isDemo() ? "-DEMO" : ""}-${stamp}.${fmt}`;
      const meta = { exported_at: new Date().toISOString(), demo: isDemo(), filter: q, rows: rows.length, total_matching: total };
      if (fmt === "json") {
        download(name, JSON.stringify({ ...meta, events: rows }, null, 2), "application/json");
      } else {
        download(name, toCSV(
          ["id", "received_at", "event_id", "sensor_id", "source", "event_type", "bssid", "channel", "payload"],
          rows.map((e) => [e.id, isoTime(e.received_at), e.event_id, e.sensor_id, e.source, e.event_type, e.bssid, e.channel, e.payload]),
        ), "text/csv");
      }
      if (rows.length < total) alert(`Export limited to ${EXPORT_MAX} of ${total} matching events. Narrow the filter to export the rest.`);
    } catch (e) {
      alert(`Export failed: ${(e as Error).message}`);
    } finally {
      setExporting(null);
    }
  };

  const total = ev.data?.pagination.total ?? 0;
  return (
    <>
      <PageHeader title="Event History" subtitle="Every validated event Core stored, newest first. Exports contain the original JSON payloads as evidence."
        right={<span className="flex gap-2">
          <Button disabled={!!exporting || total === 0} onClick={() => doExport("json")}>{exporting === "json" ? "Exporting…" : "Export JSON"}</Button>
          <Button disabled={!!exporting || total === 0} onClick={() => doExport("csv")}>{exporting === "csv" ? "Exporting…" : "Export CSV"}</Button>
        </span>} />
      <Panel className="mb-3">
        <Toolbar>
          <label className="text-xs text-ink-3">From <Input type="datetime-local" value={params.get("from") ?? ""} onChange={(e) => set("from", e.target.value)} /></label>
          <label className="text-xs text-ink-3">To <Input type="datetime-local" value={params.get("to") ?? ""} onChange={(e) => set("to", e.target.value)} /></label>
          <Input placeholder="BSSID AA:BB:…" aria-label="BSSID" value={params.get("bssid") ?? ""} onChange={(e) => set("bssid", e.target.value.trim())} className="w-48 font-mono" />
          <Input placeholder="Sensor id" aria-label="Sensor" value={params.get("sensor") ?? ""} onChange={(e) => set("sensor", e.target.value.trim())} className="w-36" />
          {key && <Button onClick={() => { setParams({}, { replace: true }); setOffset(0); }}>Clear filters</Button>}
        </Toolbar>
        <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs">
          {TYPES.map((t) => (
            <label key={t} className="flex items-center gap-1.5 text-ink-2">
              <input type="checkbox" checked={types.has(t)} onChange={() => toggleType(t)} className="accent-[var(--color-accent)]" />
              <span className="font-mono">{t}</span>
            </label>
          ))}
          <span className="text-ink-3">(none checked = all types)</span>
        </div>
      </Panel>
      <Panel>
        <Async state={ev} isEmpty={(d) => d.data.length === 0} empty="No events match these filters.">
          {(d) => (
            <>
              <Table head={["Received", "Type", "BSSID", "Ch", "Sensor", "Details"]}>
                {d.data.map((e) => (
                  <tr key={e.id}>
                    <td className={`${td} whitespace-nowrap tabular-nums text-xs`}>{new Date(e.received_at).toLocaleString()}</td>
                    <td className={`${td} font-mono text-xs`}>{e.event_type}</td>
                    <td className={td}><BssidLink bssid={e.bssid} /></td>
                    <td className={`${td} tabular-nums`}>{e.channel ?? "—"}</td>
                    <td className={`${td} font-mono text-xs whitespace-nowrap`}>{e.sensor_id}</td>
                    <td className={td}>{describeEvent(e)}</td>
                  </tr>
                ))}
              </Table>
              <div className="mt-3 flex items-center gap-2 text-xs text-ink-3">
                <span>{offset + 1}–{offset + d.data.length} of {total}</span>
                <span className="ml-auto flex gap-2">
                  <Button disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - PAGE))}>Newer</Button>
                  <Button disabled={offset + PAGE >= total} onClick={() => setOffset(offset + PAGE)}>Older</Button>
                </span>
              </div>
            </>
          )}
        </Async>
      </Panel>
    </>
  );
}
