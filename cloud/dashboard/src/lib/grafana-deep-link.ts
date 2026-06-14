/**
 * grafana-deep-link — Grafana templated URL builder for J-18 Status page.
 *
 * T2.3 of plan 2026-06-06-dashboard-journey-j18-status-runtime-health v1.1.
 *
 * Per ADR D2: open Grafana dashboard in new tab (no iframe — CSP block).
 * URL template: `<grafanaBaseURL>/d/loopzero-project?var-project=<X>&var-env=<Y>&var-signal=<Z>`.
 *
 * EC-6 absorption (per edge-case-plan review): URL-encode project/env/signal
 * names so values containing `&`, `?`, `#` produce a valid Grafana URL.
 *
 * EC-13 absorption (DOCUMENT): when grafanaBaseURL is empty/null, returns null
 * so consumers (T3.3 SignalDetailSheet) render the "Open in Grafana" link as
 * disabled (honest per ADR D2 — no fake URL pretending things work).
 */

export interface GrafanaDeepLinkInput {
  grafanaBaseURL: string | null | undefined;
  projectName: string;
  env: string;
  signal?: string;
}

/**
 * buildGrafanaDeepLink — pure URL templating helper.
 *
 * Returns null when grafanaBaseURL is empty/null (honest disabled state).
 * Returns a valid Grafana dashboard URL with var-project + var-env + optional
 * var-signal query parameters URL-encoded.
 */
export function buildGrafanaDeepLink(input: GrafanaDeepLinkInput): string | null {
  const { grafanaBaseURL, projectName, env, signal } = input;

  if (!grafanaBaseURL || grafanaBaseURL.trim() === '') {
    return null;
  }

  // Strip trailing slash from base URL to avoid double-slash in template.
  const base = grafanaBaseURL.replace(/\/+$/, '');

  // EC-6 absorption: URL-encode the project/env/signal segments. encodeURIComponent
  // handles `&`, `?`, `#`, space, accented chars correctly.
  const params = new URLSearchParams();
  params.set('var-project', projectName);
  params.set('var-env', env);
  if (signal && signal.trim() !== '') {
    params.set('var-signal', signal);
  }

  return `${base}/d/loopzero-project?${params.toString()}`;
}
