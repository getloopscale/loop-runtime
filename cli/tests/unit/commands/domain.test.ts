// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import {
  DomainAddCommand,
  DomainVerifyCommand,
  DomainStatusCommand,
  DomainListCommand,
  DomainRemoveCommand,
  DomainDeps,
} from '../../../src/commands/domain';
import { ApiError } from '../../../src/api/client';

// --- Test helpers ---

function makeDeps(overrides: Partial<DomainDeps> = {}): DomainDeps {
  const output: string[] = [];
  const errors: string[] = [];
  return {
    getToken: () => 'test-token',
    createDomain: jest.fn().mockResolvedValue({
      id: 'dom-12345678',
      domain: 'api.mycompany.com',
      project: 'myapi',
      app: 'web',
      environment: 'production',
      status: 'pending_verification',
      verification: { txt_name: '_loopzero-verify.api.mycompany.com', txt_value: 'REDACTED_SECRET_055' },
      cname_target: 'cname.dev.loopzero.example',
    }),
    verifyDomain: jest.fn().mockResolvedValue({
      domain: 'api.mycompany.com',
      status: 'verified',
      verified_at: '2026-04-05T12:05:00Z',
      message: 'Provisioning in progress.',
    }),
    getDomain: jest.fn().mockResolvedValue({
      id: 'dom-12345678',
      domain: 'api.mycompany.com',
      project: 'myapi',
      app: 'web',
      environment: 'production',
      status: 'active',
      tls: { status: 'issued', expires_at: '2026-07-04T00:00:00Z' },
      cname_target: 'cname.dev.loopzero.example',
      created_at: '2026-04-05T12:00:00Z',
      verified_at: '2026-04-05T12:05:00Z',
    }),
    listDomains: jest.fn().mockResolvedValue({
      domains: [],
      total: 0,
      limit: 50,
      offset: 0,
    }),
    deleteDomain: jest.fn().mockResolvedValue({ ok: true, domain: 'api.mycompany.com', status: 'removing' }),
    writeTo: (msg: string) => output.push(msg),
    writeError: (msg: string) => errors.push(msg),
    ...overrides,
    _output: output,
    _errors: errors,
  } as DomainDeps & { _output: string[]; _errors: string[] };
}

function getOutput(deps: DomainDeps): string[] {
  return (deps as any)._output;
}
function getErrors(deps: DomainDeps): string[] {
  return (deps as any)._errors;
}

// ============================================================
// DomainAddCommand
// ============================================================

describe('DomainAddCommand', () => {
  it('returns exit 4 when not authenticated', async () => {
    const deps = makeDeps({ getToken: () => null });
    const cmd = new DomainAddCommand(deps);
    const code = await cmd.execute('api.example.com', { project: 'p', app: 'a', env: 'e' });
    expect(code).toBe(4);
  });

  it('returns exit 1 when --project is missing', async () => {
    const deps = makeDeps();
    const cmd = new DomainAddCommand(deps);
    const code = await cmd.execute('api.example.com', { project: '', app: 'a', env: 'e' });
    expect(code).toBe(1);
  });

  it('returns exit 1 when --app is missing', async () => {
    const deps = makeDeps();
    const cmd = new DomainAddCommand(deps);
    const code = await cmd.execute('api.example.com', { project: 'p', app: '', env: 'e' });
    expect(code).toBe(1);
  });

  it('returns exit 1 when --env is missing', async () => {
    const deps = makeDeps();
    const cmd = new DomainAddCommand(deps);
    const code = await cmd.execute('api.example.com', { project: 'p', app: 'a', env: '' });
    expect(code).toBe(1);
  });

  it('returns exit 0 on success (human output)', async () => {
    const deps = makeDeps();
    const cmd = new DomainAddCommand(deps);
    const code = await cmd.execute('api.mycompany.com', { project: 'myapi', app: 'web', env: 'production' });
    expect(code).toBe(0);
    expect(getOutput(deps).some(line => line.includes('Domain registered'))).toBe(true);
    expect(getOutput(deps).some(line => line.includes('cname.dev.loopzero.example'))).toBe(true);
  });

  it('returns exit 0 with JSON output', async () => {
    const deps = makeDeps();
    const cmd = new DomainAddCommand(deps);
    const code = await cmd.execute('api.mycompany.com', { project: 'myapi', app: 'web', env: 'production', json: true });
    expect(code).toBe(0);
    const json = JSON.parse(getOutput(deps)[0]);
    expect(json.domain).toBe('api.mycompany.com');
    expect(json.status).toBe('pending_verification');
    expect(json.verification.txt_name).toBe('_loopzero-verify.api.mycompany.com');
    expect(json.cname_target).toBe('cname.dev.loopzero.example');
  });

  it('returns exit 3 on 409 conflict', async () => {
    const deps = makeDeps({
      createDomain: jest.fn().mockRejectedValue(new ApiError('conflict', 1, 409, 'DOMAIN_ALREADY_EXISTS')),
    });
    const cmd = new DomainAddCommand(deps);
    const code = await cmd.execute('taken.com', { project: 'p', app: 'a', env: 'e' });
    expect(code).toBe(3);
  });

  it('--json error has correct shape', async () => {
    const deps = makeDeps({ getToken: () => null });
    const cmd = new DomainAddCommand(deps);
    await cmd.execute('api.example.com', { project: 'p', app: 'a', env: 'e', json: true });
    const json = JSON.parse(getOutput(deps)[0]);
    expect(json.error).toBeDefined();
    expect(json.exit_code).toBe(4);
    expect(json.code).toBe('AUTH_REQUIRED');
  });
});

// ============================================================
// DomainVerifyCommand
// ============================================================

describe('DomainVerifyCommand', () => {
  it('returns exit 4 when not authenticated', async () => {
    const deps = makeDeps({ getToken: () => null });
    const cmd = new DomainVerifyCommand(deps);
    expect(await cmd.execute('x.com')).toBe(4);
  });

  it('returns exit 1 when domain is empty', async () => {
    const deps = makeDeps();
    const cmd = new DomainVerifyCommand(deps);
    expect(await cmd.execute('')).toBe(1);
  });

  it('returns exit 0 on verified (human)', async () => {
    const deps = makeDeps();
    const cmd = new DomainVerifyCommand(deps);
    expect(await cmd.execute('api.mycompany.com')).toBe(0);
    expect(getOutput(deps).some(line => line.includes('DNS verified'))).toBe(true);
  });

  it('returns exit 0 on pending (human)', async () => {
    const deps = makeDeps({
      verifyDomain: jest.fn().mockResolvedValue({
        domain: 'x.com',
        status: 'pending_verification',
        verification_error: 'TXT record not found',
        hint: 'DNS propagation may take time.',
      }),
    });
    const cmd = new DomainVerifyCommand(deps);
    expect(await cmd.execute('x.com')).toBe(0);
    expect(getErrors(deps).some(line => line.includes('pending'))).toBe(true);
  });

  it('returns JSON shape on verified', async () => {
    const deps = makeDeps();
    const cmd = new DomainVerifyCommand(deps);
    await cmd.execute('api.mycompany.com', { json: true });
    const json = JSON.parse(getOutput(deps)[0]);
    expect(json.status).toBe('verified');
    expect(json.verified_at).toBeDefined();
  });
});

// ============================================================
// DomainStatusCommand
// ============================================================

describe('DomainStatusCommand', () => {
  it('returns exit 4 when not authenticated', async () => {
    const deps = makeDeps({ getToken: () => null });
    const cmd = new DomainStatusCommand(deps);
    expect(await cmd.execute('x.com')).toBe(4);
  });

  it('returns exit 0 on success (human)', async () => {
    const deps = makeDeps();
    const cmd = new DomainStatusCommand(deps);
    expect(await cmd.execute('api.mycompany.com')).toBe(0);
    expect(getOutput(deps).some(line => line.includes('active'))).toBe(true);
    expect(getOutput(deps).some(line => line.includes('web'))).toBe(true);
  });

  it('returns exit 1 on 404', async () => {
    const deps = makeDeps({
      getDomain: jest.fn().mockRejectedValue(new ApiError('not found', 1, 404, 'NOT_FOUND')),
    });
    const cmd = new DomainStatusCommand(deps);
    expect(await cmd.execute('gone.com')).toBe(1);
  });

  it('JSON output has correct shape', async () => {
    const deps = makeDeps();
    const cmd = new DomainStatusCommand(deps);
    await cmd.execute('api.mycompany.com', { json: true });
    const json = JSON.parse(getOutput(deps)[0]);
    expect(json.domain).toBe('api.mycompany.com');
    expect(json.project).toBe('myapi');
    expect(json.app).toBe('web');
    expect(json.status).toBe('active');
    expect(json.tls.status).toBe('issued');
  });
});

// ============================================================
// DomainListCommand
// ============================================================

describe('DomainListCommand', () => {
  it('returns exit 4 when not authenticated', async () => {
    const deps = makeDeps({ getToken: () => null });
    const cmd = new DomainListCommand(deps);
    expect(await cmd.execute()).toBe(4);
  });

  it('returns exit 0 with empty list', async () => {
    const deps = makeDeps();
    const cmd = new DomainListCommand(deps);
    expect(await cmd.execute()).toBe(0);
    expect(getOutput(deps).some(line => line.includes('No custom domains'))).toBe(true);
  });

  it('returns exit 0 with JSON on empty list', async () => {
    const deps = makeDeps();
    const cmd = new DomainListCommand(deps);
    await cmd.execute({ json: true });
    const json = JSON.parse(getOutput(deps)[0]);
    expect(json.domains).toEqual([]);
    expect(json.total).toBe(0);
  });
});

// ============================================================
// DomainRemoveCommand
// ============================================================

describe('DomainRemoveCommand', () => {
  it('returns exit 4 when not authenticated', async () => {
    const deps = makeDeps({ getToken: () => null });
    const cmd = new DomainRemoveCommand(deps);
    expect(await cmd.execute('x.com', { confirm: 'x.com' })).toBe(4);
  });

  it('returns exit 1 without --confirm', async () => {
    const deps = makeDeps();
    const cmd = new DomainRemoveCommand(deps);
    expect(await cmd.execute('api.example.com')).toBe(1);
    expect(getErrors(deps).some(line => line.includes('--confirm'))).toBe(true);
  });

  it('returns exit 1 when --confirm does not match', async () => {
    const deps = makeDeps();
    const cmd = new DomainRemoveCommand(deps);
    expect(await cmd.execute('api.example.com', { confirm: 'wrong' })).toBe(1);
  });

  it('returns exit 0 on successful removal', async () => {
    const deps = makeDeps();
    const cmd = new DomainRemoveCommand(deps);
    expect(await cmd.execute('api.mycompany.com', { confirm: 'api.mycompany.com' })).toBe(0);
    expect(getOutput(deps).some(line => line.includes('removal in progress'))).toBe(true);
  });

  it('returns exit 1 on 404', async () => {
    const deps = makeDeps({
      deleteDomain: jest.fn().mockRejectedValue(new ApiError('not found', 1, 404, 'NOT_FOUND')),
    });
    const cmd = new DomainRemoveCommand(deps);
    expect(await cmd.execute('gone.com', { confirm: 'gone.com' })).toBe(1);
  });

  it('JSON error shape on missing --confirm', async () => {
    const deps = makeDeps();
    const cmd = new DomainRemoveCommand(deps);
    await cmd.execute('api.example.com', { json: true });
    const json = JSON.parse(getOutput(deps)[0]);
    expect(json.exit_code).toBe(1);
    expect(json.code).toBe('CONFIRMATION_REQUIRED');
  });
});

// ============================================================
// No internal terms (Rule 3)
// ============================================================

describe('Rule 3: No internal terms in domain commands', () => {
  const FORBIDDEN_TERMS = [
    'ArgoCD', 'argocd', 'GitOps', 'gitops-push', 'CRD', 'tenant_id',
    'advisory lock', 'pg_advisory', 'Gitea', 'MinIO', 'kubebuilder', 'envtest', 'Kaniko',
  ];

  it('domain add output contains no internal terms', async () => {
    const deps = makeDeps();
    const cmd = new DomainAddCommand(deps);
    await cmd.execute('api.example.com', { project: 'p', app: 'a', env: 'e' });
    const allOutput = getOutput(deps).join('\n');
    for (const term of FORBIDDEN_TERMS) {
      expect(allOutput).not.toContain(term);
    }
  });

  it('domain verify output contains no internal terms', async () => {
    const deps = makeDeps();
    const cmd = new DomainVerifyCommand(deps);
    await cmd.execute('api.example.com');
    const allOutput = [...getOutput(deps), ...getErrors(deps)].join('\n');
    for (const term of FORBIDDEN_TERMS) {
      expect(allOutput).not.toContain(term);
    }
  });
});
