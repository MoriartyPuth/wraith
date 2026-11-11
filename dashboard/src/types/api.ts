// Typed mirrors of WRAITH Core /api/v1 responses (core/internal/api, storage).

export interface ApiErrorBody { error: { code: string; message: string } }
export interface Pagination { limit: number; offset: number; total: number }
export interface Paged<T> { data: T[]; pagination: Pagination }
export interface One<T> { data: T }

export interface Health {
  status: "ok" | "degraded";
  time: number;
  checks: Record<string, string>;
}

export interface SystemInfo {
  name: string; version: string; go_version: string; os: string; kernel: string;
  hostname: string; started_at: number; uptime_seconds: number;
  schema_version: number; protocol_version: number; tx_enabled: boolean;
}

// Deauth test transmission (docs/tx.md): POST /api/v1/tx.
export interface TxRequest {
  kind: "deauth" | "disassoc";
  bssid: string;
  client: string;
  reason?: number;
  count?: number;
  interval_ms?: number;
  confirm: boolean;
}

export interface TxAccepted {
  kind: string;
  bssid: string;
  client: string;
  reason: number;
  count: number;
  interval_ms: number;
  channel: number;
  note: string;
}

export type ProviderState = "unavailable" | "stopped" | "starting" | "running" | "degraded" | "error";
export interface ProviderStatus {
  name: string; state: ProviderState; message: string; since: string; implemented: boolean;
  stats?: Record<string, number>;
}

export interface WirelessInterface {
  name: string; phy: string; driver: string; mode: string;
  supported_modes: string[] | null; monitor_capable: boolean; monitor_concurrent: boolean;
}

export interface Sensor {
  id: string; kind: string; firmware_version: string | null; board: string | null;
  state: string; capabilities: string[]; boot_id: string | null;
  first_seen: number; last_seen: number; last_heartbeat: number | null;
  current_channel: number | null; rejected_count: number; duplicate_count: number; gap_count: number;
  connected: boolean;
}

export type PMF = "required" | "capable" | "not_advertised" | "unavailable";
export interface AccessPoint {
  bssid: string; ssid: string; ssid_hex: string; first_seen: number; last_seen: number;
  last_channel: number | null; last_rssi: number | null; auth_mode: string | null;
  rsn: unknown; pmf: PMF; last_sensor: string | null;
}

// A station seen exchanging data with an AP (protocol v3). A sighting, not
// guaranteed association state.
export interface Client {
  bssid: string; client: string; ssid: string;
  first_seen: number; last_seen: number; frames: number;
  last_rssi: number | null; last_channel: number | null; last_sensor: string | null;
}

export interface StoredEvent {
  id: number; event_id: string; sensor_id: string; source: string; event_type: string;
  bssid: string | null; channel: number | null; sensor_ts: number; received_at: number;
  payload: unknown;
}

export type Severity = "info" | "low" | "medium" | "high";
export type Classification = "observed" | "suspicious" | "potential_attack" | "confirmed";
export interface Finding {
  id: string; category: string; severity: Severity; confidence: number;
  classification: Classification; bssid: string | null; sensor_id: string | null;
  explanation: string; limitations: string; evidence: string[]; status: "open" | "reviewed" | "dismissed";
  created_at: number; updated_at: number;
}

export interface Statistics {
  sensors: number; access_points: number; events: number; findings: number; open_findings: number;
  aps_by_channel: Record<string, number>; events_by_type: Record<string, number>;
  runtime: Record<string, number> | null;
}

/** Live stream envelope from /api/v1/ws. */
export interface StreamMessage {
  type: "hello" | "event" | "access_point" | "client" | "finding" | "sensor" | "provider";
  time: number;
  data: unknown;
}

export interface Observation {
  sensor_id: string; channel: number | null; rssi: number | null; observed_at: number;
}

export interface RSNInfo {
  version: number; group_cipher: string; pairwise: string[]; akm: string[]; mfpc: boolean; mfpr: boolean;
}

export interface EventQuery {
  types?: string[]; bssid?: string; sensor?: string; since?: number; until?: number; eventIds?: string[];
}
