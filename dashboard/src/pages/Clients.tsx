import { useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { api } from "../services/api";
import { useApi } from "../hooks/useApi";
import { useLiveRefresh } from "../services/live";
import { Async, Badge, Input, Panel, PageHeader, Table, Toolbar, td } from "../components/ui";
import { BssidLink } from "../components/badges";
import { timeAgo } from "../utils/format";

export default function Clients() {
  const [params, setParams] = useSearchParams();
  const bssid = (params.get("bssid") || "").toUpperCase();
  const [q, setQ] = useState("");
  const clients = useApi((s) => api.clients(bssid || undefined, 500, 0, s), 30000, [bssid]);
  useLiveRefresh(["client"], clients.reload, 3000);

  const needle = q.trim().toLowerCase();
  const rows = useMemo(
    () => (clients.data?.data ?? []).filter((c) =>
      !needle || c.client.toLowerCase().includes(needle) || c.ssid.toLowerCase().includes(needle) ||
      c.bssid.toLowerCase().includes(needle)),
    [clients.data, needle],
  );

  return (
    <>
      <PageHeader title="Clients"
        subtitle="Stations seen exchanging data with an access point. A sighting, not guaranteed association: idle clients are invisible and phones often randomise MACs." />
      <Toolbar>
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search client, SSID or BSSID" aria-label="Search" className="w-72" />
        {bssid && (
          <span className="flex items-center gap-2 text-xs text-ink-3">
            AP filter: <span className="font-mono">{bssid}</span>
            <button className="rounded border border-line px-1.5 py-0.5 hover:bg-raised"
              onClick={() => { const p = new URLSearchParams(params); p.delete("bssid"); setParams(p, { replace: true }); }}>
              clear
            </button>
          </span>
        )}
        <span className="ml-auto text-xs text-ink-3">{clients.data ? `${clients.data.pagination.total} sightings` : ""}</span>
      </Toolbar>
      <Panel>
        <Async state={clients} isEmpty={() => rows.length === 0}
          empty={bssid ? "No client data frames observed for this AP yet." : "No client data frames observed yet."}>
          {() => (
            <Table head={["Client", "Access point", "Ch", "Last RSSI", "Frames", "First seen", "Last seen"]}>
              {rows.map((c) => (
                <tr key={`${c.bssid}/${c.client}`}>
                  <td className={`${td} font-mono text-xs whitespace-nowrap`}>{c.client}</td>
                  <td className={td}>
                    <Link to={`/access-points/${encodeURIComponent(c.bssid)}`} className="hover:text-accent">
                      {c.ssid || <span className="text-ink-3">(hidden)</span>}
                    </Link>{" "}
                    <BssidLink bssid={c.bssid} />
                  </td>
                  <td className={`${td} tabular-nums`}>{c.last_channel ?? "—"}</td>
                  <td className={`${td} tabular-nums`}>{c.last_rssi ?? "—"}</td>
                  <td className={`${td} tabular-nums`}>{c.frames}</td>
                  <td className={`${td} whitespace-nowrap`}>{timeAgo(c.first_seen)}</td>
                  <td className={`${td} whitespace-nowrap`}>
                    <span className="inline-flex items-center gap-2">
                      {timeAgo(c.last_seen)}
                      {Date.now() - c.last_seen < 120_000 && <Badge t="good">active</Badge>}
                    </span>
                  </td>
                </tr>
              ))}
            </Table>
          )}
        </Async>
      </Panel>
      <p className="mt-3 text-xs text-ink-3">
        WRAITH Nano sees client addresses in 802.11 data-frame headers on the channel it is tuned to (one channel at a
        time, ~250 ms per hop), so coverage is best-effort. Encrypted payloads are not decrypted; only the MAC header is used.
      </p>
    </>
  );
}
