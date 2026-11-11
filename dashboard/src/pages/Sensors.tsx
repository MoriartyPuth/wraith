import { api } from "../services/api";
import { useApi } from "../hooks/useApi";
import { useLiveRefresh } from "../services/live";
import { Async, Badge, Panel, PageHeader, Table, td, mono } from "../components/ui";
import { timeAgo } from "../utils/format";

export default function Sensors() {
  const s = useApi((sig) => api.sensors(100, 0, sig), 5000);
  const prov = useApi((sig) => api.captureStatus(sig), 10000);
  useLiveRefresh(["sensor"], s.reload, 300);
  useLiveRefresh(["provider"], prov.reload, 300);
  return (
    <>
      <PageHeader title="Sensors" subtitle="WRAITH Nano devices that have reported to Core. A sensor is shown disconnected after 15 s without a heartbeat." />
      <Panel>
        <Async state={s} isEmpty={(d) => d.data.length === 0}
          empty="No sensors have connected. Flash WRAITH Nano and start Core with -serial /dev/ttyUSB0.">
          {(d) => (
            <Table head={["Sensor", "Firmware", "Board", "Status", "Last heartbeat", "Channel", "Capabilities", "Rejected / duplicate / lost"]}>
              {d.data.map((x) => (
                <tr key={x.id}>
                  <td className={`${td} ${mono}`}>{x.id}<div className="text-ink-3">boot {x.boot_id ?? "—"}</div></td>
                  <td className={`${td} ${mono}`}>{x.firmware_version ?? "—"}</td>
                  <td className={td}>{x.board ?? "—"}</td>
                  <td className={td}>
                    <span className="flex flex-wrap gap-1">
                      <Badge t={x.connected ? "good" : "bad"}>{x.connected ? "connected" : "disconnected"}</Badge>
                      <Badge t={x.state === "online" ? "good" : x.state === "error" ? "bad" : "warn"}>{x.state}</Badge>
                    </span>
                  </td>
                  <td className={`${td} whitespace-nowrap`}>{timeAgo(x.last_heartbeat)}</td>
                  <td className={`${td} tabular-nums`}>{x.current_channel ?? "—"}</td>
                  <td className={td}>{x.capabilities.join(", ") || "—"}</td>
                  <td className={`${td} tabular-nums`}>{x.rejected_count} / {x.duplicate_count} / {x.gap_count}</td>
                </tr>
              ))}
            </Table>
          )}
        </Async>
      </Panel>
      <Panel title="Serial link" className="mt-4">
        <Async state={prov}>{(d) => (
          <div className="space-y-3">
            {d.data.map((p) => (
              <div key={p.name}>
                <div className="flex flex-wrap items-center gap-2 text-sm">
                  <span className="font-mono text-xs">{p.name}</span>
                  <Badge t={p.state === "running" ? "good" : p.state === "error" ? "bad" : p.state === "degraded" ? "warn" : "neutral"}>{p.state}</Badge>
                  <span className="text-xs text-ink-3">{p.message}</span>
                </div>
                {p.stats && (
                  <dl className="mt-2 flex flex-wrap gap-x-6 gap-y-1 text-xs">
                    {Object.entries(p.stats).map(([k, v]) => (
                      <div key={k}><dt className="inline text-ink-3">{k.replaceAll("_", " ")}: </dt><dd className="inline tabular-nums">{v}</dd></div>
                    ))}
                  </dl>
                )}
              </div>
            ))}
          </div>
        )}</Async>
      </Panel>
    </>
  );
}
