import { Link, useParams } from "react-router-dom";
import { api } from "../services/api";
import { useApi } from "../hooks/useApi";
import { useLiveRefresh } from "../services/live";
import { Async, Badge, Empty, Panel, PageHeader, Table, td } from "../components/ui";
import { RssiChart } from "../components/LazyCharts";
import { AuthBadge, PmfBadge, SeverityBadge } from "../components/badges";
import type { RSNInfo } from "../types/api";
import { describeEvent } from "../utils/describe";
import { timeAgo } from "../utils/format";

function Field({ k, children }: { k: string; children: React.ReactNode }) {
  return <div className="contents"><dt className="text-ink-3">{k}</dt><dd className="min-w-0 break-words">{children}</dd></div>;
}

export default function APDetail() {
  const bssid = decodeURIComponent(useParams().bssid ?? "").toUpperCase();
  const ap = useApi((s) => api.accessPoint(bssid, s), 15000, [bssid]);
  const obs = useApi((s) => api.observations(bssid, 500, s), 30000, [bssid]);
  const evs = useApi((s) => api.events({ bssid }, 30, 0, s), 30000, [bssid]);
  const fs = useApi((s) => api.detections(500, 0, s), 60000);
  useLiveRefresh(["access_point"], ap.reload, 3000);
  useLiveRefresh(["access_point"], obs.reload, 10000);
  const related = (fs.data?.data ?? []).filter((f) => f.bssid === bssid);
  const ssid = ap.data?.data.ssid;

  return (
    <>
      <PageHeader title={ssid === undefined ? bssid : ssid || "(hidden SSID)"} subtitle={bssid}
        right={<Link to="/access-points" className="text-xs text-ink-3 hover:text-accent">← All access points</Link>} />
      <div className="grid gap-4 lg:grid-cols-3">
        <Panel title="Advertised configuration" className="lg:col-span-1">
          <Async state={ap}>{({ data: a }) => {
            const rsn = a.rsn as RSNInfo | null;
            return (
              <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-sm">
                <Field k="SSID">{a.ssid || "(hidden)"} <span className="font-mono text-xs text-ink-3">{a.ssid_hex ? `0x${a.ssid_hex}` : ""}</span></Field>
                <Field k="Channel">{a.last_channel ?? "—"}</Field>
                <Field k="Last RSSI">{a.last_rssi !== null ? `${a.last_rssi} dBm` : "—"}</Field>
                <Field k="Security"><AuthBadge mode={a.auth_mode} /></Field>
                <Field k="PMF"><PmfBadge pmf={a.pmf} /></Field>
                <Field k="Group cipher">{rsn?.group_cipher ?? "—"}</Field>
                <Field k="Pairwise">{rsn?.pairwise.join(", ") || "—"}</Field>
                <Field k="AKM">{rsn?.akm.join(", ") || "—"}</Field>
                <Field k="MFPC / MFPR">{rsn ? `${rsn.mfpc ? 1 : 0} / ${rsn.mfpr ? 1 : 0}` : "no RSN element"}</Field>
                <Field k="First seen">{new Date(a.first_seen).toLocaleString()}</Field>
                <Field k="Last seen">{timeAgo(a.last_seen)}</Field>
                <Field k="Sensor"><span className="font-mono text-xs">{a.last_sensor ?? "—"}</span></Field>
              </dl>
            );
          }}</Async>
        </Panel>
        <Panel title="Signal strength history" className="lg:col-span-2">
          <Async state={obs} isEmpty={(d) => d.data.length < 2} empty="Not enough observations yet for a history.">
            {(d) => (
              <>
                <RssiChart data={d.data} />
                <p className="mt-2 text-xs text-ink-3">Last {d.data.length} observations by WRAITH Nano (one per beacon report, ~10 s apart while heard). RSSI depends on sensor placement; it is not a distance measurement.</p>
              </>
            )}
          </Async>
        </Panel>
        <Panel title="Findings for this AP" className="lg:col-span-3">
          {related.length === 0 ? <Empty>No findings reference this BSSID.</Empty> : (
            <ul className="space-y-2 text-sm">
              {related.map((f) => (
                <li key={f.id} className="flex items-start gap-2">
                  <SeverityBadge s={f.severity} /><Badge>{f.classification.replace("_", " ")}</Badge>
                  <span className="min-w-0 flex-1">{f.explanation}</span>
                  <span className="shrink-0 text-xs text-ink-3">{timeAgo(f.created_at)}</span>
                </li>
              ))}
            </ul>
          )}
        </Panel>
        <Panel title="Recent events" className="lg:col-span-3"
          action={<Link to={`/history?bssid=${encodeURIComponent(bssid)}`} className="text-xs text-ink-3 hover:text-accent">Full history →</Link>}>
          <Async state={evs} isEmpty={(d) => d.data.length === 0} empty="No events for this BSSID.">
            {(d) => (
              <Table head={["Received", "Type", "Details"]}>
                {d.data.map((e) => (
                  <tr key={e.id}>
                    <td className={`${td} whitespace-nowrap`}>{timeAgo(e.received_at)}</td>
                    <td className={`${td} font-mono text-xs`}>{e.event_type}</td>
                    <td className={td}>{describeEvent(e)}</td>
                  </tr>
                ))}
              </Table>
            )}
          </Async>
        </Panel>
      </div>
    </>
  );
}
