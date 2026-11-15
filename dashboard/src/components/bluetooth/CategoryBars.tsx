import type { Breakdown } from "../../utils/bluetooth";

const HUE = ["bg-accent/80", "bg-good/70", "bg-warn/75", "bg-accent-strong/70", "bg-ink-2/50", "bg-ink-3/40"];

/** A compact category breakdown as horizontal bars — lighter than a chart for
 * a handful of counts, and the same numbers read out to a screen reader. */
export function CategoryBars({ b }: { b: Breakdown }) {
  const max = Math.max(1, ...b.categories.map((c) => c.count));
  return (
    <div>
      <div className="mb-1 flex items-baseline justify-between gap-2">
        <h3 className="text-xs font-medium uppercase tracking-wide text-ink-3">{b.title}</h3>
        <span className="text-xs text-ink-3">{b.total}</span>
      </div>
      <ul className="space-y-1">
        {b.categories.map((c, i) => (
          <li key={c.label} className="flex items-center gap-2 text-sm" title={c.title}>
            <span className="w-28 shrink-0 truncate text-ink-2">{c.label}</span>
            <span className="relative h-4 flex-1 overflow-hidden rounded bg-raised">
              <span className={`absolute inset-y-0 left-0 ${HUE[i % HUE.length]}`}
                style={{ width: `${Math.max(3, (c.count / max) * 100)}%` }} />
            </span>
            <span className="w-8 shrink-0 text-right tabular-nums text-ink-2">{c.count}</span>
          </li>
        ))}
      </ul>
      {b.note && <p className="mt-1 text-xs text-ink-3">{b.note}</p>}
    </div>
  );
}
