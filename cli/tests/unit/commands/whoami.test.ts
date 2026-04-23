// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { WhoamiCommand, WhoamiDeps } from '../../../src/commands/whoami';

function createMockDeps(overrides: Partial<WhoamiDeps> = {}): WhoamiDeps {
  return {
    getToken: jest.fn().mockReturnValue('test-token'),
    getEmail: jest.fn().mockReturnValue('user@example.com'),
    getTeam: jest.fn().mockReturnValue('acme'),
    writeTo: jest.fn(),
    writeError: jest.fn(),
    ...overrides,
  };
}

describe('WhoamiCommand', () => {
  describe('authenticated (no verifyToken — backwards compat)', () => {
    it('should output email and team and return exit code 0', async () => {
      const deps = createMockDeps();
      const cmd = new WhoamiCommand(deps);

      const exitCode = await cmd.execute();

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('user@example.com');
      expect(output).toContain('acme');
    });

    it('should output JSON when json flag is set', async () => {
      const deps = createMockDeps();
      const cmd = new WhoamiCommand(deps);

      await cmd.execute({ json: true });

      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.authenticated).toBe(true);
      expect(parsed.email).toBe('user@example.com');
      expect(parsed.team).toBe('acme');
    });
  });

  describe('authenticated (with verifyToken)', () => {
    it('should validate token against API and return server-side data', async () => {
      const deps = createMockDeps({
        verifyToken: jest.fn().mockResolvedValue({ email: 'real@example.com', team: 'real-team' }),
      });
      const cmd = new WhoamiCommand(deps);

      const exitCode = await cmd.execute();

      expect(exitCode).toBe(0);
      expect(deps.verifyToken).toHaveBeenCalledWith('test-token');
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('real@example.com');
      expect(output).toContain('real-team');
    });

    it('should return exit 4 when API rejects token', async () => {
      const deps = createMockDeps({
        verifyToken: jest.fn().mockResolvedValue(null),
      });
      const cmd = new WhoamiCommand(deps);

      const exitCode = await cmd.execute();

      expect(exitCode).toBe(4);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('invalid or expired');
    });

    it('should return exit 4 JSON when API rejects token', async () => {
      const deps = createMockDeps({
        verifyToken: jest.fn().mockResolvedValue(null),
      });
      const cmd = new WhoamiCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(4);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.authenticated).toBe(false);
      expect(parsed.error).toContain('invalid or expired');
    });

    it('should use API data in JSON mode', async () => {
      const deps = createMockDeps({
        verifyToken: jest.fn().mockResolvedValue({ email: 'api@test.com', team: 'api-team' }),
      });
      const cmd = new WhoamiCommand(deps);

      await cmd.execute({ json: true });

      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.email).toBe('api@test.com');
      expect(parsed.team).toBe('api-team');
    });
  });

  // CHG-2026-04-23-AUDIT-10: service-account tokens (Hydra client_credentials)
  // have no email claim. API returns empty email but populates `username` from
  // the users.username column (migration 000037). CLI must use username as
  // display fallback — previously showed "Authenticated as: " with no value.
  describe('service account identity (AUDIT-10)', () => {
    it('displays username when email is empty', async () => {
      const deps = createMockDeps({
        getToken: jest.fn().mockReturnValue('bearer-token'),
        verifyToken: jest.fn().mockResolvedValue({
          email: '', username: 'loopzero-e2e', team: 'e2e-tenant',
        }),
      });
      const cmd = new WhoamiCommand(deps);
      await cmd.execute();
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('Authenticated as: loopzero-e2e');
      expect(output).not.toMatch(/Authenticated as: $/m);
    });

    it('prefers email over username when both present', async () => {
      const deps = createMockDeps({
        getToken: jest.fn().mockReturnValue('bearer-token'),
        verifyToken: jest.fn().mockResolvedValue({
          email: 'me@example.com', username: 'me', team: 'my-team',
        }),
      });
      const cmd = new WhoamiCommand(deps);
      await cmd.execute();
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('Authenticated as: me@example.com');
    });

    it('JSON output includes username when API returns it', async () => {
      const deps = createMockDeps({
        getToken: jest.fn().mockReturnValue('bearer-token'),
        verifyToken: jest.fn().mockResolvedValue({
          email: '', username: 'loopzero-e2e', team: 'e2e-tenant',
        }),
      });
      const cmd = new WhoamiCommand(deps);
      await cmd.execute({ json: true });
      const parsed = JSON.parse((deps.writeTo as jest.Mock).mock.calls[0][0]);
      expect(parsed.authenticated).toBe(true);
      expect(parsed.username).toBe('loopzero-e2e');
    });
  });

  describe('not authenticated', () => {
    it('should return exit code 4 when no token and no email', async () => {
      const deps = createMockDeps({
        getToken: jest.fn().mockReturnValue(null),
        getEmail: jest.fn().mockReturnValue(null),
        getTeam: jest.fn().mockReturnValue(null),
      });
      const cmd = new WhoamiCommand(deps);

      const exitCode = await cmd.execute();

      expect(exitCode).toBe(4);
    });

    it('should output auth error message', async () => {
      const deps = createMockDeps({
        getToken: jest.fn().mockReturnValue(null),
        getEmail: jest.fn().mockReturnValue(null),
        getTeam: jest.fn().mockReturnValue(null),
      });
      const cmd = new WhoamiCommand(deps);

      await cmd.execute();

      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('Not authenticated');
      expect(errOutput).toContain('loopzero login');
    });

    it('should output JSON with authenticated=false', async () => {
      const deps = createMockDeps({
        getToken: jest.fn().mockReturnValue(null),
        getEmail: jest.fn().mockReturnValue(null),
        getTeam: jest.fn().mockReturnValue(null),
      });
      const cmd = new WhoamiCommand(deps);

      await cmd.execute({ json: true });

      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.authenticated).toBe(false);
    });
  });

  describe('no internal terms', () => {
    it('should not contain forbidden terms in any output', async () => {
      const forbidden = [
        'ArgoCD', 'argocd', 'GitOps', 'gitops-push', 'CRD',
        'tenant_id', 'advisory lock', 'pg_advisory', 'Gitea',
        'MinIO', 'kubebuilder', 'envtest', 'Kaniko',
      ];

      const deps1 = createMockDeps();
      const cmd1 = new WhoamiCommand(deps1);
      await cmd1.execute();

      const deps2 = createMockDeps({
        getToken: jest.fn().mockReturnValue(null),
        getEmail: jest.fn().mockReturnValue(null),
        getTeam: jest.fn().mockReturnValue(null),
      });
      const cmd2 = new WhoamiCommand(deps2);
      await cmd2.execute();

      // Also check invalid token scenario
      const deps3 = createMockDeps({
        verifyToken: jest.fn().mockResolvedValue(null),
      });
      const cmd3 = new WhoamiCommand(deps3);
      await cmd3.execute();

      for (const deps of [deps1, deps2, deps3]) {
        const allOutput = [
          ...(deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]),
          ...(deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]),
        ].join(' ');
        for (const term of forbidden) {
          expect(allOutput).not.toContain(term);
        }
      }
    });
  });
});
