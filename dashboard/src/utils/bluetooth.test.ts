import { describe, expect, it } from "vitest";
import type { BTDevice, StoredEvent } from "../types/api";
import {
  btDeviceHref, coexistence, companyLabel, deviceFields, deviceSearchText, rssiHistogram, scanStateLabel, shortUUID, uuidName,
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
});
