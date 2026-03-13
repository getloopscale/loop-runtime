import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { detectMonorepoTool } from '../../../src/detection/monorepo';

describe('detectMonorepoTool', () => {
  let tmpDir: string;

  beforeEach(() => {
    tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'loopzero-monorepo-'));
  });

  afterEach(() => {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  });

  function writeFile(relativePath: string, content: string): void {
    const fullPath = path.join(tmpDir, relativePath);
    fs.mkdirSync(path.dirname(fullPath), { recursive: true });
    fs.writeFileSync(fullPath, content);
  }

  describe('Turborepo', () => {
    it('should detect turbo.json', () => {
      writeFile('turbo.json', '{}');
      writeFile('package.json', JSON.stringify({
        workspaces: ['apps/*', 'packages/*'],
      }));
      writeFile('apps/api/package.json', '{}');
      writeFile('apps/web/package.json', '{}');

      const result = detectMonorepoTool(tmpDir);
      expect(result).not.toBeNull();
      expect(result!.tool).toBe('turborepo');
      expect(result!.workspaces).toContain('apps/api');
      expect(result!.workspaces).toContain('apps/web');
    });
  });

  describe('Nx', () => {
    it('should detect nx.json', () => {
      writeFile('nx.json', '{}');
      writeFile('package.json', JSON.stringify({
        workspaces: ['packages/*'],
      }));
      writeFile('packages/lib-a/package.json', '{}');

      const result = detectMonorepoTool(tmpDir);
      expect(result).not.toBeNull();
      expect(result!.tool).toBe('nx');
      expect(result!.workspaces).toContain('packages/lib-a');
    });
  });

  describe('Lerna', () => {
    it('should detect lerna.json', () => {
      writeFile('lerna.json', JSON.stringify({
        packages: ['packages/*'],
      }));
      writeFile('packages/core/package.json', '{}');
      writeFile('packages/utils/package.json', '{}');

      const result = detectMonorepoTool(tmpDir);
      expect(result).not.toBeNull();
      expect(result!.tool).toBe('lerna');
      expect(result!.workspaces).toContain('packages/core');
      expect(result!.workspaces).toContain('packages/utils');
    });
  });

  describe('pnpm workspaces', () => {
    it('should detect pnpm-workspace.yaml', () => {
      writeFile('pnpm-workspace.yaml', 'packages:\n  - "apps/*"\n');
      writeFile('apps/server/package.json', '{}');

      const result = detectMonorepoTool(tmpDir);
      expect(result).not.toBeNull();
      expect(result!.tool).toBe('pnpm');
      expect(result!.workspaces).toContain('apps/server');
    });
  });

  describe('yarn workspaces', () => {
    it('should detect package.json workspaces + yarn.lock', () => {
      writeFile('package.json', JSON.stringify({
        workspaces: ['packages/*'],
      }));
      writeFile('yarn.lock', '');
      writeFile('packages/app/package.json', '{}');

      const result = detectMonorepoTool(tmpDir);
      expect(result).not.toBeNull();
      expect(result!.tool).toBe('yarn');
      expect(result!.workspaces).toContain('packages/app');
    });
  });

  describe('npm workspaces', () => {
    it('should detect package.json workspaces without yarn.lock', () => {
      writeFile('package.json', JSON.stringify({
        workspaces: ['packages/*'],
      }));
      writeFile('packages/app/package.json', '{}');

      const result = detectMonorepoTool(tmpDir);
      expect(result).not.toBeNull();
      expect(result!.tool).toBe('npm');
      expect(result!.workspaces).toContain('packages/app');
    });
  });

  describe('flat monorepo', () => {
    it('should detect multiple projects without a tool', () => {
      writeFile('api/package.json', '{}');
      writeFile('worker/go.mod', 'module worker');
      writeFile('ml/requirements.txt', 'torch');

      const result = detectMonorepoTool(tmpDir);
      expect(result).not.toBeNull();
      expect(result!.tool).toBe('flat');
      expect(result!.workspaces).toContain('api');
      expect(result!.workspaces).toContain('worker');
      expect(result!.workspaces).toContain('ml');
    });
  });

  describe('priority', () => {
    it('should prefer Turborepo over npm workspaces', () => {
      writeFile('turbo.json', '{}');
      writeFile('package.json', JSON.stringify({ workspaces: ['apps/*'] }));
      writeFile('apps/api/package.json', '{}');

      const result = detectMonorepoTool(tmpDir);
      expect(result).not.toBeNull();
      expect(result!.tool).toBe('turborepo');
    });
  });

  describe('no monorepo', () => {
    it('should return null when single project', () => {
      writeFile('package.json', '{}');
      writeFile('src/index.ts', '');

      const result = detectMonorepoTool(tmpDir);
      expect(result).toBeNull();
    });
  });
});
