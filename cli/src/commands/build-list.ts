import { ApiError } from '../api/client';
import type { LoopZeroYamlResult } from '../adapters/loopzero-yaml';
import { classifyNetworkError } from '../output/error-message';

interface BuildItem {
  build_id: string;
  project: string;
  environment: string;
  status: string;
  commit_sha: string;
  created_at: string;
  duration_seconds?: number;
}

interface BuildListResponse {
  builds: BuildItem[];
  total: number;
}

export interface BuildListDeps {
  getToken: () => string | null;
  findLoopZeroYaml: () => LoopZeroYamlResult | null;
  listBuilds: (project: string, env?: string, limit?: number) => Promise<BuildListResponse>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface BuildListOptions {
  env?: string;
  limit?: number;
  json?: boolean;
}

/**
 * BuildListCommand lists recent builds for the current project.
 *
 * Exit codes:
 *   0 — success (including empty list)
 *   1 — usage error (no loopzero.yaml, API error)
 *   4 — auth error
 */
export class BuildListCommand {
  private readonly deps: BuildListDeps;

  constructor(deps: BuildListDeps) {
    this.deps = deps;
  }

  async execute(opts: BuildListOptions = {}): Promise<number> {
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

    // 3. Call API
    let response: BuildListResponse;
    try {
      response = await this.deps.listBuilds(project, opts.env, opts.limit);
    } catch (err) {
      return this.handleError(opts, err);
    }

    // 4. Display result
    if (opts.json) {
      this.deps.writeTo(JSON.stringify({ builds: response.builds, total: response.total }));
      return 0;
    }

    if (response.builds.length === 0) {
      this.deps.writeTo('No builds found.');
      return 0;
    }

    this.deps.writeTo(`Builds for ${project}:`);
    this.deps.writeTo('');
    // CHG-2026-04-23-AUDIT-5: build IDs are 22 chars (`build-` + 16 hex), so we
    // truncate to 14 chars for display and widen the column to match. Header is
    // "COMMIT" not "VERSION" — the content is a shortened commit SHA, not semver.
    // Full ID remains available in --json output.
    this.deps.writeTo(
      `  ${pad('ID', 16)}${pad('STATUS', 11)}${pad('COMMIT', 9)}${pad('STARTED', 21)}DURATION`
    );
    for (const b of response.builds) {
      const started = formatTimestamp(b.created_at);
      const duration = b.duration_seconds != null ? `${b.duration_seconds}s` : '-';
      const sha = b.commit_sha.substring(0, 7);
      const shortId = b.build_id.length > 14 ? b.build_id.substring(0, 14) : b.build_id;
      this.deps.writeTo(
        `  ${pad(shortId, 16)}${pad(b.status, 11)}${pad(sha, 9)}${pad(started, 21)}${duration}`
      );
    }
    this.deps.writeTo('');
    this.deps.writeTo(`${response.total} build${response.total !== 1 ? 's' : ''}`);

    return 0;
  }

  private exitWithError(opts: BuildListOptions, message: string, exitCode: number, code?: string): number {
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

  private handleError(opts: BuildListOptions, err: unknown): number {
    if (err instanceof ApiError) {
      if (err.exitCode === 4 || err.statusCode === 401) {
        return this.exitWithError(opts, 'Authentication failed. Run: loopzero login', 4, 'AUTH_REQUIRED');
      }
      return this.exitWithError(opts, `Failed to list builds: ${err.message}. Try again.`, 1, err.code);
    }
    // CHG-2026-04-23-AUDIT-24: surface API_UNREACHABLE for network errors.
    const net = classifyNetworkError(err);
    if (net.isNetwork) {
      return this.exitWithError(opts, net.message, 1, 'API_UNREACHABLE');
    }
    if (err instanceof Error) {
      return this.exitWithError(opts, `Failed to list builds: ${err.message}. Try again.`, 1, 'UNEXPECTED_ERROR');
    }
    return this.exitWithError(opts, 'Failed to list builds. Try again later.', 1, 'UNEXPECTED_ERROR');
  }
}

function pad(str: string, len: number): string {
  return str.length >= len ? str + ' ' : str + ' '.repeat(len - str.length);
}

function formatTimestamp(iso: string): string {
  try {
    const d = new Date(iso);
    const yyyy = d.getFullYear();
    const mm = String(d.getMonth() + 1).padStart(2, '0');
    const dd = String(d.getDate()).padStart(2, '0');
    const hh = String(d.getHours()).padStart(2, '0');
    const min = String(d.getMinutes()).padStart(2, '0');
    const ss = String(d.getSeconds()).padStart(2, '0');
    return `${yyyy}-${mm}-${dd} ${hh}:${min}:${ss}`;
  } catch {
    return iso;
  }
}
