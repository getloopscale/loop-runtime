// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ProjectCreateCommand, ProjectCreateDeps } from '../../../src/commands/project-create';

describe('ProjectCreateCommand', () => {
  let output: string[];
  let errors: string[];

  function makeDeps(overrides: Partial<ProjectCreateDeps> = {}): ProjectCreateDeps {
    return {
      getToken: () => 'test-token',
      createProject: async (name: string) => ({
        id: 'p-1',
        name,
        team_id: 'tn-loopzero',
        created_at: '2026-03-01T00:00:00Z',
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
      const cmd = new ProjectCreateCommand(makeDeps());
      const code = await cmd.execute('my-new-api');
      expect(code).toBe(0);
    });

    it('returns 0 on 409 conflict (idempotent)', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new ProjectCreateCommand(makeDeps({
        createProject: async () => { throw new ApiError('Conflict', 1, 409, 'CONFLICT'); },
      }));
      const code = await cmd.execute('my-new-api');
      expect(code).toBe(0);
    });

    it('returns 4 when not authenticated', async () => {
      const cmd = new ProjectCreateCommand(makeDeps({ getToken: () => null }));
      const code = await cmd.execute('my-new-api');
      expect(code).toBe(4);
    });

    it('returns 1 when name is missing', async () => {
      const cmd = new ProjectCreateCommand(makeDeps());
      const code = await cmd.execute('');
      expect(code).toBe(1);
    });

    it('returns 4 on 401 from API', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new ProjectCreateCommand(makeDeps({
        createProject: async () => { throw new ApiError('Unauthorized', 4, 401); },
      }));
      const code = await cmd.execute('my-new-api');
      expect(code).toBe(4);
    });

    it('returns 1 on generic API error', async () => {
      const cmd = new ProjectCreateCommand(makeDeps({
        createProject: async () => { throw new Error('connection refused'); },
      }));
      const code = await cmd.execute('my-new-api');
      expect(code).toBe(1);
    });
  });

  describe('human output', () => {
    it('shows success message with project name', async () => {
      const cmd = new ProjectCreateCommand(makeDeps());
      await cmd.execute('my-new-api');
      expect(output[0]).toBe('Project my-new-api created.');
    });

    it('shows already exists message on 409', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new ProjectCreateCommand(makeDeps({
        createProject: async () => { throw new ApiError('Conflict', 1, 409); },
      }));
      await cmd.execute('my-new-api');
      expect(output[0]).toBe('Project my-new-api already exists.');
    });

    it('shows error message on auth failure', async () => {
      const cmd = new ProjectCreateCommand(makeDeps({ getToken: () => null }));
      await cmd.execute('my-new-api');
      expect(errors[0]).toContain('Not authenticated');
      expect(errors[0]).toContain('loopzero login');
    });

    it('shows actionable error on API failure', async () => {
      const cmd = new ProjectCreateCommand(makeDeps({
        createProject: async () => { throw new Error('timeout'); },
      }));
      await cmd.execute('my-new-api');
      expect(errors[0]).toContain('Try again');
    });
  });

  describe('--json output', () => {
    it('outputs JSON on success with status created', async () => {
      const cmd = new ProjectCreateCommand(makeDeps());
      const code = await cmd.execute('my-new-api', { json: true });
      expect(code).toBe(0);
      const parsed = JSON.parse(output[0]);
      expect(parsed.name).toBe('my-new-api');
      expect(parsed.status).toBe('created');
      expect(parsed.team_id).toBe('tn-loopzero');
    });

    it('outputs JSON on 409 with status already_exists', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new ProjectCreateCommand(makeDeps({
        createProject: async () => { throw new ApiError('Conflict', 1, 409); },
      }));
      const code = await cmd.execute('my-new-api', { json: true });
      expect(code).toBe(0);
      const parsed = JSON.parse(output[0]);
      expect(parsed.name).toBe('my-new-api');
      expect(parsed.status).toBe('already_exists');
    });

    it('outputs JSON on error', async () => {
      const cmd = new ProjectCreateCommand(makeDeps({ getToken: () => null }));
      const code = await cmd.execute('my-new-api', { json: true });
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
      'envtest', 'Kaniko',
    ];

    it('success output contains no internal terms', async () => {
      const cmd = new ProjectCreateCommand(makeDeps());
      await cmd.execute('my-new-api');
      const allOutput = [...output, ...errors].join(' ');
      for (const term of FORBIDDEN_TERMS) {
        expect(allOutput).not.toContain(term);
      }
    });

    it('error output contains no internal terms', async () => {
      const cmd = new ProjectCreateCommand(makeDeps({ getToken: () => null }));
      await cmd.execute('my-new-api');
      const allOutput = [...output, ...errors].join(' ');
      for (const term of FORBIDDEN_TERMS) {
        expect(allOutput).not.toContain(term);
      }
    });
  });
});
