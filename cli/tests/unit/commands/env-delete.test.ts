// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { EnvDeleteCommand, EnvDeleteDeps } from '../../../src/commands/env-delete';

describe('EnvDeleteCommand', () => {
  let output: string[];
  let errors: string[];

  function makeDeps(overrides: Partial<EnvDeleteDeps> = {}): EnvDeleteDeps {
    return {
      getToken: () => 'test-token',
      deleteEnvironment: async () => {},
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
    it('returns 0 on successful delete', async () => {
      const cmd = new EnvDeleteCommand(makeDeps());
      const code = await cmd.execute('staging', { confirm: 'staging' });
      expect(code).toBe(0);
    });

    it('returns 4 when not authenticated', async () => {
      const cmd = new EnvDeleteCommand(makeDeps({ getToken: () => null }));
      const code = await cmd.execute('staging', { confirm: 'staging' });
      expect(code).toBe(4);
    });

    it('returns 1 when name is missing', async () => {
      const cmd = new EnvDeleteCommand(makeDeps());
      const code = await cmd.execute('', { confirm: '' });
      expect(code).toBe(1);
    });

    it('returns 1 when trying to delete production', async () => {
      const cmd = new EnvDeleteCommand(makeDeps());
      const code = await cmd.execute('production', { confirm: 'production' });
      expect(code).toBe(1);
    });

    it('returns 1 when --confirm is not provided', async () => {
      const cmd = new EnvDeleteCommand(makeDeps());
      const code = await cmd.execute('staging');
      expect(code).toBe(1);
    });

    it('returns 1 when --confirm does not match name', async () => {
      const cmd = new EnvDeleteCommand(makeDeps());
      const code = await cmd.execute('staging', { confirm: 'wrong' });
      expect(code).toBe(1);
    });

    it('returns 3 on delete failure', async () => {
      const cmd = new EnvDeleteCommand(makeDeps({
        deleteEnvironment: async () => { throw new Error('server error'); },
      }));
      const code = await cmd.execute('staging', { confirm: 'staging' });
      expect(code).toBe(3);
    });

    it('returns 4 on 401 from API', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new EnvDeleteCommand(makeDeps({
        deleteEnvironment: async () => { throw new ApiError('Unauthorized', 4, 401); },
      }));
      const code = await cmd.execute('staging', { confirm: 'staging' });
      expect(code).toBe(4);
    });

    it('returns 1 on 404 from API', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new EnvDeleteCommand(makeDeps({
        deleteEnvironment: async () => { throw new ApiError('Not found', 1, 404, 'NOT_FOUND'); },
      }));
      const code = await cmd.execute('staging', { confirm: 'staging' });
      expect(code).toBe(1);
    });
  });

  describe('production guard', () => {
    it('refuses to delete production with clear message', async () => {
      const cmd = new EnvDeleteCommand(makeDeps());
      await cmd.execute('production', { confirm: 'production' });
      expect(errors[0]).toContain('Cannot delete the production environment');
    });

    it('refuses production even without confirm flag', async () => {
      const cmd = new EnvDeleteCommand(makeDeps());
      const code = await cmd.execute('production');
      expect(code).toBe(1);
      expect(errors[0]).toContain('Cannot delete the production environment');
    });
  });

  describe('confirmation', () => {
    it('requires --confirm flag', async () => {
      const cmd = new EnvDeleteCommand(makeDeps());
      await cmd.execute('staging');
      expect(errors[0]).toContain('Confirmation required');
      expect(errors[0]).toContain('--confirm staging');
    });

    it('requires --confirm to match the environment name', async () => {
      const cmd = new EnvDeleteCommand(makeDeps());
      await cmd.execute('staging', { confirm: 'other' });
      expect(errors[0]).toContain('does not match');
    });
  });

  describe('human output', () => {
    it('shows success message with deployment removal note', async () => {
      const cmd = new EnvDeleteCommand(makeDeps());
      await cmd.execute('staging', { confirm: 'staging' });
      expect(output[0]).toBe('Environment staging deleted. All deployments in staging have been removed.');
    });

    it('shows error message on auth failure', async () => {
      const cmd = new EnvDeleteCommand(makeDeps({ getToken: () => null }));
      await cmd.execute('staging', { confirm: 'staging' });
      expect(errors[0]).toContain('Not authenticated');
      expect(errors[0]).toContain('loopzero login');
    });

    it('shows actionable error on delete failure', async () => {
      const cmd = new EnvDeleteCommand(makeDeps({
        deleteEnvironment: async () => { throw new Error('timeout'); },
      }));
      await cmd.execute('staging', { confirm: 'staging' });
      expect(errors[0]).toContain('Try again');
    });
  });

  describe('--json output', () => {
    it('outputs JSON on success', async () => {
      const cmd = new EnvDeleteCommand(makeDeps());
      const code = await cmd.execute('staging', { confirm: 'staging', json: true });
      expect(code).toBe(0);
      const parsed = JSON.parse(output[0]);
      expect(parsed.name).toBe('staging');
      expect(parsed.status).toBe('deleted');
    });

    it('outputs JSON on production guard error', async () => {
      const cmd = new EnvDeleteCommand(makeDeps());
      const code = await cmd.execute('production', { confirm: 'production', json: true });
      expect(code).toBe(1);
      const parsed = JSON.parse(output[0]);
      expect(parsed.code).toBe('PRODUCTION_PROTECTED');
    });

    it('outputs JSON on confirmation mismatch', async () => {
      const cmd = new EnvDeleteCommand(makeDeps());
      const code = await cmd.execute('staging', { confirm: 'wrong', json: true });
      expect(code).toBe(1);
      const parsed = JSON.parse(output[0]);
      expect(parsed.code).toBe('CONFIRMATION_MISMATCH');
    });

    it('outputs JSON on auth error', async () => {
      const cmd = new EnvDeleteCommand(makeDeps({ getToken: () => null }));
      const code = await cmd.execute('staging', { confirm: 'staging', json: true });
      expect(code).toBe(4);
      const parsed = JSON.parse(output[0]);
      expect(parsed.error).toBeDefined();
      expect(parsed.exit_code).toBe(4);
      expect(parsed.code).toBe('AUTH_REQUIRED');
    });
  });

  describe('no internal terms', () => {
    const FORBIDDEN_TERMS = [
      'ArgoCD', 'argocd', 'GitOps', 'gitops-push', 'CRD', 'tenant_id',
      'advisory lock', 'pg_advisory', 'Gitea', 'MinIO', 'kubebuilder',
      'envtest', 'Kaniko', 'namespace',
    ];

    it('success output contains no internal terms', async () => {
      const cmd = new EnvDeleteCommand(makeDeps());
      await cmd.execute('staging', { confirm: 'staging' });
      const allOutput = [...output, ...errors].join(' ');
      for (const term of FORBIDDEN_TERMS) {
        expect(allOutput).not.toContain(term);
      }
    });

    it('error output contains no internal terms', async () => {
      const cmd = new EnvDeleteCommand(makeDeps({ getToken: () => null }));
      await cmd.execute('staging', { confirm: 'staging' });
      const allOutput = [...output, ...errors].join(' ');
      for (const term of FORBIDDEN_TERMS) {
        expect(allOutput).not.toContain(term);
      }
    });
  });
});
