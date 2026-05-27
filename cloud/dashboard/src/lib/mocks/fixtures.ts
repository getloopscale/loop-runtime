// Realistic fixtures for /api/v1/* mock handlers — Plan v1.0 T0.5.
//
// Shapes mirror lib/api/types.ts (which mirrors Go structs). Data is plausible
// (acme-* projects, real-looking commit SHAs, ISO timestamps) so the dashboard
// renders as if connected to a real customer account — useful for visual
// validation, demos, and offline development.

import type {
  AuditEntry,
  Build,
  Domain,
  Environment,
  Preview,
  Project,
  ProjectConnection,
  ProjectStatusDetail,
  Secret,
  TeamMember,
} from '@/lib/api/types';

// ============================================================================
// Projects
// ============================================================================

export const projectsFixture: Project[] = [
  {
    name: 'acme-web',
    environment: 'production',
    last_deploy: '2026-05-26T06:42:00Z',
    last_deploy_status: 'live',
    apps: [
      { name: 'frontend', type: 'frontend', framework: 'next', port: 3000 },
      { name: 'api', type: 'server', framework: 'go', port: 8080 },
    ],
  },
  {
    name: 'acme-payments',
    environment: 'production',
    last_deploy: '2026-05-25T19:11:00Z',
    last_deploy_status: 'live',
    apps: [{ name: 'stripe-webhook', type: 'server', framework: 'go' }],
  },
  {
    name: 'acme-marketing',
    environment: 'staging',
    last_deploy: '2026-05-24T14:30:00Z',
    last_deploy_status: 'building',
    apps: [{ name: 'landing', type: 'frontend', framework: 'astro' }],
  },
  {
    name: 'acme-ml-pipeline',
    environment: 'production',
    last_deploy: null,
    last_deploy_status: 'idle',
    apps: [{ name: 'inference', type: 'server', framework: 'fastapi', port: 8000 }],
  },
];

export function makeProject(name: string): Project {
  return {
    name,
    environment: 'production',
    last_deploy: null,
    last_deploy_status: 'idle',
    apps: [],
  };
}

// ============================================================================
// Builds (per project)
// ============================================================================

export const buildsFixture: Build[] = [
  {
    build_id: 'bld_01HXYZ_acme_web_001',
    project_id: 'acme-web',
    environment: 'production',
    status: 'succeeded',
    commit_sha: 'a3b2c1d4e5f6g7h8i9j0',
    created_by: 'paulo@loopzero.example',
    created_at: '2026-05-26T06:38:00Z',
    started_at: '2026-05-26T06:38:10Z',
    finished_at: '2026-05-26T06:42:00Z',
    duration_seconds: 230,
    apps: [
      {
        name: 'frontend',
        image_digest: 'sha256:abc123def456',
        url: 'https://acme-web.loopzero.app',
        duration_ms: 145000,
      },
    ],
  },
  {
    build_id: 'bld_01HXYZ_acme_web_002',
    project_id: 'acme-web',
    environment: 'production',
    status: 'failed',
    commit_sha: 'b1c2d3e4f5g6h7i8j9k0',
    created_by: 'alice@acme.com',
    created_at: '2026-05-25T18:11:00Z',
    started_at: '2026-05-25T18:11:05Z',
    finished_at: '2026-05-25T18:13:42Z',
    duration_seconds: 157,
    error: 'Build step "install" exited with code 1: missing dependency @internal/foo',
  },
  {
    build_id: 'bld_01HXYZ_acme_web_003',
    project_id: 'acme-web',
    environment: 'production',
    status: 'running',
    commit_sha: 'c2d3e4f5g6h7i8j9k0l1',
    created_by: 'bob@acme.com',
    created_at: '2026-05-26T07:15:00Z',
    started_at: '2026-05-26T07:15:08Z',
  },
];

// ============================================================================
// Secrets
// ============================================================================

export const secretsFixture: Secret[] = [
  {
    name: 'STRIPE_SECRET_KEY',
    environments: ['production'],
    last_updated_by: 'paulo@loopzero.example',
    updated_at: '2026-05-20T10:00:00Z',
    eso_sync_status: 'synced',
  },
  {
    name: 'DATABASE_URL',
    environments: ['production', 'staging'],
    last_updated_by: 'alice@acme.com',
    updated_at: '2026-05-15T14:30:00Z',
    eso_sync_status: 'synced',
  },
  {
    name: 'OPENAI_API_KEY',
    environments: ['production'],
    last_updated_by: 'bob@acme.com',
    updated_at: '2026-05-22T09:12:00Z',
    eso_sync_status: 'pending',
  },
];

// ============================================================================
// Teams
// ============================================================================

export const teamMembersFixture: TeamMember[] = [
  {
    username: 'paulo',
    name: 'Paulo Henrique',
    email: 'paulo@loopzero.example',
    role: 'owner',
    joined: '2026-01-15',
  },
  {
    username: 'alice',
    name: 'Alice Costa',
    email: 'alice@acme.com',
    role: 'admin',
    joined: '2026-02-03',
  },
  {
    username: 'bob',
    name: 'Bob Tanaka',
    email: 'bob@acme.com',
    role: 'admin',
    joined: '2026-02-18',
  },
  {
    username: 'carlos',
    name: 'Carlos Mendes',
    email: 'carlos@acme.com',
    role: 'member',
    joined: '2026-03-12',
  },
  {
    username: 'dana',
    name: 'Dana Singh',
    email: 'dana@acme.com',
    role: 'member',
    joined: '2026-04-01',
  },
];

// ============================================================================
// Environments
// ============================================================================

export const environmentsFixture: Environment[] = [
  { name: 'production', status: 'live', projects: 4, protected: true, domain: 'app.acme.com' },
  { name: 'staging', status: 'building', projects: 3, protected: false, domain: 'staging.acme.com' },
  { name: 'preview', status: 'live', projects: 5, protected: false, domain: '*.preview.acme.com' },
];

// ============================================================================
// Domains
// ============================================================================

export const domainsFixture: Domain[] = [
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
    domain: 'staging.acme.com',
    project: 'acme-marketing',
    app: 'landing',
    environment: 'staging',
    status: 'pending_verification',
    cname_target: 'acme-marketing.loopzero.app',
    created_at: '2026-05-24T14:00:00Z',
    verification: {
      txt_name: '_loopzero.staging.acme.com',
      txt_value: 'verify=a1b2c3d4e5f6g7h8',
    },
  },
];

// ============================================================================
// Previews
// ============================================================================

export const previewsFixture: Preview[] = [
  {
    id: 'prv-001',
    project: 'acme-web',
    branch: 'feature/new-checkout',
    pr_title: 'Refactor checkout flow with Stripe Elements',
    pr_number: 1247,
    pr_url: 'https://github.com/acme/web/pull/1247',
    url: 'https://pr-1247.preview.acme.com',
    status: 'active',
    created_by: 'alice@acme.com',
    created_at: '2026-05-25T10:30:00Z',
    expires_at: '2026-05-28T10:30:00Z',
  },
  {
    id: 'prv-002',
    project: 'acme-web',
    branch: 'fix/cart-totals',
    pr_title: 'Fix cart totals when discount applied',
    pr_number: 1251,
    pr_url: 'https://github.com/acme/web/pull/1251',
    url: 'https://pr-1251.preview.acme.com',
    status: 'expiring',
    created_by: 'bob@acme.com',
    created_at: '2026-05-23T08:00:00Z',
    expires_at: '2026-05-26T08:00:00Z',
  },
];

// ============================================================================
// Project status
// ============================================================================

export function makeProjectStatus(project: string): ProjectStatusDetail {
  return {
    project,
    environment: 'production',
    replicas_ready: 2,
    replicas_desired: 2,
    deploy_status: 'succeeded',
    build_status: 'succeeded',
    traffic_5xx_pct_1h: 0.12,
    traffic_5xx_pct_24h: 0.08,
    alerts_active: 0,
    last_deploy_at: '2026-05-26T06:42:00Z',
  };
}

// ============================================================================
// Project connection (GitHub)
// ============================================================================

export function makeProjectConnection(project: string): ProjectConnection {
  if (project === 'acme-ml-pipeline') {
    return { connected: false };
  }
  return {
    connected: true,
    repo_url: `https://github.com/acme/${project.replace('acme-', '')}`,
    branch: 'main',
    auto_deploy: true,
    installation_id: 12345678,
  };
}

// ============================================================================
// Audit log
// ============================================================================

export const auditEntriesFixture: AuditEntry[] = [
  {
    id: 'aud-001',
    ts: '2026-05-26T06:42:00Z',
    actor_email: 'paulo@loopzero.example',
    actor_kind: 'user',
    action: 'deploy.succeeded',
    resource_type: 'project',
    resource_id: 'acme-web',
    severity: 'info',
  },
  {
    id: 'aud-002',
    ts: '2026-05-26T05:30:00Z',
    actor_email: 'alice@acme.com',
    actor_kind: 'user',
    action: 'secret.create',
    resource_type: 'secret',
    resource_id: 'OPENAI_API_KEY',
    severity: 'warning',
  },
  {
    id: 'aud-003',
    ts: '2026-05-25T18:13:42Z',
    actor_email: 'bob@acme.com',
    actor_kind: 'user',
    action: 'deploy.failed',
    resource_type: 'project',
    resource_id: 'acme-web',
    severity: 'critical',
  },
];

// ============================================================================
// Mock log lines for SSE streams (build + runtime)
// ============================================================================

export const buildLogLines = [
  { ts: '2026-05-26T07:15:08Z', level: 'info', msg: 'Cloning github.com/acme/web @ c2d3e4f' },
  { ts: '2026-05-26T07:15:09Z', level: 'info', msg: 'Detected framework: Next.js' },
  { ts: '2026-05-26T07:15:11Z', level: 'info', msg: 'Installing dependencies via pnpm install --frozen-lockfile' },
  { ts: '2026-05-26T07:15:32Z', level: 'info', msg: '1843 packages installed in 21s' },
  { ts: '2026-05-26T07:15:33Z', level: 'info', msg: 'Running build: next build' },
  { ts: '2026-05-26T07:15:48Z', level: 'info', msg: 'Compiled successfully' },
  { ts: '2026-05-26T07:15:52Z', level: 'info', msg: 'Pushing image: registry.loopzero.dev/acme-web/frontend:c2d3e4f' },
  { ts: '2026-05-26T07:16:01Z', level: 'info', msg: 'Image signed with cosign keyless' },
  { ts: '2026-05-26T07:16:03Z', level: 'info', msg: 'SBOM uploaded to attestation store' },
  { ts: '2026-05-26T07:16:08Z', level: 'info', msg: 'Build succeeded in 60.3s' },
] as const;

export const runtimeLogLines = [
  { ts: '2026-05-26T07:20:00Z', level: 'info', msg: 'GET /api/health 200 12ms', source: 'frontend' },
  { ts: '2026-05-26T07:20:01Z', level: 'info', msg: 'GET / 200 45ms', source: 'frontend' },
  { ts: '2026-05-26T07:20:02Z', level: 'info', msg: 'POST /api/checkout 200 234ms', source: 'frontend' },
  { ts: '2026-05-26T07:20:05Z', level: 'warn', msg: 'Slow database query: select * from orders (820ms)', source: 'api' },
  { ts: '2026-05-26T07:20:08Z', level: 'info', msg: 'GET /api/products 200 28ms', source: 'frontend' },
] as const;
