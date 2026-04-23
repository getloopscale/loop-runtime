import {
  PreviewListCommand,
  PreviewInspectCommand,
  PreviewDeleteCommand,
  PreviewDeps,
} from '../../../src/commands/preview';
import { ApiError } from '../../../src/api/client';

function createMockDeps(overrides: Partial<PreviewDeps> = {}): PreviewDeps {
  return {
    getToken: jest.fn().mockReturnValue('test-token'),
    findLoopZeroYaml: jest.fn().mockReturnValue({
      path: '/project/loopzero.yaml',
      content: { project: 'my-app', apps: { web: { path: '.', framework: 'express', port: 3000 } } },
    }),
    listPreviews: jest.fn().mockResolvedValue({
      previews: [
        {
          id: 'prev-abc123',
          pr_number: 42,
          branch: 'feat/new-feature',
          url: 'https://pr-42-my-app.dev.loopzero.example',
          status: 'active',
          created_at: new Date(Date.now() - 3600000).toISOString(),
        },
        {
          id: 'prev-def456',
          pr_number: 43,
          branch: 'fix/bug',
          url: 'https://pr-43-my-app.dev.loopzero.example',
          status: 'active',
          created_at: new Date(Date.now() - 86400000 * 2).toISOString(),
        },
      ],
    }),
    inspectPreview: jest.fn().mockResolvedValue({
      id: 'prev-abc123',
      pr_number: 42,
      branch: 'feat/new-feature',
      url: 'https://pr-42-my-app.dev.loopzero.example',
      status: 'active',
      created_at: '2026-04-20T10:00:00Z',
      updated_at: '2026-04-20T10:05:00Z',
      apps: [
        { name: 'web', status: 'running', url: 'https://pr-42-my-app.dev.loopzero.example' },
      ],
      ttl_expires_at: '2026-04-27T10:00:00Z',
    }),
    deletePreview: jest.fn().mockResolvedValue({ id: 'prev-abc123', status: 'deleting' }),
    writeTo: jest.fn(),
    writeError: jest.fn(),
    ...overrides,
  };
}

describe('PreviewListCommand', () => {
  describe('success', () => {
    it('should list previews and return exit code 0', async () => {
      const deps = createMockDeps();
      const cmd = new PreviewListCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(0);
      expect(deps.listPreviews).toHaveBeenCalledWith('my-app');
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('prev-abc123');
      expect(output).toContain('PR #42');
      expect(output).toContain('prev-def456');
    });

    it('should show empty message when no previews exist', async () => {
      const deps = createMockDeps({
        listPreviews: jest.fn().mockResolvedValue({ previews: [] }),
      });
      const cmd = new PreviewListCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('No preview environments found');
    });
  });

  describe('json output', () => {
    it('should return JSON with correct schema', async () => {
      const deps = createMockDeps();
      const cmd = new PreviewListCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls[0][0];
      const parsed = JSON.parse(output);
      expect(parsed.count).toBe(2);
      expect(parsed.previews).toHaveLength(2);
      expect(parsed.previews[0].id).toBe('prev-abc123');
      expect(parsed.previews[0].pr_number).toBe(42);
    });
  });

  describe('auth errors', () => {
    it('should return exit code 4 when not authenticated', async () => {
      const deps = createMockDeps({ getToken: jest.fn().mockReturnValue(null) });
      const cmd = new PreviewListCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(4);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('loopzero login');
    });
  });

  describe('missing config', () => {
    it('should return exit code 1 when loopzero.yaml is missing', async () => {
      const deps = createMockDeps({ findLoopZeroYaml: jest.fn().mockReturnValue(null) });
      const cmd = new PreviewListCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(1);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('loopzero init');
    });
  });

  describe('no internal terms', () => {
    it('should not contain forbidden terms in any output', async () => {
      const forbidden = [
        'ArgoCD', 'argocd', 'GitOps', 'gitops-push', 'CRD',
        'tenant_id', 'advisory lock', 'pg_advisory', 'Gitea',
        'MinIO', 'kubebuilder', 'envtest', 'Kaniko',
      ];

      const deps = createMockDeps();
      const cmd = new PreviewListCommand(deps);
      await cmd.execute({});

      const allOutput = [
        ...(deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]),
        ...(deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]),
      ].join('\n');

      for (const term of forbidden) {
        expect(allOutput).not.toContain(term);
      }
    });
  });
});

describe('PreviewInspectCommand', () => {
  describe('success', () => {
    it('should show preview details and return exit code 0', async () => {
      const deps = createMockDeps();
      const cmd = new PreviewInspectCommand(deps);

      const exitCode = await cmd.execute('prev-abc123');

      expect(exitCode).toBe(0);
      expect(deps.inspectPreview).toHaveBeenCalledWith('my-app', 'prev-abc123');
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('prev-abc123');
      expect(output).toContain('#42');
      expect(output).toContain('feat/new-feature');
      expect(output).toContain('web');
    });
  });

  describe('json output', () => {
    it('should return JSON with full details', async () => {
      const deps = createMockDeps();
      const cmd = new PreviewInspectCommand(deps);

      const exitCode = await cmd.execute('prev-abc123', { json: true });

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls[0][0];
      const parsed = JSON.parse(output);
      expect(parsed.id).toBe('prev-abc123');
      expect(parsed.branch).toBe('feat/new-feature');
      expect(parsed.apps).toHaveLength(1);
    });
  });

  describe('auth errors', () => {
    it('should return exit code 4 when not authenticated', async () => {
      const deps = createMockDeps({ getToken: jest.fn().mockReturnValue(null) });
      const cmd = new PreviewInspectCommand(deps);

      const exitCode = await cmd.execute('prev-abc123');

      expect(exitCode).toBe(4);
    });
  });

  describe('not found', () => {
    // CHG-2026-04-23-AUDIT-15: Rule 4 mandates exit 1 for usage errors
    // (including NOT_FOUND on lookups). Exit 3 is "deploy failure" — wrong
    // semantics for "preview doesn't exist". Other NOT_FOUND commands in the
    // CLI (project inspect, env inspect, build inspect) use exit 1.
    it('should return exit code 1 when preview not found', async () => {
      const deps = createMockDeps({
        inspectPreview: jest.fn().mockRejectedValue(new ApiError('Not found', 3, 404, 'NOT_FOUND')),
      });
      const cmd = new PreviewInspectCommand(deps);

      const exitCode = await cmd.execute('prev-nonexistent');

      expect(exitCode).toBe(1);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('not found');
    });
  });

  describe('missing argument', () => {
    it('should return exit code 1 when ID is empty', async () => {
      const deps = createMockDeps();
      const cmd = new PreviewInspectCommand(deps);

      const exitCode = await cmd.execute('');

      expect(exitCode).toBe(1);
    });
  });
});

describe('PreviewDeleteCommand', () => {
  describe('success', () => {
    it('should delete preview and return exit code 0', async () => {
      const deps = createMockDeps();
      const cmd = new PreviewDeleteCommand(deps);

      const exitCode = await cmd.execute('prev-abc123', { yes: true });

      expect(exitCode).toBe(0);
      expect(deps.deletePreview).toHaveBeenCalledWith('my-app', 'prev-abc123');
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('being deleted');
    });
  });

  describe('json output', () => {
    it('should return JSON on success', async () => {
      const deps = createMockDeps();
      const cmd = new PreviewDeleteCommand(deps);

      const exitCode = await cmd.execute('prev-abc123', { json: true, yes: true });

      expect(exitCode).toBe(0);
      const output = (deps.writeTo as jest.Mock).mock.calls[0][0];
      const parsed = JSON.parse(output);
      expect(parsed.ok).toBe(true);
      expect(parsed.preview_id).toBe('prev-abc123');
      expect(parsed.status).toBe('deleting');
    });
  });

  describe('auth errors', () => {
    it('should return exit code 4 when not authenticated', async () => {
      const deps = createMockDeps({ getToken: jest.fn().mockReturnValue(null) });
      const cmd = new PreviewDeleteCommand(deps);

      const exitCode = await cmd.execute('prev-abc123', { yes: true });

      expect(exitCode).toBe(4);
    });
  });

  describe('confirmation', () => {
    it('should skip API call when user declines confirmation', async () => {
      const deps = createMockDeps({
        confirm: jest.fn().mockResolvedValue(false),
      });
      const cmd = new PreviewDeleteCommand(deps);

      const exitCode = await cmd.execute('prev-abc123');

      expect(exitCode).toBe(0);
      expect(deps.deletePreview).not.toHaveBeenCalled();
      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('cancelled');
    });

    it('should require --yes in CI mode', async () => {
      const deps = createMockDeps({ isCI: true });
      const cmd = new PreviewDeleteCommand(deps);

      const exitCode = await cmd.execute('prev-abc123');

      expect(exitCode).toBe(1);
      expect(deps.deletePreview).not.toHaveBeenCalled();
    });
  });

  describe('API errors', () => {
    // CHG-2026-04-23-AUDIT-15: 404 NOT_FOUND → exit 1 (Rule 4 usage error).
    it('should return exit code 1 for 404 not found', async () => {
      const deps = createMockDeps({
        deletePreview: jest.fn().mockRejectedValue(new ApiError('Not found', 3, 404, 'NOT_FOUND')),
      });
      const cmd = new PreviewDeleteCommand(deps);

      const exitCode = await cmd.execute('prev-nonexistent', { yes: true });

      expect(exitCode).toBe(1);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('not found');
    });

    it('should return exit code 3 for 409 already deleting', async () => {
      const deps = createMockDeps({
        deletePreview: jest.fn().mockRejectedValue(new ApiError('Conflict', 3, 409, 'ALREADY_DELETING')),
      });
      const cmd = new PreviewDeleteCommand(deps);

      const exitCode = await cmd.execute('prev-abc123', { yes: true });

      expect(exitCode).toBe(3);
      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('already being deleted');
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
      const cmd1 = new PreviewDeleteCommand(deps1);
      await cmd1.execute('prev-abc123', { yes: true });

      // Not authenticated
      const deps2 = createMockDeps({ getToken: jest.fn().mockReturnValue(null) });
      const cmd2 = new PreviewDeleteCommand(deps2);
      await cmd2.execute('prev-abc123', { yes: true });

      for (const deps of [deps1, deps2]) {
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
