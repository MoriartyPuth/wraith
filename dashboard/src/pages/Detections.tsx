import { useState } from "react";
import { api } from "../services/api";
import { useApi } from "../hooks/useApi";
import { useAPIndex } from "../hooks/useAPIndex";
import { useLiveRefresh } from "../services/live";
import { Async, Badge, Button, Empty, ErrorState, Loading, Panel, PageHeader, Select, Table, Toolbar, td } from "../components/ui";
import { BssidLink, SeverityBadge } from "../components/badges";
import type { Finding } from "../types/api";
import { describeEvent } from "../utils/describe";
import { timeAgo } from "../utils/format";

function Evidence({ ids }: { ids: string[] }) {
  const ev = useApi((s) => api.events({ eventIds: ids }, 64, 0, s), undefined, [ids.join(",")]);
  if (ev.loading && !ev.data) return <Loading what="evidence" />;
  if (ev.error) return <ErrorState error={ev.error} />;
  const rows = ev.data?.data ?? [];
  return (
    <div className="mt-2">
      {rows.length < ids.length && (
        <p className="mb-1 text-xs text-warn">{ids.length - rows.length} of {ids.length} cited event(s) are not in the database (e.g. pruned or from another Core instance).</p>
      )}
      <Table head={["Received", "Event", "Details"]}>
        {rows.map((e) => (
          <tr key={e.id}>
            <td className={`${td} whitespace-nowrap`}>{new Date(e.received_at).toLocaleTimeString()}</td>
            <td className={`${td} font-mono text-xs`}>{e.event_id}</td>
            <td className={td}>{describeEvent(e)}</td>
          </tr>
        ))}
      </Table>
    </div>
  );
}

function FindingCard({ f, apSSID, onStatus }: { f: Finding; apSSID?: string; onStatus: (s: Finding["status"]) => Promise<void> }) {
  const [open, setOpen] = useState(location.hash === `#${f.id}`);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const act = async (s: Finding["status"]) => {
    setBusy(true);
    setErr(null);
    try { await onStatus(s); } catch (e) { setErr((e as Error).message); } finally { setBusy(false); }
  };
  return (
    <section id={f.id} className={`rounded-lg border bg-surface p-4 ${f.status === "open" ? "border-line" : "border-line/60 opacity-75"}`}>
      <div className="flex flex-wrap items-center gap-2">
        <SeverityBadge s={f.severity} />
        <Badge>{f.classification.replace("_", " ")}</Badge>
        <span className="font-mono text-xs text-ink-2">{f.category}</span>
        <span className="text-xs text-ink-3">confidence {(f.confidence * 100).toFixed(0)}%</span>
        {f.status !== "open" && <Badge t="good">{f.status}</Badge>}
        <span className="ml-auto text-xs text-ink-3">{timeAgo(f.created_at)}</span>
      </div>
      <p className="mt-2 text-sm">{f.explanation}</p>
      {f.limitations && <p className="mt-2 text-xs text-ink-3"><span className="font-medium text-ink-2">Limitations: </span>{f.limitations}</p>}
      <div className="mt-3 flex flex-wrap items-center gap-2 text-xs text-ink-3">
        <span>AP</span><BssidLink bssid={f.bssid} ssid={apSSID} />
        <span className="ml-2">sensor <span className="font-mono">{f.sensor_id ?? "—"}</span></span>
        <span className="ml-2">{f.id}</span>
        <span className="ml-auto flex gap-2">
          {f.evidence.length > 0 && <Button onClick={() => setOpen((o) => !o)}>{open ? "Hide" : "Show"} evidence ({f.evidence.length})</Button>}
          {f.status === "open" ? (
            <>
              <Button variant="primary" disabled={busy} onClick={() => act("reviewed")}>Mark reviewed</Button>
              <Button disabled={busy} onClick={() => act("dismissed")} title="Not a concern (e.g. known benign cause)">Dismiss</Button>
            </>
          ) : <Button disabled={busy} onClick={() => act("open")}>Reopen</Button>}
        </span>
      </div>
      {err && <p className="mt-2 text-xs text-bad">Could not update: {err}</p>}
      {open && <Evidence ids={f.evidence} />}
    </section>
  );
}

export default function Detections() {
  const [status, setStatus] = useState("open");
  const [sev, setSev] = useState("all");
  const fs = useApi((s) => api.detections(500, 0, s), 15000);
  const apIndex = useAPIndex();
  useLiveRefresh(["finding"], fs.reload, 500);

  const setFindingStatus = async (id: string, s: Finding["status"]) => {
    await api.setFindingStatus(id, s);
    fs.reload();
  };

  return (
    <>
      <PageHeader title="Detection Center"
        subtitle="Findings from rule-based detectors. Each states its evidence, confidence and limitations. Detectors never mark anything confirmed; review is yours." />
      <Toolbar>
        <Select label="Status" value={status} onChange={setStatus} options={[
          { value: "open", label: "Open" }, { value: "reviewed", label: "Reviewed" }, { value: "dismissed", label: "Dismissed" }, { value: "all", label: "All statuses" }]} />
        <Select label="Severity" value={sev} onChange={setSev} options={[
          { value: "all", label: "Any severity" }, { value: "high", label: "High" }, { value: "medium", label: "Medium" }, { value: "low", label: "Low" }, { value: "info", label: "Info" }]} />
      </Toolbar>
      <Async state={fs} isEmpty={(d) => d.data.length === 0}
        empty="No findings. Detectors report deauthentication bursts, security/PMF downgrades, SSID changes and conflicting identities — a quiet environment produces none.">
        {(d) => {
          const shown = d.data.filter((f) => (status === "all" || f.status === status) && (sev === "all" || f.severity === sev));
          return shown.length === 0 ? <Panel><Empty>No findings match these filters.</Empty></Panel> : (
            <div className="space-y-3">
              {shown.map((f) => (
                <FindingCard key={f.id} f={f} apSSID={f.bssid ? apIndex.get(f.bssid)?.ssid : undefined}
                  onStatus={(s) => setFindingStatus(f.id, s)} />
              ))}
            </div>
          );
        }}
      </Async>
    </>
  );
}
