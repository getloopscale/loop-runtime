import { ApiError } from '../api/client';

interface BuildDetailApp {
  name: string;
  framework?: string;
  type?: string;
}

interface BuildDetail {
  build_id: string;
  project: string;
  environment: string;
  status: string;
  commit_sha: string;
  created_at: string;
  finished_at?: string;
  duration_seconds?: number;
  apps?: BuildDetailApp[];
  error?: string;
}

export interface BuildInspectDeps {
  getToken: () => string | null;
  getBuild: (buildId: string) => Promise<BuildDetail>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface BuildInspectOptions {
  json?: boolean;
}

/**
 * BuildInspectCommand shows detailed information about a single build.
 *
 * Does NOT require loopzero.yaml — the build ID is sufficient.
 *
 * Exit codes:
 *   0 — success
 *   1 — usage error (missing build-id, not found, API error)
 *   4 — auth error
 */
export class BuildInspectCommand {
  private readonly deps: BuildInspectDeps;

  constructor(deps: BuildInspectDeps) {
    this.deps = deps;
  }

  async execute(buildId: string, opts: BuildInspectOptions = {}): Promise<number> {
    // 1. Auth check
    if (!this.deps.getToken()) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    // 2. Build ID required
    if (!buildId || buildId.trim() === '') {
      return this.exitWithError(opts, 'Build ID is required. Usage: loopzero build inspect <build-id>', 1, 'MISSING_ARGUMENT');
    }

    // 3. Call API
    let build: BuildDetail;
    try {
      build = await this.deps.getBuild(buildId);
    } catch (err) {
      return this.handleError(opts, err, buildId);
    }

    // 4. Display result
    if (opts.json) {
      this.deps.writeTo(JSON.stringify(build));
      return 0;
    }

    this.deps.writeTo(`Build ${build.build_id}`);
    this.deps.writeTo('');
    this.deps.writeTo(`  Project:      ${build.project}`);
    this.deps.writeTo(`  Environment:  ${build.environment}`);
    this.deps.writeTo(`  Status:       ${build.status}`);
    this.deps.writeTo(`  Version:      ${build.commit_sha.substring(0, 15)}`);
    this.deps.writeTo(`  Started:      ${formatTimestamp(build.created_at)}`);
    if (build.finished_at) {
      this.deps.writeTo(`  Finished:     ${formatTimestamp(build.finished_at)}`);
    }
    if (build.duration_seconds != null) {
      this.deps.writeTo(`  Duration:     ${build.duration_seconds}s`);
    }

    if (build.apps && build.apps.length > 0) {
      this.deps.writeTo('');
      this.deps.writeTo('  Apps:');
      for (const app of build.apps) {
        const parts = [app.name];
        if (app.framework) parts.push(`(${app.framework})`);
        if (app.type) parts.push(`[${app.type}]`);
        this.deps.writeTo(`    - ${parts.join(' ')}`);
      }
    }

    if (build.error) {
      this.deps.writeTo('');
      this.deps.writeTo(`  Error: ${build.error}`);
    }

    this.deps.writeTo('');
    this.deps.writeTo(`View logs: loopzero logs --build ${build.build_id}`);

    return 0;
  }

  private exitWithError(opts: BuildInspectOptions, message: string, exitCode: number, code?: string): number {
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

  private handleError(opts: BuildInspectOptions, err: unknown, buildId: string): number {
    if (err instanceof ApiError) {
      if (err.exitCode === 4 || err.statusCode === 401) {
        return this.exitWithError(opts, 'Authentication failed. Run: loopzero login', 4, 'AUTH_REQUIRED');
      }
      if (err.statusCode === 404) {
        return this.exitWithError(opts, `Build ${buildId} not found.`, 1, 'BUILD_NOT_FOUND');
      }
      return this.exitWithError(opts, `Failed to get build: ${err.message}. Try again.`, 1, err.code);
    }
    if (err instanceof Error) {
      return this.exitWithError(opts, `Failed to get build: ${err.message}. Try again.`, 1, 'UNEXPECTED_ERROR');
    }
    return this.exitWithError(opts, 'Failed to get build. Try again later.', 1, 'UNEXPECTED_ERROR');
  }
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
