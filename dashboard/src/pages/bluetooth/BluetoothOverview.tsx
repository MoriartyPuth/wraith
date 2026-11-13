import { Link } from "react-router-dom";
import { api } from "../../services/api";
import { useApi } from "../../hooks/useApi";
import { useLiveRefresh } from "../../services/live";
import { Async, Badge, PageHeader, Panel, Rssi, Stat, Table, td } from "../../components/ui";
import { RssiHistogram } from "../../components/LazyCharts";
import { ActiveBadge, BTDeviceLink, TransportBadge } from "../../components/bluetooth/BTBadges";
import type { BTSensorStatus } from "../../types/api";
import { coexistence, rssiHistogram, scanStateLabel } from "../../utils/bluetooth";
import { timeAgo } from "../../utils/format";

const HIST_LIMIT = 500;

function SensorCard({ s }: { s: BTSensorStatus }) {
  const c = coexistence(s);
  return (
    <div className="rounded-lg border border-line bg-surface px-4 py-3">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-mono text-sm">{s.sensor_id}</span>
        <Badge t={s.mode === "bluetooth" ? "accent" : "neutral"}>radio: {s.mode ?? "unknown"}</Badge>
        <Badge t={s.state === "error" ? "bad" : s.state?.startsWith("scanning") ? "good" : "neutral"}>{scanStateLabel(s.state)}</Badge>
        <span className="ml-auto text-xs text-ink-3">updated {timeAgo(s.updated_at)}</span>
      </div>
      <p className={`mt-2 text-xs ${c.tone === "warn" ? "text-warn" : "text-ink-2"}`}>{c.text}</p>
      {s.message && <p className="mt-1 text-xs text-ink-3">Sensor: {s.message}</p>}
      <div className="mt-2 text-xs text-ink-3">
        Sensor-reported counts: Classic {s.classic_devices ?? "—"} · BLE {s.ble_devices ?? "—"} · Bluetooth errors {s.error_count}
      </div>
      {s.last_error_code && (
        <div role="alert" className="mt-2 rounded border border-bad/30 bg-bad/10 px-2 py-1 text-xs text-bad">
          Last error <span className="font-mono">{s.last_error_code}</span>: {s.last_error_message} · {timeAgo(s.last_error_at)}
        </div>
      )}
    </div>
  );
}

export default function BluetoothOverview() {
  const status = useApi((s) => api.btStatus(s), 15000);
  const devices = useApi((s) => api.btDevices({}, HIST_LIMIT, 0, s), 30000);
  useLiveRefresh(["bt_status"], status.reload, 1000);
  useLiveRefresh(["bt_device"], devices.reload, 5000);
  const st = status.data?.data;
  const all = devices.data?.data ?? [];
  const hist = rssiHistogram(all);
  const recentMin = st ? Math.round(st.devices.recent_window_ms / 60000) : 10;

  return (
    <>
      <PageHeader title="Bluetooth"
        subtitle="Bluetooth Classic and BLE observations. The sensor has one radio: it scans Bluetooth only while switched to Bluetooth mode, and Wi-Fi monitoring pauses meanwhile." />
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <Stat label="Addresses" value={st?.devices.total ?? "—"} hint="stored, both transports" />
        <Stat label="Classic" value={st?.devices.classic ?? "—"} hint={st ? `${st.devices.recent_classic} in last ${recentMin} min` : undefined} />
        <Stat label="BLE" value={st?.devices.ble ?? "—"} hint={st ? `${st.devices.recent_ble} in last ${recentMin} min` : undefined} />
        <Stat label="Bluetooth errors" value={st ? st.sensors.reduce((a, s) => a + s.error_count, 0) : "—"} hint="reported by sensors" />
      </div>

      <Panel title="Sensors · radio mode and coexistence" className="mt-4">
        <Async state={status} isEmpty={(d) => d.data.sensors.length === 0}
          empty="No sensor has reported Bluetooth status. Sensors running the Wi-Fi-only firmware never do; Bluetooth needs the Bluetooth firmware build.">
          {(d) => <div className="grid gap-3 md:grid-cols-2">{d.data.sensors.map((s) => <SensorCard key={s.sensor_id} s={s} />)}</div>}
        </Async>
      </Panel>

      <div className="mt-4 grid gap-4 lg:grid-cols-2">
        <Panel title="Signal strength distribution">
          <Async state={devices} isEmpty={(d) => d.data.length === 0} empty="No Bluetooth devices observed yet.">
            {(d) => (
              <>
                <RssiHistogram buckets={hist.buckets} unknown={hist.unknown} />
                {d.pagination.total > HIST_LIMIT && (
                  <p className="mt-1 text-xs text-ink-3">Based on the {HIST_LIMIT} most recently seen of {d.pagination.total} addresses.</p>
                )}
              </>
            )}
          </Async>
        </Panel>
        <Panel title="Recent observations" action={<span className="flex gap-3 text-xs"><Link className="hover:text-accent" to="/bluetooth/classic">Classic →</Link><Link className="hover:text-accent" to="/bluetooth/ble">BLE →</Link></span>}>
          <Async state={devices} isEmpty={(d) => d.data.length === 0} empty="No Bluetooth devices observed yet.">
            {(d) => (
              <Table head={["Device", "Type", "Last RSSI", "Last seen"]}>
                {d.data.slice(0, 8).map((x) => (
                  <tr key={x.id}>
                    <td className={td}><BTDeviceLink d={x} /></td>
                    <td className={td}><TransportBadge t={x.transport} /></td>
                    <td className={td}><Rssi dbm={x.last_rssi} /></td>
                    <td className={`${td} whitespace-nowrap`}><span className="inline-flex items-center gap-2">{timeAgo(x.last_seen)}<ActiveBadge lastSeen={x.last_seen} /></span></td>
                  </tr>
                ))}
              </Table>
            )}
          </Async>
        </Panel>
      </div>
      <p className="mt-3 text-xs text-ink-3">
        One row per observed address. BLE random addresses rotate, so counts are addresses, not people or physical
        devices. Live updates for busy areas are throttled (at most one per device every 2 s); stored data is complete.
      </p>
    </>
  );
}
