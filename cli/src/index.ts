// Suppress Node.js ExperimentalWarning (buffer.File in Node 20 multipart uploads)
// Must run before any imports that might trigger the warning.
const _origEmit = process.emit;
// eslint-disable-next-line @typescript-eslint/no-explicit-any
(process as any).emit = function (event: string, ...args: unknown[]) {
  if (event === 'warning' && args[0] && (args[0] as { name?: string }).name === 'ExperimentalWarning') {
    return false;
  }
  return _origEmit.apply(process, [event, ...args] as Parameters<typeof _origEmit>);
};

import { Command } from 'commander';

import { ConfigStore } from './config/store';
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

import { hasGitRepo, hasUncommittedChanges, hasLockfile, getHeadCommit, getGitRoot, createGitBundle } from './adapters/git';
import { findLoopZeroYaml } from './adapters/loopzero-yaml';
import { cleanupFile } from './adapters/source-bundle';
import { uploadSource } from './adapters/source-upload';

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
              baseURL: process.env.LOOPZERO_API_URL ?? 'http://localhost:3001',
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
        detectMonorepo: (dir: string) => detectMonorepoTool(dir),
        detectFramework: (dir: string) => detectFramework(dir),
        createProject: async (input) => {
          const baseURL = process.env.LOOPZERO_API_URL ?? 'http://localhost:3001';
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

      const exitCode = await initCmd.execute({
        yes: cmdOpts.yes,
        force: cmdOpts.force,
        name: cmdOpts.name,
        json: globalOpts.json,
      });
      process.exitCode = exitCode;
    });

  // --- deploy ---
  program
    .command('deploy')
    .description('Deploy your application. Use --json for structured output.')
    .option('--env <name>', 'Target environment', 'production')
    .option('--app <name...>', 'Deploy specific app(s)')
    .option('--no-wait', 'Return immediately after submit')
    .action(async (cmdOpts) => {
      const globalOpts = program.opts();
      const token = configStore.getToken();
      const baseURL = process.env.LOOPZERO_API_URL ?? 'http://localhost:3001';
      const apiClient = new ApiClient({ baseURL, token: token ?? undefined });
      const { randomUUID } = await import('crypto');

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
        generateIdempotencyKey: () => randomUUID(),
        writeTo,
        writeError,
        isCI,
        isTTY: process.stdout.isTTY ?? false,
      });

      const exitCode = await deployCmd.execute({
        env: cmdOpts.env,
        app: cmdOpts.app,
        wait: cmdOpts.wait,
        json: globalOpts.json,
      });
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
