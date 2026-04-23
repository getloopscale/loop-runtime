// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiClient } from '../api/client';
import {
  PreviewListCommand,
  PreviewInspectCommand,
  PreviewDeleteCommand,
  PreviewListApiResponse,
  PreviewInspectApiResponse,
  PreviewDeleteApiResponse,
} from '../commands/preview';
import { API_REQUEST_TIMEOUT_MS } from '../config/constants';
import { getGitRoot } from '../adapters/git';
import { findLoopZeroYaml } from '../adapters/loopzero-yaml';
import { promptConfirm } from '../output/prompt-confirm';
import { CommandDeps } from './types';

export function registerPreviewCommands(deps: CommandDeps): void {
  const { program, configStore, isCI, writeTo, writeError, getGlobalOpts, apiUrl, handleUnauthorized, withTimeout } = deps;

  const previewCmd = program
    .command('preview')
    .description('Manage preview environments');

  // --- preview list ---
  previewCmd
    .command('list')
    .description('List preview environments for the current project')
    .action(async () => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const previewDeps = createPreviewDeps(apiClient, configStore, apiUrl, writeTo, writeError, isCI);

      const cmd = new PreviewListCommand(previewDeps);
      const exitCode = await withTimeout(
        cmd.execute({ json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Preview list request timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'PREVIEW_TIMEOUT', error: err.message, exit_code: 3 }));
        } else {
          writeError(err.message);
        }
        return 3;
      });
      process.exitCode = exitCode;
    });

  // --- preview inspect ---
  previewCmd
    .command('inspect <id>')
    .description('Show details of a preview environment')
    .action(async (id: string) => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const previewDeps = createPreviewDeps(apiClient, configStore, apiUrl, writeTo, writeError, isCI);

      const cmd = new PreviewInspectCommand(previewDeps);
      const exitCode = await withTimeout(
        cmd.execute(id, { json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Preview inspect request timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'PREVIEW_TIMEOUT', error: err.message, exit_code: 3 }));
        } else {
          writeError(err.message);
        }
        return 3;
      });
      process.exitCode = exitCode;
    });

  // --- preview delete ---
  previewCmd
    .command('delete <id>')
    .description('Delete a preview environment')
    .option('--yes', 'Skip confirmation prompt')
    .action(async (id: string, cmdOpts: { yes?: boolean }) => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const previewDeps = createPreviewDeps(apiClient, configStore, apiUrl, writeTo, writeError, isCI);

      const cmd = new PreviewDeleteCommand(previewDeps);
      const exitCode = await withTimeout(
        cmd.execute(id, { json: globalOpts.json, yes: cmdOpts.yes }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Preview delete request timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'PREVIEW_TIMEOUT', error: err.message, exit_code: 3 }));
        } else {
          writeError(err.message);
        }
        return 3;
      });
      process.exitCode = exitCode;
    });
}

function createPreviewDeps(
  apiClient: ApiClient,
  configStore: { getToken: () => string | null },
  _apiUrl: string,
  writeTo: (msg: string) => void,
  writeError: (msg: string) => void,
  isCI: boolean,
) {
  return {
    getToken: () => configStore.getToken(),
    findLoopZeroYaml: () => {
      const gitRoot = getGitRoot() ?? process.cwd();
      return findLoopZeroYaml(process.cwd(), gitRoot);
    },
    listPreviews: async (project: string) => {
      return apiClient.get<PreviewListApiResponse>(`/api/v1/projects/${encodeURIComponent(project)}/previews`);
    },
    inspectPreview: async (project: string, id: string) => {
      return apiClient.get<PreviewInspectApiResponse>(`/api/v1/projects/${encodeURIComponent(project)}/previews/${encodeURIComponent(id)}`);
    },
    deletePreview: async (project: string, id: string) => {
      return apiClient.del<PreviewDeleteApiResponse>(`/api/v1/projects/${encodeURIComponent(project)}/previews/${encodeURIComponent(id)}`);
    },
    // CHG-2026-04-23-AUDIT-9: use shared promptConfirm that is safe on non-TTY
    // stdin. The old inline readline.question hung indefinitely on pipes.
    confirm: promptConfirm,
    writeTo,
    writeError,
    isCI,
  };
}
