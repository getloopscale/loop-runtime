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

interface StatusResponse {
  last_build?: { id: string };
}

/** SSE log event from /api/v1/builds/{id}/logs/stream */
export interface StreamLogEvent {
  timestamp: string;
  line: string;
  step?: string;
}

/** Callback to connect to the SSE log stream. Returns a disconnect function. */
export type StreamLogsFn = (
  buildID: string,
  onLine: (event: StreamLogEvent) => void,
  onError: (err: Error) => void,
  onClose: () => void,
) => { close: () => void };

export interface LogsDeps {
  getToken: () => string | null;
  findLoopZeroYaml: () => LoopZeroYamlResult | null;
  getLogs: (buildID: string, opts: { tail?: number; step?: string }) => Promise<LogsApiResponse>;
  getProjectStatus?: (project: string, env: string) => Promise<StatusResponse>;
  /** SSE streaming function for --follow mode. Optional: if absent, --follow is unsupported. */
  streamLogs?: StreamLogsFn;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface LogsOptions {
  build?: string;
  tail?: number;
  step?: string;
  env?: string;
  json?: boolean;
  /** Follow mode: stream logs in real-time via SSE. */
  follow?: boolean;
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

    // 2. Resolve build ID — explicit --build or infer from last build
    if (!opts.build) {
      // F-03: Infer from project status when --build not provided
      const yaml = this.deps.findLoopZeroYaml();
      if (!yaml?.content?.project) {
        return this.exitWithError(opts, 'No loopzero.yaml found. Specify --build <id> or run from a project directory.', 1, 'MISSING_CONFIG');
      }
      if (!this.deps.getProjectStatus) {
        return this.exitWithError(opts, 'Build ID is required. Usage: loopzero logs --build <id>', 1, 'MISSING_BUILD_ID');
      }
      try {
        const env = opts.env ?? 'production';
        const status = await this.deps.getProjectStatus(yaml.content.project, env);
        if (!status?.last_build?.id) {
          return this.exitWithError(opts, `No builds found for project "${yaml.content.project}" in ${env}.`, 1, 'NO_BUILDS');
        }
        opts.build = status.last_build.id;
      } catch {
        return this.exitWithError(opts, 'Could not resolve last build. Specify --build <id> explicitly.', 1, 'BUILD_RESOLVE_FAILED');
      }
    }

    // 4. Follow mode: stream logs via SSE
    if (opts.follow) {
      return this.executeFollow(opts);
    }

    // 5. Batch mode: call API for all logs at once
    let response: LogsApiResponse;
    try {
      response = await this.deps.getLogs(opts.build!, {
        tail: opts.tail,
        step: opts.step,
      });
    } catch (err) {
      return this.handleError(opts, err);
    }

    // 6. Display result
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

  private executeFollow(opts: LogsOptions): Promise<number> {
    if (!this.deps.streamLogs) {
      return Promise.resolve(
        this.exitWithError(opts, 'Log streaming is not available. Try without --follow.', 1, 'STREAM_UNAVAILABLE'),
      );
    }

    return new Promise<number>((resolve) => {
      const stream = this.deps.streamLogs!(
        opts.build!,
        (event: StreamLogEvent) => {
          if (opts.json) {
            this.deps.writeTo(JSON.stringify(event));
          } else {
            const prefix = event.step ? `[${event.step}]` : '';
            const ts = event.timestamp ? `${event.timestamp} ` : '';
            this.deps.writeTo(`${ts}${prefix} ${event.line}`);
          }
        },
        (err: Error) => {
          this.deps.writeError(`Stream error: ${err.message}`);
        },
        () => {
          // Stream closed (build completed or server closed)
          resolve(0);
        },
      );

      // Handle SIGINT gracefully
      const cleanup = () => {
        stream.close();
        resolve(0);
      };
      process.once('SIGINT', cleanup);
      process.once('SIGTERM', cleanup);
    });
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
