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
import { initTracing, shutdownTracing, getTracer, SpanStatusCode } from './tracing';

import { ConfigStore } from './config/store';
import { getDefaultApiUrl, getCloudUrl } from './config/constants';
import { ApiClient } from './api/client';
import { Formatter } from './output/formatter';
import { ensureFreshToken } from './auth/token-refresh';

import { registerAuthCommands } from './register/auth';
// Plan B v3.1 T-B.2 (2026-06-05) — `loopzero init` deleted post LoopZeroKit-only cutover.
// All apps must use LoopZeroKit ≥0.5.0 + emit `.loopzero/services.json` via `loopzerokit build`.
// import { registerInitCommand } from './register/init';
import { registerDeployCommands } from './register/deploy';
import { registerProjectCommands } from './register/project';
import { registerTeamCommands } from './register/team';
import { registerEnvCommands } from './register/env';
import { registerDomainCommands } from './register/domain';
import { registerInfraCommands } from './register/infra';
import { registerPreviewCommands } from './register/preview';
import { registerScaleCommands } from './register/scale';
import { registerPlatformCommands } from './register/platform';
import { registerDiagnosticsCommands } from './register/diagnostics';
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

// Rule 4: exit codes are permanent — only {0, 1, 2, 3, 4, 5} permitted.
// SIGINT/SIGTERM cleanup → exit 1 (treated as usage error: user canceled).
// Plan: 2026-06-15-code-review-49-findings-remediation T0.2 (R3-F1 BLOCKER).
// See .claude/rules/cli-output.md § Exit Codes (Rule 4 — Permanent).
process.on('SIGINT', () => { runCleanup(); process.exit(1); });
process.on('SIGTERM', () => { runCleanup(); process.exit(1); });

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
    .description('CLI-first Kubernetes LoopZeroCloud for monorepo deployments')
    .option('--json', 'Output in JSON format');

  // F-05: Configure Commander to output JSON errors when --json is present.
  // F-03 (CHG-2026-04-29-DOGFOOD T1.2): Commander's outputError fires for
  // parse errors (unknown option/command, missing arg). We seta
  // `process.exitCode = 1` here como guarantee — em algumas versões do
  // Commander, exitOverride é silenciosamente engolido quando configureOutput
  // está presente, então este é o mais confiável "ponto de set" para exit
  // code 1 em parse errors.
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
      // F-03: Rule 4 mandates exit 1 for usage errors. Set exitCode here.
      process.exitCode = 1;
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
  // CHG-2026-04-23-AUDIT-16: ensureFreshToken no longer writes to stderr;
  // caller handles error presentation (json/human) after receiving null.
  const handleUnauthorized = async (): Promise<string | null> => {
    return ensureFreshToken({
      getToken: () => configStore.getToken(),
      getRefreshToken: () => configStore.getRefreshToken(),
      isTokenExpired: () => true, // Already got 401 — treat as expired
      saveToken: (t, e, tm, rt, ei) => configStore.saveToken(t, e, tm, rt, ei),
      getEmail: () => configStore.getEmail(),
      getTeam: () => configStore.getTeam(),
      fetchFn: fetch,
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
  // Plan B v3.1 T-B.2 — `loopzero init` removed (LoopZeroKit-only cutover).
  registerDeployCommands(deps);
  registerProjectCommands(deps);
  registerTeamCommands(deps);
  registerEnvCommands(deps);
  registerDomainCommands(deps);
  registerInfraCommands(deps);
  registerPreviewCommands(deps);
  registerScaleCommands(deps);
  registerPlatformCommands(deps);
  registerDiagnosticsCommands(deps);

  // Plan v1.1 T4.1 (loopzero-cli-vercel-like-ux) — zero-config delegation.
  // `loopzero` with no positional subcommand and no --help/--version routes to
  // `loopzero deploy` (Vercel-parity ergonomic). EC-8 absorbed: help/version
  // flags ALWAYS win, regardless of argv position; Commander handles them
  // via exitOverride before this action fires, but we also guard
  // defensively in case the action is reached.
  program.argument('[args...]');
  program.action(async (args: string[]) => {
    const globalOpts = program.opts();
    const unknownCommand = args.length > 0 ? args[0] : null;

    // EC-8 — explicit guard: never delegate when --help/-h/--version/-V is
    // anywhere in argv. Commander typically handles these earlier; this is a
    // belt-and-suspenders check.
    const argv = process.argv.slice(2);
    if (
      argv.includes('--help') ||
      argv.includes('-h') ||
      argv.includes('--version') ||
      argv.includes('-V')
    ) {
      program.help();
      return;
    }

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
      return;
    }

    // No positional subcommand → delegate to `loopzero deploy` per Plan v1.1 D3.
    const deployCmd = program.commands.find((cmd) => cmd.name() === 'deploy');
    if (deployCmd === undefined) {
      // Defensive: deploy registration somehow missing. Fall back to help.
      program.help();
      return;
    }
    // Reconstruct argv with `deploy` injected as the subcommand so commander
    // re-parses with the right action wired.
    const newArgv = [process.argv[0], process.argv[1], 'deploy', ...argv];
    try {
      await program.parseAsync(newArgv);
    } catch {
      // parseAsync errors already surface via Commander's exitOverride hook.
    }
  });

  // F-03 (CHG-2026-04-29-DOGFOOD T1.2) — exitOverride para parse errors
  // (unknown option/command, missing required arg). Sem isso, Commander.js
  // imprime "error: unknown option" no stderr mas o processo termina com
  // exit 0, mascarando typos em scripts CI/CD com `set -e`. Rule 4 exige
  // exit 1 para usage errors. Para help/version (também throw via
  // exitOverride) precisamos exit 0 — não são erros, são modos legítimos.
  // Para ambos os caminhos: silenciar a re-throw (parseAsync resolve
  // limpo); para usage errors o exitCode=1 já foi setado via writeErr.
  program.exitOverride((err) => {
    // Help / version são exit 0 success cases. Não throw, deixa o caller
    // resolver sem ruído de "Error: (outputHelp)" no stderr.
    if (err.code === 'commander.helpDisplayed' || err.code === 'commander.version' || err.code === 'commander.help') {
      return;
    }
    // Parse errors: exitCode=1 já foi setado por writeErr; re-throw para
    // que parseAsync rejeite e o bin/loopzero .catch handle propagation.
    const usageCodes = new Set([
      'commander.unknownCommand',
      'commander.unknownOption',
      'commander.missingArgument',
      'commander.missingMandatoryOptionValue',
      'commander.optionMissingArgument',
      'commander.invalidOptionArgument',
      'commander.invalidArgument',
      'commander.excessArguments',
    ]);
    if (usageCodes.has(err.code)) {
      process.exitCode = 1;
    }
    throw err;
  });

  return program;
}

export const program = createProgram();

if (require.main === module) {
  // Initialize OTel tracing (opt-in via LOOPZERO_TRACING_ENABLED=true).
  initTracing();

  process.on('beforeExit', async () => {
    await shutdownTracing();
  });

  // F-03 (CHG-2026-04-29-DOGFOOD T1.2) — exitOverride já é configurado em
  // createProgram() com tratamento granular de codes de parse error.
  // Removido o `program.exitOverride()` placeholder que sobrescrevia esse
  // tratamento sem preservar o exit code 1 para usage errors.

  // Root span wrapping the entire CLI command execution.
  const commandName = process.argv.slice(2).find(a => !a.startsWith('-')) ?? 'unknown';
  const tracer = getTracer();
  tracer.startActiveSpan('loopzero.cli.command', (rootSpan) => {
    rootSpan.setAttribute('loopzero.cli.command_name', commandName);
    rootSpan.setAttribute('loopzero.cli.args', process.argv.slice(2).join(' '));

    program.parseAsync(process.argv)
      .then(() => {
        rootSpan.end();
      })
      .catch((err: unknown) => {
        const isJSON = process.argv.includes('--json');
        const message = err instanceof Error ? err.message : String(err);
        const exitCode = (err as { exitCode?: number }).exitCode || 1;

        rootSpan.setStatus({ code: SpanStatusCode.ERROR, message });
        rootSpan.end();

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
  });
}
