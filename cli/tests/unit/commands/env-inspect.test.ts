// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { EnvInspectCommand, EnvInspectDeps } from '../../../src/commands/env-inspect';

describe('EnvInspectCommand', () => {
  let output: string[];
  let errors: string[];

  function makeDeps(overrides: Partial<EnvInspectDeps> = {}): EnvInspectDeps {
    return {
      getToken: () => 'test-token',
      getEnvironment: async (name: string) => ({
        name,
        status: 'active',
        projects: [
          { name: 'my-api', release_id: 'rel-abc123', url: 'https://my-api-staging.dev.loopzero.example' },
          { name: 'my-web', release_id: 'rel-def456', url: 'https://my-web-staging.dev.loopzero.example' },
        ],
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
    it('returns 0 on successful inspect', async () => {
      const cmd = new EnvInspectCommand(makeDeps());
      const code = await cmd.execute('staging');
      expect(code).toBe(0);
    });

    it('returns 4 when not authenticated', async () => {
      const cmd = new EnvInspectCommand(makeDeps({ getToken: () => null }));
      const code = await cmd.execute('staging');
      expect(code).toBe(4);
    });

    it('returns 1 when name is missing', async () => {
      const cmd = new EnvInspectCommand(makeDeps());
      const code = await cmd.execute('');
      expect(code).toBe(1);
    });

    it('returns 1 on 404 not found', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new EnvInspectCommand(makeDeps({
        getEnvironment: async () => { throw new ApiError('Not found', 1, 404, 'NOT_FOUND'); },
      }));
      const code = await cmd.execute('nonexistent');
      expect(code).toBe(1);
    });

    it('returns 4 on 401 from API', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new EnvInspectCommand(makeDeps({
        getEnvironment: async () => { throw new ApiError('Unauthorized', 4, 401); },
      }));
      const code = await cmd.execute('staging');
      expect(code).toBe(4);
    });

    it('returns 1 on generic API error', async () => {
      const cmd = new EnvInspectCommand(makeDeps({
        getEnvironment: async () => { throw new Error('connection refused'); },
      }));
      const code = await cmd.execute('staging');
      expect(code).toBe(1);
    });
  });

  describe('human output', () => {
    it('shows environment name and status', async () => {
      const cmd = new EnvInspectCommand(makeDeps());
      await cmd.execute('staging');
      const allOutput = output.join('\n');
      expect(allOutput).toContain('Environment: staging');
      expect(allOutput).toContain('Status:   active');
    });

    it('shows projects with release IDs and URLs', async () => {
      const cmd = new EnvInspectCommand(makeDeps());
      await cmd.execute('staging');
      const allOutput = output.join('\n');
      expect(allOutput).toContain('my-api');
      expect(allOutput).toContain('rel-abc123');
      expect(allOutput).toContain('https://my-api-staging.dev.loopzero.example');
      expect(allOutput).toContain('my-web');
      expect(allOutput).toContain('rel-def456');
    });

    it('shows "none" when no projects in environment', async () => {
      const cmd = new EnvInspectCommand(makeDeps({
        getEnvironment: async (name: string) => ({
          name,
          status: 'active',
          projects: [],
        }),
      }));
      await cmd.execute('staging');
      const allOutput = output.join('\n');
      expect(allOutput).toContain('Projects: none');
    });

    // CHG-2026-04-23-AUDIT-1: the API can return {name, status} without a
    // `projects` field entirely when no releases exist — not just empty array.
    // Previously the CLI crashed with "Cannot read properties of undefined
    // (reading 'length')" and exposed a JS runtime error to the user.
    it('shows "none" when API response omits projects field entirely', async () => {
      const cmd = new EnvInspectCommand(makeDeps({
        getEnvironment: async (name: string) => ({
          name,
          status: 'active',
        } as unknown as { name: string; status: string; projects: never[] }),
      }));
      const exit = await cmd.execute('staging');
      expect(exit).toBe(0);
      const allOutput = output.join('\n');
      expect(allOutput).toContain('Projects: none');
      expect(errors.join('\n')).not.toContain('Cannot read properties');
    });

    it('shows error message on auth failure', async () => {
      const cmd = new EnvInspectCommand(makeDeps({ getToken: () => null }));
      await cmd.execute('staging');
      expect(errors[0]).toContain('Not authenticated');
      expect(errors[0]).toContain('loopzero login');
    });

    it('shows not found message on 404', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new EnvInspectCommand(makeDeps({
        getEnvironment: async () => { throw new ApiError('Not found', 1, 404); },
      }));
      await cmd.execute('staging');
      expect(errors[0]).toContain('not found');
    });
  });

  describe('--json output', () => {
    it('outputs full environment detail as JSON', async () => {
      const cmd = new EnvInspectCommand(makeDeps());
      const code = await cmd.execute('staging', { json: true });
      expect(code).toBe(0);
      const parsed = JSON.parse(output[0]);
      expect(parsed.name).toBe('staging');
      expect(parsed.status).toBe('active');
      expect(parsed.projects).toHaveLength(2);
      expect(parsed.projects[0].name).toBe('my-api');
      expect(parsed.projects[0].release_id).toBe('rel-abc123');
      expect(parsed.projects[0].url).toBe('https://my-api-staging.dev.loopzero.example');
    });

    it('outputs JSON on error', async () => {
      const cmd = new EnvInspectCommand(makeDeps({ getToken: () => null }));
      const code = await cmd.execute('staging', { json: true });
      expect(code).toBe(4);
      const parsed = JSON.parse(output[0]);
      expect(parsed.error).toBeDefined();
      expect(parsed.exit_code).toBe(4);
      expect(parsed.code).toBe('AUTH_REQUIRED');
    });

    it('outputs JSON with ENV_NOT_FOUND on 404', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new EnvInspectCommand(makeDeps({
        getEnvironment: async () => { throw new ApiError('Not found', 1, 404); },
      }));
      const code = await cmd.execute('staging', { json: true });
      expect(code).toBe(1);
      const parsed = JSON.parse(output[0]);
      expect(parsed.code).toBe('ENV_NOT_FOUND');
    });
  });

  describe('no internal terms', () => {
    const FORBIDDEN_TERMS = [
      'ArgoCD', 'argocd', 'GitOps', 'gitops-push', 'CRD', 'tenant_id',
      'advisory lock', 'pg_advisory', 'Gitea', 'MinIO', 'kubebuilder',
      'envtest', 'Kaniko', 'namespace',
    ];

    it('success output contains no internal terms', async () => {
      const cmd = new EnvInspectCommand(makeDeps());
      await cmd.execute('staging');
      const allOutput = [...output, ...errors].join(' ');
      for (const term of FORBIDDEN_TERMS) {
        expect(allOutput).not.toContain(term);
      }
    });

    it('error output contains no internal terms', async () => {
      const cmd = new EnvInspectCommand(makeDeps({ getToken: () => null }));
      await cmd.execute('staging');
      const allOutput = [...output, ...errors].join(' ');
      for (const term of FORBIDDEN_TERMS) {
        expect(allOutput).not.toContain(term);
      }
    });
  });
});
