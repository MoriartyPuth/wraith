import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import type { Bucket } from "../../utils/bluetooth";

/** Bluetooth devices per 10 dB last-RSSI bucket. One series → one hue, no
 * legend; same marks as ChannelChart. A screen-reader table carries the same
 * numbers. */
export default function RssiHistogram({ buckets, unknown }: { buckets: Bucket[]; unknown: number }) {
  return (
    <div>
      <div className="h-48" role="img" aria-label="Bluetooth devices by last received signal strength">
        <ResponsiveContainer width="100%" height="100%">
          <BarChart data={buckets} margin={{ top: 8, right: 8, bottom: 0, left: -16 }} barCategoryGap={2}>
            <CartesianGrid vertical={false} stroke="var(--color-line)" strokeDasharray="2 4" />
            <XAxis dataKey="label" tickLine={false} axisLine={{ stroke: "var(--color-line)" }}
              tick={{ fill: "var(--color-ink-3)", fontSize: 11 }} interval={0} />
            <YAxis allowDecimals={false} tickLine={false} axisLine={false}
              tick={{ fill: "var(--color-ink-3)", fontSize: 11 }} />
            <Tooltip
              cursor={{ fill: "var(--color-raised)" }}
              contentStyle={{ background: "var(--color-surface)", border: "1px solid var(--color-line)", borderRadius: 6, fontSize: 12 }}
              labelStyle={{ color: "var(--color-ink-2)" }}
              itemStyle={{ color: "var(--color-ink)" }}
              labelFormatter={(l) => `${l} dBm`}
              formatter={(v) => [v, "devices"]}
            />
            <Bar dataKey="count" fill="var(--color-accent)" radius={[4, 4, 0, 0]} maxBarSize={40} />
          </BarChart>
        </ResponsiveContainer>
      </div>
      {/* sr-only on a wrapper: a table's <caption> is drawn outside the table's
          own clip box, so clipping the table alone leaves the caption visible. */}
      <div className="sr-only">
        <table>
          <caption>Devices by last RSSI (dBm)</caption>
          <tbody>
            {buckets.map((b) => <tr key={b.label}><th>{b.label} dBm</th><td>{b.count}</td></tr>)}
            <tr><th>no RSSI reported</th><td>{unknown}</td></tr>
          </tbody>
        </table>
      </div>
      <p className="mt-2 text-xs text-ink-3">
        Last RSSI reported per address, dBm. {unknown > 0 && `${unknown} device(s) reported no RSSI and are not shown. `}
        RSSI depends on distance, walls, transmit power and antenna orientation; it is not a distance measurement.
      </p>
    </div>
  );
}
