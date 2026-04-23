// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { LogsCommand, LogsDeps, StreamLogEvent } from '../../../src/commands/logs';
import { ApiError } from '../../../src/api/client';

function createMockDeps(overrides: Partial<LogsDeps> = {}): LogsDeps {
  return {
    getToken: jest.fn().mockReturnValue('test-token'),
    findLoopZeroYaml: jest.fn().mockReturnValue({
      path: '/project/loopzero.yaml',
      content: { project: 'my-app', apps: { web: { path: '.', framework: 'express', port: 3000 } } },
    }),
    getLogs: jest.fn().mockResolvedValue({
      build_id: 'build-abc123',
      status: 'SUCCEEDED',
      entries: [
        { step: 'clone', stream: 'stdout', content: "Cloning into '/workspace'...", timestamp: '2026-03-25T14:30:01Z' },
        { step: 'install-deps', stream: 'stdout', content: 'npm install', timestamp: '2026-03-25T14:30:05Z' },
        { step: 'build', stream: 'stdout', content: 'Successfully built image', timestamp: '2026-03-25T14:30:30Z' },
      ],
      total_lines: 3,
    }),
    writeTo: jest.fn(),
    writeError: jest.fn(),
    ...overrides,
  };
}

describe('LogsCommand', () => {
  describe('success', () => {
    it('should display build logs with step names and return exit code 0', async () => {
      const deps = createMockDeps();
      const cmd = new LogsCommand(deps);

      const exitCode = await cmd.execute({ build: 'build-abc123' });

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('build-abc123');
      expect(output).toContain('SUCCEEDED');
      expect(output).toContain('[clone]');
      expect(output).toContain('[install-deps]');
      expect(output).toContain('[build]');
      expect(output).toContain("Cloning into '/workspace'...");
    });

    it('should pass tail parameter to API', async () => {
      const deps = createMockDeps();
      const cmd = new LogsCommand(deps);

      await cmd.execute({ build: 'build-abc123', tail: 5 });

      expect(deps.getLogs).toHaveBeenCalledWith('build-abc123', { tail: 5, step: undefined });
    });

    it('should pass step parameter to API', async () => {
      const deps = createMockDeps();
      const cmd = new LogsCommand(deps);

      await cmd.execute({ build: 'build-abc123', step: 'clone' });

      expect(deps.getLogs).toHaveBeenCalledWith('build-abc123', { tail: undefined, step: 'clone' });
    });

    it('should show message when no logs available (non-terminal status)', async () => {
      const deps = createMockDeps({
        getLogs: jest.fn().mockResolvedValue({
          build_id: 'build-abc123',
          status: 'RUNNING',
          entries: [],
          total_lines: 0,
        }),
      });
      const cmd = new LogsCommand(deps);

      const exitCode = await cmd.execute({ build: 'build-abc123' });

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('No logs available');
      // non-terminal → "Build may still be starting" hint is honest
      expect(output).toContain('Build may still be starting');
    });

    // CHG-2026-04-23-AUDIT-31: for terminal statuses (SUCCEEDED/FAILED/
    // CANCELLED) the "Build may still be starting" hint is contradictory
    // and misleading. Surface a status-appropriate hint instead.
    it('terminal status (SUCCEEDED) shows pruned/uncaptured hint, not "starting"', async () => {
      const deps = createMockDeps({
        getLogs: jest.fn().mockResolvedValue({
          build_id: 'build-done',
          status: 'SUCCEEDED',
          entries: [],
          total_lines: 0,
        }),
      });
      const cmd = new LogsCommand(deps);

      const exitCode = await cmd.execute({ build: 'build-done' });

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).not.toContain('Build may still be starting');
      expect(output).toMatch(/not captured|pruned/i);
    });

    it('terminal status (FAILED) also avoids the "starting" hint', async () => {
      const deps = createMockDeps({
        getLogs: jest.fn().mockResolvedValue({
          build_id: 'build-failed',
          status: 'FAILED',
          entries: [],
          total_lines: 0,
        }),
      });
      const cmd = new LogsCommand(deps);
      await cmd.execute({ build: 'build-failed' });
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).not.toContain('Build may still be starting');
    });
  });

  describe('json output', () => {
    it('should return JSON with correct schema', async () => {
      const deps = createMockDeps();
      const cmd = new LogsCommand(deps);

      const exitCode = await cmd.execute({ build: 'build-abc123', json: true });

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls[0][0];
      const parsed = JSON.parse(output);
      expect(parsed.build_id).toBe('build-abc123');
      expect(parsed.status).toBe('SUCCEEDED');
      expect(parsed.entries).toHaveLength(3);
      expect(parsed.total_lines).toBe(3);
    });
  });

  describe('missing build ID', () => {
    it('should return exit code 1 when build ID is missing', async () => {
      const deps = createMockDeps();
      const cmd = new LogsCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(1);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('Build ID is required');
    });
  });

  describe('auth errors', () => {
    it('should return exit code 4 when not authenticated', async () => {
      const deps = createMockDeps({
        getToken: jest.fn().mockReturnValue(null),
      });
      const cmd = new LogsCommand(deps);

      const exitCode = await cmd.execute({ build: 'build-abc123' });

      expect(exitCode).toBe(4);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('loopzero login');
    });
  });

  describe('missing config', () => {
    it('should succeed with --build even without loopzero.yaml', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue(null),
      });
      const cmd = new LogsCommand(deps);

      const exitCode = await cmd.execute({ build: 'build-abc123' });

      expect(exitCode).toBe(0);
      expect(deps.getLogs).toHaveBeenCalledWith('build-abc123', expect.anything());
    });

    it('should fail without --build and without loopzero.yaml', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue(null),
      });
      const cmd = new LogsCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(1);
      // F-03: without loopzero.yaml and without --build, we can't infer the build
      const allOutput = [
        ...(deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]),
        ...(deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]),
      ].join('\n');
      expect(allOutput).toMatch(/loopzero\.yaml|--build/);
    });
  });

  describe('API errors', () => {
    it('should handle API 404 error', async () => {
      const deps = createMockDeps({
        getLogs: jest.fn().mockRejectedValue(new ApiError('Build not found', 1, 404, 'BUILD_NOT_FOUND')),
      });
      const cmd = new LogsCommand(deps);

      const exitCode = await cmd.execute({ build: 'build-nonexistent' });

      expect(exitCode).toBe(1);
    });

    it('should handle API auth error', async () => {
      const deps = createMockDeps({
        getLogs: jest.fn().mockRejectedValue(new ApiError('Unauthorized', 4, 401, 'AUTH_REQUIRED')),
      });
      const cmd = new LogsCommand(deps);

      const exitCode = await cmd.execute({ build: 'build-abc123' });

      expect(exitCode).toBe(4);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('loopzero login');
    });
  });

  describe('no internal terms', () => {
    it('should not contain forbidden terms in any output', async () => {
      const forbidden = [
        'ArgoCD', 'argocd', 'GitOps', 'gitops-push', 'CRD',
        'tenant_id', 'advisory lock', 'pg_advisory', 'Gitea',
        'MinIO', 'kubebuilder', 'envtest', 'Kaniko',
        'Loki', 'Promtail', 'LogQL', 'workflow',
      ];

      // Success scenario
      const deps1 = createMockDeps();
      const cmd1 = new LogsCommand(deps1);
      await cmd1.execute({ build: 'build-abc123' });

      // No logs scenario
      const deps2 = createMockDeps({
        getLogs: jest.fn().mockResolvedValue({
          build_id: 'build-abc123',
          status: 'RUNNING',
          entries: [],
          total_lines: 0,
        }),
      });
      const cmd2 = new LogsCommand(deps2);
      await cmd2.execute({ build: 'build-abc123' });

      // Error scenario
      const deps3 = createMockDeps({
        getToken: jest.fn().mockReturnValue(null),
      });
      const cmd3 = new LogsCommand(deps3);
      await cmd3.execute({ build: 'build-abc123' });

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

  describe('--follow mode', () => {
    it('should stream log events and return exit code 0 when stream closes', async () => {
      const lines: string[] = [];
      const deps = createMockDeps({
        streamLogs: (buildID, onLine, _onError, onClose) => {
          // Simulate SSE events
          setTimeout(() => {
            onLine({ timestamp: '2026-01-01T00:00:01Z', line: 'Building...', step: 'build' });
            onLine({ timestamp: '2026-01-01T00:00:02Z', line: 'Done!', step: 'build' });
            onClose();
          }, 10);
          return { close: jest.fn() };
        },
        writeTo: jest.fn((msg: string) => lines.push(msg)),
      });
      const cmd = new LogsCommand(deps);

      const exitCode = await cmd.execute({ build: 'build-abc123', follow: true });

      expect(exitCode).toBe(0);
      expect(lines.some((l: string) => l.includes('Building...'))).toBe(true);
      expect(lines.some((l: string) => l.includes('Done!'))).toBe(true);
    });

    it('should display timestamps and step prefixes in follow mode', async () => {
      const lines: string[] = [];
      const deps = createMockDeps({
        streamLogs: (_buildID, onLine, _onError, onClose) => {
          setTimeout(() => {
            onLine({ timestamp: '2026-01-01T00:00:01Z', line: 'Cloning...', step: 'clone' });
            onClose();
          }, 10);
          return { close: jest.fn() };
        },
        writeTo: jest.fn((msg: string) => lines.push(msg)),
      });
      const cmd = new LogsCommand(deps);

      await cmd.execute({ build: 'build-abc123', follow: true });

      // Should contain timestamp and step prefix
      const output = lines.join('\n');
      expect(output).toContain('2026-01-01T00:00:01Z');
      expect(output).toContain('[clone]');
      expect(output).toContain('Cloning...');
    });

    it('should output JSON events in follow + json mode', async () => {
      const lines: string[] = [];
      const deps = createMockDeps({
        streamLogs: (_buildID, onLine, _onError, onClose) => {
          setTimeout(() => {
            onLine({ timestamp: '2026-01-01T00:00:01Z', line: 'test', step: 'build' });
            onClose();
          }, 10);
          return { close: jest.fn() };
        },
        writeTo: jest.fn((msg: string) => lines.push(msg)),
      });
      const cmd = new LogsCommand(deps);

      await cmd.execute({ build: 'build-abc123', follow: true, json: true });

      const parsed = JSON.parse(lines[0]);
      expect(parsed.timestamp).toBe('2026-01-01T00:00:01Z');
      expect(parsed.line).toBe('test');
      expect(parsed.step).toBe('build');
    });

    it('should return exit code 1 when streamLogs is not available', async () => {
      const deps = createMockDeps({
        streamLogs: undefined,
      });
      const cmd = new LogsCommand(deps);

      const exitCode = await cmd.execute({ build: 'build-abc123', follow: true });

      expect(exitCode).toBe(1);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('not available');
    });

    it('should require auth in follow mode', async () => {
      const deps = createMockDeps({
        getToken: jest.fn().mockReturnValue(null),
      });
      const cmd = new LogsCommand(deps);

      const exitCode = await cmd.execute({ build: 'build-abc123', follow: true });

      expect(exitCode).toBe(4);
    });

    it('should not contain forbidden terms in follow mode output', async () => {
      const forbidden = [
        'ArgoCD', 'argocd', 'GitOps', 'gitops-push', 'CRD',
        'tenant_id', 'advisory lock', 'pg_advisory', 'Gitea',
        'MinIO', 'kubebuilder', 'envtest', 'Kaniko',
        'Loki', 'Promtail', 'LogQL', 'workflow',
      ];
      const lines: string[] = [];
      const deps = createMockDeps({
        streamLogs: (_buildID, onLine, _onError, onClose) => {
          setTimeout(() => {
            onLine({ timestamp: '2026-01-01T00:00:01Z', line: 'Build complete', step: 'build' });
            onClose();
          }, 10);
          return { close: jest.fn() };
        },
        writeTo: jest.fn((msg: string) => lines.push(msg)),
        writeError: jest.fn((msg: string) => lines.push(msg)),
      });
      const cmd = new LogsCommand(deps);

      await cmd.execute({ build: 'build-abc123', follow: true });

      const allOutput = lines.join('\n');
      for (const term of forbidden) {
        expect(allOutput).not.toContain(term);
      }
    });
  });

  // F-03: Infer build ID from project status when --build not provided
  describe('build ID inference (F-03)', () => {
    it('should infer build ID from project status when --build not provided', async () => {
      const deps = createMockDeps({
        getProjectStatus: jest.fn().mockResolvedValue({
          last_build: { id: 'build-inferred-123' },
        }),
      });
      const cmd = new LogsCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(0);
      expect(deps.getLogs).toHaveBeenCalledWith('build-inferred-123', expect.any(Object));
    });

    it('should return NO_BUILDS when project has no builds', async () => {
      const deps = createMockDeps({
        getProjectStatus: jest.fn().mockResolvedValue({ last_build: null }),
      });
      const cmd = new LogsCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(1);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      expect(output).toContain('NO_BUILDS');
    });

    it('should return MISSING_CONFIG when no loopzero.yaml and no --build', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue(null),
        getProjectStatus: jest.fn(),
      });
      const cmd = new LogsCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(1);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      expect(output).toContain('MISSING_CONFIG');
    });
  });
});
