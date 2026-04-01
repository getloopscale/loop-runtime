import { CancelCommand, CancelDeps } from '../../../src/commands/cancel';
import { ApiError } from '../../../src/api/client';

function createMockDeps(overrides: Partial<CancelDeps> = {}): CancelDeps {
  return {
    getToken: jest.fn().mockReturnValue('test-token'),
    cancelBuild: jest.fn().mockResolvedValue({ status: 'cancelled' }),
    writeTo: jest.fn(),
    writeError: jest.fn(),
    ...overrides,
  };
}

describe('CancelCommand', () => {
  describe('success', () => {
    it('should cancel build and return exit code 0', async () => {
      const deps = createMockDeps();
      const cmd = new CancelCommand(deps);

      const exitCode = await cmd.execute('bld-abc12345');

      expect(exitCode).toBe(0);
      expect(deps.cancelBuild).toHaveBeenCalledWith('bld-abc12345');
      expect(deps.writeTo).toHaveBeenCalledWith('Build bld-abc12345 cancelled.');
    });

    it('should return JSON on success with --json', async () => {
      const deps = createMockDeps();
      const cmd = new CancelCommand(deps);

      const exitCode = await cmd.execute('bld-abc12345', { json: true });

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls[0][0];
      const parsed = JSON.parse(output);
      expect(parsed.ok).toBe(true);
      expect(parsed.build_id).toBe('bld-abc12345');
      expect(parsed.status).toBe('cancelled');
    });
  });

  describe('auth errors', () => {
    it('should return exit code 4 when not authenticated', async () => {
      const deps = createMockDeps({ getToken: jest.fn().mockReturnValue(null) });
      const cmd = new CancelCommand(deps);

      const exitCode = await cmd.execute('bld-abc12345');

      expect(exitCode).toBe(4);
      expect(deps.writeError).toHaveBeenCalledWith('Not authenticated. Run: loopzero login');
      expect(deps.cancelBuild).not.toHaveBeenCalled();
    });

    it('should return JSON error on auth failure with --json', async () => {
      const deps = createMockDeps({ getToken: jest.fn().mockReturnValue(null) });
      const cmd = new CancelCommand(deps);

      const exitCode = await cmd.execute('bld-abc12345', { json: true });

      expect(exitCode).toBe(4);
      const output = (deps.writeTo as jest.Mock).mock.calls[0][0];
      const parsed = JSON.parse(output);
      expect(parsed.code).toBe('AUTH_REQUIRED');
      expect(parsed.exit_code).toBe(4);
    });

    it('should return exit code 4 on API 401', async () => {
      const deps = createMockDeps({
        cancelBuild: jest.fn().mockRejectedValue(new ApiError('Unauthorized', 4, 401, 'AUTH_REQUIRED')),
      });
      const cmd = new CancelCommand(deps);

      const exitCode = await cmd.execute('bld-abc12345');

      expect(exitCode).toBe(4);
    });
  });

  describe('build not found', () => {
    it('should return exit code 3 when build not found (404)', async () => {
      const deps = createMockDeps({
        cancelBuild: jest.fn().mockRejectedValue(new ApiError('Build not found', 3, 404, 'BUILD_NOT_FOUND')),
      });
      const cmd = new CancelCommand(deps);

      const exitCode = await cmd.execute('bld-notexist');

      expect(exitCode).toBe(3);
      expect(deps.writeError).toHaveBeenCalledWith('Build bld-notexist not found.');
    });

    it('should return JSON error when build not found with --json', async () => {
      const deps = createMockDeps({
        cancelBuild: jest.fn().mockRejectedValue(new ApiError('Build not found', 3, 404, 'BUILD_NOT_FOUND')),
      });
      const cmd = new CancelCommand(deps);

      const exitCode = await cmd.execute('bld-notexist', { json: true });

      expect(exitCode).toBe(3);
      const output = (deps.writeTo as jest.Mock).mock.calls[0][0];
      const parsed = JSON.parse(output);
      expect(parsed.code).toBe('BUILD_NOT_FOUND');
    });
  });

  describe('build already completed', () => {
    it('should return exit code 3 when build already completed (409)', async () => {
      const deps = createMockDeps({
        cancelBuild: jest.fn().mockRejectedValue(new ApiError('Build already completed', 3, 409, 'BUILD_ALREADY_COMPLETED')),
      });
      const cmd = new CancelCommand(deps);

      const exitCode = await cmd.execute('bld-done123');

      expect(exitCode).toBe(3);
      expect(deps.writeError).toHaveBeenCalledWith(
        'Build bld-done123 has already completed. Nothing to cancel.'
      );
    });
  });

  describe('usage errors', () => {
    it('should return exit code 1 when build ID is empty', async () => {
      const deps = createMockDeps();
      const cmd = new CancelCommand(deps);

      const exitCode = await cmd.execute('');

      expect(exitCode).toBe(1);
      expect(deps.cancelBuild).not.toHaveBeenCalled();
    });

    it('should return exit code 1 when build ID is whitespace', async () => {
      const deps = createMockDeps();
      const cmd = new CancelCommand(deps);

      const exitCode = await cmd.execute('   ');

      expect(exitCode).toBe(1);
    });
  });

  describe('server errors', () => {
    it('should return exit code 3 on API 500 with retry suggestion', async () => {
      const deps = createMockDeps({
        cancelBuild: jest.fn().mockRejectedValue(new ApiError('Internal error', 3, 500, 'INTERNAL_ERROR')),
      });
      const cmd = new CancelCommand(deps);

      const exitCode = await cmd.execute('bld-abc12345');

      expect(exitCode).toBe(3);
      const errorMsg = (deps.writeError as jest.Mock).mock.calls[0][0];
      expect(errorMsg).toContain('Try again');
    });

    it('should handle non-ApiError exceptions', async () => {
      const deps = createMockDeps({
        cancelBuild: jest.fn().mockRejectedValue(new Error('Network timeout')),
      });
      const cmd = new CancelCommand(deps);

      const exitCode = await cmd.execute('bld-abc12345');

      expect(exitCode).toBe(3);
    });
  });

  describe('Rule 3 compliance', () => {
    it('should never contain internal terms in output', async () => {
      const deps = createMockDeps();
      const cmd = new CancelCommand(deps);

      await cmd.execute('bld-abc12345');

      const allOutput = [
        ...(deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]),
        ...(deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]),
      ].join(' ');

      const forbidden = ['ArgoCD', 'argocd', 'Argo Workflow', 'CANCELLING', 'advisory lock', 'Gitea', 'MinIO'];
      for (const term of forbidden) {
        expect(allOutput).not.toContain(term);
      }
    });
  });
});
