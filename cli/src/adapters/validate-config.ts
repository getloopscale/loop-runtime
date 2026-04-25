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
    // RFC 1123 app name validation
    if (!/^[a-z][a-z0-9-]*$/.test(name) || name.length > 63 || name.endsWith('-')) {
      errors.push({
        field: `apps.${name}`,
        message: `App name '${name}' is invalid. Must start with a lowercase letter, contain only lowercase letters, digits, and hyphens, not end with a hyphen, and be at most 63 characters.`,
      });
    }

    // SEC-10: Reject path traversal (defense-in-depth, server has SEC-3)
    const appPath = path.resolve(projectRoot, app.path);
    if (!appPath.startsWith(path.resolve(projectRoot))) {
      errors.push({
        field: `apps.${name}.path`,
        message: `App '${name}': path '${app.path}' escapes project root. Use a relative path within the project.`,
      });
      continue; // skip further checks for this app
    }

    // Path existence (only when filesystem checks enabled)
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

    // Framework validation — reject unknown frameworks BEFORE build (not a warning).
    // This prevents 5-minute build failures from unrecognized frameworks.
    const knownFrameworks = [
      // Node.js ecosystem
      'nextjs', 'next', 'vite', 'remix', 'nestjs', 'express', 'node',
      'react', 'vue', 'svelte', 'astro', 'angular', 'fastify',
      // Go
      'gin', 'go',
      // Python
      'fastapi', 'django', 'python',
      // Ruby
      'sinatra', 'rails', 'ruby',
      // Java
      'spring', 'spring-boot', 'java',
      // PHP
      'laravel', 'slim', 'php',
      // Rust
      'axum', 'actix', 'rocket', 'rust',
      // Custom (user-provided Dockerfile)
      'custom',
    ];
    const fw = app.framework ?? 'custom';
    if (fw !== 'unknown' && !knownFrameworks.includes(fw)) {
      errors.push({
        field: `apps.${name}.framework`,
        message: `App '${name}': unknown framework '${fw}'. Supported: ${knownFrameworks.filter(f => f !== 'custom').join(', ')}. Use 'framework: custom' with a Dockerfile for unsupported frameworks.`,
      });
    }

    // Custom framework requires Dockerfile in app path
    if (!skipFs && (fw === 'custom' || fw === 'unknown')) {
      const dockerfileName = app.dockerfile ?? 'Dockerfile';
      const appPathResolved = path.resolve(projectRoot, app.path);
      const dockerfilePath = path.resolve(appPathResolved, dockerfileName);
      if (fs.existsSync(appPathResolved) && !fs.existsSync(dockerfilePath)) {
        errors.push({
          field: `apps.${name}.dockerfile`,
          message: `App '${name}': framework 'custom' requires a Dockerfile at ${app.path}/${dockerfileName}. Add a Dockerfile or set a supported framework.`,
        });
      }
    }

    // Auto-detect: if user has Dockerfile in app path but build is 'buildpack', warn
    if (!skipFs && app.build !== 'dockerfile' && fw !== 'custom' && fw !== 'unknown') {
      const appPathResolved = path.resolve(projectRoot, app.path);
      const dockerfilePath = path.resolve(appPathResolved, 'Dockerfile');
      if (fs.existsSync(dockerfilePath)) {
        warnings.push(`App '${name}': Dockerfile found at ${app.path}/Dockerfile but build is '${app.build ?? 'buildpack'}'. Set 'build: dockerfile' in loopzero.yaml to use your custom Dockerfile, or remove it to use the auto-generated one.`);
      }
    }

    // Frontend rendering validation — frontends are ALWAYS static (enforced in normalizeApp).
    // This check catches loopzero.yaml files that explicitly set render: server on frontends.
    if (appType === 'frontend' && app.render === 'server') {
      errors.push({
        field: `apps.${name}.render`,
        message: `App '${name}': frontend apps are always static (CDN delivery, zero pods). Remove 'render: server'. If you need a server process, use 'type: server' instead.`,
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

    // B15 (CHG-2026-04-23-B15 / MEET-2026-04-23-059): validate scaling fields.
    // Plan caps (max_replicas_per_service: starter=2, pro=5, team=10, enterprise=NULL)
    // are enforced server-side in BuildOrchestrator.SubmitBuild. The CLI only rejects
    // values that are structurally invalid for ANY plan (< 1 or > 100).
    const MAX_REPLICAS = 100;
    const hasReplicas = app.replicas !== undefined && app.replicas !== null;
    const hasAutoscaling = app.autoscaling !== undefined && app.autoscaling !== null;

    if (hasReplicas) {
      const r = app.replicas as number;
      if (typeof r !== 'number' || !Number.isInteger(r) || r < 1) {
        errors.push({
          field: `apps.${name}.replicas`,
          message: `App '${name}': replicas must be an integer >= 1 (got ${JSON.stringify(app.replicas)}).`,
        });
      } else if (r > MAX_REPLICAS) {
        errors.push({
          field: `apps.${name}.replicas`,
          message: `App '${name}': replicas ${r} exceeds maximum of ${MAX_REPLICAS} per service.`,
        });
      }
    }

    if (hasAutoscaling) {
      const as = app.autoscaling!;
      const min = as.min;
      const max = as.max;
      const targetCPU = as.targetCPU;
      if (min !== undefined && (typeof min !== 'number' || !Number.isInteger(min) || min < 0)) {
        errors.push({
          field: `apps.${name}.autoscaling.min`,
          message: `App '${name}': autoscaling.min must be an integer >= 0 (got ${JSON.stringify(min)}).`,
        });
      }
      // Scale-to-zero (min: 0) is not supported for workers — they don't receive
      // HTTP traffic, so there's no trigger to wake them up.
      if (min === 0 && appType === 'worker') {
        errors.push({
          field: `apps.${name}.autoscaling.min`,
          message: `App '${name}': autoscaling.min = 0 (scale-to-zero) is not supported for workers. Workers don't receive HTTP traffic for wake-up. Use min >= 1.`,
        });
      }
      if (max !== undefined && (typeof max !== 'number' || !Number.isInteger(max) || max < 1)) {
        errors.push({
          field: `apps.${name}.autoscaling.max`,
          message: `App '${name}': autoscaling.max must be an integer >= 1 (got ${JSON.stringify(max)}).`,
        });
      }
      if (max !== undefined && typeof max === 'number' && max > MAX_REPLICAS) {
        errors.push({
          field: `apps.${name}.autoscaling.max`,
          message: `App '${name}': autoscaling.max ${max} exceeds maximum of ${MAX_REPLICAS} per service.`,
        });
      }
      if (min !== undefined && max !== undefined && typeof min === 'number' && typeof max === 'number' && max < min) {
        errors.push({
          field: `apps.${name}.autoscaling`,
          message: `App '${name}': autoscaling.max (${max}) must be >= autoscaling.min (${min}).`,
        });
      }
      if (targetCPU !== undefined) {
        if (typeof targetCPU !== 'number' || !Number.isInteger(targetCPU) || targetCPU < 1 || targetCPU > 100) {
          errors.push({
            field: `apps.${name}.autoscaling.targetCPU`,
            message: `App '${name}': autoscaling.targetCPU must be an integer between 1 and 100 (got ${JSON.stringify(targetCPU)}).`,
          });
        }
      }
      const cooldown = as.cooldown;
      if (cooldown !== undefined) {
        if (typeof cooldown !== 'number' || !Number.isInteger(cooldown) || cooldown < 30 || cooldown > 3600) {
          errors.push({
            field: `apps.${name}.autoscaling.cooldown`,
            message: `App '${name}': autoscaling.cooldown must be an integer between 30 and 3600 seconds (got ${JSON.stringify(cooldown)}).`,
          });
        }
      }

      // Detect unknown autoscaling sub-fields
      const knownAutoscalingFields = new Set(['min', 'max', 'targetCPU', 'cooldown']);
      if (typeof as === 'object' && as !== null) {
        for (const key of Object.keys(as)) {
          if (!knownAutoscalingFields.has(key)) {
            warnings.push(`Unknown field 'apps.${name}.autoscaling.${key}' -- ignored`);
          }
        }
      }
    }

    // Mutual exclusion (MEET-059 Constraint 7): replicas and autoscaling set together
    // is ambiguous — fail-fast with a message naming both fields.
    if (hasReplicas && hasAutoscaling) {
      errors.push({
        field: `apps.${name}`,
        message: `App '${name}': 'replicas' and 'autoscaling' are mutually exclusive — choose one. Use 'replicas' for a fixed count, or 'autoscaling' for HPA-driven scaling.`,
      });
    }

    // Detect unknown fields
    const knownFields = new Set([
      'path', 'framework', 'type', 'port', 'build', 'dockerfile', 'render', 'env', 'secrets', 'commands',
      // B15: scaling fields are now known (MEET-2026-04-23-059).
      'replicas', 'autoscaling',
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
