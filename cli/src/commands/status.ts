import { ApiError } from '../api/client';

interface LoopZeroYamlApp {
  path: string;
  framework: string;
  type?: string;
  port: number | null;
  secrets?: string[];
}

interface LoopZeroYaml {
  project: string;
  apps: Record<string, LoopZeroYamlApp>;
}

interface LoopZeroYamlResult {
  path: string;
  content: LoopZeroYaml;
  parseError?: string;
}

interface StatusApiResponse {
  environment: string;
  last_build?: {
    build_id: string;
    status: string;
    finished_at: string;
  };
  release?: {
    release_id: string;
    state: string;
    activated_at: string;
  };
  sync_status?: string;
  url?: string;
}

export interface StatusDeps {
  getToken: () => string | null;
  findLoopZeroYaml: () => LoopZeroYamlResult | null;
  getStatus: (project: string, environment: string) => Promise<StatusApiResponse>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface StatusOptions {
  env?: string;
  json?: boolean;
}

export class StatusCommand {
  private readonly deps: StatusDeps;

  constructor(deps: StatusDeps) {
    this.deps = deps;
  }

  async execute(opts: StatusOptions = {}): Promise<number> {
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

    // 3. Call API
    let status: StatusApiResponse;
    try {
      status = await this.deps.getStatus(project, environment);
    } catch (err) {
      return this.handleError(opts, err);
    }

    // 4. Display result
    if (opts.json) {
      this.deps.writeTo(JSON.stringify(status));
      return 0;
    }

    this.deps.writeTo(`Environment: ${status.environment}`);

    if (!status.last_build) {
      this.deps.writeTo('No deploys yet. Run `loopzero deploy` to get started.');
      return 0;
    }

    const buildSummary = `${status.last_build.build_id}`;
    this.deps.writeTo(`Last deploy:  ${buildSummary} — ${status.last_build.status}`);

    if (status.release) {
      this.deps.writeTo(`Release:      ${status.release.release_id} (${status.release.state.toLowerCase()} since ${this.formatTime(status.release.activated_at)})`);
    }

    if (status.sync_status) {
      this.deps.writeTo(`Sync:         ${status.sync_status}`);
    }

    if (status.url) {
      this.deps.writeTo(`URL:          ${status.url}`);
    }

    return 0;
  }

  private formatTime(isoDate: string): string {
    try {
      const date = new Date(isoDate);
      return date.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hour12: false });
    } catch {
      return isoDate;
    }
  }

  private exitWithError(opts: StatusOptions, message: string, exitCode: number, code?: string): number {
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

  private handleError(opts: StatusOptions, err: unknown): number {
    if (err instanceof ApiError) {
      return this.exitWithError(opts, err.message, err.exitCode, err.code);
    }
    if (err instanceof Error) {
      return this.exitWithError(opts, `Unexpected error: ${err.message}`, 3, 'UNEXPECTED_ERROR');
    }
    return this.exitWithError(opts, 'Unexpected error. Try again later.', 3, 'UNEXPECTED_ERROR');
  }
}
