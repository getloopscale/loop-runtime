// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiClient } from '../api/client';
import { ProjectDeleteCommand } from '../commands/project-delete';
import { ProjectListCommand } from '../commands/project-list';
import { ProjectInspectCommand } from '../commands/project-inspect';
import { ProjectCreateCommand } from '../commands/project-create';
import { API_REQUEST_TIMEOUT_MS } from '../config/constants';
import { CommandDeps } from './types';

export function registerProjectCommands(deps: CommandDeps): void {
  const { program, configStore, isCI, writeTo, writeError, getGlobalOpts, apiUrl, handleUnauthorized, withTimeout } = deps;

  const projectCmd = program
    .command('project')
    .description('Manage projects');

  projectCmd
    .command('delete <name>')
    .description('Delete a project and its deployed resources')
    .option('--env <name>', 'Delete from specific environment only')
    .option('--yes', 'Skip confirmation prompt (required in CI)')
    .action(async (name: string, cmdOpts: { env?: string; yes?: boolean }) => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

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
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const cmd = new ProjectListCommand({
        getToken: () => configStore.getToken(),
        listProjects: async () => {
          return apiClient.get<{ projects: Array<{ id: string; name: string; team: string; created_at: string }> }>('/api/v1/projects');
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
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const cmd = new ProjectInspectCommand({
        getToken: () => configStore.getToken(),
        getProject: async (projectName: string) => {
          return apiClient.get<{ id: string; name: string; team: string; created_at: string; created_by?: string }>(
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
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const cmd = new ProjectCreateCommand({
        getToken: () => configStore.getToken(),
        createProject: async (projectName: string) => {
          return apiClient.post<{ id: string; name: string; team: string; created_at: string }>(
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
}
