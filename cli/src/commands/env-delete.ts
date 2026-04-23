// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiError } from '../api/client';

import { formatErrorMessage } from '../output/error-message';
export interface EnvDeleteDeps {
  getToken: () => string | null;
  deleteEnvironment: (name: string) => Promise<void>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface EnvDeleteOptions {
  confirm?: string;
  json?: boolean;
}

/**
 * EnvDeleteCommand deletes an environment.
 *
 * Requires --confirm <name> for safety. Refuses to delete "production".
 *
 * Exit codes:
 *   0 — success
 *   1 — usage error (no confirm, production guard, mismatch)
 *   3 — delete failed
 *   4 — auth error
 */
export class EnvDeleteCommand {
  private readonly deps: EnvDeleteDeps;

  constructor(deps: EnvDeleteDeps) {
    this.deps = deps;
  }

  async execute(name: string, opts: EnvDeleteOptions = {}): Promise<number> {
    // 1. Auth check
    if (!this.deps.getToken()) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    // 2. Name required
    if (!name || name.trim() === '') {
      return this.exitWithError(opts, 'Environment name is required. Usage: loopzero env delete <name> --confirm <name>', 1, 'MISSING_ARGUMENT');
    }

    // 3. Production guard — NEVER deletable
    if (name === 'production') {
      return this.exitWithError(
        opts,
        'Cannot delete the production environment. This is a safety constraint that cannot be overridden.',
        1,
        'PRODUCTION_PROTECTED',
      );
    }

    // 4. Confirm flag required
    if (!opts.confirm) {
      return this.exitWithError(
        opts,
        `Confirmation required. Run: loopzero env delete ${name} --confirm ${name}`,
        1,
        'CONFIRMATION_REQUIRED',
      );
    }

    // 5. Confirm must match name
    if (opts.confirm !== name) {
      return this.exitWithError(
        opts,
        `Confirmation name "${opts.confirm}" does not match "${name}". Run: loopzero env delete ${name} --confirm ${name}`,
        1,
        'CONFIRMATION_MISMATCH',
      );
    }

    // 6. Call API
    try {
      await this.deps.deleteEnvironment(name);

      if (opts.json) {
        this.deps.writeTo(JSON.stringify({ name, status: 'deleted' }));
      } else {
        this.deps.writeTo(`Environment ${name} deleted. All deployments in ${name} have been removed.`);
      }
      return 0;
    } catch (err) {
      return this.handleError(opts, err, name);
    }
  }

  private exitWithError(opts: EnvDeleteOptions, message: string, exitCode: number, code?: string): number {
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

  private handleError(opts: EnvDeleteOptions, err: unknown, name: string): number {
    if (err instanceof ApiError) {
      if (err.exitCode === 4 || err.statusCode === 401) {
        return this.exitWithError(opts, 'Authentication failed. Run: loopzero login', 4, 'AUTH_REQUIRED');
      }
      if (err.statusCode === 404) {
        return this.exitWithError(opts, `Environment "${name}" not found.`, 1, 'ENV_NOT_FOUND');
      }
      return this.exitWithError(opts, formatErrorMessage('Failed to delete environment', err.message, 'Try again'), 3, err.code);
    }
    if (err instanceof Error) {
      return this.exitWithError(opts, formatErrorMessage('Failed to delete environment', err.message, 'Try again'), 3, 'DELETE_FAILED');
    }
    return this.exitWithError(opts, 'Failed to delete environment. Try again later.', 3, 'DELETE_FAILED');
  }
}
