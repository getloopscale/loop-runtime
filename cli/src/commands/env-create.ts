// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiError } from '../api/client';

interface EnvCreateResponse {
  name: string;
  status: string;
}

export interface EnvCreateDeps {
  getToken: () => string | null;
  createEnvironment: (name: string) => Promise<EnvCreateResponse>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface EnvCreateOptions {
  json?: boolean;
}

const MAX_ENVIRONMENT_NAME_LENGTH = 63;

/** Reserved names that cannot be used as environment names. */
const RESERVED_NAMES = ['default', 'system', 'internal'];

/** Validates environment name format: lowercase alphanumeric + hyphens, no leading hyphen, max 63 chars. */
function isValidEnvName(name: string): boolean {
  return /^[a-z][a-z0-9-]*$/.test(name) && name.length <= MAX_ENVIRONMENT_NAME_LENGTH;
}

/**
 * EnvCreateCommand creates a new environment.
 *
 * Handles 409 (already exists) as idempotent success.
 *
 * Exit codes:
 *   0 — success (including 409 idempotent)
 *   1 — usage error (missing name, invalid name, reserved name, API error)
 *   4 — auth error
 */
export class EnvCreateCommand {
  private readonly deps: EnvCreateDeps;

  constructor(deps: EnvCreateDeps) {
    this.deps = deps;
  }

  async execute(name: string, opts: EnvCreateOptions = {}): Promise<number> {
    // 1. Auth check
    if (!this.deps.getToken()) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    // 2. Name required
    if (!name || name.trim() === '') {
      return this.exitWithError(opts, 'Environment name is required. Usage: loopzero env create <name>', 1, 'MISSING_ARGUMENT');
    }

    // 3. Name format validation
    if (!isValidEnvName(name)) {
      return this.exitWithError(
        opts,
        'Invalid environment name. Must be lowercase, start with a letter, and contain only letters, numbers, and hyphens.',
        1,
        'INVALID_NAME',
      );
    }

    // 4. Reserved name check
    if (RESERVED_NAMES.includes(name)) {
      return this.exitWithError(
        opts,
        `"${name}" is a reserved name and cannot be used as an environment name.`,
        1,
        'RESERVED_NAME',
      );
    }

    // 5. Call API
    try {
      const result = await this.deps.createEnvironment(name);

      if (opts.json) {
        this.deps.writeTo(JSON.stringify({ ...result, status: 'created' }));
      } else {
        this.deps.writeTo(`Environment ${result.name} created.`);
      }
      return 0;
    } catch (err) {
      return this.handleError(opts, err, name);
    }
  }

  private exitWithError(opts: EnvCreateOptions, message: string, exitCode: number, code?: string): number {
    if (opts.json) {
      const payload: Record<string, unknown> = { error: message, exit_code: exitCode };
      if (code) {
        payload.code = code;
      }
      this.deps.writeTo(JSON.stringify(payload));
    } else {
      this.deps.writeError(message);
    }
    return exitCode;
  }

  private handleError(opts: EnvCreateOptions, err: unknown, name: string): number {
    if (err instanceof ApiError) {
      if (err.exitCode === 4 || err.statusCode === 401) {
        return this.exitWithError(opts, 'Authentication failed. Run: loopzero login', 4, 'AUTH_REQUIRED');
      }
      // 409 = already exists — idempotent success
      if (err.statusCode === 409) {
        if (opts.json) {
          this.deps.writeTo(JSON.stringify({ name, status: 'already_exists' }));
        } else {
          this.deps.writeTo(`Environment ${name} already exists.`);
        }
        return 0;
      }
      return this.exitWithError(opts, `Failed to create environment: ${err.message}. Try again.`, 1, err.code);
    }
    if (err instanceof Error) {
      return this.exitWithError(opts, `Failed to create environment: ${err.message}. Try again.`, 1, 'UNEXPECTED_ERROR');
    }
    return this.exitWithError(opts, 'Failed to create environment. Try again later.', 1, 'UNEXPECTED_ERROR');
  }
}
