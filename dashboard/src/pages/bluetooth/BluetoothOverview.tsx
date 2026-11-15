import { useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../../services/api";
import { useApi } from "../../hooks/useApi";
import { useLiveRefresh } from "../../services/live";
import { Async, Badge, Button, PageHeader, Panel, Rssi, Stat, Table, td } from "../../components/ui";
import { RssiHistogram } from "../../components/LazyCharts";
import { ActiveBadge, BTDeviceLink, TransportBadge } from "../../components/bluetooth/BTBadges";
import type { BTSensorStatus } from "../../types/api";
import { breakdowns, CLASSIC_TX_WARNING, classicInquiryOn, coexistence, presence, rssiHistogram, scanStateLabel } from "../../utils/bluetooth";
import { CategoryBars } from "../../components/bluetooth/CategoryBars";
import { timeAgo } from "../../utils/format";

const HIST_LIMIT = 500;
const NEAR_WINDOW_MS = 5 * 60_000; // "near now" = heard within 5 min (BLE tracker expiry)

/** Radio mode switch. Bluetooth mode pauses Wi-Fi monitoring on the sensor,
 * so it needs an explicit second click that says so. */
function ModeControl({ s }: { s: BTSensorStatus }) {
  const [confirming, setConfirming] = useState(false);
  const [confirmClassic, setConfirmClassic] = useState(false);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ t: "good" | "bad"; text: string } | null>(null);
  const classicOn = classicInquiryOn(s);
  const send = (mode: "wifi" | "bluetooth" | "status", classic?: boolean) => {
    setBusy(true);
    setMsg(null);
    const what = classic !== undefined && s.mode === "bluetooth"
      ? `Classic inquiry ${classic ? "on" : "off"} requested`
      : `Switch to ${mode === "wifi" ? "Wi-Fi" : "Bluetooth"} requested`;
    api.btSetMode(mode, classic)
      .then(() => setMsg({ t: "good", text: mode === "status" ? "Status requested." : `${what}; the card updates when the sensor confirms.` }))
      .catch((e: Error) => setMsg({ t: "bad", text: e.message }))
      .finally(() => { setBusy(false); setConfirming(false); setConfirmClassic(false); });
  };
  return (
    <div className="mt-3 border-t border-line pt-3">
      <div className="flex flex-wrap items-center gap-2">
        {s.mode === "bluetooth" ? (
          <>
            <Button variant="primary" disabled={busy} onClick={() => send("wifi")}>Back to Wi-Fi</Button>
            {classicOn ? (
              <Button disabled={busy} onClick={() => send("bluetooth", false)} title="BLE passive scanning continues">Stop Classic inquiry</Button>
            ) : confirmClassic ? (
              <>
                <span className="text-xs text-warn">{CLASSIC_TX_WARNING}</span>
                <Button variant="primary" disabled={busy} onClick={() => send("bluetooth", true)}>Start Classic inquiry</Button>
                <Button disabled={busy} onClick={() => setConfirmClassic(false)}>Cancel</Button>
              </>
            ) : (
              <Button disabled={busy} onClick={() => setConfirmClassic(true)}>Classic inquiry…</Button>
            )}
          </>
        ) : confirming ? (
          <>
            <span className="text-xs text-warn">Wi-Fi monitoring on {s.sensor_id} pauses until you switch back, the host link drops, or the sensor's Bluetooth session limit ends it.</span>
            <Button variant="primary" disabled={busy} onClick={() => send("bluetooth")}>Switch to Bluetooth</Button>
            <Button disabled={busy} onClick={() => setConfirming(false)}>Cancel</Button>
          </>
        ) : (
          <Button disabled={busy} onClick={() => setConfirming(true)}>Switch to Bluetooth…</Button>
        )}
        <Button disabled={busy} onClick={() => send("status")} title="Ask the sensor to report its mode">Refresh</Button>
      </div>
      {msg && <p role="status" className={`mt-2 text-xs ${msg.t === "bad" ? "text-bad" : "text-ink-2"}`}>{msg.text}</p>}
    </div>
  );
}

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
      <ModeControl s={s} />
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
  const near = presence(all, Date.now(), NEAR_WINDOW_MS);
  const bd = breakdowns(all);

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

      <Panel title={`Near now · heard in the last ${NEAR_WINDOW_MS / 60000} min`} className="mt-4"
        action={<span className="text-xs text-ink-3">{near.length} device{near.length === 1 ? "" : "s"}</span>}>
        <Async state={devices} isEmpty={() => near.length === 0}
          empty="No device heard in the last few minutes. Bluetooth is only scanned while the radio is in Bluetooth mode.">
          {() => (
            <Table head={["Device", "Type", "Signal", "Last seen"]}>
              {near.slice(0, 15).map((x) => (
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
        <p className="mt-2 text-xs text-ink-3">Strongest signal first. Signal depends on distance, obstacles and the device's transmit power — it is not a distance.</p>
      </Panel>

      {bd.length > 0 && (
        <Panel title="Breakdowns" className="mt-4">
          <div className="grid gap-x-6 gap-y-4 sm:grid-cols-2 lg:grid-cols-3">
            {bd.map((b) => <CategoryBars key={b.title} b={b} />)}
          </div>
          {all.length > HIST_LIMIT && <p className="mt-2 text-xs text-ink-3">Based on the {HIST_LIMIT} most recently seen of {devices.data?.pagination.total} addresses.</p>}
        </Panel>
      )}

      <div className="mt-4 grid gap-4 lg:grid-cols-2 [&>*]:min-w-0">
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
