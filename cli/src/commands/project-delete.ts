// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

export interface ProjectDeleteDeps {
  getToken: () => string | null;
  deleteProject: (name: string, env?: string) => Promise<ProjectDeleteResponse>;
  confirm: (message: string) => Promise<boolean>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
  isCI: boolean;
}

export interface ProjectDeleteOptions {
  env?: string;
  yes?: boolean;
  json?: boolean;
}

export interface ProjectDeleteResponse {
  project: string;
  deleted_environments: string[];
  cancelled_builds: number;
}

export class ProjectDeleteCommand {
  private readonly deps: ProjectDeleteDeps;

  constructor(deps: ProjectDeleteDeps) {
    this.deps = deps;
  }

  async execute(name: string, opts: ProjectDeleteOptions = {}): Promise<number> {
    // Auth check
    const token = this.deps.getToken();
    if (!token) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    // Name validation
    if (!name || name.trim() === '') {
      return this.exitWithError(opts, 'Project name is required. Usage: loopzero project delete <name>', 1, 'MISSING_ARGUMENT');
    }

    // Confirmation required (unless --yes)
    if (!opts.yes) {
      if (this.deps.isCI) {
        return this.exitWithError(opts, 'Confirmation required. Use --yes in CI mode.', 1, 'CONFIRMATION_REQUIRED');
      }

      const envSuffix = opts.env ? ` from environment "${opts.env}"` : ' from all environments';
      const confirmed = await this.deps.confirm(
        `Delete project "${name}"${envSuffix}? This will cancel active builds and remove deployed resources. [y/N]`
      );
      if (!confirmed) {
        if (opts.json) {
          this.deps.writeTo(JSON.stringify({ status: 'cancelled', project: name }));
        } else {
          this.deps.writeTo('Delete cancelled.');
        }
        return 0;
      }
    }

    // Call API
    try {
      const result = await this.deps.deleteProject(name, opts.env);

      if (opts.json) {
        this.deps.writeTo(JSON.stringify({
          ...result,
          status: 'deleted',
        }));
      } else {
        const envInfo = result.deleted_environments.length > 0
          ? result.deleted_environments.join(', ')
          : 'none';
        this.deps.writeTo(`Deleted project "${result.project}" from environments: ${envInfo}`);
        if (result.cancelled_builds > 0) {
          this.deps.writeTo(`Cancelled ${result.cancelled_builds} active build(s).`);
        }
      }

      return 0;
    } catch (err: unknown) {
      const apiErr = err as { message?: string; exitCode?: number; code?: string };
      const message = apiErr.message || 'Delete failed. Check project name and try again.';
      const exitCode = apiErr.exitCode || 3;
      const code = apiErr.code || 'DELETE_FAILED';
      return this.exitWithError(opts, message, exitCode, code);
    }
  }

  private exitWithError(opts: ProjectDeleteOptions, message: string, exitCode: number, code?: string): number {
    if (opts.json) {
      this.deps.writeTo(JSON.stringify({ error: message, exit_code: exitCode, code }));
    } else {
      this.deps.writeError(message);
    }
    return exitCode;
  }
}
