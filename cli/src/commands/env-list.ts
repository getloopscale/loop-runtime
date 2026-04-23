// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiError } from '../api/client';

import { formatErrorMessage } from '../output/error-message';
interface EnvItem {
  name: string;
  status: string;
}

interface EnvListResponse {
  environments: EnvItem[];
}

export interface EnvListDeps {
  getToken: () => string | null;
  listEnvironments: () => Promise<EnvListResponse>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface EnvListOptions {
  json?: boolean;
}

/**
 * EnvListCommand lists all environments for the current team.
 *
 * Does NOT require loopzero.yaml.
 *
 * Exit codes:
 *   0 — success (including empty list)
 *   1 — usage error (API error)
 *   4 — auth error
 */
export class EnvListCommand {
  private readonly deps: EnvListDeps;

  constructor(deps: EnvListDeps) {
    this.deps = deps;
  }

  async execute(opts: EnvListOptions = {}): Promise<number> {
    // 1. Auth check
    if (!this.deps.getToken()) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    // 2. Call API
    let response: EnvListResponse;
    try {
      response = await this.deps.listEnvironments();
    } catch (err) {
      return this.handleError(opts, err);
    }

    // 3. Display result
    if (opts.json) {
      this.deps.writeTo(JSON.stringify({ environments: response.environments, total: response.environments.length }));
      return 0;
    }

    if (response.environments.length === 0) {
      this.deps.writeTo('No environments found.');
      return 0;
    }

    this.deps.writeTo('Environments:');
    this.deps.writeTo('');
    this.deps.writeTo(`  ${pad('NAME', 14)}STATUS`);
    for (const env of response.environments) {
      this.deps.writeTo(`  ${pad(env.name, 14)}${env.status}`);
    }
    this.deps.writeTo('');
    this.deps.writeTo(`${response.environments.length} environment${response.environments.length !== 1 ? 's' : ''}`);

    return 0;
  }

  private exitWithError(opts: EnvListOptions, message: string, exitCode: number, code?: string): number {
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

  private handleError(opts: EnvListOptions, err: unknown): number {
    if (err instanceof ApiError) {
      if (err.exitCode === 4 || err.statusCode === 401) {
        return this.exitWithError(opts, 'Authentication failed. Run: loopzero login', 4, 'AUTH_REQUIRED');
      }
      return this.exitWithError(opts, formatErrorMessage('Failed to list environments', err.message, 'Try again'), 1, err.code);
    }
    if (err instanceof Error) {
      return this.exitWithError(opts, formatErrorMessage('Failed to list environments', err.message, 'Try again'), 1, 'UNEXPECTED_ERROR');
    }
    return this.exitWithError(opts, 'Failed to list environments. Try again later.', 1, 'UNEXPECTED_ERROR');
  }
}

function pad(str: string, len: number): string {
  return str.length >= len ? str + ' ' : str + ' '.repeat(len - str.length);
}
