// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { TeamRoleCommand, TeamRoleDeps } from '../../../src/commands/team-role';

describe('TeamRoleCommand', () => {
  let output: string[];
  let errors: string[];

  function makeDeps(overrides: Partial<TeamRoleDeps> = {}): TeamRoleDeps {
    return {
      getToken: () => 'test-token',
      changeRole: async (username: string, role: string) => ({ username, role }),
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
    it('returns 0 on successful role change', async () => {
      const cmd = new TeamRoleCommand(makeDeps());
      const code = await cmd.execute('johndoe', 'admin');
      expect(code).toBe(0);
    });

    it('returns 4 when not authenticated', async () => {
      const cmd = new TeamRoleCommand(makeDeps({ getToken: () => null }));
      const code = await cmd.execute('johndoe', 'admin');
      expect(code).toBe(4);
    });

    it('returns 1 when username is missing', async () => {
      const cmd = new TeamRoleCommand(makeDeps());
      const code = await cmd.execute('', 'admin');
      expect(code).toBe(1);
    });

    it('returns 1 when role is missing', async () => {
      const cmd = new TeamRoleCommand(makeDeps());
      const code = await cmd.execute('johndoe', '');
      expect(code).toBe(1);
    });

    it('returns 1 for invalid role', async () => {
      const cmd = new TeamRoleCommand(makeDeps());
      const code = await cmd.execute('johndoe', 'owner');
      expect(code).toBe(1);
    });

    it('returns 1 for invalid role "superadmin"', async () => {
      const cmd = new TeamRoleCommand(makeDeps());
      const code = await cmd.execute('johndoe', 'superadmin');
      expect(code).toBe(1);
    });

    it('returns 3 when forbidden', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new TeamRoleCommand(makeDeps({
        changeRole: async () => { throw new ApiError('Forbidden', 3, 403, 'FORBIDDEN'); },
      }));
      const code = await cmd.execute('johndoe', 'admin');
      expect(code).toBe(3);
    });

    it('returns 4 on 401 from API', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new TeamRoleCommand(makeDeps({
        changeRole: async () => { throw new ApiError('Unauthorized', 4, 401); },
      }));
      const code = await cmd.execute('johndoe', 'admin');
      expect(code).toBe(4);
    });

    it('returns 1 when trying to change owner role', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new TeamRoleCommand(makeDeps({
        changeRole: async () => { throw new ApiError('Owner protected', 1, 409, 'OWNER_PROTECTED'); },
      }));
      const code = await cmd.execute('loopzerowner', 'member');
      expect(code).toBe(1);
    });

    it('returns 1 when member not found', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new TeamRoleCommand(makeDeps({
        changeRole: async () => { throw new ApiError('Not found', 1, 404, 'MEMBER_NOT_FOUND'); },
      }));
      const code = await cmd.execute('ghost', 'admin');
      expect(code).toBe(1);
    });
  });

  describe('human output', () => {
    it('shows success message', async () => {
      const cmd = new TeamRoleCommand(makeDeps());
      await cmd.execute('johndoe', 'admin');
      expect(output[0]).toBe("Changed johndoe's role to admin.");
    });

    it('shows invalid role error with valid options', async () => {
      const cmd = new TeamRoleCommand(makeDeps());
      await cmd.execute('johndoe', 'owner');
      expect(errors[0]).toContain('Invalid role');
      expect(errors[0]).toContain('admin, member');
      expect(errors[0]).toContain('Ownership transfer');
    });

    it('shows forbidden error', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new TeamRoleCommand(makeDeps({
        changeRole: async () => { throw new ApiError('Forbidden', 3, 403, 'FORBIDDEN'); },
      }));
      await cmd.execute('johndoe', 'admin');
      expect(errors[0]).toContain('Permission denied');
      expect(errors[0]).toContain('owner');
    });

    it('shows owner protected error', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new TeamRoleCommand(makeDeps({
        changeRole: async () => { throw new ApiError('Owner protected', 1, 409, 'OWNER_PROTECTED'); },
      }));
      await cmd.execute('loopzerowner', 'member');
      expect(errors[0]).toContain("Cannot change the owner's role");
      expect(errors[0]).toContain('Transfer ownership');
    });
  });

  describe('--json output', () => {
    it('outputs JSON on success', async () => {
      const cmd = new TeamRoleCommand(makeDeps());
      const code = await cmd.execute('johndoe', 'admin', { json: true });
      expect(code).toBe(0);
      const parsed = JSON.parse(output[0]);
      expect(parsed.ok).toBe(true);
      expect(parsed.username).toBe('johndoe');
      expect(parsed.role).toBe('admin');
    });

    it('outputs JSON on error', async () => {
      const cmd = new TeamRoleCommand(makeDeps({ getToken: () => null }));
      const code = await cmd.execute('johndoe', 'admin', { json: true });
      expect(code).toBe(4);
      const parsed = JSON.parse(output[0]);
      expect(parsed.error).toBeDefined();
      expect(parsed.exit_code).toBe(4);
      expect(parsed.code).toBe('AUTH_REQUIRED');
    });

    it('outputs JSON on invalid role', async () => {
      const cmd = new TeamRoleCommand(makeDeps());
      const code = await cmd.execute('johndoe', 'owner', { json: true });
      expect(code).toBe(1);
      const parsed = JSON.parse(output[0]);
      expect(parsed.error).toContain('Invalid role');
      expect(parsed.code).toBe('INVALID_ROLE');
    });
  });

  describe('no internal terms', () => {
    const FORBIDDEN_TERMS = [
      'ArgoCD', 'argocd', 'GitOps', 'gitops-push', 'CRD', 'tenant_id',
      'advisory lock', 'pg_advisory', 'Gitea', 'MinIO', 'kubebuilder',
      'envtest', 'Kaniko',
    ];

    it('success output contains no internal terms', async () => {
      const cmd = new TeamRoleCommand(makeDeps());
      await cmd.execute('johndoe', 'admin');
      const allOutput = [...output, ...errors].join(' ');
      for (const term of FORBIDDEN_TERMS) {
        expect(allOutput).not.toContain(term);
      }
    });

    it('error output contains no internal terms', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new TeamRoleCommand(makeDeps({
        changeRole: async () => { throw new ApiError('Forbidden', 3, 403, 'FORBIDDEN'); },
      }));
      await cmd.execute('johndoe', 'admin');
      const allOutput = [...output, ...errors].join(' ');
      for (const term of FORBIDDEN_TERMS) {
        expect(allOutput).not.toContain(term);
      }
    });
  });
});
