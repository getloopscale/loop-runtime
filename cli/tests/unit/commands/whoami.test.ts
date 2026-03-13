import { WhoamiCommand, WhoamiDeps } from '../../../src/commands/whoami';

function createMockDeps(overrides: Partial<WhoamiDeps> = {}): WhoamiDeps {
  return {
    getEmail: jest.fn().mockReturnValue('user@example.com'),
    getTeam: jest.fn().mockReturnValue('acme'),
    writeTo: jest.fn(),
    writeError: jest.fn(),
    ...overrides,
  };
}

describe('WhoamiCommand', () => {
  describe('authenticated', () => {
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

  describe('not authenticated', () => {
    it('should return exit code 4 when no token', async () => {
      const deps = createMockDeps({
        getEmail: jest.fn().mockReturnValue(null),
        getTeam: jest.fn().mockReturnValue(null),
      });
      const cmd = new WhoamiCommand(deps);

      const exitCode = await cmd.execute();

      expect(exitCode).toBe(4);
    });

    it('should output auth error message', async () => {
      const deps = createMockDeps({
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
        getEmail: jest.fn().mockReturnValue(null),
        getTeam: jest.fn().mockReturnValue(null),
      });
      const cmd2 = new WhoamiCommand(deps2);
      await cmd2.execute();

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
