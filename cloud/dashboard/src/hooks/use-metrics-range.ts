/**
 * use-metrics-range — TanStack Query hook for J-18 Prometheus histogram fetch.
 *
 * T2.2 of plan 2026-06-06-dashboard-journey-j18-status-runtime-health v1.1.
 *
 * Consumes backend T1.2 endpoint:
 *   GET /api/v1/projects/{name}/metrics?range=7d&signal=uptime_24h
 *
 * Returns:
 *   - { status: 'ok', data: [...] } when histogram populated
 *   - { status: 'collecting', collectingHint: 'Coletando dados...', data: [] }
 *     when project < 24h old (EC-8 absorption — UI renders honest collecting
 *     state, NOT empty chart)
 *   - { status: 'prometheus_unreachable' } when backend returns 504 (EC-5
 *     absorption — UI renders "—" for KPI / disabled drill-down)
 *
 * Used by T3.3 SignalDetailSheet to render the 7d histogram chart.
 */

import { useQuery } from '@tanstack/react-query';
import { engineFetch } from '@/lib/api';

export interface MetricsRangeDataPoint {
  /** Unix timestamp (seconds). */
  t: number;
  /** Metric scalar value. */
  v: number;
}

export type MetricsRangeStatus =
  | 'ok'
  | 'collecting'
  | 'prometheus_unreachable'
  | 'unknown';

export interface MetricsRangeResponse {
  signal: string;
  range: string;
  status: MetricsRangeStatus;
  data: MetricsRangeDataPoint[];
  collecting_hint?: string;
}

export interface UseMetricsRangeOptions {
  signal: string;
  range?: string;
}

export interface UseMetricsRangeResult {
  data: MetricsRangeDataPoint[];
  status: MetricsRangeStatus;
  collectingHint?: string;
  loading: boolean;
  error: boolean;
}

/**
 * useMetricsRange — fetches Prometheus query_range data for one signal.
 *
 * Returns honest status field reflecting backend state — consumer (T3.3
 * SignalDetailSheet) renders different UI for each: histogram for `ok`,
 * "Coletando dados..." for `collecting`, "—" for `prometheus_unreachable`.
 */
export function useMetricsRange(
  projectName: string | undefined,
  env: string | undefined,
  options: UseMetricsRangeOptions,
): UseMetricsRangeResult {
  const { signal, range = '7d' } = options;
  const resolvedEnv = env ?? 'production';

  const path = `/api/v1/projects/${encodeURIComponent(
    projectName ?? '',
  )}/metrics?range=${encodeURIComponent(range)}&signal=${encodeURIComponent(signal)}`;

  const query = useQuery({
    queryKey: ['metricsRange', projectName, resolvedEnv, signal, range],
    queryFn: () => engineFetch<MetricsRangeResponse>(path),
    enabled: !!projectName && !!signal,
    // Histogram is mostly static once Prometheus warms up — refetch every 30s
    // is plenty (vs 5s for status snapshot). Pause when tab hidden.
    refetchInterval: 30_000,
    refetchIntervalInBackground: false,
  });

  const resp = query.data ?? null;

  // EC-5 absorption: backend 504 returns null from engineFetch — surface as
  // prometheus_unreachable status so UI renders "—" not 0%.
  if (query.isError || resp === null) {
    if (query.isLoading) {
      return {
        data: [],
        status: 'unknown',
        loading: true,
        error: false,
      };
    }
    return {
      data: [],
      status: query.isError ? 'prometheus_unreachable' : 'unknown',
      loading: false,
      error: query.isError,
    };
  }

  return {
    data: resp.data ?? [],
    status: resp.status,
    collectingHint: resp.collecting_hint,
    loading: query.isLoading,
    error: false,
  };
}
