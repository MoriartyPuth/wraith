import { useEffect, useRef, useState } from "react";
import { wsURL } from "../services/api";

export type StreamState = "connecting" | "open" | "closed" | "unsupported";

const MIN_DELAY = 1000;
const MAX_DELAY = 30000;

/**
 * Connects to /api/v1/ws and reconnects with exponential backoff + jitter.
 * While Core is down the hook keeps backing off (max 30 s) and reports
 * "closed"; pages fall back to REST polling.
 */
export function useLiveStream(onMessage?: (msg: unknown) => void, disabled = false): { state: StreamState; retryInMs: number } {
  const [state, setState] = useState<StreamState>("connecting");
  const [retryInMs, setRetry] = useState(0);
  const handler = useRef(onMessage);
  handler.current = onMessage;

  useEffect(() => {
    if (disabled) { setState("closed"); return; }
    if (typeof WebSocket === "undefined") { setState("unsupported"); return; }
    let ws: WebSocket | null = null;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let attempt = 0;
    let disposed = false;

    const connect = () => {
      setState("connecting");
      ws = new WebSocket(wsURL());
      ws.onopen = () => { attempt = 0; setState("open"); setRetry(0); };
      ws.onmessage = (ev) => {
        try { handler.current?.(JSON.parse(ev.data as string)); } catch { /* ignore malformed */ }
      };
      ws.onclose = () => {
        if (disposed) return;
        setState("closed");
        const base = Math.min(MAX_DELAY, MIN_DELAY * 2 ** attempt++);
        const delay = base / 2 + Math.random() * (base / 2);
        setRetry(delay);
        timer = setTimeout(connect, delay);
      };
    };
    connect();
    return () => { disposed = true; clearTimeout(timer); ws?.close(); };
  }, [disabled]);

  return { state, retryInMs };
}
