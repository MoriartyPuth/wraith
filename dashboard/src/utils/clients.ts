import type { Client } from "../types/api";

/** Default "recent" window, matching Core's client_window_ms (10 min). */
export const CLIENT_RECENT_MS = 600_000;

/**
 * True for a locally administered unicast MAC (bit 0x02 of the first octet set,
 * multicast bit clear). Phones use these as private / randomised Wi-Fi
 * addresses, so one device can show up as several "clients" over time.
 */
export function isPrivateMac(mac: string): boolean {
  const b = parseInt(mac.slice(0, 2), 16);
  return Number.isFinite(b) && (b & 0x02) !== 0 && (b & 0x01) === 0;
}

export interface NetworkClients {
  /** Group key: the SSID, or the BSSID for hidden networks. */
  key: string;
  ssid: string;
  bssids: string[];
  /** Distinct client addresses, most recently heard first. */
  clients: Client[];
  recent: number;
}

/**
 * Groups client sightings by network (SSID; hidden SSIDs by BSSID). A station
 * seen on several APs of the same network is listed once, with its latest
 * sighting. Networks with recent clients come first, then by size.
 */
export function groupByNetwork(rows: Client[], now: number, windowMs = CLIENT_RECENT_MS): NetworkClients[] {
  const groups = new Map<string, { ssid: string; bssids: Set<string>; byMac: Map<string, Client> }>();
  for (const c of rows) {
    const key = c.ssid || c.bssid;
    let g = groups.get(key);
    if (!g) groups.set(key, (g = { ssid: c.ssid, bssids: new Set(), byMac: new Map() }));
    g.bssids.add(c.bssid);
    const prev = g.byMac.get(c.client);
    if (!prev || c.last_seen > prev.last_seen) g.byMac.set(c.client, c);
  }
  return [...groups].map(([key, g]) => {
    const clients = [...g.byMac.values()].sort((a, b) => b.last_seen - a.last_seen || a.client.localeCompare(b.client));
    return { key, ssid: g.ssid, bssids: [...g.bssids].sort(), clients,
      recent: clients.filter((c) => now - c.last_seen < windowMs).length };
  }).sort((a, b) => b.recent - a.recent || b.clients.length - a.clients.length || a.key.localeCompare(b.key));
}

/** Private (randomised) address: Core's flag, else computed from the address. */
export const clientIsPrivate = (c: Client) => c.private ?? isPrivateMac(c.client);

/** Short display name: the operator's label, else the manufacturer, else null. */
export const clientName = (c: Client): string | null => c.label || c.vendor || null;

/** True when text matches a client's address, network, label or manufacturer info. */
export function clientMatches(c: Client, needle: string): boolean {
  if (!needle) return true;
  return [c.client, c.ssid, c.bssid, c.label, c.vendor, c.vendor_full, c.device_hint]
    .some((v) => v?.toLowerCase().includes(needle));
}

/**
 * A device guess short enough for a chip: only single-category guesses
 * ("camera", "air conditioner"); multi-way guesses ("TV or appliance",
 * "iPhone, iPad, Mac or …") return "" so the chip doesn't overclaim.
 */
export const chipHint = (hint?: string): string =>
  hint && !/,| or /.test(hint) && hint.length <= 24 ? hint : "";
