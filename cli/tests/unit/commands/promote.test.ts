import { PromoteCommand, PromoteDeps } from '../../../src/commands/promote';
import { ApiError } from '../../../src/api/client';

function createMockDeps(overrides: Partial<PromoteDeps> = {}): PromoteDeps {
  return {
    getToken: jest.fn().mockReturnValue('test-token'),
    findLoopZeroYaml: jest.fn().mockReturnValue({
      path: '/project/loopzero.yaml',
      content: { project: 'my-app', apps: { web: { path: '.', framework: 'express', port: 3000 } } },
    }),
    promote: jest.fn().mockResolvedValue({
      promotion_id: 'promo-abc123',
      status: 'promoting',
      source_release: 'rel-src-001',
      new_release: 'rel-new-001',
      from: 'staging',
      to: 'production',
      url: 'https://my-app-production.dev.loopzero.example',
    }),
    writeTo: jest.fn(),
    writeError: jest.fn(),
    ...overrides,
  };
}

describe('PromoteCommand', () => {
  describe('success', () => {
    it('should promote and return exit code 0', async () => {
      const deps = createMockDeps();
      const cmd = new PromoteCommand(deps);

      const exitCode = await cmd.execute({ from: 'staging', to: 'production' });

      expect(exitCode).toBe(0);
      expect(deps.promote).toHaveBeenCalledWith('my-app', 'staging', 'production');
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('rel-src-001');
      expect(output).toContain('rel-new-001');
      expect(output).toContain('https://my-app-production.dev.loopzero.example');
    });

    it('should default to staging -> production', async () => {
      const deps = createMockDeps();
      const cmd = new PromoteCommand(deps);

      await cmd.execute({});

      expect(deps.promote).toHaveBeenCalledWith('my-app', 'staging', 'production');
    });
  });

  describe('json output', () => {
    it('should return JSON with correct schema', async () => {
      const deps = createMockDeps();
      const cmd = new PromoteCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls[0][0];
      const parsed = JSON.parse(output);
      expect(parsed.promotion_id).toBe('promo-abc123');
      expect(parsed.status).toBe('promoting');
      expect(parsed.source_release).toBe('rel-src-001');
      expect(parsed.new_release).toBe('rel-new-001');
      expect(parsed.from).toBe('staging');
      expect(parsed.to).toBe('production');
      expect(parsed.url).toBe('https://my-app-production.dev.loopzero.example');
      expect(typeof parsed.duration_seconds).toBe('number');
    });
  });

  describe('validation errors', () => {
    it('should return exit code 1 when from == to', async () => {
      const deps = createMockDeps();
      const cmd = new PromoteCommand(deps);

      const exitCode = await cmd.execute({ from: 'staging', to: 'staging' });

      expect(exitCode).toBe(1);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('different');
    });
  });

  describe('auth errors', () => {
    it('should return exit code 4 when not authenticated', async () => {
      const deps = createMockDeps({ getToken: jest.fn().mockReturnValue(null) });
      const cmd = new PromoteCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(4);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('loopzero login');
    });
  });

  describe('missing config', () => {
    it('should return exit code 1 when loopzero.yaml is missing', async () => {
      const deps = createMockDeps({ findLoopZeroYaml: jest.fn().mockReturnValue(null) });
      const cmd = new PromoteCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(1);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('loopzero init');
    });

    it('should return exit code 1 when loopzero.yaml has parse error', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue({
          path: '/project/loopzero.yaml',
          content: {},
          parseError: 'bad yaml',
        }),
      });
      const cmd = new PromoteCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(1);
    });
  });

  describe('API errors', () => {
    it('should return exit code 3 for 409 concurrent operation', async () => {
      const deps = createMockDeps({
        promote: jest.fn().mockRejectedValue(new ApiError('Conflict', 3, 409, 'DEPLOY_IN_PROGRESS')),
      });
      const cmd = new PromoteCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(3);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('Another operation is in progress');
    });

    it('should return exit code 3 for 404 no active release', async () => {
      const deps = createMockDeps({
        promote: jest.fn().mockRejectedValue(new ApiError('No active release', 3, 404, 'NO_ACTIVE_RELEASE')),
      });
      const cmd = new PromoteCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(3);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('No active release');
    });

    it('should return exit code 4 for auth error', async () => {
      const deps = createMockDeps({
        promote: jest.fn().mockRejectedValue(new ApiError('Unauthorized', 4, 401, 'AUTH_REQUIRED')),
      });
      const cmd = new PromoteCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(4);
    });

    it('should return exit code 1 for 400 same environment', async () => {
      const deps = createMockDeps({
        promote: jest.fn().mockRejectedValue(new ApiError('Same environment', 3, 400, 'SAME_ENVIRONMENT')),
      });
      const cmd = new PromoteCommand(deps);

      // Skip client-side validation by using different envs — let server return 400
      const exitCode = await cmd.execute({ from: 'staging', to: 'production' });

      expect(exitCode).toBe(1);
    });
  });

  describe('confirmation', () => {
    it('should skip API call when user declines confirmation', async () => {
      const deps = createMockDeps({
        confirm: jest.fn().mockResolvedValue(false),
      });
      const cmd = new PromoteCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(0);
      expect(deps.promote).not.toHaveBeenCalled();
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('cancelled');
    });

    it('should require --yes in CI mode', async () => {
      const deps = createMockDeps({ isCI: true });
      const cmd = new PromoteCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(1);
      expect(deps.promote).not.toHaveBeenCalled();
    });

    it('should skip confirmation with --yes', async () => {
      const deps = createMockDeps();
      const cmd = new PromoteCommand(deps);

      const exitCode = await cmd.execute({ yes: true });

      expect(exitCode).toBe(0);
      expect(deps.promote).toHaveBeenCalled();
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
      const cmd1 = new PromoteCommand(deps1);
      await cmd1.execute({});

      // Error scenario - not authenticated
      const deps2 = createMockDeps({ getToken: jest.fn().mockReturnValue(null) });
      const cmd2 = new PromoteCommand(deps2);
      await cmd2.execute({});

      // Error scenario - 409
      const deps3 = createMockDeps({
        promote: jest.fn().mockRejectedValue(new ApiError('Conflict', 3, 409, 'DEPLOY_IN_PROGRESS')),
      });
      const cmd3 = new PromoteCommand(deps3);
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
