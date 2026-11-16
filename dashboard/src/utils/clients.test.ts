import { describe, expect, it } from "vitest";
import type { Client } from "../types/api";
import { groupByNetwork, isPrivateMac } from "./clients";

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
