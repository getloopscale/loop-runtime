// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiClient } from '../api/client';
import { ScaleCommand } from '../commands/scale';
import { API_REQUEST_TIMEOUT_MS } from '../config/constants';
import { getGitRoot } from '../adapters/git';
import { findLoopZeroYaml } from '../adapters/loopzero-yaml';
import { CommandDeps } from './types';

export function registerScaleCommands(deps: CommandDeps): void {
  const { program, configStore, writeTo, writeError, getGlobalOpts, apiUrl, handleUnauthorized, withTimeout } = deps;

  program
    .command('scale')
    .description('Scale a service (fixed replicas or autoscaling)')
    .requiredOption('--app <name>', 'App to scale')
    .option('--env <name>', 'Target environment', 'production')
    .option('--replicas <n>', 'Fixed replica count', parseInt)
    .option('--min <n>', 'Minimum replicas (autoscaling)', parseInt)
    .option('--max <n>', 'Maximum replicas (autoscaling)', parseInt)
    .option('--cpu <n>', 'CPU target percentage (autoscaling)', parseInt)
    .option('--json', 'Output in JSON format')
    .action(async (cmdOpts: {
      app: string;
      env?: string;
      replicas?: number;
      min?: number;
      max?: number;
      cpu?: number;
      json?: boolean;
    }) => {
      const globalOpts = getGlobalOpts();
      const jsonMode = Boolean(cmdOpts.json || globalOpts.json);
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const scaleCmd = new ScaleCommand({
        getToken: () => configStore.getToken(),
        findLoopZeroYaml: () => {
          const gitRoot = getGitRoot() ?? process.cwd();
          return findLoopZeroYaml(process.cwd(), gitRoot);
        },
        scale: async (project: string, environment: string, app: string, config) => {
          return apiClient.post(
            `/api/v1/projects/${encodeURIComponent(project)}/scale`,
            { environment, app, ...config },
          );
        },
        writeTo,
        writeError,
      });

      const exitCode = await withTimeout(
        scaleCmd.execute({
          app: cmdOpts.app,
          env: cmdOpts.env,
          replicas: cmdOpts.replicas,
          min: cmdOpts.min,
          max: cmdOpts.max,
          cpu: cmdOpts.cpu,
          json: jsonMode,
        }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Scale request timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (jsonMode) {
          writeTo(JSON.stringify({ code: 'SCALE_TIMEOUT', error: err.message, exit_code: 3 }));
        } else {
          writeError(err.message);
        }
        return 3;
      });
      process.exitCode = exitCode;
    });
}
