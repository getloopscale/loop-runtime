import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { InitCommand, InitDeps } from '../../../src/commands/init';

function createMockDeps(
  tmpDir: string,
  overrides: Partial<InitDeps> = {}
): InitDeps {
  return {
    getToken: jest.fn().mockReturnValue('ghp_test123'),
    getTeam: jest.fn().mockReturnValue('acme'),
    detectMonorepo: jest.fn().mockReturnValue({
      tool: 'turborepo',
      workspaces: ['apps/api', 'apps/web'],
    }),
    detectFramework: jest.fn().mockReturnValue({
      framework: 'express',
      port: 3000,
    }),
    createProject: jest.fn().mockResolvedValue({ id: 'proj-12345678', name: 'my-saas', team: 'acme' }),
    writeTo: jest.fn(),
    writeError: jest.fn(),
    isCI: false,
    cwd: tmpDir,
    ...overrides,
  };
}

describe('InitCommand', () => {
  let tmpDir: string;

  beforeEach(() => {
    tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'loopzero-init-'));
  });

  afterEach(() => {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  });

  describe('loopzero.yaml generation', () => {
    it('should generate loopzero.yaml in the project root', async () => {
      const deps = createMockDeps(tmpDir);
      const cmd = new InitCommand(deps);

      const exitCode = await cmd.execute({ yes: true });

      expect(exitCode).toBe(0);
      const yamlPath = path.join(tmpDir, 'loopzero.yaml');
      expect(fs.existsSync(yamlPath)).toBe(true);
    });

    it('should include project name and apps in loopzero.yaml', async () => {
      const deps = createMockDeps(tmpDir);
      const cmd = new InitCommand(deps);

      await cmd.execute({ yes: true, name: 'my-saas' });

      const content = fs.readFileSync(path.join(tmpDir, 'loopzero.yaml'), 'utf-8');
      expect(content).toContain('my-saas');
      expect(content).toContain('apps');
    });

    it('should not contain tenant field in loopzero.yaml', async () => {
      const deps = createMockDeps(tmpDir);
      const cmd = new InitCommand(deps);

      await cmd.execute({ yes: true });

      const content = fs.readFileSync(path.join(tmpDir, 'loopzero.yaml'), 'utf-8');
      expect(content).not.toContain('tenant');
    });
  });

  describe('--force flag', () => {
    it('should exit 1 if loopzero.yaml exists without --force', async () => {
      fs.writeFileSync(path.join(tmpDir, 'loopzero.yaml'), 'existing');
      const deps = createMockDeps(tmpDir);
      const cmd = new InitCommand(deps);

      const exitCode = await cmd.execute({ yes: true });

      expect(exitCode).toBe(1);
    });

    it('should overwrite if --force is set', async () => {
      fs.writeFileSync(path.join(tmpDir, 'loopzero.yaml'), 'old-content');
      const deps = createMockDeps(tmpDir);
      const cmd = new InitCommand(deps);

      const exitCode = await cmd.execute({ yes: true, force: true });

      expect(exitCode).toBe(0);
      const content = fs.readFileSync(path.join(tmpDir, 'loopzero.yaml'), 'utf-8');
      expect(content).not.toBe('old-content');
    });
  });

  describe('auth required', () => {
    it('should exit 4 if not authenticated', async () => {
      const deps = createMockDeps(tmpDir, {
        getToken: jest.fn().mockReturnValue(null),
      });
      const cmd = new InitCommand(deps);

      const exitCode = await cmd.execute({ yes: true });

      expect(exitCode).toBe(4);
    });
  });

  describe('.gitignore update', () => {
    it('should add .env.local to .gitignore', async () => {
      const deps = createMockDeps(tmpDir);
      const cmd = new InitCommand(deps);

      await cmd.execute({ yes: true });

      const gitignorePath = path.join(tmpDir, '.gitignore');
      if (fs.existsSync(gitignorePath)) {
        const content = fs.readFileSync(gitignorePath, 'utf-8');
        expect(content).toContain('.env.local');
      }
    });

    it('should not duplicate .env.local if already in .gitignore', async () => {
      fs.writeFileSync(path.join(tmpDir, '.gitignore'), '.env.local\n');
      const deps = createMockDeps(tmpDir);
      const cmd = new InitCommand(deps);

      await cmd.execute({ yes: true });

      const content = fs.readFileSync(path.join(tmpDir, '.gitignore'), 'utf-8');
      const count = (content.match(/\.env\.local/g) || []).length;
      expect(count).toBe(1);
    });
  });

  describe('API call', () => {
    it('should call createProject with correct payload', async () => {
      const deps = createMockDeps(tmpDir);
      const cmd = new InitCommand(deps);

      await cmd.execute({ yes: true, name: 'my-saas' });

      expect(deps.createProject).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'my-saas' })
      );
    });
  });

  describe('CI mode', () => {
    it('should exit 1 in CI mode without --yes', async () => {
      const deps = createMockDeps(tmpDir, { isCI: true });
      const cmd = new InitCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(1);
    });

    it('should work in CI mode with --yes', async () => {
      const deps = createMockDeps(tmpDir, { isCI: true });
      const cmd = new InitCommand(deps);

      const exitCode = await cmd.execute({ yes: true });

      expect(exitCode).toBe(0);
    });
  });

  describe('no internal terms', () => {
    it('should not contain forbidden terms in output', async () => {
      const forbidden = [
        'ArgoCD', 'argocd', 'GitOps', 'gitops-push', 'CRD',
        'tenant_id', 'advisory lock', 'pg_advisory', 'Gitea',
        'MinIO', 'kubebuilder', 'envtest', 'Kaniko',
      ];

      const deps = createMockDeps(tmpDir);
      const cmd = new InitCommand(deps);
      await cmd.execute({ yes: true });

      const allOutput = [
        ...(deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]),
        ...(deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]),
      ].join(' ');

      for (const term of forbidden) {
        expect(allOutput).not.toContain(term);
      }
    });
  });
});
