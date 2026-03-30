// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiError } from '../api/client';
import type { LoopZeroYamlResult } from '../adapters/loopzero-yaml';

interface LogEntry {
  step: string;
  stream: string;
  content: string;
  timestamp: string;
}

interface LogsApiResponse {
  build_id: string;
  status: string;
  entries: LogEntry[];
  total_lines: number;
}

export interface LogsDeps {
  getToken: () => string | null;
  findLoopZeroYaml: () => LoopZeroYamlResult | null;
  getLogs: (buildID: string, opts: { tail?: number; step?: string }) => Promise<LogsApiResponse>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface LogsOptions {
  build?: string;
  tail?: number;
  step?: string;
  json?: boolean;
}

export class LogsCommand {
  private readonly deps: LogsDeps;

  constructor(deps: LogsDeps) {
    this.deps = deps;
  }

  async execute(opts: LogsOptions = {}): Promise<number> {
    // 1. Auth check
    if (!this.deps.getToken()) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    // 2. Validate build ID — when --build is provided, loopzero.yaml is not required
    // (the build ID is globally unique and sufficient for the API query)
    if (!opts.build) {
      return this.exitWithError(
        opts,
        'Build ID is required. Find it in the output of \'loopzero deploy\' or \'loopzero deploy --no-wait\'.\nUsage: loopzero logs --build <id>',
        1,
        'MISSING_BUILD_ID',
      );
    }

    // 4. Call API
    let response: LogsApiResponse;
    try {
      response = await this.deps.getLogs(opts.build, {
        tail: opts.tail,
        step: opts.step,
      });
    } catch (err) {
      return this.handleError(opts, err);
    }

    // 5. Display result
    if (opts.json) {
      this.deps.writeTo(JSON.stringify(response));
      return 0;
    }

    this.deps.writeTo(`Build: ${response.build_id} (${response.status})`);
    this.deps.writeTo('');

    if (response.entries.length === 0) {
      this.deps.writeTo('No logs available. Build may still be starting.');
      return 0;
    }

    for (const entry of response.entries) {
      const prefix = entry.step ? `[${entry.step}]` : '';
      this.deps.writeTo(`${prefix} ${entry.content}`);
    }

    return 0;
  }

  private exitWithError(opts: LogsOptions, message: string, exitCode: number, code?: string): number {
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

  private handleError(opts: LogsOptions, err: unknown): number {
    if (err instanceof ApiError) {
      if (err.exitCode === 4) {
        return this.exitWithError(opts, 'Authentication failed. Run: loopzero login', 4, 'AUTH_REQUIRED');
      }
      return this.exitWithError(opts, err.message, err.exitCode, err.code);
    }
    if (err instanceof Error) {
      return this.exitWithError(opts, `Unexpected error: ${err.message}`, 3, 'UNEXPECTED_ERROR');
    }
    return this.exitWithError(opts, 'Unexpected error. Try again later.', 3, 'UNEXPECTED_ERROR');
  }
}
