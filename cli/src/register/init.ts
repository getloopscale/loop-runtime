// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiClient } from '../api/client';
import { InitCommand } from '../commands/init';
import { API_REQUEST_TIMEOUT_MS } from '../config/constants';
import { hasGitRepo } from '../adapters/git';
import { detectMonorepoTool } from '../detection/monorepo';
import { detectFramework } from '../detection/framework';
import { CommandDeps } from './types';

export function registerInitCommand(deps: CommandDeps): void {
  const { program, configStore, isCI, writeTo, writeError, getGlobalOpts, apiUrl, handleUnauthorized, withTimeout } = deps;

  // --- init ---
  program
    .command('init')
    .description('Initialize a project in the current directory')
    .option('--yes', 'Skip confirmation prompts (required in CI)')
    .option('--force', 'Overwrite existing loopzero.yaml')
    .option('--name <name>', 'Project name')
    .option('--offline', 'Skip server-side token validation; create loopzero.yaml locally only (F-05)')
    .action(async (cmdOpts) => {
      const globalOpts = getGlobalOpts();
      const token = configStore.getToken();

      const initCmd = new InitCommand({
        getToken: () => configStore.getToken(),
        getTeam: () => configStore.getTeam(),
        hasGitRepo,
        detectMonorepo: (dir: string) => detectMonorepoTool(dir),
        detectFramework: (dir: string) => detectFramework(dir),
        // F-05 (CHG-2026-04-29-DOGFOOD T1.3) — preflightAuth chamado antes de
        // escrever loopzero.yaml. Skipped quando --offline.
        preflightAuth: async () => {
          try {
            const client = new ApiClient({ baseURL: apiUrl, token: token ?? undefined });
            await client.get('/api/v1/auth/whoami');
            return { ok: true };
          } catch (err) {
            const e = err as { statusCode?: number; message?: string };
            if (e.statusCode === 401 || e.statusCode === 403) {
              return { ok: false, status: e.statusCode, reason: e.message };
            }
            return { ok: false, status: e.statusCode, reason: e.message || 'Unknown error contacting API' };
          }
        },
        createProject: async (input) => {
          const authedClient = new ApiClient({ baseURL: apiUrl, token: token ?? undefined, onUnauthorized: handleUnauthorized });
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
          offline: cmdOpts.offline,
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
}
