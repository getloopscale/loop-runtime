// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { EnvListCommand, EnvListDeps } from '../../../src/commands/env-list';

describe('EnvListCommand', () => {
  let output: string[];
  let errors: string[];

  function makeDeps(overrides: Partial<EnvListDeps> = {}): EnvListDeps {
    return {
      getToken: () => 'test-token',
      listEnvironments: async () => ({
        environments: [
          { name: 'production', status: 'active' },
          { name: 'staging', status: 'active' },
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
    it('returns 0 on successful list', async () => {
      const cmd = new EnvListCommand(makeDeps());
      const code = await cmd.execute();
      expect(code).toBe(0);
    });

    it('returns 0 when no environments found', async () => {
      const cmd = new EnvListCommand(makeDeps({
        listEnvironments: async () => ({ environments: [] }),
      }));
      const code = await cmd.execute();
      expect(code).toBe(0);
    });

    it('returns 4 when not authenticated', async () => {
      const cmd = new EnvListCommand(makeDeps({ getToken: () => null }));
      const code = await cmd.execute();
      expect(code).toBe(4);
    });

    it('returns 1 on API failure', async () => {
      const cmd = new EnvListCommand(makeDeps({
        listEnvironments: async () => { throw new Error('connection refused'); },
      }));
      const code = await cmd.execute();
      expect(code).toBe(1);
    });

    it('returns 4 on 401 from API', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new EnvListCommand(makeDeps({
        listEnvironments: async () => { throw new ApiError('Unauthorized', 4, 401); },
      }));
      const code = await cmd.execute();
      expect(code).toBe(4);
    });
  });

  describe('human output', () => {
    it('shows environment list with names and statuses', async () => {
      const cmd = new EnvListCommand(makeDeps());
      await cmd.execute();
      const allOutput = output.join('\n');
      expect(allOutput).toContain('Environments:');
      expect(allOutput).toContain('production');
      expect(allOutput).toContain('staging');
      expect(allOutput).toContain('active');
      expect(allOutput).toContain('2 environments');
    });

    it('shows singular "environment" for single result', async () => {
      const cmd = new EnvListCommand(makeDeps({
        listEnvironments: async () => ({
          environments: [{ name: 'staging', status: 'active' }],
        }),
      }));
      await cmd.execute();
      const allOutput = output.join('\n');
      expect(allOutput).toContain('1 environment');
      expect(allOutput).not.toContain('1 environments');
    });

    it('shows "No environments found." when empty', async () => {
      const cmd = new EnvListCommand(makeDeps({
        listEnvironments: async () => ({ environments: [] }),
      }));
      await cmd.execute();
      expect(output[0]).toBe('No environments found.');
    });

    it('shows error message on auth failure', async () => {
      const cmd = new EnvListCommand(makeDeps({ getToken: () => null }));
      await cmd.execute();
      expect(errors[0]).toContain('Not authenticated');
      expect(errors[0]).toContain('loopzero login');
    });
  });

  describe('--json output', () => {
    it('outputs JSON with environments array and total', async () => {
      const cmd = new EnvListCommand(makeDeps());
      const code = await cmd.execute({ json: true });
      expect(code).toBe(0);
      const parsed = JSON.parse(output[0]);
      expect(parsed.environments).toHaveLength(2);
      expect(parsed.total).toBe(2);
      expect(parsed.environments[0].name).toBe('production');
      expect(parsed.environments[1].name).toBe('staging');
    });

    it('outputs JSON on error', async () => {
      const cmd = new EnvListCommand(makeDeps({ getToken: () => null }));
      const code = await cmd.execute({ json: true });
      expect(code).toBe(4);
      const parsed = JSON.parse(output[0]);
      expect(parsed.error).toBeDefined();
      expect(parsed.exit_code).toBe(4);
      expect(parsed.code).toBe('AUTH_REQUIRED');
    });

    it('outputs JSON with empty array when no environments', async () => {
      const cmd = new EnvListCommand(makeDeps({
        listEnvironments: async () => ({ environments: [] }),
      }));
      const code = await cmd.execute({ json: true });
      expect(code).toBe(0);
      const parsed = JSON.parse(output[0]);
      expect(parsed.environments).toEqual([]);
      expect(parsed.total).toBe(0);
    });
  });

  describe('no internal terms', () => {
    const FORBIDDEN_TERMS = [
      'ArgoCD', 'argocd', 'GitOps', 'gitops-push', 'CRD', 'tenant_id',
      'advisory lock', 'pg_advisory', 'Gitea', 'MinIO', 'kubebuilder',
      'envtest', 'Kaniko', 'namespace',
    ];

    it('success output contains no internal terms', async () => {
      const cmd = new EnvListCommand(makeDeps());
      await cmd.execute();
      const allOutput = [...output, ...errors].join(' ');
      for (const term of FORBIDDEN_TERMS) {
        expect(allOutput).not.toContain(term);
      }
    });

    it('error output contains no internal terms', async () => {
      const cmd = new EnvListCommand(makeDeps({ getToken: () => null }));
      await cmd.execute();
      const allOutput = [...output, ...errors].join(' ');
      for (const term of FORBIDDEN_TERMS) {
        expect(allOutput).not.toContain(term);
      }
    });
  });
});
