import { describe, expect, it } from "vitest";
import type { BTDevice, StoredEvent } from "../types/api";
import {
  batteryText, breakdowns, btDeviceHref, btDisplayName, isTracker, btDeviceLabel, classicInquiryOn, isBluetoothFinding, presence, splitFindings, coexistence, companyLabel, deviceFields, deviceSearchText, rssiHistogram, scanStateLabel, shortUUID, uuidName,
} from "./bluetooth";
import { describeEvent } from "./describe";
import * as demo from "../services/demo";

const base: BTDevice = {
  id: "ble:random:11:22:33:44:55:66", transport: "ble", address: "11:22:33:44:55:66", address_type: "random",
  name: null, cod: null, device_class: null, classification: null, confidence: null, manufacturer_id: null,
  tx_power: null, services: [], last_rssi: null, rssi_avg: null, observations: 1, first_seen: 1, last_seen: 2, last_sensor: "n",
};

describe("bluetooth ids and lookups", () => {
  it("encodes device ids into a single path segment", () => {
    expect(btDeviceHref("ble:random:11:22:33:44:55:66")).toBe("/bluetooth/devices/ble%3Arandom%3A11%3A22%3A33%3A44%3A55%3A66");
  });
  it("labels company ids, never guessing unknown ones", () => {
    expect(companyLabel(0x004c)).toBe("Apple (0x004C)");
    expect(companyLabel(0x7fff)).toBe("0x7FFF (unlisted company id)");
    expect(companyLabel(null)).toBeNull();
  });
  it("shortens SIG-based UUIDs only", () => {
    expect(shortUUID("0000180F-0000-1000-8000-00805F9B34FB")).toBe("180f");
    expect(shortUUID("6e400001-b5a3-f393-e0a9-e50e24dcca9e")).toBe("6e400001-b5a3-f393-e0a9-e50e24dcca9e");
    expect(uuidName("0000180f-0000-1000-8000-00805f9b34fb")).toBe("Battery");
    expect(uuidName("6e400001-b5a3-f393-e0a9-e50e24dcca9e")).toBeUndefined();
  });
});

describe("rssiHistogram", () => {
  it("puts boundary values in the documented buckets and counts missing RSSI apart", () => {
    const h = rssiHistogram([-30, -50, -51, -60, -61, -90, -91, -127, null].map((r) => ({ last_rssi: r })));
    expect(h.buckets.map((b) => b.count)).toEqual([2, 2, 1, 0, 1, 2]);
    expect(h.unknown).toBe(1);
  });
  it("handles a crowded environment (100k addresses) correctly", () => {
    const many = Array.from({ length: 100_000 }, (_, i) => ({ last_rssi: i % 10 === 0 ? null : -40 - (i % 60) }));
    const t = performance.now();
    const h = rssiHistogram(many);
    expect(performance.now() - t).toBeLessThan(200);
    expect(h.buckets.reduce((a, b) => a + b.count, 0) + h.unknown).toBe(100_000);
    expect(h.unknown).toBe(10_000);
  });
});

describe("deviceFields provenance", () => {
  const byLabel = (d: BTDevice) => Object.fromEntries(deviceFields(d).map((f) => [f.label, f]));
  it("marks unreported values unavailable instead of inventing them", () => {
    const f = byLabel(base);
    for (const l of ["Name", "Manufacturer", "TX power", "Services", "Classification", "Last RSSI"]) {
      expect(f[l]?.kind, l).toBe("unavailable");
      expect(f[l]?.value, l).toBeNull();
      expect(f[l]?.note, l).toBeTruthy();
    }
    expect(f["Address type"]?.note).toMatch(/rotate/);
  });
  it("separates observed, derived and inferred values", () => {
    const ble = byLabel({ ...base, name: "Band", manufacturer_id: 0x0157, classification: "wearable", confidence: 0.7,
      last_rssi: -60, rssi_avg: -62.4, services: ["0000180d-0000-1000-8000-00805f9b34fb"] });
    expect(ble["Name"]?.kind).toBe("observed");
    expect(ble["Manufacturer"]?.value).toBe("Anhui Huami (0x0157)");
    expect(ble["Classification"]).toMatchObject({ kind: "inferred", value: "wearable (confidence 70%)" });
    expect(ble["Services"]?.value).toBe("Heart Rate (180d)");
    expect(ble["Average RSSI"]).toMatchObject({ kind: "derived", value: "-62 dBm" });
    const classic = byLabel({ ...base, id: "classic:AA:BB:CC:DD:EE:01", transport: "classic", address_type: "",
      cod: 0x240404, device_class: "audio" });
    expect(classic["Class of Device"]).toMatchObject({ kind: "observed", value: "0x240404" });
    expect(classic["Device class"]).toMatchObject({ kind: "derived", value: "audio" });
    expect(classic["Classification"]).toBeUndefined(); // BLE-only field
    expect(classic["Address type"]).toBeUndefined();
  });
});

describe("coexistence and status", () => {
  const s = (mode: "wifi" | "bluetooth" | null) => ({ sensor_id: "n", mode, state: "idle" as const, classic_devices: null,
    ble_devices: null, message: null, updated_at: null, error_count: 0, last_error_code: null, last_error_message: null, last_error_at: null });
  it("says what each radio mode means for Wi-Fi", () => {
    expect(coexistence(s("bluetooth"))).toMatchObject({ tone: "warn" });
    expect(coexistence(s("bluetooth")).text).toMatch(/Wi-Fi monitoring is paused/);
    expect(coexistence(s("wifi")).text).toMatch(/Bluetooth is not being scanned/);
    expect(coexistence(undefined).text).toMatch(/No Bluetooth status/);
    expect(coexistence(s(null)).tone).toBe("neutral");
  });
  it("labels scan states", () => {
    expect(scanStateLabel("scanning_dual")).toBe("BLE + Classic");
    expect(scanStateLabel(null)).toBe("unknown");
  });

  it("classicInquiryOn follows the reported scan state in Bluetooth mode only", () => {
    const base = { sensor_id: "n", mode: "bluetooth", state: "scanning_ble" } as Parameters<typeof classicInquiryOn>[0];
    expect(classicInquiryOn(base)).toBe(false);
    expect(classicInquiryOn({ ...base!, state: "scanning_dual" })).toBe(true);
    expect(classicInquiryOn({ ...base!, state: "scanning_classic" })).toBe(true);
    expect(classicInquiryOn({ ...base!, mode: "wifi", state: "scanning_dual" })).toBe(false);
    expect(classicInquiryOn(undefined)).toBe(false);
  });
});

describe("search", () => {
  it("matches name, address, manufacturer and service names", () => {
    const t = deviceSearchText({ ...base, name: "Band", manufacturer_id: 0x004c, services: ["180f"] });
    for (const q of ["band", "11:22:33", "apple", "battery", "180f"]) expect(t).toContain(q);
  });
});

describe("describeEvent: bluetooth", () => {
  const ev = (event_type: string, metadata: Record<string, unknown>): StoredEvent => ({
    id: 1, event_id: "e-1", sensor_id: "n", source: "wraith-nano", event_type, bssid: null, channel: null,
    sensor_ts: 0, received_at: 0, payload: { metadata } });
  const cases: [string, Record<string, unknown>, RegExp][] = [
    ["bluetooth.classic.device_discovered", { address: "AA:BB:CC:DD:EE:01", device_class: "audio", rssi: -60 }, /Classic AA:BB.*audio.*-60 dBm/],
    ["bluetooth.classic.device_updated", { address: "AA:BB:CC:DD:EE:01", name: "Spk" }, /Classic Spk · class not reported/],
    ["bluetooth.ble.advertisement_observed", { address: "11:22:33:44:55:66", address_type: "random", rssi: -70, count: 4 }, /BLE advertising.*random.*-70 dBm.*4 report/],
    ["bluetooth.ble.device_discovered", { address: "11:22:33:44:55:66", address_type: "public", classification: "beacon" }, /inferred beacon/],
    ["bluetooth.ble.device_updated", { address: "11:22:33:44:55:66", address_type: "public" }, /BLE device 11:22/],
    ["bluetooth.ble.connection_observed", { address: "11:22:33:44:55:66" }, /GATT connection to 11:22/],
    ["bluetooth.ble.connection_lost", { address: "11:22:33:44:55:66", reason: "timeout" }, /ended — timeout/],
    ["bluetooth.ble.gatt_service_discovered", { address: "11:22:33:44:55:66", uuid: "180f" }, /GATT service Battery/],
    ["bluetooth.ble.gatt_characteristic_discovered", { address: "11:22:33:44:55:66", uuid: "2a19", properties: ["read", "notify"] }, /Battery Level \[read, notify\]/],
    ["bluetooth.system.status", { state: "scanning_ble", mode: "bluetooth" }, /BLE scanning · radio mode bluetooth/],
    ["bluetooth.system.error", { code: "inquiry_failed", message: "ESP_FAIL" }, /error inquiry_failed: ESP_FAIL/],
  ];
  for (const [type, m, want] of cases) {
    it(type, () => expect(describeEvent(ev(type, m))).toMatch(want));
  }
  it("tolerates missing metadata", () => {
    for (const [type] of cases) expect(() => describeEvent(ev(type, {}))).not.toThrow();
  });
});

describe("demo Bluetooth data cannot pass for real observations", () => {
  it("uses locally administered 02:DE addresses and Demo names", () => {
    expect(demo.btDevices.length).toBeGreaterThan(0);
    for (const d of demo.btDevices) {
      expect(d.address.startsWith("02:DE:")).toBe(true);
      expect(d.name === null || d.name.startsWith("Demo-")).toBe(true);
      expect(d.id).toBe(d.transport === "classic" ? `classic:${d.address}` : `ble:${d.address_type}:${d.address}`);
    }
    expect(demo.btStatus.devices.total).toBe(demo.btDevices.length);
  });

  it("btDeviceLabel names a finding's Bluetooth device", () => {
    expect(btDeviceLabel("classic:E4:0C:FD:77:F9:84")).toBe("E4:0C:FD:77:F9:84 (Classic)");
    expect(btDeviceLabel("ble:random:C3:11:22:33:44:55")).toBe("C3:11:22:33:44:55 (BLE random)");
    expect(btDeviceLabel("ble:public:00:11:22:33:44:55")).toBe("00:11:22:33:44:55 (BLE public)");
    expect(btDeviceLabel("garbage")).toBe("garbage");
    expect(isBluetoothFinding("ble_advertising_flood") && isBluetoothFinding("bt_identity_change")).toBe(true);
    expect(isBluetoothFinding("deauth_activity") || isBluetoothFinding("rogue_ap_indicator")).toBe(false);
  });

  it("splitFindings separates Wi-Fi and Bluetooth findings", () => {
    const r = splitFindings([
      { category: "rogue_ap_indicator", status: "open", severity: "low" },
      { category: "deauth_activity", status: "dismissed", severity: "high" },
      { category: "ble_advertising_flood", status: "open", severity: "high" },
      { category: "bt_identity_change", status: "reviewed", severity: "low" },
    ]);
    expect(r).toEqual({ wifi: { open: 1, total: 2 }, bluetooth: { open: 1, total: 2 }, openHigh: 1 });
    expect(splitFindings([])).toEqual({ wifi: { open: 0, total: 0 }, bluetooth: { open: 0, total: 0 }, openHigh: 0 });
  });
});

describe("bluetooth breakdowns and presence", () => {
  const dev = (o: Partial<BTDevice>): BTDevice => ({
    id: o.id ?? "x", transport: "ble", address: "AA:BB:CC:00:00:01", address_type: "random", name: null,
    cod: null, device_class: null, classification: null, confidence: null, manufacturer_id: null, tx_power: null,
    services: [], last_rssi: null, rssi_avg: null, observations: 1, first_seen: 0, last_seen: 0, last_sensor: null, ...o,
  });

  it("breaks devices down by transport, address type, manufacturer, class", () => {
    const devices = [
      dev({ transport: "ble", address_type: "random", manufacturer_id: 0x004c, classification: "beacon" }),
      dev({ transport: "ble", address_type: "public", manufacturer_id: 0x004c }),
      dev({ transport: "ble", address_type: "random", manufacturer_id: 0x0006 }),
      dev({ transport: "ble", address_type: "random", manufacturer_id: null }),
      dev({ transport: "classic", address_type: "", device_class: "phone" }),
      dev({ transport: "classic", address_type: "", device_class: "audio" }),
    ];
    const bd = breakdowns(devices);
    const by = Object.fromEntries(bd.map((b) => [b.title, b]));
    expect(by["Transport"]!.categories).toEqual([{ label: "BLE", count: 4 }, { label: "Classic", count: 2 }]);
    expect(by["BLE address type"]!.total).toBe(4);
    expect(by["BLE address type"]!.categories).toEqual([{ label: "Random", count: 3 }, { label: "Public", count: 1 }]);
    // Manufacturer counts only BLE devices that reported one; Apple named, Microsoft named.
    expect(by["BLE manufacturer"]!.total).toBe(3);
    expect(by["BLE manufacturer"]!.categories.map((c) => c.label)).toEqual(["Apple", "Microsoft"]);
    expect(by["Classic device class"]!.categories).toEqual([{ label: "Audio", count: 1 }, { label: "Phone", count: 1 }]);
    expect(by["BLE classification"]!.categories).toEqual([{ label: "Beacon", count: 1 }]);
    expect(breakdowns([])).toEqual([]);
  });

  it("caps long category lists into Other", () => {
    const devices = Array.from({ length: 10 }, (_, i) => dev({ transport: "classic", address_type: "", device_class: `c${i}` }));
    const bd = breakdowns(devices);
    const cls = bd.find((b) => b.title === "Classic device class")!;
    expect(cls.categories).toHaveLength(6);
    expect(cls.categories[5]!.label).toBe("Other");
    expect(cls.categories[5]!.count).toBe(5);
    expect(cls.categories.reduce((a, c) => a + c.count, 0)).toBe(10);
  });

  it("presence keeps recent devices, strongest signal first", () => {
    const now = 1_000_000;
    const devices = [
      dev({ id: "a", last_seen: now - 10_000, last_rssi: -80 }),
      dev({ id: "b", last_seen: now - 5_000, last_rssi: -45 }),
      dev({ id: "c", last_seen: now - 500_000, last_rssi: -40 }), // too old
      dev({ id: "d", last_seen: now - 1_000, last_rssi: null }),  // recent, no RSSI → last
    ];
    const near = presence(devices, now, 60_000);
    expect(near.map((d) => d.id)).toEqual(["b", "a", "d"]);
  });
});

describe("decoded identity helpers", () => {
  const base = { id: "ble:random:02:DE:B7:00:09:01", transport: "ble", address: "02:DE:B7:00:09:01", address_type: "random",
    name: null, cod: null, device_class: null, classification: null, confidence: null, manufacturer_id: 0x004c,
    tx_power: null, services: [], last_rssi: -50, rssi_avg: null, observations: 1, first_seen: 0, last_seen: 0,
    last_sensor: null } as BTDevice;
  const pods = { ...base, identity: { kind: "headphones", label: "AirPods Pro (2nd gen)", source: "Apple Continuity: Proximity Pairing",
    rank: 90, battery: { left: 70, right: 90, case: 80, left_charging: true } } } as BTDevice;
  const tile = { ...base, address: "02:DE:B7:00:09:02", identity: { kind: "tracker", label: "Tile tracker", source: "service UUID 0xfeed (Tile)", rank: 85, tracker: "tile" } } as BTDevice;

  it("names a device by label, then advertised name, then identity", () => {
    expect(btDisplayName({ ...pods, label: "my pods", name: "Pods" })).toBe("my pods");
    expect(btDisplayName({ ...pods, name: "Pods" })).toBe("Pods");
    expect(btDisplayName(pods)).toBe("AirPods Pro (2nd gen)");
    expect(btDisplayName(base)).toBeNull();
  });
  it("formats battery with charging marks, skipping unreported parts", () => {
    expect(batteryText(pods.identity!.battery)).toBe("L 70% ⚡ · R 90% · case 80%");
    expect(batteryText({ case: 40, case_charging: true })).toBe("case 40% ⚡");
    expect(batteryText(undefined)).toBe("");
  });
  it("searches label and identity, flags trackers, breaks down by type", () => {
    expect(deviceSearchText({ ...pods, label: "Kitchen" })).toContain("kitchen");
    expect(deviceSearchText(pods)).toContain("airpods pro");
    expect(deviceSearchText(tile)).toContain("item tracker");
    expect(isTracker(tile)).toBe(true);
    expect(isTracker(pods)).toBe(false);
    const b = breakdowns([pods, tile, base]).find((x) => x.title === "BLE device type");
    expect(b?.total).toBe(2);
    expect(b?.categories.map((c) => c.label).sort()).toEqual(["Headphones & earbuds", "Item tracker"]);
  });
  it("shows identity, battery and tracker state as detail rows", () => {
    const rows = deviceFields(pods);
    expect(rows.find((r) => r.label === "Identified as")?.kind).toBe("derived");
    expect(rows.find((r) => r.label === "Battery")?.value).toBe("L 70% ⚡ · R 90% · case 80%");
    expect(deviceFields(tile).find((r) => r.label === "Tracker state")?.value).toBe("no separated signal");
    expect(deviceFields(base).find((r) => r.label === "Identified as")?.kind).toBe("unavailable");
  });
});
