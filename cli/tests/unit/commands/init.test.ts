import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { InitCommand, InitDeps } from '../../../src/commands/init';
import { ApiError } from '../../../src/api/client';

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

  describe('YAML generation with special characters', () => {
    it('should quote project name containing special characters', async () => {
      const deps = createMockDeps(tmpDir, {
        detectMonorepo: jest.fn().mockReturnValue(null),
        detectFramework: jest.fn().mockReturnValue({ framework: 'express', port: 3000 }),
      });
      const cmd = new InitCommand(deps);

      await cmd.execute({ yes: true, name: 'my:project' });

      const content = fs.readFileSync(path.join(tmpDir, 'loopzero.yaml'), 'utf-8');
      expect(content).toContain('"my:project"');
    });

    it('should quote app path containing spaces', async () => {
      const deps = createMockDeps(tmpDir, {
        detectMonorepo: jest.fn().mockReturnValue({
          tool: 'turborepo',
          workspaces: ['apps/my app'],
        }),
        detectFramework: jest.fn().mockReturnValue({ framework: 'next', port: 3000 }),
      });
      const cmd = new InitCommand(deps);

      await cmd.execute({ yes: true });

      const content = fs.readFileSync(path.join(tmpDir, 'loopzero.yaml'), 'utf-8');
      expect(content).toContain('"apps/my app"');
    });

    it('should not quote simple alphanumeric names', async () => {
      const deps = createMockDeps(tmpDir, {
        detectMonorepo: jest.fn().mockReturnValue(null),
        detectFramework: jest.fn().mockReturnValue({ framework: 'express', port: 3000 }),
      });
      const cmd = new InitCommand(deps);

      await cmd.execute({ yes: true, name: 'my-saas' });

      const content = fs.readFileSync(path.join(tmpDir, 'loopzero.yaml'), 'utf-8');
      expect(content).toContain('project: my-saas');
      expect(content).not.toContain('"my-saas"');
    });
  });

  describe('subdirectory app detection (non-monorepo)', () => {
    it('should detect apps in subdirectories when root has no framework', async () => {
      // Create subdirectory with package.json
      fs.mkdirSync(path.join(tmpDir, 'api'));
      fs.writeFileSync(
        path.join(tmpDir, 'api', 'package.json'),
        '{"name":"api","dependencies":{"express":"^4.0.0"}}',
      );

      const detectFramework = jest.fn().mockImplementation((dir: string) => {
        if (dir.endsWith('/api')) {
          return { framework: 'express', port: 3000 };
        }
        return { framework: 'custom', port: null };
      });

      const deps = createMockDeps(tmpDir, {
        detectMonorepo: jest.fn().mockReturnValue(null),
        detectFramework,
      });
      const cmd = new InitCommand(deps);

      await cmd.execute({ yes: true });

      const content = fs.readFileSync(path.join(tmpDir, 'loopzero.yaml'), 'utf-8');
      expect(content).toContain('api:');
      expect(content).toContain('path: ./api');
      expect(content).toContain('port: 3000');
    });

    it('should detect multiple apps in subdirectories', async () => {
      fs.mkdirSync(path.join(tmpDir, 'api'));
      fs.writeFileSync(path.join(tmpDir, 'api', 'package.json'), '{}');
      fs.mkdirSync(path.join(tmpDir, 'worker'));
      fs.writeFileSync(path.join(tmpDir, 'worker', 'package.json'), '{}');

      const detectFramework = jest.fn().mockImplementation((dir: string) => {
        if (dir.endsWith('/api')) return { framework: 'express', port: 3000 };
        if (dir.endsWith('/worker')) return { framework: 'node', port: 3000 };
        return { framework: 'custom', port: null };
      });

      const deps = createMockDeps(tmpDir, {
        detectMonorepo: jest.fn().mockReturnValue(null),
        detectFramework,
      });
      const cmd = new InitCommand(deps);

      await cmd.execute({ yes: true });

      const content = fs.readFileSync(path.join(tmpDir, 'loopzero.yaml'), 'utf-8');
      expect(content).toContain('api:');
      expect(content).toContain('worker:');
      expect(content).toContain('type: worker');
    });

    it('should skip node_modules, .git, and hidden directories', async () => {
      fs.mkdirSync(path.join(tmpDir, 'node_modules'));
      fs.mkdirSync(path.join(tmpDir, '.git'));
      fs.mkdirSync(path.join(tmpDir, 'api'));
      fs.writeFileSync(path.join(tmpDir, 'api', 'package.json'), '{}');

      const detectFramework = jest.fn().mockImplementation((dir: string) => {
        if (dir.endsWith('/api')) return { framework: 'express', port: 3000 };
        return { framework: 'custom', port: null };
      });

      const deps = createMockDeps(tmpDir, {
        detectMonorepo: jest.fn().mockReturnValue(null),
        detectFramework,
      });
      const cmd = new InitCommand(deps);

      await cmd.execute({ yes: true });

      // detectFramework should NOT be called with node_modules or .git
      const calledDirs = detectFramework.mock.calls.map((c: string[]) => c[0]);
      expect(calledDirs.some((d: string) => d.includes('node_modules'))).toBe(false);
      expect(calledDirs.some((d: string) => d.includes('.git'))).toBe(false);
    });
  });

  describe('conventional monorepo directory detection (non-monorepo fallback)', () => {
    it('should detect apps inside apps/ directory when root has no framework', async () => {
      // Create apps/hello-api/package.json — no workspace config
      fs.mkdirSync(path.join(tmpDir, 'apps', 'hello-api'), { recursive: true });
      fs.writeFileSync(
        path.join(tmpDir, 'apps', 'hello-api', 'package.json'),
        '{"name":"hello-api","dependencies":{"express":"^4.0.0"}}',
      );

      const detectFramework = jest.fn().mockImplementation((dir: string) => {
        if (dir.endsWith('/hello-api')) {
          return { framework: 'express', port: 3000 };
        }
        return { framework: 'custom', port: null };
      });

      const deps = createMockDeps(tmpDir, {
        detectMonorepo: jest.fn().mockReturnValue(null),
        detectFramework,
      });
      const cmd = new InitCommand(deps);

      await cmd.execute({ yes: true });

      const content = fs.readFileSync(path.join(tmpDir, 'loopzero.yaml'), 'utf-8');
      expect(content).toContain('hello-api:');
      expect(content).toContain('framework: express');
      expect(content).toContain('port: 3000');
    });

    it('should detect apps inside services/ directory when root has no framework', async () => {
      fs.mkdirSync(path.join(tmpDir, 'services', 'auth-svc'), { recursive: true });
      fs.writeFileSync(
        path.join(tmpDir, 'services', 'auth-svc', 'go.mod'),
        'module auth-svc',
      );

      const detectFramework = jest.fn().mockImplementation((dir: string) => {
        if (dir.endsWith('/auth-svc')) {
          return { framework: 'go', port: 8080 };
        }
        return { framework: 'custom', port: null };
      });

      const deps = createMockDeps(tmpDir, {
        detectMonorepo: jest.fn().mockReturnValue(null),
        detectFramework,
      });
      const cmd = new InitCommand(deps);

      await cmd.execute({ yes: true });

      const content = fs.readFileSync(path.join(tmpDir, 'loopzero.yaml'), 'utf-8');
      expect(content).toContain('auth-svc:');
      expect(content).toContain('framework: go');
    });
  });

  describe('interactive wizard (no --yes)', () => {
    it('should show detection summary when confirm dep is provided', async () => {
      const deps = createMockDeps(tmpDir, {
        confirm: jest.fn().mockResolvedValue(true),
      });
      const cmd = new InitCommand(deps);

      await cmd.execute({});

      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('turborepo');
      expect(output).toContain('express');
    });

    it('should exit 0 without generating files when confirm returns false', async () => {
      const deps = createMockDeps(tmpDir, {
        confirm: jest.fn().mockResolvedValue(false),
      });
      const cmd = new InitCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(0);
      const yamlPath = path.join(tmpDir, 'loopzero.yaml');
      expect(fs.existsSync(yamlPath)).toBe(false);
    });

    it('should generate loopzero.yaml when confirm returns true', async () => {
      const deps = createMockDeps(tmpDir, {
        confirm: jest.fn().mockResolvedValue(true),
      });
      const cmd = new InitCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(0);
      const yamlPath = path.join(tmpDir, 'loopzero.yaml');
      expect(fs.existsSync(yamlPath)).toBe(true);
    });

    it('should prompt for project name when promptText provided and no --name', async () => {
      const deps = createMockDeps(tmpDir, {
        confirm: jest.fn().mockResolvedValue(true),
        promptText: jest.fn().mockResolvedValue('custom-name'),
      });
      const cmd = new InitCommand(deps);

      await cmd.execute({});

      expect(deps.promptText).toHaveBeenCalled();
      const content = fs.readFileSync(path.join(tmpDir, 'loopzero.yaml'), 'utf-8');
      expect(content).toContain('custom-name');
    });

    it('should not prompt for name when --name is provided', async () => {
      const deps = createMockDeps(tmpDir, {
        confirm: jest.fn().mockResolvedValue(true),
        promptText: jest.fn().mockResolvedValue('ignored'),
      });
      const cmd = new InitCommand(deps);

      await cmd.execute({ name: 'explicit-name' });

      expect(deps.promptText).not.toHaveBeenCalled();
      const content = fs.readFileSync(path.join(tmpDir, 'loopzero.yaml'), 'utf-8');
      expect(content).toContain('explicit-name');
    });

    it('should generate without prompting when no confirm/promptText deps (backwards compat)', async () => {
      const deps = createMockDeps(tmpDir);
      const cmd = new InitCommand(deps);

      const exitCode = await cmd.execute({});

      expect(exitCode).toBe(0);
      const yamlPath = path.join(tmpDir, 'loopzero.yaml');
      expect(fs.existsSync(yamlPath)).toBe(true);
    });

    it('should skip prompts with --yes even when confirm/promptText deps exist', async () => {
      const deps = createMockDeps(tmpDir, {
        confirm: jest.fn().mockResolvedValue(false),
        promptText: jest.fn().mockResolvedValue('should-not-use'),
      });
      const cmd = new InitCommand(deps);

      const exitCode = await cmd.execute({ yes: true });

      expect(exitCode).toBe(0);
      expect(deps.confirm).not.toHaveBeenCalled();
      expect(deps.promptText).not.toHaveBeenCalled();
      const yamlPath = path.join(tmpDir, 'loopzero.yaml');
      expect(fs.existsSync(yamlPath)).toBe(true);
    });

    it('should include framework and port for each app in detection summary', async () => {
      const deps = createMockDeps(tmpDir, {
        confirm: jest.fn().mockResolvedValue(true),
        detectFramework: jest.fn().mockReturnValue({ framework: 'fastify', port: 8080 }),
      });
      const cmd = new InitCommand(deps);

      await cmd.execute({});

      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('fastify');
      expect(output).toContain('8080');
    });

    it('should show final message mentioning loopzero deploy', async () => {
      const deps = createMockDeps(tmpDir, {
        confirm: jest.fn().mockResolvedValue(true),
      });
      const cmd = new InitCommand(deps);

      await cmd.execute({});

      const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(output).toContain('loopzero deploy');
    });
  });

  describe('API unavailable', () => {
    it('should still generate loopzero.yaml and warn when API fails', async () => {
      const deps = createMockDeps(tmpDir, {
        createProject: jest.fn().mockRejectedValue(new Error('ECONNREFUSED')),
      });
      const cmd = new InitCommand(deps);

      const exitCode = await cmd.execute({ yes: true });

      expect(exitCode).toBe(0);
      const yamlPath = path.join(tmpDir, 'loopzero.yaml');
      expect(fs.existsSync(yamlPath)).toBe(true);

      const errorOutput = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(errorOutput).toContain('Could not reach server');
      expect(errorOutput).toContain('loopzero deploy');

      const stdOutput = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(stdOutput).toContain('Generated loopzero.yaml');
    });
  });

  describe('--json flag consistency (F-01, F-02, F-03, F-05, F-07)', () => {
    it('F-01: init --json outputs only valid JSON with no human text mixed in', async () => {
      const deps = createMockDeps(tmpDir);
      const cmd = new InitCommand(deps);

      const exitCode = await cmd.execute({ yes: true, name: 'my-saas', json: true });

      expect(exitCode).toBe(0);
      const allWrites = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]);
      // Should be exactly one writeTo call containing JSON
      expect(allWrites).toHaveLength(1);
      const parsed = JSON.parse(allWrites[0]);
      expect(parsed.project).toBe('my-saas');
      expect(parsed.id).toBe('proj-12345678');
      expect(parsed.apps).toBeDefined();
      // No human text should appear before or after
      expect(allWrites[0]).not.toContain('Detected');
      expect(allWrites[0]).not.toContain('Generated');
    });

    it('F-01: init --json includes detection info in JSON output for monorepo', async () => {
      const deps = createMockDeps(tmpDir);
      const cmd = new InitCommand(deps);

      await cmd.execute({ yes: true, name: 'my-saas', json: true });

      const allWrites = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]);
      const parsed = JSON.parse(allWrites[0]);
      expect(parsed.detected).toBeDefined();
      expect(parsed.detected.tool).toBe('turborepo');
    });

    it('F-02: init --json emits JSON when API returns 409', async () => {
      const deps = createMockDeps(tmpDir, {
        createProject: jest.fn().mockRejectedValue(
          new ApiError('Project already exists', 3, 409)
        ),
      });
      const cmd = new InitCommand(deps);

      const exitCode = await cmd.execute({ yes: true, name: 'my-saas', json: true });

      expect(exitCode).toBe(0);
      const allWrites = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]);
      expect(allWrites).toHaveLength(1);
      const parsed = JSON.parse(allWrites[0]);
      expect(parsed.project).toBe('my-saas');
      expect(parsed.warning).toBeDefined();
    });

    it('F-02: init --json emits JSON when API returns non-409 error', async () => {
      const deps = createMockDeps(tmpDir, {
        createProject: jest.fn().mockRejectedValue(new Error('ECONNREFUSED')),
      });
      const cmd = new InitCommand(deps);

      const exitCode = await cmd.execute({ yes: true, name: 'my-saas', json: true });

      expect(exitCode).toBe(0);
      const allWrites = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]);
      expect(allWrites).toHaveLength(1);
      const parsed = JSON.parse(allWrites[0]);
      expect(parsed.project).toBe('my-saas');
      expect(parsed.warning).toBeDefined();
    });

    it('F-03: init (human) with API 409 shows "Project already registered" not confusing warning', async () => {
      const deps = createMockDeps(tmpDir, {
        createProject: jest.fn().mockRejectedValue(
          new ApiError('Project already exists', 3, 409)
        ),
      });
      const cmd = new InitCommand(deps);

      const exitCode = await cmd.execute({ yes: true, name: 'my-saas' });

      expect(exitCode).toBe(0);
      const allWrites = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      const allErrors = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      // Should NOT show confusing "Could not register project on server" for 409
      expect(allErrors).not.toContain('Could not register project on server');
      // Should show success-like message
      expect(allWrites).toContain('Generated loopzero.yaml');
    });

    it('F-03: init (human) with connection error shows appropriate warning', async () => {
      const deps = createMockDeps(tmpDir, {
        createProject: jest.fn().mockRejectedValue(new Error('ECONNREFUSED')),
      });
      const cmd = new InitCommand(deps);

      const exitCode = await cmd.execute({ yes: true, name: 'my-saas' });

      expect(exitCode).toBe(0);
      const allErrors = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
      expect(allErrors).toContain('Could not reach server');
      expect(allErrors).toContain('loopzero deploy');
    });

    it('F-05: init --json with auth error outputs JSON error with exit_code 4', async () => {
      const deps = createMockDeps(tmpDir, {
        getToken: jest.fn().mockReturnValue(null),
      });
      const cmd = new InitCommand(deps);

      const exitCode = await cmd.execute({ yes: true, json: true });

      expect(exitCode).toBe(4);
      const allWrites = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]);
      expect(allWrites).toHaveLength(1);
      const parsed = JSON.parse(allWrites[0]);
      expect(parsed.error).toContain('Not authenticated');
      expect(parsed.exit_code).toBe(4);
    });

    it('F-07: init --json with loopzero.yaml exists outputs JSON error with exit_code 1', async () => {
      fs.writeFileSync(path.join(tmpDir, 'loopzero.yaml'), 'existing');
      const deps = createMockDeps(tmpDir);
      const cmd = new InitCommand(deps);

      const exitCode = await cmd.execute({ yes: true, json: true });

      expect(exitCode).toBe(1);
      const allWrites = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]);
      expect(allWrites).toHaveLength(1);
      const parsed = JSON.parse(allWrites[0]);
      expect(parsed.error).toContain('loopzero.yaml already exists');
      expect(parsed.exit_code).toBe(1);
    });

    it('F-07: init --json with CI mode requires --yes outputs JSON error', async () => {
      const deps = createMockDeps(tmpDir, { isCI: true });
      const cmd = new InitCommand(deps);

      const exitCode = await cmd.execute({ json: true });

      expect(exitCode).toBe(1);
      const allWrites = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]);
      expect(allWrites).toHaveLength(1);
      const parsed = JSON.parse(allWrites[0]);
      expect(parsed.error).toContain('--yes');
      expect(parsed.exit_code).toBe(1);
    });
  });

  describe('project name sanitization from directory name', () => {
    it('should sanitize ugly directory names like tmp.DbFESo52xo', async () => {
      // Create a temp dir with dots in the name to simulate ugly auto-detection
      const uglyDir = path.join(tmpDir, 'tmp.DbFESo52xo');
      fs.mkdirSync(uglyDir);

      const deps = createMockDeps(uglyDir, {
        detectMonorepo: jest.fn().mockReturnValue(null),
        detectFramework: jest.fn().mockReturnValue({ framework: 'express', port: 3000 }),
        createProject: jest.fn().mockResolvedValue({ id: 'proj-123', name: 'tmp-dbfeso52xo', team: 'acme' }),
      });
      const cmd = new InitCommand(deps);

      const exitCode = await cmd.execute({ yes: true });

      expect(exitCode).toBe(0);
      const content = fs.readFileSync(path.join(uglyDir, 'loopzero.yaml'), 'utf-8');
      // Project name should be sanitized
      expect(content).toContain('project: tmp-dbfeso52xo');
      // App name in the YAML should also be sanitized (not the raw ugly dir name)
      expect(content).not.toContain('tmp.DbFESo52xo');
    });

    it('should lowercase and replace underscores in directory names', async () => {
      const uglyDir = path.join(tmpDir, 'My_Cool_Project');
      fs.mkdirSync(uglyDir);

      const deps = createMockDeps(uglyDir, {
        detectMonorepo: jest.fn().mockReturnValue(null),
        detectFramework: jest.fn().mockReturnValue({ framework: 'express', port: 3000 }),
        createProject: jest.fn().mockResolvedValue({ id: 'proj-123', name: 'my-cool-project', team: 'acme' }),
      });
      const cmd = new InitCommand(deps);

      const exitCode = await cmd.execute({ yes: true });

      expect(exitCode).toBe(0);
      const content = fs.readFileSync(path.join(uglyDir, 'loopzero.yaml'), 'utf-8');
      expect(content).toContain('project: my-cool-project');
      expect(content).not.toContain('My_Cool_Project');
    });

    it('should collapse consecutive hyphens and trim edges', async () => {
      const uglyDir = path.join(tmpDir, '--my...project--');
      fs.mkdirSync(uglyDir);

      const deps = createMockDeps(uglyDir, {
        detectMonorepo: jest.fn().mockReturnValue(null),
        detectFramework: jest.fn().mockReturnValue({ framework: 'express', port: 3000 }),
        createProject: jest.fn().mockResolvedValue({ id: 'proj-123', name: 'my-project', team: 'acme' }),
      });
      const cmd = new InitCommand(deps);

      const exitCode = await cmd.execute({ yes: true });

      expect(exitCode).toBe(0);
      const content = fs.readFileSync(path.join(uglyDir, 'loopzero.yaml'), 'utf-8');
      expect(content).toContain('project: my-project');
    });

    it('should pass sanitized name to createProject API call', async () => {
      const uglyDir = path.join(tmpDir, 'tmp.DbFESo52xo');
      fs.mkdirSync(uglyDir);

      const deps = createMockDeps(uglyDir, {
        detectMonorepo: jest.fn().mockReturnValue(null),
        detectFramework: jest.fn().mockReturnValue({ framework: 'express', port: 3000 }),
        createProject: jest.fn().mockResolvedValue({ id: 'proj-123', name: 'tmp-dbfeso52xo', team: 'acme' }),
      });
      const cmd = new InitCommand(deps);

      await cmd.execute({ yes: true });

      expect(deps.createProject).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'tmp-dbfeso52xo' })
      );
    });

    it('should sanitize app name in loopzero.yaml when using root directory as single app', async () => {
      const uglyDir = path.join(tmpDir, 'tmp.DbFESo52xo');
      fs.mkdirSync(uglyDir);

      const deps = createMockDeps(uglyDir, {
        detectMonorepo: jest.fn().mockReturnValue(null),
        detectFramework: jest.fn().mockReturnValue({ framework: 'express', port: 3000 }),
        createProject: jest.fn().mockResolvedValue({ id: 'proj-123', name: 'tmp-dbfeso52xo', team: 'acme' }),
      });
      const cmd = new InitCommand(deps);

      await cmd.execute({ yes: true });

      const content = fs.readFileSync(path.join(uglyDir, 'loopzero.yaml'), 'utf-8');
      // The app key should use sanitized name, not the raw directory name
      expect(content).toContain('tmp-dbfeso52xo:');
      expect(content).not.toContain('tmp.DbFESo52xo');
    });
  });

  describe('custom framework default port (F-06)', () => {
    it('should default to port 3000 when root app has custom framework and no port', async () => {
      const deps = createMockDeps(tmpDir, {
        detectMonorepo: jest.fn().mockReturnValue(null),
        detectFramework: jest.fn().mockReturnValue({ framework: 'custom', port: null }),
      });
      const cmd = new InitCommand(deps);

      const exitCode = await cmd.execute({ yes: true });

      expect(exitCode).toBe(0);
      const content = fs.readFileSync(path.join(tmpDir, 'loopzero.yaml'), 'utf-8');
      expect(content).toContain('port: 3000');
    });

    it('should use detected port when framework provides one', async () => {
      const deps = createMockDeps(tmpDir, {
        detectMonorepo: jest.fn().mockReturnValue(null),
        detectFramework: jest.fn().mockReturnValue({ framework: 'express', port: 8080 }),
      });
      const cmd = new InitCommand(deps);

      const exitCode = await cmd.execute({ yes: true });

      expect(exitCode).toBe(0);
      const content = fs.readFileSync(path.join(tmpDir, 'loopzero.yaml'), 'utf-8');
      expect(content).toContain('port: 8080');
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
