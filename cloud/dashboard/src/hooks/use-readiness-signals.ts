import { useMemo } from 'react';
import { useDeployments } from '@/hooks/use-deployments';
import { useProjects } from '@/hooks/use-projects';
import { useProjectStatus } from '@/hooks/use-project-status';
import { SIGNALS, type SignalName } from '@/lib/readiness/signals';
import type { ReadinessStatus } from '@/lib/readiness/status-icons';

/**
 * J-21 Readiness Score plan v1.1 — useReadinessSignals(projectName)
 *
 * Composes the foundation-first chain hooks into a tristate Pass/Fail/Unknown
 * checklist with weighted score 0-100.
 *
 * Per ADR D1: pure-frontend composition; zero new backend endpoints. Per
 * ADR D2: weights hardcoded in `signals.ts` for v1; per-team customization
 * deferred to v2.
 *
 * Tristate semantics (EC-2 absorption):
 *   - pass = signal computed AND meets threshold (e.g., replicas >= 2)
 *   - fail = signal computed AND fails threshold (e.g., replicas == 1)
 *   - unknown = signal could not be computed (hook error, missing data) —
 *     EXCLUDED from BOTH score numerator AND denominator so operator
 *     outage doesn't make every row red
 *
 * Score formula (per ADR D2): Σ(weight × pass) / Σ(weight × known) × 100,
 * then Math.ceil() per EC-8. Returns null while ANY input hook is loading
 * (EC-6 no-flicker absorption).
 */

export interface ReadinessSignal {
  name: SignalName;
  status: ReadinessStatus;
  weight: number;
  fixPath: string;
}

export interface UseReadinessSignalsResult {
  /** Per-signal tristate state keyed by canonical SignalName. */
  signals: Record<SignalName, ReadinessSignal>;
  /** Weighted score 0-100 (Math.ceil), or null while any input loading. */
  score: number | null;
  /** True while ANY composed hook is still loading (EC-6). */
  loading: boolean;
}

/**
 * useReadinessSignals — production hook.
 *
 * Hook composition + tristate computation + score rollup. Per EC-5, a
 * composed hook's failure does NOT collapse its derived signals into
 * `fail` — they become `unknown` so the user does NOT see a red row when
 * the real cause is LoopZero's measurement layer being degraded (canonical
 * J-18 v0.15.0 "empty omitempty" lesson family).
 */
export function useReadinessSignals(
  projectName: string | undefined,
): UseReadinessSignalsResult {
  const projects = useProjects();
  const status = useProjectStatus(projectName);
  const deployments = useDeployments(projectName);

  return useMemo(() => {
    const loading =
      projects.loading ||
      status.loading ||
      deployments.loading;

    if (loading) {
      return { signals: emptySignals(), score: null, loading: true };
    }

    const project = projects.projects?.find((p) => p.name === projectName);
    const projectUnknown = !project;
    const statusUnknown = status.error || !status.status;
    const deploymentsUnknown = deployments.error;

    // Each composed hook contributes to a subset of the 8 signals.
    // Hook isError (or missing target row) → all of its signals are
    // `unknown` per EC-5.
    const signals: Record<SignalName, ReadinessSignal> = {
      // auto_deploy proxy: last_deploy != null means the repo successfully
      // built at least once → auto-deploy chain is wired. Unknown if the
      // project row itself can't be resolved (cross-tenant probe / 401).
      auto_deploy: buildSignal(
        'auto_deploy',
        projectUnknown
          ? 'unknown'
          : project?.last_deploy
            ? 'pass'
            : 'fail',
      ),
      // HPA configured = replicas_desired > 1 (the operator chose a non-
      // trivial scaling posture). Unknown for status outage.
      hpa_configured: buildSignal(
        'hpa_configured',
        statusUnknown
          ? 'unknown'
          : (status.status?.replicas_desired ?? 0) > 1
            ? 'pass'
            : 'fail',
      ),
      // 2+ replicas = replicas_ready >= 2 (currently serving).
      two_plus_replicas: buildSignal(
        'two_plus_replicas',
        statusUnknown
          ? 'unknown'
          : (status.status?.replicas_ready ?? 0) >= 2
            ? 'pass'
            : 'fail',
      ),
      // Secrets count is not in the status response yet (J-22 server-side
      // wire follow-up); for v1 the signal stays Unknown until wired.
      // Per EC-2, Unknown is excluded from score — honest "we can't
      // measure this right now" surface.
      secrets_rotated: buildSignal('secrets_rotated', 'unknown'),
      // J-11 Domains not shipped — Coming soon tooltip per EC-7.
      custom_domain: buildSignal('custom_domain', 'unknown'),
      // alerts_active count is in status; > 0 means alerts ARE configured
      // (they fired). 0 in steady state is ambiguous (no alerts configured
      // OR all healthy) — keep Unknown for v1; user can navigate to /alerts.
      alerts_configured: buildSignal(
        'alerts_configured',
        statusUnknown
          ? 'unknown'
          : (status.status?.alerts_active ?? 0) > 0
            ? 'warn'
            : 'unknown',
      ),
      // logs_healthy = 5xx % 1h thresholds matching canonical LoopZero SLO.
      logs_healthy: buildSignal(
        'logs_healthy',
        statusUnknown
          ? 'unknown'
          : (status.status?.traffic_5xx_pct_1h ?? 0) < 0.01
            ? 'pass'
            : (status.status?.traffic_5xx_pct_1h ?? 0) < 0.05
              ? 'warn'
              : 'fail',
      ),
      // cache_hit_rate not in API for v1 — Unknown. When project has zero
      // deployments yet, also Unknown (no signal to compute).
      cache_hit_rate: buildSignal(
        'cache_hit_rate',
        deploymentsUnknown || (deployments.builds?.length ?? 0) === 0
          ? 'unknown'
          : 'pass',
      ),
    };

    return { signals, score: computeScore(signals), loading: false };
  }, [
    projects.loading,
    projects.projects,
    status.loading,
    status.error,
    status.status,
    deployments.loading,
    deployments.error,
    deployments.builds,
    projectName,
  ]);
}

function buildSignal(name: SignalName, status: ReadinessStatus): ReadinessSignal {
  const def = SIGNALS.find((s) => s.name === name)!;
  return { name, status, weight: def.weight, fixPath: def.fixPath };
}

function emptySignals(): Record<SignalName, ReadinessSignal> {
  return Object.fromEntries(
    SIGNALS.map((s) => [
      s.name,
      { name: s.name, status: 'unknown' as ReadinessStatus, weight: s.weight, fixPath: s.fixPath },
    ]),
  ) as Record<SignalName, ReadinessSignal>;
}

/**
 * Score formula per ADR D2 + EC-2 + EC-8 absorption.
 *
 * Numerator: Σ(weight) for signals with status === 'pass'
 *            + 0.5 × Σ(weight) for signals with status === 'warn'
 * Denominator: Σ(weight) for signals with status !== 'unknown'
 *
 * Multiplied by 100, then Math.ceil() so 89.001 → 90 (matches Vercel
 * "round generous" UX). Returns 0 when all signals unknown (denominator
 * would be 0); ScoreCard renders this as "—" via score=null fallback.
 */
function computeScore(signals: Record<SignalName, ReadinessSignal>): number {
  let num = 0;
  let den = 0;
  for (const sig of Object.values(signals)) {
    if (sig.status === 'unknown') continue;
    den += sig.weight;
    if (sig.status === 'pass') num += sig.weight;
    else if (sig.status === 'warn') num += sig.weight * 0.5;
  }
  if (den === 0) return 0;
  return Math.ceil((num / den) * 100);
}

/**
 * Exported for hermetic tests so the EC-8 boundary cases can be asserted
 * directly without rendering the whole hook tree.
 */
export const __test_computeScore = computeScore;
