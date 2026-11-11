// Live stream context: one WebSocket per tab, shared by all pages.
import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useLiveStream, type StreamState } from "../hooks/useLiveStream";
import type { AccessPoint, StoredEvent, StreamMessage } from "../types/api";
import { isDemo } from "./mode";

const FEED_MAX = 200;

type Kind = StreamMessage["type"];
type Listener = (m: StreamMessage) => void;

interface Live {
  state: StreamState | "demo";
  retryInMs: number;
  feed: StoredEvent[]; // newest first
  apUpdates: Map<string, { ap: AccessPoint; at: number }>;
  subscribe: (kinds: Kind[], fn: Listener) => () => void;
}

const Ctx = createContext<Live | null>(null);

export function LiveProvider({ children }: { children: ReactNode }) {
  const demo = isDemo();
  const listeners = useRef(new Set<{ kinds: Kind[]; fn: Listener }>());
  const [feed, setFeed] = useState<StoredEvent[]>([]);
  const [apUpdates, setAPUpdates] = useState(new Map<string, { ap: AccessPoint; at: number }>());

  const stream = useLiveStream(demo ? undefined : (raw) => {
    const m = raw as StreamMessage;
    if (!m || typeof m.type !== "string") return;
    if (m.type === "event") setFeed((f) => [m.data as StoredEvent, ...f].slice(0, FEED_MAX));
    if (m.type === "access_point") {
      const ap = m.data as AccessPoint;
      setAPUpdates((prev) => new Map(prev).set(ap.bssid, { ap, at: Date.now() }));
    }
    for (const l of listeners.current) if (l.kinds.includes(m.type)) l.fn(m);
  }, demo);

  const value = useMemo<Live>(() => ({
    state: demo ? "demo" : stream.state,
    retryInMs: stream.retryInMs,
    feed,
    apUpdates,
    subscribe: (kinds, fn) => {
      const entry = { kinds, fn };
      listeners.current.add(entry);
      return () => { listeners.current.delete(entry); };
    },
  }), [demo, stream.state, stream.retryInMs, feed, apUpdates]);

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useLive(): Live {
  const v = useContext(Ctx);
  if (!v) throw new Error("useLive outside LiveProvider");
  return v;
}

/** Call `reload` (debounced) whenever a message of one of `kinds` arrives. */
export function useLiveRefresh(kinds: Kind[], reload: () => void, debounceMs = 1000): void {
  const { subscribe } = useLive();
  const key = kinds.join(",");
  useEffect(() => {
    let t: ReturnType<typeof setTimeout> | undefined;
    const unsub = subscribe(key.split(",") as Kind[], () => {
      clearTimeout(t);
      t = setTimeout(reload, debounceMs);
    });
    return () => { unsub(); clearTimeout(t); };
  }, [subscribe, key, reload, debounceMs]);
}
