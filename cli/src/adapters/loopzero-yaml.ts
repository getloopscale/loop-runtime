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

export interface LoopZeroYamlAutoscaling {
  min?: number;           // minimum replicas (default: 1)
  max?: number;           // maximum replicas (default: 3)
  targetCPU?: number;     // target CPU utilization % (default: 80)
}

export interface LoopZeroYamlApp {
  path: string;
  framework: string;
  type?: string;                     // "server" | "worker" | "frontend"
  port: number | null;
  build?: BuildStrategy;             // v2: how to build this app (default: "buildpack" if framework known)
  dockerfile?: string;               // v2: Dockerfile path relative to app path (default: "Dockerfile")
  render?: 'static' | 'server';     // v2: delivery mode (MEET-009). "static" = S3/CDN, "server" = container.
  env?: Record<string, string>;      // v2: build-time environment variables (not runtime)
  secrets?: string[];
  replicas?: number;                 // desired replica count (default: 1)
  autoscaling?: LoopZeroYamlAutoscaling; // HPA configuration
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

      // v2: render mode (MEET-009)
      const render = typeof cfg.render === 'string' && ['static', 'server'].includes(cfg.render)
        ? (cfg.render as 'static' | 'server')
        : undefined;

      // Replicas (must be >= 1)
      const rawReplicas = cfg.replicas != null ? Number(cfg.replicas) : undefined;
      const replicas = rawReplicas !== undefined && !isNaN(rawReplicas) && rawReplicas >= 1
        ? rawReplicas
        : undefined;

      // Autoscaling (HPA configuration)
      let autoscaling: LoopZeroYamlAutoscaling | undefined;
      if (cfg.autoscaling && typeof cfg.autoscaling === 'object' && !Array.isArray(cfg.autoscaling)) {
        const asCfg = cfg.autoscaling as Record<string, unknown>;
        const min = asCfg.min != null ? Number(asCfg.min) : undefined;
        const max = asCfg.max != null ? Number(asCfg.max) : undefined;
        const targetCPU = asCfg.targetCPU != null ? Number(asCfg.targetCPU) : undefined;

        // Validate: min >= 1, max >= min, targetCPU 1-100
        const validMin = min !== undefined && !isNaN(min) && min >= 1 ? min : undefined;
        const validMax = max !== undefined && !isNaN(max) && max >= (validMin ?? 1) ? max : undefined;
        const validCPU = targetCPU !== undefined && !isNaN(targetCPU) && targetCPU >= 1 && targetCPU <= 100
          ? targetCPU
          : undefined;

        autoscaling = {
          ...(validMin !== undefined ? { min: validMin } : {}),
          ...(validMax !== undefined ? { max: validMax } : {}),
          ...(validCPU !== undefined ? { targetCPU: validCPU } : {}),
        };
      }

      apps[appName] = {
        path: String(cfg.path ?? '.'),
        framework: String(cfg.framework ?? 'unknown'),
        type: cfg.type != null ? String(cfg.type) : undefined,
        port: port !== null && !isNaN(port) ? port : null,
        ...(build ? { build } : {}),
        ...(dockerfile ? { dockerfile } : {}),
        ...(render ? { render } : {}),
        ...(env ? { env } : {}),
        ...(secrets ? { secrets } : {}),
        ...(replicas !== undefined ? { replicas } : {}),
        ...(autoscaling ? { autoscaling } : {}),
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
