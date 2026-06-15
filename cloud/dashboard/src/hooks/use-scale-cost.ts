import { useMemo } from 'react';

/**
 * Default per-pod hourly cost (USD). Mirror of the backend
 * pkg/enforcement.DefaultPodCostPerHour constant; the client preview
 * uses the SAME rate so the displayed estimate matches the server's
 * authoritative computation within ±0.001 (rounding floor only).
 *
 * Per J-20 plan v1.1 ADR D1 EC-2 absorption: the client preview is a
 * HINT — the canonical `monthly_cost_usd` value in the ScaleResponse
 * after save is the source of truth. The hook returns BOTH the
 * preview AND a delta-against-current so the UI can render
 * "+\$54/mo" or "–\$22/mo" diff badges.
 */
export const DEFAULT_POD_COST_PER_HOUR = 0.05;
const HOURS_PER_MONTH = 730;

export interface UseScaleCostInput {
  /** Replicas the user is currently editing (slider value or HPA max). */
  replicas: number;
  /** Replicas currently in effect server-side — basis for delta. */
  currentReplicas: number;
  /** Optional per-cluster override; defaults to DEFAULT_POD_COST_PER_HOUR. */
  podCostPerHour?: number;
}

export interface UseScaleCostResult {
  /** Estimated monthly cost at the proposed replica count. */
  estimatedMonthlyUSD: number;
  /** Estimated monthly cost at the currently-applied replica count. */
  currentMonthlyUSD: number;
  /** Signed delta (estimate - current). Positive = increase. */
  deltaUSD: number;
  /**
   * `up` when delta > 0, `down` when delta < 0, `flat` when 0. The UI
   * uses this to pick the icon + color (red for up, green for down).
   */
  direction: 'up' | 'down' | 'flat';
}

/**
 * useScaleCost — J-20 Scale plan v1.1 T2.2.
 *
 * Pure client-side preview helper. Does NOT call any API — the
 * authoritative cost flows back via `ScaleResponse.monthly_cost_usd`
 * after the save mutation completes. The dashboard MUST reconcile its
 * shown value to that response (ADR D1).
 *
 * Why pure-client when the server is authoritative?
 *
 * Stripe metered billing reconciles at end-of-cycle; the preview is
 * UX-only ("up to ~\$X/mo if scaled to max"). Pulling a server-side
 * cost on every slider drag would flood the API with GET requests and
 * the value drifts only with the per-cluster rate (which the dashboard
 * caches via env). Net: keep the slider smooth, reconcile on save.
 *
 * Reconciliation pattern (UI consumer of this hook):
 *
 *   1. Slider drag → useScaleCost preview shows "~\$54.75/mo (+\$36.50)"
 *   2. User clicks Save
 *   3. Mutation succeeds → ScaleResponse.monthly_cost_usd = 54.75
 *   4. UI displays "Applied: \$54.75/mo" (no diff vs preview ⇒ silent)
 *   5. If server value differs by > \$0.01, surface a banner so users
 *      see the canonical bill amount BEFORE the next billing cycle
 *      (defensive — preview should rarely lie since both use the same
 *      730 hr * \$0.05 formula).
 */
export function useScaleCost(input: UseScaleCostInput): UseScaleCostResult {
  const rate = input.podCostPerHour ?? DEFAULT_POD_COST_PER_HOUR;
  return useMemo(() => {
    const safeReplicas = Math.max(0, input.replicas);
    const safeCurrent = Math.max(0, input.currentReplicas);
    const estimatedMonthlyUSD = safeReplicas * rate * HOURS_PER_MONTH;
    const currentMonthlyUSD = safeCurrent * rate * HOURS_PER_MONTH;
    const deltaUSD = estimatedMonthlyUSD - currentMonthlyUSD;
    const direction: 'up' | 'down' | 'flat' =
      deltaUSD > 0.001 ? 'up' : deltaUSD < -0.001 ? 'down' : 'flat';
    return { estimatedMonthlyUSD, currentMonthlyUSD, deltaUSD, direction };
  }, [input.replicas, input.currentReplicas, rate]);
}
