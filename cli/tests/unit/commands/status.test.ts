// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { StatusCommand, StatusDeps } from '../../../src/commands/status';
import { ApiError } from '../../../src/api/client';

function createMockDeps(overrides: Partial<StatusDeps> = {}): StatusDeps {
  return {
    getToken: jest.fn().mockReturnValue('test-token'),
    findLoopZeroYaml: jest.fn().mockReturnValue({
      path: '/project/loopzero.yaml',
      content: { project: 'my-app', apps: { web: { path: '.', framework: 'express', port: 3000 } } },
    }),
    getStatus: jest.fn().mockResolvedValue({
      environment: 'production',
      last_build: {
        id: 'build-a1b2c3d4',
        status: 'SUCCEEDED',
        finished_at: '2026-03-25T12:30:00Z',
      },
      // CHG-2026-04-23-AUDIT-33: shape matches API
      // (api/internal/routes/status.go activeReleaseResponse):
      // id, status, git_commit, created_at.
      active_release: {
        id: 'rel-e5f6g7h8',
        status: 'ACTIVE',
        git_commit: 'deadbeef',
        created_at: '2026-03-25T12:30:00Z',
      },
      sync_status: 'Synced',
      url: 'https://my-app.dev.loopzero.example',
    }),
    writeTo: jest.fn(),
    writeError: jest.fn(),
    ...overrides,
  };
}

describe('StatusCommand', () => {
  describe('success', () => {
    it('should display environment status and return exit code 0', async () => {
      const deps = createMockDeps();
      const cmd = new StatusCommand(deps);

      const exitCode = await cmd.execute({ env: 'production' });

      expect(exitCode).toBe(0);
      expect(deps.getStatus).toHaveBeenCalledWith('my-app', 'production');
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('production');
      expect(output).toContain('build-a1b2c3d4');
      expect(output).toContain('SUCCEEDED');
      expect(output).toContain('rel-e5f6g7h8');
      expect(output).toContain('https://my-app.dev.loopzero.example');
    });

    it('should default to production environment', async () => {
      const deps = createMockDeps();
      const cmd = new StatusCommand(deps);

      await cmd.execute({});

      expect(deps.getStatus).toHaveBeenCalledWith('my-app', 'production');
    });

    it('should display no-deploys message when no builds exist', async () => {
      const deps = createMockDeps({
        getStatus: jest.fn().mockResolvedValue({
          environment: 'production',
        }),
      });
      const cmd = new StatusCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('production');
      expect(output).toContain('No deploys yet');
      expect(output).toContain('loopzero deploy');
    });
  });

  describe('sync_status mapping', () => {
    it('should map Synced to "Up to date" in human output', async () => {
      const deps = createMockDeps();
      const cmd = new StatusCommand(deps);
      await cmd.execute({});
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('Up to date');
      expect(output).not.toContain('Synced');
    });

    it('should map OutOfSync to "Update pending"', async () => {
      const deps = createMockDeps({
        getStatus: jest.fn().mockResolvedValue({
          environment: 'production',
          last_build: { id: 'b-1', status: 'SUCCEEDED', finished_at: '2026-03-25T12:30:00Z' },
          sync_status: 'OutOfSync',
        }),
      });
      const cmd = new StatusCommand(deps);
      await cmd.execute({});
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('Update pending');
    });

    it('should map Progressing to "Deploying"', async () => {
      const deps = createMockDeps({
        getStatus: jest.fn().mockResolvedValue({
          environment: 'production',
          last_build: { id: 'b-1', status: 'SUCCEEDED', finished_at: '2026-03-25T12:30:00Z' },
          sync_status: 'Progressing',
        }),
      });
      const cmd = new StatusCommand(deps);
      await cmd.execute({});
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('Deploying');
    });

    it('should map sync_status in JSON output too', async () => {
      const deps = createMockDeps({
        getStatus: jest.fn().mockResolvedValue({
          environment: 'production',
          sync_status: 'Synced',
        }),
      });
      const cmd = new StatusCommand(deps);
      await cmd.execute({ json: true });
      const parsed = JSON.parse((deps.writeTo as jest.Mock).mock.calls[0][0]);
      expect(parsed.sync_status).toBe('Up to date');
    });
  });

  describe('--json flag', () => {
    it('should output raw API response as JSON', async () => {
      const apiResponse = {
        environment: 'production',
        last_build: {
          id: 'build-a1b2c3d4',
          status: 'SUCCEEDED',
          finished_at: '2026-03-25T12:30:00Z',
        },
        active_release: {
          id: 'rel-e5f6g7h8',
          status: 'ACTIVE',
          git_commit: 'deadbeef',
          created_at: '2026-03-25T12:30:00Z',
        },
        sync_status: 'Synced',
        url: 'https://my-app.dev.loopzero.example',
      };
      const deps = createMockDeps({
        getStatus: jest.fn().mockResolvedValue(apiResponse),
      });
      const cmd = new StatusCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.environment).toBe('production');
      expect(parsed.last_build.id).toBe('build-a1b2c3d4');
      expect(parsed.active_release.id).toBe('rel-e5f6g7h8');
      expect(parsed.active_release.status).toBe('ACTIVE');
      expect(parsed.url).toBe('https://my-app.dev.loopzero.example');
    });

    it('should output JSON for no-deploys state', async () => {
      const deps = createMockDeps({
        getStatus: jest.fn().mockResolvedValue({ environment: 'production' }),
      });
      const cmd = new StatusCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.environment).toBe('production');
    });
  });

  describe('auth failure', () => {
    it('should return exit code 4 when not authenticated', async () => {
      const deps = createMockDeps({
        getToken: jest.fn().mockReturnValue(null),
      });
      const cmd = new StatusCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(4);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('Not authenticated');
      expect(errOutput).toContain('loopzero login');
    });

    it('should return exit code 4 with JSON when not authenticated', async () => {
      const deps = createMockDeps({
        getToken: jest.fn().mockReturnValue(null),
      });
      const cmd = new StatusCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(4);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.error).toContain('Not authenticated');
      expect(parsed.exit_code).toBe(4);
      expect(parsed.code).toBe('AUTH_REQUIRED');
    });
  });

  describe('missing loopzero.yaml', () => {
    it('should return exit code 1 when loopzero.yaml not found', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue(null),
      });
      const cmd = new StatusCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(1);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('loopzero.yaml');
      expect(errOutput).toContain('loopzero init');
    });

    it('should return exit code 1 when loopzero.yaml has parse error', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue({
          path: '/project/loopzero.yaml',
          content: { project: 'my-app', apps: {} },
          parseError: 'Invalid YAML syntax',
        }),
      });
      const cmd = new StatusCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(1);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('Invalid loopzero.yaml');
    });

    it('should return exit code 1 with JSON when loopzero.yaml not found', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue(null),
      });
      const cmd = new StatusCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(1);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
      const parsed = JSON.parse(output);
      expect(parsed.exit_code).toBe(1);
      expect(parsed.code).toBe('MISSING_CONFIG');
    });
  });

  describe('API error handling', () => {
    it('should handle API errors gracefully', async () => {
      const deps = createMockDeps({
        getStatus: jest.fn().mockRejectedValue(
          new ApiError('Service unavailable. Try again in a moment.', 3, 503, 'SERVICE_UNAVAILABLE'),
        ),
      });
      const cmd = new StatusCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(3);
    });

    it('should return exit 4 on 401 API error', async () => {
      const deps = createMockDeps({
        getStatus: jest.fn().mockRejectedValue(
          new ApiError('Not authenticated. Run: loopzero login', 4, 401, 'AUTH_REQUIRED'),
        ),
      });
      const cmd = new StatusCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(4);
    });

    it('should handle unexpected non-ApiError with exit code 3', async () => {
      const deps = createMockDeps({
        getStatus: jest.fn().mockRejectedValue(new Error('Connection refused')),
      });
      const cmd = new StatusCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(3);
      const errOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
      expect(errOutput).toContain('Connection refused');
    });

    it('should handle non-Error thrown values with exit code 3', async () => {
      const deps = createMockDeps({
        getStatus: jest.fn().mockRejectedValue('network failure'),
      });
      const cmd = new StatusCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(3);
    });
  });

  // CHG-2026-04-23-AUDIT-24: network errors now carry code=API_UNREACHABLE
  // with "Cannot reach API: ... Check LOOPZERO_URL." so operators spot config
  // errors instead of retrying blindly. Exit code stays 3 (contract).
  describe('API unreachable (AUDIT-24)', () => {
    it('network error produces API_UNREACHABLE code + Check LOOPZERO_URL hint', async () => {
      const deps = createMockDeps({
        getStatus: jest.fn().mockRejectedValue(new Error('fetch failed')),
      });
      const cmd = new StatusCommand(deps);
      const exitCode = await cmd.execute({ json: true });
      expect(exitCode).toBe(3);
      const parsed = JSON.parse((deps.writeTo as jest.Mock).mock.calls[0][0]);
      expect(parsed.code).toBe('API_UNREACHABLE');
      expect(parsed.error).toContain('Cannot reach API');
      expect(parsed.error).toContain('Check LOOPZERO_URL');
    });

    it('non-network Error keeps UNEXPECTED_ERROR code', async () => {
      const deps = createMockDeps({
        getStatus: jest.fn().mockRejectedValue(new Error('arbitrary failure')),
      });
      const cmd = new StatusCommand(deps);
      const exitCode = await cmd.execute({ json: true });
      expect(exitCode).toBe(3);
      const parsed = JSON.parse((deps.writeTo as jest.Mock).mock.calls[0][0]);
      expect(parsed.code).toBe('UNEXPECTED_ERROR');
    });
  });

  describe('no internal terms', () => {
    it('should not contain forbidden terms in any output', async () => {
      const forbidden = [
        'ArgoCD', 'argocd', 'GitOps', 'gitops-push', 'CRD',
        'tenant_id', 'advisory lock', 'pg_advisory', 'Gitea',
        'MinIO', 'kubebuilder', 'envtest', 'Kaniko',
      ];

      // Success scenario
      const deps1 = createMockDeps();
      const cmd1 = new StatusCommand(deps1);
      await cmd1.execute({});

      // No deploys scenario
      const deps2 = createMockDeps({
        getStatus: jest.fn().mockResolvedValue({ environment: 'production' }),
      });
      const cmd2 = new StatusCommand(deps2);
      await cmd2.execute({});

      // Auth failure scenario
      const deps3 = createMockDeps({
        getToken: jest.fn().mockReturnValue(null),
      });
      const cmd3 = new StatusCommand(deps3);
      await cmd3.execute({});

      // Missing config scenario
      const deps4 = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue(null),
      });
      const cmd4 = new StatusCommand(deps4);
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

  // CHG-2026-04-23-AUDIT-2: CLI formerly expected last_build.build_id, release,
  // and displayed literal "unknown" for sync_status. Actual API returns
  // last_build.id, active_release, and sync_status="unknown" when unknown.
  describe('API contract alignment (AUDIT-2)', () => {
    it('renders last_build.id (not build_id) so users see the real build ID', async () => {
      const deps = createMockDeps({
        getStatus: jest.fn().mockResolvedValue({
          environment: 'production',
          last_build: {
            id: 'build-95cb34790df47867',
            status: 'SUCCEEDED',
            finished_at: '2026-04-23T14:30:31Z',
          },
          active_release: null,
          sync_status: 'unknown',
          url: 'https://e2e-test-hello-production.loopzero.local',
        }),
      });
      const cmd = new StatusCommand(deps);
      await cmd.execute({ env: 'production' });
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('build-95cb34790df47867');
      expect(output).not.toContain('undefined');
    });

    it('does not display "Sync: unknown" literal when sync_status is unknown', async () => {
      const deps = createMockDeps({
        getStatus: jest.fn().mockResolvedValue({
          environment: 'production',
          last_build: { id: 'b-1', status: 'SUCCEEDED', finished_at: '2026-04-23T14:30:31Z' },
          active_release: null,
          sync_status: 'unknown',
          url: 'https://e2e-test-hello-production.loopzero.local',
        }),
      });
      const cmd = new StatusCommand(deps);
      await cmd.execute({ env: 'production' });
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).not.toMatch(/Sync:\s+unknown/);
    });

    it('renders active_release (not release) so release info is visible', async () => {
      const deps = createMockDeps({
        getStatus: jest.fn().mockResolvedValue({
          environment: 'production',
          last_build: { id: 'b-1', status: 'SUCCEEDED', finished_at: '2026-04-23T14:30:31Z' },
          active_release: {
            id: 'rel-deadbeef',
            status: 'ACTIVE',
            git_commit: 'beefcafe',
            created_at: '2026-04-23T14:30:31Z',
          },
          sync_status: 'Synced',
          url: 'https://app.loopzero.local',
        }),
      });
      const cmd = new StatusCommand(deps);
      await cmd.execute({ env: 'production' });
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('rel-deadbeef');
    });

    // CHG-2026-04-23-AUDIT-33: active_release uses API field names
    // (id, status) — previous interface declared release_id + state,
    // causing `state.toLowerCase()` to crash at runtime against the
    // real API response. Regression guard.
    it('does NOT crash on toLowerCase when reading active_release.status', async () => {
      const deps = createMockDeps({
        getStatus: jest.fn().mockResolvedValue({
          environment: 'production',
          last_build: { id: 'b-2', status: 'SUCCEEDED', finished_at: '2026-04-23T21:00:00Z' },
          active_release: {
            id: 'rel-abc',
            status: 'ACTIVE',
            git_commit: 'deadbeef',
            created_at: '2026-04-23T21:00:00Z',
          },
          sync_status: 'Synced',
          url: 'https://x.loopzero.local',
        }),
      });
      const cmd = new StatusCommand(deps);
      await expect(cmd.execute({ env: 'production' })).resolves.toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('rel-abc');
      expect(output).toContain('active');  // lowercase rendering
      const errs = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errs).not.toContain('Cannot read properties');
    });
  });

  // KEDA scale-to-zero: scaling field in status response
  describe('scaling display (KEDA scale-to-zero)', () => {
    it('shows "sleeping" when scaled_to_zero is true', async () => {
      const deps = createMockDeps({
        getStatus: jest.fn().mockResolvedValue({
          environment: 'production',
          last_build: { id: 'b-1', status: 'SUCCEEDED', finished_at: '2026-04-25T10:00:00Z' },
          active_release: { id: 'rel-1', status: 'ACTIVE', git_commit: 'abc', created_at: '2026-04-25T10:00:00Z' },
          scaling: { replicas: 0, min: 0, max: 5, scaled_to_zero: true },
          url: 'https://my-app-production.dev.loopzero.example',
        }),
      });
      const cmd = new StatusCommand(deps);
      const exitCode = await cmd.execute({});
      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('sleeping');
      expect(output).toContain('0/5 replicas');
      expect(output).toContain('wakes up in ~5-10s');
    });

    it('shows normal replica count when not scaled to zero', async () => {
      const deps = createMockDeps({
        getStatus: jest.fn().mockResolvedValue({
          environment: 'production',
          last_build: { id: 'b-1', status: 'SUCCEEDED', finished_at: '2026-04-25T10:00:00Z' },
          active_release: { id: 'rel-1', status: 'ACTIVE', git_commit: 'abc', created_at: '2026-04-25T10:00:00Z' },
          scaling: { replicas: 3, min: 1, max: 10, scaled_to_zero: false },
          url: 'https://my-app-production.dev.loopzero.example',
        }),
      });
      const cmd = new StatusCommand(deps);
      const exitCode = await cmd.execute({});
      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('3/10 replicas');
      expect(output).not.toContain('sleeping');
      expect(output).not.toContain('wakes up');
    });

    it('does not show scaling section when scaling is absent', async () => {
      const deps = createMockDeps();
      const cmd = new StatusCommand(deps);
      await cmd.execute({});
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).not.toContain('sleeping');
      expect(output).not.toContain('replicas');
    });

    it('includes scaling in --json output', async () => {
      const deps = createMockDeps({
        getStatus: jest.fn().mockResolvedValue({
          environment: 'production',
          scaling: { replicas: 0, min: 0, max: 5, scaled_to_zero: true },
        }),
      });
      const cmd = new StatusCommand(deps);
      await cmd.execute({ json: true });
      const parsed = JSON.parse((deps.writeTo as jest.Mock).mock.calls[0][0]);
      expect(parsed.scaling).toBeDefined();
      expect(parsed.scaling.scaled_to_zero).toBe(true);
      expect(parsed.scaling.replicas).toBe(0);
      expect(parsed.scaling.max).toBe(5);
    });

    it('does not contain internal terms (KEDA, ScaledObject, HTTPScaledObject)', async () => {
      const deps = createMockDeps({
        getStatus: jest.fn().mockResolvedValue({
          environment: 'production',
          last_build: { id: 'b-1', status: 'SUCCEEDED', finished_at: '2026-04-25T10:00:00Z' },
          scaling: { replicas: 0, min: 0, max: 5, scaled_to_zero: true },
        }),
      });
      const cmd = new StatusCommand(deps);
      await cmd.execute({});
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).not.toContain('KEDA');
      expect(output).not.toContain('ScaledObject');
      expect(output).not.toContain('HTTPScaledObject');
    });
  });
});
