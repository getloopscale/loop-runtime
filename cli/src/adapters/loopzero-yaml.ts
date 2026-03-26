// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as path from 'path';
import * as fs from 'fs';
import YAML from 'js-yaml';

// BuildStrategy defines how an app's container image is built.
//   - "dockerfile": User-provided Dockerfile (field `dockerfile` specifies path, default "Dockerfile")
//   - "buildpack": Auto-generated Dockerfile based on detected framework
//   - "custom": User-provided build script (framework must be "custom")
export type BuildStrategy = 'dockerfile' | 'buildpack' | 'custom';

export interface LoopZeroYamlApp {
  path: string;
  framework: string;
  type?: string;                     // "server" | "worker" | "frontend"
  port: number | null;
  build?: BuildStrategy;             // v2: how to build this app (default: "buildpack" if framework known)
  dockerfile?: string;               // v2: Dockerfile path relative to app path (default: "Dockerfile")
  env?: Record<string, string>;      // v2: build-time environment variables (not runtime)
  secrets?: string[];
}

export interface LoopZeroYaml {
  version?: number;                  // v2: schema version (current: 1). Missing = legacy format.
  project: string;
  apps: Record<string, LoopZeroYamlApp>;
  ignore?: string[];                 // v2: glob patterns of dirs that are NOT deployable apps
}

export interface LoopZeroYamlResult {
  path: string;
  content: LoopZeroYaml;
  parseError?: string;
}

/**
 * Parse loopzero.yaml content using js-yaml.
 * Validates that the required structure (project + apps) is present.
 */
export function parseLoopZeroYaml(raw: string): LoopZeroYaml {
  const doc = YAML.load(raw) as Record<string, unknown> | null;

  if (!doc || typeof doc !== 'object') {
    throw new Error('could not parse loopzero.yaml — check YAML syntax');
  }

  const project = String(doc.project ?? doc.name ?? '');
  const rawApps = doc.apps;

  if (!project && (!rawApps || typeof rawApps !== 'object')) {
    throw new Error('could not parse loopzero.yaml — check YAML syntax');
  }

  // v2 fields
  const version = typeof doc.version === 'number' ? doc.version : undefined;
  const ignore = Array.isArray(doc.ignore)
    ? (doc.ignore as unknown[]).map((s) => String(s))
    : undefined;

  const apps: Record<string, LoopZeroYamlApp> = {};

  if (rawApps && typeof rawApps === 'object' && !Array.isArray(rawApps)) {
    for (const [appName, appConfig] of Object.entries(rawApps as Record<string, unknown>)) {
      const cfg = appConfig as Record<string, unknown> | null;
      if (!cfg || typeof cfg !== 'object') {
        apps[appName] = { path: '.', framework: 'unknown', port: null };
        continue;
      }

      const port = cfg.port != null ? Number(cfg.port) : null;
      const secrets = Array.isArray(cfg.secrets)
        ? cfg.secrets.map((s: unknown) => String(s))
        : undefined;

      // v2 fields
      const build = typeof cfg.build === 'string' && ['dockerfile', 'buildpack', 'custom'].includes(cfg.build)
        ? (cfg.build as BuildStrategy)
        : undefined;
      const dockerfile = typeof cfg.dockerfile === 'string' ? cfg.dockerfile : undefined;
      const env = cfg.env && typeof cfg.env === 'object' && !Array.isArray(cfg.env)
        ? Object.fromEntries(
            Object.entries(cfg.env as Record<string, unknown>).map(([k, v]) => [k, String(v)])
          )
        : undefined;

      apps[appName] = {
        path: String(cfg.path ?? '.'),
        framework: String(cfg.framework ?? 'unknown'),
        type: cfg.type != null ? String(cfg.type) : undefined,
        port: port !== null && !isNaN(port) ? port : null,
        ...(build ? { build } : {}),
        ...(dockerfile ? { dockerfile } : {}),
        ...(env ? { env } : {}),
        ...(secrets ? { secrets } : {}),
      };
    }
  }

  return {
    project,
    apps,
    ...(version !== undefined ? { version } : {}),
    ...(ignore ? { ignore } : {}),
  };
}

/**
 * Walk up from cwd to gitRoot looking for loopzero.yaml, parse it, and return the result.
 */
export function findLoopZeroYaml(cwd: string, gitRoot: string): LoopZeroYamlResult | null {
  let dir = cwd;
  while (true) {
    const candidate = path.join(dir, 'loopzero.yaml');
    if (fs.existsSync(candidate)) {
      const raw = fs.readFileSync(candidate, 'utf-8');
      try {
        const content = parseLoopZeroYaml(raw);
        return { path: candidate, content };
      } catch (e) {
        return { path: candidate, content: { project: '', apps: {} }, parseError: (e as Error).message };
      }
    }
    if (dir === gitRoot || dir === path.dirname(dir)) {
      return null;
    }
    dir = path.dirname(dir);
  }
}
