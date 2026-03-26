// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as fs from 'fs';
import * as path from 'path';
import { LoopZeroYaml, LoopZeroYamlApp, BuildStrategy } from './loopzero-yaml';

export interface ValidationError {
  field: string;
  message: string;
}

/**
 * Validate a parsed LoopZeroYaml config strictly.
 * Returns an array of errors. Empty array = valid.
 *
 * Checks:
 *   - Path existence (relative to projectRoot)
 *   - Type + port compatibility (server needs port, worker rejects port)
 *   - Build + dockerfile consistency
 *   - Unknown fields (warning-level, returned in warnings)
 */
export function validateConfig(
  config: LoopZeroYaml,
  projectRoot: string,
): { errors: ValidationError[]; warnings: string[] } {
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
    // Path existence
    const appPath = path.resolve(projectRoot, app.path);
    if (!fs.existsSync(appPath)) {
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

    // Build + dockerfile consistency
    if (app.build === 'dockerfile') {
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
      'path', 'framework', 'type', 'port', 'build', 'dockerfile', 'env', 'secrets',
    ]);
    if (typeof app === 'object' && app !== null) {
      for (const key of Object.keys(app)) {
        if (!knownFields.has(key)) {
          warnings.push(`Unknown field 'apps.${name}.${key}' -- ignored`);
        }
      }
    }
  }

  return { errors, warnings };
}
