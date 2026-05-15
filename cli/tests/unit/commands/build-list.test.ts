import { BuildListCommand, BuildListDeps } from '../../../src/commands/build-list';
import { ApiError } from '../../../src/api/client';

const sampleBuilds = [
  {
    build_id: 'bld-abc12345',
    project: 'my-project',
    environment: 'production',
    status: 'SUCCEEDED',
    commit_sha: 'f3c9a01',
    created_at: '2026-03-31T14:23:01Z',
    duration_seconds: 24,
  },
  {
    build_id: 'bld-def67890',
    project: 'my-project',
    environment: 'production',
    status: 'FAILED',
    commit_sha: 'b2a8c12',
    created_at: '2026-03-31T13:50:44Z',
    duration_seconds: 18,
  },
];

function createMockDeps(overrides: Partial<BuildListDeps> = {}): BuildListDeps {
  return {
    getToken: jest.fn().mockReturnValue('test-token'),
    findLoopZeroYaml: jest.fn().mockReturnValue({
      path: '/app/loopzero.yaml',
      content: { project: 'my-project', apps: {} },
    }),
    listBuilds: jest.fn().mockResolvedValue({ builds: sampleBuilds, total: 2 }),
    writeTo: jest.fn(),
    writeError: jest.fn(),
    ...overrides,
  };
}

describe('BuildListCommand', () => {
  describe('success with builds', () => {
    it('should list builds and return exit code 0', async () => {
      const deps = createMockDeps();
      const cmd = new BuildListCommand(deps);

      const exitCode = await cmd.execute();

      expect(exitCode).toBe(0);
      expect(deps.listBuilds).toHaveBeenCalledWith('my-project', undefined, undefined);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('bld-abc12345');
      expect(output).toContain('SUCCEEDED');
      expect(output).toContain('f3c9a01');
      expect(output).toContain('24s');
      expect(output).toContain('bld-def67890');
      expect(output).toContain('FAILED');
      expect(output).toContain('2 builds');
    });

    it('should format table with headers', async () => {
      const deps = createMockDeps();
      const cmd = new BuildListCommand(deps);

      await cmd.execute();

      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('ID');
      expect(output).toContain('STATUS');
      // CHG-2026-04-23-AUDIT-5: rename header "VERSION" → "COMMIT" (real content
      // is a truncated commit_sha, not a semver).
      expect(output).toContain('COMMIT');
      expect(output).not.toContain('VERSION');
    });
  });

  // CHG-2026-04-23-AUDIT-5: build IDs are 22 chars (build-<16 hex>), but the
  // column pad is 16 — causing alignment to break. Display should truncate.
  // Full ID is still available via --json.
  describe('ID column layout (AUDIT-5)', () => {
    it('truncates long build_id in human output so table alignment is preserved', async () => {
      const deps = createMockDeps({
        listBuilds: jest.fn().mockResolvedValue({
          builds: [
            {
              build_id: 'build-95cb34790df47867',
              project_id: 'e2e-test-hello',
              environment: 'production',
              status: 'SUCCEEDED',
              commit_sha: '3792658fac8e66da9fcfac910882091d3978059e',
              created_by: 'loopzero-e2e',
              created_at: '2026-04-23T14:27:23Z',
              duration_seconds: 187,
            },
          ],
          total: 1,
        }),
      });
      const cmd = new BuildListCommand(deps);
      await cmd.execute();
      const lines = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]);
      const dataLine = lines.find((l: string) => /build-/.test(l) && /SUCCEEDED/.test(l))!;
      expect(dataLine).toBeDefined();
      // Header row should align with data row: STATUS should start at the same column.
      const headerLine = lines.find((l: string) => l.includes('ID') && l.includes('STATUS'))!;
      const headerStatusCol = headerLine.indexOf('STATUS');
      const dataStatusCol = dataLine.indexOf('SUCCEEDED');
      expect(dataStatusCol).toBe(headerStatusCol);
    });

    it('keeps full build_id available in --json output', async () => {
      const fullId = 'build-95cb34790df47867';
      const deps = createMockDeps({
        listBuilds: jest.fn().mockResolvedValue({
          builds: [{
            build_id: fullId,
            project_id: 'p', environment: 'production', status: 'SUCCEEDED',
            commit_sha: 'deadbeef', created_by: 'u', created_at: '2026-04-23T00:00:00Z',
          }],
          total: 1,
        }),
      });
      const cmd = new BuildListCommand(deps);
      await cmd.execute({ json: true });
      const parsed = JSON.parse((deps.writeTo as jest.Mock).mock.calls[0][0]);
      expect(parsed.builds[0].build_id).toBe(fullId);
    });
  });

  describe('empty list', () => {
    it('should show no builds message and return exit code 0', async () => {
      const deps = createMockDeps({
        listBuilds: jest.fn().mockResolvedValue({ builds: [], total: 0 }),
      });
      const cmd = new BuildListCommand(deps);

      const exitCode = await cmd.execute();

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('No builds found');
    });
  });

  describe('--json output', () => {
    it('should return JSON with builds array and total', async () => {
      const deps = createMockDeps();
      const cmd = new BuildListCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls[0][0];
      const parsed = JSON.parse(output);
      expect(parsed.builds).toHaveLength(2);
      expect(parsed.total).toBe(2);
      expect(parsed.builds[0].build_id).toBe('bld-abc12345');
    });

    it('should return empty JSON for no builds', async () => {
      const deps = createMockDeps({
        listBuilds: jest.fn().mockResolvedValue({ builds: [], total: 0 }),
      });
      const cmd = new BuildListCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls[0][0];
      const parsed = JSON.parse(output);
      expect(parsed.builds).toHaveLength(0);
      expect(parsed.total).toBe(0);
    });
  });

  describe('--env filter', () => {
    it('should pass env filter to API', async () => {
      const deps = createMockDeps();
      const cmd = new BuildListCommand(deps);

      await cmd.execute({ env: 'staging' });

      expect(deps.listBuilds).toHaveBeenCalledWith('my-project', 'staging', undefined);
    });
  });

  describe('--limit option', () => {
    it('should pass limit to API', async () => {
      const deps = createMockDeps();
      const cmd = new BuildListCommand(deps);

      await cmd.execute({ limit: 5 });

      expect(deps.listBuilds).toHaveBeenCalledWith('my-project', undefined, 5);
    });
  });

  describe('auth errors', () => {
    it('should return exit code 4 when not authenticated', async () => {
      const deps = createMockDeps({ getToken: jest.fn().mockReturnValue(null) });
      const cmd = new BuildListCommand(deps);

      const exitCode = await cmd.execute();

      expect(exitCode).toBe(4);
      expect(deps.writeError).toHaveBeenCalledWith('Not authenticated. Run: loopzero login');
      expect(deps.listBuilds).not.toHaveBeenCalled();
    });

    it('should return JSON error on auth failure with --json', async () => {
      const deps = createMockDeps({ getToken: jest.fn().mockReturnValue(null) });
      const cmd = new BuildListCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(4);
      const output = (deps.writeTo as jest.Mock).mock.calls[0][0];
      const parsed = JSON.parse(output);
      expect(parsed.code).toBe('AUTH_REQUIRED');
      expect(parsed.exit_code).toBe(4);
    });

    it('should return exit code 4 on API 401', async () => {
      const deps = createMockDeps({
        listBuilds: jest.fn().mockRejectedValue(new ApiError('Unauthorized', 4, 401, 'AUTH_REQUIRED')),
      });
      const cmd = new BuildListCommand(deps);

      const exitCode = await cmd.execute();

      expect(exitCode).toBe(4);
    });
  });

  // CHG-2026-04-23-AUDIT-24: same rationale as team-list / project-list.
  describe('API unreachable (AUDIT-24)', () => {
    it('network error produces API_UNREACHABLE code + Check LOOPZERO_URL hint', async () => {
      const deps = createMockDeps({
        listBuilds: jest.fn().mockRejectedValue(new Error('fetch failed')),
      });
      const cmd = new BuildListCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(1);
      const parsed = JSON.parse((deps.writeTo as jest.Mock).mock.calls[0][0]);
      expect(parsed.code).toBe('API_UNREACHABLE');
      expect(parsed.error).toContain('Cannot reach API');
      expect(parsed.error).toContain('Check LOOPZERO_URL');
    });

    it('non-network Error keeps UNEXPECTED_ERROR code', async () => {
      const deps = createMockDeps({
        listBuilds: jest.fn().mockRejectedValue(new Error('random logic error')),
      });
      const cmd = new BuildListCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(1);
      const parsed = JSON.parse((deps.writeTo as jest.Mock).mock.calls[0][0]);
      expect(parsed.code).toBe('UNEXPECTED_ERROR');
    });
  });

  describe('no loopzero.yaml', () => {
    it('should return exit code 1 when no loopzero.yaml found', async () => {
      const deps = createMockDeps({ findLoopZeroYaml: jest.fn().mockReturnValue(null) });
      const cmd = new BuildListCommand(deps);

      const exitCode = await cmd.execute();

      expect(exitCode).toBe(1);
      expect(deps.writeError).toHaveBeenCalledWith('No loopzero.yaml found. Run: loopzero init (or pass --project <name>)');
      expect(deps.listBuilds).not.toHaveBeenCalled();
    });

    // Plan T2.3 D6 — --project flag overrides loopzero.yaml lookup.
    it('test_build_list_with_project_flag_filters', async () => {
      const deps = createMockDeps({ findLoopZeroYaml: jest.fn().mockReturnValue(null) });
      const cmd = new BuildListCommand(deps);

      const exitCode = await cmd.execute({ project: 'override-project' });

      expect(exitCode).toBe(0);
      expect(deps.findLoopZeroYaml).not.toHaveBeenCalled(); // --project bypasses yaml lookup
      expect(deps.listBuilds).toHaveBeenCalledWith('override-project', undefined, undefined);
    });

    it('should return exit code 1 when loopzero.yaml has parse error', async () => {
      const deps = createMockDeps({
        findLoopZeroYaml: jest.fn().mockReturnValue({
          path: '/app/loopzero.yaml',
          content: { project: 'test', apps: {} },
          parseError: 'bad yaml',
        }),
      });
      const cmd = new BuildListCommand(deps);

      const exitCode = await cmd.execute();

      expect(exitCode).toBe(1);
      expect(deps.writeError).toHaveBeenCalledWith('Invalid loopzero.yaml: bad yaml');
    });
  });

  describe('server errors', () => {
    it('should return exit code 1 on API error', async () => {
      const deps = createMockDeps({
        listBuilds: jest.fn().mockRejectedValue(new ApiError('Internal error', 3, 500, 'INTERNAL_ERROR')),
      });
      const cmd = new BuildListCommand(deps);

      const exitCode = await cmd.execute();

      expect(exitCode).toBe(1);
      const errorMsg = (deps.writeError as jest.Mock).mock.calls[0][0];
      expect(errorMsg).toContain('Try again');
    });

    it('should handle non-ApiError exceptions', async () => {
      const deps = createMockDeps({
        listBuilds: jest.fn().mockRejectedValue(new Error('Network timeout')),
      });
      const cmd = new BuildListCommand(deps);

      const exitCode = await cmd.execute();

      expect(exitCode).toBe(1);
    });
  });

  describe('builds without duration', () => {
    it('should show dash for missing duration', async () => {
      const deps = createMockDeps({
        listBuilds: jest.fn().mockResolvedValue({
          builds: [{
            build_id: 'bld-running1',
            project: 'my-project',
            environment: 'production',
            status: 'RUNNING',
            commit_sha: 'abc1234',
            created_at: '2026-03-31T14:00:00Z',
          }],
          total: 1,
        }),
      });
      const cmd = new BuildListCommand(deps);

      await cmd.execute();

      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('-');
    });
  });

  describe('Rule 3 compliance', () => {
    it('should never contain internal terms in output', async () => {
      const deps = createMockDeps();
      const cmd = new BuildListCommand(deps);

      await cmd.execute();

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
