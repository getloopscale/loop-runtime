// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiClient } from '../api/client';
import { EnvListCommand } from '../commands/env-list';
import { EnvCreateCommand } from '../commands/env-create';
import { EnvDeleteCommand } from '../commands/env-delete';
import { EnvInspectCommand } from '../commands/env-inspect';
import { API_REQUEST_TIMEOUT_MS } from '../config/constants';
import { CommandDeps } from './types';

export function registerEnvCommands(deps: CommandDeps): void {
  const { program, configStore, writeTo, writeError, getGlobalOpts, apiUrl, handleUnauthorized, withTimeout } = deps;

  const envCmd = program
    .command('env')
    .description('Manage environments');

  envCmd
    .command('list')
    .description('List all environments for the current team')
    .option('--project <name>', 'Filter environments by project name (plan T2.3 D6)')
    .action(async (cmdOpts: { project?: string }) => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const cmd = new EnvListCommand({
        getToken: () => configStore.getToken(),
        listEnvironments: async () => {
          const query = cmdOpts.project ? `?project=${encodeURIComponent(cmdOpts.project)}` : '';
          return apiClient.get<{ environments: Array<{ name: string; status: string }> }>(`/api/v1/environments${query}`);
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
    .description('Create a new environment (M2: --project required)')
    .option('--project <name>', 'Project slug the environment belongs to (M2 v1.2 T6.1; REQUIRED post-M2)')
    .action(async (name: string, cmdOpts: { project?: string }) => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const cmd = new EnvCreateCommand({
        getToken: () => configStore.getToken(),
        // M2 v1.2 T6.1: project propagates from --project flag into the
        // POST body so the canonical 3-segment namespace lands.
        createEnvironment: async (envName: string, project: string) => {
          return apiClient.post<{ name: string; status: string }>(
            '/api/v1/environments',
            { name: envName, project }
          );
        },
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.execute(name, { json: globalOpts.json, project: cmdOpts.project }),
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
    .option('--confirm <name>', 'Confirm deletion by repeating the environment name (safer)')
    .option('--yes', 'Skip confirmation prompt (CI/scripts; consistent with other delete commands)')
    .option('--project <name>', 'Project slug the environment belongs to (M2 v1.2 T6.1 — required for non-legacy envs)')
    .action(async (name: string, cmdOpts: { confirm?: string; yes?: boolean; project?: string }) => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const cmd = new EnvDeleteCommand({
        getToken: () => configStore.getToken(),
        // M2 v1.2 T6.1 expanded: append `?project=<slug>` when set so
        // the backend filters by the (name, project) tuple. Empty
        // project preserves the legacy production-only lookup.
        deleteEnvironment: async (envName: string, project: string) => {
          const url = `/api/v1/environments/${encodeURIComponent(envName)}`;
          const query = project ? `?project=${encodeURIComponent(project)}` : '';
          await apiClient.del<void>(`${url}${query}`);
        },
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.execute(name, {
          confirm: cmdOpts.confirm,
          yes: cmdOpts.yes,
          json: globalOpts.json,
          project: cmdOpts.project,
        }),
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
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

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
}
