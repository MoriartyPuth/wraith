// Recharts is large; charts load on demand so the first page paints fast.
import { lazy, Suspense } from "react";
import type { Observation } from "../types/api";
import { Loading } from "./ui";

const ChannelChartImpl = lazy(() => import("./ChannelChart"));
const RssiChartImpl = lazy(() => import("./RssiChart"));

export function ChannelChart(props: { byChannel: Record<string, number> }) {
  return <Suspense fallback={<Loading what="chart" />}><ChannelChartImpl {...props} /></Suspense>;
}

export function RssiChart(props: { data: Observation[] }) {
  return <Suspense fallback={<Loading what="chart" />}><RssiChartImpl {...props} /></Suspense>;
}
