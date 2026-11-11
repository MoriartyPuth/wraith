import { useMemo } from "react";
import { Link } from "react-router-dom";
import { api } from "../services/api";
import { useApi } from "../hooks/useApi";
import { useAPIndex } from "../hooks/useAPIndex";
import { useLiveRefresh } from "../services/live";
import { Async, Badge, Panel, PageHeader, Table, td } from "../components/ui";
import { BssidLink, SeverityBadge } from "../components/badges";
import { describeEvent } from "../utils/describe";
import { timeAgo } from "../utils/format";

const TYPES = ["wifi.deauth_observed", "wifi.disassoc_observed", "wifi.pmf_observed"];

export default function SecurityEvents() {
  const ev = useApi((s) => api.events({ types: TYPES }, 200, 0, s), 30000);
  const fs = useApi((s) => api.detections(500, 0, s), 30000);
  const apIndex = useAPIndex();
  useLiveRefresh(["event"], ev.reload, 2000);
  useLiveRefresh(["finding"], fs.reload);

  // event_id → finding that cites it as evidence
  const citedBy = useMemo(() => {
    const m = new Map<string, NonNullable<typeof fs.data>["data"][number]>();
    for (const f of fs.data?.data ?? []) for (const id of f.evidence) m.set(id, f);
    return m;
  }, [fs.data]);

  return (
    <>
      <PageHeader title="Security Events"
        subtitle="Security-relevant observations: deauthentication/disassociation frames and PMF advertisements. These are observations, not verdicts — severity is assigned only when a detector raises a finding." />
      <Panel>
        <Async state={ev} isEmpty={(d) => d.data.length === 0}
          empty="No deauthentication, disassociation or PMF events yet. A device leaving a network normally produces one.">
          {(d) => (
            <Table head={["Received", "Type", "Access point", "Sensor", "Details", "Finding"]}>
              {d.data.map((e) => {
                const f = citedBy.get(e.event_id);
                return (
                  <tr key={e.id}>
                    <td className={`${td} whitespace-nowrap`}>{timeAgo(e.received_at)}</td>
                    <td className={td}><Badge>{e.event_type.replace("wifi.", "").replace("_observed", "")}</Badge></td>
                    <td className={td}><BssidLink bssid={e.bssid} ssid={e.bssid ? apIndex.get(e.bssid)?.ssid : undefined} /></td>
                    <td className={`${td} font-mono text-xs whitespace-nowrap`}>{e.sensor_id}</td>
                    <td className={td}>{describeEvent(e)}</td>
                    <td className={td}>{f ? (
                      <Link to={`/detections#${f.id}`} className="inline-flex items-center gap-1 whitespace-nowrap hover:text-accent">
                        <SeverityBadge s={f.severity} /> <span className="text-xs">{f.category.replaceAll("_", " ")}</span>
                      </Link>
                    ) : <span className="text-xs text-ink-3">observation</span>}</td>
                  </tr>
                );
              })}
            </Table>
          )}
        </Async>
      </Panel>
    </>
  );
}
