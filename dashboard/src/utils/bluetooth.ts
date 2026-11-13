// Pure helpers for the Bluetooth pages (unit-tested in bluetooth.test.ts).
// Nothing here guesses: lookups return undefined for unknown ids, and every
// device field is labelled with where its value came from.
import type { BTDevice, BTSensorStatus } from "../types/api";

/** A device seen within this window is shown as "active". */
export const BT_ACTIVE_MS = 120_000;

export function btDeviceHref(id: string): string {
  return `/bluetooth/devices/${encodeURIComponent(id)}`;
}

// Bluetooth SIG Company Identifiers (Assigned Numbers). Deliberately partial:
// only well-known ids; the raw id is always shown alongside.
const COMPANIES: Record<number, string> = {
  0x0001: "Nokia",
  0x0002: "Intel",
  0x0006: "Microsoft",
  0x000a: "Qualcomm Technologies International",
  0x000d: "Texas Instruments",
  0x000f: "Broadcom",
  0x004c: "Apple",
  0x0059: "Nordic Semiconductor",
  0x0075: "Samsung Electronics",
  0x0087: "Garmin",
  0x00e0: "Google",
  0x0157: "Anhui Huami",
  0x02e5: "Espressif",
  0x038f: "Xiaomi",
  0x0499: "Ruuvi Innovations",
};

export function companyName(id: number | null | undefined): string | undefined {
  return id === null || id === undefined ? undefined : COMPANIES[id];
}

export function companyLabel(id: number | null | undefined): string | null {
  if (id === null || id === undefined) return null;
  const hex = `0x${id.toString(16).toUpperCase().padStart(4, "0")}`;
  const name = companyName(id);
  return name ? `${name} (${hex})` : `${hex} (unlisted company id)`;
}

// 16-bit SIG-assigned UUIDs for common services, characteristics and member
// services. Partial on purpose.
const UUIDS: Record<string, string> = {
  "1800": "Generic Access",
  "1801": "Generic Attribute",
  "180a": "Device Information",
  "180d": "Heart Rate",
  "180f": "Battery",
  "1809": "Health Thermometer",
  "1812": "Human Interface Device",
  "1816": "Cycling Speed and Cadence",
  "181a": "Environmental Sensing",
  "2a00": "Device Name",
  "2a01": "Appearance",
  "2a19": "Battery Level",
  "2a24": "Model Number String",
  "2a29": "Manufacturer Name String",
  "2a37": "Heart Rate Measurement",
  "2a6e": "Temperature",
  "fd6f": "Exposure Notification",
  "fe95": "Xiaomi",
  "feaa": "Eddystone (Google)",
};

const SIG_BASE = /^0000([0-9a-f]{4})-0000-1000-8000-00805f9b34fb$/;

/** 16-bit form of a SIG-based UUID ("0000180f-…-00805f9b34fb" → "180f"),
 * otherwise the UUID lowercased. */
export function shortUUID(uuid: string): string {
  const u = uuid.toLowerCase();
  return SIG_BASE.exec(u)?.[1] ?? u;
}

export function uuidName(uuid: string): string | undefined {
  return UUIDS[shortUUID(uuid)];
}

export interface Bucket { label: string; count: number }

/** Devices by last RSSI in 10 dB buckets, strongest first. Devices without
 * an RSSI are counted separately rather than placed in a bucket. */
export function rssiHistogram(devices: Pick<BTDevice, "last_rssi">[]): { buckets: Bucket[]; unknown: number } {
  const buckets: Bucket[] = [
    { label: "≥ −50", count: 0 },
    { label: "−51…−60", count: 0 },
    { label: "−61…−70", count: 0 },
    { label: "−71…−80", count: 0 },
    { label: "−81…−90", count: 0 },
    { label: "< −90", count: 0 },
  ];
  let unknown = 0;
  for (const d of devices) {
    const r = d.last_rssi;
    if (r === null || r === undefined) { unknown++; continue; }
    const i = r >= -50 ? 0 : r >= -60 ? 1 : r >= -70 ? 2 : r >= -80 ? 3 : r >= -90 ? 4 : 5;
    buckets[i]!.count++;
  }
  return { buckets, unknown };
}

/** Where a displayed value comes from. */
export type Provenance = "observed" | "derived" | "inferred" | "unavailable";

export interface FieldRow { label: string; value: string | null; kind: Provenance; note?: string }

const unavailable = (label: string, note: string): FieldRow => ({ label, value: null, kind: "unavailable", note });

/** The device detail view, one row per field, each labelled with its provenance. */
export function deviceFields(d: BTDevice): FieldRow[] {
  const rows: FieldRow[] = [];
  rows.push(d.name
    ? { label: "Name", value: d.name, kind: "observed" }
    : unavailable("Name", d.transport === "classic"
      ? "The device did not return its name to inquiry."
      : "Not present in the advertisements received."));
  rows.push({ label: "Address", value: d.address, kind: "observed" });
  if (d.transport === "ble") {
    rows.push({ label: "Address type", value: d.address_type || "—", kind: "observed",
      note: d.address_type === "random" ? "Random addresses may rotate; this row may not be the same physical device over time." : undefined });
  }
  if (d.transport === "classic") {
    rows.push(d.cod !== null
      ? { label: "Class of Device", value: `0x${d.cod.toString(16).toUpperCase().padStart(6, "0")}`, kind: "observed",
        note: "Advertised by the device itself; a device can advertise any class." }
      : unavailable("Class of Device", "Not reported."));
    rows.push(d.device_class
      ? { label: "Device class", value: d.device_class, kind: "derived", note: "Decoded from the Class of Device above." }
      : unavailable("Device class", "No Class of Device to decode."));
  }
  if (d.transport === "ble") {
    rows.push(d.manufacturer_id !== null
      ? { label: "Manufacturer", value: companyLabel(d.manufacturer_id), kind: "observed",
        note: "Company id from manufacturer-specific advertising data; any device can send any id." }
      : unavailable("Manufacturer", "No manufacturer-specific data advertised."));
    rows.push(d.tx_power !== null
      ? { label: "TX power", value: `${d.tx_power} dBm`, kind: "observed" }
      : unavailable("TX power", "Not advertised."));
    rows.push(d.services.length
      ? { label: "Services", value: d.services.map((u) => uuidName(u) ? `${uuidName(u)} (${shortUUID(u)})` : shortUUID(u)).join(", "), kind: "observed" }
      : unavailable("Services", "No service UUIDs advertised."));
    rows.push(d.classification
      ? { label: "Classification", value: d.classification + (d.confidence !== null ? ` (confidence ${Math.round(d.confidence * 100)}%)` : ""),
        kind: "inferred", note: "The sensor's guess from advertising data, not an identification." }
      : unavailable("Classification", "The sensor made no classification."));
  }
  rows.push(d.last_rssi !== null
    ? { label: "Last RSSI", value: `${d.last_rssi} dBm`, kind: "observed" }
    : unavailable("Last RSSI", d.transport === "classic" ? "Not included in the inquiry results." : "Not reported."));
  if (d.rssi_avg !== null) {
    rows.push({ label: "Average RSSI", value: `${Math.round(d.rssi_avg)} dBm`, kind: "derived", note: "Rolling average of reported RSSI." });
  }
  return rows;
}

export interface Coexistence { tone: "good" | "warn" | "neutral"; text: string }

/** What one sensor's radio mode means for coverage (switch mode: one radio,
 * Wi-Fi or Bluetooth, never both). */
export function coexistence(s: BTSensorStatus | undefined): Coexistence {
  if (!s || s.mode === null) {
    return { tone: "neutral", text: "No Bluetooth status reported. The sensor runs the Wi-Fi-only firmware or has not reported yet; Bluetooth is not being scanned." };
  }
  if (s.mode === "bluetooth") {
    return { tone: "warn", text: "Radio in Bluetooth mode: Wi-Fi monitoring is paused on this sensor until it is switched back (one radio, never both)." };
  }
  return { tone: "good", text: "Radio in Wi-Fi mode: Wi-Fi monitoring is running and Bluetooth is not being scanned. Bluetooth data shown is from earlier Bluetooth sessions." };
}

export function scanStateLabel(state: BTSensorStatus["state"]): string {
  switch (state) {
    case "scanning_ble": return "BLE scanning";
    case "scanning_classic": return "Classic inquiry";
    case "scanning_dual": return "BLE + Classic";
    case "idle": return "idle";
    case "error": return "error";
    default: return "unknown";
  }
}

/** Lowercased search text for one device: name, address, class and (BLE)
 * manufacturer and services, by id and by name. */
export function deviceSearchText(d: BTDevice): string {
  return [d.name, d.address, d.device_class, d.classification, companyLabel(d.manufacturer_id),
    ...d.services.flatMap((u) => [shortUUID(u), uuidName(u)])].filter(Boolean).join(" ").toLowerCase();
}
