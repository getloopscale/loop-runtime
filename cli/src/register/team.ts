// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiClient } from '../api/client';
import { TeamListCommand } from '../commands/team-list';
import { TeamAddCommand } from '../commands/team-add';
import { TeamRemoveCommand } from '../commands/team-remove';
import { TeamRoleCommand } from '../commands/team-role';
import { API_REQUEST_TIMEOUT_MS } from '../config/constants';
import { CommandDeps } from './types';

export function registerTeamCommands(deps: CommandDeps): void {
  const { program, configStore, writeTo, writeError, getGlobalOpts, apiUrl, handleUnauthorized, withTimeout } = deps;

  const teamCmd = program
    .command('team')
    .description('Manage team members');

  teamCmd
    .command('list')
    .description('List all team members')
    .action(async () => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

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
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

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
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

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
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

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
}
