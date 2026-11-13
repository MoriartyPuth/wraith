import { useMemo, useState } from "react";
import { api } from "../../services/api";
import { useApi } from "../../hooks/useApi";
import { useLiveRefresh } from "../../services/live";
import { Async, Input, Panel, Rssi, Table, Toolbar, td } from "../ui";
import { ActiveBadge, BTDeviceLink } from "./BTBadges";
import type { BTTransport } from "../../types/api";
import { companyLabel, deviceSearchText, shortUUID, uuidName } from "../../utils/bluetooth";
import { timeAgo } from "../../utils/format";

const LIMIT = 500;

export function BTDeviceList({ transport }: { transport: BTTransport }) {
  const [q, setQ] = useState("");
  const devices = useApi((s) => api.btDevices({ transport }, LIMIT, 0, s), 30000, [transport]);
  useLiveRefresh(["bt_device"], devices.reload, 3000);
  const needle = q.trim().toLowerCase();
  const rows = useMemo(() => (devices.data?.data ?? []).filter((d) => !needle || deviceSearchText(d).includes(needle)),
    [devices.data, needle]);
  const total = devices.data?.pagination.total ?? 0;

  return (
    <>
      <Toolbar>
        <Input value={q} onChange={(e) => setQ(e.target.value)} aria-label="Search" className="w-72"
          placeholder={transport === "ble" ? "Search name, address, manufacturer, service" : "Search name, address or class"} />
        <span className="ml-auto text-xs text-ink-3">
          {devices.data ? `${total} address${total === 1 ? "" : "es"}${total > LIMIT ? ` · newest ${LIMIT} shown` : ""}` : ""}
        </span>
      </Toolbar>
      <Panel>
        <Async state={devices} isEmpty={() => rows.length === 0}
          empty={needle ? "No device matches the search." : transport === "ble"
            ? "No BLE advertisers observed yet. BLE is only scanned while a sensor's radio is in Bluetooth mode."
            : "No Classic devices observed yet. Classic devices appear only when discoverable, and only while a sensor's radio is in Bluetooth mode."}>
          {() => transport === "classic" ? (
            <Table head={["Device", "Class (from CoD)", "Last RSSI", "Observations", "First seen", "Last seen"]}>
              {rows.map((d) => (
                <tr key={d.id}>
                  <td className={td}><BTDeviceLink d={d} /></td>
                  <td className={td}>{d.device_class ?? <span className="text-ink-3">not reported</span>}</td>
                  <td className={td}><Rssi dbm={d.last_rssi} /></td>
                  <td className={`${td} tabular-nums`}>{d.observations}</td>
                  <td className={`${td} whitespace-nowrap`}>{timeAgo(d.first_seen)}</td>
                  <td className={`${td} whitespace-nowrap`}><span className="inline-flex items-center gap-2">{timeAgo(d.last_seen)}<ActiveBadge lastSeen={d.last_seen} /></span></td>
                </tr>
              ))}
            </Table>
          ) : (
            <Table head={["Device", "Address type", "Last RSSI", "Manufacturer", "Services", "Classification", "Adverts", "First seen", "Last seen"]}>
              {rows.map((d) => (
                <tr key={d.id}>
                  <td className={td}><BTDeviceLink d={d} /></td>
                  <td className={td}>{d.address_type}</td>
                  <td className={td}><Rssi dbm={d.last_rssi} /></td>
                  <td className={td}>{companyLabel(d.manufacturer_id) ?? <span className="text-ink-3">—</span>}</td>
                  <td className={`${td} text-xs`}>
                    {d.services.length ? d.services.map((u) => uuidName(u) ?? shortUUID(u)).join(", ") : <span className="text-ink-3">—</span>}
                  </td>
                  <td className={td}>
                    {d.classification
                      ? <span title="Inferred by the sensor from advertising data">{d.classification} <span className="text-xs text-ink-3">(inferred)</span></span>
                      : <span className="text-ink-3">—</span>}
                  </td>
                  <td className={`${td} tabular-nums`}>{d.observations}</td>
                  <td className={`${td} whitespace-nowrap`}>{timeAgo(d.first_seen)}</td>
                  <td className={`${td} whitespace-nowrap`}><span className="inline-flex items-center gap-2">{timeAgo(d.last_seen)}<ActiveBadge lastSeen={d.last_seen} /></span></td>
                </tr>
              ))}
            </Table>
          )}
        </Async>
      </Panel>
    </>
  );
}
