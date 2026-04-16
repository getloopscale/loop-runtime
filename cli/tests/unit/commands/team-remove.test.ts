// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { TeamRemoveCommand, TeamRemoveDeps } from '../../../src/commands/team-remove';

describe('TeamRemoveCommand', () => {
  let output: string[];
  let errors: string[];

  function makeDeps(overrides: Partial<TeamRemoveDeps> = {}): TeamRemoveDeps {
    return {
      getToken: () => 'test-token',
      removeMember: async () => {},
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
    it('returns 0 on successful remove', async () => {
      const cmd = new TeamRemoveCommand(makeDeps());
      const code = await cmd.execute('johndoe');
      expect(code).toBe(0);
    });

    it('returns 4 when not authenticated', async () => {
      const cmd = new TeamRemoveCommand(makeDeps({ getToken: () => null }));
      const code = await cmd.execute('johndoe');
      expect(code).toBe(4);
    });

    it('returns 1 when username is missing', async () => {
      const cmd = new TeamRemoveCommand(makeDeps());
      const code = await cmd.execute('');
      expect(code).toBe(1);
    });

    it('returns 1 when trying to remove owner', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new TeamRemoveCommand(makeDeps({
        removeMember: async () => { throw new ApiError('Owner protected', 1, 409, 'OWNER_PROTECTED'); },
      }));
      const code = await cmd.execute('loopzerowner');
      expect(code).toBe(1);
    });

    it('returns 1 when trying to remove self', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new TeamRemoveCommand(makeDeps({
        removeMember: async () => { throw new ApiError('Self remove', 1, 409, 'SELF_REMOVE'); },
      }));
      const code = await cmd.execute('myself');
      expect(code).toBe(1);
    });

    it('returns 3 when forbidden', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new TeamRemoveCommand(makeDeps({
        removeMember: async () => { throw new ApiError('Forbidden', 3, 403, 'FORBIDDEN'); },
      }));
      const code = await cmd.execute('johndoe');
      expect(code).toBe(3);
    });

    it('returns 4 on 401 from API', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new TeamRemoveCommand(makeDeps({
        removeMember: async () => { throw new ApiError('Unauthorized', 4, 401); },
      }));
      const code = await cmd.execute('johndoe');
      expect(code).toBe(4);
    });

    it('returns 3 on generic error', async () => {
      const cmd = new TeamRemoveCommand(makeDeps({
        removeMember: async () => { throw new Error('network error'); },
      }));
      const code = await cmd.execute('johndoe');
      expect(code).toBe(3);
    });
  });

  describe('human output', () => {
    it('shows success message', async () => {
      const cmd = new TeamRemoveCommand(makeDeps());
      await cmd.execute('johndoe');
      expect(output[0]).toBe('Removed johndoe from team.');
    });

    it('shows owner protection error', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new TeamRemoveCommand(makeDeps({
        removeMember: async () => { throw new ApiError('Owner protected', 1, 409, 'OWNER_PROTECTED'); },
      }));
      await cmd.execute('loopzerowner');
      expect(errors[0]).toBe('Cannot remove the team owner.');
    });

    it('shows self-remove error with suggestion', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new TeamRemoveCommand(makeDeps({
        removeMember: async () => { throw new ApiError('Self remove', 1, 409, 'SELF_REMOVE'); },
      }));
      await cmd.execute('myself');
      expect(errors[0]).toContain('Cannot remove yourself');
      expect(errors[0]).toContain('Transfer ownership');
    });

    it('shows forbidden error', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new TeamRemoveCommand(makeDeps({
        removeMember: async () => { throw new ApiError('Forbidden', 3, 403, 'FORBIDDEN'); },
      }));
      await cmd.execute('johndoe');
      expect(errors[0]).toContain('Permission denied');
      expect(errors[0]).toContain('owners and admins');
    });
  });

  describe('--json output', () => {
    it('outputs JSON on success', async () => {
      const cmd = new TeamRemoveCommand(makeDeps());
      const code = await cmd.execute('johndoe', { json: true });
      expect(code).toBe(0);
      const parsed = JSON.parse(output[0]);
      expect(parsed.ok).toBe(true);
      expect(parsed.username).toBe('johndoe');
      expect(parsed.status).toBe('removed');
    });

    it('outputs JSON on error', async () => {
      const cmd = new TeamRemoveCommand(makeDeps({ getToken: () => null }));
      const code = await cmd.execute('johndoe', { json: true });
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
      const cmd = new TeamRemoveCommand(makeDeps());
      await cmd.execute('johndoe');
      const allOutput = [...output, ...errors].join(' ');
      for (const term of FORBIDDEN_TERMS) {
        expect(allOutput).not.toContain(term);
      }
    });

    it('error output contains no internal terms', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new TeamRemoveCommand(makeDeps({
        removeMember: async () => { throw new ApiError('Forbidden', 3, 403, 'FORBIDDEN'); },
      }));
      await cmd.execute('johndoe');
      const allOutput = [...output, ...errors].join(' ');
      for (const term of FORBIDDEN_TERMS) {
        expect(allOutput).not.toContain(term);
      }
    });
  });
});
