import { api } from "../services/api";
import { useApi } from "../hooks/useApi";
import { useLiveRefresh } from "../services/live";
import { Async, Panel, PageHeader, Stat, Table, td } from "../components/ui";
import { AuthBadge, BssidLink, PmfBadge } from "../components/badges";
import type { PMF, RSNInfo } from "../types/api";

const LEGEND: { pmf: PMF; text: string }[] = [
  { pmf: "required", text: "MFPR=1: clients must use PMF. Unprotected deauthentication frames should be ignored by them." },
  { pmf: "capable", text: "MFPC=1, MFPR=0: PMF is used with clients that support it; others connect without it." },
  { pmf: "not_advertised", text: "No RSN element or MFPC=0: management frames are unprotected for every client." },
  { pmf: "unavailable", text: "Not yet known, malformed RSN element, or the invalid MFPR=1 without MFPC=1." },
];

export default function PMFInspector() {
  const aps = useApi((s) => api.accessPoints(500, 0, s), 30000);
  useLiveRefresh(["access_point"], aps.reload, 5000);
  const list = aps.data?.data ?? [];
  const count = (p: PMF) => list.filter((a) => a.pmf === p).length;

  return (
    <>
      <PageHeader title="PMF Inspector"
        subtitle="Protected Management Frames (802.11w) as advertised in each AP's RSN element. This is what the AP offers, not what any client negotiated." />
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Stat label="PMF required" value={aps.data ? count("required") : "—"} />
        <Stat label="PMF capable" value={aps.data ? count("capable") : "—"} />
        <Stat label="Not advertised" value={aps.data ? count("not_advertised") : "—"} />
        <Stat label="Unknown" value={aps.data ? count("unavailable") : "—"} />
      </div>
      <div className="mt-4 grid gap-4 lg:grid-cols-3">
        <Panel title="What the states mean">
          <dl className="space-y-3 text-sm">
            {LEGEND.map((l) => <div key={l.pmf}><dt><PmfBadge pmf={l.pmf} /></dt><dd className="mt-1 text-ink-2">{l.text}</dd></div>)}
          </dl>
        </Panel>
        <Panel title="Access points" className="lg:col-span-2">
          <Async state={aps} isEmpty={(d) => d.data.length === 0} empty="No access points observed yet.">
            {(d) => (
              <Table head={["Access point", "Security", "PMF", "MFPC", "MFPR", "AKM"]}>
                {[...d.data].sort((a, b) => a.pmf.localeCompare(b.pmf)).map((a) => {
                  const rsn = a.rsn as RSNInfo | null;
                  return (
                    <tr key={a.bssid}>
                      <td className={td}><BssidLink bssid={a.bssid} ssid={a.ssid} /></td>
                      <td className={td}><AuthBadge mode={a.auth_mode} /></td>
                      <td className={td}><PmfBadge pmf={a.pmf} /></td>
                      <td className={`${td} tabular-nums`}>{rsn ? (rsn.mfpc ? 1 : 0) : "—"}</td>
                      <td className={`${td} tabular-nums`}>{rsn ? (rsn.mfpr ? 1 : 0) : "—"}</td>
                      <td className={td}>{rsn?.akm.join(", ") || "—"}</td>
                    </tr>
                  );
                })}
              </Table>
            )}
          </Async>
        </Panel>
      </div>
    </>
  );
}
