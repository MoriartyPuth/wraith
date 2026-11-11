import { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { api, ApiError } from "../services/api";
import { useApi } from "../hooks/useApi";
import { Badge, Button, Input, Panel, Select } from "./ui";
import type { Client, TxAccepted } from "../types/api";

const reMAC = /^[0-9A-Fa-f]{2}(:[0-9A-Fa-f]{2}){5}$/;

/**
 * Deauth test transmission card for one access point (docs/tx.md). Rendered
 * only when Core exposes the feature (tx_enabled) and a sensor is connected;
 * the target AP must have been observed on a 2.4 GHz channel.
 */
export function TxCard({ bssid, lastChannel, clients = [] }:
  { bssid: string; lastChannel: number | null; clients?: Client[] }) {
  const info = useApi((s) => api.systemInfo(s), 30000);
  const providers = useApi((s) => api.captureStatus(s), 15000);

  const enabled = info.data?.tx_enabled === true &&
    providers.data?.data.some((p) => p.state === "running" || p.state === "degraded");
  if (!enabled) return null;

  const channelOk = lastChannel !== null && lastChannel >= 1 && lastChannel <= 14;
  if (!channelOk) {
    return (
      <Panel title="Deauth test transmission" className="lg:col-span-3">
        <p className="text-sm text-ink-3">
          This access point has no 2.4 GHz channel in its last observation, so the sensor cannot target it.
        </p>
      </Panel>
    );
  }
  return (
    <Panel title="Deauth test transmission" className="lg:col-span-3">
      <TxForm bssid={bssid} lastChannel={lastChannel} clients={clients} />
    </Panel>
  );
}

function TxForm({ bssid, lastChannel, clients }: { bssid: string; lastChannel: number; clients: Client[] }) {
  const [kind, setKind] = useState<"deauth" | "disassoc">("deauth");
  const [client, setClient] = useState("");
  const [count, setCount] = useState("1");
  const [interval, setInterval] = useState("500");
  const [confirm, setConfirm] = useState(false);
  const [sending, setSending] = useState(false);
  const [result, setResult] = useState<TxAccepted | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [repeating, setRepeating] = useState(false);
  const [everyS, setEveryS] = useState("35");
  const repeatRef = useRef(false);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const clientOk = reMAC.test(client) && client.toUpperCase() !== bssid && client.toUpperCase() !== "FF:FF:FF:FF:FF:FF";
  const countNum = Number(count);
  const intervalNum = Number(interval);
  const valid = clientOk && countNum >= 1 && countNum <= 10 && intervalNum >= 100 && intervalNum <= 10000;
  const everyNum = Number(everyS);
  const repeatOk = Number.isFinite(everyNum) && everyNum > 0;

  useEffect(() => () => {
    repeatRef.current = false;
    if (timerRef.current) clearTimeout(timerRef.current);
  }, []);

  const sendOnce = async (): Promise<boolean> => {
    setSending(true);
    setError(null);
    setNotice(null);
    try {
      const r = await api.sendTx({
        kind, bssid, client: client.toUpperCase(),
        count: countNum, interval_ms: intervalNum, confirm: true,
      });
      setResult(r.data);
      return true;
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e));
      return false;
    } finally {
      setSending(false);
    }
  };

  const stopRepeat = () => {
    repeatRef.current = false;
    if (timerRef.current) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
    setRepeating(false);
  };

  const toggleRepeat = () => {
    if (repeating) {
      stopRepeat();
      return;
    }
    if (!valid || !confirm || !repeatOk) return;
    repeatRef.current = true;
    setRepeating(true);
    setNotice(null);
    const loop = async () => {
      if (!repeatRef.current) return;
      const ok = await sendOnce();
      if (!ok || !repeatRef.current) {
        stopRepeat();
        return;
      }
      timerRef.current = setTimeout(loop, Math.round(Number(everyS) * 1000));
    };
    void loop();
  };

  const stopAll = async () => {
    stopRepeat();
    setError(null);
    setNotice(null);
    try {
      await api.stopTx();
      setNotice("Stop sent — any running burst was aborted.");
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e));
    }
  };

  return (
    <div className="space-y-3">
      <div className="flex items-start gap-3 rounded border border-warn/30 bg-warn/5 px-3 py-2 text-xs text-ink-2">
        <Badge t="warn">AUTHORIZATION REQUIRED</Badge>
        <p className="flex-1">
          This sends {kind === "deauth" ? "deauthentication" : "disassociation"} frames from the sensor radio to
          disconnect a client you control from <span className="font-mono">{bssid}</span> (channel {lastChannel}).
          Use only on networks you own or are explicitly authorized to test — in many jurisdictions this is a
          criminal offence otherwise. See <span className="font-mono">docs/tx.md</span>.
        </p>
      </div>
      <div className="flex flex-wrap items-end gap-3">
        <Select label="frame type" value={kind} onChange={(v) => setKind(v as "deauth" | "disassoc")}
          options={[{ value: "deauth", label: "deauthentication" }, { value: "disassoc", label: "disassociation" }]} />
        <label className="block">
          <span className="mb-1 block text-xs text-ink-3">Client MAC</span>
          <Input placeholder="AA:BB:CC:DD:EE:FF" value={client}
            onChange={(e) => setClient(e.target.value.toUpperCase())} className="w-44 font-mono" />
        </label>
        {clients.length > 0 && (
          <label className="block">
            <span className="mb-1 block text-xs text-ink-3">…or pick an observed client</span>
            <select aria-label="observed client"
              className="rounded border border-line bg-raised px-2.5 py-1.5 text-sm outline-none focus:border-accent"
              onChange={(e) => { if (e.target.value) setClient(e.target.value); }}>
              <option value="">— observed clients —</option>
              {clients.map((c) => (
                <option key={c.client} value={c.client}>
                  {c.client} ({c.ssid || c.bssid}, ch {c.last_channel ?? "?"})
                </option>
              ))}
            </select>
          </label>
        )}
        <label className="block">
          <span className="mb-1 block text-xs text-ink-3">Frames (1-10)</span>
          <Input type="number" min={1} max={10} value={count} onChange={(e) => setCount(e.target.value)} className="w-20" />
        </label>
        <label className="block">
          <span className="mb-1 block text-xs text-ink-3">Spacing (ms, ≥100)</span>
          <Input type="number" min={100} max={10000} step={100} value={interval} onChange={(e) => setInterval(e.target.value)} className="w-24" />
        </label>
        <label className="block">
          <span className="mb-1 block text-xs text-ink-3">Repeat every (s)</span>
          <Input type="number" min={1} max={3600} value={everyS}
            onChange={(e) => setEveryS(e.target.value)} className="w-24" disabled={repeating} />
        </label>
        <label className="flex items-center gap-2 text-sm text-ink-2">
          <input type="checkbox" checked={confirm} onChange={(e) => setConfirm(e.target.checked)} />
          I am authorized to test this network
        </label>
        <Button variant="primary" disabled={!valid || !confirm || sending || repeating} onClick={() => void sendOnce()}>
          {sending && !repeating ? "Sending…" : "Send test burst"}
        </Button>
        <Button variant={repeating ? "default" : "primary"}
          disabled={!repeating && (!valid || !confirm || !repeatOk)}
          onClick={toggleRepeat}>
          {repeating ? "Stop repeating" : "Repeat bursts"}
        </Button>
        <Button onClick={() => void stopAll()} title="Stop repeating and abort a burst already running on the sensor">
          Stop
        </Button>
      </div>
      {repeating && (
        <p className="text-xs text-accent">
          Repeating every {everyS} s until stopped — leave this page open. A request sent while the previous burst
          is still running is rejected by the sensor; keep the interval above the burst length.
        </p>
      )}
      {error && <p className="text-sm text-bad">{error}</p>}
      {notice && <p className="text-sm text-good">{notice}</p>}
      {result && (
        <div className="rounded border border-good/30 bg-good/5 px-3 py-2 text-sm text-ink-2">
          <p>
            Accepted — the sensor will transmit {result.count} {result.kind} frame(s), {result.interval_ms} ms apart,
            on channel {result.channel}.
          </p>
          <p className="mt-1 text-xs text-ink-3">
            Outcome appears as <span className="font-mono">tx.ack</span> / <span className="font-mono">tx.report</span> events —{" "}
            <Link className="text-accent hover:underline" to={`/history?type=tx.ack,tx.report&bssid=${encodeURIComponent(bssid)}`}>
              view in event history
            </Link>.
          </p>
        </div>
      )}
      <p className="text-xs text-ink-3">
        Limits mirror the sensor build: at most 10 frames per burst, ≥100 ms spacing, a cooldown between bursts
        (30 s by default; 0 in the test build), no broadcast clients, and bursts abort if the host link drops.
        The sensor logs every burst as an event.
      </p>
    </div>
  );
}
