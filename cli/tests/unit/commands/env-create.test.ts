// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { EnvCreateCommand, EnvCreateDeps } from '../../../src/commands/env-create';

describe('EnvCreateCommand', () => {
  let output: string[];
  let errors: string[];

  function makeDeps(overrides: Partial<EnvCreateDeps> = {}): EnvCreateDeps {
    return {
      getToken: () => 'test-token',
      createEnvironment: async (name: string, _project: string) => ({
        name,
        status: 'active',
      }),
      writeTo: (msg: string) => output.push(msg),
      writeError: (msg: string) => errors.push(msg),
      ...overrides,
    };
  }

  beforeEach(() => {
    output = [];
    errors = [];
  });

  describe('exit codes', () => {
    it('returns 0 on successful create', async () => {
      const cmd = new EnvCreateCommand(makeDeps());
      const code = await cmd.execute('staging', { project: 'web' });
      expect(code).toBe(0);
    });

    it('returns 0 on 409 conflict (idempotent)', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new EnvCreateCommand(makeDeps({
        createEnvironment: async () => { throw new ApiError('Conflict', 1, 409, 'CONFLICT'); },
      }));
      const code = await cmd.execute('staging', { project: 'web' });
      expect(code).toBe(0);
    });

    it('returns 4 when not authenticated', async () => {
      const cmd = new EnvCreateCommand(makeDeps({ getToken: () => null }));
      const code = await cmd.execute('staging', { project: 'web' });
      expect(code).toBe(4);
    });

    it('returns 1 when name is missing', async () => {
      const cmd = new EnvCreateCommand(makeDeps());
      const code = await cmd.execute('');
      expect(code).toBe(1);
    });

    it('returns 1 on invalid name format', async () => {
      const cmd = new EnvCreateCommand(makeDeps());
      const code = await cmd.execute('-staging');
      expect(code).toBe(1);
    });

    it('returns 1 on reserved name', async () => {
      const cmd = new EnvCreateCommand(makeDeps());
      const code = await cmd.execute('default');
      expect(code).toBe(1);
    });

    it('returns 4 on 401 from API', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new EnvCreateCommand(makeDeps({
        createEnvironment: async () => { throw new ApiError('Unauthorized', 4, 401); },
      }));
      const code = await cmd.execute('staging', { project: 'web' });
      expect(code).toBe(4);
    });

    it('returns 1 on generic API error', async () => {
      const cmd = new EnvCreateCommand(makeDeps({
        createEnvironment: async () => { throw new Error('connection refused'); },
      }));
      const code = await cmd.execute('staging', { project: 'web' });
      expect(code).toBe(1);
    });
  });

  describe('name validation', () => {
    it('rejects names starting with a hyphen', async () => {
      const cmd = new EnvCreateCommand(makeDeps());
      const code = await cmd.execute('-bad');
      expect(code).toBe(1);
      expect(errors[0]).toContain('Invalid environment name');
    });

    it('rejects names with uppercase letters', async () => {
      const cmd = new EnvCreateCommand(makeDeps());
      const code = await cmd.execute('Staging');
      expect(code).toBe(1);
      expect(errors[0]).toContain('lowercase');
    });

    it('rejects names with spaces', async () => {
      const cmd = new EnvCreateCommand(makeDeps());
      const code = await cmd.execute('my env');
      expect(code).toBe(1);
    });

    it('accepts valid names with hyphens', async () => {
      const cmd = new EnvCreateCommand(makeDeps());
      const code = await cmd.execute('pre-prod', { project: 'web' });
      expect(code).toBe(0);
    });

    it('accepts valid names with numbers', async () => {
      const cmd = new EnvCreateCommand(makeDeps());
      const code = await cmd.execute('staging2', { project: 'web' });
      expect(code).toBe(0);
    });
  });

  describe('reserved names', () => {
    it('rejects "default"', async () => {
      const cmd = new EnvCreateCommand(makeDeps());
      const code = await cmd.execute('default');
      expect(code).toBe(1);
      expect(errors[0]).toContain('reserved');
    });

    it('rejects "system"', async () => {
      const cmd = new EnvCreateCommand(makeDeps());
      const code = await cmd.execute('system');
      expect(code).toBe(1);
      expect(errors[0]).toContain('reserved');
    });

    it('rejects "internal"', async () => {
      const cmd = new EnvCreateCommand(makeDeps());
      const code = await cmd.execute('internal');
      expect(code).toBe(1);
      expect(errors[0]).toContain('reserved');
    });
  });

  describe('human output', () => {
    it('shows success message with environment name', async () => {
      const cmd = new EnvCreateCommand(makeDeps());
      await cmd.execute('staging', { project: 'web' });
      expect(output[0]).toBe('Environment staging created.');
    });

    it('shows already exists message on 409', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new EnvCreateCommand(makeDeps({
        createEnvironment: async () => { throw new ApiError('Conflict', 1, 409); },
      }));
      await cmd.execute('staging', { project: 'web' });
      expect(output[0]).toBe('Environment staging already exists.');
    });

    it('shows error message on auth failure', async () => {
      const cmd = new EnvCreateCommand(makeDeps({ getToken: () => null }));
      await cmd.execute('staging', { project: 'web' });
      expect(errors[0]).toContain('Not authenticated');
      expect(errors[0]).toContain('loopzero login');
    });

    it('shows actionable error on API failure', async () => {
      const cmd = new EnvCreateCommand(makeDeps({
        createEnvironment: async () => { throw new Error('timeout'); },
      }));
      await cmd.execute('staging', { project: 'web' });
      expect(errors[0]).toContain('Try again');
    });
  });

  describe('--json output', () => {
    it('outputs JSON on success with status created', async () => {
      const cmd = new EnvCreateCommand(makeDeps());
      const code = await cmd.execute('staging', { json: true, project: 'web' });
      expect(code).toBe(0);
      const parsed = JSON.parse(output[0]);
      expect(parsed.name).toBe('staging');
      expect(parsed.status).toBe('created');
    });

    it('outputs JSON on 409 with status already_exists', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new EnvCreateCommand(makeDeps({
        createEnvironment: async () => { throw new ApiError('Conflict', 1, 409); },
      }));
      const code = await cmd.execute('staging', { json: true, project: 'web' });
      expect(code).toBe(0);
      const parsed = JSON.parse(output[0]);
      expect(parsed.name).toBe('staging');
      expect(parsed.status).toBe('already_exists');
    });

    it('outputs JSON on error', async () => {
      const cmd = new EnvCreateCommand(makeDeps({ getToken: () => null }));
      const code = await cmd.execute('staging', { json: true, project: 'web' });
      expect(code).toBe(4);
      const parsed = JSON.parse(output[0]);
      expect(parsed.error).toBeDefined();
      expect(parsed.exit_code).toBe(4);
      expect(parsed.code).toBe('AUTH_REQUIRED');
    });

    it('outputs JSON on reserved name error', async () => {
      const cmd = new EnvCreateCommand(makeDeps());
      const code = await cmd.execute('default', { json: true });
      expect(code).toBe(1);
      const parsed = JSON.parse(output[0]);
      expect(parsed.code).toBe('RESERVED_NAME');
    });
  });

  describe('no internal terms', () => {
    const FORBIDDEN_TERMS = [
      'ArgoCD', 'argocd', 'GitOps', 'gitops-push', 'CRD', 'tenant_id',
      'advisory lock', 'pg_advisory', 'Gitea', 'MinIO', 'kubebuilder',
      'envtest', 'Kaniko', 'namespace',
    ];

    it('success output contains no internal terms', async () => {
      const cmd = new EnvCreateCommand(makeDeps());
      await cmd.execute('staging', { project: 'web' });
      const allOutput = [...output, ...errors].join(' ');
      for (const term of FORBIDDEN_TERMS) {
        expect(allOutput).not.toContain(term);
      }
    });

    it('error output contains no internal terms', async () => {
      const cmd = new EnvCreateCommand(makeDeps({ getToken: () => null }));
      await cmd.execute('staging', { project: 'web' });
      const allOutput = [...output, ...errors].join(' ');
      for (const term of FORBIDDEN_TERMS) {
        expect(allOutput).not.toContain(term);
      }
    });
  });
});

// =============================================================================
// M2 v1.2 — T6.1 --project flag tests
// Plan: 2026-06-07-runtime-envs-per-project-m2 v1.2
// =============================================================================

describe('M2 v1.2 T6.1 — --project flag', () => {
  let output: string[];
  let errors: string[];

  const writeTo = (msg: string) => output.push(msg);
  const writeError = (msg: string) => errors.push(msg);

  const makeDeps = (over?: Partial<EnvCreateDeps>): EnvCreateDeps => ({
    getToken: () => 'tok',
    createEnvironment: async () => ({ name: 'staging', status: 'active' }),
    writeTo,
    writeError,
    ...over,
  });

  beforeEach(() => {
    output = [];
    errors = [];
  });

  it('requires --project; missing → exit 1 with clear message', async () => {
    const cmd = new EnvCreateCommand(makeDeps());
    // Note: no project in options.
    const code = await cmd.execute('staging', { json: true });
    expect(code).toBe(1);
    const parsed = JSON.parse(output[0]);
    expect(parsed.code).toBe('MISSING_FIELD');
    expect(parsed.error).toContain('Project is required');
    expect(parsed.error).toContain('--project');
  });

  it('passes project to createEnvironment closure verbatim', async () => {
    let capturedName: string | null = null;
    let capturedProject: string | null = null;
    const cmd = new EnvCreateCommand(makeDeps({
      createEnvironment: async (name: string, project: string) => {
        capturedName = name;
        capturedProject = project;
        return { name, status: 'active' };
      },
    }));
    const code = await cmd.execute('staging', { project: 'web' });
    expect(code).toBe(0);
    expect(capturedName).toBe('staging');
    expect(capturedProject).toBe('web');
  });

  it('rejects empty --project (whitespace-only) with MISSING_FIELD', async () => {
    const cmd = new EnvCreateCommand(makeDeps());
    const code = await cmd.execute('staging', { project: '   ', json: true });
    expect(code).toBe(1);
    const parsed = JSON.parse(output[0]);
    expect(parsed.code).toBe('MISSING_FIELD');
  });
});
