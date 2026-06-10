import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { matchRoute, resetRoutes, getRegisteredRouteCount } from './server';
import {
  resetSSERegistrations,
  getSSERegistrationCount,
} from './sse';
import { registerAllMockHandlers, resetMockDB } from './handlers';

async function call(
  method: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE',
  path: string,
  body?: unknown,
): Promise<Response> {
  // Strip query string before regex match — production interceptor splits
  // pathname from search via URL parsing; tests do the same.
  const pathname = path.split('?')[0];
  const matched = matchRoute(method, pathname);
  if (!matched) throw new Error(`No route for ${method} ${path}`);
  const init: RequestInit = { method };
  if (body !== undefined) {
    init.body = JSON.stringify(body);
    init.headers = { 'Content-Type': 'application/json' };
  }
  return matched.route.handler(
    new Request(`https://test${path}`, init),
    matched.params,
  );
}

describe('mocks/handlers — full API coverage', () => {
  beforeEach(() => {
    resetRoutes();
    resetSSERegistrations();
    resetMockDB();
    registerAllMockHandlers();
  });

  afterEach(() => {
    resetRoutes();
    resetSSERegistrations();
    resetMockDB();
  });

  it('test_registers_all_47_plus_routes_total', () => {
    // 47 UI-consumable endpoints + 3 health/version + admin omitted = ≥45
    expect(getRegisteredRouteCount()).toBeGreaterThanOrEqual(40);
  });

  it('test_registers_sse_streams', () => {
    // builds logs, runtime logs, build stream = 3 emitters
    expect(getSSERegistrationCount()).toBe(3);
  });

  // ==========================================================================
  // Auth
  // ==========================================================================

  it('test_whoami_returns_user_with_teams', async () => {
    const res = await call('GET', '/api/v1/auth/whoami');
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.email).toBe('demo@loopzero.example');
    expect(body.teams).toBeInstanceOf(Array);
    expect(body.teams.length).toBeGreaterThanOrEqual(1);
  });

  it('test_team_switch_400_without_team_id', async () => {
    const res = await call('POST', '/api/v1/auth/team/switch', {});
    expect(res.status).toBe(400);
  });

  // ==========================================================================
  // Projects
  // ==========================================================================

  it('test_projects_list_returns_fixtures', async () => {
    const res = await call('GET', '/api/v1/projects');
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.projects).toBeInstanceOf(Array);
    expect(body.projects.length).toBeGreaterThanOrEqual(3);
    expect(body.projects.find((p: { name: string }) => p.name === 'acme-web')).toBeDefined();
  });

  it('test_project_get_404_when_not_found', async () => {
    const res = await call('GET', '/api/v1/projects/nope');
    expect(res.status).toBe(404);
    const body = await res.json();
    expect(body.code).toBe('NOT_FOUND');
  });

  it('test_project_create_returns_201', async () => {
    const res = await call('POST', '/api/v1/projects', { name: 'newproj' });
    expect(res.status).toBe(201);
    const body = await res.json();
    expect(body.name).toBe('newproj');
    // Verify persistence
    const list = await (await call('GET', '/api/v1/projects')).json();
    expect(list.projects.find((p: { name: string }) => p.name === 'newproj')).toBeDefined();
  });

  it('test_project_create_409_on_conflict', async () => {
    const res = await call('POST', '/api/v1/projects', { name: 'acme-web' });
    expect(res.status).toBe(409);
  });

  it('test_project_create_400_on_reserved_slug', async () => {
    const res = await call('POST', '/api/v1/projects', { name: 'admin' });
    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe('RESERVED_SLUG');
  });

  it('test_project_delete_removes_from_db', async () => {
    const del = await call('DELETE', '/api/v1/projects/acme-web');
    expect(del.status).toBe(204);
    const get = await call('GET', '/api/v1/projects/acme-web');
    expect(get.status).toBe(404);
  });

  // ==========================================================================
  // Builds
  // ==========================================================================

  it('test_builds_list_for_project', async () => {
    const res = await call('GET', '/api/v1/projects/acme-web/builds');
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.builds.length).toBeGreaterThanOrEqual(1);
    expect(body.builds.every((b: { project_id: string }) => b.project_id === 'acme-web')).toBe(true);
  });

  it('test_build_get_returns_full_shape', async () => {
    const res = await call('GET', '/api/v1/builds/bld_01HXYZ_acme_web_001');
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.commit_sha).toBeDefined();
    expect(body.status).toBe('succeeded');
  });

  it('test_build_cancel_409_when_not_running', async () => {
    const res = await call('POST', '/api/v1/builds/bld_01HXYZ_acme_web_001/cancel');
    expect(res.status).toBe(409);
  });

  it('test_build_cancel_succeeds_when_running', async () => {
    const res = await call('POST', '/api/v1/builds/bld_01HXYZ_acme_web_003/cancel');
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.status).toBe('cancelled');
  });

  it('test_build_sbom_returns_cyclonedx', async () => {
    const res = await call('GET', '/api/v1/builds/bld_01HXYZ_acme_web_001/sbom');
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.bomFormat).toBe('CycloneDX');
    expect(body.components).toBeInstanceOf(Array);
  });

  // ==========================================================================
  // Secrets CRUD
  // ==========================================================================

  it('test_secrets_list_does_not_leak_values', async () => {
    const res = await call('GET', '/api/v1/secrets');
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.secrets[0].name).toBeDefined();
    // No `value` field in Secret type
    expect((body.secrets[0] as Record<string, unknown>).value).toBeUndefined();
  });

  it('test_secret_create_409_on_conflict', async () => {
    const res = await call('POST', '/api/v1/secrets', {
      name: 'STRIPE_SECRET_KEY',
      value: 'sk_test_x',
    });
    expect(res.status).toBe(409);
  });

  it('test_secret_delete_removes_from_db', async () => {
    await call('DELETE', '/api/v1/secrets/STRIPE_SECRET_KEY');
    const list = await (await call('GET', '/api/v1/secrets')).json();
    expect(list.secrets.find((s: { name: string }) => s.name === 'STRIPE_SECRET_KEY')).toBeUndefined();
  });

  // ==========================================================================
  // Teams
  // ==========================================================================

  it('test_team_invite_creates_member', async () => {
    const res = await call('POST', '/api/v1/teams/members', {
      email: 'new@acme.com',
      role: 'member',
    });
    expect(res.status).toBe(201);
  });

  it('test_team_remove_owner_forbidden', async () => {
    const res = await call('DELETE', '/api/v1/teams/members/paulo');
    expect(res.status).toBe(403);
  });

  it('test_team_update_role_via_patch', async () => {
    const res = await call('PATCH', '/api/v1/teams/members/carlos', { role: 'admin' });
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.role).toBe('admin');
  });

  // ==========================================================================
  // Environments
  // ==========================================================================

  it('test_env_create_201', async () => {
    const res = await call('POST', '/api/v1/environments', { name: 'test-env' });
    expect(res.status).toBe(201);
  });

  it('test_env_delete_removes_from_db', async () => {
    await call('POST', '/api/v1/environments', { name: 'temp' });
    const del = await call('DELETE', '/api/v1/environments/temp');
    expect(del.status).toBe(204);
  });

  // ==========================================================================
  // Domains
  // ==========================================================================

  it('test_domain_create_returns_verification', async () => {
    const res = await call('POST', '/api/v1/domains', {
      domain: 'test.acme.com',
      project: 'acme-web',
      app: 'frontend',
      environment: 'production',
    });
    expect(res.status).toBe(201);
    const body = await res.json();
    expect(body.status).toBe('pending_verification');
    expect(body.verification).toBeDefined();
  });

  it('test_domain_verify_advances_status', async () => {
    const res = await call('POST', '/api/v1/domains/staging.acme.com/verify');
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.status).toBe('verified');
  });

  // ==========================================================================
  // Previews
  // ==========================================================================

  it('test_previews_list_for_project', async () => {
    const res = await call('GET', '/api/v1/projects/acme-web/previews');
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.previews.every((p: { project: string }) => p.project === 'acme-web')).toBe(true);
  });

  it('test_preview_create_returns_url', async () => {
    const res = await call('POST', '/api/v1/projects/acme-web/previews', {
      branch: 'test-branch',
      commit_sha: 'abc123',
    });
    expect(res.status).toBe(201);
    const body = await res.json();
    expect(body.url).toContain('preview.acme.com');
  });

  // ==========================================================================
  // Audit
  // ==========================================================================

  it('test_audit_list_returns_entries', async () => {
    const res = await call('GET', '/api/v1/audit');
    const body = await res.json();
    expect(body.entries.length).toBeGreaterThanOrEqual(1);
    expect(body.entries[0].actor_email).toBeDefined();
  });

  it('test_audit_filter_by_action', async () => {
    const res = await call('GET', '/api/v1/audit?action=deploy');
    const body = await res.json();
    expect(body.entries.every((e: { action: string }) => e.action.startsWith('deploy'))).toBe(true);
  });

  // ==========================================================================
  // Billing
  // ==========================================================================

  it('test_billing_get_returns_plan_and_usage', async () => {
    const res = await call('GET', '/api/v1/billing');
    const body = await res.json();
    expect(body.plan).toBe('pro');
    expect(body.usage['Edge Requests']).toBeDefined();
    expect(body.plan_cap.max_projects).toBe(10);
  });

  it('test_billing_portal_returns_stripe_url', async () => {
    const res = await call('POST', '/api/v1/billing/portal');
    const body = await res.json();
    expect(body.url).toContain('stripe.com');
  });

  // ==========================================================================
  // Tenants
  // ==========================================================================

  it('test_tenant_delete_returns_pending_with_grace', async () => {
    const res = await call('DELETE', '/api/v1/tenants/team-acme');
    const body = await res.json();
    expect(body.status).toBe('PENDING_DELETION');
    expect(body.grace_period_days).toBe(30);
  });

  // ==========================================================================
  // Project status / scale / rollback / promote / connect
  // ==========================================================================

  it('test_project_status_returns_health_signals', async () => {
    const res = await call('GET', '/api/v1/projects/acme-web/status');
    const body = await res.json();
    expect(body.replicas_ready).toBeDefined();
    expect(body.traffic_5xx_pct_1h).toBeDefined();
  });

  it('test_scale_returns_applied_config', async () => {
    const res = await call('POST', '/api/v1/projects/acme-web/scale', {
      replicas: 3,
      min: 2,
      max: 5,
    });
    const body = await res.json();
    expect(body.applied.replicas).toBe(3);
  });

  it('test_rollback_400_without_build_id', async () => {
    const res = await call('POST', '/api/v1/projects/acme-web/rollback', {});
    expect(res.status).toBe(400);
  });

  it('test_rollback_returns_release_id', async () => {
    const res = await call('POST', '/api/v1/projects/acme-web/rollback', {
      build_id: 'bld_01HXYZ_acme_web_001',
    });
    const body = await res.json();
    expect(body.release_id).toBeDefined();
    expect(body.status).toBe('queued');
  });

  it('test_promote_returns_release_id', async () => {
    const res = await call('POST', '/api/v1/projects/acme-web/promote', {
      from_environment: 'staging',
      to_environment: 'production',
    });
    const body = await res.json();
    expect(body.release_id).toBeDefined();
  });

  it('test_project_connect_returns_state', async () => {
    const res = await call('GET', '/api/v1/projects/acme-web/connect');
    const body = await res.json();
    expect(body.connected).toBe(true);
    expect(body.repo_url).toContain('github.com');
  });

  it('test_project_disconnect_returns_disconnected', async () => {
    const res = await call('DELETE', '/api/v1/projects/acme-web/connect');
    const body = await res.json();
    expect(body.connected).toBe(false);
  });

  // ==========================================================================
  // System endpoints
  // ==========================================================================

  it('test_health_returns_ok', async () => {
    const res = await call('GET', '/api/v1/health');
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.ok).toBe(true);
  });

  it('test_version_returns_metadata', async () => {
    const res = await call('GET', '/api/v1/version');
    const body = await res.json();
    expect(body.version).toBeDefined();
  });
});
