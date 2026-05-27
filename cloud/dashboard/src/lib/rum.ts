// T-DR-5.2 v1.2 (Phase 5) — Real User Monitoring (RUM)
// Per ADR D8: ship web-vitals reporter day-1 so prod customers signal flow
// immediately when they arrive. Dev mode = console.log; prod mode = future
// POST to /v1/telemetry/web-vitals (backend endpoint = separate scope).
//
// Resolves G2 FAANG gap (RUM day-1).

import { onLCP, onINP, onCLS, type Metric } from 'web-vitals';

function reportInDev(metric: Metric): void {
  // Dev: log to console with thresholds per ADR D7 budget
  const thresholds: Record<string, [number, number]> = {
    LCP: [2500, 4000],
    INP: [200, 500],
    CLS: [0.1, 0.25],
  };
  const [good, poor] = thresholds[metric.name] ?? [Infinity, Infinity];
  const verdict = metric.value <= good ? 'GOOD' : metric.value <= poor ? 'NEEDS_IMPROVEMENT' : 'POOR';
  // eslint-disable-next-line no-console
  console.log(
    `[Web Vitals] ${metric.name}=${metric.value.toFixed(metric.name === 'CLS' ? 3 : 0)} (${verdict})`,
    { id: metric.id, navigationType: metric.navigationType }
  );
}

function reportInProd(metric: Metric): void {
  // Prod: POST to telemetry endpoint when backend ships /v1/telemetry/web-vitals
  // For now: same as dev (console). Future:
  //   navigator.sendBeacon('/v1/telemetry/web-vitals', JSON.stringify(metric))
  reportInDev(metric);
}

export function initWebVitals(): void {
  const isDev = import.meta.env.DEV;
  const reporter = isDev ? reportInDev : reportInProd;
  onLCP(reporter);
  onINP(reporter);
  onCLS(reporter);
}

/**
 * T3.5 — Activation event instrumentation. Emits onboarding lifecycle events
 * via sendBeacon (prod) or console.log (dev). Same pattern as error-reporter.ts:
 * never throws (telemetry MUST NOT crash app).
 */
export type ActivationEvent =
  | 'onboarding_step_completed'
  | 'onboarding_dismissed'
  | 'onboarding_replayed'
  | 'first_deploy_celebrated'
  | 'cli_command_copied';

export function reportEvent(type: ActivationEvent, payload: Record<string, unknown> = {}): void {
  try {
    const body = JSON.stringify({ type, payload, timestamp: Date.now() });

    if (import.meta.env.DEV) {
      // eslint-disable-next-line no-console
      console.log(`[activation] ${type}`, payload);
      return;
    }

    try {
      if (typeof navigator !== 'undefined' && navigator.sendBeacon) {
        navigator.sendBeacon('/v1/telemetry/events', body);
      } else if (typeof fetch !== 'undefined') {
        fetch('/v1/telemetry/events', {
          method: 'POST',
          body,
          headers: { 'Content-Type': 'application/json' },
          keepalive: true,
        }).catch(() => {});
      }
    } catch {
      // Transport failed — non-fatal observability gap
    }
  } catch {
    // Defensive: any unexpected error swallowed
  }
}
