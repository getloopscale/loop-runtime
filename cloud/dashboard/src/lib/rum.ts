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
 *
 * Plan 2026-05-29-login-page-pragmatic-uplift (T2.2 D5): added 4 auth.method.*
 * events. These follow the AuthTelemetryPayload schema with v:1 versioning.
 * If backend /v1/telemetry/events ever adds schema validation, version bump
 * here is the contract evolution point.
 */
export type ActivationEvent =
  | 'onboarding_step_completed'
  | 'onboarding_dismissed'
  | 'onboarding_replayed'
  | 'first_deploy_celebrated'
  | 'cli_command_copied'
  | 'auth.method.attempt'
  | 'auth.method.success'
  | 'auth.method.failure'
  | 'auth.method.cancelled';

/**
 * Auth telemetry types (plan 2026-05-29 T2.2 / D5 — FAANG-grade).
 *
 * PII-FREE INVARIANT for auth.method.* events:
 * - NO email (user-supplied identifier)
 * - NO IP (sendBeacon adds it at HTTP-layer; NEVER in payload)
 * - NO Kratos session ID (server-side ephemeral)
 * - NO user ID / tenant ID (post-auth concerns)
 * - NO password (obviously)
 * - NO referrer (could leak email via URL params)
 *
 * Allowed:
 * - method (functional)
 * - correlation_id (random UUID, no identifying info)
 * - latency_ms (numeric)
 * - reason (categorical enum)
 * - viewport_bucket (categorical)
 * - timestamp (epoch ms)
 *
 * Schema is versioned (v: 1) for future migration without ambiguity.
 */
export type AuthMethod = 'github' | 'google' | 'password' | 'passkey';

export type AuthFailureReason =
  | 'kratos_5xx'          // HTTP 500+ from Kratos (EC-1)
  | 'kratos_validation'   // 400-class, Kratos ui error nodes
  | 'csrf_expired'        // csrf_token invalid
  | 'network'             // fetch threw
  | 'aborted_by_user';    // user navigated away mid-flow

export type ViewportBucket = 'mobile' | 'tablet' | 'desktop';

export interface AuthTelemetryPayload {
  v: 1;
  method: AuthMethod;
  correlation_id: string;
  latency_ms?: number;
  reason?: AuthFailureReason;
  viewport_bucket: ViewportBucket;
  timestamp: number;
}

/**
 * viewportBucket — categorical viewport size (Tailwind sm/md/lg breakpoints).
 * SSR-safe: returns 'desktop' when window is undefined.
 */
export function viewportBucket(): ViewportBucket {
  if (typeof window === 'undefined') return 'desktop';
  const w = window.innerWidth;
  if (w < 640) return 'mobile';
  if (w < 1024) return 'tablet';
  return 'desktop';
}

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

/**
 * T1.2 — UX interaction metrics taxonomy per sponsor critério #17.
 *
 * EC-03 note: /v1/telemetry/interactions endpoint NÃO existe em backend
 * (grep confirmed em api/internal/routes/). Helper exported para use futuro
 * mas wire-sites SKIPPED até backend route ship. Dev mode logs to console;
 * prod sendBeacon falha silenciosamente até endpoint exist.
 *
 * Quando backend route shipar, wire em 5 paths críticos: rollback button,
 * secrets add/delete, log viewer mount em error context, scale submit,
 * team invite submit.
 */
export type InteractionCategory =
  | 'rollback' | 'secrets' | 'logs' | 'scale'
  | 'team' | 'billing' | 'domains' | 'deploys';

export function trackInteraction(
  category: InteractionCategory,
  action: string,
  payload: Record<string, unknown> = {},
): void {
  try {
    const body = JSON.stringify({
      type: 'interaction',
      category,
      action,
      payload,
      timestamp: Date.now(),
    });
    if (import.meta.env.DEV) {
      // eslint-disable-next-line no-console
      console.log(`[interaction] ${category}.${action}`, payload);
      return;
    }
    try {
      if (typeof navigator !== 'undefined' && navigator.sendBeacon) {
        navigator.sendBeacon('/v1/telemetry/interactions', body);
      }
    } catch { /* never throw */ }
  } catch { /* defensive */ }
}
