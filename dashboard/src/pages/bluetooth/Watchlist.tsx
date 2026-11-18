import { useState } from "react";
import { Link } from "react-router-dom";
import { api, ApiError } from "../../services/api";
import { useApi } from "../../hooks/useApi";
import { useLiveRefresh } from "../../services/live";
import { Async, Badge, Button, Input, PageHeader, Panel, Select, Table, Toolbar, td } from "../../components/ui";
import { BTDeviceLink } from "../../components/bluetooth/BTBadges";
import { btDisplayName } from "../../utils/bluetooth";
import { duration, timeAgo } from "../../utils/format";

export default function Watchlist() {
  const list = useApi((s) => api.btWatchlist(s), 30000);
  const devices = useApi((s) => api.btDevices({}, 200, 0, s), 60000);
  useLiveRefresh(["bt_device"], list.reload, 5000);
  const [pick, setPick] = useState("");
  const [label, setLabel] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const watched = new Set((list.data?.data ?? []).map((w) => w.id));
  const options = (devices.data?.data ?? []).filter((d) => !watched.has(d.id));
  const add = async () => {
    if (!pick) return;
    setBusy(true);
    setErr(null);
    try {
      await api.btWatch(pick, label.trim());
      setPick("");
      setLabel("");
      list.reload();
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };
  const remove = async (id: string) => {
    setErr(null);
    try {
      await api.btUnwatch(id);
      list.reload();
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : String(e));
    }
  };

  return (
    <>
      <PageHeader title="Watchlist"
        subtitle="Bluetooth addresses you track. Presence is inferred from sightings: present means heard within 5 minutes; a session ends after 5 minutes of silence. Arrival and departure are recorded as findings." />
      <Toolbar>
        {options.length > 0 ? (
          <>
            <Select label="device" value={pick} onChange={setPick}
              options={[{ value: "", label: "— choose an observed device —" },
                ...options.map((d) => ({ value: d.id, label: `${btDisplayName(d) ?? "(no name)"} · ${d.address}` }))]} />
            <Input value={label} onChange={(e) => setLabel(e.target.value)}
              placeholder="optional name (e.g. my keys)" aria-label="Watchlist name" className="w-56" />
            <Button variant="primary" disabled={!pick || busy} onClick={() => void add()}>Watch</Button>
          </>
        ) : (
          <span className="text-xs text-ink-3">No un-watched devices are in the inventory yet.</span>
        )}
        <span className="ml-auto text-xs text-ink-3">{list.data ? `${list.data.data.length} watched` : ""}</span>
      </Toolbar>
      {err && <p className="mb-3 text-sm text-bad">{err}</p>}
      <Panel>
        <Async state={list} isEmpty={(d) => d.data.length === 0}
          empty="Nothing watched yet. Choose a device above, or use Watch on a device page.">
          {(d) => (
            <Table head={["Name", "Address", "Presence", "Last seen", "Sessions · 24 h", "Dwell · 24 h", ""]}>
              {d.data.map((w) => (
                <tr key={w.id}>
                  <td className={td}>
                    <Link to={`/bluetooth/devices/${encodeURIComponent(w.id)}`} className="hover:text-accent">
                      {w.label || w.device?.name || "(no name)"}
                    </Link>
                    {w.label && w.device?.name && w.device.name !== w.label && (
                      <span className="block text-xs text-ink-3">{w.device.name}</span>
                    )}
                  </td>
                  <td className={td}>
                    {w.device ? <BTDeviceLink d={w.device} />
                      : <Link to={`/bluetooth/devices/${encodeURIComponent(w.id)}`} className="font-mono text-xs hover:text-accent">{w.address}</Link>}
                  </td>
                  <td className={td}>
                    <Badge t={w.present ? "good" : "neutral"}>{w.present ? "present" : "away"}</Badge>
                    {w.state_since !== null && <span className="ml-2 text-xs text-ink-3">since {timeAgo(w.state_since)}</span>}
                  </td>
                  <td className={`${td} whitespace-nowrap`}>{w.last_seen ? timeAgo(w.last_seen) : "—"}</td>
                  <td className={`${td} tabular-nums`}>{w.sessions_24h}</td>
                  <td className={`${td} tabular-nums`}>{duration(Math.round(w.dwell_ms_24h / 1000))}</td>
                  <td className={`${td} text-right`}>
                    <Button onClick={() => void remove(w.id)} title="Stop tracking this address">Remove</Button>
                  </td>
                </tr>
              ))}
            </Table>
          )}
        </Async>
      </Panel>
      <p className="mt-3 text-xs text-ink-3">
        Presence is a heuristic: a device that is silent but still nearby looks "away", and BLE random addresses rotate (an
        address is not a physical device). Findings appear in the Detection Center as <span className="font-mono">bt_watch_arrived</span> /{" "}
        <span className="font-mono">bt_watch_left</span>.
      </p>
    </>
  );
}
