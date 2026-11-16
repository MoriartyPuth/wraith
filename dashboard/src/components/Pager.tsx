import { useState } from "react";
import { Button } from "./ui";
import { pageWindow } from "../utils/paging";

/** Rows per page for every list in the dashboard. */
export const PAGE_SIZE = 15;

export interface PagerProps {
  /** Zero-based page index. */
  page: number;
  total: number;
  size?: number;
  onPage: (page: number) => void;
  /** Labels for the previous/next buttons, e.g. "Newer"/"Older" on event lists. */
  prevLabel?: string;
  nextLabel?: string;
}

/**
 * Pagination for a list already in memory: returns the rows of the current
 * page and the props for <Pager>. The page resets to the first one whenever
 * resetKey changes (search text, filter) and is clamped when live updates
 * shrink the list.
 */
export function usePaged<T>(rows: T[], resetKey: string = "", size = PAGE_SIZE) {
  const [page, setPage] = useState(0);
  const [key, setKey] = useState(resetKey);
  if (key !== resetKey) { // adjust state during render (React-documented pattern), no extra effect pass
    setKey(resetKey);
    setPage(0);
  }
  const w = pageWindow(rows.length, key !== resetKey ? 0 : page, size);
  return {
    rows: rows.slice(w.start, w.end),
    pager: { page: w.page, total: rows.length, size, onPage: setPage } satisfies PagerProps,
  };
}

/** "1–15 of 51 · ‹ Prev · Page 1 of 4 · Next ›". Renders nothing for a single page. */
export function Pager({ page, total, size = PAGE_SIZE, onPage, prevLabel = "‹ Prev", nextLabel = "Next ›" }: PagerProps) {
  const w = pageWindow(total, page, size);
  if (w.pages <= 1) return null;
  return (
    <nav aria-label="Pagination" className="mt-3 flex flex-wrap items-center gap-2 text-xs text-ink-3">
      <span className="tabular-nums">{w.start + 1}–{w.end} of {total}</span>
      <span className="ml-auto flex items-center gap-1.5">
        <Button disabled={w.page === 0} onClick={() => onPage(0)} title="First page">«</Button>
        <Button disabled={w.page === 0} onClick={() => onPage(w.page - 1)}>{prevLabel}</Button>
        <span className="px-1 tabular-nums">Page {w.page + 1} of {w.pages}</span>
        <Button disabled={w.page >= w.pages - 1} onClick={() => onPage(w.page + 1)}>{nextLabel}</Button>
        <Button disabled={w.page >= w.pages - 1} onClick={() => onPage(w.pages - 1)} title="Last page">»</Button>
      </span>
    </nav>
  );
}
