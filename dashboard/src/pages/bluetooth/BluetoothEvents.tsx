import { useState } from "react";
import { api } from "../../services/api";
import { useApi } from "../../hooks/useApi";
import { useLiveRefresh } from "../../services/live";
import { Async, Button, PageHeader, Panel, Select, Table, Toolbar, td } from "../../components/ui";
import { describeEvent } from "../../utils/describe";
import { timeAgo } from "../../utils/format";

const GROUPS: Record<string, string[]> = {
  all: [],
  classic: ["bluetooth.classic.device_discovered", "bluetooth.classic.device_updated"],
  ble_adv: ["bluetooth.ble.advertisement_observed"],
  ble_dev: ["bluetooth.ble.device_discovered", "bluetooth.ble.device_updated"],
  gatt: ["bluetooth.ble.connection_observed", "bluetooth.ble.connection_lost",
    "bluetooth.ble.gatt_service_discovered", "bluetooth.ble.gatt_characteristic_discovered"],
  system: ["bluetooth.system.status", "bluetooth.system.error"],
};
const PAGE = 100;

export default function BluetoothEvents() {
  const [group, setGroup] = useState("all");
  const [offset, setOffset] = useState(0);
  const evs = useApi((s) => api.btEvents({ types: GROUPS[group] }, PAGE, offset, s), 30000, [group, offset]);
  useLiveRefresh(["event", "bt_status"], evs.reload, 3000);
  const total = evs.data?.pagination.total ?? 0;

  return (
    <>
      <PageHeader title="Bluetooth events" subtitle="Stored bluetooth.* events, newest first." />
      <Toolbar>
        <Select label="Event type" value={group} onChange={(v) => { setGroup(v); setOffset(0); }} options={[
          { value: "all", label: "All Bluetooth events" }, { value: "classic", label: "Classic devices" },
          { value: "ble_adv", label: "BLE advertisements" }, { value: "ble_dev", label: "BLE devices" },
          { value: "gatt", label: "GATT / connections" }, { value: "system", label: "Status and errors" }]} />
        <span className="ml-auto flex items-center gap-2 text-xs text-ink-3">
          {total > 0 && `${offset + 1}–${Math.min(offset + PAGE, total)} of ${total}`}
          <Button disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - PAGE))}>Newer</Button>
          <Button disabled={offset + PAGE >= total} onClick={() => setOffset(offset + PAGE)}>Older</Button>
        </span>
      </Toolbar>
      <Panel>
        <Async state={evs} isEmpty={(d) => d.data.length === 0} empty="No Bluetooth events stored for this filter.">
          {(d) => (
            <Table head={["Time", "Event", "Sensor"]}>
              {d.data.map((e) => (
                <tr key={e.id}>
                  <td className={`${td} whitespace-nowrap`} title={new Date(e.received_at).toLocaleString()}>{timeAgo(e.received_at)}</td>
                  <td className={td}>{describeEvent(e)}<span className="block font-mono text-xs text-ink-3">{e.event_type}</span></td>
                  <td className={`${td} font-mono text-xs`}>{e.sensor_id}</td>
                </tr>
              ))}
            </Table>
          )}
        </Async>
      </Panel>
      <p className="mt-3 text-xs text-ink-3">
        BLE advertisement reports are stored and listed here but are not pushed to the live feed; they reach live views as
        throttled device updates.
      </p>
    </>
  );
}
