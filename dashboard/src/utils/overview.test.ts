import { describe, expect, it } from "vitest";
import type { AccessPoint, BTDevice, Client, CoverageSegment } from "../types/api";
import { coverageSummary, demoSearch, hourTicks, layoutCoverage, searchHref, searchMatcher, spanText } from "./overview";

const fmt = (ms: number) => String(ms);

describe("coverage timeline", () => {
  it("lays segments out as percentages of the window, clipped and labelled", () => {
    const segs: CoverageSegment[] = [
      { from: -100, to: 250, kind: "wifi" },
      { from: 250, to: 250, kind: "no_data" }, // zero width: dropped
      { from: 250, to: 750, kind: "bluetooth", classic: true },
      { from: 750, to: 2000, kind: "no_data" },
    ];
    const bars = layoutCoverage(segs, 0, 1000, fmt);
    expect(bars.map((b) => [b.left, b.width, b.seg.kind])).toEqual([[0, 25, "wifi"], [25, 50, "bluetooth"], [75, 25, "no_data"]]);
    expect(bars[1]!.label).toBe("Bluetooth + Classic inquiry · 250–750 (1s)");
    expect(bars.reduce((a, b) => a + b.width, 0)).toBe(100);
    expect(layoutCoverage(segs, 10, 10, fmt)).toEqual([]);
  });

  it("formats spans and summaries", () => {
    expect(spanText(45_000)).toBe("45s");
    expect(spanText(14 * 60_000)).toBe("14m");
    expect(spanText(65 * 60_000)).toBe("1h 05m");
    expect(spanText(50 * 3_600_000)).toBe("2d 2h");
    expect(spanText(-5)).toBe("0s");
    expect(coverageSummary({ wifi: 22 * 3_600_000, bluetooth: 3_600_000, classic: 25 * 60_000, no_data: 0 }))
      .toBe("Wi-Fi 22h 00m · Bluetooth 1h 00m (Classic 25m)");
    expect(coverageSummary({ wifi: 0, bluetooth: 0, no_data: 30 * 60_000 })).toBe("Wi-Fi 0s · Bluetooth 0s · no data 30m");
  });

  it("places hour ticks on whole hours that are multiples of the step", () => {
    const d = new Date(2026, 10, 16, 10, 20).getTime();
    const ticks = hourTicks(d, d + 6 * 3_600_000, 3);
    expect(ticks.map((t) => new Date(t.at).getHours())).toEqual([12, 15]);
    expect(ticks.every((t) => t.pct >= 0 && t.pct <= 100 && new Date(t.at).getMinutes() === 0)).toBe(true);
  });
});

describe("global search", () => {
  it("links each kind to its page", () => {
    expect(searchHref({ kind: "access_point", id: "AA:BB:CC:00:00:01", title: "", detail: "", last_seen: 0 })).toBe("/access-points/AA%3ABB%3ACC%3A00%3A00%3A01");
    expect(searchHref({ kind: "client", id: "32:B4:EC:04:82:C5", title: "", detail: "", last_seen: 0 })).toBe("/clients?q=32%3AB4%3AEC%3A04%3A82%3AC5");
    expect(searchHref({ kind: "bt_device", id: "classic:E4:0C:FD:77:F9:84", title: "", detail: "", last_seen: 0 })).toBe("/bluetooth/devices/classic%3AE4%3A0C%3AFD%3A77%3AF9%3A84");
  });

  it("matches like Core: text in names, addresses with any separator", () => {
    const m = searchMatcher("70-b2-a8");
    expect(m.addr("38:3E:51:70:B2:A8")).toBe(true);
    expect(m.text("Lab")).toBe(false);
    expect(searchMatcher("lab").addr("AB:00:00:00:00:00")).toBe(false); // "lab" is not hex
    expect(searchMatcher("ab").addr("AB:00:00:00:00:00")).toBe(true);
    expect(searchMatcher("a").addr("AB:00:00:00:00:00")).toBe(false); // too short
    expect(searchMatcher("OPPO").text("oppo a54")).toBe(true);
  });

  it("demo search covers all three inventories, newest first, clients by address only", () => {
    const aps = [{ bssid: "02:DE:00:00:00:01", ssid: "Lab", auth_mode: "wpa2-psk", last_seen: 5 },
      { bssid: "02:DE:00:00:00:02", ssid: "", auth_mode: null, last_seen: 9 }] as unknown as AccessPoint[];
    const clients = [{ bssid: "02:DE:00:00:00:01", client: "DE:AD:00:00:00:01", ssid: "Lab", last_seen: 3 },
      { bssid: "02:DE:00:00:00:02", client: "DE:AD:00:00:00:01", ssid: "", last_seen: 7 }] as unknown as Client[];
    const bt = [{ id: "classic:DE:AD:00:00:00:09", transport: "classic", address: "DE:AD:00:00:00:09", address_type: "", name: "Lab speaker", last_seen: 4 }] as unknown as BTDevice[];
    expect(demoSearch("lab", aps, clients, bt).map((h) => h.kind + ":" + h.title)).toEqual(["access_point:Lab", "bt_device:Lab speaker"]);
    const hits = demoSearch("de:ad", aps, clients, bt);
    expect(hits.map((h) => h.kind)).toEqual(["client", "bt_device"]);
    expect(hits[0]!.detail).toBe("client · last with 02:DE:00:00:00:02"); // newest sighting, hidden SSID -> BSSID
    expect(demoSearch("02de", aps, clients, bt).map((h) => h.title)).toEqual(["(hidden SSID)", "Lab"]);
  });
});
