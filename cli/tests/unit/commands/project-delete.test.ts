import { ProjectDeleteCommand, ProjectDeleteDeps } from '../../../src/commands/project-delete';

describe('ProjectDeleteCommand', () => {
  let output: string[];
  let errors: string[];

  function makeDeps(overrides: Partial<ProjectDeleteDeps> = {}): ProjectDeleteDeps {
    return {
      getToken: () => 'test-token',
      deleteProject: async () => ({
        project: 'my-app',
        deleted_environments: ['production'],
        cancelled_builds: 0,
      }),
      confirm: async () => true,
      writeTo: (msg: string) => output.push(msg),
      writeError: (msg: string) => errors.push(msg),
      isCI: false,
      ...overrides,
    };
  }

  beforeEach(() => {
    output = [];
    errors = [];
  });

  describe('exit codes', () => {
    it('returns 0 on successful delete', async () => {
      const cmd = new ProjectDeleteCommand(makeDeps());
      const code = await cmd.execute('my-app', { yes: true });
      expect(code).toBe(0);
    });

    it('returns 4 when not authenticated', async () => {
      const cmd = new ProjectDeleteCommand(makeDeps({ getToken: () => null }));
      const code = await cmd.execute('my-app', { yes: true });
      expect(code).toBe(4);
    });

    it('returns 1 when name is missing', async () => {
      const cmd = new ProjectDeleteCommand(makeDeps());
      const code = await cmd.execute('', { yes: true });
      expect(code).toBe(1);
    });

    it('returns 1 in CI without --yes', async () => {
      const cmd = new ProjectDeleteCommand(makeDeps({ isCI: true }));
      const code = await cmd.execute('my-app', {});
      expect(code).toBe(1);
    });

    it('returns 3 on API failure', async () => {
      const cmd = new ProjectDeleteCommand(makeDeps({
        deleteProject: async () => {
          throw { message: 'Service unavailable', exitCode: 3, code: 'SERVICE_UNAVAILABLE' };
        },
      }));
      const code = await cmd.execute('my-app', { yes: true });
      expect(code).toBe(3);
    });

    it('returns 4 on auth error from API', async () => {
      const cmd = new ProjectDeleteCommand(makeDeps({
        deleteProject: async () => {
          throw { message: 'Not authenticated', exitCode: 4, code: 'AUTH_REQUIRED' };
        },
      }));
      const code = await cmd.execute('my-app', { yes: true });
      expect(code).toBe(4);
    });
  });

  describe('--json output', () => {
    it('outputs JSON on success', async () => {
      const cmd = new ProjectDeleteCommand(makeDeps({
        deleteProject: async () => ({
          project: 'my-app',
          deleted_environments: ['production', 'staging'],
          cancelled_builds: 2,
        }),
      }));
      const code = await cmd.execute('my-app', { yes: true, json: true });
      expect(code).toBe(0);
      const parsed = JSON.parse(output[0]);
      expect(parsed.project).toBe('my-app');
      expect(parsed.deleted_environments).toEqual(['production', 'staging']);
      expect(parsed.cancelled_builds).toBe(2);
      expect(parsed.status).toBe('deleted');
    });

    it('outputs JSON on error', async () => {
      const cmd = new ProjectDeleteCommand(makeDeps({ getToken: () => null }));
      const code = await cmd.execute('my-app', { yes: true, json: true });
      expect(code).toBe(4);
      const parsed = JSON.parse(output[0]);
      expect(parsed.error).toBeDefined();
      expect(parsed.exit_code).toBe(4);
      expect(parsed.code).toBe('AUTH_REQUIRED');
    });

    it('outputs JSON when cancelled', async () => {
      const cmd = new ProjectDeleteCommand(makeDeps({
        confirm: async () => false,
      }));
      const code = await cmd.execute('my-app', { json: true });
      expect(code).toBe(0);
      const parsed = JSON.parse(output[0]);
      expect(parsed.status).toBe('cancelled');
      expect(parsed.project).toBe('my-app');
    });
  });

  describe('confirmation', () => {
    it('cancels when user declines', async () => {
      const cmd = new ProjectDeleteCommand(makeDeps({
        confirm: async () => false,
      }));
      const code = await cmd.execute('my-app', {});
      expect(code).toBe(0);
      expect(output[0]).toContain('cancelled');
    });

    it('skips confirmation with --yes', async () => {
      let confirmCalled = false;
      const cmd = new ProjectDeleteCommand(makeDeps({
        confirm: async () => { confirmCalled = true; return true; },
      }));
      const code = await cmd.execute('my-app', { yes: true });
      expect(code).toBe(0);
      expect(confirmCalled).toBe(false);
    });

    it('includes environment name in confirmation prompt when --env is set', async () => {
      let promptMessage = '';
      const cmd = new ProjectDeleteCommand(makeDeps({
        confirm: async (msg: string) => { promptMessage = msg; return false; },
      }));
      await cmd.execute('my-app', { env: 'staging' });
      expect(promptMessage).toContain('staging');
    });
  });

  describe('human output', () => {
    it('shows deleted environments on success', async () => {
      const cmd = new ProjectDeleteCommand(makeDeps({
        deleteProject: async () => ({
          project: 'my-app',
          deleted_environments: ['production', 'staging'],
          cancelled_builds: 0,
        }),
      }));
      await cmd.execute('my-app', { yes: true });
      expect(output[0]).toContain('production, staging');
    });

    it('shows cancelled builds count when > 0', async () => {
      const cmd = new ProjectDeleteCommand(makeDeps({
        deleteProject: async () => ({
          project: 'my-app',
          deleted_environments: ['production'],
          cancelled_builds: 3,
        }),
      }));
      await cmd.execute('my-app', { yes: true });
      expect(output[1]).toContain('3');
      expect(output[1]).toContain('active build');
    });

    it('shows "none" when no environments deleted', async () => {
      const cmd = new ProjectDeleteCommand(makeDeps({
        deleteProject: async () => ({
          project: 'my-app',
          deleted_environments: [],
          cancelled_builds: 0,
        }),
      }));
      await cmd.execute('my-app', { yes: true });
      expect(output[0]).toContain('none');
    });

    it('shows error message on auth failure', async () => {
      const cmd = new ProjectDeleteCommand(makeDeps({ getToken: () => null }));
      await cmd.execute('my-app', { yes: true });
      expect(errors[0]).toContain('Not authenticated');
      expect(errors[0]).toContain('loopzero login');
    });
  });

  describe('no internal terms', () => {
    const FORBIDDEN_TERMS = [
      'ArgoCD', 'argocd', 'GitOps', 'gitops-push', 'CRD', 'tenant_id',
      'advisory lock', 'pg_advisory', 'Gitea', 'MinIO', 'kubebuilder',
      'envtest', 'Kaniko',
    ];

    it('success output contains no internal terms', async () => {
      const cmd = new ProjectDeleteCommand(makeDeps());
      await cmd.execute('my-app', { yes: true });
      const allOutput = [...output, ...errors].join(' ');
      for (const term of FORBIDDEN_TERMS) {
        expect(allOutput).not.toContain(term);
      }
    });

    it('error output contains no internal terms', async () => {
      const cmd = new ProjectDeleteCommand(makeDeps({ getToken: () => null }));
      await cmd.execute('my-app', { yes: true });
      const allOutput = [...output, ...errors].join(' ');
      for (const term of FORBIDDEN_TERMS) {
        expect(allOutput).not.toContain(term);
      }
    });
  });

  describe('--env flag', () => {
    it('passes environment to API', async () => {
      let calledEnv: string | undefined;
      const cmd = new ProjectDeleteCommand(makeDeps({
        deleteProject: async (_name: string, env?: string) => {
          calledEnv = env;
          return { project: 'my-app', deleted_environments: ['staging'], cancelled_builds: 0 };
        },
      }));
      await cmd.execute('my-app', { yes: true, env: 'staging' });
      expect(calledEnv).toBe('staging');
    });

    it('does not pass environment when --env is not set', async () => {
      let calledEnv: string | undefined = 'should-be-undefined';
      const cmd = new ProjectDeleteCommand(makeDeps({
        deleteProject: async (_name: string, env?: string) => {
          calledEnv = env;
          return { project: 'my-app', deleted_environments: ['production'], cancelled_builds: 0 };
        },
      }));
      await cmd.execute('my-app', { yes: true });
      expect(calledEnv).toBeUndefined();
    });
  });
});
