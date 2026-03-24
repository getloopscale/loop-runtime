import * as fs from 'fs';
import * as path from 'path';

export interface MonorepoDetection {
  tool: string;
  workspaces: string[];
}

const PROJECT_MARKERS = ['package.json', 'go.mod', 'requirements.txt', 'Cargo.toml', 'pyproject.toml'];
const CONVENTIONAL_APP_DIRS = ['apps', 'services', 'microservices'];

export function detectMonorepoTool(rootDir: string): MonorepoDetection | null {
  // Priority order: Turborepo > Nx > Lerna > pnpm > yarn > npm > flat

  if (fileExists(rootDir, 'turbo.json')) {
    const workspaces = resolveWorkspaces(rootDir);
    return { tool: 'turborepo', workspaces };
  }

  if (fileExists(rootDir, 'nx.json')) {
    const workspaces = resolveWorkspaces(rootDir);
    return { tool: 'nx', workspaces };
  }

  if (fileExists(rootDir, 'lerna.json')) {
    const lernaConfig = readJSON(rootDir, 'lerna.json');
    const packages = lernaConfig?.packages;
    const patterns: string[] = Array.isArray(packages) ? packages : ['packages/*'];
    const workspaces = resolveGlobPatterns(rootDir, patterns);
    return { tool: 'lerna', workspaces };
  }

  if (fileExists(rootDir, 'pnpm-workspace.yaml')) {
    const content = fs.readFileSync(path.join(rootDir, 'pnpm-workspace.yaml'), 'utf-8');
    const patterns = parsePnpmWorkspaceYaml(content);
    const workspaces = resolveGlobPatterns(rootDir, patterns);
    return { tool: 'pnpm', workspaces };
  }

  const pkgJson = readJSON(rootDir, 'package.json');
  if (pkgJson?.workspaces) {
    const patterns: string[] = extractWorkspacePatterns(pkgJson.workspaces);
    const workspaces = resolveGlobPatterns(rootDir, patterns);

    if (fileExists(rootDir, 'yarn.lock')) {
      return { tool: 'yarn', workspaces };
    }
    return { tool: 'npm', workspaces };
  }

  // Flat detection: look for multiple subdirectories with project markers
  // Also scans inside conventional monorepo directories (apps/, services/, microservices/)
  const flatWorkspaces = detectFlatWorkspaces(rootDir);
  if (flatWorkspaces.length >= 1) {
    return { tool: 'flat', workspaces: flatWorkspaces };
  }

  return null;
}

function fileExists(rootDir: string, fileName: string): boolean {
  return fs.existsSync(path.join(rootDir, fileName));
}

function readJSON(rootDir: string, fileName: string): Record<string, unknown> | null {
  const filePath = path.join(rootDir, fileName);
  if (!fs.existsSync(filePath)) return null;
  try {
    return JSON.parse(fs.readFileSync(filePath, 'utf-8'));
  } catch {
    return null;
  }
}

function resolveWorkspaces(rootDir: string): string[] {
  const pkgJson = readJSON(rootDir, 'package.json');
  if (!pkgJson?.workspaces) return [];
  const patterns: string[] = extractWorkspacePatterns(pkgJson.workspaces);
  return resolveGlobPatterns(rootDir, patterns);
}

function extractWorkspacePatterns(workspaces: unknown): string[] {
  if (Array.isArray(workspaces)) return workspaces;
  if (typeof workspaces === 'object' && workspaces !== null) {
    const packages = (workspaces as Record<string, unknown>).packages;
    if (Array.isArray(packages)) return packages;
  }
  return [];
}

function resolveGlobPatterns(rootDir: string, patterns: string[]): string[] {
  const workspaces: string[] = [];

  for (const pattern of patterns) {
    // Handle simple glob: "apps/*", "packages/*"
    if (pattern.endsWith('/*')) {
      const parentDir = path.join(rootDir, pattern.slice(0, -2));
      if (fs.existsSync(parentDir) && fs.statSync(parentDir).isDirectory()) {
        const entries = fs.readdirSync(parentDir, { withFileTypes: true });
        for (const entry of entries) {
          if (entry.isDirectory()) {
            const relative = path.relative(rootDir, path.join(parentDir, entry.name));
            workspaces.push(relative);
          }
        }
      }
    } else {
      // Exact path
      const fullPath = path.join(rootDir, pattern);
      if (fs.existsSync(fullPath) && fs.statSync(fullPath).isDirectory()) {
        workspaces.push(pattern);
      }
    }
  }

  return workspaces.sort();
}

function detectFlatWorkspaces(rootDir: string): string[] {
  const workspaces: string[] = [];
  const entries = fs.readdirSync(rootDir, { withFileTypes: true });

  for (const entry of entries) {
    if (!entry.isDirectory() || entry.name.startsWith('.') || entry.name === 'node_modules') {
      continue;
    }
    const subDir = path.join(rootDir, entry.name);
    for (const marker of PROJECT_MARKERS) {
      if (fs.existsSync(path.join(subDir, marker))) {
        workspaces.push(entry.name);
        break;
      }
    }
  }

  // Also check inside conventional monorepo directories (apps/, services/, microservices/)
  for (const dirName of CONVENTIONAL_APP_DIRS) {
    const conventionalDir = path.join(rootDir, dirName);
    if (!fs.existsSync(conventionalDir) || !fs.statSync(conventionalDir).isDirectory()) continue;
    const subEntries = fs.readdirSync(conventionalDir, { withFileTypes: true });
    for (const entry of subEntries) {
      if (!entry.isDirectory() || entry.name.startsWith('.') || entry.name === 'node_modules') continue;
      const subDir = path.join(conventionalDir, entry.name);
      for (const marker of PROJECT_MARKERS) {
        if (fs.existsSync(path.join(subDir, marker))) {
          workspaces.push(path.join(dirName, entry.name));
          break;
        }
      }
    }
  }

  return workspaces.sort();
}

function parsePnpmWorkspaceYaml(content: string): string[] {
  // Simple YAML parser for pnpm-workspace.yaml — just extract quoted strings after "- "
  const patterns: string[] = [];
  const lines = content.split('\n');
  for (const line of lines) {
    const match = line.match(/^\s*-\s*["']?([^"'\s]+)["']?\s*$/);
    if (match) {
      patterns.push(match[1]);
    }
  }
  return patterns;
}
