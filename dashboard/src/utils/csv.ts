// CSV export. Cells are RFC 4180-quoted. Values starting with a formula
// trigger (= + - @ tab CR) are prefixed with ' so spreadsheet apps show them
// as text: SSIDs and other observed strings are attacker-controlled, and an
// AP named "=HYPERLINK(...)" must not execute when an export is opened.

const FORMULA_START = /^[=+\-@\t\r]/;

export function csvCell(v: unknown): string {
  if (v === null || v === undefined) return "";
  let s = typeof v === "string" ? v : typeof v === "object" ? JSON.stringify(v) : String(v);
  if (typeof v === "string" && FORMULA_START.test(s)) s = "'" + s;
  return /[",\r\n]/.test(s) || s !== s.trim() ? `"${s.replace(/"/g, '""')}"` : s;
}

export function toCSV(header: string[], rows: unknown[][]): string {
  return [header, ...rows].map((r) => r.map(csvCell).join(",")).join("\r\n") + "\r\n";
}

export function download(filename: string, content: string, type: string): void {
  const url = URL.createObjectURL(new Blob([content], { type }));
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
