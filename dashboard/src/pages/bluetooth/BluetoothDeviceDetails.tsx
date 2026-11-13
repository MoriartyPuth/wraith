import { useMemo } from "react";
import { Link, useParams } from "react-router-dom";
import { api } from "../../services/api";
import { useApi } from "../../hooks/useApi";
import { useLiveRefresh } from "../../services/live";
import { Async, Badge, PageHeader, Panel, Table, td } from "../../components/ui";
import { RssiChart } from "../../components/LazyCharts";
import { ActiveBadge, ProvenanceBadge, TransportBadge } from "../../components/bluetooth/BTBadges";
import type { BTDevice, GATTItem, Observation } from "../../types/api";
import { deviceFields, shortUUID, uuidName } from "../../utils/bluetooth";
import { describeEvent } from "../../utils/describe";
import { timeAgo } from "../../utils/format";

function uuidCell(u: string) {
  const n = uuidName(u);
  return <span>{n ?? <span className="text-ink-3">unlisted</span>} <span className="font-mono text-xs text-ink-3">{shortUUID(u)}</span></span>;
}

function GATTPanel({ id }: { id: string }) {
  const svcs = useApi((s) => api.btGATT(id, "services", s), undefined, [id]);
  const chars = useApi((s) => api.btGATT(id, "characteristics", s), undefined, [id]);
  const none = "No GATT discovery has been run for this device. GATT discovery connects to a device, is operator-initiated only and is off by default; passive scanning never connects.";
  return (
    <Panel title="GATT (operator-initiated discovery)" className="mt-4">
      <Async state={svcs} isEmpty={(d) => d.data.length === 0} empty={none}>
        {(d) => (
          <>
            <h3 className="mb-1 text-xs uppercase tracking-wide text-ink-3">Services</h3>
            <Table head={["Service", "Handles", "Primary", "Discovered"]}>
              {d.data.map((g: GATTItem) => (
                <tr key={g.event_id}>
                  <td className={td}>{uuidCell(g.uuid)}</td>
                  <td className={`${td} tabular-nums`}>{g.start_handle ?? "?"}–{g.end_handle ?? "?"}</td>
                  <td className={td}>{g.primary === undefined ? "—" : g.primary ? "yes" : "no"}</td>
                  <td className={`${td} whitespace-nowrap`}>{timeAgo(g.discovered_at)}</td>
                </tr>
              ))}
            </Table>
            <h3 className="mb-1 mt-4 text-xs uppercase tracking-wide text-ink-3">Characteristics</h3>
            <Async state={chars} isEmpty={(c) => c.data.length === 0} empty="No characteristics discovered.">
              {(c) => (
                <Table head={["Characteristic", "Service", "Handle", "Properties"]}>
                  {c.data.map((g) => (
                    <tr key={g.event_id}>
                      <td className={td}>{uuidCell(g.uuid)}</td>
                      <td className={td}>{g.service_uuid ? uuidCell(g.service_uuid) : "—"}</td>
                      <td className={`${td} tabular-nums`}>{g.handle ?? "—"}</td>
                      <td className={`${td} text-xs`}>{g.properties?.join(", ") || "—"}</td>
                    </tr>
                  ))}
                </Table>
              )}
            </Async>
            {(d.truncated || chars.data?.truncated) && <p className="mt-2 text-xs text-warn">Only the newest 1000 GATT events were read.</p>}
          </>
        )}
      </Async>
    </Panel>
  );
}

function Fields({ d }: { d: BTDevice }) {
  return (
    <Table head={["Field", "Value", "Source"]}>
      {deviceFields(d).map((f) => (
        <tr key={f.label}>
          <td className={`${td} whitespace-nowrap text-ink-2`}>{f.label}</td>
          <td className={td}>
            {f.value ?? <span className="text-ink-3">unavailable</span>}
            {f.note && <span className="block text-xs text-ink-3">{f.note}</span>}
          </td>
          <td className={td}><ProvenanceBadge kind={f.kind} /></td>
        </tr>
      ))}
      <tr><td className={`${td} text-ink-2`}>Observations</td><td className={`${td} tabular-nums`}>{d.observations}</td><td className={td}><ProvenanceBadge kind="observed" /></td></tr>
      <tr><td className={`${td} text-ink-2`}>First seen</td><td className={td}>{new Date(d.first_seen).toLocaleString()}</td><td className={td}><ProvenanceBadge kind="observed" /></td></tr>
      <tr><td className={`${td} text-ink-2`}>Last seen</td><td className={td}>{new Date(d.last_seen).toLocaleString()} · {d.last_sensor ?? "?"}</td><td className={td}><ProvenanceBadge kind="observed" /></td></tr>
    </Table>
  );
}

export default function BluetoothDeviceDetails() {
  const id = decodeURIComponent(useParams().id ?? "");
  const dev = useApi((s) => api.btDevice(id, s), 30000, [id]);
  const obs = useApi((s) => api.btObservations(id, 500, s), 30000, [id]);
  const evs = useApi((s) => api.btDeviceEvents(id, 50, 0, s), 30000, [id]);
  useLiveRefresh(["bt_device"], dev.reload, 3000);
  useLiveRefresh(["bt_device"], obs.reload, 3000);
  const points: Observation[] = useMemo(() => (obs.data?.data ?? [])
    .map((o) => ({ sensor_id: o.sensor_id, channel: null, rssi: o.rssi, observed_at: o.observed_at })), [obs.data]);
  const d = dev.data?.data;

  return (
    <>
      <PageHeader title={d?.name ?? (d ? "(no name)" : "Bluetooth device")}
        subtitle={d ? `${d.address}${d.transport === "ble" ? ` · ${d.address_type} address` : ""}` : id}
        right={d && <span className="flex items-center gap-2"><TransportBadge t={d.transport} /><ActiveBadge lastSeen={d.last_seen} /></span>} />
      <p className="mb-3 text-xs"><Link to={d?.transport === "ble" ? "/bluetooth/ble" : "/bluetooth/classic"} className="text-ink-3 hover:text-accent">← back to list</Link></p>
      <Async state={dev} what="device">
        {(r) => (
          <>
            <div className="grid gap-4 lg:grid-cols-2">
              <Panel title="What WRAITH knows">
                <Fields d={r.data} />
                <p className="mt-2 text-xs text-ink-3">
                  <Badge t="good">observed</Badge> reported by the device or sensor · <Badge t="accent">derived</Badge> computed
                  from observed data · <Badge t="warn">inferred</Badge> a guess · <Badge>unavailable</Badge> never reported.
                </p>
              </Panel>
              <Panel title="Signal strength over time">
                <Async state={obs} isEmpty={() => points.every((p) => p.rssi === null)} empty="No RSSI reported for this address.">
                  {() => <RssiChart data={points} />}
                </Async>
              </Panel>
            </div>
            {r.data.transport === "ble" && <GATTPanel id={id} />}
            <Panel title="Events for this address" className="mt-4">
              <Async state={evs} isEmpty={(e) => e.data.length === 0} empty="No stored events (older events are removed by retention).">
                {(e) => (
                  <Table head={["Time", "Event", "Sensor"]}>
                    {e.data.map((x) => (
                      <tr key={x.id}>
                        <td className={`${td} whitespace-nowrap`}>{timeAgo(x.received_at)}</td>
                        <td className={td}>{describeEvent(x)}<span className="block font-mono text-xs text-ink-3">{x.event_type}</span></td>
                        <td className={`${td} font-mono text-xs`}>{x.sensor_id}</td>
                      </tr>
                    ))}
                  </Table>
                )}
              </Async>
            </Panel>
          </>
        )}
      </Async>
    </>
  );
}
