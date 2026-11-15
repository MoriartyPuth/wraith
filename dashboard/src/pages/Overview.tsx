import { useMemo } from "react";
import { Link } from "react-router-dom";
import { api } from "../services/api";
import { useApi } from "../hooks/useApi";
import { useLive, useLiveRefresh } from "../services/live";
import { Async, Badge, Empty, Panel, PageHeader, Stat } from "../components/ui";
import { SeverityBadge } from "../components/badges";
import { CoverageTimeline } from "../components/CoverageTimeline";
import { describeEvent } from "../utils/describe";
import { clockTime, duration, timeAgo } from "../utils/format";
import { coexistence, isBluetoothFinding, scanStateLabel, splitFindings } from "../utils/bluetooth";

const FINDINGS_SCAN = 500; // findings fetched to split by domain (newest first)
// Stored events that seed the activity list before live ones arrive.
const RECENT_TYPES = ["wifi.ap_observed", "wifi.deauth_observed", "wifi.disassoc_observed", "sensor.status",
  "bluetooth.classic.device_discovered", "bluetooth.ble.device_discovered", "bluetooth.system.status", "bluetooth.system.error"];

/** One-line summary card for a section, linking to its own overview. */
function SectionCard({ title, to, children }: { title: string; to: string; children: React.ReactNode }) {
  return (
    <Panel title={title} action={<Link to={to} className="text-xs text-ink-3 hover:text-accent">Open {title} →</Link>}>
      {children}
    </Panel>
  );
}

function Row({ label, value, hint }: { label: string; value: React.ReactNode; hint?: string }) {
  return (
    <div className="flex items-baseline justify-between gap-3 border-b border-line/60 py-1.5 text-sm last:border-b-0">
      <span className="text-ink-2">{label}</span>
      <span className="text-right tabular-nums">{value}{hint && <span className="ml-2 text-xs text-ink-3">{hint}</span>}</span>
    </div>
  );
}

/** Master overview: both radios, the shared detections and the sensors. */
export default function Overview() {
  const stats = useApi((s) => api.statistics(s), 15000);
  const info = useApi((s) => api.systemInfo(s), 30000);
  const clients = useApi((s) => api.clients(undefined, 1, 0, s), 30000);
  const bt = useApi((s) => api.btStatus(s), 15000);
  const findings = useApi((s) => api.detections(FINDINGS_SCAN, 0, s), 30000);
  const recent = useApi((s) => api.events({ types: RECENT_TYPES }, 12, 0, s), 30000);
  const live = useLive();
  useLiveRefresh(["event", "access_point", "finding"], stats.reload, 3000);
  useLiveRefresh(["bt_status", "bt_device"], bt.reload, 2000);
  useLiveRefresh(["finding"], findings.reload);
  const st = stats.data;
  const btd = bt.data?.data;
  const split = findings.data ? splitFindings(findings.data.data) : null;
  const sensorsBT = btd?.sensors ?? [];
  const inBT = sensorsBT.filter((s) => s.mode === "bluetooth").length;

  // Combined timeline: live feed topped up with recent stored events; routine
  // reports (heartbeats, beacons, BLE advert aggregates) are left out so
  // changes stand out.
  const timeline = useMemo(() => {
    const quiet = new Set(["sensor.heartbeat", "wifi.beacon_observed", "bluetooth.ble.advertisement_observed"]);
    const seen = new Set<string>();
    return [...live.feed.filter((e) => !quiet.has(e.event_type)), ...(recent.data?.data ?? [])]
      .filter((e) => !seen.has(e.sensor_id + e.event_id) && seen.add(e.sensor_id + e.event_id))
      .slice(0, 12);
  }, [live.feed, recent.data]);

  return (
    <>
      <PageHeader title="Overview" subtitle="Everything WRAITH observes: Wi-Fi, Bluetooth, findings and sensors." />
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
        <Stat label="Sensors" value={st?.sensors ?? "—"}
          hint={sensorsBT.length ? `${inBT} in Bluetooth mode` : undefined} />
        <Stat label="Access points" value={st?.access_points ?? "—"}
          hint={clients.data ? `${clients.data.pagination.total} clients` : undefined} />
        <Stat label="Bluetooth addresses" value={btd?.devices.total ?? "—"}
          hint={btd ? `${btd.devices.classic} Classic · ${btd.devices.ble} BLE` : undefined} />
        <Stat label="Open findings" value={st?.open_findings ?? "—"}
          hint={split ? `${split.wifi.open} Wi-Fi · ${split.bluetooth.open} Bluetooth${split.openHigh ? ` · ${split.openHigh} high` : ""}` : undefined} />
        <Stat label="Core uptime" value={info.data ? duration(info.data.uptime_seconds) : "—"}
          hint={info.data ? `v${info.data.version} · protocol v${info.data.protocol_version}` : undefined} />
      </div>

      <Panel title="Radio" className="mt-4">
        <Async state={bt} what="radio status" isEmpty={(d) => d.data.sensors.length === 0}
          empty="No sensor reports a radio mode: it runs the Wi-Fi-only firmware (Wi-Fi monitoring only), or has not reported yet.">
          {(d) => (
            <ul className="space-y-2">
              {d.data.sensors.map((s) => {
                const c = coexistence(s);
                return (
                  <li key={s.sensor_id} className="flex flex-wrap items-center gap-2 text-sm">
                    <span className="font-mono">{s.sensor_id}</span>
                    <Badge t={s.mode === "bluetooth" ? "accent" : "good"}>{s.mode === "bluetooth" ? "Bluetooth" : s.mode === "wifi" ? "Wi-Fi" : "unknown"}</Badge>
                    {s.mode === "bluetooth" && <Badge>{scanStateLabel(s.state)}</Badge>}
                    <span className={`order-last basis-full text-xs sm:order-none sm:min-w-0 sm:flex-1 sm:basis-auto ${c.tone === "warn" ? "text-warn" : "text-ink-3"}`}>{c.text}</span>
                    <Link to="/bluetooth" className="ml-auto text-xs text-ink-3 hover:text-accent sm:ml-0">Switch…</Link>
                  </li>
                );
              })}
            </ul>
          )}
        </Async>
      </Panel>

      <Panel title="Radio coverage" className="mt-4">
        <CoverageTimeline />
      </Panel>

      <div className="mt-4 grid gap-4 lg:grid-cols-2 [&>*]:min-w-0">
        <SectionCard title="Wi-Fi" to="/wifi">
          <Row label="Access points" value={st?.access_points ?? "—"} />
          <Row label="Clients" value={clients.data?.pagination.total ?? "—"} />
          <Row label="Channels with APs" value={st ? Object.keys(st.aps_by_channel).length : "—"} />
          <Row label="Open findings" value={split?.wifi.open ?? "—"} hint={split ? `${split.wifi.total} total` : undefined} />
        </SectionCard>
        <SectionCard title="Bluetooth" to="/bluetooth">
          <Row label="Classic addresses" value={btd?.devices.classic ?? "—"}
            hint={btd ? `${btd.devices.recent_classic} recent` : undefined} />
          <Row label="BLE addresses" value={btd?.devices.ble ?? "—"}
            hint={btd ? `${btd.devices.recent_ble} recent` : undefined} />
          <Row label="Errors reported" value={btd ? btd.sensors.reduce((a, s) => a + s.error_count, 0) : "—"} />
          <Row label="Open findings" value={split?.bluetooth.open ?? "—"} hint={split ? `${split.bluetooth.total} total` : undefined} />
        </SectionCard>

        <Panel title="Latest findings" action={<Link to="/detections" className="text-xs text-ink-3 hover:text-accent">Detection Center →</Link>}>
          <Async state={findings} isEmpty={(d) => d.data.length === 0} empty="No findings. Quiet is normal.">
            {(d) => (
              <ul className="space-y-2 text-sm">
                {d.data.slice(0, 6).map((f) => (
                  <li key={f.id} className="flex items-start gap-2">
                    <SeverityBadge s={f.severity} />
                    <Badge>{isBluetoothFinding(f.category) ? "Bluetooth" : "Wi-Fi"}</Badge>
                    <Link to={`/detections#${f.id}`} className="min-w-0 flex-1 truncate hover:text-accent" title={f.explanation}>
                      {f.category.replaceAll("_", " ")} · {f.explanation}
                    </Link>
                    <span className="shrink-0 text-xs text-ink-3">{timeAgo(f.created_at)}</span>
                    {f.status !== "open" && <Badge>{f.status}</Badge>}
                  </li>
                ))}
              </ul>
            )}
          </Async>
        </Panel>
        <Panel title="Recent activity" action={<Link to="/history" className="text-xs text-ink-3 hover:text-accent">All events →</Link>}>
          {timeline.length === 0 ? (
            <Empty>No activity yet. Heartbeats, beacon reports and BLE advert reports are hidden here.</Empty>
          ) : (
            <ol className="space-y-2 text-sm">
              {timeline.map((e) => (
                <li key={e.sensor_id + e.event_id} className="flex gap-3">
                  <span className="w-24 shrink-0 whitespace-nowrap tabular-nums text-xs text-ink-3">{clockTime(e.received_at)}</span>
                  <Badge>{e.event_type.startsWith("bluetooth.") ? "BT" : e.event_type.startsWith("wifi.") ? "Wi-Fi" : "sys"}</Badge>
                  <span className="min-w-0 truncate" title={describeEvent(e)}>{describeEvent(e)}</span>
                </li>
              ))}
            </ol>
          )}
        </Panel>
      </div>
    </>
  );
}
