/**
 * The window of rows shown for a page: page is clamped into range (a live
 * update can shrink the list under the reader), start/end index the rows.
 */
export function pageWindow(total: number, page: number, size: number) {
  const pages = Math.max(1, Math.ceil(Math.max(0, total) / size));
  const p = Math.min(Math.max(0, Math.floor(page) || 0), pages - 1);
  const start = p * size;
  return { page: p, pages, start, end: Math.min(start + size, Math.max(0, total)) };
}
