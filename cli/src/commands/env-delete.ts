// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiError } from '../api/client';

import { formatErrorMessage } from '../output/error-message';
export interface EnvDeleteDeps {
  getToken: () => string | null;
  // M2 v1.2 T6.1 expanded (LIVE catch 2026-06-08): the closure carries
  // `project` so DELETE /api/v1/environments/{name}?project=<slug>
  // filters by the (name, project) tuple. Empty project preserves the
  // legacy production-only lookup (ADR D2 backward compat).
  deleteEnvironment: (name: string, project: string) => Promise<void>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface EnvDeleteOptions {
  /** Legacy safety flag: must repeat the env name ("type the name" pattern). */
  confirm?: string;
  /**
   * CHG-2026-04-23-AUDIT-11: UX consistency with the other destructive ops
   * (project/preview/domain/secret delete). Skips confirmation prompt and
   * bypasses the --confirm requirement. Production guard still applies.
   */
  yes?: boolean;
  json?: boolean;
  /**
   * M2 v1.2 T6.1 expanded (2026-06-08): symmetric `--project` flag with
   * `loopzero env create`. Required for non-legacy envs (every env created
   * post-M2 lives at an explicit (project, env) tuple); the legacy
   * production env still resolves without it.
   *
   * LIVE catch 2026-06-08: `loopzero env delete staging` (no flag) returned
   * 404 ENV_NOT_FOUND because the CLI sent DELETE without ?project=,
   * so the backend's tuple lookup failed. This flag closes that gap.
   */
  project?: string;
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

    // 4. Confirmation required — either --yes (UX consistency with peers)
    // or --confirm <name> (safer "type the name" pattern).
    if (!opts.yes && !opts.confirm) {
      return this.exitWithError(
        opts,
        `Confirmation required. Run: loopzero env delete ${name} --yes   (or --confirm ${name} for extra safety)`,
        1,
        'CONFIRMATION_REQUIRED',
      );
    }

    // 5. If --confirm is provided, it must match the name (safety check preserved).
    if (opts.confirm && opts.confirm !== name) {
      return this.exitWithError(
        opts,
        `Confirmation name "${opts.confirm}" does not match "${name}". Run: loopzero env delete ${name} --confirm ${name}`,
        1,
        'CONFIRMATION_MISMATCH',
      );
    }

    // 6. Call API. M2 v1.2: propagate `project` so the backend filters
    // by (name, project) tuple. Empty string preserves the legacy
    // production-only lookup (ADR D2). Whitespace-only is normalized
    // to empty (defensive — same convention as env-create).
    const project = (opts.project ?? '').trim();
    try {
      await this.deps.deleteEnvironment(name, project);

      if (opts.json) {
        const payload: Record<string, unknown> = { name, status: 'deleted' };
        if (project) {
          payload.project = project;
        }
        this.deps.writeTo(JSON.stringify(payload));
      } else {
        const where = project ? ` in project ${project}` : '';
        this.deps.writeTo(`Environment ${name} deleted${where}. All deployments in ${name} have been removed.`);
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
