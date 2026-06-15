/**
 * J-21 Readiness — single source of truth for the 8 signals + weights +
 * fix-it routes. Plan v1.1 T0.1 + T1.2.
 *
 * Canonical name (per plan ADR D2): score = Σ(weight × pass) / Σ(weight × known)
 * with Unknown excluded from BOTH numerator AND denominator (EC-2 absorption).
 *
 * Weight scale 0-25 — chosen so the 8 signals can sum to ≤ 100 and the
 * "important" ones (HPA, secrets) outweigh the "optional" ones (cache hit).
 */

export type SignalName =
  | 'auto_deploy'
  | 'hpa_configured'
  | 'custom_domain'
  | 'secrets_rotated'
  | 'two_plus_replicas'
  | 'alerts_configured'
  | 'logs_healthy'
  | 'cache_hit_rate';

export interface SignalDefinition {
  /** Stable name used as React key + URL param + test fixtures. */
  name: SignalName;
  /** User-facing label rendered in the checklist row. */
  label: string;
  /** One-line FAQ description for the collapse panel. */
  description: string;
  /** Score weight 0-25; higher = more important to project readiness. */
  weight: number;
  /**
   * Route the "Fix it" link navigates to when the signal status is
   * Fail or Warn. Use `:name` placeholder for the project name; consumers
   * replace at render. Routes outside KNOWN_ROUTES render "Coming soon"
   * tooltip per ADR D4 + EC-7.
   */
  fixPath: string;
}

/**
 * Allowlist of routes already shipped by foundation-first chain. A signal's
 * fixPath that is NOT in this set renders "Coming soon" tooltip instead of
 * a broken `<Link>` (EC-7 absorption).
 *
 * Add a route to this allowlist when the corresponding J-XX feature ships.
 */
export const KNOWN_ROUTES: ReadonlySet<string> = new Set([
  '/projects/:name', // J-08 detail page
  '/projects/:name/scale', // J-20 v0.16.0 ✅
  '/projects/:name/secrets', // J-22 ✅
  '/projects/:name/deployments', // J-17 ✅
  '/projects/:name/logs', // J-19 v0.14.0 ✅
  '/projects/:name/status', // J-18 v0.15.0 ✅
  '/projects/:name/connect', // J-09 ✅
]);

/**
 * Canonical 8 signal definitions. Order is preserved in the rendered
 * checklist top-to-bottom.
 */
export const SIGNALS: ReadonlyArray<SignalDefinition> = [
  {
    name: 'auto_deploy',
    label: 'Auto-deploy enabled',
    description:
      'Pushes to your default branch trigger an automatic deploy. Disable only if you prefer manual deploys via the CLI.',
    weight: 20,
    fixPath: '/projects/:name/connect',
  },
  {
    name: 'hpa_configured',
    label: 'Autoscaling configured',
    description:
      'Horizontal Pod Autoscaler keeps your service responsive under load. Without it, every replica handles every request.',
    weight: 20,
    fixPath: '/projects/:name/scale',
  },
  {
    name: 'two_plus_replicas',
    label: 'At least 2 replicas',
    description:
      'A single replica means any pod restart is a brief outage. Scale to 2+ for zero-downtime deploys.',
    weight: 15,
    fixPath: '/projects/:name/scale',
  },
  {
    name: 'secrets_rotated',
    label: 'Secrets defined',
    description:
      'Your service has the secrets it needs to start. Apps without their required env vars will crashloop on first deploy.',
    weight: 15,
    fixPath: '/projects/:name/secrets',
  },
  {
    name: 'custom_domain',
    label: 'Custom domain',
    description:
      'A custom domain makes your service production-ready. The auto-generated subdomain works for previews but not for customers.',
    weight: 10,
    // J-11 not yet shipped — renders "Coming soon" tooltip per EC-7.
    fixPath: '/projects/:name/domains',
  },
  {
    name: 'alerts_configured',
    label: 'Alerts configured',
    description:
      'Slack or email alerts fire when your service goes Degraded so you find out before customers do.',
    weight: 10,
    // Not yet shipped — Coming soon tooltip per EC-7.
    fixPath: '/projects/:name/alerts',
  },
  {
    name: 'logs_healthy',
    label: 'Runtime logs healthy',
    description:
      'No error-rate spike in the last hour. Refer to the Logs page for details when this signal turns Warn or Fail.',
    weight: 5,
    fixPath: '/projects/:name/logs',
  },
  {
    name: 'cache_hit_rate',
    label: 'Build cache hit rate',
    description:
      'A high build cache hit rate keeps deploys fast. A low rate is informational, not blocking.',
    weight: 5,
    fixPath: '/projects/:name/deployments',
  },
];

/**
 * Sum of weights for a known signal set. Score denominator at full coverage.
 * Computed at module load (constants only) — no recomputation per render.
 */
export const TOTAL_WEIGHT: number = SIGNALS.reduce((sum, s) => sum + s.weight, 0);

/**
 * Returns whether `path` (with `:name` placeholder) is in the KNOWN_ROUTES
 * allowlist. Used by the Checklist row renderer per EC-7.
 */
export function isKnownRoute(path: string): boolean {
  return KNOWN_ROUTES.has(path);
}
