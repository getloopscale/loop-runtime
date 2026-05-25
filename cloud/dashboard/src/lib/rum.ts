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
