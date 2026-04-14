// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Suppress Node.js ExperimentalWarning (buffer.File in Node 20 multipart uploads).
// In Node 22+ this warning is gone. Filter instead of monkey-patching process.emit.
process.removeAllListeners('warning');
process.on('warning', (warning) => {
  if (warning.name !== 'ExperimentalWarning') {
    console.warn(warning);
  }
});

import { Command } from 'commander';
import { createHash } from 'crypto';
import * as childProcess from 'child_process';

import { ConfigStore } from './config/store';
import { getDefaultApiUrl, getCloudUrl, API_REQUEST_TIMEOUT_MS, DEPLOY_TIMEOUT_MS } from './config/constants';
import { ApiClient } from './api/client';
import { SSEClient } from './api/sse-client';
import { Formatter } from './output/formatter';
import { LoginCommand } from './commands/login';
import { LogoutCommand } from './commands/logout';
import { WhoamiCommand } from './commands/whoami';
import { InitCommand } from './commands/init';
import { DeployCommand } from './commands/deploy';
import { ProjectDeleteCommand } from './commands/project-delete';
import { ProjectListCommand } from './commands/project-list';
import { ProjectInspectCommand } from './commands/project-inspect';
import { ProjectCreateCommand } from './commands/project-create';
import { EnvListCommand } from './commands/env-list';
import { EnvCreateCommand } from './commands/env-create';
import { EnvDeleteCommand } from './commands/env-delete';
import { EnvInspectCommand } from './commands/env-inspect';
import { StatusCommand } from './commands/status';
import { LogsCommand } from './commands/logs';
import { RollbackCommand } from './commands/rollback';
import { CancelCommand } from './commands/cancel';
import { BuildListCommand } from './commands/build-list';
import { BuildInspectCommand } from './commands/build-inspect';
import { SecretSetCommand, SecretListCommand, SecretDeleteCommand } from './commands/secret';
import { ValidateCommand } from './commands/validate';
import { BillingCommand } from './commands/billing';
import { TeamListCommand } from './commands/team-list';
import { TeamAddCommand } from './commands/team-add';
import { TeamRemoveCommand } from './commands/team-remove';
import { TeamRoleCommand } from './commands/team-role';
import { DomainAddCommand, DomainVerifyCommand, DomainStatusCommand, DomainListCommand, DomainRemoveCommand, DomainDNSCommand } from './commands/domain';
import { ConnectCommand, DisconnectCommand } from './commands/connect';
import { detectMonorepoTool } from './detection/monorepo';
import { detectFramework } from './detection/framework';

import { hasGitRepo, hasUncommittedChanges, hasLockfile, getSourceIdentifier, getGitRoot, createSourceBundle, checkSensitiveFiles } from './adapters/git';
import { executeDeviceCodeFlow } from './auth/device-flow';
import { openBrowser } from './auth/open-browser';
import { ensureFreshToken } from './auth/token-refresh';
import { findLoopZeroYaml } from './adapters/loopzero-yaml';
import { cleanupFile } from './adapters/source-bundle';
import { uploadSource } from './adapters/source-upload';

// --- Cleanup registry for graceful signal handling (B-09) ---
const cleanupFns: Array<() => void> = [];

export function registerCleanup(fn: () => void): void {
  cleanupFns.push(fn);
}

export function deregisterCleanup(fn: () => void): void {
  const idx = cleanupFns.indexOf(fn);
  if (idx !== -1) cleanupFns.splice(idx, 1);
}

function runCleanup(): void {
  for (const fn of cleanupFns) {
    try { fn(); } catch { /* ignore cleanup errors */ }
  }
  cleanupFns.length = 0;
}

process.on('SIGINT', () => { runCleanup(); process.exit(130); });
process.on('SIGTERM', () => { runCleanup(); process.exit(143); });

async function withTimeout<T>(promise: Promise<T>, ms: number, message: string): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const timer = setTimeout(() => {
      reject(new Error(message));
    }, ms);
    promise
      .then((value) => {
        clearTimeout(timer);
        resolve(value);
      })
      .catch((err) => {
        clearTimeout(timer);
        reject(err);
      });
  });
}

export function createProgram(): Command {
  const program = new Command();

  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { version: cliVersion } = require('../package.json');

  program
    .name('loopzero')
    .version(cliVersion)
    .description('CLI-first Kubernetes PaaS for monorepo deployments')
    .option('--json', 'Output in JSON format');

  // F-05: Configure Commander to output JSON errors when --json is present.
  // Commander calls configureOutput hooks for validation errors (missing args,
  // unknown options) BEFORE the action handler runs, so we must intercept here.
  program.configureOutput({
    writeOut: (str: string) => process.stdout.write(str),
    writeErr: (str: string) => {
      if (process.argv.includes('--json')) {
        const cleaned = str.replace(/^error:\s*/i, '').trim();
        process.stdout.write(JSON.stringify({
          error: cleaned,
          exit_code: 1,
          code: 'COMMAND_VALIDATION_ERROR',
        }) + '\n');
      } else {
        process.stderr.write(str);
      }
    },
    outputError: (str: string, write: (s: string) => void) => write(str),
  });

  const configStore = ConfigStore.default();
  const isCI = Formatter.detectCI();

  const writeTo = (msg: string): void => {
    process.stdout.write(msg + '\n');
  };
  const writeError = (msg: string): void => {
    process.stderr.write(msg + '\n');
  };

  // Token refresh callback — used by ApiClient when a 401 is received.
  // Attempts silent refresh via refresh_token, returns new access token or null.
  const handleUnauthorized = async (): Promise<string | null> => {
    return ensureFreshToken({
      getToken: () => configStore.getToken(),
      getRefreshToken: () => configStore.getRefreshToken(),
      isTokenExpired: () => true, // Already got 401 — treat as expired
      saveToken: (t, e, tm, rt, ei) => configStore.saveToken(t, e, tm, rt, ei),
      getEmail: () => configStore.getEmail(),
      getTeam: () => configStore.getTeam(),
      fetchFn: fetch,
      writeError,
    });
  };

  // --- login ---
  program
    .command('login')
    .description('Authenticate with your account')
    .option('--token <token>', 'Authentication token')
    .option('--team <team>', 'Select team')
    .action(async (cmdOpts) => {
      const globalOpts = program.opts();

      const loginCmd = new LoginCommand({
        saveToken: (token, email, team, refreshToken?, expiresIn?) =>
          configStore.saveToken(token, email, team, refreshToken, expiresIn),
        executeDeviceFlow: () => executeDeviceCodeFlow({
          fetchFn: globalThis.fetch,
          baseURL: getCloudUrl(),
          openBrowser,
          writeTo,
          writeError,
          sleep: (ms: number) => new Promise(r => setTimeout(r, ms)),
        }),
        validateToken: async (token: string) => {
          // Try cloud service /v1/auth/me first (for Hydra JWTs)
          // Falls back to engine API /api/v1/auth/whoami (for GitHub PATs)
          try {
            const cloudResp = await globalThis.fetch(`${getCloudUrl()}/v1/auth/me`, {
              headers: { Authorization: `Bearer ${token}` },
              signal: AbortSignal.timeout(5000),
            });
            if (cloudResp.ok) {
              const me = await cloudResp.json() as { email?: string; name?: string; team?: string; user_id?: string };
              if (me.email) {
                return { valid: true, email: me.email, teams: me.team ? [me.team] : ['default'] };
              }
            }
          } catch { /* cloud service unavailable — try engine API */ }

          try {
            const client = new ApiClient({
              baseURL: getDefaultApiUrl(),
              token,
            });
            const result = await client.get<{
              user_id: string;
              email: string;
              team: string;
              role: string;
            }>('/api/v1/auth/whoami');
            const username = result.user_id.replace(/^github-/, '');
            const displayName = result.email || `${username}@users.noreply.github.com`;
            return {
              valid: true,
              email: displayName,
              teams: [result.team],
            };
          } catch (err) {
            const errMsg = (err as Error).message || '';
            if (errMsg.includes('ECONNREFUSED') || errMsg.includes('ETIMEDOUT') ||
                errMsg.includes('ENOTFOUND') || errMsg.includes('fetch failed')) {
              return { valid: false, email: null, teams: [], serverError: true };
            }
            const apiErr = err as { statusCode?: number };
            if (apiErr.statusCode !== undefined && apiErr.statusCode >= 500) {
              return { valid: false, email: null, teams: [], serverError: true };
            }
            return { valid: false, email: null, teams: [] };
          }
        },
        writeTo,
        writeError,
        isCI,
      });

      const exitCode = await loginCmd.execute({
        token: cmdOpts.token,
        team: cmdOpts.team,
        json: globalOpts.json,
      });
      process.exitCode = exitCode;
    });

  // --- logout ---
  program
    .command('logout')
    .description('Remove stored credentials')
    .option('-y, --yes', 'Skip confirmation prompt')
    .action(async (cmdOpts) => {
      const globalOpts = program.opts();

      const logoutCmd = new LogoutCommand({
        clearToken: () => configStore.clearToken(),
        writeTo,
        confirm: async (message: string) => {
          const readline = await import('readline');
          const rl = readline.createInterface({ input: process.stdin, output: process.stdout });
          return new Promise<boolean>((resolve) => {
            rl.question(message + ' ', (answer) => {
              rl.close();
              resolve(answer.toLowerCase() === 'y' || answer.toLowerCase() === 'yes');
            });
          });
        },
        isCI: process.env.CI === 'true',
      });

      const exitCode = await logoutCmd.execute({
        json: globalOpts.json,
        yes: cmdOpts.yes,
      });
      process.exitCode = exitCode;
    });

  // --- whoami ---
  program
    .command('whoami')
    .description('Show current authenticated user')
    .action(async () => {
      const globalOpts = program.opts();

      const whoamiCmd = new WhoamiCommand({
        getToken: () => configStore.getToken(),
        getEmail: () => configStore.getEmail(),
        getTeam: () => configStore.getTeam(),
        verifyToken: async (token: string) => {
          try {
            const client = new ApiClient({ baseURL: getDefaultApiUrl(), token });
            const result = await client.get<{ email: string; team: string }>('/api/v1/auth/whoami');
            return { email: result.email, team: result.team };
          } catch {
            // API unreachable or token invalid — return null
            return null;
          }
        },
        writeTo,
        writeError,
      });

      const exitCode = await whoamiCmd.execute({
        json: globalOpts.json,
      });
      process.exitCode = exitCode;
    });

  // --- init ---
  program
    .command('init')
    .description('Initialize a project in the current directory')
    .option('--yes', 'Skip confirmation prompts (required in CI)')
    .option('--force', 'Overwrite existing loopzero.yaml')
    .option('--name <name>', 'Project name')
    .action(async (cmdOpts) => {
      const globalOpts = program.opts();
      const token = configStore.getToken();

      const initCmd = new InitCommand({
        getToken: () => configStore.getToken(),
        getTeam: () => configStore.getTeam(),
        hasGitRepo,
        detectMonorepo: (dir: string) => detectMonorepoTool(dir),
        detectFramework: (dir: string) => detectFramework(dir),
        createProject: async (input) => {
          const baseURL = getDefaultApiUrl();
          const authedClient = new ApiClient({ baseURL, token: token ?? undefined, onUnauthorized: handleUnauthorized });
          return authedClient.post<{ id: string; name: string; team: string }>(
            '/api/v1/projects',
            {
              name: input.name,
              apps: input.apps,
            }
          );
        },
        writeTo,
        writeError,
        isCI,
        cwd: process.cwd(),
      });

      const exitCode = await withTimeout(
        initCmd.execute({
          yes: cmdOpts.yes,
          force: cmdOpts.force,
          name: cmdOpts.name,
          json: globalOpts.json,
        }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Init timed out waiting for API response. Run "loopzero init" again after the API is healthy.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'INIT_TIMEOUT', error: err.message, exit_code: 3 }));
        } else {
          writeError(err.message);
        }
        return 3;
      });
      process.exitCode = exitCode;
    });

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
      const globalOpts = program.opts();
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
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined, onUnauthorized: handleUnauthorized });

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
          uploadSource(baseURL, configStore.getConfig()?.token ?? null, bundlePath, loopzeroYamlPath),
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
              })),
            },
            headers,
          );
          // F-08: Ensure stream_url is absolute (API returns relative path)
          if (result.stream_url && result.stream_url.startsWith('/')) {
            result.stream_url = `${baseURL}${result.stream_url}`;
          }
          return result;
        },
        connectSSE: (buildId, onEvent) => {
          const url = `${baseURL}/api/v1/builds/${buildId}/stream`;
          const client = new SSEClient({
            url,
            pollUrl: `${baseURL}/api/v1/builds/${buildId}`,
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
        generateIdempotencyKey: (commitSha: string, project: string, environment: string, apps: string[]) => {
          // Include a 60-second time window to allow retry after failures
          // while still deduplicating accidental double-submits within the window.
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
      const globalOpts = program.opts();
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
    .action(async (cmdOpts) => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined, onUnauthorized: handleUnauthorized });

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
        statusCmd.execute({ env: cmdOpts.env, json: globalOpts.json }),
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
    .action(async (cmdOpts) => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined, onUnauthorized: handleUnauthorized });

      const logsCmd = new LogsCommand({
        getToken: () => configStore.getToken(),
        findLoopZeroYaml: () => {
          const gitRoot = getGitRoot() ?? process.cwd();
          return findLoopZeroYaml(process.cwd(), gitRoot);
        },
        getLogs: async (buildID: string, opts: { tail?: number; step?: string }) => {
          const params = new URLSearchParams();
          if (opts.tail) params.set('tail', String(opts.tail));
          if (opts.step) params.set('step', opts.step);
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
        writeTo,
        writeError,
      });

      const exitCode = await withTimeout(
        logsCmd.execute({ build: cmdOpts.build, tail: cmdOpts.tail, step: cmdOpts.step, json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
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
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined, onUnauthorized: handleUnauthorized });

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

  // --- cancel ---
  program
    .command('cancel <build-id>')
    .description('Cancel a running or queued build')
    .action(async (buildId: string) => {
      const globalOpts = program.opts();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

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

  // --- build ---
  const buildCmd = program
    .command('build')
    .description('Manage builds');

  buildCmd
    .command('list')
    .description('List recent builds')
    .option('--env <name>', 'Filter by environment')
    .option('--limit <n>', 'Max builds to show')
    .action(async (cmdOpts: { env?: string; limit?: string }) => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined, onUnauthorized: handleUnauthorized });

      const buildListCmd = new BuildListCommand({
        getToken: () => configStore.getToken(),
        findLoopZeroYaml: () => {
          const gitRoot = getGitRoot() ?? process.cwd();
          return findLoopZeroYaml(process.cwd(), gitRoot);
        },
        listBuilds: async (project: string, env?: string, limit?: number) => {
          const params = new URLSearchParams();
          if (env) params.set('env', env);
          if (limit) params.set('limit', String(limit));
          const query = params.toString();
          const path = `/api/v1/projects/${encodeURIComponent(project)}/builds${query ? `?${query}` : ''}`;
          return apiClient.get(path);
        },
        writeTo,
        writeError,
      });

      const limit = cmdOpts.limit ? parseInt(cmdOpts.limit, 10) : undefined;
      const exitCode = await withTimeout(
        buildListCmd.execute({ env: cmdOpts.env, limit, json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Build list request timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'BUILD_LIST_TIMEOUT', error: err.message, exit_code: 1 }));
        } else {
          writeError(err.message);
        }
        return 1;
      });
      process.exitCode = exitCode;
    });

  buildCmd
    .command('inspect <build-id>')
    .description('Show build details')
    .action(async (buildId: string) => {
      const globalOpts = program.opts();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const buildInspectCmd = new BuildInspectCommand({
        getToken: () => configStore.getToken(),
        getBuild: async (id: string) => {
          return apiClient.get(`/api/v1/builds/${encodeURIComponent(id)}`);
        },
        writeTo,
        writeError,
      });

      const exitCode = await withTimeout(
        buildInspectCmd.execute(buildId, { json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Build inspect request timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'BUILD_INSPECT_TIMEOUT', error: err.message, exit_code: 1 }));
        } else {
          writeError(err.message);
        }
        return 1;
      });
      process.exitCode = exitCode;
    });

  // --- secret ---
  const secretCmd = program
    .command('secret')
    .description('Manage application secrets');

  secretCmd
    .command('set <nameValue>')
    .description('Set a secret (format: NAME=value)')
    .option('--env <name>', 'Target environment', 'production')
    .option('--app <name>', 'Target app')
    .action(async (nameValue: string, cmdOpts: { env?: string; app?: string }) => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined, onUnauthorized: handleUnauthorized });

      const deps = {
        getToken: () => configStore.getToken(),
        findLoopZeroYaml: () => {
          const gitRoot = getGitRoot() ?? process.cwd();
          return findLoopZeroYaml(process.cwd(), gitRoot);
        },
        setSecret: async (project: string, name: string, value: string, env: string, app: string) => {
          return apiClient.post<{ name: string; environment: string; app: string; created: boolean }>(`/api/v1/secrets?project=${encodeURIComponent(project)}`, {
            name, value, environment: env, app,
          });
        },
        listSecrets: async () => ({ secrets: [] as Array<{ name: string; environment: string; app: string }> }),
        deleteSecret: async () => ({ name: '', deleted: false }),
        writeTo,
        writeError,
      };

      const cmd = new SecretSetCommand(deps);
      process.exitCode = await withTimeout(
        cmd.execute({ nameValue, env: cmdOpts.env, app: cmdOpts.app, json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Secret set timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'SECRET_TIMEOUT', error: err.message, exit_code: 3 }));
        } else {
          writeError(err.message);
        }
        return 3;
      });
    });

  secretCmd
    .command('list')
    .description('List secret names (never values)')
    .option('--env <name>', 'Target environment', 'production')
    .action(async (cmdOpts: { env?: string }) => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined, onUnauthorized: handleUnauthorized });

      const deps = {
        getToken: () => configStore.getToken(),
        findLoopZeroYaml: () => {
          const gitRoot = getGitRoot() ?? process.cwd();
          return findLoopZeroYaml(process.cwd(), gitRoot);
        },
        setSecret: async () => ({ name: '', environment: '', app: '', created: false }),
        listSecrets: async (project: string, env: string) => {
          return apiClient.get<{ secrets: Array<{ name: string; environment: string; app: string }> }>(`/api/v1/secrets?project=${encodeURIComponent(project)}&env=${encodeURIComponent(env)}`);
        },
        deleteSecret: async () => ({ name: '', deleted: false }),
        writeTo,
        writeError,
      };

      const cmd = new SecretListCommand(deps);
      process.exitCode = await withTimeout(
        cmd.execute({ env: cmdOpts.env, json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Secret list timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'SECRET_TIMEOUT', error: err.message, exit_code: 3 }));
        } else {
          writeError(err.message);
        }
        return 3;
      });
    });

  secretCmd
    .command('delete <name>')
    .description('Delete a secret')
    .option('--env <name>', 'Target environment', 'production')
    .option('--app <name>', 'Target app')
    .option('--yes', 'Skip confirmation')
    .action(async (name: string, cmdOpts: { env?: string; app?: string; yes?: boolean }) => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined, onUnauthorized: handleUnauthorized });

      const yamlResult = (() => {
        const gitRoot = getGitRoot() ?? process.cwd();
        return findLoopZeroYaml(process.cwd(), gitRoot);
      })();

      const project = yamlResult?.content?.project ?? '';

      const deps = {
        getToken: () => configStore.getToken(),
        findLoopZeroYaml: () => yamlResult,
        setSecret: async () => ({ name: '', environment: '', app: '', created: false }),
        listSecrets: async () => ({ secrets: [] as Array<{ name: string; environment: string; app: string }> }),
        deleteSecret: async (_proj: string, secretName: string, env: string, app: string) => {
          return apiClient.del<{ name: string; deleted: boolean }>(
            `/api/v1/secrets/${encodeURIComponent(secretName)}?project=${encodeURIComponent(project)}&environment=${encodeURIComponent(env)}&app=${encodeURIComponent(app)}`,
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
      };

      const cmd = new SecretDeleteCommand(deps);
      process.exitCode = await withTimeout(
        cmd.execute({ name, env: cmdOpts.env, app: cmdOpts.app, yes: cmdOpts.yes, json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Secret delete timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'SECRET_TIMEOUT', error: err.message, exit_code: 3 }));
        } else {
          writeError(err.message);
        }
        return 3;
      });
    });

  // --- project ---
  const projectCmd = program
    .command('project')
    .description('Manage projects');

  projectCmd
    .command('delete <name>')
    .description('Delete a project and its deployed resources')
    .option('--env <name>', 'Delete from specific environment only')
    .option('--yes', 'Skip confirmation prompt (required in CI)')
    .action(async (name: string, cmdOpts: { env?: string; yes?: boolean }) => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined, onUnauthorized: handleUnauthorized });

      const deleteCmd = new ProjectDeleteCommand({
        getToken: () => configStore.getToken(),
        deleteProject: async (projectName: string, env?: string) => {
          const path = env
            ? `/api/v1/projects/${encodeURIComponent(projectName)}/environments/${encodeURIComponent(env)}`
            : `/api/v1/projects/${encodeURIComponent(projectName)}`;
          return apiClient.del<{
            project: string;
            deleted_environments: string[];
            cancelled_builds: number;
          }>(path);
        },
        confirm: async (message: string) => {
          const readline = await import('readline');
          const rl = readline.createInterface({
            input: process.stdin,
            output: process.stdout,
          });
          return new Promise<boolean>((resolve) => {
            rl.question(message + ' ', (answer) => {
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
        deleteCmd.execute(name, { env: cmdOpts.env, yes: cmdOpts.yes, json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Delete timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'DELETE_TIMEOUT', error: err.message, exit_code: 3 }));
        } else {
          writeError(err.message);
        }
        return 3;
      });
      process.exitCode = exitCode;
    });

  projectCmd
    .command('list')
    .description('List all projects for the current team')
    .action(async () => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined, onUnauthorized: handleUnauthorized });

      const cmd = new ProjectListCommand({
        getToken: () => configStore.getToken(),
        listProjects: async () => {
          return apiClient.get<{ projects: Array<{ id: string; name: string; team_id: string; created_at: string }> }>('/api/v1/projects');
        },
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.execute({ json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Project list timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'PROJECT_LIST_TIMEOUT', error: err.message, exit_code: 1 }));
        } else {
          writeError(err.message);
        }
        return 1;
      });
    });

  projectCmd
    .command('inspect <name>')
    .description('Show detailed information about a project')
    .action(async (name: string) => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined, onUnauthorized: handleUnauthorized });

      const cmd = new ProjectInspectCommand({
        getToken: () => configStore.getToken(),
        getProject: async (projectName: string) => {
          return apiClient.get<{ id: string; name: string; team_id: string; created_at: string; created_by?: string }>(
            `/api/v1/projects/${encodeURIComponent(projectName)}`
          );
        },
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.execute(name, { json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Project inspect timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'PROJECT_INSPECT_TIMEOUT', error: err.message, exit_code: 1 }));
        } else {
          writeError(err.message);
        }
        return 1;
      });
    });

  projectCmd
    .command('create <name>')
    .description('Create a new project')
    .action(async (name: string) => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined, onUnauthorized: handleUnauthorized });

      const cmd = new ProjectCreateCommand({
        getToken: () => configStore.getToken(),
        createProject: async (projectName: string) => {
          return apiClient.post<{ id: string; name: string; team_id: string; created_at: string }>(
            '/api/v1/projects',
            { name: projectName }
          );
        },
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.execute(name, { json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Project create timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'PROJECT_CREATE_TIMEOUT', error: err.message, exit_code: 1 }));
        } else {
          writeError(err.message);
        }
        return 1;
      });
    });

  // --- team ---
  const teamCmd = program
    .command('team')
    .description('Manage team members');

  teamCmd
    .command('list')
    .description('List all team members')
    .action(async () => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined, onUnauthorized: handleUnauthorized });

      const cmd = new TeamListCommand({
        getToken: () => configStore.getToken(),
        listMembers: async () => {
          return apiClient.get<{ members: Array<{ username: string; email: string; role: string; joined_at: string }> }>('/api/v1/teams/members');
        },
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.execute({ json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Team list timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'TEAM_LIST_TIMEOUT', error: err.message, exit_code: 1 }));
        } else {
          writeError(err.message);
        }
        return 1;
      });
    });

  teamCmd
    .command('add <username>')
    .description('Add a member to the team')
    .option('--role <role>', 'Role to assign (member or admin)', 'member')
    .action(async (username: string, cmdOpts: { role?: string }) => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined, onUnauthorized: handleUnauthorized });

      const cmd = new TeamAddCommand({
        getToken: () => configStore.getToken(),
        addMember: async (user: string, role: string) => {
          return apiClient.post<{ username: string; role: string }>('/api/v1/teams/members', { username: user, role });
        },
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.execute(username, { role: cmdOpts.role, json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Team add timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'TEAM_ADD_TIMEOUT', error: err.message, exit_code: 3 }));
        } else {
          writeError(err.message);
        }
        return 3;
      });
    });

  teamCmd
    .command('remove <username>')
    .description('Remove a member from the team')
    .action(async (username: string) => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined, onUnauthorized: handleUnauthorized });

      const cmd = new TeamRemoveCommand({
        getToken: () => configStore.getToken(),
        removeMember: async (user: string) => {
          await apiClient.del<void>(`/api/v1/teams/members/${encodeURIComponent(user)}`);
        },
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.execute(username, { json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Team remove timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'TEAM_REMOVE_TIMEOUT', error: err.message, exit_code: 3 }));
        } else {
          writeError(err.message);
        }
        return 3;
      });
    });

  teamCmd
    .command('role <username> <role>')
    .description('Change a team member\'s role')
    .action(async (username: string, role: string) => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined, onUnauthorized: handleUnauthorized });

      const cmd = new TeamRoleCommand({
        getToken: () => configStore.getToken(),
        changeRole: async (user: string, newRole: string) => {
          return apiClient.patch<{ username: string; role: string }>(
            `/api/v1/teams/members/${encodeURIComponent(user)}`,
            { role: newRole },
          );
        },
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.execute(username, role, { json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Team role change timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'TEAM_ROLE_TIMEOUT', error: err.message, exit_code: 3 }));
        } else {
          writeError(err.message);
        }
        return 3;
      });
    });

  // --- env ---
  const envCmd = program
    .command('env')
    .description('Manage environments');

  envCmd
    .command('list')
    .description('List all environments for the current team')
    .action(async () => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined, onUnauthorized: handleUnauthorized });

      const cmd = new EnvListCommand({
        getToken: () => configStore.getToken(),
        listEnvironments: async () => {
          return apiClient.get<{ environments: Array<{ name: string; status: string }> }>('/api/v1/environments');
        },
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.execute({ json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Environment list timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'ENV_LIST_TIMEOUT', error: err.message, exit_code: 1 }));
        } else {
          writeError(err.message);
        }
        return 1;
      });
    });

  envCmd
    .command('create <name>')
    .description('Create a new environment')
    .action(async (name: string) => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined, onUnauthorized: handleUnauthorized });

      const cmd = new EnvCreateCommand({
        getToken: () => configStore.getToken(),
        createEnvironment: async (envName: string) => {
          return apiClient.post<{ name: string; status: string }>(
            '/api/v1/environments',
            { name: envName }
          );
        },
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.execute(name, { json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Environment create timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'ENV_CREATE_TIMEOUT', error: err.message, exit_code: 1 }));
        } else {
          writeError(err.message);
        }
        return 1;
      });
    });

  envCmd
    .command('delete <name>')
    .description('Delete an environment and its deployed resources')
    .option('--confirm <name>', 'Confirm deletion by repeating the environment name')
    .action(async (name: string, cmdOpts: { confirm?: string }) => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined, onUnauthorized: handleUnauthorized });

      const cmd = new EnvDeleteCommand({
        getToken: () => configStore.getToken(),
        deleteEnvironment: async (envName: string) => {
          await apiClient.del<void>(
            `/api/v1/environments/${encodeURIComponent(envName)}`
          );
        },
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.execute(name, { confirm: cmdOpts.confirm, json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Environment delete timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'ENV_DELETE_TIMEOUT', error: err.message, exit_code: 3 }));
        } else {
          writeError(err.message);
        }
        return 3;
      });
    });

  envCmd
    .command('inspect <name>')
    .description('Show detailed information about an environment')
    .action(async (name: string) => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined, onUnauthorized: handleUnauthorized });

      const cmd = new EnvInspectCommand({
        getToken: () => configStore.getToken(),
        getEnvironment: async (envName: string) => {
          return apiClient.get<{ name: string; status: string; projects: Array<{ name: string; release_id: string; url: string }> }>(
            `/api/v1/environments/${encodeURIComponent(envName)}`
          );
        },
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.execute(name, { json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Environment inspect timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'ENV_INSPECT_TIMEOUT', error: err.message, exit_code: 1 }));
        } else {
          writeError(err.message);
        }
        return 1;
      });
    });

  // --- domain (MEET-2026-04-05-033: Custom Domains) ---
  const domainCmd = program
    .command('domain')
    .description('Manage custom domains');

  domainCmd
    .command('add <domain>')
    .description('Register a custom domain for an app')
    .requiredOption('--project <name>', 'Project name')
    .requiredOption('--app <name>', 'App name')
    .requiredOption('--env <name>', 'Environment name')
    .action(async (domain: string, opts: { project: string; app: string; env: string }) => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined });

      const cmd = new DomainAddCommand({
        getToken: () => configStore.getToken(),
        createDomain: async (body) => apiClient.post('/api/v1/domains', body, { 'Idempotency-Key': 'domain-' + (body as any).domain + '-' + Date.now() }),
        verifyDomain: async (d) => apiClient.post(`/api/v1/domains/${encodeURIComponent(d)}/verify`, {}),
        getDomain: async (d) => apiClient.get(`/api/v1/domains/${encodeURIComponent(d)}`),
        listDomains: async (params) => {
          const qs = new URLSearchParams();
          if (params.project) qs.set('project', params.project);
          if (params.app) qs.set('app', params.app);
          if (params.environment) qs.set('environment', params.environment);
          return apiClient.get(`/api/v1/domains?${qs.toString()}`);
        },
        deleteDomain: async (d) => apiClient.del(`/api/v1/domains/${encodeURIComponent(d)}`),
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.execute(domain, { ...opts, json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Domain add timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) { writeTo(JSON.stringify({ code: 'DOMAIN_ADD_TIMEOUT', error: err.message, exit_code: 3 })); }
        else { writeError(err.message); }
        return 3;
      });
    });

  domainCmd
    .command('verify <domain>')
    .description('Verify DNS records for a custom domain')
    .action(async (domain: string) => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined });

      const cmd = new DomainVerifyCommand({
        getToken: () => configStore.getToken(),
        createDomain: async (body) => apiClient.post('/api/v1/domains', body, { 'Idempotency-Key': 'domain-' + (body as any).domain + '-' + Date.now() }),
        verifyDomain: async (d) => apiClient.post(`/api/v1/domains/${encodeURIComponent(d)}/verify`, {}),
        getDomain: async (d) => apiClient.get(`/api/v1/domains/${encodeURIComponent(d)}`),
        listDomains: async () => apiClient.get('/api/v1/domains'),
        deleteDomain: async (d) => apiClient.del(`/api/v1/domains/${encodeURIComponent(d)}`),
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.execute(domain, { json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Domain verify timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) { writeTo(JSON.stringify({ code: 'DOMAIN_VERIFY_TIMEOUT', error: err.message, exit_code: 3 })); }
        else { writeError(err.message); }
        return 3;
      });
    });

  domainCmd
    .command('status <domain>')
    .description('Show custom domain status and details')
    .action(async (domain: string) => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined });

      const cmd = new DomainStatusCommand({
        getToken: () => configStore.getToken(),
        createDomain: async (body) => apiClient.post('/api/v1/domains', body, { 'Idempotency-Key': 'domain-' + (body as any).domain + '-' + Date.now() }),
        verifyDomain: async (d) => apiClient.post(`/api/v1/domains/${encodeURIComponent(d)}/verify`, {}),
        getDomain: async (d) => apiClient.get(`/api/v1/domains/${encodeURIComponent(d)}`),
        listDomains: async () => apiClient.get('/api/v1/domains'),
        deleteDomain: async (d) => apiClient.del(`/api/v1/domains/${encodeURIComponent(d)}`),
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.execute(domain, { json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Domain status timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) { writeTo(JSON.stringify({ code: 'DOMAIN_STATUS_TIMEOUT', error: err.message, exit_code: 3 })); }
        else { writeError(err.message); }
        return 3;
      });
    });

  domainCmd
    .command('list')
    .description('List custom domains')
    .option('--project <name>', 'Filter by project')
    .option('--app <name>', 'Filter by app')
    .option('--env <name>', 'Filter by environment')
    .action(async (opts: { project?: string; app?: string; env?: string }) => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined });

      const cmd = new DomainListCommand({
        getToken: () => configStore.getToken(),
        createDomain: async (body) => apiClient.post('/api/v1/domains', body, { 'Idempotency-Key': 'domain-' + (body as any).domain + '-' + Date.now() }),
        verifyDomain: async (d) => apiClient.post(`/api/v1/domains/${encodeURIComponent(d)}/verify`, {}),
        getDomain: async (d) => apiClient.get(`/api/v1/domains/${encodeURIComponent(d)}`),
        listDomains: async (params) => {
          const qs = new URLSearchParams();
          if (params.project) qs.set('project', params.project);
          if (params.app) qs.set('app', params.app);
          if (params.environment) qs.set('environment', params.environment);
          return apiClient.get(`/api/v1/domains?${qs.toString()}`);
        },
        deleteDomain: async (d) => apiClient.del(`/api/v1/domains/${encodeURIComponent(d)}`),
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.execute({ ...opts, json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Domain list timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) { writeTo(JSON.stringify({ code: 'DOMAIN_LIST_TIMEOUT', error: err.message, exit_code: 3 })); }
        else { writeError(err.message); }
        return 3;
      });
    });

  domainCmd
    .command('remove <domain>')
    .description('Remove a custom domain')
    .option('--yes', 'Skip confirmation prompt')
    .action(async (domain: string, opts: { yes?: boolean }) => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined });

      const cmd = new DomainRemoveCommand({
        getToken: () => configStore.getToken(),
        createDomain: async (body) => apiClient.post('/api/v1/domains', body, { 'Idempotency-Key': 'domain-' + (body as any).domain + '-' + Date.now() }),
        verifyDomain: async (d) => apiClient.post(`/api/v1/domains/${encodeURIComponent(d)}/verify`, {}),
        getDomain: async (d) => apiClient.get(`/api/v1/domains/${encodeURIComponent(d)}`),
        listDomains: async () => apiClient.get('/api/v1/domains'),
        deleteDomain: async (d) => apiClient.del(`/api/v1/domains/${encodeURIComponent(d)}`),
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.execute(domain, { ...opts, json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Domain remove timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) { writeTo(JSON.stringify({ code: 'DOMAIN_REMOVE_TIMEOUT', error: err.message, exit_code: 3 })); }
        else { writeError(err.message); }
        return 3;
      });
    });

  domainCmd
    .command('dns <domain>')
    .description('List DNS records for a Cloudflare-managed domain')
    .action(async (domain: string) => {
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined });

      const cmd = new DomainDNSCommand({
        getToken: () => configStore.getToken(),
        createDomain: async (body) => apiClient.post('/api/v1/domains', body, { 'Idempotency-Key': 'domain-' + (body as any).domain + '-' + Date.now() }),
        verifyDomain: async (d) => apiClient.post(`/api/v1/domains/${encodeURIComponent(d)}/verify`, {}),
        getDomain: async (d) => apiClient.get(`/api/v1/domains/${encodeURIComponent(d)}`),
        listDomains: async () => apiClient.get('/api/v1/domains'),
        deleteDomain: async (d) => apiClient.del(`/api/v1/domains/${encodeURIComponent(d)}`),
        listDNSRecords: async (d) => apiClient.get(`/api/v1/domains/${encodeURIComponent(d)}/dns-records`),
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.execute(domain, { json: program.opts().json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Domain DNS lookup timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        const globalOpts = program.opts();
        if (globalOpts.json) { writeTo(JSON.stringify({ code: 'DOMAIN_DNS_TIMEOUT', error: err.message, exit_code: 3 })); }
        else { writeError(err.message); }
        return 3;
      });
    });

  // --- connect (GitHub App auto-deploy) ---
  program
    .command('connect')
    .description('Connect a GitHub repository for auto-deploy on push')
    .option('--repo <owner/repo>', 'GitHub repository (auto-detected from git remote)')
    .option('--branch <branch>', 'Default branch for production deploys', 'main')
    .option('--installation-id <id>', 'GitHub App installation ID')
    .action(async (opts: { repo?: string; branch?: string; installationId?: string }) => {
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined });

      const cmd = new ConnectCommand({
        getToken: () => configStore.getToken(),
        getGitRemote: () => {
          try {
            return childProcess.execSync('git remote get-url origin', { encoding: 'utf-8' }).trim();
          } catch { return null; }
        },
        getProjectName: () => {
          const yamlResult = findLoopZeroYaml(process.cwd(), process.cwd());
          return yamlResult?.content?.project ?? null;
        },
        connectRepo: async (project, body) => apiClient.post(`/api/v1/projects/${encodeURIComponent(project)}/connect`, body),
        disconnectRepo: async (project) => apiClient.del(`/api/v1/projects/${encodeURIComponent(project)}/connect`),
        getConnection: async (project) => apiClient.get<any>(`/api/v1/projects/${encodeURIComponent(project)}/connect`).catch(() => null),
        writeTo,
        writeError,
      });

      process.exitCode = await cmd.execute({
        repo: opts.repo,
        branch: opts.branch,
        installationId: opts.installationId ? parseInt(opts.installationId, 10) : undefined,
        json: program.opts().json,
      });
    });

  program
    .command('disconnect')
    .description('Disconnect GitHub repository (stop auto-deploy)')
    .action(async () => {
      const token = configStore.getToken();
      const baseURL = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined });

      const cmd = new DisconnectCommand({
        getToken: () => configStore.getToken(),
        getGitRemote: () => null,
        getProjectName: () => {
          const yamlResult = findLoopZeroYaml(process.cwd(), process.cwd());
          return yamlResult?.content?.project ?? null;
        },
        connectRepo: async () => { throw new Error('not used'); },
        disconnectRepo: async (project) => apiClient.del(`/api/v1/projects/${encodeURIComponent(project)}/connect`),
        getConnection: async () => null,
        writeTo,
        writeError,
      });

      process.exitCode = await cmd.execute({ json: program.opts().json });
    });

  // --- billing ---
  const billingCmd = program
    .command('billing')
    .description('View plan and usage, or open the billing portal');

  billingCmd
    .command('portal')
    .description('Open the billing portal in your browser')
    .action(async () => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const cloudURL = getCloudUrl();
      const apiClient = new ApiClient({ baseURL: cloudURL, token: token ?? undefined, onUnauthorized: handleUnauthorized });

      const cmd = new BillingCommand({
        getBilling: async () => apiClient.get('/v1/billing'),
        getPortalURL: async () => apiClient.get('/v1/billing/portal'),
        openBrowser,
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.execute({ json: globalOpts.json, subcommand: 'portal' }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Billing portal request timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) { writeTo(JSON.stringify({ code: 'BILLING_TIMEOUT', error: err.message, exit_code: 3 })); }
        else { writeError(err.message); }
        return 3;
      });
    });

  billingCmd
    .action(async () => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const cloudURL = getCloudUrl();
      const apiClient = new ApiClient({ baseURL: cloudURL, token: token ?? undefined, onUnauthorized: handleUnauthorized });

      const cmd = new BillingCommand({
        getBilling: async () => apiClient.get('/v1/billing'),
        getPortalURL: async () => apiClient.get('/v1/billing/portal'),
        openBrowser,
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.execute({ json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Billing request timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) { writeTo(JSON.stringify({ code: 'BILLING_TIMEOUT', error: err.message, exit_code: 3 })); }
        else { writeError(err.message); }
        return 3;
      });
    });

  // --- audit ---
  program
    .command('audit')
    .description('View audit log (requires admin or owner role)')
    .option('--action <action>', 'Filter by action (e.g., project.created)')
    .option('--since <date>', 'Filter by date (e.g., 2026-04-01)')
    .action(async (cmdOpts: { action?: string; since?: string }) => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const apiUrl = getDefaultApiUrl();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: token ?? undefined, onUnauthorized: handleUnauthorized });

      const { AuditCommand } = await import('./commands/audit');
      const cmd = new AuditCommand({
        getToken: () => configStore.getToken(),
        getAudit: async (action?: string, since?: string) => {
          const params = new URLSearchParams();
          if (action) params.set('action', action);
          if (since) params.set('since', since);
          params.set('limit', '20');
          return apiClient.get(`/api/v1/audit?${params.toString()}`);
        },
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.run({
          json: globalOpts.json,
          action: cmdOpts.action,
          since: cmdOpts.since,
        }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Audit log request timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) { writeTo(JSON.stringify({ code: 'AUDIT_TIMEOUT', error: err.message, exit_code: 3 })); }
        else { writeError(err.message); }
        return 3;
      });
    });

  // Handle case when no subcommand is given (e.g., `loopzero` or `loopzero --json`)
  // Also handles unknown commands since Commander passes them as arguments to the default action.
  program.argument('[args...]');
  program.action((args: string[]) => {
    const globalOpts = program.opts();
    const unknownCommand = args.length > 0 ? args[0] : null;

    if (unknownCommand) {
      // Unknown command provided
      if (globalOpts.json) {
        writeTo(JSON.stringify({
          error: `Unknown command: ${unknownCommand}. Run: loopzero --help`,
          exit_code: 1,
        }));
      } else {
        writeError(`Unknown command: ${unknownCommand}. Run: loopzero --help`);
      }
      process.exitCode = 1;
    } else {
      // No command provided at all
      if (globalOpts.json) {
        const commandNames = program.commands.map((cmd) => cmd.name());
        writeTo(JSON.stringify({
          error: 'No command specified. Run: loopzero --help',
          commands: commandNames,
          exit_code: 1,
        }));
        process.exitCode = 1;
      } else {
        program.help();
      }
    }
  });

  return program;
}

export const program = createProgram();

if (require.main === module) {
  // exitOverride makes Commander throw instead of calling process.exit(),
  // letting us format errors as JSON when --json is active.
  program.exitOverride();

  program.parseAsync(process.argv).catch((err: unknown) => {
    const isJSON = process.argv.includes('--json');
    const message = err instanceof Error ? err.message : String(err);
    // Commander errors have exitCode property
    const exitCode = (err as { exitCode?: number }).exitCode || 1;

    if (isJSON) {
      process.stdout.write(JSON.stringify({
        error: message,
        exit_code: exitCode,
        code: 'COMMAND_VALIDATION_ERROR',
      }) + '\n');
    } else {
      process.stderr.write(`Error: ${message}\n`);
    }
    process.exitCode = exitCode;
  });
}
