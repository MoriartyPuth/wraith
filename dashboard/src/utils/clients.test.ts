import { describe, expect, it } from "vitest";
import type { Client } from "../types/api";
import { chipHint, clientIsPrivate, clientMatches, clientName, groupByNetwork, isPrivateMac } from "./clients";

const c = (bssid: string, client: string, ssid: string, last_seen: number): Client => ({
  bssid, client, ssid, first_seen: 0, last_seen, frames: 1, last_rssi: null, last_channel: null, last_sensor: null,
});

describe("isPrivateMac", () => {
  it("flags locally administered unicast addresses only", () => {
    expect(isPrivateMac("32:B4:EC:04:82:C5")).toBe(true);
    expect(isPrivateMac("F6:C1:32:28:B8:85")).toBe(true);
    expect(isPrivateMac("FC:B0:DE:86:8F:E3")).toBe(false); // vendor (OUI) address
    expect(isPrivateMac("64:24:4D:11:27:AA")).toBe(false);
    expect(isPrivateMac("33:33:00:00:00:01")).toBe(false); // multicast
    expect(isPrivateMac("zz")).toBe(false);
  });
});

describe("groupByNetwork", () => {
  const now = 1_000_000;
  it("groups by SSID, dedups roaming stations, counts recent, orders by activity", () => {
    const rows = [
      c("AA:00:00:00:00:01", "11:00:00:00:00:01", "Home", now - 1_000),
      c("AA:00:00:00:00:02", "11:00:00:00:00:01", "Home", now - 5_000), // same station, other AP
      c("AA:00:00:00:00:02", "11:00:00:00:00:02", "Home", now - 700_000), // old
      c("BB:00:00:00:00:01", "22:00:00:00:00:01", "Cafe", now - 900_000),
      c("BB:00:00:00:00:01", "22:00:00:00:00:02", "Cafe", now - 800_000),
      c("CC:00:00:00:00:01", "33:00:00:00:00:01", "", now - 2_000), // hidden
    ];
    const g = groupByNetwork(rows, now, 600_000);
    expect(g.map((x) => x.key)).toEqual(["Home", "CC:00:00:00:00:01", "Cafe"]);
    expect(g[0]!.bssids).toEqual(["AA:00:00:00:00:01", "AA:00:00:00:00:02"]);
    expect(g[0]!.clients.map((x) => x.client)).toEqual(["11:00:00:00:00:01", "11:00:00:00:00:02"]);
    expect(g[0]!.clients[0]!.bssid).toBe("AA:00:00:00:00:01"); // latest sighting kept
    expect(g[0]!.recent).toBe(1);
    expect(g[2]!.recent).toBe(0);
    expect(g[2]!.clients[0]!.client).toBe("22:00:00:00:00:02");
  });
  it("handles no rows", () => expect(groupByNetwork([], now)).toEqual([]));
});

describe("client naming", () => {
  const base = c("AA:00:00:00:00:01", "8C:98:6B:26:DD:59", "Home", 0);
  it("prefers the label, then the maker", () => {
    expect(clientName({ ...base, label: "my Mac", vendor: "Apple" })).toBe("my Mac");
    expect(clientName({ ...base, label: "", vendor: "Apple" })).toBe("Apple");
    expect(clientName({ ...base })).toBeNull();
  });
  it("uses Core's private flag, falling back to the address bit", () => {
    expect(clientIsPrivate({ ...base, private: true })).toBe(true);
    expect(clientIsPrivate(c("x", "32:B4:EC:04:82:C5", "", 0))).toBe(true);
    expect(clientIsPrivate(base)).toBe(false);
  });
  it("matches search text on label, maker and guess too", () => {
    const x = { ...base, label: "Living room TV", vendor: "LG", vendor_full: "LG Innotek", device_hint: "TV or appliance" };
    for (const q of ["living", "innotek", "appliance", "8c:98", "home"]) expect(clientMatches(x, q)).toBe(true);
    expect(clientMatches(x, "camera")).toBe(false);
    expect(clientMatches(x, "")).toBe(true);
  });
  it("only puts single-category guesses on chips", () => {
    expect(chipHint("camera")).toBe("camera");
    expect(chipHint("air conditioner")).toBe("air conditioner");
    expect(chipHint("TV or appliance")).toBe("");
    expect(chipHint("iPhone, iPad, Mac or other Apple device")).toBe("");
    expect(chipHint(undefined)).toBe("");
  });
});
