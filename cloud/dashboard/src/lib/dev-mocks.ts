// Visual-validation mock layer for the LoopZero dashboard.
//
// Activated only when `import.meta.env.VITE_USE_MOCKS === 'true'`. Returns
// seeded fixtures for session + every cloudFetch/engineFetch path so all 13
// dashboard routes render with realistic data, WITHOUT a running Kratos +
// engine API + cloud backend.
//
// Purpose: closes the T-H.1 visual smoke gap from MEET-ASYNC-001 without
// requiring `task setup` + `tilt up` (which is 15-25min cold start).
//
// Each fixture comments the TS source-of-truth shape it satisfies so future
// drift between mock and real backend stays visible in code review.
//
// **NOT** for tests (those use vi.mock at the hook layer). **NOT** for prod
// (gated by env var at boot — `useMocks()` returns false unless VITE_USE_MOCKS
// is set, and Vite's dead-code-elimination drops the import in prod builds
// because the env var is statically resolvable at build time).

import type { Session } from './auth';

/** Single source of truth for "is the dev mock layer active?". */
export function useMocks(): boolean {
  return import.meta.env.VITE_USE_MOCKS === 'true';
}

/** Fixture: a fully-authenticated session with a verified email. */
export function getMockSession(): Session {
  return {
    active: true,
    identity: {
      id: 'mock-user-uuid-0000-0000',
      traits: {
        email: 'demo@loopzero.example',
        name: 'Demo Operator',
      },
      verifiable_addresses: [
        { value: 'demo@loopzero.example', verified: true, via: 'email' },
      ],
    },
    authentication_methods: [
      { method: 'password', completed_at: '2026-05-25T08:00:00Z' },
    ],
  };
}

// Path → mock response map. Keys match what cloudFetch/engineFetch receive
// AFTER the `/engine` prefix is added by engineFetch (so engineFetch('/api/v1/x')
// looks up '/engine/api/v1/x' here).
const FIXTURES: Record<string, unknown> = {
  // -- /v1/billing (BillingResponse from pages/billing.tsx) ----------------
  '/v1/billing': {
    plan: 'pro',
    team: 'acme',
    period_start: '2026-05-01T00:00:00Z',
    period_end: '2026-05-31T23:59:59Z',
    usage: {
      'Fast Data Transfer': { used: 412, limit: 1000 },
      'Fast Origin Transfer': { used: 87, limit: 250 },
      'Edge Requests': { used: 1_240_000, limit: 10_000_000 },
      'Build Minutes': { used: 145, limit: 400 },
      'Concurrent Builds': { used: 2, limit: 4 },
    },
  },

  // -- /engine/api/v1/projects (ProjectsResponse from hooks/use-projects.ts)
  '/engine/api/v1/projects': {
    projects: [
      {
        name: 'acme-web',
        environment: 'production',
        last_deploy: '2026-05-25T06:42:00Z',
        apps: [
          { name: 'frontend', type: 'web', framework: 'next', status: 'live' },
          { name: 'api', type: 'service', framework: 'go', status: 'live' },
        ],
      },
      {
        name: 'acme-payments',
        environment: 'production',
        last_deploy: '2026-05-24T19:11:00Z',
        apps: [
          { name: 'stripe-webhook', type: 'service', framework: 'go', status: 'live' },
        ],
      },
      {
        name: 'acme-marketing',
        environment: 'staging',
        last_deploy: '2026-05-23T14:30:00Z',
        apps: [
          { name: 'landing', type: 'web', framework: 'astro', status: 'building' },
        ],
      },
      {
        name: 'acme-internal-tools',
        environment: 'production',
        last_deploy: '2026-05-20T09:15:00Z',
        apps: [
          { name: 'dashboard', type: 'web', framework: 'vite', status: 'live' },
          { name: 'cron-worker', type: 'job', framework: 'node', status: 'live' },
        ],
      },
      {
        name: 'acme-experiments',
        environment: 'preview',
        last_deploy: '2026-05-19T22:08:00Z',
        apps: [
          { name: 'a-b-test', type: 'web', framework: 'remix', status: 'idle' },
        ],
      },
    ],
  },

  // -- /engine/api/v1/domains (DomainsResponse from pages/domains.tsx) -----
  '/engine/api/v1/domains': {
    total: 4,
    domains: [
      {
        id: 'dom-001',
        domain: 'app.acme.com',
        project: 'acme-web',
        app: 'frontend',
        environment: 'production',
        status: 'active',
        cname_target: 'acme-web.loopzero.app',
        created_at: '2026-04-10T12:00:00Z',
        verified_at: '2026-04-10T12:08:00Z',
        tls: { status: 'issued', expires_at: '2026-07-10T12:08:00Z' },
      },
      {
        id: 'dom-002',
        domain: 'api.acme.com',
        project: 'acme-payments',
        app: 'stripe-webhook',
        environment: 'production',
        status: 'active',
        cname_target: 'acme-payments.loopzero.app',
        created_at: '2026-04-15T09:30:00Z',
        verified_at: '2026-04-15T09:42:00Z',
        tls: { status: 'issued', expires_at: '2026-07-15T09:42:00Z' },
      },
      {
        id: 'dom-003',
        domain: 'staging.acme.com',
        project: 'acme-marketing',
        app: 'landing',
        environment: 'staging',
        status: 'pending_verification',
        cname_target: 'acme-marketing.loopzero.app',
        created_at: '2026-05-23T14:00:00Z',
        verification: {
          txt_name: '_loopzero.staging.acme.com',
          txt_value: 'verify=a1b2c3d4e5f6g7h8',
        },
      },
      {
        id: 'dom-004',
        domain: 'preview.acme.com',
        project: 'acme-experiments',
        app: 'a-b-test',
        environment: 'preview',
        status: 'provisioning',
        cname_target: 'acme-experiments.loopzero.app',
        created_at: '2026-05-25T07:30:00Z',
      },
    ],
  },

  // -- /engine/api/v1/environments (EnvsResponse from pages/environments.tsx)
  '/engine/api/v1/environments': {
    environments: [
      { name: 'production', status: 'live', projects: 4, protected: true, domain: 'app.acme.com' },
      { name: 'staging', status: 'building', projects: 3, protected: false, domain: 'staging.acme.com' },
      { name: 'preview', status: 'live', projects: 5, protected: false, domain: '*.preview.acme.com' },
      { name: 'pr-1247', status: 'live', projects: 1, protected: false, domain: 'pr-1247.preview.acme.com' },
      { name: 'pr-1251', status: 'idle', projects: 1, protected: false, domain: 'pr-1251.preview.acme.com' },
      { name: 'load-test', status: 'failed', projects: 1, protected: false, domain: 'lt.acme.com' },
    ],
  },

  // -- /engine/api/v1/teams/members (TeamResponse from pages/team.tsx) -----
  '/engine/api/v1/teams/members': {
    members: [
      { username: 'paulo', name: 'Paulo Henrique', email: 'paulo@loopzero.example', role: 'owner', joined: '2026-01-15' },
      { username: 'alice', name: 'Alice Costa', email: 'alice@acme.com', role: 'admin', joined: '2026-02-03' },
      { username: 'bob', name: 'Bob Tanaka', email: 'bob@acme.com', role: 'admin', joined: '2026-02-18' },
      { username: 'carlos', name: 'Carlos Mendes', email: 'carlos@acme.com', role: 'member', joined: '2026-03-12' },
      { username: 'dana', name: 'Dana Singh', email: 'dana@acme.com', role: 'member', joined: '2026-04-01' },
      { username: 'eve', name: 'Eve Johansson', email: 'eve@acme.com', role: 'member', joined: '2026-04-22' },
    ],
  },
};

/**
 * Lookup a mock response for a given path. Returns `undefined` for unknown
 * paths so the caller can fall through to the real fetch (or null).
 *
 * Wrapped as Promise<T | null> in cloudFetch's guard so the call signature
 * matches production behavior.
 */
export function getMockResponse<T>(path: string): T | null {
  const fixture = FIXTURES[path];
  if (fixture === undefined) {
    // Path not seeded — log once for diagnostic, return null (production behavior)
    if (typeof console !== 'undefined') {
      console.warn(`[dev-mocks] No fixture for path: ${path}`);
    }
    return null;
  }
  // Structured clone so mutations in components don't poison the fixture
  return JSON.parse(JSON.stringify(fixture)) as T;
}
