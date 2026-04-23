import { ApiError } from '../api/client';
import type { LoopZeroYamlResult } from '../adapters/loopzero-yaml';

interface PromoteApiResponse {
  promotion_id: string;
  status: string;
  source_release: string;
  new_release: string;
  from: string;
  to: string;
  url?: string;
}

export interface PromoteDeps {
  getToken: () => string | null;
  findLoopZeroYaml: () => LoopZeroYamlResult | null;
  promote: (project: string, from: string, to: string) => Promise<PromoteApiResponse>;
  confirm?: (message: string) => Promise<boolean>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
  isCI?: boolean;
}

export interface PromoteOptions {
  from?: string;
  to?: string;
  json?: boolean;
  yes?: boolean;
}

export class PromoteCommand {
  private readonly deps: PromoteDeps;

  constructor(deps: PromoteDeps) {
    this.deps = deps;
  }

  async execute(opts: PromoteOptions = {}): Promise<number> {
    const from = opts.from ?? 'staging';
    const to = opts.to ?? 'production';

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

    // 3. Validate from != to
    if (from === to) {
      return this.exitWithError(opts, 'Source and target environments must be different.', 1, 'SAME_ENVIRONMENT');
    }

    // 4. Confirmation required (unless --yes)
    if (!opts.yes) {
      if (this.deps.isCI) {
        return this.exitWithError(opts, 'Confirmation required. Use --yes in CI mode.', 1, 'CONFIRMATION_REQUIRED');
      }
      if (this.deps.confirm) {
        const confirmed = await this.deps.confirm(
          `Promote ${from} to ${to}? [y/N] `
        );
        if (!confirmed) {
          if (opts.json) {
            this.deps.writeTo(JSON.stringify({ status: 'cancelled' }));
          } else {
            this.deps.writeTo('Promotion cancelled.');
          }
          return 0;
        }
      }
    }

    // 5. Call API
    const startTime = Date.now();
    let response: PromoteApiResponse;
    try {
      response = await this.deps.promote(project, from, to);
    } catch (err) {
      return this.handleError(opts, err);
    }
    const durationSeconds = Math.round((Date.now() - startTime) / 1000);

    // 6. Display result
    if (opts.json) {
      this.deps.writeTo(JSON.stringify({
        promotion_id: response.promotion_id,
        status: response.status,
        source_release: response.source_release,
        new_release: response.new_release,
        from: response.from,
        to: response.to,
        duration_seconds: durationSeconds,
        url: response.url,
      }));
      return 0;
    }

    this.deps.writeTo(`Promoting ${from} to ${to}...`);
    this.deps.writeTo('');
    this.deps.writeTo(`  Source: ${response.source_release} (${from})`);
    this.deps.writeTo(`  Target: ${response.new_release} (${to})`);
    this.deps.writeTo('');
    this.deps.writeTo(`Promotion started in ${durationSeconds}s.`);
    if (response.url) {
      this.deps.writeTo(`URL: ${response.url}`);
    }

    return 0;
  }

  private exitWithError(opts: PromoteOptions, message: string, exitCode: number, code?: string): number {
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

  private handleError(opts: PromoteOptions, err: unknown): number {
    if (err instanceof ApiError) {
      if (err.exitCode === 4) {
        return this.exitWithError(opts, 'Authentication failed. Run: loopzero login', 4, 'AUTH_REQUIRED');
      }
      if (err.statusCode === 409) {
        return this.exitWithError(opts, 'Another operation is in progress. Try again in a moment.', 3, 'DEPLOY_IN_PROGRESS');
      }
      if (err.statusCode === 404) {
        return this.exitWithError(opts, err.message, 3, err.code);
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
