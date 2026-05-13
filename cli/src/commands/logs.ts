// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiError } from '../api/client';
import { classifyNetworkError } from '../output/error-message';
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

/**
 * Callback to connect to the runtime SSE log stream. Returns a disconnect
 * function. Beta Logs Gate Criterion 3 — per-app filter via `app` arg
 * (empty string = all apps in the namespace).
 */
export type StreamRuntimeLogsFn = (
  project: string,
  env: string,
  app: string,
  onLine: (event: StreamLogEvent) => void,
  onError: (err: Error) => void,
  onClose: () => void,
) => { close: () => void };

export interface LogsDeps {
  getToken: () => string | null;
  findLoopZeroYaml: () => LoopZeroYamlResult | null;
  getLogs: (buildID: string, opts: { tail?: number; step?: string; since?: string }) => Promise<LogsApiResponse>;
  getProjectStatus?: (project: string, env: string) => Promise<StatusResponse>;
  /** SSE streaming function for --follow mode. Optional: if absent, --follow is unsupported. */
  streamLogs?: StreamLogsFn;
  /**
   * SSE streaming function for `loopzero logs --runtime` mode.
   * Optional: when absent, --runtime returns an error rather than
   * silently no-op.
   */
  streamRuntimeLogs?: StreamRuntimeLogsFn;
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
  /**
   * Beta Logs Gate Criterion 3 — runtime mode. When true, stream
   * runtime pod logs from the workload namespace instead of build
   * pod logs. Requires --env; supports optional --app filter.
   */
  runtime?: boolean;
  /**
   * Filter runtime logs to a single app within the project. Empty
   * (default) means "all apps in the namespace". Only meaningful
   * when runtime is true.
   */
  app?: string;
  /**
   * Beta Logs Gate Criterion 7 — restrict to entries newer than
   * NOW - <duration>. Accepts "24h", "7d", "30m", or raw seconds.
   * Default (when omitted): server uses 7d window. The API
   * normalizes "d" shorthand and Go duration syntax.
   */
  since?: string;
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

    // Beta Logs Gate Criterion 3 — runtime mode bypasses build-id
    // resolution and routes to a different SSE endpoint that streams
    // pod logs from the workload namespace. Requires --env; --app is
    // optional (empty = all apps in namespace).
    if (opts.runtime) {
      return this.executeRuntime(opts);
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
        since: opts.since,
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
      // CHG-2026-04-23-AUDIT-31: differentiate by status — "Build may
      // still be starting" is a lie when status is already terminal.
      const terminal = new Set(['SUCCEEDED', 'FAILED', 'CANCELLED']);
      if (terminal.has(response.status)) {
        this.deps.writeTo('No logs available. Logs were not captured or have been pruned.');
      } else {
        this.deps.writeTo('No logs available. Build may still be starting.');
      }
      return 0;
    }

    for (const entry of response.entries) {
      const prefix = entry.step ? `[${entry.step}]` : '';
      this.deps.writeTo(`${prefix} ${entry.content}`);
    }

    return 0;
  }

  private executeRuntime(opts: LogsOptions): Promise<number> {
    if (!this.deps.streamRuntimeLogs) {
      return Promise.resolve(
        this.exitWithError(opts, 'Runtime log streaming is not available in this build.', 1, 'STREAM_UNAVAILABLE'),
      );
    }
    const yaml = this.deps.findLoopZeroYaml();
    if (!yaml?.content?.project) {
      return Promise.resolve(
        this.exitWithError(opts, 'No loopzero.yaml found. Run from a project directory.', 1, 'MISSING_CONFIG'),
      );
    }
    const env = opts.env ?? 'production';
    const app = opts.app ?? '';

    return new Promise<number>((resolve) => {
      let streamErrored = false;
      const stream = this.deps.streamRuntimeLogs!(
        yaml.content.project!,
        env,
        app,
        (event: StreamLogEvent) => {
          if (opts.json) {
            this.deps.writeTo(JSON.stringify(event));
          } else {
            const ts = event.timestamp ? `${event.timestamp} ` : '';
            this.deps.writeTo(`${ts}${event.line}`);
          }
        },
        (err: Error) => {
          streamErrored = true;
          this.deps.writeError(`Runtime stream error: ${err.message}`);
        },
        () => {
          resolve(streamErrored ? 3 : 0);
        },
      );

      const cleanup = () => {
        stream.close();
        resolve(0);
      };
      process.once('SIGINT', cleanup);
      process.once('SIGTERM', cleanup);
    });
  }

  private executeFollow(opts: LogsOptions): Promise<number> {
    if (!this.deps.streamLogs) {
      return Promise.resolve(
        this.exitWithError(opts, 'Log streaming is not available. Try without --follow.', 1, 'STREAM_UNAVAILABLE'),
      );
    }

    return new Promise<number>((resolve) => {
      // CHG-2026-04-23-AUDIT-37: track if the stream errored so close
      // can resolve with exit 3 instead of 0. Previously an SSE error
      // followed by close always reported success, silently masking
      // failures from scripts that check exit code.
      let streamErrored = false;
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
          streamErrored = true;
          this.deps.writeError(`Stream error: ${err.message}`);
        },
        () => {
          // Stream closed (build completed or server closed).
          // Exit 3 if we saw an error during the stream; 0 otherwise.
          resolve(streamErrored ? 3 : 0);
        },
      );

      // Handle SIGINT gracefully — user cancellation is exit 0 regardless.
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
