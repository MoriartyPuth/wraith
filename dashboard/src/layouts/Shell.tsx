import { NavLink, Outlet } from "react-router-dom";
import { Suspense } from "react";
import { useApi } from "../hooks/useApi";
import { api } from "../services/api";
import { useLive } from "../services/live";
import { isDemo, setDemo } from "../services/mode";
import { Loading } from "../components/ui";
import { GlobalSearch } from "../components/GlobalSearch";

type NavItem = { to: string; label: string };
type NavGroup = { title: string | null; short?: string; items: NavItem[] };

// Sidebar: the master overview, then one group per radio, then what both
// share. Group titles are hidden on narrow screens, where `short` prefixes
// the ambiguous labels instead ("Wi-Fi Overview", "BT Events").
export const NAV_GROUPS: NavGroup[] = [
  { title: null, items: [{ to: "/", label: "Overview" }] },
  {
    title: "Wi-Fi", short: "Wi-Fi", items: [
      { to: "/wifi", label: "Overview" },
      { to: "/monitor", label: "Wireless Monitor" },
      { to: "/access-points", label: "Access Points" },
      { to: "/clients", label: "Clients" },
      { to: "/events", label: "Security Events" },
      { to: "/pmf", label: "PMF Inspector" },
      { to: "/channels", label: "Channel Analysis" },
    ],
  },
  {
    title: "Bluetooth", short: "BT", items: [
      { to: "/bluetooth", label: "Overview" },
      { to: "/bluetooth/classic", label: "Classic" },
      { to: "/bluetooth/ble", label: "BLE" },
      { to: "/bluetooth/events", label: "Events" },
    ],
  },
  {
    title: "System", items: [
      { to: "/detections", label: "Detection Center" },
      { to: "/sensors", label: "Sensors" },
      { to: "/history", label: "Event History" },
      { to: "/settings", label: "Settings" },
    ],
  },
];

/** Items whose label only makes sense under their group title. */
const AMBIGUOUS = new Set(["Overview", "Events"]);

const navCls = ({ isActive }: { isActive: boolean }) =>
  `whitespace-nowrap rounded px-3 py-1.5 text-sm ${isActive ? "bg-raised text-ink" : "text-ink-2 hover:bg-raised/60 hover:text-ink"}`;

function Dot({ tone }: { tone: "good" | "warn" | "bad" | "off" }) {
  const c = { good: "bg-good", warn: "bg-warn", bad: "bg-bad", off: "bg-ink-3" }[tone];
  return <span className={`inline-block h-2 w-2 rounded-full ${c}`} aria-hidden />;
}

export function Shell() {
  const demo = isDemo();
  const health = useApi((s) => api.health(s), 10000);
  const live = useLive();
  const providers = useApi((s) => api.captureStatus(s), 10000);
  const degraded = (providers.data?.data ?? []).filter((p) => p.state === "degraded" || p.state === "error");
  const coreOk = health.data?.status === "ok" && !health.error;
  const checking = health.loading && !health.data && !health.error;
  const streamText = live.state === "demo" ? "off (demo)" : live.state === "open" ? "live"
    : live.state === "connecting" ? "connecting" : `reconnecting${live.retryInMs ? ` in ${Math.ceil(live.retryInMs / 1000)}s` : ""}`;

  return (
    <div className="flex h-full min-h-0 flex-col">
      {demo && (
        <div role="status" className="flex flex-wrap items-center justify-center gap-3 bg-warn/15 px-4 py-1.5 text-center text-xs font-medium text-warn">
          DEMO DATA — fabricated sample data, not real observations. Nothing shown here was captured.
          <button onClick={() => setDemo(false)} className="rounded border border-warn/50 px-2 py-0.5 hover:bg-warn/20">Exit demo</button>
        </div>
      )}
      <div className="flex min-h-0 flex-1 flex-col md:flex-row">
        <aside className="shrink-0 border-b border-line bg-surface md:w-56 md:border-b-0 md:border-r">
          <div className="px-4 py-4">
            <div className="text-base font-semibold tracking-[0.2em]">WRAITH</div>
            <div className="text-xs text-ink-3">Observe. Analyze. Understand.</div>
          </div>
          <nav className="flex gap-1 overflow-x-auto px-2 pb-2 md:flex-col md:overflow-visible">
            {NAV_GROUPS.map((g) => (
              <div key={g.title ?? "top"} className="contents">
                {g.title && <div className="hidden px-3 pb-1 pt-4 text-xs uppercase tracking-wide text-ink-3 md:block" aria-hidden>{g.title}</div>}
                {g.items.map((n) => {
                  const prefix = g.short && AMBIGUOUS.has(n.label) ? g.short : null;
                  return (
                    <NavLink key={n.to} to={n.to} end={n.to === "/" || n.to === "/wifi" || n.to === "/bluetooth"} className={navCls}
                      aria-label={prefix ? `${g.title} ${n.label}` : undefined}>
                      {prefix && <span className="md:hidden">{prefix} </span>}{n.label}
                    </NavLink>
                  );
                })}
              </div>
            ))}
          </nav>
        </aside>
        <div className="flex min-h-0 min-w-0 flex-1 flex-col">
          <header className="flex flex-wrap items-center gap-4 border-b border-line px-6 py-2.5 text-xs text-ink-2">
            <span className="flex items-center gap-2"><Dot tone={coreOk ? "good" : checking ? "warn" : "bad"} />
              Core {coreOk ? "connected" : checking ? "checking…" : "unreachable"}</span>
            <span className="flex items-center gap-2"><Dot tone={live.state === "open" ? "good" : live.state === "demo" ? "off" : live.state === "connecting" ? "warn" : "bad"} />
              Stream {streamText}</span>
            <span className="order-last w-full sm:order-none sm:ml-auto sm:w-auto"><GlobalSearch /></span>
            <span className={`ml-auto rounded border px-1.5 py-0.5 font-medium sm:ml-0 ${demo ? "border-warn/50 text-warn" : "border-good/40 text-good"}`}>
              {demo ? "DEMO DATA" : "LIVE DATA"}
            </span>
          </header>
          {degraded.map((p) => (
            <div key={p.name} role="alert" className={`border-b px-6 py-2 text-xs ${p.state === "error" ? "border-bad/30 bg-bad/10 text-bad" : "border-warn/30 bg-warn/10 text-warn"}`}>
              <span className="font-medium">{p.name} {p.state}:</span> {p.message}
            </div>
          ))}
          <main className="min-h-0 flex-1 overflow-y-auto px-4 py-6 md:px-6">
            <Suspense fallback={<Loading what="page" />}><Outlet /></Suspense>
          </main>
        </div>
      </div>
    </div>
  );
}
