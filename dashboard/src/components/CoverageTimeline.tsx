import { useState } from "react";
import { api } from "../services/api";
import { useApi } from "../hooks/useApi";
import { useLiveRefresh } from "../services/live";
import { Async } from "./ui";
import type { CoverageSegment } from "../types/api";
import { coverageSummary, hourTicks, layoutCoverage, segmentLabel } from "../utils/overview";

const WINDOWS = [
  { h: 6, label: "6 h", tickH: 1 },
  { h: 24, label: "24 h", tickH: 3 },
  { h: 168, label: "7 d", tickH: 24 },
];

const SWATCH: Record<string, string> = {
  wifi: "bg-good/70",
  bluetooth: "bg-accent/80",
  classic: "bg-warn/80",
  no_data: "bg-ink-3/25",
};

function swatch(s: CoverageSegment): string {
  return s.kind === "bluetooth" && s.classic ? SWATCH.classic! : SWATCH[s.kind]!;
}

/** One bar per sensor: what its single radio was doing over the window.
 * While it is on Bluetooth, Wi-Fi is not monitored, and vice versa; "no
 * data" means no heartbeat reached Core (sensor offline or Core stopped). */
export function CoverageTimeline() {
  const [win, setWin] = useState(WINDOWS[1]!);
  const cov = useApi((s) => {
    const until = Date.now();
    return api.coverage(until - win.h * 3_600_000, until, s);
  }, 60000, [win.h]);
  useLiveRefresh(["bt_status"], cov.reload, 5000);
  const fmt = (ms: number) => new Date(ms).toLocaleTimeString([], win.h > 24
    ? { weekday: "short", hour: "2-digit", minute: "2-digit" } : { hour: "2-digit", minute: "2-digit" });

  return (
    <div>
      <div className="mb-3 flex flex-wrap items-center gap-3 text-xs text-ink-3">
        <span className="flex items-center gap-1.5"><i className={`inline-block h-2.5 w-3 rounded-sm ${SWATCH.wifi}`} />Wi-Fi monitoring</span>
        <span className="flex items-center gap-1.5"><i className={`inline-block h-2.5 w-3 rounded-sm ${SWATCH.bluetooth}`} />Bluetooth (BLE, passive)</span>
        <span className="flex items-center gap-1.5"><i className={`inline-block h-2.5 w-3 rounded-sm ${SWATCH.classic}`} />Bluetooth + Classic (transmits)</span>
        <span className="flex items-center gap-1.5"><i className={`inline-block h-2.5 w-3 rounded-sm ${SWATCH.no_data}`} />No data</span>
        <span className="ml-auto flex gap-1" role="group" aria-label="Time window">
          {WINDOWS.map((w) => (
            <button key={w.h} onClick={() => setWin(w)} aria-pressed={w.h === win.h}
              className={`rounded border px-2 py-0.5 ${w.h === win.h ? "border-accent/60 text-ink" : "border-line hover:bg-raised"}`}>{w.label}</button>
          ))}
        </span>
      </div>
      <Async state={cov} what="coverage" isEmpty={(d) => d.data.sensors.length === 0} empty="No sensor was active in this window.">
        {(d) => {
          const { since, until } = d.data;
          const ticks = hourTicks(since, until, win.tickH);
          return (
            <div className="space-y-3">
              {d.data.sensors.map((sc) => {
                const bars = layoutCoverage(sc.segments, since, until, fmt);
                const summary = coverageSummary(sc.totals_ms);
                return (
                  <div key={sc.sensor_id}>
                    <div className="mb-1 flex flex-wrap items-baseline justify-between gap-2 text-xs">
                      <span className="font-mono text-ink-2">{sc.sensor_id}</span>
                      <span className="text-ink-3">{summary}</span>
                    </div>
                    <div className="relative h-5 overflow-hidden rounded bg-raised" role="img"
                      aria-label={`${sc.sensor_id} radio coverage, last ${win.label}: ${summary}`}>
                      {bars.map((b) => (
                        <div key={b.seg.from} title={b.label} className={`absolute inset-y-0 ${swatch(b.seg)}`}
                          style={{ left: `${b.left}%`, width: b.seg.to - b.seg.from >= 5000 ? `max(${b.width}%, 2px)` : `${b.width}%` }} />
                      ))}
                    </div>
                    <ul className="sr-only">
                      {bars.filter((b) => b.seg.kind !== "wifi").map((b) => <li key={b.seg.from}>{b.label}</li>)}
                    </ul>
                  </div>
                );
              })}
              <div className="relative h-4 overflow-hidden text-[10px] text-ink-3" aria-hidden>
                {ticks.map((t, i) => (
                  <span key={t.at} style={{ left: `${t.pct}%` }}
                    className={`absolute tabular-nums ${t.pct < 6 ? "" : t.pct > 94 ? "-translate-x-full" : "-translate-x-1/2"} ${i % 2 ? "hidden sm:inline" : ""}`}>
                    {new Date(t.at).toLocaleTimeString([], win.h > 24 ? { weekday: "short" } : { hour: "2-digit", minute: "2-digit" })}
                  </span>
                ))}
              </div>
              <p className="text-xs text-ink-3">
                The sensor has one radio: Wi-Fi is not monitored during Bluetooth time and Bluetooth is not scanned during Wi-Fi time, so no
                events from a radio in a period it was off means nothing. Hover a segment for its times. {segmentLabel({ from: 0, to: 0, kind: "no_data" })}: no
                heartbeat for over {Math.round(d.data.gap_threshold_ms / 1000)} s (sensor offline, link down or Core not running).
              </p>
            </div>
          );
        }}
      </Async>
    </div>
  );
}
