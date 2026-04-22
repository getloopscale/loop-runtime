// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiError } from '../api/client';
import type { LoopZeroYamlResult } from '../adapters/loopzero-yaml';

interface ScaleApiResponse {
  app: string;
  replicas?: number;
  autoscaling?: {
    min: number;
    max: number;
    cpu_target: number;
  };
  status: string;
}

export interface ScaleDeps {
  getToken: () => string | null;
  findLoopZeroYaml: () => LoopZeroYamlResult | null;
  scale: (project: string, environment: string, app: string, config: ScaleConfig) => Promise<ScaleApiResponse>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

interface ScaleConfig {
  replicas?: number;
  min?: number;
  max?: number;
  cpu?: number;
}

export interface ScaleOptions {
  app: string;
  env?: string;
  replicas?: number;
  min?: number;
  max?: number;
  cpu?: number;
  json?: boolean;
}

/**
 * ScaleCommand adjusts replica count or autoscaling for a service.
 *
 * Usage:
 *   loopzero scale --app api --replicas 3
 *   loopzero scale --app api --min 2 --max 10 --cpu 80
 *
 * Exit codes:
 *   0 — success
 *   1 — usage error (missing app, invalid config, no loopzero.yaml)
 *   3 — scale failed (API error, not found)
 *   4 — auth error
 */
export class ScaleCommand {
  private readonly deps: ScaleDeps;

  constructor(deps: ScaleDeps) {
    this.deps = deps;
  }

  async execute(opts: ScaleOptions): Promise<number> {
    const environment = opts.env ?? 'production';

    // 1. Auth check
    if (!this.deps.getToken()) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    // 2. Validate --app is provided
    if (!opts.app || opts.app.trim() === '') {
      return this.exitWithError(opts, 'App name is required. Usage: loopzero scale --app <name> --replicas <n>', 1, 'MISSING_ARGUMENT');
    }

    // 3. Validate at least one scaling parameter
    const hasFixed = opts.replicas !== undefined;
    const hasAuto = opts.min !== undefined || opts.max !== undefined || opts.cpu !== undefined;

    if (!hasFixed && !hasAuto) {
      return this.exitWithError(
        opts,
        'Specify scaling config: --replicas <n> for fixed, or --min/--max/--cpu for autoscaling.',
        1,
        'MISSING_ARGUMENT',
      );
    }

    // 4. Validate cannot mix fixed + autoscaling
    if (hasFixed && hasAuto) {
      return this.exitWithError(
        opts,
        'Cannot mix --replicas with --min/--max/--cpu. Use --replicas for fixed scaling or --min/--max/--cpu for autoscaling.',
        1,
        'INVALID_ARGUMENT',
      );
    }

    // 5. Validate numeric ranges
    if (hasFixed) {
      if (opts.replicas! < 0 || opts.replicas! > 100) {
        return this.exitWithError(opts, 'Replicas must be between 0 and 100.', 1, 'INVALID_ARGUMENT');
      }
    }

    if (hasAuto) {
      if (opts.min !== undefined && opts.min < 0) {
        return this.exitWithError(opts, 'Minimum replicas cannot be negative.', 1, 'INVALID_ARGUMENT');
      }
      if (opts.max !== undefined && opts.max < 1) {
        return this.exitWithError(opts, 'Maximum replicas must be at least 1.', 1, 'INVALID_ARGUMENT');
      }
      if (opts.min !== undefined && opts.max !== undefined && opts.min > opts.max) {
        return this.exitWithError(opts, 'Minimum replicas cannot exceed maximum replicas.', 1, 'INVALID_ARGUMENT');
      }
      if (opts.cpu !== undefined && (opts.cpu < 1 || opts.cpu > 100)) {
        return this.exitWithError(opts, 'CPU target must be between 1 and 100 (percentage).', 1, 'INVALID_ARGUMENT');
      }
    }

    // 6. Find loopzero.yaml
    const yamlResult = this.deps.findLoopZeroYaml();
    if (!yamlResult) {
      return this.exitWithError(opts, 'No loopzero.yaml found. Run: loopzero init', 1, 'MISSING_CONFIG');
    }

    if (yamlResult.parseError) {
      return this.exitWithError(opts, `Invalid loopzero.yaml: ${yamlResult.parseError}`, 1, 'INVALID_CONFIG');
    }

    const project = yamlResult.content.project;

    // 7. Build scale config
    const config: ScaleConfig = {};
    if (hasFixed) {
      config.replicas = opts.replicas;
    } else {
      if (opts.min !== undefined) config.min = opts.min;
      if (opts.max !== undefined) config.max = opts.max;
      if (opts.cpu !== undefined) config.cpu = opts.cpu;
    }

    // 8. Call API
    let response: ScaleApiResponse;
    try {
      response = await this.deps.scale(project, environment, opts.app, config);
    } catch (err) {
      return this.handleError(opts, err);
    }

    // 9. Display result
    if (opts.json) {
      this.deps.writeTo(JSON.stringify({
        app: response.app,
        environment,
        replicas: response.replicas,
        autoscaling: response.autoscaling,
        status: response.status,
      }));
      return 0;
    }

    this.deps.writeTo(`Scaling ${opts.app} in ${environment}...`);
    this.deps.writeTo('');
    if (response.replicas !== undefined) {
      this.deps.writeTo(`  Replicas: ${response.replicas}`);
    }
    if (response.autoscaling) {
      this.deps.writeTo(`  Autoscaling: ${response.autoscaling.min}-${response.autoscaling.max} replicas`);
      this.deps.writeTo(`  CPU target:  ${response.autoscaling.cpu_target}%`);
    }
    this.deps.writeTo('');
    this.deps.writeTo('Scaling applied. Changes will take effect on the next deploy.');
    this.deps.writeTo('Run `loopzero deploy` to apply immediately.');

    return 0;
  }

  private exitWithError(opts: { json?: boolean }, message: string, exitCode: number, code?: string): number {
    if (opts.json) {
      const payload: Record<string, unknown> = { error: message, exit_code: exitCode };
      if (code) payload.code = code;
      this.deps.writeTo(JSON.stringify(payload));
    } else {
      this.deps.writeError(message);
    }
    return exitCode;
  }

  private handleError(opts: { json?: boolean }, err: unknown): number {
    if (err instanceof ApiError) {
      if (err.exitCode === 4) {
        return this.exitWithError(opts, 'Authentication failed. Run: loopzero login', 4, 'AUTH_REQUIRED');
      }
      if (err.statusCode === 404) {
        return this.exitWithError(opts, err.message, 3, err.code);
      }
      if (err.statusCode === 409) {
        return this.exitWithError(opts, 'Another operation is in progress. Try again in a moment.', 3, 'DEPLOY_IN_PROGRESS');
      }
      if (err.statusCode === 400) {
        return this.exitWithError(opts, err.message, 1, err.code);
      }
      return this.exitWithError(opts, err.message, err.exitCode, err.code);
    }
    if (err instanceof Error) {
      return this.exitWithError(opts, `Unexpected error: ${err.message}`, 3, 'UNEXPECTED_ERROR');
    }
    return this.exitWithError(opts, 'Unexpected error. Try again later.', 3, 'UNEXPECTED_ERROR');
  }
}
