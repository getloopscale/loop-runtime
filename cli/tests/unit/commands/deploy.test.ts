import { DeployCommand, DeployDeps } from '../../../src/commands/deploy';
import { ApiError } from '../../../src/api/client';

interface SSEEvent {
  type: string;
  data: Record<string, unknown>;
}

function createMockDeps(overrides: Partial<DeployDeps> = {}): DeployDeps {
  return {
    getToken: jest.fn().mockReturnValue('ghp_test123'),
    hasUncommittedChanges: jest.fn().mockReturnValue(false),
    findLoopZeroYaml: jest.fn().mockReturnValue({
      path: '/project/loopzero.yaml',
      content: {
        project: 'my-saas',
        apps: {
          api: { path: 'apps/api', framework: 'express', port: 3000 },
          web: { path: 'apps/web', framework: 'next', port: 3001 },
        },
      },
    }),
    hasGitRepo: jest.fn().mockReturnValue(true),
    hasLockfile: jest.fn().mockReturnValue(true),
    getHeadCommit: jest.fn().mockReturnValue('abc1234'),
    createGitBundle: jest.fn().mockResolvedValue('/tmp/bundle.bundle'),
    uploadSource: jest.fn().mockResolvedValue({ commit_sha: 'abc1234', apps: ['api', 'web'] }),
    submitBuild: jest.fn().mockResolvedValue({
      build_id: 'build-deadbeef',
      deploy_id: 'd-8f3a',
      status: 'queued',
    }),
    connectSSE: jest.fn().mockImplementation(
      (_buildId: string, onEvent: (event: SSEEvent) => void) => {
        onEvent({
          type: 'deploy.completed',
          data: {
            deploy_id: 'd-8f3a',
            status: 'success',
            duration_seconds: 97,
            apps: [
              { name: 'api', status: 'deployed', url: 'https://api.example.com', build_duration_seconds: 45 },
              { name: 'web', status: 'deployed', url: 'https://web.example.com', build_duration_seconds: 52 },
            ],
          },
        });
        return { close: jest.fn() };
      }
    ),
    getSecrets: jest.fn().mockResolvedValue([]),
    cleanupFile: jest.fn(),
    generateIdempotencyKey: jest.fn().mockReturnValue('uuid-key-1234'),
    writeTo: jest.fn(),
    writeError: jest.fn(),
    isCI: false,
    ...overrides,
  };
}

describe('DeployCommand', () => {
  describe('happy path', () => {
    it('should submit build, stream events, display URLs, and exit 0', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ env: 'production', wait: true });

      expect(deps.getToken).toHaveBeenCalled();
      expect(deps.findLoopZeroYaml).toHaveBeenCalled();
      expect(deps.hasGitRepo).toHaveBeenCalled();
      expect(deps.getHeadCommit).toHaveBeenCalled();
      expect(deps.createGitBundle).toHaveBeenCalled();
      expect(deps.uploadSource).toHaveBeenCalled();
      expect(deps.submitBuild).toHaveBeenCalled();
      expect(deps.connectSSE).toHaveBeenCalled();
      expect(exitCode).toBe(0);

      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('api');
      expect(output).toContain('web');
      expect(output).toContain('https://api.example.com');
      expect(output).toContain('https://web.example.com');
    });

    it('should use default environment production', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      await cmd.execute({});

      expect(deps.submitBuild).toHaveBeenCalledWith(
        expect.objectContaining({ environment: 'production' })
      );
    });

    it('should send idempotency key and commitSha with build submission', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      await cmd.execute({});

      expect(deps.submitBuild).toHaveBeenCalledWith(
        expect.objectContaining({ idempotencyKey: 'uuid-key-1234', commitSha: 'abc1234' })
      );
    });

    it('should cleanup temp bundle file after upload', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      await cmd.execute({});

      expect(deps.cleanupFile).toHaveBeenCalledWith('/tmp/bundle.bundle');
    });

    it('should display worker apps without URL as deployed', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue({
          path: '/project/loopzero.yaml',
          content: {
            project: 'my-saas',
            apps: {
              api: { path: 'apps/api', framework: 'express', port: 3000 },
              worker: { path: 'apps/worker', framework: 'express', port: null, type: 'worker' },
            },
          },
        }),
        connectSSE: jest.fn().mockImplementation(
          (_buildId: string, onEvent: (event: SSEEvent) => void) => {
            onEvent({
              type: 'deploy.completed',
              data: {
                deploy_id: 'd-8f3a',
                status: 'success',
                duration_seconds: 97,
                apps: [
                  { name: 'api', status: 'deployed', url: 'https://api.example.com', build_duration_seconds: 45 },
                  { name: 'worker', status: 'deployed', build_duration_seconds: 30 },
                ],
              },
            });
            return { close: jest.fn() };
          }
        ),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ env: 'production', wait: true });

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('api');
      expect(output).toContain('https://api.example.com');
      expect(output).toContain('worker');
    });
  });

  describe('build failure', () => {
    it('should exit 2 on build failure', async () => {
      const deps = createMockDeps({
        connectSSE: jest.fn().mockImplementation(
          (_buildId: string, onEvent: (event: SSEEvent) => void) => {
            onEvent({
              type: 'deploy.completed',
              data: {
                deploy_id: 'd-1234',
                status: 'failed',
                duration_seconds: 30,
                apps: [
                  { name: 'api', status: 'failed', error: 'Build failed: compilation error', build_duration_seconds: 30 },
                ],
              },
            });
            return { close: jest.fn() };
          }
        ),
        findLoopZeroYaml: jest.fn().mockReturnValue({
          path: '/project/loopzero.yaml',
          content: {
            project: 'my-saas',
            apps: { api: { path: '.', framework: 'express', port: 3000 } },
          },
        }),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(2);
    });
  });

  describe('deploy failure (409)', () => {
    it('should exit 3 on deploy conflict (409)', async () => {
      const deps = createMockDeps({
        submitBuild: jest.fn().mockRejectedValue(
          Object.assign(new Error('A deploy is already in progress for this environment. Wait for it to complete or cancel it.'), {
            exitCode: 3,
            statusCode: 409,
          })
        ),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(3);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('already in progress');
    });
  });

  describe('auth error', () => {
    it('should exit 4 when no token', async () => {
      const deps = createMockDeps({
        getToken: jest.fn().mockReturnValue(null),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(4);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('Not authenticated');
      expect(errOutput).toContain('loopzero login');
    });

    it('should not attempt any operations after auth failure', async () => {
      const deps = createMockDeps({
        getToken: jest.fn().mockReturnValue(null),
      });
      const cmd = new DeployCommand(deps);

      await cmd.execute({});

      expect(deps.findLoopZeroYaml).not.toHaveBeenCalled();
      expect(deps.hasGitRepo).not.toHaveBeenCalled();
      expect(deps.createGitBundle).not.toHaveBeenCalled();
    });

    it('should exit 4 when token is empty string', async () => {
      const deps = createMockDeps({
        getToken: jest.fn().mockReturnValue(''),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(4);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('Not authenticated');
      expect(errOutput).toContain('loopzero login');
      expect(deps.findLoopZeroYaml).not.toHaveBeenCalled();
      expect(deps.hasGitRepo).not.toHaveBeenCalled();
    });

    it('should exit 4 when token is undefined', async () => {
      const deps = createMockDeps({
        getToken: jest.fn().mockReturnValue(undefined),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(4);
      expect(deps.findLoopZeroYaml).not.toHaveBeenCalled();
    });
  });

  describe('partial failure (2+ apps)', () => {
    it('should exit 5 when 2+ apps and at least one fails', async () => {
      const deps = createMockDeps({
        connectSSE: jest.fn().mockImplementation(
          (_buildId: string, onEvent: (event: SSEEvent) => void) => {
            onEvent({
              type: 'deploy.completed',
              data: {
                deploy_id: 'd-abcd',
                status: 'partial_failure',
                duration_seconds: 60,
                apps: [
                  { name: 'api', status: 'deployed', url: 'https://api.example.com', build_duration_seconds: 30 },
                  { name: 'web', status: 'failed', error: 'Build failed', build_duration_seconds: 30 },
                ],
              },
            });
            return { close: jest.fn() };
          }
        ),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(5);
    });
  });

  describe('single app failure', () => {
    it('should exit 2 (NOT 5) when single app fails (DA-3)', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue({
          path: '/project/loopzero.yaml',
          content: {
            project: 'my-saas',
            apps: { api: { path: '.', framework: 'express', port: 3000 } },
          },
        }),
        connectSSE: jest.fn().mockImplementation(
          (_buildId: string, onEvent: (event: SSEEvent) => void) => {
            onEvent({
              type: 'deploy.completed',
              data: {
                deploy_id: 'd-1111',
                status: 'failed',
                duration_seconds: 30,
                apps: [
                  { name: 'api', status: 'failed', error: 'Build error', build_duration_seconds: 30 },
                ],
              },
            });
            return { close: jest.fn() };
          }
        ),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(2);
    });
  });

  describe('--json output', () => {
    it('should output JSON matching DA-5 schema with deploy_id format d-{4hex}', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed).toHaveProperty('deploy_id');
      expect(parsed.deploy_id).toMatch(/^d-[0-9a-f]{4}$/);
      expect(parsed).toHaveProperty('status');
      expect(parsed).toHaveProperty('duration_seconds');
      expect(parsed).toHaveProperty('apps');
      expect(Array.isArray(parsed.apps)).toBe(true);
      expect(parsed.apps[0]).toHaveProperty('name');
      expect(parsed.apps[0]).toHaveProperty('status');
      expect(parsed.apps[0]).toHaveProperty('url');
      expect(parsed.apps[0]).toHaveProperty('build_duration_seconds');
    });

    it('should include error field for failed apps in JSON output', async () => {
      const deps = createMockDeps({
        connectSSE: jest.fn().mockImplementation(
          (_buildId: string, onEvent: (event: SSEEvent) => void) => {
            onEvent({
              type: 'deploy.completed',
              data: {
                deploy_id: 'd-abcd',
                status: 'partial_failure',
                duration_seconds: 60,
                apps: [
                  { name: 'api', status: 'deployed', url: 'https://api.example.com', build_duration_seconds: 30 },
                  { name: 'web', status: 'failed', error: 'Compilation error', build_duration_seconds: 30 },
                ],
              },
            });
            return { close: jest.fn() };
          }
        ),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(5);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      const failedApp = parsed.apps.find((a: Record<string, unknown>) => a.status === 'failed');
      expect(failedApp).toHaveProperty('error');
    });

    it('should include build_log field for failed apps in JSON output (F-04)', async () => {
      const deps = createMockDeps({
        submitBuild: jest.fn().mockResolvedValue({
          build_id: 'build-abc123',
          stream_url: '/api/v1/builds/build-abc123/stream',
        }),
        connectSSE: jest.fn().mockImplementation(
          (_buildId: string, onEvent: (event: SSEEvent) => void) => {
            onEvent({
              type: 'deploy.completed',
              data: {
                deploy_id: 'd-abcd',
                status: 'partial_failure',
                duration_seconds: 60,
                apps: [
                  { name: 'api', status: 'deployed', url: 'https://api.example.com', build_duration_seconds: 30 },
                  { name: 'web', status: 'failed', error: 'TS2307 module not found', build_duration_seconds: 30 },
                ],
              },
            });
            return { close: jest.fn() };
          }
        ),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(5);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      const failedApp = parsed.apps.find((a: Record<string, unknown>) => a.status === 'failed');
      expect(failedApp.build_log).toBe('loopzero logs --build build-abc123');
      // Deployed apps should NOT have build_log
      const deployedApp = parsed.apps.find((a: Record<string, unknown>) => a.status === 'deployed');
      expect(deployedApp.build_log).toBeUndefined();
    });
  });

  describe('--no-wait', () => {
    it('should exit 0 with message after submit accepted (DA-2)', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ wait: false });

      expect(exitCode).toBe(0);
      expect(deps.connectSSE).not.toHaveBeenCalled();
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(output).toContain('Deploy submitted');
      expect(output).toContain('build-deadbeef');
      expect(output).toContain('Track status');
    });
  });

  describe('no .git', () => {
    it('should exit 1 with message when no git repository', async () => {
      const deps = createMockDeps({
        hasGitRepo: jest.fn().mockReturnValue(false),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(1);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('Not a git repository.');
    });
  });

  describe('no loopzero.yaml', () => {
    it('should exit 1 with message when no loopzero.yaml found', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue(null),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(1);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('No loopzero.yaml found');
      expect(errOutput).toContain('loopzero init');
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
      const cmd1 = new DeployCommand(deps1);
      await cmd1.execute({});

      // Test error paths
      const deps2 = createMockDeps({ getToken: jest.fn().mockReturnValue(null) });
      const cmd2 = new DeployCommand(deps2);
      await cmd2.execute({});

      const deps3 = createMockDeps({ findLoopZeroYaml: jest.fn().mockReturnValue(null) });
      const cmd3 = new DeployCommand(deps3);
      await cmd3.execute({});

      const deps4 = createMockDeps({ hasGitRepo: jest.fn().mockReturnValue(false) });
      const cmd4 = new DeployCommand(deps4);
      await cmd4.execute({});

      for (const deps of [deps1, deps2, deps3, deps4]) {
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

  describe('--env validation (DA-10)', () => {
    it('should exit 1 when environment name is uppercase', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ env: 'PRODUCTION' });

      expect(exitCode).toBe(1);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('Environment names must be lowercase.');
    });

    it('should exit 1 when environment name has mixed case', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ env: 'Staging' });

      expect(exitCode).toBe(1);
    });

    it('should accept valid lowercase environment names', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ env: 'staging' });

      expect(exitCode).toBe(0);
    });

    it('should accept environment names with numbers and hyphens', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ env: 'dev-2' });

      expect(exitCode).toBe(0);
    });

    it('should reject environment names starting with a number', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ env: '2dev' });

      expect(exitCode).toBe(1);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('must start with a lowercase letter');
    });

    it('should show specific message for env starting with number (F-04)', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ env: '123staging' });

      expect(exitCode).toBe(1);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('must start with a lowercase letter');
      expect(errOutput).not.toContain('must be lowercase');
    });

    it('should show specific message for env with invalid chars like @ (F-04)', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ env: 'stag@ing' });

      expect(exitCode).toBe(1);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('can only contain lowercase letters, numbers, and hyphens');
    });

    it('should show specific message for env too long (F-04)', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ env: 'a'.repeat(64) });

      expect(exitCode).toBe(1);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('too long');
      expect(errOutput).toContain('64 chars');
      expect(errOutput).toContain('max 63');
    });
  });

  describe('0 apps in loopzero.yaml', () => {
    it('should exit 1 when loopzero.yaml has no apps', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue({
          path: '/project/loopzero.yaml',
          content: { project: 'my-saas', apps: {} },
        }),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(1);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('No apps defined in loopzero.yaml');
    });
  });

  describe('--app flag validation (DA-19)', () => {
    it('should exit 1 when --app specifies nonexistent app (fail fast)', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ app: ['nonexistent'] });

      expect(exitCode).toBe(1);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('nonexistent');
      // Must fail BEFORE any upload
      expect(deps.createGitBundle).not.toHaveBeenCalled();
      expect(deps.uploadSource).not.toHaveBeenCalled();
    });

    it('should accept valid --app names that exist in loopzero.yaml', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ app: ['api'] });

      expect(exitCode).toBe(0);
    });

    it('should exit 1 if any of multiple --app names is invalid', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ app: ['api', 'ghost'] });

      expect(exitCode).toBe(1);
      expect(deps.createGitBundle).not.toHaveBeenCalled();
    });
  });

  describe('loopzero.yaml search walks up to git root', () => {
    it('should search from cwd up to git root for loopzero.yaml', async () => {
      const findLoopZeroYaml = jest.fn().mockReturnValue({
        path: '/project/loopzero.yaml',
        content: {
          project: 'my-saas',
          apps: { api: { path: '.', framework: 'express', port: 3000 } },
        },
      });
      const deps = createMockDeps({ findLoopZeroYaml });
      const cmd = new DeployCommand(deps);

      await cmd.execute({});

      expect(findLoopZeroYaml).toHaveBeenCalled();
    });
  });

  describe('--env passed to submitBuild', () => {
    it('should pass custom environment to build submission', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      await cmd.execute({ env: 'staging' });

      expect(deps.submitBuild).toHaveBeenCalledWith(
        expect.objectContaining({ environment: 'staging' })
      );
    });
  });

  describe('--app passed to submitBuild', () => {
    it('should pass selected apps with type/path/framework to build submission', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      await cmd.execute({ app: ['api'] });

      expect(deps.submitBuild).toHaveBeenCalledWith(
        expect.objectContaining({
          apps: [expect.objectContaining({ name: 'api', type: 'server', path: 'apps/api', framework: 'express' })],
        })
      );
    });

    it('should pass all apps with type info when no --app flag', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      await cmd.execute({});

      expect(deps.submitBuild).toHaveBeenCalledWith(
        expect.objectContaining({
          apps: [
            expect.objectContaining({ name: 'api', type: 'server' }),
            expect.objectContaining({ name: 'web', type: 'frontend' }),
          ],
        })
      );
    });
  });

  describe('deploy failure from SSE stream', () => {
    it('should exit 3 when deploy status indicates deploy failure', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue({
          path: '/project/loopzero.yaml',
          content: {
            project: 'my-saas',
            apps: { api: { path: '.', framework: 'express', port: 3000 } },
          },
        }),
        connectSSE: jest.fn().mockImplementation(
          (_buildId: string, onEvent: (event: SSEEvent) => void) => {
            onEvent({
              type: 'deploy.completed',
              data: {
                deploy_id: 'd-1234',
                status: 'deploy_failed',
                duration_seconds: 90,
                apps: [
                  { name: 'api', status: 'failed', error: 'Deploy failed: sync error', build_duration_seconds: 45 },
                ],
              },
            });
            return { close: jest.fn() };
          }
        ),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(3);
    });
  });

  describe('upload failure', () => {
    it('should exit 3 when upload fails with server error', async () => {
      const deps = createMockDeps({
        uploadSource: jest.fn().mockRejectedValue(
          Object.assign(new Error('Service unavailable'), { exitCode: 3, statusCode: 500 })
        ),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(3);
    });

    it('should prefix server-side upload errors with "Upload rejected:" in human mode', async () => {
      const deps = createMockDeps({
        uploadSource: jest.fn().mockRejectedValue(
          new ApiError('loopzero.yaml is required', 1, 422, 'VALIDATION_FAILED')
        ),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(1);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('Upload rejected:');
      expect(errOutput).toContain('loopzero.yaml is required');
    });

    it('should prefix server-side upload errors with "Upload rejected:" in JSON mode', async () => {
      const deps = createMockDeps({
        uploadSource: jest.fn().mockRejectedValue(
          new ApiError('loopzero.yaml is required', 1, 422, 'VALIDATION_FAILED')
        ),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(1);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.error).toContain('Upload rejected:');
      expect(parsed.error).toContain('loopzero.yaml is required');
    });
  });

  describe('build submission failure', () => {
    it('should prefix server-side build submission errors with "Deploy rejected:" in human mode', async () => {
      const deps = createMockDeps({
        submitBuild: jest.fn().mockRejectedValue(
          new ApiError('Project quota exceeded', 1, 422, 'VALIDATION_FAILED')
        ),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(1);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('Deploy rejected:');
      expect(errOutput).toContain('Project quota exceeded');
    });

    it('should prefix server-side build submission errors with "Deploy rejected:" in JSON mode', async () => {
      const deps = createMockDeps({
        submitBuild: jest.fn().mockRejectedValue(
          new ApiError('Project quota exceeded', 1, 422, 'VALIDATION_FAILED')
        ),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(1);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.error).toContain('Deploy rejected:');
      expect(parsed.error).toContain('Project quota exceeded');
    });
  });

  describe('--env validation edge cases', () => {
    it('should exit 1 when environment name contains special characters', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ env: 'prod!ion' });

      expect(exitCode).toBe(1);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('can only contain lowercase letters, numbers, and hyphens');
    });

    it('should exit 1 when environment name contains spaces', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ env: 'my env' });

      expect(exitCode).toBe(1);
    });

    it('should exit 1 when environment name contains underscores', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ env: 'my_env' });

      expect(exitCode).toBe(1);
    });

    it('should exit 1 when environment name is empty string', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ env: '' });

      expect(exitCode).toBe(1);
    });

    it('should exit 1 when environment name starts with hyphen', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ env: '-staging' });

      expect(exitCode).toBe(1);
    });

    it('should exit 1 when environment name exceeds 63 characters', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ env: 'a'.repeat(64) });

      expect(exitCode).toBe(1);
    });

    it('should accept environment name at max 63 characters', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ env: 'a'.repeat(63) });

      expect(exitCode).toBe(0);
    });
  });

  describe('--app multiple valid apps (DA-19)', () => {
    it('should accept and send multiple valid --app names with types', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ app: ['api', 'web'] });

      expect(exitCode).toBe(0);
      expect(deps.submitBuild).toHaveBeenCalledWith(
        expect.objectContaining({
          apps: [
            expect.objectContaining({ name: 'api', type: 'server' }),
            expect.objectContaining({ name: 'web', type: 'frontend' }),
          ],
        })
      );
    });
  });

  describe('--env and --app combination', () => {
    it('should send correct environment and filtered apps together', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ env: 'staging', app: ['api'] });

      expect(exitCode).toBe(0);
      expect(deps.submitBuild).toHaveBeenCalledWith(
        expect.objectContaining({
          environment: 'staging',
          apps: [expect.objectContaining({ name: 'api', type: 'server' })],
        })
      );
    });

    it('should send correct environment and multiple filtered apps together', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ env: 'staging', app: ['api', 'web'] });

      expect(exitCode).toBe(0);
      expect(deps.submitBuild).toHaveBeenCalledWith(
        expect.objectContaining({
          environment: 'staging',
          apps: [
            expect.objectContaining({ name: 'api', type: 'server' }),
            expect.objectContaining({ name: 'web', type: 'frontend' }),
          ],
        })
      );
    });
  });

  describe('--app fail fast ensures no upload', () => {
    it('should not upload when --app api --app nonexistent (fail fast DA-19)', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ app: ['api', 'nonexistent'] });

      expect(exitCode).toBe(1);
      expect(deps.createGitBundle).not.toHaveBeenCalled();
      expect(deps.uploadSource).not.toHaveBeenCalled();
      expect(deps.submitBuild).not.toHaveBeenCalled();
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('nonexistent');
      expect(errOutput).toContain('Available apps');
    });
  });

  describe('secrets validation pre-deploy', () => {
    it('should continue deploy when all declared secrets are configured', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue({
          path: '/project/loopzero.yaml',
          content: {
            project: 'my-saas',
            apps: {
              api: { path: 'apps/api', framework: 'express', port: 3000, secrets: ['DATABASE_URL', 'JWT_SECRET'] },
            },
          },
        }),
        getSecrets: jest.fn().mockResolvedValue(['DATABASE_URL', 'JWT_SECRET']),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(0);
      expect(deps.createGitBundle).toHaveBeenCalled();
      expect(deps.uploadSource).toHaveBeenCalled();
    });

    it('should exit 1 with list of missing secrets when secrets are not configured', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue({
          path: '/project/loopzero.yaml',
          content: {
            project: 'my-saas',
            apps: {
              api: { path: 'apps/api', framework: 'express', port: 3000, secrets: ['DATABASE_URL', 'JWT_SECRET'] },
            },
          },
        }),
        getSecrets: jest.fn().mockResolvedValue([]),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(1);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('DATABASE_URL');
      expect(errOutput).toContain('JWT_SECRET');
      expect(errOutput).toContain('production');
      // Must fail BEFORE any upload (fail fast)
      expect(deps.createGitBundle).not.toHaveBeenCalled();
      expect(deps.uploadSource).not.toHaveBeenCalled();
    });

    it('should continue deploy when no secrets are declared in loopzero.yaml', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue({
          path: '/project/loopzero.yaml',
          content: {
            project: 'my-saas',
            apps: {
              api: { path: 'apps/api', framework: 'express', port: 3000 },
              web: { path: 'apps/web', framework: 'next', port: 3001 },
            },
          },
        }),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(0);
      // Should NOT call getSecrets when no secrets declared
      expect(deps.getSecrets).not.toHaveBeenCalled();
    });

    it('should NOT suggest loopzero secret set in error message (DA-8)', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue({
          path: '/project/loopzero.yaml',
          content: {
            project: 'my-saas',
            apps: {
              api: { path: 'apps/api', framework: 'express', port: 3000, secrets: ['DATABASE_URL'] },
            },
          },
        }),
        getSecrets: jest.fn().mockResolvedValue([]),
      });
      const cmd = new DeployCommand(deps);

      await cmd.execute({});

      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).not.toContain('loopzero secret set');
      expect(errOutput).not.toContain('loopzero secret');
    });

    it('should include documentation URL in missing secrets message', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue({
          path: '/project/loopzero.yaml',
          content: {
            project: 'my-saas',
            apps: {
              api: { path: 'apps/api', framework: 'express', port: 3000, secrets: ['DATABASE_URL'] },
            },
          },
        }),
        getSecrets: jest.fn().mockResolvedValue([]),
      });
      const cmd = new DeployCommand(deps);

      await cmd.execute({});

      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('https://docs.loopzero.example/secrets');
    });

    it('should aggregate secrets from all apps and deduplicate', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue({
          path: '/project/loopzero.yaml',
          content: {
            project: 'my-saas',
            apps: {
              api: { path: 'apps/api', framework: 'express', port: 3000, secrets: ['DATABASE_URL', 'JWT_SECRET'] },
              worker: { path: 'apps/worker', framework: 'express', port: null, secrets: ['DATABASE_URL', 'REDIS_URL'] },
            },
          },
        }),
        getSecrets: jest.fn().mockResolvedValue(['DATABASE_URL']),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(1);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('JWT_SECRET');
      expect(errOutput).toContain('REDIS_URL');
    });

    it('should pass project and environment to getSecrets', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue({
          path: '/project/loopzero.yaml',
          content: {
            project: 'my-saas',
            apps: {
              api: { path: 'apps/api', framework: 'express', port: 3000, secrets: ['DATABASE_URL'] },
            },
          },
        }),
        getSecrets: jest.fn().mockResolvedValue(['DATABASE_URL']),
      });
      const cmd = new DeployCommand(deps);

      await cmd.execute({ env: 'staging' });

      expect(deps.getSecrets).toHaveBeenCalledWith('my-saas', 'staging');
    });

    it('should only check secrets for selected apps when --app is used (F-02)', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue({
          path: '/project/loopzero.yaml',
          content: {
            project: 'my-saas',
            apps: {
              api: { path: 'apps/api', framework: 'express', port: 3000, secrets: ['DATABASE_URL', 'JWT_SECRET'] },
              gateway: { path: 'apps/gateway', framework: 'express', port: 4000 },
              worker: { path: 'apps/worker', framework: 'node', port: null, secrets: ['DATABASE_URL', 'REDIS_URL'] },
            },
          },
        }),
        getSecrets: jest.fn().mockResolvedValue([]),
      });
      const cmd = new DeployCommand(deps);

      // Deploy only gateway (which has NO secrets) — should succeed without checking secrets
      const exitCode = await cmd.execute({ app: ['gateway'], wait: false });

      expect(exitCode).toBe(0);
      // getSecrets should NOT be called because gateway declares no secrets
      expect(deps.getSecrets).not.toHaveBeenCalled();
    });

    it('should check only selected app secrets when --app selects app with secrets (F-02)', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue({
          path: '/project/loopzero.yaml',
          content: {
            project: 'my-saas',
            apps: {
              api: { path: 'apps/api', framework: 'express', port: 3000, secrets: ['DATABASE_URL', 'JWT_SECRET'] },
              gateway: { path: 'apps/gateway', framework: 'express', port: 4000 },
              worker: { path: 'apps/worker', framework: 'node', port: null, secrets: ['DATABASE_URL', 'REDIS_URL'] },
            },
          },
        }),
        getSecrets: jest.fn().mockResolvedValue(['DATABASE_URL']),
      });
      const cmd = new DeployCommand(deps);

      // Deploy only api — should fail on JWT_SECRET but NOT on REDIS_URL (worker's secret)
      const exitCode = await cmd.execute({ app: ['api'] });

      expect(exitCode).toBe(1);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('JWT_SECRET');
      expect(errOutput).not.toContain('REDIS_URL');
    });
  });

  describe('malformed loopzero.yaml (F-05)', () => {
    it('should exit 1 with parse error when loopzero.yaml is malformed', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue({
          path: '/project/loopzero.yaml',
          content: { project: '', apps: {} },
          parseError: 'unexpected token at line 1',
        }),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(1);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('Invalid loopzero.yaml');
      expect(errOutput).toContain('unexpected token');
    });

    it('should not say "No apps defined" when YAML is malformed', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue({
          path: '/project/loopzero.yaml',
          content: { project: '', apps: {} },
          parseError: 'invalid syntax',
        }),
      });
      const cmd = new DeployCommand(deps);

      await cmd.execute({});

      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).not.toContain('No apps defined');
      expect(errOutput).toContain('Invalid loopzero.yaml');
    });
  });

  describe('uncommitted changes warning (F-06)', () => {
    it('should warn when there are uncommitted changes', async () => {
      const deps = createMockDeps({
        hasUncommittedChanges: jest.fn().mockReturnValue(true),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('uncommitted changes');
      expect(output).toContain('Only committed code will be deployed');
    });

    it('should not warn when there are no uncommitted changes', async () => {
      const deps = createMockDeps({
        hasUncommittedChanges: jest.fn().mockReturnValue(false),
      });
      const cmd = new DeployCommand(deps);

      await cmd.execute({});

      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).not.toContain('uncommitted changes');
    });

    it('should not show warning in JSON mode', async () => {
      const deps = createMockDeps({
        hasUncommittedChanges: jest.fn().mockReturnValue(true),
      });
      const cmd = new DeployCommand(deps);

      await cmd.execute({ json: true });

      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      // JSON output should only contain the deploy result JSON, not the warning text
      expect(output).not.toContain('uncommitted changes');
    });

    it('should still deploy successfully even with uncommitted changes', async () => {
      const deps = createMockDeps({
        hasUncommittedChanges: jest.fn().mockReturnValue(true),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(0);
      expect(deps.createGitBundle).toHaveBeenCalled();
      expect(deps.uploadSource).toHaveBeenCalled();
      expect(deps.submitBuild).toHaveBeenCalled();
    });
  });

  describe('--json on error paths (F-02)', () => {
    it('should output JSON when --json and no auth token', async () => {
      const deps = createMockDeps({
        getToken: jest.fn().mockReturnValue(null),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(4);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed).toEqual({ code: 'AUTH_REQUIRED', error: 'Not authenticated. Run: loopzero login', exit_code: 4 });
      expect(deps.writeError).not.toHaveBeenCalled();
    });

    it('should output JSON when --json and invalid env (uppercase)', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ env: 'PRODUCTION', json: true });

      expect(exitCode).toBe(1);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.error).toContain('lowercase');
      expect(parsed.exit_code).toBe(1);
      expect(deps.writeError).not.toHaveBeenCalled();
    });

    it('should output JSON when --json and no .git repo', async () => {
      const deps = createMockDeps({
        hasGitRepo: jest.fn().mockReturnValue(false),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(1);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed).toEqual({ code: 'NOT_GIT_REPO', error: 'Not a git repository. Run this command from your project directory.', exit_code: 1 });
      expect(deps.writeError).not.toHaveBeenCalled();
    });

    it('should output JSON when --json and no loopzero.yaml', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue(null),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(1);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed).toEqual({ code: 'MISSING_CONFIG', error: 'No loopzero.yaml found. Run: loopzero init', exit_code: 1 });
      expect(deps.writeError).not.toHaveBeenCalled();
    });

    it('should output JSON when --json and no apps in loopzero.yaml', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue({
          path: '/project/loopzero.yaml',
          content: { project: 'my-saas', apps: {} },
        }),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(1);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.error).toContain('No apps defined');
      expect(parsed.exit_code).toBe(1);
    });

    it('should output JSON when --json and invalid --app', async () => {
      const deps = createMockDeps();
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ app: ['nonexistent'], json: true });

      expect(exitCode).toBe(1);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.error).toContain('nonexistent');
      expect(parsed.exit_code).toBe(1);
    });

    it('should output JSON when --json and missing secrets', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue({
          path: '/project/loopzero.yaml',
          content: {
            project: 'my-saas',
            apps: {
              api: { path: 'apps/api', framework: 'express', port: 3000, secrets: ['DATABASE_URL'] },
            },
          },
        }),
        getSecrets: jest.fn().mockResolvedValue([]),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(1);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.error).toContain('DATABASE_URL');
      expect(parsed.exit_code).toBe(1);
      expect(deps.writeError).not.toHaveBeenCalled();
    });

    it('should include typed code field in all --json error responses (F-03)', async () => {
      // Test AUTH_REQUIRED
      const authDeps = createMockDeps({ getToken: jest.fn().mockReturnValue(null) });
      let exitCode = await new DeployCommand(authDeps).execute({ json: true });
      let parsed = JSON.parse((authDeps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(''));
      expect(parsed.code).toBe('AUTH_REQUIRED');
      expect(exitCode).toBe(4);

      // Test NOT_GIT_REPO
      const gitDeps = createMockDeps({ hasGitRepo: jest.fn().mockReturnValue(false) });
      exitCode = await new DeployCommand(gitDeps).execute({ json: true });
      parsed = JSON.parse((gitDeps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(''));
      expect(parsed.code).toBe('NOT_GIT_REPO');

      // Test INVALID_ENVIRONMENT
      const envDeps = createMockDeps();
      exitCode = await new DeployCommand(envDeps).execute({ env: 'PROD', json: true });
      parsed = JSON.parse((envDeps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(''));
      expect(parsed.code).toBe('INVALID_ENVIRONMENT');

      // Test APP_NOT_FOUND
      const appDeps = createMockDeps();
      exitCode = await new DeployCommand(appDeps).execute({ app: ['nonexistent'], json: true });
      parsed = JSON.parse((appDeps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(''));
      expect(parsed.code).toBe('APP_NOT_FOUND');
    });

    it('should output JSON when --json and deploy conflict (409)', async () => {
      const deps = createMockDeps({
        submitBuild: jest.fn().mockRejectedValue(
          Object.assign(new Error('A deploy is already in progress for this environment. Wait for it to complete or cancel it.'), {
            exitCode: 3,
            statusCode: 409,
          })
        ),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(3);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.error).toContain('already in progress');
      expect(parsed.exit_code).toBe(3);
      expect(deps.writeError).not.toHaveBeenCalled();
    });
  });

  describe('wall-clock duration fallback (F-04)', () => {
    it('should show wall-clock time when server reports duration_seconds 0', async () => {
      // Mock Date.now: returns base time initially, then advances when SSE event fires
      let sseEventFired = false;
      jest.spyOn(Date, 'now').mockImplementation(() => {
        return sseEventFired ? 1002000 : 1000000;
      });

      const deps = createMockDeps({
        connectSSE: jest.fn().mockImplementation(
          (_buildId: string, onEvent: (event: SSEEvent) => void) => {
            sseEventFired = true;
            onEvent({
              type: 'deploy.completed',
              data: {
                deploy_id: 'd-8f3a',
                status: 'success',
                duration_seconds: 0,
                apps: [
                  { name: 'api', status: 'deployed', url: 'https://api.example.com', build_duration_seconds: 0 },
                ],
              },
            });
            return { close: jest.fn() };
          }
        ),
        findLoopZeroYaml: jest.fn().mockReturnValue({
          path: '/project/loopzero.yaml',
          content: {
            project: 'my-saas',
            apps: { api: { path: 'apps/api', framework: 'express', port: 3000 } },
          },
        }),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ env: 'production', wait: true });

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).not.toContain('completed in 0s');
      expect(output).toContain('completed in 2s');

      jest.restoreAllMocks();
    });

    it('should use server duration when server reports nonzero duration_seconds', async () => {
      const deps = createMockDeps({
        connectSSE: jest.fn().mockImplementation(
          (_buildId: string, onEvent: (event: SSEEvent) => void) => {
            onEvent({
              type: 'deploy.completed',
              data: {
                deploy_id: 'd-8f3a',
                status: 'success',
                duration_seconds: 97,
                apps: [
                  { name: 'api', status: 'deployed', url: 'https://api.example.com', build_duration_seconds: 45 },
                ],
              },
            });
            return { close: jest.fn() };
          }
        ),
        findLoopZeroYaml: jest.fn().mockReturnValue({
          path: '/project/loopzero.yaml',
          content: {
            project: 'my-saas',
            apps: { api: { path: 'apps/api', framework: 'express', port: 3000 } },
          },
        }),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ env: 'production', wait: true });

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('97s');
    });

    it('should use wall-clock time in JSON output when server reports duration_seconds 0', async () => {
      let sseEventFired = false;
      jest.spyOn(Date, 'now').mockImplementation(() => {
        return sseEventFired ? 1003000 : 1000000;
      });

      const deps = createMockDeps({
        connectSSE: jest.fn().mockImplementation(
          (_buildId: string, onEvent: (event: SSEEvent) => void) => {
            sseEventFired = true;
            onEvent({
              type: 'deploy.completed',
              data: {
                deploy_id: 'd-8f3a',
                status: 'success',
                duration_seconds: 0,
                apps: [
                  { name: 'api', status: 'deployed', url: 'https://api.example.com', build_duration_seconds: 0 },
                ],
              },
            });
            return { close: jest.fn() };
          }
        ),
        findLoopZeroYaml: jest.fn().mockReturnValue({
          path: '/project/loopzero.yaml',
          content: {
            project: 'my-saas',
            apps: { api: { path: 'apps/api', framework: 'express', port: 3000 } },
          },
        }),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ env: 'production', wait: true, json: true });

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.duration_seconds).toBe(3);

      jest.restoreAllMocks();
    });
  });

  describe('worker app URL is null not empty string in JSON (F-08)', () => {
    it('should output url as null for worker apps in JSON mode', async () => {
      const deps = createMockDeps({
        connectSSE: jest.fn().mockImplementation(
          (_buildId: string, onEvent: (event: SSEEvent) => void) => {
            onEvent({
              type: 'deploy.completed',
              data: {
                deploy_id: 'd-8f3a',
                status: 'success',
                duration_seconds: 97,
                apps: [
                  { name: 'api', status: 'deployed', url: 'https://api.example.com', build_duration_seconds: 45 },
                  { name: 'worker', status: 'deployed', url: '', build_duration_seconds: 30 },
                ],
              },
            });
            return { close: jest.fn() };
          }
        ),
        findLoopZeroYaml: jest.fn().mockReturnValue({
          path: '/project/loopzero.yaml',
          content: {
            project: 'my-saas',
            apps: {
              api: { path: 'apps/api', framework: 'express', port: 3000 },
              worker: { path: 'apps/worker', framework: 'express', port: null, type: 'worker' },
            },
          },
        }),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      const workerApp = parsed.apps.find((a: Record<string, unknown>) => a.name === 'worker');
      expect(workerApp.url).toBeNull();
    });

    it('should output url as null for apps with undefined url in JSON mode', async () => {
      const deps = createMockDeps({
        connectSSE: jest.fn().mockImplementation(
          (_buildId: string, onEvent: (event: SSEEvent) => void) => {
            onEvent({
              type: 'deploy.completed',
              data: {
                deploy_id: 'd-8f3a',
                status: 'success',
                duration_seconds: 97,
                apps: [
                  { name: 'api', status: 'deployed', url: 'https://api.example.com', build_duration_seconds: 45 },
                  { name: 'worker', status: 'deployed', build_duration_seconds: 30 },
                ],
              },
            });
            return { close: jest.fn() };
          }
        ),
        findLoopZeroYaml: jest.fn().mockReturnValue({
          path: '/project/loopzero.yaml',
          content: {
            project: 'my-saas',
            apps: {
              api: { path: 'apps/api', framework: 'express', port: 3000 },
              worker: { path: 'apps/worker', framework: 'express', port: null, type: 'worker' },
            },
          },
        }),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      const workerApp = parsed.apps.find((a: Record<string, unknown>) => a.name === 'worker');
      expect(workerApp.url).toBeNull();
    });
  });

  describe('--json with --no-wait (F-03)', () => {
    it('should output JSON with build_id, stream_url, and status when --json --no-wait', async () => {
      const deps = createMockDeps({
        submitBuild: jest.fn().mockResolvedValue({
          build_id: 'build-deadbeef',
          stream_url: 'https://api.example.com/builds/build-deadbeef/stream',
        }),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ wait: false, json: true });

      expect(exitCode).toBe(0);
      expect(deps.connectSSE).not.toHaveBeenCalled();
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed).toEqual({
        build_id: 'build-deadbeef',
        stream_url: 'https://api.example.com/builds/build-deadbeef/stream',
        status: 'submitted',
      });
      expect(deps.writeError).not.toHaveBeenCalled();
    });
  });

  describe('intermediate SSE progress events (F-04)', () => {
    it('should display progress lines for intermediate events in human mode', async () => {
      const deps = createMockDeps({
        connectSSE: jest.fn().mockImplementation(
          (_buildId: string, onEvent: (event: SSEEvent) => void) => {
            // Emit intermediate events before completion
            onEvent({ type: 'build.started', data: { app: 'api' } });
            onEvent({ type: 'build.completed', data: { app: 'api', duration_seconds: 12 } });
            onEvent({ type: 'health.checking', data: { app: 'api' } });
            onEvent({ type: 'health.passed', data: { app: 'api', duration_seconds: 3 } });
            onEvent({
              type: 'deploy.completed',
              data: {
                deploy_id: 'd-8f3a',
                status: 'success',
                duration_seconds: 15,
                apps: [
                  { name: 'api', status: 'deployed', url: 'https://api.example.com', build_duration_seconds: 12 },
                ],
              },
            });
            return { close: jest.fn() };
          }
        ),
      });
      const cmd = new DeployCommand(deps);

      await cmd.execute({});

      const writeToArgs = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]);

      // Should have progress lines for intermediate events
      expect(writeToArgs.some((msg: string) => msg.includes('Building api'))).toBe(true);
      expect(writeToArgs.some((msg: string) => msg.includes('12s'))).toBe(true);
      expect(writeToArgs.some((msg: string) => /health/i.test(msg))).toBe(true);
    });

    it('should NOT display progress lines for intermediate events in --json mode', async () => {
      const deps = createMockDeps({
        connectSSE: jest.fn().mockImplementation(
          (_buildId: string, onEvent: (event: SSEEvent) => void) => {
            onEvent({ type: 'build.started', data: { app: 'api' } });
            onEvent({ type: 'build.completed', data: { app: 'api', duration_seconds: 12 } });
            onEvent({ type: 'health.checking', data: { app: 'api' } });
            onEvent({
              type: 'deploy.completed',
              data: {
                deploy_id: 'd-8f3a',
                status: 'success',
                duration_seconds: 15,
                apps: [
                  { name: 'api', status: 'deployed', url: 'https://api.example.com', build_duration_seconds: 12 },
                ],
              },
            });
            return { close: jest.fn() };
          }
        ),
      });
      const cmd = new DeployCommand(deps);

      await cmd.execute({ json: true });

      const writeToArgs = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]);

      // In JSON mode, only the final JSON object should be written
      expect(writeToArgs).toHaveLength(1);
      const parsed = JSON.parse(writeToArgs[0]);
      expect(parsed.deploy_id).toBe('d-8f3a');
      expect(parsed.status).toBe('success');
    });

    it('should exit 2 and display error on build.failed event in human mode', async () => {
      const deps = createMockDeps({
        connectSSE: jest.fn().mockImplementation(
          (_buildId: string, onEvent: (event: SSEEvent) => void) => {
            onEvent({ type: 'build.started', data: { app: 'api' } });
            onEvent({ type: 'build.failed', data: { build_id: 'build-deadbeef', status: 'failed', error: 'compilation error' } });
            return { close: jest.fn() };
          }
        ),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(2);
      expect(deps.writeError).toHaveBeenCalledWith(expect.stringContaining('Build failed'));
      expect(deps.writeError).toHaveBeenCalledWith(expect.stringContaining('compilation error'));
    });

    it('should render deploy.app_live event with URL', async () => {
      const deps = createMockDeps({
        connectSSE: jest.fn().mockImplementation(
          (_buildId: string, onEvent: (event: SSEEvent) => void) => {
            onEvent({ type: 'deploy.app_live', data: { app: 'api', url: 'https://api.example.com' } });
            onEvent({
              type: 'deploy.completed',
              data: {
                deploy_id: 'd-8f3a',
                status: 'success',
                duration_seconds: 15,
                apps: [
                  { name: 'api', status: 'deployed', url: 'https://api.example.com', build_duration_seconds: 12 },
                ],
              },
            });
            return { close: jest.fn() };
          }
        ),
      });
      const cmd = new DeployCommand(deps);

      await cmd.execute({});

      const writeToArgs = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]);
      expect(writeToArgs.some((msg: string) => msg.includes('api') && msg.includes('deployed'))).toBe(true);
    });

    it('should use CI-style progress lines when isCI is true', async () => {
      const deps = createMockDeps({
        isCI: true,
        connectSSE: jest.fn().mockImplementation(
          (_buildId: string, onEvent: (event: SSEEvent) => void) => {
            onEvent({ type: 'build.started', data: { app: 'api' } });
            onEvent({
              type: 'deploy.completed',
              data: {
                deploy_id: 'd-8f3a',
                status: 'success',
                duration_seconds: 15,
                apps: [
                  { name: 'api', status: 'deployed', url: 'https://api.example.com', build_duration_seconds: 12 },
                ],
              },
            });
            return { close: jest.fn() };
          }
        ),
      });
      const cmd = new DeployCommand(deps);

      await cmd.execute({});

      const writeToArgs = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]);
      // CI mode uses [build] prefix without ANSI color codes
      expect(writeToArgs.some((msg: string) => msg.includes('[build] api: building...'))).toBe(true);
    });
  });

  describe('build.failed SSE event', () => {
    it('exits with code 2 when build.failed event received', async () => {
      const deps = createMockDeps({
        connectSSE: jest.fn().mockImplementation(
          (_buildId: string, onEvent: (event: SSEEvent) => void) => {
            onEvent({
              type: 'build.failed',
              data: { build_id: 'build-deadbeef', status: 'failed', error: 'npm install failed' },
            });
            return { close: jest.fn() };
          }
        ),
      });
      const cmd = new DeployCommand(deps);
      const exitCode = await cmd.execute({});
      expect(exitCode).toBe(2);
      expect(deps.writeError).toHaveBeenCalledWith(expect.stringContaining('Build failed'));
    });

    it('outputs JSON on build.failed when --json is set', async () => {
      const deps = createMockDeps({
        connectSSE: jest.fn().mockImplementation(
          (_buildId: string, onEvent: (event: SSEEvent) => void) => {
            onEvent({
              type: 'build.failed',
              data: { build_id: 'build-deadbeef', status: 'failed', error: 'npm install failed' },
            });
            return { close: jest.fn() };
          }
        ),
      });
      const cmd = new DeployCommand(deps);
      const exitCode = await cmd.execute({ json: true });
      expect(exitCode).toBe(2);
      expect(deps.writeTo).toHaveBeenCalledWith(expect.stringContaining('"exit_code":2'));
    });
  });

  describe('deploy.sync_failed SSE event', () => {
    it('exits with code 3 when deploy.sync_failed received', async () => {
      const deps = createMockDeps({
        connectSSE: jest.fn().mockImplementation(
          (_buildId: string, onEvent: (event: SSEEvent) => void) => {
            onEvent({
              type: 'deploy.sync_failed',
              data: { deploy_id: 'd-1234', status: 'sync_failed', reason: 'HEALTH_PROBE_FAILED' },
            });
            return { close: jest.fn() };
          }
        ),
      });
      const cmd = new DeployCommand(deps);
      const exitCode = await cmd.execute({});
      expect(exitCode).toBe(3);
      expect(deps.writeError).toHaveBeenCalledWith(expect.stringContaining("Service didn't become healthy in time"));
      expect(deps.writeError).toHaveBeenCalledWith(expect.stringContaining('loopzero logs --build'));
    });

    it('outputs JSON with exit_code 3 on deploy.sync_failed with --json', async () => {
      const deps = createMockDeps({
        connectSSE: jest.fn().mockImplementation(
          (_buildId: string, onEvent: (event: SSEEvent) => void) => {
            onEvent({
              type: 'deploy.sync_failed',
              data: { deploy_id: 'd-1234', status: 'sync_failed', reason: 'HEALTH_PROBE_FAILED' },
            });
            return { close: jest.fn() };
          }
        ),
      });
      const cmd = new DeployCommand(deps);
      const exitCode = await cmd.execute({ json: true });
      expect(exitCode).toBe(3);
      const output = (deps.writeTo as jest.Mock).mock.calls[0][0];
      const parsed = JSON.parse(output);
      expect(parsed.exit_code).toBe(3);
      expect(parsed.error).toContain("Service didn't become healthy in time");
      expect(parsed.reason).toBe('HEALTH_PROBE_FAILED');
      expect(parsed.build_id).toBe('build-deadbeef');
      expect(parsed.deploy_id).toBe('d-1234');
    });
  });

  describe('stream terminal errors', () => {
    it('exits with code 3 on stream.error and shows message', async () => {
      const deps = createMockDeps({
        connectSSE: jest.fn().mockImplementation(
          (_buildId: string, onEvent: (event: SSEEvent) => void) => {
            onEvent({
              type: 'stream.error',
              data: { error: 'SSE connection failed with status 503' },
            });
            return { close: jest.fn() };
          }
        ),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(3);
      expect(deps.writeError).toHaveBeenCalledWith(expect.stringContaining('Deploy stream error'));
    });

    it('outputs JSON with exit_code 3 on stream.closed in --json mode', async () => {
      const deps = createMockDeps({
        connectSSE: jest.fn().mockImplementation(
          (_buildId: string, onEvent: (event: SSEEvent) => void) => {
            onEvent({
              type: 'stream.closed',
              data: {},
            });
            return { close: jest.fn() };
          }
        ),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(3);
      const output = (deps.writeTo as jest.Mock).mock.calls[0][0];
      const parsed = JSON.parse(output);
      expect(parsed.code).toBe('STREAM_CLOSED');
      expect(parsed.exit_code).toBe(3);
      expect(parsed.build_id).toBe('build-deadbeef');
    });
  });

  describe('worker app display', () => {
    it('shows worker app in deploy result', async () => {
      const deps = createMockDeps({
        connectSSE: jest.fn().mockImplementation(
          (_buildId: string, onEvent: (event: SSEEvent) => void) => {
            onEvent({
              type: 'deploy.completed',
              data: {
                deploy_id: 'd-8f3a',
                status: 'success',
                duration_seconds: 45,
                apps: [{ name: 'worker', status: 'deployed', build_duration_seconds: 20 }],
              },
            });
            return { close: jest.fn() };
          }
        ),
      });
      const cmd = new DeployCommand(deps);
      await cmd.execute({});
      const allOutput = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(allOutput).toContain('worker');
      expect(allOutput).toContain('Deploy completed');
    });
  });

  describe('lockfile pre-check (F-03)', () => {
    it('should warn when no lockfile exists (human mode)', async () => {
      const deps = createMockDeps({
        hasLockfile: jest.fn().mockReturnValue(false),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({});

      // Should still deploy (warning, not blocking), but emit a warning
      expect(deps.writeTo).toHaveBeenCalledWith(
        expect.stringContaining('lockfile')
      );
    });

    it('should not warn when lockfile exists', async () => {
      const deps = createMockDeps({
        hasLockfile: jest.fn().mockReturnValue(true),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(0);
      const allOutput = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(allOutput).not.toContain('lockfile');
    });

    it('should not warn in --json mode', async () => {
      const deps = createMockDeps({
        hasLockfile: jest.fn().mockReturnValue(false),
      });
      const cmd = new DeployCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      // In JSON mode, only emit JSON objects — no warning text
      const allOutput = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      if (allOutput.includes('lockfile')) {
        // If mentioned, it must be inside JSON
        expect(() => JSON.parse(allOutput)).not.toThrow();
      }
    });
  });
});
