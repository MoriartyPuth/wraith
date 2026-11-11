import { describe, expect, it } from "vitest";
import { csvCell, toCSV } from "./csv";
import { describeEvent } from "./describe";
import { reasonName } from "./reasons";
import type { StoredEvent } from "../types/api";

describe("csv", () => {
  it("quotes separators, quotes and newlines", () => {
    expect(csvCell('a,"b"\nc')).toBe('"a,""b""\nc"');
    expect(csvCell("plain")).toBe("plain");
    expect(csvCell(null)).toBe("");
    expect(csvCell(42)).toBe("42");
  });
  it("neutralises spreadsheet formulas in observed strings", () => {
    for (const evil of ["=HYPERLINK(\"http://x\")", "+1+1", "-2+3", "@SUM(A1)", "\tcmd", "\rx"]) {
      const out = csvCell(evil);
      expect(out.replace(/^"/, "").startsWith("'")).toBe(true);
    }
  });
  it("does not prefix negative numbers (numeric RSSI)", () => {
    expect(csvCell(-48)).toBe("-48");
  });
  it("serialises objects as JSON and builds rows with CRLF", () => {
    expect(toCSV(["a", "b"], [[1, { x: "y" }]])).toBe('a,b\r\n1,"{""x"":""y""}"\r\n');
  });
});

const ev = (event_type: string, metadata: object): StoredEvent => ({
  id: 1, event_id: "x-1", sensor_id: "nano-1", source: "wraith-nano", event_type, bssid: null, channel: 6,
  sensor_ts: 0, received_at: 0, payload: { metadata },
});

describe("describeEvent", () => {
  it("names reason codes and broadcast", () => {
    const d = describeEvent(ev("wifi.deauth_observed", { count: 4, window_ms: 1000, dst: "FF:FF:FF:FF:FF:FF", reason_codes: [7], protected: false }));
    expect(d).toContain("4 deauthentication");
    expect(d).toContain("broadcast");
    expect(d).toContain("7 (class 3 frame from non-associated station)");
  });
  it("explains why protected frames have no reason", () => {
    expect(describeEvent(ev("wifi.disassoc_observed", { count: 1, window_ms: 1000, protected: true }))).toContain("encrypted");
  });
  it("labels hidden SSIDs", () => {
    expect(describeEvent(ev("wifi.ap_observed", { ssid: "", rssi: -70, auth_mode: "wpa2-psk" }))).toContain("(hidden)");
  });
  it("survives malformed payloads", () => {
    expect(() => describeEvent({ ...ev("wifi.beacon_observed", {}), payload: null })).not.toThrow();
  });
});

describe("reasons", () => {
  it("maps known and unknown codes", () => {
    expect(reasonName(3)).toContain("leaving");
    expect(reasonName(999)).toBe("other");
  });
});
