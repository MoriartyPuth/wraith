import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";

/** Observed APs per 2.4 GHz channel. One series → one hue, no legend. */
export default function ChannelChart({ byChannel }: { byChannel: Record<string, number> }) {
  const data = Array.from({ length: 14 }, (_, i) => ({ ch: i + 1, aps: byChannel[String(i + 1)] ?? 0 }));
  const other = Object.entries(byChannel).filter(([c]) => Number(c) > 14).reduce((a, [, n]) => a + n, 0);
  return (
    <div>
      <div className="h-56" role="img" aria-label="Access points observed per channel">
        <ResponsiveContainer width="100%" height="100%">
          <BarChart data={data} margin={{ top: 8, right: 8, bottom: 0, left: -16 }} barCategoryGap={2}>
            <CartesianGrid vertical={false} stroke="var(--color-line)" strokeDasharray="2 4" />
            <XAxis dataKey="ch" tickLine={false} axisLine={{ stroke: "var(--color-line)" }}
              tick={{ fill: "var(--color-ink-3)", fontSize: 11 }} />
            <YAxis allowDecimals={false} tickLine={false} axisLine={false}
              tick={{ fill: "var(--color-ink-3)", fontSize: 11 }} />
            <Tooltip
              cursor={{ fill: "var(--color-raised)" }}
              contentStyle={{ background: "var(--color-surface)", border: "1px solid var(--color-line)", borderRadius: 6, fontSize: 12 }}
              labelStyle={{ color: "var(--color-ink-2)" }}
              itemStyle={{ color: "var(--color-ink)" }}
              labelFormatter={(c) => `Channel ${c}`}
              formatter={(v) => [v, "APs observed"]}
            />
            <Bar dataKey="aps" fill="var(--color-accent)" radius={[4, 4, 0, 0]} maxBarSize={28} />
          </BarChart>
        </ResponsiveContainer>
      </div>
      <p className="mt-2 text-xs text-ink-3">
        Observed Wi-Fi activity on 2.4 GHz channels 1–14, not RF spectrum analysis.
        {other > 0 && ` ${other} AP(s) on 5/6 GHz channels not shown.`}
      </p>
    </div>
  );
}
