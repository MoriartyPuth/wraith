// Demo mode: fabricated sample data for exploring the UI without a sensor.
// Off by default. Enabled from Settings or with ?demo=1 (?demo=0 disables).
const KEY = "wraith.demo";

export function initModeFromURL(): void {
  try {
    const p = new URLSearchParams(location.search).get("demo");
    if (p === "1") localStorage.setItem(KEY, "1");
    if (p === "0") localStorage.removeItem(KEY);
  } catch { /* storage unavailable: stay live */ }
}

export function isDemo(): boolean {
  try {
    return localStorage.getItem(KEY) === "1";
  } catch {
    return false;
  }
}

export function setDemo(on: boolean): void {
  try {
    if (on) localStorage.setItem(KEY, "1");
    else localStorage.removeItem(KEY);
  } catch { /* ignore */ }
  location.assign(location.pathname); // reload without query params
}
