import { RollbackCommand, RollbackDeps } from '../../../src/commands/rollback';
import { ApiError } from '../../../src/api/client';

function createMockDeps(overrides: Partial<RollbackDeps> = {}): RollbackDeps {
  return {
    getToken: jest.fn().mockReturnValue('test-token'),
    findLoopZeroYaml: jest.fn().mockReturnValue({
      path: '/project/loopzero.yaml',
      content: { project: 'my-app', apps: { web: { path: '.', framework: 'express', port: 3000 } } },
    }),
    rollback: jest.fn().mockResolvedValue({
      status: 'completed',
      rolled_back_release: 'rel-def456',
      reactivated_release: 'rel-abc123',
      environment: 'production',
      url: 'https://my-app-production.dev.loopzero.example',
    }),
    writeTo: jest.fn(),
    writeError: jest.fn(),
    ...overrides,
  };
}

describe('RollbackCommand', () => {
  describe('success', () => {
    it('should rollback and return exit code 0 with release IDs', async () => {
      const deps = createMockDeps();
      const cmd = new RollbackCommand(deps);

      const exitCode = await cmd.execute({ env: 'production' });

      expect(exitCode).toBe(0);
      expect(deps.rollback).toHaveBeenCalledWith('my-app', 'production', undefined);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('rel-def456');
      expect(output).toContain('rel-abc123');
      expect(output).toContain('https://my-app-production.dev.loopzero.example');
    });

    it('should pass --to release ID to API', async () => {
      const deps = createMockDeps();
      const cmd = new RollbackCommand(deps);

      await cmd.execute({ env: 'production', to: 'rel-specific' });

      expect(deps.rollback).toHaveBeenCalledWith('my-app', 'production', 'rel-specific');
    });

    it('should default to production environment', async () => {
      const deps = createMockDeps();
      const cmd = new RollbackCommand(deps);

      await cmd.execute({});

      expect(deps.rollback).toHaveBeenCalledWith('my-app', 'production', undefined);
    });
  });

  describe('migration warning', () => {
    it('should display migration warning before rollback', async () => {
      const deps = createMockDeps();
      const cmd = new RollbackCommand(deps);

      await cmd.execute({ env: 'production' });

      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('Warning: Rollback does not revert database migrations.');
      expect(output).toContain('schema changes');
    });

    it('should show warning even with --yes', async () => {
      const deps = createMockDeps();
      const cmd = new RollbackCommand(deps);

      await cmd.execute({ env: 'production', yes: true });

      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('Warning: Rollback does not revert database migrations.');
    });
  });

  describe('json output', () => {
    it('should return JSON with correct schema', async () => {
      const deps = createMockDeps();
      const cmd = new RollbackCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls[0][0];
      const parsed = JSON.parse(output);
      expect(parsed.status).toBe('completed');
      expect(parsed.rolled_back_release).toBe('rel-def456');
      expect(parsed.active_release).toBe('rel-abc123');
      expect(parsed.environment).toBe('production');
      expect(parsed.url).toBe('https://my-app-production.dev.loopzero.example');
      expect(typeof parsed.duration_seconds).toBe('number');
    });
  });

  describe('auth errors', () => {
    it('should return exit code 4 when not authenticated', async () => {
      const deps = createMockDeps({ getToken: jest.fn().mockReturnValue(null) });
      const cmd = new RollbackCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(4);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('loopzero login');
    });
  });

  describe('missing config', () => {
    it('should return exit code 1 when loopzero.yaml is missing', async () => {
      const deps = createMockDeps({ findLoopZeroYaml: jest.fn().mockReturnValue(null) });
      const cmd = new RollbackCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(1);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('loopzero init');
    });
  });

  describe('API errors', () => {
    it('should return exit code 3 for 409 concurrent operation', async () => {
      const deps = createMockDeps({
        rollback: jest.fn().mockRejectedValue(new ApiError('Conflict', 3, 409, 'DEPLOY_IN_PROGRESS')),
      });
      const cmd = new RollbackCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(3);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('Another operation is in progress');
    });

    // T5.1 CV-M-1 of plan 2026-06-01-p3-infra-observability-test-debt:
    // 409 with code CONCURRENT_ACTIVATION (partial unique index rejected
    // a second ACTIVE release) MUST surface a specific message + code
    // distinct from the generic DEPLOY_IN_PROGRESS collapse.
    it('should surface CONCURRENT_ACTIVATION as distinct from generic 409', async () => {
      const deps = createMockDeps({
        rollback: jest.fn().mockRejectedValue(
          new ApiError('Conflict', 3, 409, 'CONCURRENT_ACTIVATION'),
        ),
      });
      const cmd = new RollbackCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(3);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('previous release activation is still in progress');
      expect(errorOutput).not.toContain('Another operation is in progress');
    });

    it('should return exit code 3 for 404 no previous release', async () => {
      const deps = createMockDeps({
        rollback: jest.fn().mockRejectedValue(new ApiError('No previous release', 3, 404, 'NO_PREVIOUS_RELEASE')),
      });
      const cmd = new RollbackCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(3);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('No previous release');
    });

    it('should return exit code 4 for auth error', async () => {
      const deps = createMockDeps({
        rollback: jest.fn().mockRejectedValue(new ApiError('Unauthorized', 4, 401, 'AUTH_REQUIRED')),
      });
      const cmd = new RollbackCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(4);
    });
  });

  // CHG-2026-04-23-AUDIT-25: network errors now carry code=API_UNREACHABLE.
  describe('API unreachable (AUDIT-25)', () => {
    it('network error produces API_UNREACHABLE code + Check LOOPZERO_URL hint', async () => {
      const deps = createMockDeps({
        rollback: jest.fn().mockRejectedValue(new Error('fetch failed')),
      });
      const cmd = new RollbackCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(3);
      const parsed = JSON.parse((deps.writeTo as jest.Mock).mock.calls[0][0]);
      expect(parsed.code).toBe('API_UNREACHABLE');
      expect(parsed.error).toContain('Check LOOPZERO_URL');
    });
  });

  describe('no internal terms', () => {
    it('should not contain forbidden terms in any output', async () => {
      const forbidden = [
        'ArgoCD', 'argocd', 'GitOps', 'gitops-push', 'CRD',
        'tenant_id', 'advisory lock', 'pg_advisory', 'Gitea',
        'MinIO', 'kubebuilder', 'envtest', 'Kaniko',
        'Loki', 'Promtail', 'LogQL',
      ];

      // Success scenario
      const deps1 = createMockDeps();
      const cmd1 = new RollbackCommand(deps1);
      await cmd1.execute({});

      // Error scenario - not authenticated
      const deps2 = createMockDeps({ getToken: jest.fn().mockReturnValue(null) });
      const cmd2 = new RollbackCommand(deps2);
      await cmd2.execute({});

      // Error scenario - 409
      const deps3 = createMockDeps({
        rollback: jest.fn().mockRejectedValue(new ApiError('Conflict', 3, 409, 'DEPLOY_IN_PROGRESS')),
      });
      const cmd3 = new RollbackCommand(deps3);
      await cmd3.execute({});

      for (const deps of [deps1, deps2, deps3]) {
        const allOutput = [
          ...(deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]),
          ...(deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]),
        ].join('\n');

        for (const term of forbidden) {
          expect(allOutput).not.toContain(term);
        }
      }
    });
  });
});
