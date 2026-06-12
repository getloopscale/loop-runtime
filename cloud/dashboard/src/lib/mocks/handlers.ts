// Mock handlers for /api/v1/* endpoints — Plan v1.0 T0.4.
//
// Registers a handler for every endpoint listed in api/internal/server/router.go
// that has an end-user UI consumer. Admin/internal endpoints (admin/tenants/pin,
// internal/bootstrap, webhooks/github) intentionally omitted — no UI need.
//
// Pattern per handler:
//   1. Receives Request + extracted params.
//   2. Returns Response with status code + JSON body matching the Go API shape.
//   3. Errors use the canonical LoopZeroError envelope `{ code, message }`.
//   4. In-memory state mutates per-session (delete reflected in subsequent GET).
//
// Call `registerAllMockHandlers()` once at boot from main.tsx via installMocks.

import type {
  AuditListResponse,
  BillingResponse,
  Build,
  BuildAttestation,
  BuildListResponse,
  BuildSBOM,
  Domain,
  DomainListResponse,
  Environment,
  EnvironmentListResponse,
  Preview,
  PreviewListResponse,
  Project,
  ProjectConnection,
  ProjectListResponse,
  ProjectStatusDetail,
  Secret,
  SecretListResponse,
  TeamListResponse,
  TeamMember,
  TenantDeleteResponse,
  Whoami,
} from '@/lib/api/types';
import {
  auditEntriesFixture,
  buildsFixture,
  domainsFixture,
  environmentsFixture,
  makeProject,
  makeProjectConnection,
  makeProjectStatus,
  previewsFixture,
  projectsFixture,
  secretsFixture,
  teamMembersFixture,
  buildLogLines,
  runtimeLogLines,
} from './fixtures';
import { registerRoute } from './server';
import { registerSSE } from './sse';

// ============================================================================
// In-memory "DB" — mutates per session to simulate CRUD persistence
// ============================================================================

const db = {
  projects: [...projectsFixture] as Project[],
  builds: [...buildsFixture] as Build[],
  secrets: [...secretsFixture] as Secret[],
  members: [...teamMembersFixture] as TeamMember[],
  environments: [...environmentsFixture] as Environment[],
  domains: [...domainsFixture] as Domain[],
  previews: [...previewsFixture] as Preview[],
};

function err(code: string, message: string, status: number): Response {
  return Response.json({ code, message }, { status });
}

// ============================================================================
// Auth (api/internal/routes/auth.go)
// ============================================================================

function registerAuthHandlers() {
  registerRoute('GET', '/api/v1/auth/whoami', async () => {
    const body: Whoami = {
      user_id: 'mock-user-uuid-0000-0000',
      email: 'demo@loopzero.example',
      name: 'Demo Operator',
      team_id: 'team-acme',
      team_slug: 'acme',
      teams: [
        { id: 'team-acme', slug: 'acme', role: 'owner' },
        { id: 'team-personal', slug: 'personal', role: 'owner' },
      ],
    };
    return Response.json(body);
  });

  registerRoute('POST', '/api/v1/auth/team/switch', async (req) => {
    const body = (await req.json()) as { team_id?: string };
    if (!body.team_id) return err('INVALID_INPUT', 'team_id required', 400);
    return Response.json({ team_id: body.team_id, status: 'switched' });
  });
}

// ============================================================================
// Projects (api/internal/routes/projects.go)
// ============================================================================

const RESERVED_SLUGS = new Set([
  'new', 'edit', 'create', 'delete', 'settings', 'admin', 'api',
]);

function registerProjectHandlers() {
  registerRoute('GET', '/api/v1/projects', async () => {
    const body: ProjectListResponse = { projects: db.projects, total: db.projects.length };
    return Response.json(body);
  });

  registerRoute('GET', '/api/v1/projects/{name}', async (_req, { name }) => {
    const project = db.projects.find((p) => p.name === name);
    if (!project) return err('NOT_FOUND', `Project "${name}" not found`, 404);
    return Response.json(project);
  });

  registerRoute('POST', '/api/v1/projects', async (req) => {
    const body = (await req.json()) as { name?: string };
    if (!body.name) return err('INVALID_INPUT', 'name is required', 400);
    if (RESERVED_SLUGS.has(body.name)) {
      return err('RESERVED_SLUG', `"${body.name}" is a reserved slug`, 400);
    }
    if (db.projects.find((p) => p.name === body.name)) {
      return err('CONFLICT', `Project name "${body.name}" already taken`, 409);
    }
    const project = makeProject(body.name);
    db.projects.push(project);
    return Response.json(project, { status: 201 });
  });

  registerRoute('DELETE', '/api/v1/projects/{name}', async (_req, { name }) => {
    const i = db.projects.findIndex((p) => p.name === name);
    if (i < 0) return err('NOT_FOUND', `Project "${name}" not found`, 404);
    db.projects.splice(i, 1);
    return new Response(null, { status: 204 });
  });

  registerRoute(
    'DELETE',
    '/api/v1/projects/{name}/environments/{env}',
    async () => new Response(null, { status: 204 }),
  );

  // Project-scoped operations
  registerRoute('GET', '/api/v1/projects/{name}/status', async (_req, { name }) => {
    const status: ProjectStatusDetail = makeProjectStatus(name);
    return Response.json(status);
  });

  registerRoute('GET', '/api/v1/projects/{name}/builds', async (_req, { name }) => {
    const builds = db.builds.filter((b) => b.project_id === name);
    const body: BuildListResponse = { builds, total: builds.length };
    return Response.json(body);
  });

  registerRoute('POST', '/api/v1/projects/{name}/scale', async (req, { name }) => {
    const body = (await req.json()) as { replicas?: number; min?: number; max?: number };
    return Response.json({ project: name, applied: body });
  });

  registerRoute('POST', '/api/v1/projects/{name}/rollback', async (req) => {
    const body = (await req.json()) as { build_id?: string };
    if (!body.build_id) return err('INVALID_INPUT', 'build_id required', 400);
    return Response.json({ release_id: `rel-${Date.now()}`, status: 'queued' });
  });

  registerRoute('POST', '/api/v1/projects/{name}/promote', async (req) => {
    const body = (await req.json()) as { from_environment?: string; to_environment?: string };
    return Response.json({
      release_id: `rel-${Date.now()}`,
      status: 'queued',
      from: body.from_environment,
      to: body.to_environment,
    });
  });

  // GitHub repo connection
  registerRoute('GET', '/api/v1/projects/{name}/connect', async (_req, { name }) => {
    const conn: ProjectConnection = makeProjectConnection(name);
    return Response.json(conn);
  });

  registerRoute('POST', '/api/v1/projects/{name}/connect', async (req, { name }) => {
    const body = (await req.json()) as {
      repo_url?: string;
      branch?: string;
      auto_deploy?: boolean;
    };
    if (!body.repo_url) return err('INVALID_INPUT', 'repo_url required', 400);
    return Response.json({
      connected: true,
      repo_url: body.repo_url,
      branch: body.branch ?? 'main',
      auto_deploy: body.auto_deploy ?? true,
      project: name,
    });
  });

  registerRoute('DELETE', '/api/v1/projects/{name}/connect', async () => {
    return Response.json({ connected: false });
  });
}

// ============================================================================
// Builds (api/internal/routes/builds.go)
// ============================================================================

function registerBuildHandlers() {
  registerRoute('GET', '/api/v1/builds/{id}', async (_req, { id }) => {
    const build = db.builds.find((b) => b.build_id === id);
    if (!build) return err('NOT_FOUND', `Build "${id}" not found`, 404);
    return Response.json(build);
  });

  registerRoute('POST', '/api/v1/builds/{id}/cancel', async (_req, { id }) => {
    const build = db.builds.find((b) => b.build_id === id);
    if (!build) return err('NOT_FOUND', `Build "${id}" not found`, 404);
    if (build.status !== 'running' && build.status !== 'queued') {
      return err('INVALID_STATE', `Cannot cancel build in status "${build.status}"`, 409);
    }
    build.status = 'cancelled';
    build.finished_at = new Date().toISOString();
    return Response.json(build);
  });

  registerRoute('GET', '/api/v1/builds/{id}/sbom', async () => {
    // LIVE bug 2026-06-06: shape updated to match backend (metadata
    // per app + presigned download_url, not inline CycloneDX).
    const sbom: BuildSBOM = {
      build_id: 'b-mock',
      apps: [
        {
          app_name: 'api',
          sbom_format: 'cyclonedx-json',
          component_count: 949,
          generated_at: new Date().toISOString(),
          download_url: 'http://mock.local/sbom/api.cdx.json',
        },
      ],
    };
    return Response.json(sbom);
  });

  registerRoute('GET', '/api/v1/builds/{id}/attestation', async () => {
    const att: BuildAttestation = {
      build_id: 'b-mock',
      image_digest: 'sha256:abcdef0123456789',
      image_signature: 'mock-registry/teste/api:abc@sha256:abcdef',
      signed_at: new Date().toISOString(),
      signed: true,
    };
    return Response.json(att);
  });

  registerRoute('GET', '/api/v1/builds/{id}/logs', async () => {
    return Response.json({ lines: buildLogLines });
  });
}

// ============================================================================
// SSE log streams
// ============================================================================

function registerLogStreamHandlers() {
  registerSSE('/api/v1/builds/{id}/logs/stream', (es) => {
    let i = 0;
    const interval = setInterval(() => {
      if (i >= buildLogLines.length) {
        clearInterval(interval);
        return;
      }
      es.dispatchEvent(
        new MessageEvent('message', { data: JSON.stringify(buildLogLines[i++]) }),
      );
    }, 600);
    return () => clearInterval(interval);
  });

  registerSSE('/api/v1/projects/{name}/logs/stream', (es) => {
    let i = 0;
    const interval = setInterval(() => {
      const line = runtimeLogLines[i % runtimeLogLines.length];
      i++;
      es.dispatchEvent(
        new MessageEvent('message', {
          data: JSON.stringify({ ...line, ts: new Date().toISOString() }),
        }),
      );
    }, 1000);
    return () => clearInterval(interval);
  });

  registerSSE('/api/v1/builds/{id}/stream', (es) => {
    const events = ['queued', 'running', 'pushing', 'releasing', 'succeeded'];
    let i = 0;
    const interval = setInterval(() => {
      if (i >= events.length) {
        clearInterval(interval);
        return;
      }
      es.dispatchEvent(
        new MessageEvent('message', { data: JSON.stringify({ status: events[i++] }) }),
      );
    }, 1200);
    return () => clearInterval(interval);
  });
}

// ============================================================================
// Secrets
// ============================================================================

function registerSecretHandlers() {
  registerRoute('GET', '/api/v1/secrets', async () => {
    const body: SecretListResponse = { secrets: db.secrets, total: db.secrets.length };
    return Response.json(body);
  });

  registerRoute('POST', '/api/v1/secrets', async (req) => {
    const body = (await req.json()) as { name?: string; environments?: string[] };
    if (!body.name) return err('INVALID_INPUT', 'name required', 400);
    if (db.secrets.find((s) => s.name === body.name)) {
      return err('CONFLICT', `Secret "${body.name}" already exists`, 409);
    }
    const secret: Secret = {
      name: body.name,
      environments: body.environments ?? ['production'],
      last_updated_by: 'demo@loopzero.example',
      updated_at: new Date().toISOString(),
      eso_sync_status: 'pending',
    };
    db.secrets.push(secret);
    return Response.json(secret, { status: 201 });
  });

  registerRoute('DELETE', '/api/v1/secrets/{name}', async (_req, { name }) => {
    const i = db.secrets.findIndex((s) => s.name === name);
    if (i < 0) return err('NOT_FOUND', `Secret "${name}" not found`, 404);
    db.secrets.splice(i, 1);
    return new Response(null, { status: 204 });
  });
}

// ============================================================================
// Teams
// ============================================================================

function registerTeamHandlers() {
  registerRoute('GET', '/api/v1/teams/members', async () => {
    const body: TeamListResponse = { members: db.members };
    return Response.json(body);
  });

  registerRoute('POST', '/api/v1/teams/members', async (req) => {
    const body = (await req.json()) as { email?: string; role?: 'admin' | 'member' };
    if (!body.email) return err('INVALID_INPUT', 'email required', 400);
    if (db.members.find((m) => m.email === body.email)) {
      return err('CONFLICT', `Member "${body.email}" already exists`, 409);
    }
    const member: TeamMember = {
      email: body.email,
      role: body.role ?? 'member',
      joined: new Date().toISOString().slice(0, 10),
    };
    db.members.push(member);
    return Response.json(member, { status: 201 });
  });

  registerRoute('DELETE', '/api/v1/teams/members/{username}', async (_req, { username }) => {
    const i = db.members.findIndex((m) => m.username === username || m.email === username);
    if (i < 0) return err('NOT_FOUND', `Member "${username}" not found`, 404);
    if (db.members[i].role === 'owner') {
      return err('FORBIDDEN', 'Cannot remove owner', 403);
    }
    db.members.splice(i, 1);
    return new Response(null, { status: 204 });
  });

  registerRoute('PATCH', '/api/v1/teams/members/{username}', async (req, { username }) => {
    const body = (await req.json()) as { role?: 'admin' | 'member' };
    const member = db.members.find((m) => m.username === username || m.email === username);
    if (!member) return err('NOT_FOUND', `Member "${username}" not found`, 404);
    if (body.role) member.role = body.role;
    return Response.json(member);
  });
}

// ============================================================================
// Environments
// ============================================================================

function registerEnvironmentHandlers() {
  registerRoute('GET', '/api/v1/environments', async () => {
    const body: EnvironmentListResponse = {
      environments: db.environments,
      total: db.environments.length,
    };
    return Response.json(body);
  });

  registerRoute('GET', '/api/v1/environments/{name}', async (_req, { name }) => {
    const env = db.environments.find((e) => e.name === name);
    if (!env) return err('NOT_FOUND', `Environment "${name}" not found`, 404);
    return Response.json(env);
  });

  registerRoute('POST', '/api/v1/environments', async (req) => {
    const body = (await req.json()) as { name?: string; protected?: boolean };
    if (!body.name) return err('INVALID_INPUT', 'name required', 400);
    if (db.environments.find((e) => e.name === body.name)) {
      return err('CONFLICT', `Environment "${body.name}" already exists`, 409);
    }
    const env: Environment = {
      name: body.name,
      status: 'idle',
      projects: 0,
      protected: body.protected ?? false,
    };
    db.environments.push(env);
    return Response.json(env, { status: 201 });
  });

  registerRoute('DELETE', '/api/v1/environments/{name}', async (_req, { name }) => {
    const i = db.environments.findIndex((e) => e.name === name);
    if (i < 0) return err('NOT_FOUND', `Environment "${name}" not found`, 404);
    db.environments.splice(i, 1);
    return new Response(null, { status: 204 });
  });
}

// ============================================================================
// Domains
// ============================================================================

function registerDomainHandlers() {
  registerRoute('GET', '/api/v1/domains', async () => {
    const body: DomainListResponse = { domains: db.domains, total: db.domains.length };
    return Response.json(body);
  });

  registerRoute('GET', '/api/v1/domains/{domain}', async (_req, { domain }) => {
    const d = db.domains.find((x) => x.domain === domain);
    if (!d) return err('NOT_FOUND', `Domain "${domain}" not found`, 404);
    return Response.json(d);
  });

  registerRoute('POST', '/api/v1/domains', async (req) => {
    const body = (await req.json()) as {
      domain?: string;
      project?: string;
      app?: string;
      environment?: string;
    };
    if (!body.domain) return err('INVALID_INPUT', 'domain required', 400);
    if (db.domains.find((d) => d.domain === body.domain)) {
      return err('CONFLICT', `Domain "${body.domain}" already exists`, 409);
    }
    const domain: Domain = {
      id: `dom-${Date.now()}`,
      domain: body.domain,
      project: body.project ?? '',
      app: body.app ?? '',
      environment: body.environment ?? 'production',
      status: 'pending_verification',
      cname_target: `${body.project ?? 'app'}.loopzero.app`,
      created_at: new Date().toISOString(),
      verification: {
        txt_name: `_loopzero.${body.domain}`,
        txt_value: `verify=${Math.random().toString(36).slice(2, 18)}`,
      },
    };
    db.domains.push(domain);
    return Response.json(domain, { status: 201 });
  });

  registerRoute('POST', '/api/v1/domains/{domain}/verify', async (_req, { domain }) => {
    const d = db.domains.find((x) => x.domain === domain);
    if (!d) return err('NOT_FOUND', `Domain "${domain}" not found`, 404);
    d.status = 'verified';
    d.verified_at = new Date().toISOString();
    delete d.verification;
    return Response.json(d);
  });

  registerRoute('DELETE', '/api/v1/domains/{domain}', async (_req, { domain }) => {
    const i = db.domains.findIndex((x) => x.domain === domain);
    if (i < 0) return err('NOT_FOUND', `Domain "${domain}" not found`, 404);
    db.domains.splice(i, 1);
    return new Response(null, { status: 204 });
  });
}

// ============================================================================
// Previews
// ============================================================================

function registerPreviewHandlers() {
  registerRoute('GET', '/api/v1/projects/{project}/previews', async (_req, { project }) => {
    const previews = db.previews.filter((p) => p.project === project);
    const body: PreviewListResponse = { previews, total: previews.length };
    return Response.json(body);
  });

  registerRoute('GET', '/api/v1/projects/{project}/previews/{id}', async (_req, { id }) => {
    const preview = db.previews.find((p) => p.id === id);
    if (!preview) return err('NOT_FOUND', `Preview "${id}" not found`, 404);
    return Response.json(preview);
  });

  registerRoute('POST', '/api/v1/projects/{project}/previews', async (req, { project }) => {
    const body = (await req.json()) as { branch?: string; commit_sha?: string };
    if (!body.branch) return err('INVALID_INPUT', 'branch required', 400);
    const preview: Preview = {
      id: `prv-${Date.now()}`,
      project,
      branch: body.branch,
      url: `https://${body.branch.replace(/\W/g, '-')}.preview.acme.com`,
      status: 'creating',
      created_by: 'demo@loopzero.example',
      created_at: new Date().toISOString(),
      expires_at: new Date(Date.now() + 3 * 24 * 60 * 60 * 1000).toISOString(),
    };
    db.previews.push(preview);
    return Response.json(preview, { status: 201 });
  });

  registerRoute('DELETE', '/api/v1/projects/{project}/previews/{id}', async (_req, { id }) => {
    const i = db.previews.findIndex((p) => p.id === id);
    if (i < 0) return err('NOT_FOUND', `Preview "${id}" not found`, 404);
    db.previews.splice(i, 1);
    return new Response(null, { status: 204 });
  });
}

// ============================================================================
// Audit log
// ============================================================================

function registerAuditHandlers() {
  registerRoute('GET', '/api/v1/audit', async (req) => {
    const url = new URL(req.url);
    const action = url.searchParams.get('action');
    const actor = url.searchParams.get('actor');
    let entries = auditEntriesFixture;
    if (action) entries = entries.filter((e) => e.action.startsWith(action));
    if (actor) entries = entries.filter((e) => e.actor_email.includes(actor));
    const body: AuditListResponse = { entries, total: entries.length };
    return Response.json(body);
  });
}

// ============================================================================
// Billing
// ============================================================================

function registerBillingHandlers() {
  registerRoute('GET', '/api/v1/billing', async () => {
    const body: BillingResponse = {
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
      plan_cap: {
        max_projects: 10,
        max_environments: 3,
        max_builds_per_month: 1000,
        max_replicas: 5,
        max_seats: 10,
      },
    };
    return Response.json(body);
  });

  registerRoute('POST', '/api/v1/billing/portal', async () => {
    return Response.json({ url: 'https://billing.stripe.com/p/mock_session' });
  });
}

// ============================================================================
// Tenants (account lifecycle)
// ============================================================================

function registerTenantHandlers() {
  registerRoute('DELETE', '/api/v1/tenants/{id}', async (_req, { id }) => {
    const body: TenantDeleteResponse = {
      tenant_id: id,
      status: 'PENDING_DELETION',
      grace_period_days: 30,
    };
    return Response.json(body);
  });

  registerRoute('POST', '/api/v1/tenants/{id}/restore', async (_req, { id }) => {
    return Response.json({ tenant_id: id, status: 'ACTIVE' });
  });
}

// ============================================================================
// Health / Version (no UI consumer but completeness)
// ============================================================================

function registerSystemHandlers() {
  registerRoute('GET', '/api/v1/health', async () => Response.json({ ok: true }));
  registerRoute('GET', '/api/v1/health/deep', async () =>
    Response.json({ ok: true, postgres: 'ok', argocd: 'ok' }),
  );
  registerRoute('GET', '/api/v1/version', async () =>
    Response.json({ version: 'mock-1.0.0', commit: 'abc123', build_date: '2026-05-26' }),
  );
}

// ============================================================================
// Public install
// ============================================================================

let installed = false;

/** Register every mock handler. Idempotent — safe to call multiple times. */
export function registerAllMockHandlers(): void {
  if (installed) return;
  installed = true;
  registerAuthHandlers();
  registerProjectHandlers();
  registerBuildHandlers();
  registerLogStreamHandlers();
  registerSecretHandlers();
  registerTeamHandlers();
  registerEnvironmentHandlers();
  registerDomainHandlers();
  registerPreviewHandlers();
  registerAuditHandlers();
  registerBillingHandlers();
  registerTenantHandlers();
  registerSystemHandlers();
}

/** Test-only: reset the in-memory DB to initial fixture state. */
export function resetMockDB(): void {
  db.projects = [...projectsFixture];
  db.builds = [...buildsFixture];
  db.secrets = [...secretsFixture];
  db.members = [...teamMembersFixture];
  db.environments = [...environmentsFixture];
  db.domains = [...domainsFixture];
  db.previews = [...previewsFixture];
  installed = false;
}
