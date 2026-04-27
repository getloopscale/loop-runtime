import { ApiError } from '../api/client';
import { classifyNetworkError } from '../output/error-message';

import { formatErrorMessage } from '../output/error-message';
interface CancelApiResponse {
  status: string;
}

export interface CancelDeps {
  getToken: () => string | null;
  cancelBuild: (buildId: string) => Promise<CancelApiResponse>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface CancelOptions {
  json?: boolean;
}

/**
 * CancelCommand cancels a running or queued build.
 *
 * Backend is 100% implemented:
 *   POST /api/v1/builds/{id}/cancel → 202 Accepted
 *   State machine: QUEUED/RUNNING/RELEASING → CANCELLED
 *
 * Exit codes:
 *   0 — build cancelled
 *   1 — usage error (missing build-id)
 *   3 — cancel failed (not found, already completed, API error)
 *   4 — auth error
 */
export class CancelCommand {
  private readonly deps: CancelDeps;

  constructor(deps: CancelDeps) {
    this.deps = deps;
  }

  async execute(buildId: string, opts: CancelOptions = {}): Promise<number> {
    // 1. Auth check
    if (!this.deps.getToken()) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    // 2. Build ID required
    if (!buildId || buildId.trim() === '') {
      return this.exitWithError(opts, 'Build ID is required. Usage: loopzero cancel <build-id>', 1, 'MISSING_ARGUMENT');
    }

    // 3. Call API
    try {
      await this.deps.cancelBuild(buildId);
    } catch (err) {
      return this.handleError(opts, err, buildId);
    }

    // 4. Success
    if (opts.json) {
      this.deps.writeTo(JSON.stringify({ ok: true, build_id: buildId, status: 'cancelled' }));
    } else {
      this.deps.writeTo(`Build ${buildId} cancelled.`);
    }
    return 0;
  }

  private exitWithError(opts: CancelOptions, message: string, exitCode: number, code?: string): number {
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

  private handleError(opts: CancelOptions, err: unknown, buildId: string): number {
    if (err instanceof ApiError) {
      if (err.exitCode === 4 || err.statusCode === 401) {
        return this.exitWithError(opts, 'Authentication failed. Run: loopzero login', 4, 'AUTH_REQUIRED');
      }
      if (err.statusCode === 404) {
        return this.exitWithError(opts, `Build ${buildId} not found.`, 3, 'BUILD_NOT_FOUND');
      }
      if (err.statusCode === 409) {
        return this.exitWithError(opts, `Build ${buildId} has already completed. Nothing to cancel.`, 3, 'BUILD_ALREADY_COMPLETED');
      }
      return this.exitWithError(opts, formatErrorMessage('Cancel failed', err.message, 'Try again'), 3, err.code);
    }
    // CHG-2026-04-23-AUDIT-25: surface API_UNREACHABLE for network errors.
    const net = classifyNetworkError(err);
    if (net.isNetwork) {
      return this.exitWithError(opts, net.message, 3, 'API_UNREACHABLE');
    }
    if (err instanceof Error) {
      return this.exitWithError(opts, formatErrorMessage('Cancel failed', err.message, 'Try again'), 3, 'UNEXPECTED_ERROR');
    }
    return this.exitWithError(opts, 'Cancel failed. Try again later.', 3, 'UNEXPECTED_ERROR');
  }
}
