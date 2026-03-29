// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as fs from 'fs';
import * as path from 'path';

export type AppCategory = 'frontend' | 'server';

export interface FrameworkDetection {
  framework: string;
  port: number | null;
  category: AppCategory;
  buildCommand?: string;
  startCommand?: string;
}

// Frameworks classified as frontend are ALWAYS static delivery (zero pods).
// MEET-2026-03-29-016: No server-rendered frontends allowed.
// Remix is classified as server (SSR-first framework, static is a downgrade).
const FRONTEND_FRAMEWORKS = new Set(['nextjs', 'vite', 'react', 'vue', 'svelte', 'astro', 'angular']);

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
    // Remix is SSR-first — classified as server, not frontend (MEET-2026-03-29-016).
    // Users who want static Remix can manually set type: frontend + render: static.
    name: 'remix',
    port: 3000,
    detectByConfig: (dir) => fileExists(dir, 'remix.config.js'),
    detectByDep: (deps) => '@remix-run/node' in deps || '@remix-run/react' in deps,
  },
  {
    name: 'astro',
    port: 4321,
    detectByConfig: (dir) => fileExists(dir, 'astro.config.mjs') || fileExists(dir, 'astro.config.ts'),
    detectByDep: (deps) => 'astro' in deps,
  },
  {
    name: 'angular',
    port: 4200,
    detectByConfig: (dir) => fileExists(dir, 'angular.json'),
    detectByDep: (deps) => '@angular/core' in deps,
  },
  {
    name: 'svelte',
    port: 5173,
    detectByDep: (deps) => 'svelte' in deps || '@sveltejs/kit' in deps,
  },
  {
    name: 'vue',
    port: 5173,
    detectByDep: (deps) => 'vue' in deps,
  },
  {
    name: 'react',
    port: 3000,
    detectByDep: (deps) => 'react' in deps && !('next' in deps) && !('remix' in (deps)),
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
  const classify = (name: string, port: number): FrameworkDetection => ({
    framework: name,
    port,
    category: FRONTEND_FRAMEWORKS.has(name) ? 'frontend' : 'server',
  });

  // Phase 1: config file detection (highest priority)
  for (const fw of FRAMEWORKS) {
    if (fw.detectByConfig?.(appDir)) {
      return classify(fw.name, fw.port);
    }
  }

  // Phase 2: file-based detection
  for (const fw of FRAMEWORKS) {
    if (fw.detectByFile?.(appDir)) {
      return classify(fw.name, fw.port);
    }
  }

  // Phase 3: dependency-based detection
  const allDeps = readDeps(appDir);
  if (Object.keys(allDeps).length > 0) {
    for (const fw of FRAMEWORKS) {
      if (fw.detectByDep?.(allDeps)) {
        return classify(fw.name, fw.port);
      }
    }

    // Node.js fallback — has package.json but no known framework
    if (fileExists(appDir, 'package.json')) {
      return { framework: 'node', port: 3000, category: 'server' };
    }
  }

  // Check for bare package.json (no deps but has package.json)
  if (fileExists(appDir, 'package.json')) {
    return { framework: 'node', port: 3000, category: 'server' };
  }

  return { framework: 'custom', port: null, category: 'server' };
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
