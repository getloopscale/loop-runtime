import { ApiError } from '../api/client';
import type { LoopZeroYamlResult } from '../adapters/loopzero-yaml';
import { classifyNetworkError } from '../output/error-message';

interface RollbackApiResponse {
  status: string;
  rolled_back_release: string;
  reactivated_release: string;
  environment: string;
  url?: string;
}

export interface RollbackDeps {
  getToken: () => string | null;
  findLoopZeroYaml: () => LoopZeroYamlResult | null;
  rollback: (project: string, environment: string, toReleaseId?: string) => Promise<RollbackApiResponse>;
  confirm?: (message: string) => Promise<boolean>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
  isCI?: boolean;
}

export interface RollbackOptions {
  env?: string;
  to?: string;
  json?: boolean;
  yes?: boolean;
}

export class RollbackCommand {
  private readonly deps: RollbackDeps;

  constructor(deps: RollbackDeps) {
    this.deps = deps;
  }

  async execute(opts: RollbackOptions = {}): Promise<number> {
    const environment = opts.env ?? 'production';

    // 1. Auth check
    if (!this.deps.getToken()) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    // 2. Find loopzero.yaml
    const yamlResult = this.deps.findLoopZeroYaml();
    if (!yamlResult) {
      return this.exitWithError(opts, 'No loopzero.yaml found. Run: loopzero init', 1, 'MISSING_CONFIG');
    }

    if (yamlResult.parseError) {
      return this.exitWithError(opts, `Invalid loopzero.yaml: ${yamlResult.parseError}`, 1, 'INVALID_CONFIG');
    }

    const project = yamlResult.content.project;

    // 3. Display migration warning (always, even with --yes)
    if (!opts.json) {
      this.deps.writeTo('Warning: Rollback does not revert database migrations.');
      this.deps.writeTo('  If the deployment you\'re reverting from included schema changes,');
      this.deps.writeTo('  the previous version may not be compatible with the current database.');
      this.deps.writeTo('');
    }

    // 3b. Confirmation required (unless --yes)
    if (!opts.yes) {
      if (this.deps.isCI) {
        return this.exitWithError(opts, 'Confirmation required. Use --yes in CI mode.', 1, 'CONFIRMATION_REQUIRED');
      }
      if (this.deps.confirm) {
        const confirmed = await this.deps.confirm(
          `Rollback ${environment}${opts.to ? ` to release ${opts.to}` : ' to previous release'}? [y/N] `
        );
        if (!confirmed) {
          if (opts.json) {
            this.deps.writeTo(JSON.stringify({ status: 'cancelled' }));
          } else {
            this.deps.writeTo('Rollback cancelled.');
          }
          return 0;
        }
      }
    }

    // 4. Call API
    const startTime = Date.now();
    let response: RollbackApiResponse;
    try {
      response = await this.deps.rollback(project, environment, opts.to);
    } catch (err) {
      return this.handleError(opts, err);
    }
    const durationSeconds = Math.round((Date.now() - startTime) / 1000);

    // 5. Display result
    if (opts.json) {
      this.deps.writeTo(JSON.stringify({
        status: response.status,
        rolled_back_release: response.rolled_back_release,
        active_release: response.reactivated_release,
        environment: response.environment,
        duration_seconds: durationSeconds,
        url: response.url,
      }));
      return 0;
    }

    this.deps.writeTo(`Rolling back ${environment}...`);
    this.deps.writeTo('');
    this.deps.writeTo(`  Rolled back: ${response.rolled_back_release}`);
    this.deps.writeTo(`  Reactivated: ${response.reactivated_release}`);
    this.deps.writeTo('');
    this.deps.writeTo(`Rollback completed in ${durationSeconds}s.`);
    this.deps.writeTo(`Active release: ${response.reactivated_release}`);
    if (response.url) {
      this.deps.writeTo(`URL: ${response.url}`);
    }

    return 0;
  }

  private exitWithError(opts: RollbackOptions, message: string, exitCode: number, code?: string): number {
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

  private handleError(opts: RollbackOptions, err: unknown): number {
    if (err instanceof ApiError) {
      if (err.exitCode === 4) {
        return this.exitWithError(opts, 'Authentication failed. Run: loopzero login', 4, 'AUTH_REQUIRED');
      }
      if (err.statusCode === 409 && err.code === 'CONCURRENT_ACTIVATION') {
        // T5.1 CV-M-1 of plan 2026-06-01-p3-infra-observability-test-debt:
        // partial unique index `idx_releases_active_unique` (migration
        // 000022) rejected concurrent release activation. See
        // promote.ts for the design rationale (same mapping pattern).
        return this.exitWithError(opts, 'A previous release activation is still in progress. The platform will converge automatically — retry in a moment.', 3, 'CONCURRENT_ACTIVATION');
      }
      if (err.statusCode === 409) {
        return this.exitWithError(opts, 'Another operation is in progress. Try again in a moment.', 3, 'DEPLOY_IN_PROGRESS');
      }
      if (err.statusCode === 404) {
        return this.exitWithError(opts, err.message, 3, err.code);
      }
      return this.exitWithError(opts, err.message, err.exitCode, err.code);
    }
    // CHG-2026-04-23-AUDIT-25: surface API_UNREACHABLE for network errors.
    const net = classifyNetworkError(err);
    if (net.isNetwork) {
      return this.exitWithError(opts, net.message, 3, 'API_UNREACHABLE');
    }
    if (err instanceof Error) {
      return this.exitWithError(opts, `Unexpected error: ${err.message}`, 3, 'UNEXPECTED_ERROR');
    }
    return this.exitWithError(opts, 'Unexpected error. Try again later.', 3, 'UNEXPECTED_ERROR');
  }
}
