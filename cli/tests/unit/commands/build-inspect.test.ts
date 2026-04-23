import { BuildInspectCommand, BuildInspectDeps } from '../../../src/commands/build-inspect';
import { ApiError } from '../../../src/api/client';

const sampleBuild = {
  build_id: 'bld-abc12345',
  project_id: 'my-project',
  environment: 'production',
  status: 'SUCCEEDED',
  commit_sha: 'f3c9a01abcdef1234567890abcdef1234567890a',
  created_at: '2026-03-31T14:23:01Z',
  finished_at: '2026-03-31T14:23:25Z',
  duration_seconds: 24,
  apps: [
    { name: 'api', framework: 'express', type: 'server' },
    { name: 'web', framework: 'next', type: 'frontend' },
  ],
};

const failedBuild = {
  build_id: 'bld-failed123',
  project_id: 'my-project',
  environment: 'production',
  status: 'FAILED',
  commit_sha: 'b2a8c12abcdef1234567890abcdef1234567890b',
  created_at: '2026-03-31T13:50:44Z',
  finished_at: '2026-03-31T13:51:02Z',
  duration_seconds: 18,
  apps: [{ name: 'api', framework: 'express', type: 'server' }],
  error: 'Build step failed: npm install returned exit code 1',
};

function createMockDeps(overrides: Partial<BuildInspectDeps> = {}): BuildInspectDeps {
  return {
    getToken: jest.fn().mockReturnValue('test-token'),
    getBuild: jest.fn().mockResolvedValue(sampleBuild),
    writeTo: jest.fn(),
    writeError: jest.fn(),
    ...overrides,
  };
}

describe('BuildInspectCommand', () => {
  describe('success', () => {
    it('should display build details and return exit code 0', async () => {
      const deps = createMockDeps();
      const cmd = new BuildInspectCommand(deps);

      const exitCode = await cmd.execute('bld-abc12345');

      expect(exitCode).toBe(0);
      expect(deps.getBuild).toHaveBeenCalledWith('bld-abc12345');
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('bld-abc12345');
      expect(output).toContain('SUCCEEDED');
      expect(output).toContain('my-project');
      expect(output).toContain('production');
      expect(output).toContain('f3c9a01');
      expect(output).toContain('24s');
      // CHG-2026-04-23-AUDIT-34: regression guard. The human render
      // reads build.project_id (not build.project). A field name
      // drift makes it undefined and renders "Project: undefined".
      expect(output).not.toContain('undefined');
    });

    it('should show apps list', async () => {
      const deps = createMockDeps();
      const cmd = new BuildInspectCommand(deps);

      await cmd.execute('bld-abc12345');

      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('api');
      expect(output).toContain('web');
    });

    it('should suggest viewing logs', async () => {
      const deps = createMockDeps();
      const cmd = new BuildInspectCommand(deps);

      await cmd.execute('bld-abc12345');

      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('loopzero logs --build bld-abc12345');
    });
  });

  describe('failed build', () => {
    it('should show error field for failed builds', async () => {
      const deps = createMockDeps({
        getBuild: jest.fn().mockResolvedValue(failedBuild),
      });
      const cmd = new BuildInspectCommand(deps);

      const exitCode = await cmd.execute('bld-failed123');

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('FAILED');
      expect(output).toContain('npm install returned exit code 1');
    });
  });

  describe('--json output', () => {
    it('should return full build detail as JSON', async () => {
      const deps = createMockDeps();
      const cmd = new BuildInspectCommand(deps);

      const exitCode = await cmd.execute('bld-abc12345', { json: true });

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls[0][0];
      const parsed = JSON.parse(output);
      expect(parsed.build_id).toBe('bld-abc12345');
      // CHG-2026-04-23-AUDIT-34: API uses project_id.
      expect(parsed.project_id).toBe('my-project');
      expect(parsed.status).toBe('SUCCEEDED');
      expect(parsed.apps).toHaveLength(2);
    });

    it('should include error in JSON for failed builds', async () => {
      const deps = createMockDeps({
        getBuild: jest.fn().mockResolvedValue(failedBuild),
      });
      const cmd = new BuildInspectCommand(deps);

      const exitCode = await cmd.execute('bld-failed123', { json: true });

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls[0][0];
      const parsed = JSON.parse(output);
      expect(parsed.error).toContain('npm install');
    });
  });

  describe('not found', () => {
    it('should return exit code 1 when build not found', async () => {
      const deps = createMockDeps({
        getBuild: jest.fn().mockRejectedValue(new ApiError('Build not found', 1, 404, 'BUILD_NOT_FOUND')),
      });
      const cmd = new BuildInspectCommand(deps);

      const exitCode = await cmd.execute('bld-notexist');

      expect(exitCode).toBe(1);
      expect(deps.writeError).toHaveBeenCalledWith('Build bld-notexist not found.');
    });

    it('should return JSON error when build not found with --json', async () => {
      const deps = createMockDeps({
        getBuild: jest.fn().mockRejectedValue(new ApiError('Build not found', 1, 404, 'BUILD_NOT_FOUND')),
      });
      const cmd = new BuildInspectCommand(deps);

      const exitCode = await cmd.execute('bld-notexist', { json: true });

      expect(exitCode).toBe(1);
      const output = (deps.writeTo as jest.Mock).mock.calls[0][0];
      const parsed = JSON.parse(output);
      expect(parsed.code).toBe('BUILD_NOT_FOUND');
    });
  });

  describe('auth errors', () => {
    it('should return exit code 4 when not authenticated', async () => {
      const deps = createMockDeps({ getToken: jest.fn().mockReturnValue(null) });
      const cmd = new BuildInspectCommand(deps);

      const exitCode = await cmd.execute('bld-abc12345');

      expect(exitCode).toBe(4);
      expect(deps.writeError).toHaveBeenCalledWith('Not authenticated. Run: loopzero login');
      expect(deps.getBuild).not.toHaveBeenCalled();
    });

    it('should return JSON error on auth failure with --json', async () => {
      const deps = createMockDeps({ getToken: jest.fn().mockReturnValue(null) });
      const cmd = new BuildInspectCommand(deps);

      const exitCode = await cmd.execute('bld-abc12345', { json: true });

      expect(exitCode).toBe(4);
      const output = (deps.writeTo as jest.Mock).mock.calls[0][0];
      const parsed = JSON.parse(output);
      expect(parsed.code).toBe('AUTH_REQUIRED');
      expect(parsed.exit_code).toBe(4);
    });

    it('should return exit code 4 on API 401', async () => {
      const deps = createMockDeps({
        getBuild: jest.fn().mockRejectedValue(new ApiError('Unauthorized', 4, 401, 'AUTH_REQUIRED')),
      });
      const cmd = new BuildInspectCommand(deps);

      const exitCode = await cmd.execute('bld-abc12345');

      expect(exitCode).toBe(4);
    });
  });

  describe('missing build-id', () => {
    it('should return exit code 1 when build-id is empty', async () => {
      const deps = createMockDeps();
      const cmd = new BuildInspectCommand(deps);

      const exitCode = await cmd.execute('');

      expect(exitCode).toBe(1);
      expect(deps.getBuild).not.toHaveBeenCalled();
    });

    it('should return exit code 1 when build-id is whitespace', async () => {
      const deps = createMockDeps();
      const cmd = new BuildInspectCommand(deps);

      const exitCode = await cmd.execute('   ');

      expect(exitCode).toBe(1);
    });
  });

  describe('server errors', () => {
    it('should return exit code 1 on API 500', async () => {
      const deps = createMockDeps({
        getBuild: jest.fn().mockRejectedValue(new ApiError('Internal error', 3, 500, 'INTERNAL_ERROR')),
      });
      const cmd = new BuildInspectCommand(deps);

      const exitCode = await cmd.execute('bld-abc12345');

      expect(exitCode).toBe(1);
      const errorMsg = (deps.writeError as jest.Mock).mock.calls[0][0];
      expect(errorMsg).toContain('Try again');
    });

    it('should handle non-ApiError exceptions', async () => {
      const deps = createMockDeps({
        getBuild: jest.fn().mockRejectedValue(new Error('Network timeout')),
      });
      const cmd = new BuildInspectCommand(deps);

      const exitCode = await cmd.execute('bld-abc12345');

      expect(exitCode).toBe(1);
    });
  });

  describe('Rule 3 compliance', () => {
    it('should never contain internal terms in output', async () => {
      const deps = createMockDeps();
      const cmd = new BuildInspectCommand(deps);

      await cmd.execute('bld-abc12345');

      const allOutput = [
        ...(deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]),
        ...(deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]),
      ].join(' ');

      const forbidden = ['ArgoCD', 'argocd', 'Argo Workflow', 'advisory lock', 'Gitea', 'MinIO', 'CRD', 'tenant_id'];
      for (const term of forbidden) {
        expect(allOutput).not.toContain(term);
      }
    });
  });
});
