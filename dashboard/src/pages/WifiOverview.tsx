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
import { isBluetoothFinding } from "../utils/bluetooth";
import { clockTime, duration, timeAgo } from "../utils/format";
import { CLIENT_RECENT_MS, chipHint, clientIsPrivate, clientName, groupByNetwork } from "../utils/clients";
import type { Client } from "../types/api";

/** Wi-Fi section landing page (the master overview is pages/Overview.tsx). */
export default function WifiOverview() {
  const stats = useApi((s) => api.statistics(s), 15000);
  const info = useApi((s) => api.systemInfo(s), 30000);
  const providers = useApi((s) => api.captureStatus(s), 15000);
  const findings = useApi((s) => api.detections(50, 0, s), 30000);
  const recent = useApi((s) => api.events({ types: ["wifi.ap_observed", "wifi.deauth_observed", "wifi.disassoc_observed", "wifi.pmf_observed", "sensor.status"] }, 15, 0, s), 30000);
  const clients = useApi((s) => api.clients(undefined, 500, 0, s), 30000);
  const live = useLive();
  useLiveRefresh(["client"], clients.reload, 5000);
  useLiveRefresh(["event", "access_point", "finding"], stats.reload, 3000);
  useLiveRefresh(["finding"], findings.reload);
  useLiveRefresh(["provider"], providers.reload);
  const st = stats.data;
  const windowMs = st?.client_window_ms ?? CLIENT_RECENT_MS;
  const networks = useMemo(() => groupByNetwork(clients.data?.data ?? [], Date.now(), windowMs), [clients.data, windowMs]);

  // Timeline: live feed (newest first) topped up with recent stored events; heartbeats and
  // routine beacon reports are omitted so changes stand out.
  const timeline = useMemo(() => {
    const quiet = new Set(["sensor.heartbeat", "wifi.beacon_observed"]);
    const wifiOnly = (t: string) => t.startsWith("wifi.") || t.startsWith("sensor.");
    const seen = new Set<string>();
    return [...live.feed.filter((e) => wifiOnly(e.event_type) && !quiet.has(e.event_type)), ...(recent.data?.data ?? [])]
      .filter((e) => !seen.has(e.sensor_id + e.event_id) && seen.add(e.sensor_id + e.event_id))
      .slice(0, 15);
  }, [live.feed, recent.data]);

  return (
    <>
      <PageHeader title="Wi-Fi overview" subtitle="2.4 GHz monitoring: access points, events and Wi-Fi findings. Counts at the top cover the whole system." />
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-6">
        <Stat label="Sensors" value={st?.sensors ?? "—"} />
        <Stat label="Access points" value={st?.access_points ?? "—"} />
        <Stat label="Clients" value={st?.clients ?? "—"}
          hint={st?.recent_clients !== undefined ? `${st.recent_clients} in the last ${Math.round(windowMs / 60000)} min` : undefined} />
        <Stat label="Events" value={st?.events ?? "—"}
          hint={st?.runtime ? `${st.runtime.seq_gaps ?? 0} lost · ${st.runtime.duplicates ?? 0} dup this run` : undefined} />
        <Stat label="Open findings" value={st?.open_findings ?? "—"} hint={st ? `${st.findings} total` : undefined} />
        <Stat label="Core uptime" value={info.data ? duration(info.data.uptime_seconds) : "—"}
          hint={info.data ? `v${info.data.version} · protocol v${info.data.protocol_version}` : undefined} />
      </div>
      <div className="mt-4 grid gap-4 lg:grid-cols-2 [&>*]:min-w-0">
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
        <Panel title="Latest Wi-Fi findings" action={<Link to="/detections" className="text-xs text-ink-3 hover:text-accent">Detection Center →</Link>}>
          <Async state={findings} isEmpty={(d) => d.data.filter((f) => !isBluetoothFinding(f.category)).length === 0} empty="No Wi-Fi findings. Quiet is normal.">
            {(d) => (
              <ul className="space-y-2 text-sm">
                {d.data.filter((f) => !isBluetoothFinding(f.category)).slice(0, 5).map((f) => (
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
        <Panel title="Devices on networks" className="lg:col-span-2"
          action={<Link to="/clients" className="text-xs text-ink-3 hover:text-accent">All clients →</Link>}>
          <Async state={clients} isEmpty={(d) => d.data.length === 0}
            empty="No client devices seen yet. Clients show up when WRAITH Nano hears them exchanging data with an access point.">
            {() => <NetworkClientList networks={networks} windowMs={windowMs} />}
          </Async>
          <p className="mt-3 text-xs text-ink-3">
            Devices heard exchanging data with an access point, so likely connected to it. Idle devices may be missed, and
            &ldquo;private&rdquo; addresses are randomised by phones, so one phone can appear more than once.
          </p>
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

const SHOWN_NETWORKS = 8;
const SHOWN_CLIENTS = 6;

function NetworkClientList({ networks, windowMs }: { networks: ReturnType<typeof groupByNetwork>; windowMs: number }) {
  const now = Date.now();
  return (
    <ul className="divide-y divide-line text-sm">
      {networks.slice(0, SHOWN_NETWORKS).map((n) => {
        const href = n.bssids.length === 1 ? `/clients?bssid=${encodeURIComponent(n.bssids[0]!)}` : `/clients?q=${encodeURIComponent(n.ssid)}`;
        return (
          <li key={n.key} className="py-2 first:pt-0 last:pb-0">
            <div className="flex flex-wrap items-baseline gap-x-2">
              {n.bssids.length === 1
                ? <Link to={`/access-points/${encodeURIComponent(n.bssids[0]!)}`} className="font-medium hover:text-accent">{n.ssid || <span className="text-ink-3">(hidden) {n.key}</span>}</Link>
                : <span className="font-medium">{n.ssid}</span>}
              <span className="text-xs text-ink-3">
                {n.clients.length} device{n.clients.length === 1 ? "" : "s"}
                {n.recent > 0 && <> · <span className="text-good">{n.recent} active</span></>}
                {n.bssids.length > 1 && ` · ${n.bssids.length} APs`}
              </span>
              <Link to={href} className="ml-auto text-xs text-ink-3 hover:text-accent">view →</Link>
            </div>
            <div className="mt-1 flex flex-wrap gap-1.5">
              {n.clients.slice(0, SHOWN_CLIENTS).map((c) => <ClientChip key={c.client} c={c} active={now - c.last_seen < windowMs} />)}
              {n.clients.length > SHOWN_CLIENTS && (
                <Link to={href} className="self-center text-xs text-ink-3 hover:text-accent">+{n.clients.length - SHOWN_CLIENTS} more</Link>
              )}
            </div>
          </li>
        );
      })}
      {networks.length > SHOWN_NETWORKS && (
        <li className="pt-2 text-xs text-ink-3">
          {networks.length - SHOWN_NETWORKS} more network(s) — <Link to="/clients" className="hover:text-accent">see all clients</Link>
        </li>
      )}
    </ul>
  );
}

function ClientChip({ c, active }: { c: Client; active: boolean }) {
  const priv = clientIsPrivate(c);
  const name = clientName(c);
  const tip = [
    c.label, c.client, c.vendor_full && `maker: ${c.vendor_full}`, c.device_hint && `likely ${c.device_hint}`,
    `last seen ${timeAgo(c.last_seen)}`, `${c.frames} frames`, c.last_rssi !== null && `${c.last_rssi} dBm`,
  ].filter(Boolean).join(" · ");
  return (
    <span title={tip}
      className={`inline-flex items-center gap-1 rounded border px-1.5 py-0.5 text-xs ${name ? "" : "font-mono"} ${active ? "border-good/40 text-ink" : "border-line text-ink-3"}`}>
      {active && <span className="h-1.5 w-1.5 rounded-full bg-good" aria-label="active" />}
      {name ? <span className={c.label ? "font-medium" : ""}>{name}</span> : c.client}
      {!c.label && c.vendor && chipHint(c.device_hint) && <span className="text-[10px] text-ink-3">{chipHint(c.device_hint)}</span>}
      {!name && priv && <span className="font-sans text-[10px] uppercase text-ink-3">private</span>}
    </span>
  );
}
