// Centralized WRAITH Core API client. In demo mode every call is answered
// from the labelled demo dataset instead of the network.
import type {
  AccessPoint, ApiErrorBody, BTDevice, BTObservation, BTStatus, BTTransport, Client, EventQuery, Finding, GATTItem, Health,
  Observation, One, Paged, ProviderStatus, Sensor, Statistics, StoredEvent, SystemInfo, TxAccepted, TxRequest,
  WirelessInterface,
} from "../types/api";
import { isDemo } from "./mode";
import * as demo from "./demo";

export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string) {
    super(message);
  }
}

const BASE = "/api/v1";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response;
  try {
    res = await fetch(BASE + path, { ...init, headers: { Accept: "application/json", ...init?.headers } });
  } catch (e) {
    if ((e as Error).name === "AbortError") throw e;
    throw new ApiError(0, "unreachable", "WRAITH Core is unreachable");
  }
  const body: unknown = await res.json().catch(() => null);
  if (!res.ok) {
    const err = (body as ApiErrorBody | null)?.error;
    throw new ApiError(res.status, err?.code ?? "http_error", err?.message ?? `HTTP ${res.status}`);
  }
  return body as T;
}

const get = <T,>(path: string, signal?: AbortSignal) => request<T>(path, { signal });
const page = (limit = 50, offset = 0) => `limit=${limit}&offset=${offset}`;

function paged<T>(all: T[], limit = 50, offset = 0): Paged<T> {
  return { data: all.slice(offset, offset + limit), pagination: { limit, offset, total: all.length } };
}

function eventQuery(q: EventQuery): string {
  const p = new URLSearchParams();
  if (q.types?.length) p.set("type", q.types.join(","));
  if (q.bssid) p.set("bssid", q.bssid);
  if (q.sensor) p.set("sensor", q.sensor);
  if (q.since) p.set("since", String(q.since));
  if (q.until) p.set("until", String(q.until));
  if (q.eventIds?.length) p.set("event_id", q.eventIds.join(","));
  const s = p.toString();
  return s ? "&" + s : "";
}

function demoEvents(q: EventQuery): StoredEvent[] {
  return demo.events.filter((e) =>
    (!q.types?.length || q.types.includes(e.event_type)) &&
    (!q.bssid || e.bssid === q.bssid.toUpperCase()) &&
    (!q.sensor || e.sensor_id === q.sensor) &&
    (!q.since || e.received_at >= q.since) &&
    (!q.until || e.received_at < q.until) &&
    (!q.eventIds?.length || q.eventIds.includes(e.event_id)));
}

const resolve = <T,>(v: T) => Promise.resolve(structuredClone(v));
const notFound = () => Promise.reject(new ApiError(404, "not_found", "resource not found"));

export type SensorView = Sensor;

export const api = {
  health: (s?: AbortSignal) => (isDemo() ? resolve(demo.health) : get<Health>("/health", s)),
  systemInfo: (s?: AbortSignal) => (isDemo() ? resolve(demo.info) : get<SystemInfo>("/system/info", s)),
  statistics: (s?: AbortSignal) => (isDemo() ? resolve(demo.statistics()) : get<Statistics>("/statistics", s)),
  captureStatus: (s?: AbortSignal) =>
    isDemo() ? resolve({ data: demo.providers }) : get<{ data: ProviderStatus[] }>("/capture/status", s),
  captureInterfaces: (s?: AbortSignal) =>
    isDemo() ? resolve({ data: [] as WirelessInterface[] }) : get<{ data: WirelessInterface[] }>("/capture/interfaces", s),

  sensors: (limit?: number, offset?: number, s?: AbortSignal) =>
    isDemo() ? resolve(paged([demo.sensor], limit, offset)) : get<Paged<Sensor>>(`/sensors?${page(limit, offset)}`, s),

  accessPoints: (limit?: number, offset?: number, s?: AbortSignal) =>
    isDemo() ? resolve(paged(demo.aps, limit, offset)) : get<Paged<AccessPoint>>(`/access-points?${page(limit, offset)}`, s),
  accessPoint: (bssid: string, s?: AbortSignal) => {
    if (isDemo()) {
      const a = demo.aps.find((x) => x.bssid === bssid.toUpperCase());
      return a ? resolve({ data: a }) : notFound();
    }
    return get<One<AccessPoint>>(`/access-points/${encodeURIComponent(bssid)}`, s);
  },
  observations: (bssid: string, limit = 500, s?: AbortSignal) =>
    isDemo() ? resolve({ data: demo.observations(bssid.toUpperCase()).slice(0, limit) })
      : get<{ data: Observation[] }>(`/access-points/${encodeURIComponent(bssid)}/observations?limit=${limit}`, s),

  events: (q: EventQuery = {}, limit?: number, offset?: number, s?: AbortSignal) =>
    isDemo() ? resolve(paged(demoEvents(q), limit, offset))
      : get<Paged<StoredEvent>>(`/events?${page(limit, offset)}${eventQuery(q)}`, s),

  clients: (bssid?: string, limit?: number, offset?: number, s?: AbortSignal) => {
    if (isDemo()) {
      const all = demo.clients.filter((c) => !bssid || c.bssid === bssid.toUpperCase());
      return resolve(paged(all, limit, offset));
    }
    const filter = bssid ? `&bssid=${encodeURIComponent(bssid)}` : "";
    return get<Paged<Client>>(`/clients?${page(limit, offset)}${filter}`, s);
  },

  detections: (limit?: number, offset?: number, s?: AbortSignal) =>
    isDemo() ? resolve(paged(demo.findings, limit, offset)) : get<Paged<Finding>>(`/detections?${page(limit, offset)}`, s),
  setFindingStatus: (id: string, status: Finding["status"]) => {
    if (isDemo()) {
      const f = demo.findings.find((x) => x.id === id);
      if (!f) return notFound();
      f.status = status;
      f.updated_at = Date.now();
      return resolve({ data: f });
    }
    return request<One<Finding>>(`/detections/${encodeURIComponent(id)}`, {
      method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ status }),
    });
  },

  // ---- Bluetooth (docs/bluetooth/api.md). Read-only. ----
  btStatus: (s?: AbortSignal) =>
    isDemo() ? resolve({ data: demo.btStatus }) : get<One<BTStatus>>("/bluetooth/status", s),
  btDevices: (q: { transport?: BTTransport; since?: number } = {}, limit?: number, offset?: number, s?: AbortSignal) => {
    if (isDemo()) {
      const all = demo.btDevices.filter((d) => (!q.transport || d.transport === q.transport) && (!q.since || d.last_seen >= q.since));
      return resolve(paged(all, limit, offset));
    }
    const p = new URLSearchParams();
    if (q.transport) p.set("transport", q.transport);
    if (q.since) p.set("since", String(q.since));
    const extra = p.toString();
    return get<Paged<BTDevice>>(`/bluetooth/devices?${page(limit, offset)}${extra ? "&" + extra : ""}`, s);
  },
  btDevice: (id: string, s?: AbortSignal) => {
    if (isDemo()) {
      const d = demo.btDevices.find((x) => x.id === id);
      return d ? resolve({ data: d }) : notFound();
    }
    return get<One<BTDevice>>(`/bluetooth/devices/${encodeURIComponent(id)}`, s);
  },
  btObservations: (id: string, limit = 500, s?: AbortSignal) =>
    isDemo() ? resolve({ data: demo.btObservations(id).slice(0, limit) })
      : get<{ data: BTObservation[] }>(`/bluetooth/devices/${encodeURIComponent(id)}/observations?limit=${limit}`, s),
  btDeviceEvents: (id: string, limit?: number, offset?: number, s?: AbortSignal) => {
    if (isDemo()) {
      const d = demo.btDevices.find((x) => x.id === id);
      const all = d ? demo.events.filter((e) => (e.payload as { metadata?: { address?: string } })?.metadata?.address === d.address) : [];
      return resolve(paged(all, limit, offset));
    }
    return get<Paged<StoredEvent>>(`/bluetooth/devices/${encodeURIComponent(id)}/events?${page(limit, offset)}`, s);
  },
  btGATT: (id: string, what: "services" | "characteristics", s?: AbortSignal) =>
    isDemo() ? resolve({ data: demo.btGATT[id]?.[what] ?? [], truncated: false })
      : get<{ data: GATTItem[]; truncated: boolean }>(`/bluetooth/ble/devices/${encodeURIComponent(id)}/${what}`, s),
  btEvents: (q: EventQuery = {}, limit?: number, offset?: number, s?: AbortSignal) =>
    isDemo() ? resolve(paged(demoEvents(q).filter((e) => e.event_type.startsWith("bluetooth.")), limit, offset))
      : get<Paged<StoredEvent>>(`/bluetooth/events?${page(limit, offset)}${eventQuery({ ...q, bssid: undefined })}`, s),

  // Deauth test transmission (docs/tx.md). Only offered when Core exposes
  // it (system info tx_enabled); demo mode answers 503 to keep the UI honest.
  sendTx: (body: TxRequest) => {
    if (isDemo()) {
      return Promise.reject(new ApiError(503, "tx_disabled", "deauth test transmission is not available in demo mode"));
    }
    return request<One<TxAccepted>>("/tx", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body),
    });
  },
  stopTx: () => {
    if (isDemo()) {
      return Promise.reject(new ApiError(503, "tx_disabled", "deauth test transmission is not available in demo mode"));
    }
    return request<{ data: { stopped: boolean } }>("/tx/stop", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: "{}",
    });
  },
};

/** Fetch all pages of an event query (for export), up to `max` rows. */
export async function fetchAllEvents(q: EventQuery, max = 10000): Promise<{ rows: StoredEvent[]; total: number }> {
  const rows: StoredEvent[] = [];
  let total = 0;
  while (rows.length < max) {
    const r = await api.events(q, Math.min(500, max - rows.length), rows.length);
    total = r.pagination.total;
    rows.push(...r.data);
    if (r.data.length === 0 || rows.length >= total) break;
  }
  return { rows, total };
}

export function wsURL(): string {
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  return `${proto}//${location.host}${BASE}/ws`;
}
