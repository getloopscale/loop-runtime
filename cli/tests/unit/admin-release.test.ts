// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import {
  runAdminReleaseInspect,
  AdminReleaseInspectDeps,
  AdminReleaseInspectResponse,
} from '../../src/commands/admin-release';

function makeResponse(
  overrides: Partial<AdminReleaseInspectResponse> = {},
): AdminReleaseInspectResponse {
  return {
    release_id: 'rel-001',
    build_id: 'bld-001',
    tenant_id: 'tn-acme',
    project_id: 'proj-test',
    environment: 'production',
    status: 'ACTIVE',
    correlation_id: 'corr-001',
    workflow_id: 'loopzero-build-001-xyz',
    image_digest: 'sha256:abc',
    image_signature: 'registry.example.com/x@sha256:abc',
    image_signed_at: '2026-05-28T20:00:00Z',
    image_attestation_rekor_url:
      'https://rekor.loopzero.test/api/v1/log/entries?logIndex=42',
    signing_path: 'keyless',
    attestation_path: 'present',
    audit_log_entries: [
      {
        action: 'release.attestation_url_set',
        actor: 'system:release-attestation',
        resource_id: 'rel-001',
        created_at: '2026-05-28T20:00:01Z',
      },
    ],
    ...overrides,
  };
}

function makeDeps(overrides: Partial<AdminReleaseInspectDeps> = {}): {
  deps: AdminReleaseInspectDeps;
  out: string[];
  err: string[];
} {
  const out: string[] = [];
  const err: string[] = [];
  const deps: AdminReleaseInspectDeps = {
    getServiceAuth: () => 'fake-secret',
    getRelease: async () => ({ status: 200, body: makeResponse() }),
    writeTo: (m: string) => out.push(m),
    writeError: (m: string) => err.push(m),
    ...overrides,
  };
  return { deps, out, err };
}

describe('runAdminReleaseInspect', () => {
  test('missing release id returns exit 1', async () => {
    const { deps, err } = makeDeps();
    const code = await runAdminReleaseInspect({ releaseId: '' }, deps);
    expect(code).toBe(1);
    expect(err.join('\n')).toMatch(/Release id is required/);
  });

  test('missing LOOPZERO_SERVICE_AUTH returns exit 4', async () => {
    const { deps, err } = makeDeps({ getServiceAuth: () => null });
    const code = await runAdminReleaseInspect({ releaseId: 'rel-001' }, deps);
    expect(code).toBe(4);
    expect(err.join('\n')).toMatch(/LOOPZERO_SERVICE_AUTH/);
  });

  test('403 from API → exit 4 (auth rejected)', async () => {
    const { deps, err } = makeDeps({
      getRelease: async () => ({
        status: 403,
        body: { error: 'forbidden', code: 'FORBIDDEN' },
      }),
    });
    const code = await runAdminReleaseInspect({ releaseId: 'rel-001' }, deps);
    expect(code).toBe(4);
    expect(err.join('\n')).toMatch(/Admin auth rejected/);
  });

  test('404 from API → exit 1 with not-found message', async () => {
    const { deps, err } = makeDeps({
      getRelease: async () => ({
        status: 404,
        body: { error: 'not found', code: 'RELEASE_NOT_FOUND' },
      }),
    });
    const code = await runAdminReleaseInspect(
      { releaseId: 'rel-missing' },
      deps,
    );
    expect(code).toBe(1);
    expect(err.join('\n')).toMatch(/Release "rel-missing" not found/);
  });

  test('happy path emits text rendering with signing chain', async () => {
    const { deps, out } = makeDeps();
    const code = await runAdminReleaseInspect({ releaseId: 'rel-001' }, deps);
    expect(code).toBe(0);
    const rendered = out.join('\n');
    expect(rendered).toMatch(/Release\s+rel-001/);
    expect(rendered).toMatch(/Signing path\s+keyless/);
    expect(rendered).toMatch(/Attestation\s+present/);
    expect(rendered).toMatch(/Rekor entry\s+https:\/\/rekor\.loopzero\.test/);
  });

  test('--json emits the raw JSON payload exactly', async () => {
    const { deps, out } = makeDeps();
    const code = await runAdminReleaseInspect(
      { releaseId: 'rel-001', json: true },
      deps,
    );
    expect(code).toBe(0);
    const parsed = JSON.parse(out[0]);
    expect(parsed.release_id).toBe('rel-001');
    expect(parsed.signing_path).toBe('keyless');
    expect(parsed.audit_log_entries.length).toBe(1);
  });

  test('Rule 3: rendered output contains no internal terms', async () => {
    // CR-10 + CLAUDE.md Rule 3: no ArgoCD / Gitea / Kaniko / advisory
    // lock / pg_advisory in the user-visible rendering.
    const { deps, out } = makeDeps();
    await runAdminReleaseInspect({ releaseId: 'rel-001' }, deps);
    const rendered = out.join('\n');
    const forbidden = [
      'ArgoCD',
      'argocd',
      'GitOps',
      'Gitea',
      'MinIO',
      'Kaniko',
      'advisory lock',
      'pg_advisory',
      'tenant_id',
    ];
    for (const term of forbidden) {
      expect(rendered).not.toMatch(new RegExp(term, 'i'));
    }
  });

  test('EC-14: reaped workflow renders graceful fallback message', async () => {
    const { deps, out } = makeDeps({
      getRelease: async () => ({
        status: 200,
        body: makeResponse({ workflow_id: null }),
      }),
    });
    const code = await runAdminReleaseInspect({ releaseId: 'rel-001' }, deps);
    expect(code).toBe(0);
    const rendered = out.join('\n');
    expect(rendered).toMatch(/Workflow.*not available/);
  });

  test('500 from API → exit 1 with generic failure', async () => {
    const { deps, err } = makeDeps({
      getRelease: async () => ({
        status: 500,
        body: { error: 'internal', code: 'INSPECT_FAILED' },
      }),
    });
    const code = await runAdminReleaseInspect({ releaseId: 'rel-001' }, deps);
    expect(code).toBe(1);
    expect(err.join('\n')).toMatch(/status 500/);
  });

  test('--json on error emits structured JSON to stdout', async () => {
    const { deps, out, err } = makeDeps({
      getRelease: async () => ({
        status: 404,
        body: { error: 'not found', code: 'RELEASE_NOT_FOUND' },
      }),
    });
    const code = await runAdminReleaseInspect(
      { releaseId: 'rel-missing', json: true },
      deps,
    );
    expect(code).toBe(1);
    expect(err).toEqual([]);
    expect(out.length).toBe(1);
    const parsed = JSON.parse(out[0]);
    expect(parsed.exit_code).toBe(1);
    expect(parsed.code).toBe('RELEASE_NOT_FOUND');
  });
});
