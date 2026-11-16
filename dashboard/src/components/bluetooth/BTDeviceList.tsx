import { useMemo, useState } from "react";
import { api } from "../../services/api";
import { Pager, usePaged } from "../Pager";
import { useApi } from "../../hooks/useApi";
import { useLiveRefresh } from "../../services/live";
import { Async, Input, Panel, Rssi, Select, Table, Toolbar, td } from "../ui";
import { ActiveBadge, BTDeviceLink, IdentityCell } from "./BTBadges";
import { LabelEditor } from "../LabelEditor";
import type { BTKind, BTTransport } from "../../types/api";
import { KIND_LABELS, companyLabel, deviceSearchText, shortUUID, uuidName } from "../../utils/bluetooth";
import { timeAgo } from "../../utils/format";

const LIMIT = 500;

export function BTDeviceList({ transport }: { transport: BTTransport }) {
  const [q, setQ] = useState("");
  const [kind, setKind] = useState("all");
  const devices = useApi((s) => api.btDevices({ transport }, LIMIT, 0, s), 30000, [transport]);
  useLiveRefresh(["bt_device"], devices.reload, 3000);
  const needle = q.trim().toLowerCase();
  const all = useMemo(() => devices.data?.data ?? [], [devices.data]);
  const kinds = useMemo(() => [...new Set(all.map((d) => d.identity?.kind).filter((k): k is BTKind => !!k))]
    .sort((a, b) => KIND_LABELS[a].localeCompare(KIND_LABELS[b])), [all]);
  const rows = useMemo(() => all.filter((d) => (!needle || deviceSearchText(d).includes(needle)) &&
    (kind === "all" || (kind === "none" ? !d.identity : d.identity?.kind === kind))), [all, needle, kind]);
  const total = devices.data?.pagination.total ?? 0;
  const paged = usePaged(rows, `${transport}|${needle}|${kind}`);

  return (
    <>
      <Toolbar>
        <Input value={q} onChange={(e) => setQ(e.target.value)} aria-label="Search" className="w-72"
          placeholder={transport === "ble" ? "Search name, type, address, manufacturer, service" : "Search name, address or class"} />
        {transport === "ble" && (
          <Select label="Type" value={kind} onChange={setKind} options={[
            { value: "all", label: "All types" },
            ...kinds.map((k) => ({ value: k, label: KIND_LABELS[k] })),
            { value: "none", label: "Not identified" }]} />
        )}
        <span className="ml-auto text-xs text-ink-3">
          {devices.data ? `${total} address${total === 1 ? "" : "es"}${total > LIMIT ? ` · newest ${LIMIT} shown` : ""}` : ""}
        </span>
      </Toolbar>
      <Panel>
        <Async state={devices} isEmpty={() => rows.length === 0}
          empty={needle || kind !== "all" ? "No device matches the search or filter." : transport === "ble"
            ? "No BLE advertisers observed yet. BLE is only scanned while a sensor's radio is in Bluetooth mode."
            : "No Classic devices observed yet. Classic devices appear only when discoverable, and only while a sensor's radio is in Bluetooth mode."}>
          {() => transport === "classic" ? (
            <Table head={["Device", "Class (from CoD)", "Last RSSI", "Observations", "First seen", "Last seen"]}>
              {paged.rows.map((d) => (
                <tr key={d.id}>
                  <td className={td}>
                    <div className="group flex items-start gap-2">
                      <BTDeviceLink d={d} />
                      <LabelEditor address={d.address} label={d.label} onChanged={devices.reload} placeholder="e.g. my phone" />
                    </div>
                  </td>
                  <td className={td}>{d.device_class ?? <span className="text-ink-3">not reported</span>}</td>
                  <td className={td}><Rssi dbm={d.last_rssi} /></td>
                  <td className={`${td} tabular-nums`}>{d.observations}</td>
                  <td className={`${td} whitespace-nowrap`}>{timeAgo(d.first_seen)}</td>
                  <td className={`${td} whitespace-nowrap`}><span className="inline-flex items-center gap-2">{timeAgo(d.last_seen)}<ActiveBadge lastSeen={d.last_seen} /></span></td>
                </tr>
              ))}
            </Table>
          ) : (
            <Table head={["Device", "Identified as", "Last RSSI", "Manufacturer", "Services", "Adverts", "First seen", "Last seen"]}>
              {paged.rows.map((d) => (
                <tr key={d.id}>
                  <td className={td}>
                    <div className="group flex items-start gap-2">
                      <BTDeviceLink d={d} identityFallback={false} />
                      <LabelEditor address={d.address} label={d.label} onChanged={devices.reload} placeholder="e.g. my AirPods" />
                    </div>
                    <span className="text-xs text-ink-3">{d.address_type} address</span>
                  </td>
                  <td className={td}><IdentityCell d={d} /></td>
                  <td className={td}><Rssi dbm={d.last_rssi} /></td>
                  <td className={td}>
                    {companyLabel(d.manufacturer_id) ?? (d.address_vendor ? null : <span className="text-ink-3">—</span>)}
                    {d.address_vendor && <span className="block text-xs text-ink-3" title="IEEE maker of this public address">{d.address_vendor} (from address)</span>}
                  </td>
                  <td className={`${td} text-xs`}>
                    {d.services.length ? d.services.map((u) => uuidName(u) ?? shortUUID(u)).join(", ") : <span className="text-ink-3">—</span>}
                  </td>
                  <td className={`${td} tabular-nums`}>{d.observations}</td>
                  <td className={`${td} whitespace-nowrap`}>{timeAgo(d.first_seen)}</td>
                  <td className={`${td} whitespace-nowrap`}><span className="inline-flex items-center gap-2">{timeAgo(d.last_seen)}<ActiveBadge lastSeen={d.last_seen} /></span></td>
                </tr>
              ))}
            </Table>
          )}
        </Async>
        <Pager {...paged.pager} />
      </Panel>
    </>
  );
}
