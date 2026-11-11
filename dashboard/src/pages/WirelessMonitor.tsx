import { useMemo, useState } from "react";
import { api } from "../services/api";
import { useApi } from "../hooks/useApi";
import { useLive } from "../services/live";
import { Async, Input, Panel, PageHeader, Rssi, Select, Table, Toolbar, td } from "../components/ui";
import { AuthBadge, BssidLink, PmfBadge } from "../components/badges";
import type { AccessPoint } from "../types/api";
import { timeAgo } from "../utils/format";

type SortKey = "rssi" | "last_seen" | "channel" | "ssid";

export default function WirelessMonitor() {
  const base = useApi((s) => api.accessPoints(500, 0, s), 30000);
  const live = useLive();
  const [q, setQ] = useState("");
  const [ch, setCh] = useState("all");
  const [sec, setSec] = useState("all");
  const [fresh, setFresh] = useState("300");
  const [sort, setSort] = useState<SortKey>("rssi");

  // REST snapshot, overlaid with live access_point updates as they arrive.
  const rows = useMemo(() => {
    const m = new Map<string, { ap: AccessPoint; updated: number }>();
    for (const a of base.data?.data ?? []) m.set(a.bssid, { ap: a, updated: 0 });
    for (const [b, u] of live.apUpdates) {
      const cur = m.get(b);
      if (!cur || u.ap.last_seen >= cur.ap.last_seen) m.set(b, { ap: u.ap, updated: u.at });
    }
    return [...m.values()];
  }, [base.data, live.apUpdates]);

  const now = Date.now();
  const needle = q.trim().toLowerCase();
  const shown = rows
    .filter(({ ap }) => !needle || ap.ssid.toLowerCase().includes(needle) || ap.bssid.toLowerCase().includes(needle))
    .filter(({ ap }) => ch === "all" || String(ap.last_channel) === ch)
    .filter(({ ap }) => sec === "all" || (sec === "weak" ? ["open", "wep", "wpa-psk", "wpa-eap"].includes(ap.auth_mode ?? "") : ap.auth_mode === sec))
    .filter(({ ap }) => fresh === "all" || now - ap.last_seen <= Number(fresh) * 1000)
    .sort((a, b) => sort === "rssi" ? (b.ap.last_rssi ?? -999) - (a.ap.last_rssi ?? -999)
      : sort === "last_seen" ? b.ap.last_seen - a.ap.last_seen
      : sort === "channel" ? (a.ap.last_channel ?? 0) - (b.ap.last_channel ?? 0)
      : a.ap.ssid.localeCompare(b.ap.ssid));

  const channels = [...new Set(rows.map((r) => r.ap.last_channel).filter((c): c is number => c !== null))].sort((a, b) => a - b);
  const auths = [...new Set(rows.map((r) => r.ap.auth_mode).filter((a): a is string => !!a))].sort();

  return (
    <>
      <PageHeader title="Wireless Monitor"
        subtitle="Access points as WRAITH Nano hears them, updated live. 2.4 GHz only; one channel at a time, so some beacons are missed." />
      <Toolbar>
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search SSID or BSSID" aria-label="Search" className="w-60" />
        <Select label="Channel" value={ch} onChange={setCh} options={[{ value: "all", label: "All channels" }, ...channels.map((c) => ({ value: String(c), label: `Channel ${c}` }))]} />
        <Select label="Security" value={sec} onChange={setSec} options={[{ value: "all", label: "Any security" }, { value: "weak", label: "Weak (open/WEP/WPA1)" }, ...auths.map((a) => ({ value: a, label: a }))]} />
        <Select label="Heard within" value={fresh} onChange={setFresh} options={[
          { value: "60", label: "Heard in last 1 min" }, { value: "300", label: "Heard in last 5 min" },
          { value: "3600", label: "Heard in last hour" }, { value: "all", label: "All time" }]} />
        <Select label="Sort" value={sort} onChange={(v) => setSort(v as SortKey)} options={[
          { value: "rssi", label: "Strongest first" }, { value: "last_seen", label: "Most recent" },
          { value: "channel", label: "Channel" }, { value: "ssid", label: "SSID" }]} />
        <span className="ml-auto text-xs text-ink-3">{shown.length} of {rows.length} APs</span>
      </Toolbar>
      <Panel>
        <Async state={base} isEmpty={() => rows.length === 0} empty="No access points observed yet.">
          {() => shown.length === 0 ? <p className="py-6 text-center text-sm text-ink-3">No APs match these filters.</p> : (
            <Table head={["SSID", "BSSID", "Ch", "RSSI (dBm)", "Security", "PMF", "Sensor", "Last heard"]}>
              {shown.map(({ ap, updated }) => (
                <tr key={ap.bssid} className={now - updated < 2500 ? "bg-accent/10 transition-colors" : "transition-colors duration-1000"}>
                  <td className={td}>{ap.ssid || <span className="text-ink-3">(hidden)</span>}</td>
                  <td className={td}><BssidLink bssid={ap.bssid} /></td>
                  <td className={`${td} tabular-nums`}>{ap.last_channel ?? "—"}</td>
                  <td className={td}><Rssi dbm={ap.last_rssi} /></td>
                  <td className={td}><AuthBadge mode={ap.auth_mode} /></td>
                  <td className={td}><PmfBadge pmf={ap.pmf} /></td>
                  <td className={`${td} font-mono text-xs whitespace-nowrap`}>{ap.last_sensor ?? "—"}</td>
                  <td className={`${td} whitespace-nowrap`}>{timeAgo(ap.last_seen)}</td>
                </tr>
              ))}
            </Table>
          )}
        </Async>
      </Panel>
    </>
  );
}
