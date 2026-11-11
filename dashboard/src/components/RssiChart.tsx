import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import type { Observation } from "../types/api";
import { clockTime } from "../utils/format";

/** Signal strength over time for one AP. Single series: one hue, no legend. */
export default function RssiChart({ data }: { data: Observation[] }) {
  const points = data.filter((o) => o.rssi !== null).map((o) => ({ t: o.observed_at, rssi: o.rssi })).reverse();
  return (
    <div className="h-56" role="img" aria-label="Received signal strength over time">
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={points} margin={{ top: 8, right: 12, bottom: 0, left: -12 }}>
          <CartesianGrid vertical={false} stroke="var(--color-line)" strokeDasharray="2 4" />
          <XAxis dataKey="t" type="number" scale="time" domain={["dataMin", "dataMax"]} tickFormatter={clockTime}
            tickLine={false} axisLine={{ stroke: "var(--color-line)" }} tick={{ fill: "var(--color-ink-3)", fontSize: 11 }} minTickGap={40} />
          <YAxis domain={[-100, -20]} tickLine={false} axisLine={false} tick={{ fill: "var(--color-ink-3)", fontSize: 11 }}
            tickFormatter={(v: number) => `${v}`} />
          <Tooltip
            contentStyle={{ background: "var(--color-surface)", border: "1px solid var(--color-line)", borderRadius: 6, fontSize: 12 }}
            labelStyle={{ color: "var(--color-ink-2)" }} itemStyle={{ color: "var(--color-ink)" }}
            labelFormatter={(t) => new Date(Number(t)).toLocaleString()} formatter={(v) => [`${v} dBm`, "RSSI"]} />
          <Line type="monotone" dataKey="rssi" stroke="var(--color-accent)" strokeWidth={2} dot={false} isAnimationActive={false} />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
