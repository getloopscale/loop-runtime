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
    .action(async (cmdOpts) => {
      const globalOpts = getGlobalOpts();
      const token = configStore.getToken();

      const initCmd = new InitCommand({
        getToken: () => configStore.getToken(),
        getTeam: () => configStore.getTeam(),
        hasGitRepo,
        detectMonorepo: (dir: string) => detectMonorepoTool(dir),
        detectFramework: (dir: string) => detectFramework(dir),
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
