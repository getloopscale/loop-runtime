// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as fs from 'fs';
import * as path from 'path';
import type { LoopZeroYaml } from './loopzero-yaml';

export interface ValidationError {
  field: string;
  message: string;
}

export interface ValidateConfigOptions {
  /** Skip filesystem checks (path existence, Dockerfile existence). Useful when
   *  the source will be validated server-side after upload (e.g., during deploy). */
  skipFilesystemChecks?: boolean;
}

/**
 * Validate a parsed LoopZeroYaml config strictly.
 * Returns an array of errors. Empty array = valid.
 *
 * Checks:
 *   - Path existence (relative to projectRoot) — skipped with skipFilesystemChecks
 *   - Type + port compatibility (server needs port, worker rejects port)
 *   - Build + dockerfile consistency — filesystem check skipped with skipFilesystemChecks
 *   - Unknown fields (warning-level, returned in warnings)
 */
export function validateConfig(
  config: LoopZeroYaml,
  projectRoot: string,
  opts?: ValidateConfigOptions,
): { errors: ValidationError[]; warnings: string[] } {
  const skipFs = opts?.skipFilesystemChecks ?? false;
  const errors: ValidationError[] = [];
  const warnings: string[] = [];

  // Version validation
  if (config.version !== undefined && config.version !== 1) {
    errors.push({
      field: 'version',
      message: `Unsupported version ${config.version} (current: 1)`,
    });
  }

  for (const [name, app] of Object.entries(config.apps)) {
    // Path existence (only when filesystem checks enabled)
    const appPath = path.resolve(projectRoot, app.path);
    if (!skipFs && !fs.existsSync(appPath)) {
      errors.push({
        field: `apps.${name}.path`,
        message: `App '${name}': path '${app.path}' does not exist`,
      });
    }

    // Type + port compatibility
    const appType = app.type ?? 'server';
    if (appType === 'server' && (app.port == null || app.port <= 0)) {
      errors.push({
        field: `apps.${name}.port`,
        message: `App '${name}': type 'server' requires port > 0`,
      });
    }
    if (appType === 'worker' && app.port != null && app.port > 0) {
      errors.push({
        field: `apps.${name}.port`,
        message: `App '${name}': type 'worker' should not have a port`,
      });
    }

    // Build + dockerfile consistency (filesystem check)
    if (!skipFs && app.build === 'dockerfile') {
      const dockerfileName = app.dockerfile ?? 'Dockerfile';
      const dockerfilePath = path.resolve(projectRoot, app.path, dockerfileName);
      if (fs.existsSync(path.resolve(projectRoot, app.path)) && !fs.existsSync(dockerfilePath)) {
        errors.push({
          field: `apps.${name}.dockerfile`,
          message: `App '${name}': Dockerfile not found at '${app.path}/${dockerfileName}'`,
        });
      }
    }

    if (app.build === 'buildpack' && app.framework) {
      const knownFrameworks = [
        'nextjs', 'vite', 'remix', 'nestjs', 'express', 'node',
        'gin', 'go', 'fastapi', 'django', 'python', 'custom',
      ];
      if (!knownFrameworks.includes(app.framework) && app.framework !== 'unknown') {
        warnings.push(`App '${name}': unknown framework '${app.framework}' — build may use generic strategy`);
      }
    }

    // MEET-2026-03-29-016: Frontend apps MUST use render: static (zero pods, CDN delivery).
    if (appType === 'frontend' && app.render !== 'static') {
      errors.push({
        field: `apps.${name}.render`,
        message: `App '${name}': frontend apps must use static delivery. Add 'render: static' to your loopzero.yaml. LoopZero serves frontends from CDN — no server-side rendering.`,
      });
    }

    // Env key validation
    if (app.env) {
      const envKeyRegex = /^[A-Z_][A-Z0-9_]*$/;
      for (const key of Object.keys(app.env)) {
        if (!envKeyRegex.test(key)) {
          errors.push({
            field: `apps.${name}.env.${key}`,
            message: `Env key '${key}' must be uppercase with underscores (e.g., NODE_ENV)`,
          });
        }
      }
    }

    // Detect unknown fields
    const knownFields = new Set([
      'path', 'framework', 'type', 'port', 'build', 'dockerfile', 'render', 'env', 'secrets',
    ]);
    if (typeof app === 'object' && app !== null) {
      for (const key of Object.keys(app)) {
        if (!knownFields.has(key)) {
          warnings.push(`Unknown field 'apps.${name}.${key}' -- ignored`);
        }
      }
    }
  }

  // MEET-2026-03-29-016: Max 1 server + 1 worker per project.
  // Frontends are always static (zero pods) and don't count toward the limit.
  const serverApps = Object.entries(config.apps).filter(([, a]) => (a.type ?? 'server') === 'server');
  const workerApps = Object.entries(config.apps).filter(([, a]) => a.type === 'worker');

  if (serverApps.length > 1) {
    errors.push({
      field: 'apps',
      message: `Project has ${serverApps.length} API services (${serverApps.map(([n]) => n).join(', ')}) but only 1 is allowed. Split into separate projects or consolidate into a single API.`,
    });
  }
  if (workerApps.length > 1) {
    errors.push({
      field: 'apps',
      message: `Project has ${workerApps.length} workers (${workerApps.map(([n]) => n).join(', ')}) but only 1 is allowed. Combine background tasks into a single worker.`,
    });
  }

  return { errors, warnings };
}
