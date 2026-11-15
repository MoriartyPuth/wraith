import { useEffect, useId, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../services/api";
import type { SearchHit } from "../types/api";
import { SEARCH_KIND_LABEL, searchHref } from "../utils/overview";
import { timeAgo } from "../utils/format";

/** Header search: access points (SSID/BSSID), clients (MAC) and Bluetooth
 * devices (name/address). "/" focuses it; arrows + Enter pick a result. */
export function GlobalSearch() {
  const nav = useNavigate();
  const listId = useId();
  const input = useRef<HTMLInputElement>(null);
  const [q, setQ] = useState("");
  const [open, setOpen] = useState(false);
  const [hits, setHits] = useState<SearchHit[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [sel, setSel] = useState(0);

  // "/" focuses search unless typing somewhere else.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement | null;
      if (e.key === "/" && !(t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable))) {
        e.preventDefault();
        input.current?.focus();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // Debounced query; a newer query aborts the older request.
  useEffect(() => {
    const term = q.trim();
    if (term.length < 2) { setHits(null); setErr(null); return; }
    const ctl = new AbortController();
    const t = window.setTimeout(() => {
      api.search(term, ctl.signal)
        .then((r) => { setHits(r.data); setErr(null); setSel(0); })
        .catch((e: Error) => { if (e.name !== "AbortError") setErr(e.message); });
    }, 200);
    return () => { window.clearTimeout(t); ctl.abort(); };
  }, [q]);

  const go = (h: SearchHit) => {
    nav(searchHref(h));
    setOpen(false);
    setQ("");
    input.current?.blur();
  };
  const onKeyDown = (e: React.KeyboardEvent) => {
    const n = hits?.length ?? 0;
    if (e.key === "ArrowDown" && n) { e.preventDefault(); setOpen(true); setSel((sel + 1) % n); }
    else if (e.key === "ArrowUp" && n) { e.preventDefault(); setSel((sel - 1 + n) % n); }
    else if (e.key === "Enter" && hits?.[sel]) { e.preventDefault(); go(hits[sel]!); }
    else if (e.key === "Escape") { setOpen(false); input.current?.blur(); }
  };
  const show = open && q.trim().length >= 2;

  return (
    <div className="relative w-full sm:w-72">
      <input ref={input} value={q} type="search" placeholder="Search AP, client, Bluetooth… ( / )"
        onChange={(e) => { setQ(e.target.value); setOpen(true); }} onFocus={() => setOpen(true)}
        onBlur={() => window.setTimeout(() => setOpen(false), 150)} onKeyDown={onKeyDown}
        role="combobox" aria-expanded={show} aria-controls={listId} aria-autocomplete="list" aria-label="Search everything"
        aria-activedescendant={show && hits?.length ? `${listId}-${sel}` : undefined}
        className="w-full rounded border border-line bg-raised px-2.5 py-1 text-xs text-ink placeholder:text-ink-3 focus:border-accent/60 focus:outline-none" />
      {show && (
        <div className="absolute left-0 right-0 top-full z-20 mt-1 max-h-96 overflow-y-auto rounded-lg border border-line bg-surface shadow-xl sm:left-auto sm:w-[28rem]">
          {err ? <p className="px-3 py-2 text-xs text-bad">Search failed: {err}</p>
            : hits === null ? <p className="px-3 py-2 text-xs text-ink-3">Searching…</p>
              : hits.length === 0 ? <p className="px-3 py-2 text-xs text-ink-3">No match. Names, SSIDs and addresses (any separator, e.g. 70b2a8) are searched.</p>
                : (
                  <ul id={listId} role="listbox" aria-label="Search results">
                    {hits.map((h, i) => (
                      <li key={h.kind + h.id} id={`${listId}-${i}`} role="option" aria-selected={i === sel}
                        onMouseDown={(e) => { e.preventDefault(); go(h); }} onMouseEnter={() => setSel(i)}
                        className={`flex cursor-pointer items-start gap-2 px-3 py-2 text-sm ${i === sel ? "bg-raised" : ""}`}>
                        <span className="mt-0.5 w-16 shrink-0 text-xs text-ink-3">{SEARCH_KIND_LABEL[h.kind]}</span>
                        <span className="min-w-0 flex-1">
                          <span className="block truncate">{h.title}</span>
                          <span className="block truncate font-mono text-xs text-ink-3">{h.detail}</span>
                        </span>
                        <span className="shrink-0 text-xs text-ink-3">{timeAgo(h.last_seen)}</span>
                      </li>
                    ))}
                  </ul>
                )}
        </div>
      )}
    </div>
  );
}
