import { ScaleCommand, ScaleDeps } from '../../../src/commands/scale';
import { ApiError } from '../../../src/api/client';

function createMockDeps(overrides: Partial<ScaleDeps> = {}): ScaleDeps {
  return {
    getToken: jest.fn().mockReturnValue('test-token'),
    findLoopZeroYaml: jest.fn().mockReturnValue({
      path: '/project/loopzero.yaml',
      content: { project: 'my-app', apps: { api: { path: '.', framework: 'express', port: 3000 } } },
    }),
    scale: jest.fn().mockResolvedValue({
      app: 'api',
      replicas: 3,
      status: 'applied',
    }),
    writeTo: jest.fn(),
    writeError: jest.fn(),
    ...overrides,
  };
}

describe('ScaleCommand', () => {
  describe('fixed replicas', () => {
    it('should scale to fixed replicas and return exit code 0', async () => {
      const deps = createMockDeps();
      const cmd = new ScaleCommand(deps);

      const exitCode = await cmd.execute({ app: 'api', replicas: 3 });

      expect(exitCode).toBe(0);
      expect(deps.scale).toHaveBeenCalledWith('my-app', 'production', 'api', { replicas: 3 });
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('Replicas: 3');
      expect(output).toContain('loopzero deploy');
    });

    it('should use specified environment', async () => {
      const deps = createMockDeps();
      const cmd = new ScaleCommand(deps);

      await cmd.execute({ app: 'api', replicas: 2, env: 'staging' });

      expect(deps.scale).toHaveBeenCalledWith('my-app', 'staging', 'api', { replicas: 2 });
    });
  });

  describe('autoscaling', () => {
    it('should configure autoscaling and return exit code 0', async () => {
      const deps = createMockDeps({
        scale: jest.fn().mockResolvedValue({
          app: 'api',
          autoscaling: { min: 2, max: 10, cpu_target: 80 },
          status: 'applied',
        }),
      });
      const cmd = new ScaleCommand(deps);

      const exitCode = await cmd.execute({ app: 'api', min: 2, max: 10, cpu: 80 });

      expect(exitCode).toBe(0);
      expect(deps.scale).toHaveBeenCalledWith('my-app', 'production', 'api', { min: 2, max: 10, cpu: 80 });
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('2-10 replicas');
      expect(output).toContain('80%');
    });
  });

  describe('json output', () => {
    it('should return JSON with correct schema for fixed replicas', async () => {
      const deps = createMockDeps();
      const cmd = new ScaleCommand(deps);

      const exitCode = await cmd.execute({ app: 'api', replicas: 3, json: true });

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls[0][0];
      const parsed = JSON.parse(output);
      expect(parsed.app).toBe('api');
      expect(parsed.environment).toBe('production');
      expect(parsed.replicas).toBe(3);
      expect(parsed.status).toBe('applied');
    });

    it('should return JSON with autoscaling config', async () => {
      const deps = createMockDeps({
        scale: jest.fn().mockResolvedValue({
          app: 'api',
          autoscaling: { min: 2, max: 10, cpu_target: 80 },
          status: 'applied',
        }),
      });
      const cmd = new ScaleCommand(deps);

      const exitCode = await cmd.execute({ app: 'api', min: 2, max: 10, cpu: 80, json: true });

      expect(exitCode).toBe(0);
      const parsed = JSON.parse((deps.writeTo as jest.Mock).mock.calls[0][0]);
      expect(parsed.autoscaling.min).toBe(2);
      expect(parsed.autoscaling.max).toBe(10);
      expect(parsed.autoscaling.cpu_target).toBe(80);
    });
  });

  describe('validation errors', () => {
    it('should return exit code 1 when --app is missing', async () => {
      const deps = createMockDeps();
      const cmd = new ScaleCommand(deps);

      const exitCode = await cmd.execute({ app: '' });

      expect(exitCode).toBe(1);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('App name is required');
    });

    it('should return exit code 1 when no scaling parameters provided', async () => {
      const deps = createMockDeps();
      const cmd = new ScaleCommand(deps);

      const exitCode = await cmd.execute({ app: 'api' });

      expect(exitCode).toBe(1);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('Specify scaling config');
    });

    it('should return exit code 1 when mixing fixed and autoscaling', async () => {
      const deps = createMockDeps();
      const cmd = new ScaleCommand(deps);

      const exitCode = await cmd.execute({ app: 'api', replicas: 3, min: 2 });

      expect(exitCode).toBe(1);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('Cannot mix');
    });

    it('should return exit code 1 when replicas out of range', async () => {
      const deps = createMockDeps();
      const cmd = new ScaleCommand(deps);

      const exitCode = await cmd.execute({ app: 'api', replicas: 101 });

      expect(exitCode).toBe(1);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('between 0 and 100');
    });

    it('should return exit code 1 when min > max', async () => {
      const deps = createMockDeps();
      const cmd = new ScaleCommand(deps);

      const exitCode = await cmd.execute({ app: 'api', min: 10, max: 2 });

      expect(exitCode).toBe(1);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('cannot exceed maximum');
    });

    it('should return exit code 1 when CPU target out of range', async () => {
      const deps = createMockDeps();
      const cmd = new ScaleCommand(deps);

      const exitCode = await cmd.execute({ app: 'api', min: 1, max: 10, cpu: 150 });

      expect(exitCode).toBe(1);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('between 1 and 100');
    });
  });

  describe('auth errors', () => {
    it('should return exit code 4 when not authenticated', async () => {
      const deps = createMockDeps({ getToken: jest.fn().mockReturnValue(null) });
      const cmd = new ScaleCommand(deps);

      const exitCode = await cmd.execute({ app: 'api', replicas: 3 });

      expect(exitCode).toBe(4);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('loopzero login');
    });
  });

  describe('missing config', () => {
    it('should return exit code 1 when loopzero.yaml is missing', async () => {
      const deps = createMockDeps({ findLoopZeroYaml: jest.fn().mockReturnValue(null) });
      const cmd = new ScaleCommand(deps);

      const exitCode = await cmd.execute({ app: 'api', replicas: 3 });

      expect(exitCode).toBe(1);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('loopzero init');
    });
  });

  describe('API errors', () => {
    it('should return exit code 3 for 409 concurrent operation', async () => {
      const deps = createMockDeps({
        scale: jest.fn().mockRejectedValue(new ApiError('Conflict', 3, 409, 'DEPLOY_IN_PROGRESS')),
      });
      const cmd = new ScaleCommand(deps);

      const exitCode = await cmd.execute({ app: 'api', replicas: 3 });

      expect(exitCode).toBe(3);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('Another operation is in progress');
    });

    it('should return exit code 4 for auth failure from API', async () => {
      const deps = createMockDeps({
        scale: jest.fn().mockRejectedValue(new ApiError('Unauthorized', 4, 401, 'AUTH_REQUIRED')),
      });
      const cmd = new ScaleCommand(deps);

      const exitCode = await cmd.execute({ app: 'api', replicas: 3 });

      expect(exitCode).toBe(4);
    });

    it('should return exit code 3 for unexpected errors', async () => {
      const deps = createMockDeps({
        scale: jest.fn().mockRejectedValue(new Error('network error')),
      });
      const cmd = new ScaleCommand(deps);

      const exitCode = await cmd.execute({ app: 'api', replicas: 3 });

      expect(exitCode).toBe(3);
    });
  });

  // CHG-2026-04-23-AUDIT-27: the `/scale` backend endpoint is not yet
  // implemented. A generic 404 with code=NOT_FOUND was confusing users
  // into thinking they'd typed the wrong URL. We surface a dedicated
  // SCALE_NOT_AVAILABLE code + "Scaling is not yet available" message.
  // Resource-404 (PROJECT_NOT_FOUND etc.) still passes through.
  describe('Feature unavailable 404 (AUDIT-27)', () => {
    it('generic NOT_FOUND from router maps to SCALE_NOT_AVAILABLE', async () => {
      const deps = createMockDeps({
        scale: jest.fn().mockRejectedValue(
          new ApiError('The requested endpoint does not exist.', 3, 404, 'NOT_FOUND'),
        ),
      });
      const cmd = new ScaleCommand(deps);
      const exitCode = await cmd.execute({ app: 'api', replicas: 3, json: true });
      expect(exitCode).toBe(3);
      const parsed = JSON.parse((deps.writeTo as jest.Mock).mock.calls[0][0]);
      expect(parsed.code).toBe('SCALE_NOT_AVAILABLE');
      expect(parsed.error).toMatch(/Scaling is not yet available/i);
    });

    it('resource-specific 404 (PROJECT_NOT_FOUND) is preserved unchanged', async () => {
      const deps = createMockDeps({
        scale: jest.fn().mockRejectedValue(
          new ApiError("project 'web' not found", 3, 404, 'PROJECT_NOT_FOUND'),
        ),
      });
      const cmd = new ScaleCommand(deps);
      const exitCode = await cmd.execute({ app: 'api', replicas: 3, json: true });
      expect(exitCode).toBe(3);
      const parsed = JSON.parse((deps.writeTo as jest.Mock).mock.calls[0][0]);
      expect(parsed.code).toBe('PROJECT_NOT_FOUND');
      expect(parsed.error).toContain("project 'web' not found");
    });
  });

  // CHG-2026-04-23-AUDIT-25: network errors now carry code=API_UNREACHABLE
  // so CI scripts can match a single predicate across all commands.
  describe('API unreachable (AUDIT-25)', () => {
    it('network error produces API_UNREACHABLE code + Check LOOPZERO_URL hint', async () => {
      const deps = createMockDeps({
        scale: jest.fn().mockRejectedValue(new Error('fetch failed')),
      });
      const cmd = new ScaleCommand(deps);

      const exitCode = await cmd.execute({ app: 'api', replicas: 3, json: true });

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
      ];

      // Success
      const deps1 = createMockDeps();
      const cmd1 = new ScaleCommand(deps1);
      await cmd1.execute({ app: 'api', replicas: 3 });

      // Auth error
      const deps2 = createMockDeps({ getToken: jest.fn().mockReturnValue(null) });
      const cmd2 = new ScaleCommand(deps2);
      await cmd2.execute({ app: 'api', replicas: 3 });

      // API error
      const deps3 = createMockDeps({
        scale: jest.fn().mockRejectedValue(new ApiError('Conflict', 3, 409, 'DEPLOY_IN_PROGRESS')),
      });
      const cmd3 = new ScaleCommand(deps3);
      await cmd3.execute({ app: 'api', replicas: 3 });

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
