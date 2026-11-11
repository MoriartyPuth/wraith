import { api } from "../services/api";
import { useApi } from "../hooks/useApi";
import { useLiveRefresh } from "../services/live";
import { Async, Panel, PageHeader, Table, td } from "../components/ui";
import { ChannelChart } from "../components/LazyCharts";

export default function ChannelAnalysis() {
  const st = useApi((s) => api.statistics(s), 15000);
  const aps = useApi((s) => api.accessPoints(500, 0, s), 30000);
  useLiveRefresh(["access_point"], st.reload, 5000);
  useLiveRefresh(["access_point"], aps.reload, 5000);

  const rows = Array.from({ length: 14 }, (_, i) => i + 1).map((ch) => {
    const on = (aps.data?.data ?? []).filter((a) => a.last_channel === ch);
    const rssi = on.map((a) => a.last_rssi).filter((r): r is number => r !== null);
    return { ch, n: on.length, strongest: rssi.length ? Math.max(...rssi) : null, open: on.filter((a) => a.auth_mode === "open").length };
  }).filter((r) => r.n > 0);

  return (
    <>
      <PageHeader title="Channel Analysis" subtitle="Access points by last observed channel. Observed Wi-Fi activity only — not RF spectrum analysis; non-Wi-Fi interference is invisible here." />
      <div className="grid gap-4 lg:grid-cols-3">
        <Panel title="APs per channel (2.4 GHz)" className="lg:col-span-2">
          <Async state={st} isEmpty={(d) => Object.keys(d.aps_by_channel).length === 0} empty="No access points observed yet.">
            {(d) => <ChannelChart byChannel={d.aps_by_channel} />}
          </Async>
        </Panel>
        <Panel title="Per channel">
          {rows.length === 0 ? <p className="py-6 text-center text-sm text-ink-3">No data.</p> : (
            <Table head={["Ch", "APs", "Strongest", "Open"]}>
              {rows.map((r) => (
                <tr key={r.ch}>
                  <td className={`${td} tabular-nums`}>{r.ch}</td>
                  <td className={`${td} tabular-nums`}>{r.n}</td>
                  <td className={`${td} tabular-nums`}>{r.strongest !== null ? `${r.strongest} dBm` : "—"}</td>
                  <td className={`${td} tabular-nums`}>{r.open}</td>
                </tr>
              ))}
            </Table>
          )}
          <p className="mt-3 text-xs text-ink-3">2.4 GHz channels are 5 MHz apart but 20 MHz wide: only 1, 6 and 11 do not overlap.</p>
        </Panel>
      </div>
    </>
  );
}
