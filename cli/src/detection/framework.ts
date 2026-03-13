import * as fs from 'fs';
import * as path from 'path';

export interface FrameworkDetection {
  framework: string;
  port: number | null;
  buildCommand?: string;
  startCommand?: string;
}

interface FrameworkSpec {
  name: string;
  port: number;
  detectByConfig?: (dir: string) => boolean;
  detectByDep?: (deps: Record<string, string>) => boolean;
  detectByFile?: (dir: string) => boolean;
}

const FRAMEWORKS: FrameworkSpec[] = [
  {
    name: 'nextjs',
    port: 3000,
    detectByConfig: (dir) =>
      fileExists(dir, 'next.config.js') ||
      fileExists(dir, 'next.config.mjs') ||
      fileExists(dir, 'next.config.ts'),
    detectByDep: (deps) => 'next' in deps,
  },
  {
    name: 'vite',
    port: 5173,
    detectByConfig: (dir) =>
      fileExists(dir, 'vite.config.ts') ||
      fileExists(dir, 'vite.config.js') ||
      fileExists(dir, 'vite.config.mjs'),
    detectByDep: (deps) => 'vite' in deps,
  },
  {
    name: 'remix',
    port: 3000,
    detectByConfig: (dir) => fileExists(dir, 'remix.config.js'),
    detectByDep: (deps) => '@remix-run/node' in deps || '@remix-run/react' in deps,
  },
  {
    name: 'nestjs',
    port: 3000,
    detectByDep: (deps) => '@nestjs/core' in deps,
  },
  {
    name: 'fastify',
    port: 3000,
    detectByDep: (deps) => 'fastify' in deps,
  },
  {
    name: 'express',
    port: 3000,
    detectByDep: (deps) => 'express' in deps,
  },
  {
    name: 'fastapi',
    port: 8000,
    detectByFile: (dir) => {
      const mainPy = path.join(dir, 'main.py');
      if (fs.existsSync(mainPy)) {
        const content = fs.readFileSync(mainPy, 'utf-8');
        if (content.includes('FastAPI')) return true;
      }
      const reqTxt = path.join(dir, 'requirements.txt');
      if (fs.existsSync(reqTxt)) {
        const content = fs.readFileSync(reqTxt, 'utf-8');
        if (content.includes('fastapi')) return true;
      }
      return false;
    },
  },
  {
    name: 'django',
    port: 8000,
    detectByFile: (dir) => fileExists(dir, 'manage.py'),
  },
  {
    name: 'gin',
    port: 8080,
    detectByFile: (dir) => {
      const goMod = path.join(dir, 'go.mod');
      if (!fs.existsSync(goMod)) return false;
      const content = fs.readFileSync(goMod, 'utf-8');
      return content.includes('github.com/gin-gonic/gin');
    },
  },
];

export function detectFramework(appDir: string): FrameworkDetection {
  // Phase 1: config file detection (highest priority)
  for (const fw of FRAMEWORKS) {
    if (fw.detectByConfig?.(appDir)) {
      return { framework: fw.name, port: fw.port };
    }
  }

  // Phase 2: file-based detection
  for (const fw of FRAMEWORKS) {
    if (fw.detectByFile?.(appDir)) {
      return { framework: fw.name, port: fw.port };
    }
  }

  // Phase 3: dependency-based detection
  const allDeps = readDeps(appDir);
  if (Object.keys(allDeps).length > 0) {
    for (const fw of FRAMEWORKS) {
      if (fw.detectByDep?.(allDeps)) {
        return { framework: fw.name, port: fw.port };
      }
    }

    // Node.js fallback — has package.json but no known framework
    if (fileExists(appDir, 'package.json')) {
      return { framework: 'node', port: 3000 };
    }
  }

  // Check for bare package.json (no deps but has package.json)
  if (fileExists(appDir, 'package.json')) {
    return { framework: 'node', port: 3000 };
  }

  return { framework: 'custom', port: null };
}

function fileExists(dir: string, name: string): boolean {
  return fs.existsSync(path.join(dir, name));
}

function readDeps(dir: string): Record<string, string> {
  const pkgPath = path.join(dir, 'package.json');
  if (!fs.existsSync(pkgPath)) return {};
  try {
    const pkg = JSON.parse(fs.readFileSync(pkgPath, 'utf-8'));
    return {
      ...(pkg.dependencies ?? {}),
      ...(pkg.devDependencies ?? {}),
    };
  } catch {
    return {};
  }
}
