import { Command } from 'commander';

import { ConfigStore } from './config/store';
import { ApiClient } from './api/client';
import { Formatter } from './output/formatter';
import { LoginCommand } from './commands/login';
import { LogoutCommand } from './commands/logout';
import { WhoamiCommand } from './commands/whoami';
import { InitCommand } from './commands/init';
import { detectMonorepoTool } from './detection/monorepo';
import { detectFramework } from './detection/framework';

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
      const apiClient = new ApiClient({ token: cmdOpts.token });

      const loginCmd = new LoginCommand({
        saveToken: (token, email, team) =>
          configStore.saveToken(token, email, team),
        validateToken: async (token: string) => {
          try {
            const result = await apiClient.post<{
              valid: boolean;
              email: string | null;
              teams: string[];
            }>('/auth/validate', { token });
            return result;
          } catch {
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
      const apiClient = new ApiClient({ token: token ?? undefined });

      const initCmd = new InitCommand({
        getToken: () => configStore.getToken(),
        getTeam: () => configStore.getTeam(),
        detectMonorepo: (dir: string) => detectMonorepoTool(dir),
        detectFramework: (dir: string) => detectFramework(dir),
        createProject: async (input) => {
          return apiClient.post<{ id: string; name: string; team: string }>(
            '/projects',
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

  return program;
}

export const program = createProgram();

if (require.main === module) {
  program.parseAsync(process.argv).catch((err: Error) => {
    process.stderr.write(`Error: ${err.message}\n`);
    process.exitCode = 1;
  });
}
