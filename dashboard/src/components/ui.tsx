import type { ReactNode } from "react";
import { ApiError } from "../services/api";

export function Panel({ title, action, children, className = "" }: {
  title?: string; action?: ReactNode; children: ReactNode; className?: string;
}) {
  return (
    <section className={`rounded-lg border border-line bg-surface ${className}`}>
      {title && (
        <header className="flex items-center justify-between border-b border-line px-4 py-2.5">
          <h2 className="text-sm font-medium text-ink-2">{title}</h2>
          {action}
        </header>
      )}
      <div className="p-4">{children}</div>
    </section>
  );
}

export function PageHeader({ title, subtitle, right }: { title: string; subtitle?: string; right?: ReactNode }) {
  return (
    <div className="mb-6 flex flex-wrap items-end justify-between gap-3">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
        {subtitle && <p className="mt-1 text-sm text-ink-3">{subtitle}</p>}
      </div>
      {right}
    </div>
  );
}

export function Stat({ label, value, hint }: { label: string; value: ReactNode; hint?: string }) {
  return (
    <div className="rounded-lg border border-line bg-surface px-4 py-3">
      <div className="text-xs uppercase tracking-wide text-ink-3">{label}</div>
      <div className="mt-1 text-2xl font-semibold tabular-nums">{value}</div>
      {hint && <div className="mt-0.5 text-xs text-ink-3">{hint}</div>}
    </div>
  );
}

const tone = {
  neutral: "border-line text-ink-2",
  accent: "border-accent/40 text-accent",
  good: "border-good/40 text-good",
  warn: "border-warn/40 text-warn",
  bad: "border-bad/40 text-bad",
};

export function Badge({ children, t = "neutral" }: { children: ReactNode; t?: keyof typeof tone }) {
  return (
    <span className={`inline-flex items-center gap-1 rounded border px-1.5 py-0.5 text-xs font-medium ${tone[t]}`}>
      {children}
    </span>
  );
}

export function Loading({ what = "data" }: { what?: string }) {
  return <p className="animate-pulse py-6 text-center text-sm text-ink-3">Loading {what}…</p>;
}

export function Empty({ children }: { children: ReactNode }) {
  return <p className="py-6 text-center text-sm text-ink-3">{children}</p>;
}

export function ErrorState({ error, onRetry }: { error: Error; onRetry?: () => void }) {
  const unreachable = error instanceof ApiError && error.status === 0;
  return (
    <div className="rounded border border-bad/30 bg-bad/5 px-4 py-3 text-sm">
      <p className="font-medium text-bad">{unreachable ? "Disconnected from WRAITH Core" : "Request failed"}</p>
      <p className="mt-1 text-ink-2">
        {unreachable ? "Start Core (core/: go run ./cmd/wraith) and retry." : error.message}
      </p>
      {onRetry && (
        <button onClick={onRetry} className="mt-2 rounded border border-line px-2 py-1 text-xs text-ink-2 hover:bg-raised">
          Retry
        </button>
      )}
    </div>
  );
}

/** Renders loading / error / empty / content for an API state. */
export function Async<T>({ state, empty, isEmpty, children, what }: {
  state: { data: T | null; error: Error | null; loading: boolean; reload: () => void };
  empty?: ReactNode; isEmpty?: (d: T) => boolean; children: (d: T) => ReactNode; what?: string;
}) {
  if (state.error && !state.data) return <ErrorState error={state.error} onRetry={state.reload} />;
  if (state.loading && !state.data) return <Loading what={what} />;
  if (!state.data) return null;
  if (isEmpty?.(state.data)) return <Empty>{empty ?? "Nothing here yet."}</Empty>;
  return <>{children(state.data)}</>;
}

export function NotImplemented({ phase, children }: { phase: string; children: ReactNode }) {
  return (
    <div className="rounded-lg border border-dashed border-line px-6 py-10 text-center">
      <Badge>NOT IMPLEMENTED · {phase}</Badge>
      <p className="mx-auto mt-3 max-w-lg text-sm text-ink-3">{children}</p>
    </div>
  );
}

export function Table({ head, children }: { head: string[]; children: ReactNode }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-left text-sm">
        <thead>
          <tr className="border-b border-line text-xs uppercase tracking-wide text-ink-3">
            {head.map((h) => <th key={h} className="px-3 py-2 font-medium">{h}</th>)}
          </tr>
        </thead>
        <tbody className="divide-y divide-line">{children}</tbody>
      </table>
    </div>
  );
}

export const td = "px-3 py-2 align-top";
export const mono = "font-mono text-xs whitespace-nowrap";

const inputCls = "rounded border border-line bg-raised px-2.5 py-1.5 text-sm outline-none placeholder:text-ink-3 focus:border-accent";

export function Input(props: React.InputHTMLAttributes<HTMLInputElement>) {
  return <input {...props} className={`${inputCls} ${props.className ?? ""}`} />;
}

export function Select({ value, onChange, options, label }: {
  value: string; onChange: (v: string) => void; options: { value: string; label: string }[]; label: string;
}) {
  return (
    <select aria-label={label} value={value} onChange={(e) => onChange(e.target.value)} className={inputCls}>
      {options.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
    </select>
  );
}

export function Button({ children, onClick, disabled, variant = "default", title }: {
  children: ReactNode; onClick?: () => void; disabled?: boolean; variant?: "default" | "primary"; title?: string;
}) {
  const v = variant === "primary"
    ? "border-accent/50 bg-accent/15 text-ink hover:bg-accent/25"
    : "border-line text-ink-2 hover:bg-raised hover:text-ink";
  return (
    <button type="button" title={title} onClick={onClick} disabled={disabled}
      className={`rounded border px-2.5 py-1 text-xs font-medium disabled:cursor-not-allowed disabled:opacity-40 ${v}`}>
      {children}
    </button>
  );
}

export function Toolbar({ children }: { children: ReactNode }) {
  return <div className="mb-3 flex flex-wrap items-center gap-2">{children}</div>;
}

/** RSSI as a number plus a small strength bar (−100 dBm empty … −30 dBm full). */
export function Rssi({ dbm }: { dbm: number | null }) {
  if (dbm === null) return <span className="text-ink-3">—</span>;
  const pct = Math.max(0, Math.min(100, ((dbm + 100) / 70) * 100));
  return (
    <span className="inline-flex items-center gap-2 tabular-nums">
      <span className="w-12 text-right">{dbm}</span>
      <span className="h-1.5 w-12 overflow-hidden rounded bg-raised" aria-hidden>
        <span className="block h-full rounded bg-accent/70" style={{ width: `${pct}%` }} />
      </span>
    </span>
  );
}
