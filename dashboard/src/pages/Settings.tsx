import { api } from "../services/api";
import { useApi } from "../hooks/useApi";
import { useLive } from "../services/live";
import { isDemo, setDemo } from "../services/mode";
import { Async, Badge, Button, Panel, PageHeader, Table, td, mono } from "../components/ui";
import type { ProviderStatus } from "../types/api";

export function ProviderList({ providers }: { providers: ProviderStatus[] }) {
  return (
    <ul className="divide-y divide-line text-sm">
      {providers.map((p) => (
        <li key={p.name} className="flex flex-wrap items-start gap-x-3 gap-y-1 py-2">
          <span className="w-28 shrink-0 font-mono text-xs">{p.name}</span>
          <Badge t={p.state === "running" ? "good" : p.state === "error" ? "bad" : p.state === "unavailable" || p.state === "stopped" ? "neutral" : "warn"}>{p.state}</Badge>
          <span className="basis-full text-xs text-ink-3 sm:basis-auto">{p.message}</span>
        </li>
      ))}
    </ul>
  );
}

export default function Settings() {
  const info = useApi((s) => api.systemInfo(s));
  const ifs = useApi((s) => api.captureInterfaces(s));
  const providers = useApi((s) => api.captureStatus(s), 15000);
  const live = useLive();
  const demo = isDemo();
  return (
    <>
      <PageHeader title="Settings" subtitle="System information and dashboard preferences. WRAITH never changes host network configuration." />
      <div className="grid gap-4 lg:grid-cols-2">
        <Panel title="Data source">
          <p className="text-sm text-ink-2">
            {demo ? "The dashboard is showing fabricated demo data. Nothing on screen was observed."
              : "The dashboard is showing live data from WRAITH Core."}
          </p>
          <div className="mt-3 flex items-center gap-3">
            <Badge t={demo ? "warn" : "good"}>{demo ? "DEMO DATA" : "LIVE DATA"}</Badge>
            <Button variant={demo ? "primary" : "default"} onClick={() => setDemo(!demo)}>{demo ? "Switch to live data" : "Switch to demo data"}</Button>
          </div>
          <p className="mt-3 text-xs text-ink-3">Demo mode is a per-browser preference. Demo BSSIDs use the 02:DE prefix and demo exports are labelled DEMO.</p>
        </Panel>
        <Panel title="WRAITH Core">
          <Async state={info}>{(d) => (
            <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-1.5 text-sm">
              {([["Version", d.version], ["Host", d.hostname], ["OS", d.os], ["Kernel", d.kernel], ["Go", d.go_version],
                ["DB schema", `v${d.schema_version}`], ["Protocol", `v${d.protocol_version}`], ["Live stream", live.state]] as const).map(([k, v]) => (
                <div key={k} className="contents"><dt className="text-ink-3">{k}</dt><dd className="font-mono text-xs leading-5">{v}</dd></div>
              ))}
            </dl>
          )}</Async>
        </Panel>
        <Panel title="Capture">
          <Async state={providers}>{(d) => <ProviderList providers={d.data} />}</Async>
        </Panel>
        <Panel title="Host wireless interfaces (informational)">
          <Async state={ifs} isEmpty={(d) => d.data.length === 0} empty="None reported.">{(d) => (
            <Table head={["Interface", "Driver", "Mode"]}>
              {d.data.map((i) => (
                <tr key={i.name}>
                  <td className={`${td} ${mono}`}>{i.name}</td><td className={`${td} ${mono}`}>{i.driver || "—"}</td><td className={td}>{i.mode || "—"}</td>
                </tr>
              ))}
            </Table>
          )}</Async>
          <p className="mt-2 text-xs text-ink-3">WRAITH captures only through WRAITH Nano (ESP32); host interfaces are not used.</p>
        </Panel>
      </div>
    </>
  );
}
