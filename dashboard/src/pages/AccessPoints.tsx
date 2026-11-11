import { useState } from "react";
import { api } from "../services/api";
import { useApi } from "../hooks/useApi";
import { useLiveRefresh } from "../services/live";
import { Async, Input, Panel, PageHeader, Table, Toolbar, td } from "../components/ui";
import { AuthBadge, BssidLink, PmfBadge } from "../components/badges";
import { timeAgo } from "../utils/format";

export default function AccessPoints() {
  const [q, setQ] = useState("");
  const aps = useApi((s) => api.accessPoints(500, 0, s), 30000);
  useLiveRefresh(["access_point"], aps.reload, 5000);
  const needle = q.trim().toLowerCase();
  return (
    <>
      <PageHeader title="Access Points" subtitle="Inventory of every AP observed, including ones no longer heard. Security and PMF are what the AP advertises." />
      <Toolbar>
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search SSID or BSSID" aria-label="Search" className="w-64" />
        <span className="ml-auto text-xs text-ink-3">{aps.data ? `${aps.data.pagination.total} APs` : ""}</span>
      </Toolbar>
      <Panel>
        <Async state={aps} isEmpty={(d) => d.data.length === 0} empty="No access points observed yet.">
          {(d) => (
            <Table head={["SSID", "BSSID", "Ch", "Last RSSI", "Security", "PMF", "First seen", "Last seen"]}>
              {d.data.filter((a) => !needle || a.ssid.toLowerCase().includes(needle) || a.bssid.toLowerCase().includes(needle)).map((a) => (
                <tr key={a.bssid}>
                  <td className={td}>{a.ssid || <span className="text-ink-3">(hidden)</span>}</td>
                  <td className={td}><BssidLink bssid={a.bssid} /></td>
                  <td className={`${td} tabular-nums`}>{a.last_channel ?? "—"}</td>
                  <td className={`${td} tabular-nums`}>{a.last_rssi ?? "—"}</td>
                  <td className={td}><AuthBadge mode={a.auth_mode} /></td>
                  <td className={td}><PmfBadge pmf={a.pmf} /></td>
                  <td className={`${td} whitespace-nowrap`}>{timeAgo(a.first_seen)}</td>
                  <td className={`${td} whitespace-nowrap`}>{timeAgo(a.last_seen)}</td>
                </tr>
              ))}
            </Table>
          )}
        </Async>
      </Panel>
    </>
  );
}
