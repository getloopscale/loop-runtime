// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ProjectInspectCommand, ProjectInspectDeps } from '../../../src/commands/project-inspect';

describe('ProjectInspectCommand', () => {
  let output: string[];
  let errors: string[];

  function makeDeps(overrides: Partial<ProjectInspectDeps> = {}): ProjectInspectDeps {
    return {
      getToken: () => 'test-token',
      getProject: async () => ({
        id: 'p-1',
        name: 'my-api',
        team_id: 'tn-loopzero',
        created_at: '2026-01-15T00:00:00Z',
        created_by: 'github|12345',
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
      const cmd = new ProjectInspectCommand(makeDeps());
      const code = await cmd.execute('my-api');
      expect(code).toBe(0);
    });

    it('returns 4 when not authenticated', async () => {
      const cmd = new ProjectInspectCommand(makeDeps({ getToken: () => null }));
      const code = await cmd.execute('my-api');
      expect(code).toBe(4);
    });

    it('returns 1 when name is missing', async () => {
      const cmd = new ProjectInspectCommand(makeDeps());
      const code = await cmd.execute('');
      expect(code).toBe(1);
    });

    it('returns 1 when project not found', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new ProjectInspectCommand(makeDeps({
        getProject: async () => { throw new ApiError('Not found', 1, 404, 'NOT_FOUND'); },
      }));
      const code = await cmd.execute('nonexistent');
      expect(code).toBe(1);
    });

    it('returns 4 on 401 from API', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new ProjectInspectCommand(makeDeps({
        getProject: async () => { throw new ApiError('Unauthorized', 4, 401); },
      }));
      const code = await cmd.execute('my-api');
      expect(code).toBe(4);
    });

    it('returns 1 on generic API error', async () => {
      const cmd = new ProjectInspectCommand(makeDeps({
        getProject: async () => { throw new Error('connection refused'); },
      }));
      const code = await cmd.execute('my-api');
      expect(code).toBe(1);
    });
  });

  describe('human output', () => {
    it('shows project details', async () => {
      const cmd = new ProjectInspectCommand(makeDeps());
      await cmd.execute('my-api');
      const allOutput = output.join('\n');
      expect(allOutput).toContain('Project: my-api');
      expect(allOutput).toContain('Team:');
      expect(allOutput).toContain('tn-loopzero');
      expect(allOutput).toContain('Created:');
      expect(allOutput).toContain('Created by:');
      expect(allOutput).toContain('github|12345');
    });

    it('shows manage hint', async () => {
      const cmd = new ProjectInspectCommand(makeDeps());
      await cmd.execute('my-api');
      const allOutput = output.join('\n');
      expect(allOutput).toContain('loopzero project list');
      expect(allOutput).toContain('loopzero project delete my-api');
    });

    it('omits created_by when not present', async () => {
      const cmd = new ProjectInspectCommand(makeDeps({
        getProject: async () => ({
          id: 'p-1',
          name: 'my-api',
          team_id: 'tn-loopzero',
          created_at: '2026-01-15T00:00:00Z',
        }),
      }));
      await cmd.execute('my-api');
      const allOutput = output.join('\n');
      expect(allOutput).not.toContain('Created by:');
    });

    it('shows not found error with project name', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new ProjectInspectCommand(makeDeps({
        getProject: async () => { throw new ApiError('Not found', 1, 404); },
      }));
      await cmd.execute('missing-project');
      expect(errors[0]).toContain('missing-project');
      expect(errors[0]).toContain('not found');
    });

    it('shows error message on auth failure', async () => {
      const cmd = new ProjectInspectCommand(makeDeps({ getToken: () => null }));
      await cmd.execute('my-api');
      expect(errors[0]).toContain('Not authenticated');
      expect(errors[0]).toContain('loopzero login');
    });
  });

  describe('--json output', () => {
    it('outputs full project detail as JSON', async () => {
      const cmd = new ProjectInspectCommand(makeDeps());
      const code = await cmd.execute('my-api', { json: true });
      expect(code).toBe(0);
      const parsed = JSON.parse(output[0]);
      expect(parsed.name).toBe('my-api');
      expect(parsed.team_id).toBe('tn-loopzero');
      expect(parsed.created_at).toBe('2026-01-15T00:00:00Z');
      expect(parsed.created_by).toBe('github|12345');
    });

    it('outputs JSON on error', async () => {
      const cmd = new ProjectInspectCommand(makeDeps({ getToken: () => null }));
      const code = await cmd.execute('my-api', { json: true });
      expect(code).toBe(4);
      const parsed = JSON.parse(output[0]);
      expect(parsed.error).toBeDefined();
      expect(parsed.exit_code).toBe(4);
      expect(parsed.code).toBe('AUTH_REQUIRED');
    });

    it('outputs JSON on not found', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new ProjectInspectCommand(makeDeps({
        getProject: async () => { throw new ApiError('Not found', 1, 404); },
      }));
      const code = await cmd.execute('missing', { json: true });
      expect(code).toBe(1);
      const parsed = JSON.parse(output[0]);
      expect(parsed.error).toContain('not found');
      expect(parsed.code).toBe('PROJECT_NOT_FOUND');
    });
  });

  describe('no internal terms', () => {
    const FORBIDDEN_TERMS = [
      'ArgoCD', 'argocd', 'GitOps', 'gitops-push', 'CRD', 'tenant_id',
      'advisory lock', 'pg_advisory', 'Gitea', 'MinIO', 'kubebuilder',
      'envtest', 'Kaniko',
    ];

    it('success output contains no internal terms', async () => {
      const cmd = new ProjectInspectCommand(makeDeps());
      await cmd.execute('my-api');
      const allOutput = [...output, ...errors].join(' ');
      for (const term of FORBIDDEN_TERMS) {
        expect(allOutput).not.toContain(term);
      }
    });

    it('error output contains no internal terms', async () => {
      const cmd = new ProjectInspectCommand(makeDeps({ getToken: () => null }));
      await cmd.execute('my-api');
      const allOutput = [...output, ...errors].join(' ');
      for (const term of FORBIDDEN_TERMS) {
        expect(allOutput).not.toContain(term);
      }
    });
  });
});
