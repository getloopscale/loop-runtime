// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ProjectListCommand, ProjectListDeps } from '../../../src/commands/project-list';

describe('ProjectListCommand', () => {
  let output: string[];
  let errors: string[];

  function makeDeps(overrides: Partial<ProjectListDeps> = {}): ProjectListDeps {
    return {
      getToken: () => 'test-token',
      listProjects: async () => ({
        projects: [
          { id: 'p-1', name: 'my-api', team_id: 'tn-loopzero', created_at: '2026-01-15T00:00:00Z' },
          { id: 'p-2', name: 'my-web', team_id: 'tn-loopzero', created_at: '2026-02-20T00:00:00Z' },
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
      const cmd = new ProjectListCommand(makeDeps());
      const code = await cmd.execute();
      expect(code).toBe(0);
    });

    it('returns 0 when no projects found', async () => {
      const cmd = new ProjectListCommand(makeDeps({
        listProjects: async () => ({ projects: [] }),
      }));
      const code = await cmd.execute();
      expect(code).toBe(0);
    });

    it('returns 4 when not authenticated', async () => {
      const cmd = new ProjectListCommand(makeDeps({ getToken: () => null }));
      const code = await cmd.execute();
      expect(code).toBe(4);
    });

    it('returns 1 on API failure', async () => {
      const cmd = new ProjectListCommand(makeDeps({
        listProjects: async () => { throw new Error('connection refused'); },
      }));
      const code = await cmd.execute();
      expect(code).toBe(1);
    });

    it('returns 4 on 401 from API', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new ProjectListCommand(makeDeps({
        listProjects: async () => { throw new ApiError('Unauthorized', 4, 401); },
      }));
      const code = await cmd.execute();
      expect(code).toBe(4);
    });

    it('returns 1 on non-auth API error', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new ProjectListCommand(makeDeps({
        listProjects: async () => { throw new ApiError('Server error', 1, 500, 'SERVER_ERROR'); },
      }));
      const code = await cmd.execute();
      expect(code).toBe(1);
    });
  });

  describe('human output', () => {
    it('shows project list with names and dates', async () => {
      const cmd = new ProjectListCommand(makeDeps());
      await cmd.execute();
      const allOutput = output.join('\n');
      expect(allOutput).toContain('Projects:');
      expect(allOutput).toContain('my-api');
      expect(allOutput).toContain('my-web');
      expect(allOutput).toContain('2 projects');
    });

    it('shows singular "project" for single result', async () => {
      const cmd = new ProjectListCommand(makeDeps({
        listProjects: async () => ({
          projects: [{ id: 'p-1', name: 'only-one', team_id: 'tn-loopzero', created_at: '2026-01-01T00:00:00Z' }],
        }),
      }));
      await cmd.execute();
      const allOutput = output.join('\n');
      expect(allOutput).toContain('1 project');
      expect(allOutput).not.toContain('1 projects');
    });

    it('shows "No projects found." when empty', async () => {
      const cmd = new ProjectListCommand(makeDeps({
        listProjects: async () => ({ projects: [] }),
      }));
      await cmd.execute();
      expect(output[0]).toBe('No projects found.');
    });

    it('shows error message on auth failure', async () => {
      const cmd = new ProjectListCommand(makeDeps({ getToken: () => null }));
      await cmd.execute();
      expect(errors[0]).toContain('Not authenticated');
      expect(errors[0]).toContain('loopzero login');
    });
  });

  describe('--json output', () => {
    it('outputs JSON with projects array and total', async () => {
      const cmd = new ProjectListCommand(makeDeps());
      const code = await cmd.execute({ json: true });
      expect(code).toBe(0);
      const parsed = JSON.parse(output[0]);
      expect(parsed.projects).toHaveLength(2);
      expect(parsed.total).toBe(2);
      expect(parsed.projects[0].name).toBe('my-api');
      expect(parsed.projects[1].name).toBe('my-web');
    });

    it('outputs JSON on error', async () => {
      const cmd = new ProjectListCommand(makeDeps({ getToken: () => null }));
      const code = await cmd.execute({ json: true });
      expect(code).toBe(4);
      const parsed = JSON.parse(output[0]);
      expect(parsed.error).toBeDefined();
      expect(parsed.exit_code).toBe(4);
      expect(parsed.code).toBe('AUTH_REQUIRED');
    });

    it('outputs JSON with empty array when no projects', async () => {
      const cmd = new ProjectListCommand(makeDeps({
        listProjects: async () => ({ projects: [] }),
      }));
      const code = await cmd.execute({ json: true });
      expect(code).toBe(0);
      const parsed = JSON.parse(output[0]);
      expect(parsed.projects).toEqual([]);
      expect(parsed.total).toBe(0);
    });
  });

  describe('no internal terms', () => {
    const FORBIDDEN_TERMS = [
      'ArgoCD', 'argocd', 'GitOps', 'gitops-push', 'CRD', 'tenant_id',
      'advisory lock', 'pg_advisory', 'Gitea', 'MinIO', 'kubebuilder',
      'envtest', 'Kaniko',
    ];

    it('success output contains no internal terms', async () => {
      const cmd = new ProjectListCommand(makeDeps());
      await cmd.execute();
      const allOutput = [...output, ...errors].join(' ');
      for (const term of FORBIDDEN_TERMS) {
        expect(allOutput).not.toContain(term);
      }
    });

    it('error output contains no internal terms', async () => {
      const cmd = new ProjectListCommand(makeDeps({ getToken: () => null }));
      await cmd.execute();
      const allOutput = [...output, ...errors].join(' ');
      for (const term of FORBIDDEN_TERMS) {
        expect(allOutput).not.toContain(term);
      }
    });
  });
});
