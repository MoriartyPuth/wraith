import { useMemo } from "react";
import { Link } from "react-router-dom";
import { api } from "../services/api";
import { useApi } from "../hooks/useApi";
import { useLive, useLiveRefresh } from "../services/live";
import { Async, Badge, Empty, Panel, PageHeader, Stat } from "../components/ui";
import { ChannelChart } from "../components/LazyCharts";
import { SeverityBadge } from "../components/badges";
import { ProviderList } from "./Settings";
import { describeEvent } from "../utils/describe";
import { clockTime, duration, timeAgo } from "../utils/format";

export default function Overview() {
  const stats = useApi((s) => api.statistics(s), 15000);
  const info = useApi((s) => api.systemInfo(s), 30000);
  const providers = useApi((s) => api.captureStatus(s), 15000);
  const findings = useApi((s) => api.detections(5, 0, s), 30000);
  const recent = useApi((s) => api.events({ types: ["wifi.ap_observed", "wifi.deauth_observed", "wifi.disassoc_observed", "wifi.pmf_observed", "sensor.status"] }, 15, 0, s), 30000);
  const live = useLive();
  useLiveRefresh(["event", "access_point", "finding"], stats.reload, 3000);
  useLiveRefresh(["finding"], findings.reload);
  useLiveRefresh(["provider"], providers.reload);
  const st = stats.data;

  // Timeline: live feed (newest first) topped up with recent stored events; heartbeats and
  // routine beacon reports are omitted so changes stand out.
  const timeline = useMemo(() => {
    const quiet = new Set(["sensor.heartbeat", "wifi.beacon_observed"]);
    const seen = new Set<string>();
    return [...live.feed.filter((e) => !quiet.has(e.event_type)), ...(recent.data?.data ?? [])]
      .filter((e) => !seen.has(e.sensor_id + e.event_id) && seen.add(e.sensor_id + e.event_id))
      .slice(0, 15);
  }, [live.feed, recent.data]);

  return (
    <>
      <PageHeader title="Overview" subtitle="Current state of sensors, observations and findings." />
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
        <Stat label="Sensors" value={st?.sensors ?? "—"} />
        <Stat label="Access points" value={st?.access_points ?? "—"} />
        <Stat label="Events" value={st?.events ?? "—"}
          hint={st?.runtime ? `${st.runtime.seq_gaps ?? 0} lost · ${st.runtime.duplicates ?? 0} dup this run` : undefined} />
        <Stat label="Open findings" value={st?.open_findings ?? "—"} hint={st ? `${st.findings} total` : undefined} />
        <Stat label="Core uptime" value={info.data ? duration(info.data.uptime_seconds) : "—"}
          hint={info.data ? `v${info.data.version} · protocol v${info.data.protocol_version}` : undefined} />
      </div>
      <div className="mt-4 grid gap-4 lg:grid-cols-2">
        <Panel title="Recent activity" action={<Link to="/history" className="text-xs text-ink-3 hover:text-accent">All events →</Link>}>
          {timeline.length === 0 ? <Empty>No activity yet. Heartbeats and beacon reports are hidden here.</Empty> : (
            <ol className="space-y-2 text-sm">
              {timeline.map((e) => (
                <li key={e.sensor_id + e.event_id} className="flex gap-3">
                  <span className="w-24 shrink-0 whitespace-nowrap tabular-nums text-xs text-ink-3">{clockTime(e.received_at)}</span>
                  <span className="min-w-0 truncate" title={describeEvent(e)}>{describeEvent(e)}</span>
                </li>
              ))}
            </ol>
          )}
        </Panel>
        <Panel title="Latest findings" action={<Link to="/detections" className="text-xs text-ink-3 hover:text-accent">Detection Center →</Link>}>
          <Async state={findings} isEmpty={(d) => d.data.length === 0} empty="No findings. Quiet is normal.">
            {(d) => (
              <ul className="space-y-2 text-sm">
                {d.data.map((f) => (
                  <li key={f.id} className="flex items-start gap-2">
                    <SeverityBadge s={f.severity} />
                    <span className="min-w-0 flex-1 truncate" title={f.explanation}>{f.category.replaceAll("_", " ")} · {f.explanation}</span>
                    <span className="shrink-0 text-xs text-ink-3">{timeAgo(f.created_at)}</span>
                    {f.status !== "open" && <Badge>{f.status}</Badge>}
                  </li>
                ))}
              </ul>
            )}
          </Async>
        </Panel>
        <Panel title="Observed channel distribution">
          <Async state={stats} what="statistics" isEmpty={(d) => Object.keys(d.aps_by_channel).length === 0}
            empty="No access points observed yet. Start Core with -serial and connect WRAITH Nano.">
            {(d) => <ChannelChart byChannel={d.aps_by_channel} />}
          </Async>
        </Panel>
        <Panel title="Capture">
          <Async state={providers} what="providers">{(d) => <ProviderList providers={d.data} />}</Async>
        </Panel>
      </div>
    </>
  );
}
