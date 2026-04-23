// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { TeamListCommand, TeamListDeps } from '../../../src/commands/team-list';

describe('TeamListCommand', () => {
  let output: string[];
  let errors: string[];

  function makeDeps(overrides: Partial<TeamListDeps> = {}): TeamListDeps {
    return {
      getToken: () => 'test-token',
      listMembers: async () => ({
        members: [
          { username: 'paulohenrique', email: 'paulo@example.com', role: 'owner', joined_at: '2026-01-01T00:00:00Z' },
          { username: 'johndoe', email: 'john@example.com', role: 'admin', joined_at: '2026-01-15T00:00:00Z' },
          { username: 'janedoe', email: 'jane@example.com', role: 'member', joined_at: '2026-02-01T00:00:00Z' },
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
      const cmd = new TeamListCommand(makeDeps());
      const code = await cmd.execute();
      expect(code).toBe(0);
    });

    it('returns 0 when no members found', async () => {
      const cmd = new TeamListCommand(makeDeps({
        listMembers: async () => ({ members: [] }),
      }));
      const code = await cmd.execute();
      expect(code).toBe(0);
    });

    it('returns 4 when not authenticated', async () => {
      const cmd = new TeamListCommand(makeDeps({ getToken: () => null }));
      const code = await cmd.execute();
      expect(code).toBe(4);
    });

    it('returns 1 on API failure', async () => {
      const cmd = new TeamListCommand(makeDeps({
        listMembers: async () => { throw new Error('connection refused'); },
      }));
      const code = await cmd.execute();
      expect(code).toBe(1);
    });

    it('returns 4 on 401 from API', async () => {
      const { ApiError } = await import('../../../src/api/client');
      const cmd = new TeamListCommand(makeDeps({
        listMembers: async () => { throw new ApiError('Unauthorized', 4, 401); },
      }));
      const code = await cmd.execute();
      expect(code).toBe(4);
    });
  });

  describe('human output', () => {
    it('shows member list with usernames, roles, and emails', async () => {
      const cmd = new TeamListCommand(makeDeps());
      await cmd.execute();
      const allOutput = output.join('\n');
      expect(allOutput).toContain('Team members:');
      expect(allOutput).toContain('paulohenrique');
      expect(allOutput).toContain('johndoe');
      expect(allOutput).toContain('janedoe');
      expect(allOutput).toContain('owner');
      expect(allOutput).toContain('admin');
      expect(allOutput).toContain('member');
      expect(allOutput).toContain('3 members');
    });

    it('shows singular "member" for single result', async () => {
      const cmd = new TeamListCommand(makeDeps({
        listMembers: async () => ({
          members: [{ username: 'only-one', email: 'only@example.com', role: 'owner', joined_at: '2026-01-01T00:00:00Z' }],
        }),
      }));
      await cmd.execute();
      const allOutput = output.join('\n');
      expect(allOutput).toContain('1 member');
      expect(allOutput).not.toContain('1 members');
    });

    it('shows "No team members found." when empty', async () => {
      const cmd = new TeamListCommand(makeDeps({
        listMembers: async () => ({ members: [] }),
      }));
      await cmd.execute();
      expect(output[0]).toBe('No team members found.');
    });

    it('shows error message on auth failure', async () => {
      const cmd = new TeamListCommand(makeDeps({ getToken: () => null }));
      await cmd.execute();
      expect(errors[0]).toContain('Not authenticated');
      expect(errors[0]).toContain('loopzero login');
    });
  });

  describe('--json output', () => {
    it('outputs JSON with members array and total', async () => {
      const cmd = new TeamListCommand(makeDeps());
      const code = await cmd.execute({ json: true });
      expect(code).toBe(0);
      const parsed = JSON.parse(output[0]);
      expect(parsed.members).toHaveLength(3);
      expect(parsed.total).toBe(3);
      expect(parsed.members[0].username).toBe('paulohenrique');
      expect(parsed.members[1].role).toBe('admin');
    });

    it('outputs JSON on error', async () => {
      const cmd = new TeamListCommand(makeDeps({ getToken: () => null }));
      const code = await cmd.execute({ json: true });
      expect(code).toBe(4);
      const parsed = JSON.parse(output[0]);
      expect(parsed.error).toBeDefined();
      expect(parsed.exit_code).toBe(4);
      expect(parsed.code).toBe('AUTH_REQUIRED');
    });

    it('outputs JSON with empty array when no members', async () => {
      const cmd = new TeamListCommand(makeDeps({
        listMembers: async () => ({ members: [] }),
      }));
      const code = await cmd.execute({ json: true });
      expect(code).toBe(0);
      const parsed = JSON.parse(output[0]);
      expect(parsed.members).toEqual([]);
      expect(parsed.total).toBe(0);
    });
  });

  // CHG-2026-04-23-AUDIT-24: network failures were being reported as generic
  // "Failed to list team members: fetch failed. Try again." with
  // code=UNEXPECTED_ERROR — same hint as a transient server error, no
  // indication to check LOOPZERO_URL. Now matches whoami / audit pattern.
  describe('API unreachable (AUDIT-24)', () => {
    it('network error produces API_UNREACHABLE code + Check LOOPZERO_URL hint', async () => {
      const cmd = new TeamListCommand(makeDeps({
        listMembers: async () => { throw new Error('fetch failed'); },
      }));
      const code = await cmd.execute({ json: true });
      expect(code).toBe(1);
      const parsed = JSON.parse(output[0]);
      expect(parsed.code).toBe('API_UNREACHABLE');
      expect(parsed.error).toContain('Cannot reach API');
      expect(parsed.error).toContain('Check LOOPZERO_URL');
    });

    it('ECONNREFUSED classified as API_UNREACHABLE', async () => {
      const cmd = new TeamListCommand(makeDeps({
        listMembers: async () => { throw new Error('connect ECONNREFUSED 127.0.0.1:8080'); },
      }));
      const code = await cmd.execute({ json: true });
      expect(code).toBe(1);
      const parsed = JSON.parse(output[0]);
      expect(parsed.code).toBe('API_UNREACHABLE');
    });

    it('non-network Error keeps UNEXPECTED_ERROR code', async () => {
      const cmd = new TeamListCommand(makeDeps({
        listMembers: async () => { throw new Error('some business logic'); },
      }));
      const code = await cmd.execute({ json: true });
      expect(code).toBe(1);
      const parsed = JSON.parse(output[0]);
      expect(parsed.code).toBe('UNEXPECTED_ERROR');
    });
  });

  describe('no internal terms', () => {
    const FORBIDDEN_TERMS = [
      'ArgoCD', 'argocd', 'GitOps', 'gitops-push', 'CRD', 'tenant_id',
      'advisory lock', 'pg_advisory', 'Gitea', 'MinIO', 'kubebuilder',
      'envtest', 'Kaniko',
    ];

    it('success output contains no internal terms', async () => {
      const cmd = new TeamListCommand(makeDeps());
      await cmd.execute();
      const allOutput = [...output, ...errors].join(' ');
      for (const term of FORBIDDEN_TERMS) {
        expect(allOutput).not.toContain(term);
      }
    });

    it('error output contains no internal terms', async () => {
      const cmd = new TeamListCommand(makeDeps({ getToken: () => null }));
      await cmd.execute();
      const allOutput = [...output, ...errors].join(' ');
      for (const term of FORBIDDEN_TERMS) {
        expect(allOutput).not.toContain(term);
      }
    });
  });
});
