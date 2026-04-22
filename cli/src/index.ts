// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Suppress Node.js ExperimentalWarning (buffer.File in Node 20 multipart uploads).
// In Node 22+ this warning is gone. Filter instead of monkey-patching process.emit.
process.removeAllListeners('warning');
process.on('warning', (warning) => {
  if (warning.name !== 'ExperimentalWarning') {
    console.warn(warning);
  }
});

import { Command } from 'commander';

import { ConfigStore } from './config/store';
import { getDefaultApiUrl, getCloudUrl } from './config/constants';
import { ApiClient } from './api/client';
import { Formatter } from './output/formatter';
import { ensureFreshToken } from './auth/token-refresh';

import { registerAuthCommands } from './register/auth';
import { registerInitCommand } from './register/init';
import { registerDeployCommands } from './register/deploy';
import { registerProjectCommands } from './register/project';
import { registerTeamCommands } from './register/team';
import { registerEnvCommands } from './register/env';
import { registerDomainCommands } from './register/domain';
import { registerInfraCommands } from './register/infra';
import type { CommandDeps } from './register/types';

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

  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { version: cliVersion } = require('../package.json');

  program
    .name('loopzero')
    .version(cliVersion)
    .description('CLI-first Kubernetes PaaS for monorepo deployments')
    .option('--json', 'Output in JSON format');

  // F-05: Configure Commander to output JSON errors when --json is present.
  program.configureOutput({
    writeOut: (str: string) => process.stdout.write(str),
    writeErr: (str: string) => {
      if (process.argv.includes('--json')) {
        const cleaned = str.replace(/^error:\s*/i, '').trim();
        process.stdout.write(JSON.stringify({
          error: cleaned,
          exit_code: 1,
          code: 'COMMAND_VALIDATION_ERROR',
        }) + '\n');
      } else {
        process.stderr.write(str);
      }
    },
    outputError: (str: string, write: (s: string) => void) => write(str),
  });

  const configStore = ConfigStore.default();
  const isCI = Formatter.detectCI();
  const apiUrl = getDefaultApiUrl();
  const cloudUrl = getCloudUrl();

  const writeTo = (msg: string): void => {
    process.stdout.write(msg + '\n');
  };
  const writeError = (msg: string): void => {
    process.stderr.write(msg + '\n');
  };

  // Token refresh callback — used by ApiClient when a 401 is received.
  const handleUnauthorized = async (): Promise<string | null> => {
    return ensureFreshToken({
      getToken: () => configStore.getToken(),
      getRefreshToken: () => configStore.getRefreshToken(),
      isTokenExpired: () => true, // Already got 401 — treat as expired
      saveToken: (t, e, tm, rt, ei) => configStore.saveToken(t, e, tm, rt, ei),
      getEmail: () => configStore.getEmail(),
      getTeam: () => configStore.getTeam(),
      fetchFn: fetch,
      writeError,
    });
  };

  // Shared dependencies for all register functions
  const deps: CommandDeps = {
    program,
    configStore,
    isCI,
    writeTo,
    writeError,
    mkApiClient: (baseURL?: string) => new ApiClient({
      baseURL: baseURL ?? apiUrl,
      token: configStore.getToken() ?? undefined,
      onUnauthorized: handleUnauthorized,
    }),
    getToken: () => configStore.getToken(),
    getGlobalOpts: () => program.opts(),
    handleUnauthorized,
    apiUrl,
    cloudUrl,
    withTimeout,
  };

  // Register all command groups
  registerAuthCommands(deps);
  registerInitCommand(deps);
  registerDeployCommands(deps);
  registerProjectCommands(deps);
  registerTeamCommands(deps);
  registerEnvCommands(deps);
  registerDomainCommands(deps);
  registerInfraCommands(deps);

  // Handle case when no subcommand is given (e.g., `loopzero` or `loopzero --json`)
  program.argument('[args...]');
  program.action((args: string[]) => {
    const globalOpts = program.opts();
    const unknownCommand = args.length > 0 ? args[0] : null;

    if (unknownCommand) {
      if (globalOpts.json) {
        writeTo(JSON.stringify({
          error: `Unknown command: ${unknownCommand}. Run: loopzero --help`,
          exit_code: 1,
        }));
      } else {
        writeError(`Unknown command: ${unknownCommand}. Run: loopzero --help`);
      }
      process.exitCode = 1;
    } else {
      if (globalOpts.json) {
        const commandNames = program.commands.map((cmd) => cmd.name());
        writeTo(JSON.stringify({
          error: 'No command specified. Run: loopzero --help',
          commands: commandNames,
          exit_code: 1,
        }));
        process.exitCode = 1;
      } else {
        program.help();
      }
    }
  });

  return program;
}

export const program = createProgram();

if (require.main === module) {
  program.exitOverride();

  program.parseAsync(process.argv).catch((err: unknown) => {
    const isJSON = process.argv.includes('--json');
    const message = err instanceof Error ? err.message : String(err);
    const exitCode = (err as { exitCode?: number }).exitCode || 1;

    if (isJSON) {
      process.stdout.write(JSON.stringify({
        error: message,
        exit_code: exitCode,
        code: 'COMMAND_VALIDATION_ERROR',
      }) + '\n');
    } else {
      process.stderr.write(`Error: ${message}\n`);
    }
    process.exitCode = exitCode;
  });
}
