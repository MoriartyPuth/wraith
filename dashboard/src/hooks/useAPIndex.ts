import { useMemo } from "react";
import { api } from "../services/api";
import type { AccessPoint } from "../types/api";
import { useApi } from "./useApi";

/** BSSID → AP lookup for labelling events and findings. */
export function useAPIndex(): Map<string, AccessPoint> {
  const aps = useApi((s) => api.accessPoints(500, 0, s), 30000);
  return useMemo(() => new Map((aps.data?.data ?? []).map((a) => [a.bssid, a])), [aps.data]);
}
