// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { createHash } from 'crypto';
import { ApiClient } from '../api/client';
import { SSEClient } from '../api/sse-client';
import { DeployCommand } from '../commands/deploy';
import { ValidateCommand } from '../commands/validate';
import { StatusCommand } from '../commands/status';
import { LogsCommand } from '../commands/logs';
import { RollbackCommand } from '../commands/rollback';
import { PromoteCommand } from '../commands/promote';
import { CancelCommand } from '../commands/cancel';
import { API_REQUEST_TIMEOUT_MS, DEPLOY_TIMEOUT_MS } from '../config/constants';
import { hasGitRepo, hasUncommittedChanges, hasLockfile, getSourceIdentifier, getGitRoot, createSourceBundle, checkSensitiveFiles } from '../adapters/git';
import { findLoopZeroYaml } from '../adapters/loopzero-yaml';
import { cleanupFile } from '../adapters/source-bundle';
import { uploadSource } from '../adapters/source-upload';
import { CommandDeps } from './types';

export function registerDeployCommands(deps: CommandDeps): void {
  const { program, configStore, isCI, writeTo, writeError, getGlobalOpts, apiUrl, handleUnauthorized, withTimeout } = deps;

  // --- deploy ---
  program
    .command('deploy')
    .description('Deploy your application. Use --json for structured output.')
    .option('--env <name>', 'Target environment', 'production')
    .option('--app <name...>', 'Deploy specific app(s)')
    .option('--wait', 'Wait for deploy to complete', true)
    .option('--no-wait', 'Return immediately after submit')
    .option('--no-cache', 'Build without layer cache (full rebuild)')
    .option('--json', 'Output in JSON format')
    .action(async (cmdOpts) => {
      const globalOpts = getGlobalOpts();
      const jsonMode = Boolean(cmdOpts.json || globalOpts.json);
      let deployOutputCount = 0;
      const deployWriteTo = (msg: string): void => {
        deployOutputCount += 1;
        writeTo(msg);
      };
      const deployWriteError = (msg: string): void => {
        deployOutputCount += 1;
        writeError(msg);
      };
      const token = configStore.getToken();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: token ?? undefined, onUnauthorized: handleUnauthorized });

      const deployCmd = new DeployCommand({
        getToken: () => configStore.getToken(),
        findLoopZeroYaml: () => {
          const gitRoot = getGitRoot() ?? process.cwd();
          return findLoopZeroYaml(process.cwd(), gitRoot);
        },
        hasGitRepo,
        hasUncommittedChanges,
        hasLockfile,
        getSourceIdentifier,
        createSourceBundle,
        checkSensitiveFiles,
        uploadSource: (bundlePath: string, loopzeroYamlPath: string) =>
          // configStore.getToken() honors the LOOPZERO_TOKEN env var; the older
          // getConfig()?.token path silently ignored env-only auth (CI mode),
          // which broke `loopzero deploy` whenever no config file was written.
          uploadSource(apiUrl, configStore.getToken(), bundlePath, loopzeroYamlPath),
        submitBuild: async (input) => {
          const headers: Record<string, string> = {
            'Idempotency-Key': input.idempotencyKey,
          };
          const result = await apiClient.post<{ build_id: string; stream_url: string }>(
            '/api/v1/builds',
            {
              project_id: input.project,
              environment: input.environment,
              commit_sha: input.commitSha,
              ...(input.noCache ? { no_cache: true } : {}),
              apps: input.apps.map((a) => ({
                name: a.name,
                ...(a.type ? { type: a.type } : {}),
                ...(a.port ? { port: a.port } : {}),
                ...(a.path ? { path: a.path } : {}),
                ...(a.framework ? { framework: a.framework } : {}),
                ...(a.build ? { build: a.build } : {}),
                ...(a.rendering ? { rendering: a.rendering } : {}),
                ...(a.secrets && a.secrets.length > 0 ? { secrets: a.secrets } : {}),
                // B15 (CHG-2026-04-23-B15 / MEET-2026-04-23-059): forward scaling
                // config so the API can enforce plan caps and render Deployment/HPA.
                ...(a.replicas != null ? { replicas: a.replicas } : {}),
                ...(a.autoscaling ? { autoscaling: a.autoscaling } : {}),
              })),
            },
            headers,
          );
          // F-08: Ensure stream_url is absolute (API returns relative path)
          if (result.stream_url && result.stream_url.startsWith('/')) {
            result.stream_url = `${apiUrl}${result.stream_url}`;
          }
          return result;
        },
        connectSSE: (buildId, onEvent) => {
          const url = `${apiUrl}/api/v1/builds/${buildId}/stream`;
          const client = new SSEClient({
            url,
            pollUrl: `${apiUrl}/api/v1/builds/${buildId}`,
            headers: token ? { Authorization: `Bearer ${token}` } : {},
            onEvent: (sseEvent) => {
              onEvent({ type: sseEvent.event, data: sseEvent.data });
            },
            onError: (err) => {
              onEvent({
                type: 'stream.error',
                data: { error: err.message },
              });
            },
            onClose: () => {
              onEvent({
                type: 'stream.closed',
                data: {},
              });
            },
          });
          client.connect();
          return { close: () => client.close() };
        },
        getSecrets: async (project: string, environment: string) => {
          const result = await apiClient.get<{ secrets: Array<{ name: string }> }>(
            `/api/v1/secrets?env=${encodeURIComponent(environment)}&project=${encodeURIComponent(project)}`
          );
          return result.secrets.map((s) => s.name);
        },
        cleanupFile,
        getBuildStatus: async (buildId: string) => {
          const result = await apiClient.get<{ status: string; error_message?: string }>(
            `/api/v1/builds/${encodeURIComponent(buildId)}`
          );
          return result;
        },
        // Beta Logs Gate Criterion 4 (UX) — best-effort auto-tail.
        // Wired against the same /logs endpoint the CLI `loopzero logs`
        // command uses. Always tail=20; swallows errors so a Loki
        // outage doesn't make the deploy hang on cleanup.
        fetchRecentBuildLogs: async (buildId: string) => {
          try {
            const result = await apiClient.get<{
              build_id: string;
              status: string;
              entries: Array<{ step: string; stream: string; content: string; timestamp: string }>;
              total_lines: number;
            }>(`/api/v1/builds/${encodeURIComponent(buildId)}/logs?tail=20`);
            return result;
          } catch {
            return null;
          }
        },
        generateIdempotencyKey: (commitSha: string, project: string, environment: string, apps: string[]) => {
          const window = Math.floor(Date.now() / 60000);
          const input = [commitSha, project, environment, ...apps.sort(), String(window)].join(':');
          return createHash('sha256').update(input).digest('hex').substring(0, 32);
        },
        writeTo: deployWriteTo,
        writeError: deployWriteError,
        isCI,
        isTTY: process.stdout.isTTY ?? false,
      });

      const exitCode = await withTimeout(
        deployCmd.execute({
          env: cmdOpts.env,
          app: cmdOpts.app,
          wait: cmdOpts.wait,
          json: jsonMode,
          noCache: cmdOpts.noCache ?? false,
        }),
        DEPLOY_TIMEOUT_MS + 30_000,
        'Deploy command timed out waiting for completion.',
      ).catch((err: Error) => {
        if (jsonMode) {
          deployWriteTo(JSON.stringify({
            code: 'DEPLOY_TIMEOUT',
            error: err.message || 'Deploy command timed out waiting for completion.',
            exit_code: 3,
          }));
        } else {
          deployWriteError(err.message || 'Deploy command timed out waiting for completion.');
        }
        return 3;
      });
      if (deployOutputCount === 0) {
        if (jsonMode) {
          writeTo(JSON.stringify({
            code: 'EMPTY_OUTPUT',
            error: 'Deploy finished without output. This is unexpected; check API health and try again.',
            exit_code: 3,
          }));
        } else {
          writeError('Deploy finished without output. This is unexpected; check API health and try again.');
        }
        process.exitCode = 3;
        return;
      }
      process.exitCode = exitCode;
    });

  // --- validate ---
  program
    .command('validate')
    .description('Validate loopzero.yaml configuration')
    .option('--json', 'Output in JSON format')
    .action(async (cmdOpts) => {
      const globalOpts = getGlobalOpts();
      const jsonMode = Boolean(cmdOpts.json || globalOpts.json);

      const validateCmd = new ValidateCommand({
        findLoopZeroYaml: () => {
          const gitRoot = getGitRoot() ?? process.cwd();
          return findLoopZeroYaml(process.cwd(), gitRoot);
        },
        writeTo,
        writeError,
      });

      const exitCode = await validateCmd.execute({ json: jsonMode });
      process.exitCode = exitCode;
    });

  // --- status ---
  program
    .command('status')
    .description('Show environment status for the current project')
    .option('--env <name>', 'Target environment', 'production')
    .option('--project <name>', 'Project name (overrides loopzero.yaml lookup; plan T2.3 D6)')
    .action(async (cmdOpts) => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const statusCmd = new StatusCommand({
        getToken: () => configStore.getToken(),
        findLoopZeroYaml: () => {
          const gitRoot = getGitRoot() ?? process.cwd();
          return findLoopZeroYaml(process.cwd(), gitRoot);
        },
        getStatus: async (project: string, environment: string) => {
          return apiClient.get(
            `/api/v1/projects/${encodeURIComponent(project)}/status?env=${encodeURIComponent(environment)}`,
          );
        },
        writeTo,
        writeError,
      });

      const exitCode = await withTimeout(
        statusCmd.execute({ env: cmdOpts.env, project: cmdOpts.project, json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Status request timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'STATUS_TIMEOUT', error: err.message, exit_code: 3 }));
        } else {
          writeError(err.message);
        }
        return 3;
      });
      process.exitCode = exitCode;
    });

  // --- logs ---
  program
    .command('logs')
    .description('Show build output logs')
    .option('--build <id>', 'Build ID to show logs for (infers from last build if omitted)')
    .option('--tail <n>', 'Show last N lines', parseInt)
    .option('--step <name>', 'Filter by build step')
    .option('--env <name>', 'Target environment', 'production')
    .option('--app <name>', 'Filter runtime logs to a single app (use with --runtime). Defaults to all apps in the project namespace.')
    .option('--runtime', 'Stream runtime logs from the workload namespace instead of build logs')
    .option('--since <duration>', 'Restrict to entries newer than NOW - <duration>. Accepts "24h", "7d", "30m", or raw seconds. Default: 7d.')
    .option('-f, --follow', 'Stream logs in real-time')
    .option('--project <name>', 'Project name (overrides loopzero.yaml lookup; plan T2.3 D6)')
    .action(async (cmdOpts) => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const logsCmd = new LogsCommand({
        getToken: () => configStore.getToken(),
        findLoopZeroYaml: () => {
          const gitRoot = getGitRoot() ?? process.cwd();
          return findLoopZeroYaml(process.cwd(), gitRoot);
        },
        getLogs: async (buildID: string, opts: { tail?: number; step?: string; since?: string }) => {
          const params = new URLSearchParams();
          if (opts.tail) params.set('tail', String(opts.tail));
          if (opts.step) params.set('step', opts.step);
          if (opts.since) params.set('since', opts.since);
          const query = params.toString();
          const path = `/api/v1/builds/${encodeURIComponent(buildID)}/logs${query ? `?${query}` : ''}`;
          return apiClient.get(path);
        },
        // F-03: infer last build ID from project status when --build not provided
        getProjectStatus: async (project: string, env: string) => {
          return apiClient.get<{ last_build?: { id: string } }>(
            `/api/v1/projects/${encodeURIComponent(project)}/status?environment=${encodeURIComponent(env)}`
          );
        },
        // SSE log streaming for --follow mode
        streamLogs: (buildID, onLine, onError, onClose) => {
          const token = configStore.getToken();
          const streamUrl = `${apiUrl}/api/v1/builds/${encodeURIComponent(buildID)}/logs/stream`;
          const client = new SSEClient({
            url: streamUrl,
            headers: token ? { Authorization: `Bearer ${token}` } : {},
            onEvent: (event) => {
              if (event.event === 'log' && event.data) {
                onLine({
                  timestamp: (event.data.timestamp as string) ?? '',
                  line: (event.data.line as string) ?? '',
                  step: (event.data.step as string) ?? undefined,
                });
              } else if (event.event === 'stream.done' || event.event === 'stream.timeout') {
                client.close();
              }
            },
            onError,
            onClose,
          });
          client.connect().catch(onError);
          return { close: () => client.close() };
        },
        // Beta Logs Gate Criterion 3 — runtime SSE stream with --app filter.
        streamRuntimeLogs: (project, env, app, onLine, onError, onClose) => {
          const token = configStore.getToken();
          const params = new URLSearchParams();
          params.set('env', env);
          if (app) params.set('app', app);
          const streamUrl = `${apiUrl}/api/v1/projects/${encodeURIComponent(project)}/logs/stream?${params.toString()}`;
          const client = new SSEClient({
            url: streamUrl,
            headers: token ? { Authorization: `Bearer ${token}` } : {},
            onEvent: (event) => {
              if (event.event === 'log' && event.data) {
                onLine({
                  timestamp: (event.data.timestamp as string) ?? '',
                  line: (event.data.line as string) ?? '',
                  step: (event.data.step as string) ?? undefined,
                });
              } else if (event.event === 'stream.done' || event.event === 'stream.timeout') {
                client.close();
              }
            },
            onError,
            onClose,
          });
          return { close: () => client.close() };
        },
        writeTo,
        writeError,
      });

      // Follow mode (build OR runtime) has no timeout
      const timeout = (cmdOpts.follow || cmdOpts.runtime) ? 30 * 60 * 1000 : API_REQUEST_TIMEOUT_MS + 10_000;

      const exitCode = await withTimeout(
        logsCmd.execute({
          build: cmdOpts.build,
          tail: cmdOpts.tail,
          step: cmdOpts.step,
          env: cmdOpts.env,
          project: cmdOpts.project,
          json: globalOpts.json,
          follow: cmdOpts.follow,
          runtime: cmdOpts.runtime,
          app: cmdOpts.app,
          since: cmdOpts.since,
        }),
        timeout,
        'Logs request timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'LOGS_TIMEOUT', error: err.message, exit_code: 3 }));
        } else {
          writeError(err.message);
        }
        return 3;
      });
      process.exitCode = exitCode;
    });

  // --- rollback ---
  program
    .command('rollback')
    .description('Rollback to a previous release')
    .option('--env <name>', 'Target environment', 'production')
    .option('--to <release-id>', 'Rollback to a specific release')
    .option('--yes', 'Skip confirmation prompt')
    .action(async (cmdOpts) => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const rollbackCmd = new RollbackCommand({
        getToken: () => configStore.getToken(),
        findLoopZeroYaml: () => {
          const gitRoot = getGitRoot() ?? process.cwd();
          return findLoopZeroYaml(process.cwd(), gitRoot);
        },
        rollback: async (project: string, environment: string, toReleaseId?: string) => {
          const body: Record<string, string> = { environment };
          if (toReleaseId) body.to_release_id = toReleaseId;
          return apiClient.post(
            `/api/v1/projects/${encodeURIComponent(project)}/rollback`,
            body,
          );
        },
        confirm: async (message: string) => {
          const readline = await import('readline');
          const rl = readline.createInterface({ input: process.stdin, output: process.stdout });
          return new Promise<boolean>((resolve) => {
            rl.question(message, (answer) => {
              rl.close();
              resolve(answer.toLowerCase() === 'y' || answer.toLowerCase() === 'yes');
            });
          });
        },
        writeTo,
        writeError,
        isCI,
      });

      const exitCode = await withTimeout(
        rollbackCmd.execute({ env: cmdOpts.env, to: cmdOpts.to, json: globalOpts.json, yes: cmdOpts.yes }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Rollback timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'ROLLBACK_TIMEOUT', error: err.message, exit_code: 3 }));
        } else {
          writeError(err.message);
        }
        return 3;
      });
      process.exitCode = exitCode;
    });

  // --- promote ---
  program
    .command('promote')
    .description('Promote a release from one environment to another')
    .option('--from <env>', 'Source environment', 'staging')
    .option('--to <env>', 'Target environment', 'production')
    .option('--yes', 'Skip confirmation prompt')
    .action(async (cmdOpts) => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const promoteCmd = new PromoteCommand({
        getToken: () => configStore.getToken(),
        findLoopZeroYaml: () => {
          const gitRoot = getGitRoot() ?? process.cwd();
          return findLoopZeroYaml(process.cwd(), gitRoot);
        },
        promote: async (project: string, from: string, to: string) => {
          return apiClient.post(
            `/api/v1/projects/${encodeURIComponent(project)}/promote`,
            { from, to },
          );
        },
        confirm: async (message: string) => {
          const readline = await import('readline');
          const rl = readline.createInterface({ input: process.stdin, output: process.stdout });
          return new Promise<boolean>((resolve) => {
            rl.question(message, (answer) => {
              rl.close();
              resolve(answer.toLowerCase() === 'y' || answer.toLowerCase() === 'yes');
            });
          });
        },
        writeTo,
        writeError,
        isCI,
      });

      const exitCode = await withTimeout(
        promoteCmd.execute({ from: cmdOpts.from, to: cmdOpts.to, json: globalOpts.json, yes: cmdOpts.yes }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Promotion timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'PROMOTE_TIMEOUT', error: err.message, exit_code: 3 }));
        } else {
          writeError(err.message);
        }
        return 3;
      });
      process.exitCode = exitCode;
    });

  // --- cancel ---
  program
    .command('cancel <build-id>')
    .description('Cancel a running or queued build')
    .action(async (buildId: string) => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const cancelCmd = new CancelCommand({
        getToken: () => configStore.getToken(),
        cancelBuild: async (id: string) => {
          return apiClient.post(`/api/v1/builds/${encodeURIComponent(id)}/cancel`, {});
        },
        writeTo,
        writeError,
      });

      const exitCode = await withTimeout(
        cancelCmd.execute(buildId, { json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Cancel timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) { writeTo(JSON.stringify({ code: 'CANCEL_TIMEOUT', error: err.message, exit_code: 3 })); }
        else { writeError(err.message); }
        return 3;
      });
      process.exitCode = exitCode;
    });
}
