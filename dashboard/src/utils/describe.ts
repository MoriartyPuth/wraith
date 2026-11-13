import type { BTSensorStatus, StoredEvent } from "../types/api";
import { scanStateLabel, shortUUID, uuidName } from "./bluetooth";
import { reasonName } from "./reasons";

type Meta = Record<string, unknown>;

function meta(e: StoredEvent): Meta {
  const p = e.payload as { metadata?: Meta } | null;
  return p?.metadata ?? {};
}

const num = (v: unknown) => (typeof v === "number" ? v : undefined);
const str = (v: unknown) => (typeof v === "string" ? v : undefined);
const ssidOf = (m: Meta) => (str(m.ssid) ? str(m.ssid) : "(hidden)");

/** One-line, factual description of an event built only from its payload. */
export function describeEvent(e: StoredEvent): string {
  const m = meta(e);
  switch (e.event_type) {
    case "sensor.heartbeat":
      return `Heartbeat · ${num(m.channel) ? `ch ${m.channel} · ` : ""}heap ${Math.round((num(m.free_heap) ?? 0) / 1024)} KB · drops ${num(m.queue_drops) ?? 0}`;
    case "sensor.status":
      return `Status ${str(m.state) ?? "?"}${str(m.message) ? ` — ${m.message}` : ""}`;
    case "wifi.ap_observed":
      return `AP ${ssidOf(m)} · ${str(m.auth_mode) ?? "?"} · ${num(m.rssi)} dBm`;
    case "wifi.beacon_observed":
      return `Beacons from ${ssidOf(m)} · ${num(m.rssi)} dBm · ${num(m.count) ?? 1} received`;
    case "wifi.pmf_observed":
      return `PMF advertised: ${str(m.pmf)?.replace("_", " ") ?? "?"}`;
    case "wifi.deauth_observed":
    case "wifi.disassoc_observed": {
      const kind = e.event_type === "wifi.deauth_observed" ? "deauthentication" : "disassociation";
      const dst = str(m.dst) === "FF:FF:FF:FF:FF:FF" ? "broadcast" : str(m.dst) ?? "?";
      const reasons = Array.isArray(m.reason_codes)
        ? (m.reason_codes as number[]).map((r) => `${r} (${reasonName(r)})`).join(", ")
        : m.protected ? "encrypted (PMF-protected frame)" : "none";
      return `${num(m.count)} ${kind} frame(s) in ${num(m.window_ms)} ms → ${dst} · reason ${reasons}${m.protected ? " · protected" : ""}`;
    }
    case "wifi.client_observed": {
      const frames = num(m.frames) ?? 1;
      const rssi = num(m.rssi);
      return `Client ${str(m.client) ?? "?"} seen · ${frames} frame(s)${rssi !== undefined ? ` · ${rssi} dBm` : ""}`;
    }
    case "detection.finding":
      return str(m.explanation) ?? "Finding";
    case "tx.ack": {
      const cmd = str(m.command) ?? "tx";
      return `Sensor ${str(m.status) ?? "?"} the ${cmd} command${str(m.detail) ? ` — ${m.detail}` : ""}`;
    }
    case "tx.report": {
      const sent = num(m.sent) ?? 0;
      const asked = num(m.requested) ?? 0;
      const outcome = str(m.outcome) ?? "?";
      const detail: string[] = [`${sent} of ${asked} frame(s) sent`, `outcome ${outcome}`];
      if (num(m.reason) !== undefined) detail.push(`reason ${m.reason} (${reasonName(num(m.reason)!)})`);
      return `${str(m.command) ?? "tx"} burst: ${detail.join(" · ")}`;
    }
    case "bluetooth.classic.device_discovered":
    case "bluetooth.classic.device_updated": {
      const parts = [`Classic ${str(m.name) ?? str(m.address) ?? "?"}`, str(m.device_class) ?? "class not reported"];
      if (num(m.rssi) !== undefined) parts.push(`${m.rssi} dBm`);
      return parts.join(" · ");
    }
    case "bluetooth.ble.advertisement_observed":
      return `BLE advertising: ${str(m.name) ?? str(m.address) ?? "?"} (${str(m.address_type) ?? "?"}) · ${num(m.rssi)} dBm · ${num(m.count) ?? 1} report(s)`;
    case "bluetooth.ble.device_discovered":
    case "bluetooth.ble.device_updated":
      return `BLE device ${str(m.name) ?? str(m.address) ?? "?"} (${str(m.address_type) ?? "?"})` +
        (str(m.classification) ? ` · inferred ${m.classification}` : "");
    case "bluetooth.ble.connection_observed":
      return `GATT connection to ${str(m.address) ?? "?"}`;
    case "bluetooth.ble.connection_lost":
      return `GATT connection to ${str(m.address) ?? "?"} ended${str(m.reason) ? ` — ${m.reason}` : ""}`;
    case "bluetooth.ble.gatt_service_discovered": {
      const u = str(m.uuid) ?? "?";
      return `GATT service ${uuidName(u) ?? shortUUID(u)} on ${str(m.address) ?? "?"}`;
    }
    case "bluetooth.ble.gatt_characteristic_discovered": {
      const u = str(m.uuid) ?? "?";
      const props = Array.isArray(m.properties) ? (m.properties as unknown[]).filter((x) => typeof x === "string").join(", ") : "";
      return `GATT characteristic ${uuidName(u) ?? shortUUID(u)}${props ? ` [${props}]` : ""} on ${str(m.address) ?? "?"}`;
    }
    case "bluetooth.system.status":
      return `Bluetooth ${scanStateLabel((str(m.state) ?? null) as BTSensorStatus["state"])} · radio mode ${str(m.mode) ?? "?"}` +
        (str(m.message) ? ` — ${m.message}` : "");
    case "bluetooth.system.error":
      return `Bluetooth error ${str(m.code) ?? "?"}: ${str(m.message) ?? ""}`;
    default:
      return e.event_type;
  }
}
