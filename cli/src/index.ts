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

import { ConfigStore } from './config/store';
import { getDefaultApiUrl, API_REQUEST_TIMEOUT_MS, DEPLOY_TIMEOUT_MS } from './config/constants';
import { ApiClient } from './api/client';
import { SSEClient } from './api/sse-client';
import { Formatter } from './output/formatter';
import { LoginCommand } from './commands/login';
import { LogoutCommand } from './commands/logout';
import { WhoamiCommand } from './commands/whoami';
import { InitCommand } from './commands/init';
import { DeployCommand } from './commands/deploy';
import { detectMonorepoTool } from './detection/monorepo';
import { detectFramework } from './detection/framework';

import { hasGitRepo, hasUncommittedChanges, hasLockfile, getHeadCommit, getGitRoot, createGitBundle, checkSensitiveFiles } from './adapters/git';
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

  program
    .name('loopzero')
    .version('0.1.0')
    .description('CLI-first Kubernetes PaaS for monorepo deployments')
    .option('--json', 'Output in JSON format');

  const configStore = ConfigStore.default();
  const isCI = Formatter.detectCI();

  const writeTo = (msg: string): void => {
    process.stdout.write(msg + '\n');
  };
  const writeError = (msg: string): void => {
    process.stderr.write(msg + '\n');
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
        saveToken: (token, email, team) =>
          configStore.saveToken(token, email, team),
        validateToken: async (token: string) => {
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
            // Network/connection errors — server unreachable
            const errMsg = (err as Error).message || '';
            if (errMsg.includes('ECONNREFUSED') || errMsg.includes('ETIMEDOUT') ||
                errMsg.includes('ENOTFOUND') || errMsg.includes('fetch failed')) {
              return { valid: false, email: null, teams: [], serverError: true };
            }
            // API returned 5xx server error
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
    .action(async () => {
      const globalOpts = program.opts();

      const logoutCmd = new LogoutCommand({
        clearToken: () => configStore.clearToken(),
        writeTo,
      });

      const exitCode = await logoutCmd.execute({
        json: globalOpts.json,
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
        getEmail: () => configStore.getEmail(),
        getTeam: () => configStore.getTeam(),
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
          const authedClient = new ApiClient({ baseURL, token: token ?? undefined });
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
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined });

      const deployCmd = new DeployCommand({
        getToken: () => configStore.getToken(),
        findLoopZeroYaml: () => {
          const gitRoot = getGitRoot();
          if (!gitRoot) return null;
          return findLoopZeroYaml(process.cwd(), gitRoot);
        },
        hasGitRepo,
        hasUncommittedChanges,
        hasLockfile,
        getHeadCommit,
        createGitBundle,
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
              apps: input.apps.map((a) => ({
                name: a.name,
                ...(a.type ? { type: a.type } : {}),
                ...(a.port ? { port: a.port } : {}),
                ...(a.path ? { path: a.path } : {}),
                ...(a.framework ? { framework: a.framework } : {}),
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
        generateIdempotencyKey: (commitSha: string, project: string, environment: string, apps: string[]) => {
          const input = [commitSha, project, environment, ...apps.sort()].join(':');
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

  return program;
}

export const program = createProgram();

if (require.main === module) {
  program.parseAsync(process.argv).catch((err: Error) => {
    process.stderr.write(`Error: ${err.message}\n`);
    process.exitCode = 1;
  });
}
