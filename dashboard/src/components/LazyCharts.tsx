// Recharts is large; charts load on demand so the first page paints fast.
import { lazy, Suspense } from "react";
import type { Observation } from "../types/api";
import type { Bucket } from "../utils/bluetooth";
import { Loading } from "./ui";

const ChannelChartImpl = lazy(() => import("./ChannelChart"));
const RssiChartImpl = lazy(() => import("./RssiChart"));
const RssiHistogramImpl = lazy(() => import("./bluetooth/RssiHistogram"));

export function ChannelChart(props: { byChannel: Record<string, number> }) {
  return <Suspense fallback={<Loading what="chart" />}><ChannelChartImpl {...props} /></Suspense>;
}

export function RssiChart(props: { data: Observation[] }) {
  return <Suspense fallback={<Loading what="chart" />}><RssiChartImpl {...props} /></Suspense>;
}

export function RssiHistogram(props: { buckets: Bucket[]; unknown: number }) {
  return <Suspense fallback={<Loading what="chart" />}><RssiHistogramImpl {...props} /></Suspense>;
}
