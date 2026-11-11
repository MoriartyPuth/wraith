// Fabricated demo dataset. Every BSSID uses the locally administered 02:DE
// prefix and every SSID/explanation is marked "Demo", so demo data can never
// be mistaken for an observation. Deterministic (seeded PRNG).
import type {
  AccessPoint, Finding, Health, Observation, ProviderStatus, Sensor, Statistics, StoredEvent, SystemInfo,
} from "../types/api";

const T0 = Date.now();
let seed = 7;
const rnd = () => ((seed = (seed * 1103515245 + 12345) % 2147483648) / 2147483648);
const pick = <T,>(xs: T[]): T => xs[Math.floor(rnd() * xs.length)]!;
const hex = (s: string) => Array.from(new TextEncoder().encode(s), (b) => b.toString(16).padStart(2, "0")).join("");
const SENSOR = "nano-demo01";

interface Spec { ssid: string; ch: number; auth: string; pmf: AccessPoint["pmf"]; rssi: number }
const SPECS: Spec[] = [
  { ssid: "Demo-Lab", ch: 6, auth: "wpa2-psk", pmf: "capable", rssi: -42 },
  { ssid: "Demo-Lab", ch: 11, auth: "wpa2-psk", pmf: "capable", rssi: -61 },
  { ssid: "Demo-Office", ch: 1, auth: "wpa2-wpa3-psk", pmf: "capable", rssi: -55 },
  { ssid: "Demo-Secure", ch: 1, auth: "wpa3-sae", pmf: "required", rssi: -67 },
  { ssid: "Demo-Cafe", ch: 6, auth: "open", pmf: "not_advertised", rssi: -74 },
  { ssid: "Demo-Legacy", ch: 4, auth: "wpa-wpa2-psk", pmf: "not_advertised", rssi: -81 },
  { ssid: "", ch: 11, auth: "wpa2-psk", pmf: "not_advertised", rssi: -88 },
  { ssid: "Demo-Guest", ch: 9, auth: "owe", pmf: "required", rssi: -70 },
  { ssid: "Demo-Printer", ch: 6, auth: "wpa2-psk", pmf: "not_advertised", rssi: -79 },
  { ssid: "Demo-Lab", ch: 3, auth: "open", pmf: "not_advertised", rssi: -58 }, // the "twin"
];

const rsnFor = (auth: string, pmf: string) =>
  auth === "open" || auth === "wep" ? null : {
    version: 1, group_cipher: "ccmp-128", pairwise: ["ccmp-128"],
    akm: auth === "wpa3-sae" ? ["sae"] : auth === "owe" ? ["owe"] : auth === "wpa2-wpa3-psk" ? ["psk", "sae"] : ["psk"],
    mfpc: pmf !== "not_advertised", mfpr: pmf === "required",
  };

export const aps: AccessPoint[] = SPECS.map((s, i) => ({
  bssid: `02:DE:00:00:00:${(i + 1).toString(16).padStart(2, "0").toUpperCase()}`,
  ssid: s.ssid, ssid_hex: hex(s.ssid), first_seen: T0 - (3600 - i * 120) * 1000, last_seen: T0 - Math.floor(rnd() * 20000),
  last_channel: s.ch, last_rssi: s.rssi, auth_mode: s.auth, rsn: rsnFor(s.auth, s.pmf), pmf: s.pmf, last_sensor: SENSOR,
}));

let id = 0;
const ev = (type: string, at: number, metadata: object, bssid: string | null = null, channel: number | null = null): StoredEvent => {
  id++;
  const event_id = `demo0001-${id}`;
  return { id, event_id, sensor_id: SENSOR, source: "wraith-nano", event_type: type, bssid, channel, sensor_ts: 0, received_at: at,
    payload: { version: 1, event_id, source: "wraith-nano", sensor_id: SENSOR, event_type: type, timestamp: 0, seq: id, channel, bssid, metadata } };
};

export const events: StoredEvent[] = [];
for (let t = 3600; t >= 0; t -= 30) {
  const at = T0 - t * 1000;
  events.push(ev("sensor.heartbeat", at, { uptime_ms: (3600 - t) * 1000, free_heap: 165000, channel: pick([1, 6, 11]), queue_drops: 0 }));
  const a = pick(aps);
  events.push(ev("wifi.beacon_observed", at + 500, { ssid: a.ssid, ssid_hex: a.ssid_hex, rssi: (a.last_rssi ?? -70) + Math.round(rnd() * 6 - 3), count: 20 + Math.floor(rnd() * 10) }, a.bssid, a.last_channel));
}
const lab = aps[0]!;
const burst: StoredEvent[] = [];
for (let i = 0; i < 6; i++) {
  burst.push(ev("wifi.deauth_observed", T0 - (600 - i) * 1000, { count: 8, window_ms: 1000, src: lab.bssid, dst: "FF:FF:FF:FF:FF:FF", reason_codes: [7], rssi: -45, protected: false }, lab.bssid, 6));
}
events.push(...burst);
events.push(ev("wifi.disassoc_observed", T0 - 1800 * 1000, { count: 1, window_ms: 1000, src: "02:DE:AA:00:00:01", dst: aps[2]!.bssid, reason_codes: [8], rssi: -60, protected: false }, aps[2]!.bssid, 1));
const twin = aps[9]!;
const twinEv = ev("wifi.ap_observed", T0 - 900 * 1000, { ssid: twin.ssid, ssid_hex: twin.ssid_hex, rssi: -58, auth_mode: "open", hidden: false }, twin.bssid, 3);
events.push(twinEv);
events.sort((a, b) => b.received_at - a.received_at);

export const findings: Finding[] = [
  { id: "f-demo00000001", category: "deauth_activity", severity: "medium", confidence: 0.5, classification: "suspicious",
    bssid: lab.bssid, sensor_id: SENSOR, status: "open", created_at: T0 - 595 * 1000, updated_at: T0 - 595 * 1000,
    explanation: `Demo data: 48 deauthentication frame(s) with BSSID ${lab.bssid} in the last 1m0s (threshold 30); reason codes [7]; 48 were addressed to broadcast (all clients).`,
    limitations: "A single ESP32 sensor hops channels and captures only a fraction of frames, so counts are lower bounds. Transmitter addresses are trivially spoofed. APs and clients legitimately send these frames on reboot, roaming and idle timeouts.",
    evidence: burst.map((e) => e.event_id) },
  { id: "f-demo00000002", category: "rogue_ap_indicator", severity: "high", confidence: 0.5, classification: "suspicious",
    bssid: twin.bssid, sensor_id: SENSOR, status: "open", created_at: T0 - 900 * 1000, updated_at: T0 - 900 * 1000,
    explanation: `Demo data: New BSSID ${twin.bssid} advertises SSID "Demo-Lab" with open, while already-known BSSIDs for the same SSID advertise stronger security (best: wpa2-psk): ${aps[0]!.bssid} (wpa2-psk), ${aps[1]!.bssid} (wpa2-psk).`,
    limitations: "Many legitimate setups advertise one SSID from several BSSIDs (mesh, repeaters, guest networks). This is an indicator to investigate, not proof of an evil twin.",
    evidence: [twinEv.event_id] },
  { id: "f-demo00000003", category: "ap_identity_change", severity: "low", confidence: 0.9, classification: "observed",
    bssid: aps[8]!.bssid, sensor_id: SENSOR, status: "reviewed", created_at: T0 - 3000 * 1000, updated_at: T0 - 2900 * 1000,
    explanation: `Demo data: BSSID ${aps[8]!.bssid} changed its SSID from "Demo-Print" to "Demo-Printer".`,
    limitations: "Renaming a network is normally an administrator action.", evidence: [] },
];

export const observations = (bssid: string): Observation[] => {
  const a = aps.find((x) => x.bssid === bssid);
  if (!a) return [];
  return Array.from({ length: 120 }, (_, i) => ({
    sensor_id: SENSOR, channel: a.last_channel, observed_at: T0 - i * 30000,
    rssi: Math.round((a.last_rssi ?? -70) + Math.sin(i / 9) * 4 + (rnd() * 4 - 2)),
  }));
};

export const sensor: Sensor & { connected: boolean } = {
  id: SENSOR, kind: "wraith-nano", firmware_version: "0.1.0", board: "esp32dev", state: "online",
  capabilities: ["serial-json", "wifi-2.4ghz", "promiscuous"], boot_id: "demo0001", first_seen: T0 - 3600000,
  last_seen: T0, last_heartbeat: T0 - 2000, current_channel: 6, rejected_count: 0, duplicate_count: 0, gap_count: 0, connected: true,
};

export const health: Health = { status: "ok", time: T0, checks: { database: "ok" } };
export const info: SystemInfo = { name: "WRAITH Core", version: "0.1.0-demo", go_version: "—", os: "Demo mode", kernel: "—",
  hostname: "demo", started_at: T0 - 3600000, uptime_seconds: 3600, schema_version: 2, protocol_version: 1 };
export const providers: ProviderStatus[] = [{ name: "esp32-serial", state: "running", message: "Demo data (no device)",
  since: new Date(T0 - 3600000).toISOString(), implemented: true, stats: { lines: events.length, valid: events.length, rejected: 0 } }];

export function statistics(): Statistics {
  const byCh: Record<string, number> = {};
  for (const a of aps) if (a.last_channel) byCh[a.last_channel] = (byCh[a.last_channel] ?? 0) + 1;
  const byType: Record<string, number> = {};
  for (const e of events) byType[e.event_type] = (byType[e.event_type] ?? 0) + 1;
  return { sensors: 1, access_points: aps.length, events: events.length, findings: findings.length,
    open_findings: findings.filter((f) => f.status === "open").length, aps_by_channel: byCh, events_by_type: byType, runtime: null };
}
