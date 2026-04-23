// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiError } from '../api/client';

interface EnvProject {
  name: string;
  release_id: string;
  url: string;
}

interface EnvDetail {
  name: string;
  status: string;
  projects: EnvProject[];
}

export interface EnvInspectDeps {
  getToken: () => string | null;
  getEnvironment: (name: string) => Promise<EnvDetail>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface EnvInspectOptions {
  json?: boolean;
}

/**
 * EnvInspectCommand shows detailed information about a single environment.
 *
 * Does NOT require loopzero.yaml — the environment name is sufficient.
 *
 * Exit codes:
 *   0 — success
 *   1 — usage error (missing name, not found, API error)
 *   4 — auth error
 */
export class EnvInspectCommand {
  private readonly deps: EnvInspectDeps;

  constructor(deps: EnvInspectDeps) {
    this.deps = deps;
  }

  async execute(name: string, opts: EnvInspectOptions = {}): Promise<number> {
    // 1. Auth check
    if (!this.deps.getToken()) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    // 2. Name required
    if (!name || name.trim() === '') {
      return this.exitWithError(opts, 'Environment name is required. Usage: loopzero env inspect <name>', 1, 'MISSING_ARGUMENT');
    }

    // 3. Call API
    let env: EnvDetail;
    try {
      env = await this.deps.getEnvironment(name);
    } catch (err) {
      return this.handleError(opts, err, name);
    }

    // 4. Display result
    if (opts.json) {
      this.deps.writeTo(JSON.stringify(env));
      return 0;
    }

    this.deps.writeTo(`Environment: ${env.name}`);
    this.deps.writeTo('');
    this.deps.writeTo(`  Status:   ${env.status}`);

    // CHG-2026-04-23-AUDIT-1: the API omits `projects` entirely when no
    // releases exist. Before this null-safety check, `env.projects.length`
    // crashed with a JS error visible to the user.
    const projects = env.projects ?? [];
    if (projects.length > 0) {
      this.deps.writeTo('  Projects:');
      for (const proj of projects) {
        this.deps.writeTo(`    ${pad(proj.name, 14)}${pad(proj.release_id, 15)}${proj.url}`);
      }
    } else {
      this.deps.writeTo('  Projects: none');
    }

    return 0;
  }

  private exitWithError(opts: EnvInspectOptions, message: string, exitCode: number, code?: string): number {
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

  private handleError(opts: EnvInspectOptions, err: unknown, name: string): number {
    if (err instanceof ApiError) {
      if (err.exitCode === 4 || err.statusCode === 401) {
        return this.exitWithError(opts, 'Authentication failed. Run: loopzero login', 4, 'AUTH_REQUIRED');
      }
      if (err.statusCode === 404) {
        return this.exitWithError(opts, `Environment "${name}" not found.`, 1, 'ENV_NOT_FOUND');
      }
      return this.exitWithError(opts, `Failed to get environment: ${err.message}. Try again.`, 1, err.code);
    }
    if (err instanceof Error) {
      return this.exitWithError(opts, `Failed to get environment: ${err.message}. Try again.`, 1, 'UNEXPECTED_ERROR');
    }
    return this.exitWithError(opts, 'Failed to get environment. Try again later.', 1, 'UNEXPECTED_ERROR');
  }
}

function pad(str: string, len: number): string {
  return str.length >= len ? str + ' ' : str + ' '.repeat(len - str.length);
}
