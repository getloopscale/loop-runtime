// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { TeamAddCommand, TeamAddDeps } from '../../../src/commands/team-add';

describe('TeamAddCommand', () => {
  let output: string[];
  let errors: string[];

  function makeDeps(overrides: Partial<TeamAddDeps> = {}): TeamAddDeps {
    return {
      getToken: () => 'test-token',
      addMember: async (username: string, role: string) => ({ username, role }),
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
    it('returns 0 on successful add', async () => {
      const cmd = new TeamAddCommand(makeDeps());
      const code = await cmd.execute('johndoe');
      expect(code).toBe(0);
    });

    it('returns 0 when user is already a member (idempotent)', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new TeamAddCommand(makeDeps({
        addMember: async () => { throw new ApiError('Already a member', 1, 409, 'ALREADY_MEMBER'); },
      }));
      const code = await cmd.execute('johndoe');
      expect(code).toBe(0);
    });

    it('returns 4 when not authenticated', async () => {
      const cmd = new TeamAddCommand(makeDeps({ getToken: () => null }));
      const code = await cmd.execute('johndoe');
      expect(code).toBe(4);
    });

    it('returns 1 when username is missing', async () => {
      const cmd = new TeamAddCommand(makeDeps());
      const code = await cmd.execute('');
      expect(code).toBe(1);
    });

    it('returns 1 for invalid role', async () => {
      const cmd = new TeamAddCommand(makeDeps());
      const code = await cmd.execute('johndoe', { role: 'owner' });
      expect(code).toBe(1);
    });

    it('returns 1 when user not found', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new TeamAddCommand(makeDeps({
        addMember: async () => { throw new ApiError('Not found', 1, 404, 'USER_NOT_FOUND'); },
      }));
      const code = await cmd.execute('ghost');
      expect(code).toBe(1);
    });

    it('returns 3 when forbidden', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new TeamAddCommand(makeDeps({
        addMember: async () => { throw new ApiError('Forbidden', 3, 403, 'FORBIDDEN'); },
      }));
      const code = await cmd.execute('johndoe');
      expect(code).toBe(3);
    });

    it('returns 4 on 401 from API', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new TeamAddCommand(makeDeps({
        addMember: async () => { throw new ApiError('Unauthorized', 4, 401); },
      }));
      const code = await cmd.execute('johndoe');
      expect(code).toBe(4);
    });

    it('returns 3 on generic API error', async () => {
      const cmd = new TeamAddCommand(makeDeps({
        addMember: async () => { throw new Error('connection refused'); },
      }));
      const code = await cmd.execute('johndoe');
      expect(code).toBe(3);
    });
  });

  describe('human output', () => {
    it('shows success message with default role', async () => {
      const cmd = new TeamAddCommand(makeDeps());
      await cmd.execute('johndoe');
      expect(output[0]).toBe('Added johndoe as member.');
    });

    it('shows success message with specified role', async () => {
      const cmd = new TeamAddCommand(makeDeps());
      await cmd.execute('johndoe', { role: 'admin' });
      expect(output[0]).toBe('Added johndoe as admin.');
    });

    it('shows idempotent message when already a member', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new TeamAddCommand(makeDeps({
        addMember: async () => { throw new ApiError('Already a member', 1, 409, 'ALREADY_MEMBER'); },
      }));
      await cmd.execute('johndoe');
      expect(output[0]).toBe('johndoe is already a member.');
    });

    it('shows user not found with login suggestion', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new TeamAddCommand(makeDeps({
        addMember: async () => { throw new ApiError('Not found', 1, 404, 'USER_NOT_FOUND'); },
      }));
      await cmd.execute('ghost');
      expect(errors[0]).toContain('User ghost not found');
      expect(errors[0]).toContain('loopzero login');
    });

    it('shows invalid role error with valid options', async () => {
      const cmd = new TeamAddCommand(makeDeps());
      await cmd.execute('johndoe', { role: 'owner' });
      expect(errors[0]).toContain('Invalid role');
      expect(errors[0]).toContain('member, admin');
      expect(errors[0]).toContain('Cannot add as owner');
    });

    it('shows forbidden error', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new TeamAddCommand(makeDeps({
        addMember: async () => { throw new ApiError('Forbidden', 3, 403, 'FORBIDDEN'); },
      }));
      await cmd.execute('johndoe');
      expect(errors[0]).toContain('Permission denied');
      expect(errors[0]).toContain('owners and admins');
    });
  });

  describe('--json output', () => {
    it('outputs JSON on success', async () => {
      const cmd = new TeamAddCommand(makeDeps());
      const code = await cmd.execute('johndoe', { json: true });
      expect(code).toBe(0);
      const parsed = JSON.parse(output[0]);
      expect(parsed.ok).toBe(true);
      expect(parsed.username).toBe('johndoe');
      expect(parsed.role).toBe('member');
    });

    it('outputs JSON on error', async () => {
      const cmd = new TeamAddCommand(makeDeps({ getToken: () => null }));
      const code = await cmd.execute('johndoe', { json: true });
      expect(code).toBe(4);
      const parsed = JSON.parse(output[0]);
      expect(parsed.error).toBeDefined();
      expect(parsed.exit_code).toBe(4);
      expect(parsed.code).toBe('AUTH_REQUIRED');
    });

    it('outputs JSON on already member (idempotent)', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new TeamAddCommand(makeDeps({
        addMember: async () => { throw new ApiError('Already a member', 1, 409, 'ALREADY_MEMBER'); },
      }));
      const code = await cmd.execute('johndoe', { json: true });
      expect(code).toBe(0);
      const parsed = JSON.parse(output[0]);
      expect(parsed.ok).toBe(true);
      expect(parsed.already_member).toBe(true);
    });
  });

  describe('no internal terms', () => {
    const FORBIDDEN_TERMS = [
      'ArgoCD', 'argocd', 'GitOps', 'gitops-push', 'CRD', 'tenant_id',
      'advisory lock', 'pg_advisory', 'Gitea', 'MinIO', 'kubebuilder',
      'envtest', 'Kaniko',
    ];

    it('success output contains no internal terms', async () => {
      const cmd = new TeamAddCommand(makeDeps());
      await cmd.execute('johndoe');
      const allOutput = [...output, ...errors].join(' ');
      for (const term of FORBIDDEN_TERMS) {
        expect(allOutput).not.toContain(term);
      }
    });

    it('error output contains no internal terms', async () => {
      const cmd = new TeamAddCommand(makeDeps({ getToken: () => null }));
      await cmd.execute('johndoe');
      const allOutput = [...output, ...errors].join(' ');
      for (const term of FORBIDDEN_TERMS) {
        expect(allOutput).not.toContain(term);
      }
    });
  });
});
