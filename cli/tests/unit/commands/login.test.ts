import { LoginCommand, LoginDeps } from '../../../src/commands/login';

function createMockDeps(overrides: Partial<LoginDeps> = {}): LoginDeps {
  return {
    saveToken: jest.fn(),
    validateToken: jest.fn().mockResolvedValue({
      valid: true,
      email: 'user@example.com',
      teams: ['acme'],
    }),
    writeTo: jest.fn(),
    writeError: jest.fn(),
    isCI: false,
    ...overrides,
  };
}

describe('LoginCommand', () => {
  describe('--token flow', () => {
    it('should validate and save a valid token', async () => {
      const deps = createMockDeps();
      const cmd = new LoginCommand(deps);

      const exitCode = await cmd.execute({ token: 'REDACTED_SECRET_064', team: undefined });

      expect(deps.validateToken).toHaveBeenCalledWith('REDACTED_SECRET_064');
      expect(deps.saveToken).toHaveBeenCalledWith('REDACTED_SECRET_064', 'user@example.com', 'acme');
      expect(exitCode).toBe(0);
    });

    it('should return exit code 4 for invalid token', async () => {
      const deps = createMockDeps({
        validateToken: jest.fn().mockResolvedValue({ valid: false, email: null, teams: [] }),
      });
      const cmd = new LoginCommand(deps);

      const exitCode = await cmd.execute({ token: 'ghp_invalid', team: undefined });

      expect(deps.saveToken).not.toHaveBeenCalled();
      expect(exitCode).toBe(4);
    });

    it('should output human-readable success message', async () => {
      const deps = createMockDeps();
      const cmd = new LoginCommand(deps);

      await cmd.execute({ token: 'ghp_valid', team: undefined });

      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('Authenticated as user@example.com');
      expect(output).toContain('acme');
    });

    it('should output JSON when json flag is set', async () => {
      const deps = createMockDeps();
      const cmd = new LoginCommand(deps);

      await cmd.execute({ token: 'ghp_valid', team: undefined, json: true });

      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.authenticated).toBe(true);
      expect(parsed.email).toBe('user@example.com');
      expect(parsed.team).toBe('acme');
    });

    it('should never include the token in output', async () => {
      const deps = createMockDeps();
      const cmd = new LoginCommand(deps);

      await cmd.execute({ token: 'ghp_secret_token', team: undefined });

      const allOutput = [
        ...(deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]),
        ...(deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]),
      ].join(' ');
      expect(allOutput).not.toContain('ghp_secret_token');
    });

    it('should output auth error message on invalid token', async () => {
      const deps = createMockDeps({
        validateToken: jest.fn().mockResolvedValue({ valid: false, email: null, teams: [] }),
      });
      const cmd = new LoginCommand(deps);

      await cmd.execute({ token: 'ghp_bad', team: undefined });

      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('Authentication failed');
    });

    it('should show service unavailable message when server returns 500', async () => {
      const deps = createMockDeps({
        validateToken: jest.fn().mockResolvedValue({ valid: false, email: null, teams: [], serverError: true }),
      });
      const cmd = new LoginCommand(deps);

      const exitCode = await cmd.execute({ token: 'ghp_valid', team: undefined });

      expect(exitCode).toBe(4);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('Service temporarily unavailable');
      expect(errOutput).not.toContain('Authentication failed');
    });
  });

  describe('--team flag', () => {
    it('should auto-resolve when user has exactly one team', async () => {
      const deps = createMockDeps({
        validateToken: jest.fn().mockResolvedValue({
          valid: true,
          email: 'user@example.com',
          teams: ['solo-team'],
        }),
      });
      const cmd = new LoginCommand(deps);

      const exitCode = await cmd.execute({ token: 'ghp_valid', team: undefined });

      expect(deps.saveToken).toHaveBeenCalledWith('ghp_valid', 'user@example.com', 'solo-team');
      expect(exitCode).toBe(0);
    });

    it('should use --team flag to select among multiple teams', async () => {
      const deps = createMockDeps({
        validateToken: jest.fn().mockResolvedValue({
          valid: true,
          email: 'user@example.com',
          teams: ['acme', 'globex', 'initech'],
        }),
      });
      const cmd = new LoginCommand(deps);

      const exitCode = await cmd.execute({ token: 'ghp_valid', team: 'globex' });

      expect(deps.saveToken).toHaveBeenCalledWith('ghp_valid', 'user@example.com', 'globex');
      expect(exitCode).toBe(0);
    });

    it('should exit 1 when --team specifies a team user does not belong to', async () => {
      const deps = createMockDeps({
        validateToken: jest.fn().mockResolvedValue({
          valid: true,
          email: 'user@example.com',
          teams: ['acme', 'globex'],
        }),
      });
      const cmd = new LoginCommand(deps);

      const exitCode = await cmd.execute({ token: 'ghp_valid', team: 'unknown-team' });

      expect(deps.saveToken).not.toHaveBeenCalled();
      expect(exitCode).toBe(1);
    });

    it('should exit 1 in CI mode when multiple teams and no --team flag', async () => {
      const deps = createMockDeps({
        isCI: true,
        validateToken: jest.fn().mockResolvedValue({
          valid: true,
          email: 'user@example.com',
          teams: ['acme', 'globex', 'initech'],
        }),
      });
      const cmd = new LoginCommand(deps);

      const exitCode = await cmd.execute({ token: 'ghp_valid', team: undefined });

      expect(deps.saveToken).not.toHaveBeenCalled();
      expect(exitCode).toBe(1);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('--team');
      expect(errOutput).toContain('acme');
      expect(errOutput).toContain('globex');
    });
  });

  describe('no --token (non-CI)', () => {
    it('should exit 1 without --token with actionable message', async () => {
      const deps = createMockDeps();
      const cmd = new LoginCommand(deps);

      const exitCode = await cmd.execute({ team: undefined });

      expect(exitCode).toBe(1);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('Token is required');
    });

    it('should exit 1 in CI mode without --token with CI-specific message', async () => {
      const deps = createMockDeps({ isCI: true });
      const cmd = new LoginCommand(deps);

      const exitCode = await cmd.execute({ team: undefined });

      expect(exitCode).toBe(1);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('CI');
      expect(errOutput).toContain('--token');
    });
  });

  describe('server unreachable (F-12)', () => {
    it('should show service unavailable when server is unreachable (serverError flag)', async () => {
      const deps = createMockDeps({
        validateToken: jest.fn().mockResolvedValue({
          valid: false,
          email: null,
          teams: [],
          serverError: true,
        }),
      });
      const cmd = new LoginCommand(deps);

      const exitCode = await cmd.execute({ token: 'ghp_valid', team: undefined });

      expect(exitCode).toBe(4);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('Service temporarily unavailable');
      expect(errOutput).not.toContain('Authentication failed');
    });

    it('should not show auth error message for server errors', async () => {
      const deps = createMockDeps({
        validateToken: jest.fn().mockResolvedValue({
          valid: false,
          email: null,
          teams: [],
          serverError: true,
        }),
      });
      const cmd = new LoginCommand(deps);

      await cmd.execute({ token: 'ghp_valid', team: undefined });

      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).not.toContain('Check your token');
    });
  });

  describe('private email fallback (F-13)', () => {
    it('should save noreply email when email contains @users.noreply.github.com', async () => {
      const deps = createMockDeps({
        validateToken: jest.fn().mockResolvedValue({
          valid: true,
          email: 'aquimai@users.noreply.github.com',
          teams: ['acme'],
        }),
      });
      const cmd = new LoginCommand(deps);

      const exitCode = await cmd.execute({ token: 'ghp_valid', team: undefined });

      expect(exitCode).toBe(0);
      expect(deps.saveToken).toHaveBeenCalledWith(
        'ghp_valid',
        'aquimai@users.noreply.github.com',
        'acme'
      );
    });

    it('should display noreply email in human output', async () => {
      const deps = createMockDeps({
        validateToken: jest.fn().mockResolvedValue({
          valid: true,
          email: 'aquimai@users.noreply.github.com',
          teams: ['acme'],
        }),
      });
      const cmd = new LoginCommand(deps);

      await cmd.execute({ token: 'ghp_valid', team: undefined });

      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(output).toContain('aquimai@users.noreply.github.com');
    });
  });

  describe('--json flag consistency (F-11)', () => {
    it('should output JSON error when --json and invalid token', async () => {
      const deps = createMockDeps({
        validateToken: jest.fn().mockResolvedValue({ valid: false, email: null, teams: [] }),
      });
      const cmd = new LoginCommand(deps);

      const exitCode = await cmd.execute({ token: 'ghp_bad', json: true });

      expect(exitCode).toBe(4);
      expect(deps.writeError).not.toHaveBeenCalled();
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.error).toContain('Authentication failed');
      expect(parsed.exit_code).toBe(4);
    });

    it('should output JSON error when --json and CI mode missing token', async () => {
      const deps = createMockDeps({ isCI: true });
      const cmd = new LoginCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(1);
      expect(deps.writeError).not.toHaveBeenCalled();
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.error).toContain('CI');
      expect(parsed.exit_code).toBe(1);
    });

    it('should output JSON error when --json and server unreachable', async () => {
      const deps = createMockDeps({
        validateToken: jest.fn().mockResolvedValue({
          valid: false, email: null, teams: [], serverError: true,
        }),
      });
      const cmd = new LoginCommand(deps);

      const exitCode = await cmd.execute({ token: 'ghp_valid', json: true });

      expect(exitCode).toBe(4);
      expect(deps.writeError).not.toHaveBeenCalled();
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.error).toContain('Service temporarily unavailable');
      expect(parsed.exit_code).toBe(4);
    });

    it('should output JSON error when --json and team not found', async () => {
      const deps = createMockDeps({
        validateToken: jest.fn().mockResolvedValue({
          valid: true, email: 'user@example.com', teams: ['acme', 'globex'],
        }),
      });
      const cmd = new LoginCommand(deps);

      const exitCode = await cmd.execute({ token: 'ghp_valid', team: 'nonexistent', json: true });

      expect(exitCode).toBe(1);
      expect(deps.writeError).not.toHaveBeenCalled();
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.error).toContain('nonexistent');
      expect(parsed.exit_code).toBe(1);
    });

    it('should output JSON error when --json and no teams available', async () => {
      const deps = createMockDeps({
        validateToken: jest.fn().mockResolvedValue({
          valid: true, email: 'user@example.com', teams: [],
        }),
      });
      const cmd = new LoginCommand(deps);

      const exitCode = await cmd.execute({ token: 'ghp_valid', json: true });

      expect(exitCode).toBe(1);
      expect(deps.writeError).not.toHaveBeenCalled();
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.error).toContain('No teams');
      expect(parsed.exit_code).toBe(1);
    });

    it('should output JSON error when --json and multiple teams without --team flag', async () => {
      const deps = createMockDeps({
        isCI: true,
        validateToken: jest.fn().mockResolvedValue({
          valid: true, email: 'user@example.com', teams: ['acme', 'globex'],
        }),
      });
      const cmd = new LoginCommand(deps);

      const exitCode = await cmd.execute({ token: 'ghp_valid', json: true });

      expect(exitCode).toBe(1);
      expect(deps.writeError).not.toHaveBeenCalled();
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.error).toContain('--team');
      expect(parsed.exit_code).toBe(1);
    });

    it('should output JSON error when --json and no token', async () => {
      const deps = createMockDeps();
      const cmd = new LoginCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(1);
      expect(deps.writeError).not.toHaveBeenCalled();
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.error).toContain('Token is required');
      expect(parsed.exit_code).toBe(1);
    });
  });

  describe('JSON error code field', () => {
    it('should include code MISSING_TOKEN when --json and no token (non-CI)', async () => {
      const deps = createMockDeps();
      const cmd = new LoginCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(1);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.code).toBe('MISSING_TOKEN');
    });

    it('should include code MISSING_TOKEN when --json and CI mode missing token', async () => {
      const deps = createMockDeps({ isCI: true });
      const cmd = new LoginCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(1);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.code).toBe('MISSING_TOKEN');
    });

    it('should include code AUTH_FAILED when --json and invalid token', async () => {
      const deps = createMockDeps({
        validateToken: jest.fn().mockResolvedValue({ valid: false, email: null, teams: [] }),
      });
      const cmd = new LoginCommand(deps);

      const exitCode = await cmd.execute({ token: 'ghp_bad', json: true });

      expect(exitCode).toBe(4);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.code).toBe('AUTH_FAILED');
    });

    it('should include code SERVICE_UNAVAILABLE when --json and server error', async () => {
      const deps = createMockDeps({
        validateToken: jest.fn().mockResolvedValue({
          valid: false, email: null, teams: [], serverError: true,
        }),
      });
      const cmd = new LoginCommand(deps);

      const exitCode = await cmd.execute({ token: 'ghp_valid', json: true });

      expect(exitCode).toBe(4);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.code).toBe('SERVICE_UNAVAILABLE');
    });

    it('should include code TEAM_NOT_FOUND when --json and team not found', async () => {
      const deps = createMockDeps({
        validateToken: jest.fn().mockResolvedValue({
          valid: true, email: 'user@example.com', teams: ['acme', 'globex'],
        }),
      });
      const cmd = new LoginCommand(deps);

      const exitCode = await cmd.execute({ token: 'ghp_valid', team: 'nonexistent', json: true });

      expect(exitCode).toBe(1);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.code).toBe('TEAM_NOT_FOUND');
    });

    it('should include code TEAM_REQUIRED when --json and multiple teams without --team', async () => {
      const deps = createMockDeps({
        isCI: true,
        validateToken: jest.fn().mockResolvedValue({
          valid: true, email: 'user@example.com', teams: ['acme', 'globex'],
        }),
      });
      const cmd = new LoginCommand(deps);

      const exitCode = await cmd.execute({ token: 'ghp_valid', json: true });

      expect(exitCode).toBe(1);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.code).toBe('TEAM_REQUIRED');
    });

    it('should include code NO_TEAMS when --json and no teams available', async () => {
      const deps = createMockDeps({
        validateToken: jest.fn().mockResolvedValue({
          valid: true, email: 'user@example.com', teams: [],
        }),
      });
      const cmd = new LoginCommand(deps);

      const exitCode = await cmd.execute({ token: 'ghp_valid', json: true });

      expect(exitCode).toBe(1);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.code).toBe('NO_TEAMS');
    });
  });

  describe('no internal terms', () => {
    it('should not contain forbidden terms in any output', async () => {
      const forbidden = [
        'ArgoCD', 'argocd', 'GitOps', 'gitops-push', 'CRD',
        'tenant_id', 'advisory lock', 'pg_advisory', 'Gitea',
        'MinIO', 'kubebuilder', 'envtest', 'Kaniko',
      ];

      // Test success path
      const deps1 = createMockDeps();
      const cmd1 = new LoginCommand(deps1);
      await cmd1.execute({ token: 'ghp_valid', team: undefined });

      // Test error path
      const deps2 = createMockDeps({
        validateToken: jest.fn().mockResolvedValue({ valid: false, email: null, teams: [] }),
      });
      const cmd2 = new LoginCommand(deps2);
      await cmd2.execute({ token: 'ghp_bad', team: undefined });

      for (const deps of [deps1, deps2]) {
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
