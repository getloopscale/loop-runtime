// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// T2.6 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap:
// Wire the `loopzero doctor` command (commands/doctor.ts, M01 audit gap).
// The DoctorCommand was fully implemented but never registered — closing
// that orphan-wiring gap here.

import { resolve } from 'path';
import { execSync } from 'child_process';
import { readFileSync, existsSync } from 'fs';

import { ApiClient, ApiError } from '../api/client';
import { DoctorCommand } from '../commands/doctor';
import { findLoopZeroYaml } from '../adapters/loopzero-yaml';
import { CommandDeps } from './types';

function getCliVersion(): string {
  try {
    const pkgPath = resolve(__dirname, '..', '..', 'package.json');
    const pkg = JSON.parse(readFileSync(pkgPath, 'utf-8')) as { version?: string };
    return pkg.version ?? 'unknown';
  } catch {
    return 'unknown';
  }
}

function findRepoRoot(cwd: string): string {
  try {
    const root = execSync('git rev-parse --show-toplevel', { cwd, stdio: ['pipe', 'pipe', 'ignore'] }).toString().trim();
    return root || cwd;
  } catch {
    return cwd;
  }
}

export function registerDiagnosticsCommands(deps: CommandDeps): void {
  const { program, configStore, isCI, writeTo, writeError, getGlobalOpts, apiUrl } = deps;

  program
    .command('doctor')
    .description('Run a health check on the CLI environment')
    .action(async () => {
      const globalOpts = getGlobalOpts();

      const doctorCmd = new DoctorCommand({
        getToken: () => configStore.getToken(),
        validateToken: async (token: string) => {
          try {
            const client = new ApiClient({ baseURL: apiUrl, token });
            const result = await client.get<{ email: string }>('/api/v1/auth/whoami');
            return { valid: true, email: result.email ?? null };
          } catch (err) {
            if (err instanceof ApiError && (err.statusCode === 401 || err.statusCode === 403)) {
              return { valid: false, email: null };
            }
            throw err;
          }
        },
        pingAPI: async () => {
          const started = Date.now();
          try {
            const resp = await globalThis.fetch(`${apiUrl}/api/v1/health`, {
              signal: AbortSignal.timeout(5000),
            });
            const latencyMs = Date.now() - started;
            if (resp.ok) return { ok: true, latencyMs };
            return { ok: false, error: `HTTP ${resp.status}`, latencyMs };
          } catch (err) {
            return { ok: false, error: err instanceof Error ? err.message : 'unknown' };
          }
        },
        readProjectConfig: () => {
          const cwd = process.cwd();
          const gitRoot = findRepoRoot(cwd);
          try {
            const result = findLoopZeroYaml(cwd, gitRoot);
            if (!result) {
              if (existsSync(resolve(cwd, 'loopzero.yaml'))) {
                return { exists: true, valid: false, error: 'loopzero.yaml present but could not be parsed' };
              }
              return { exists: false, valid: false };
            }
            return { exists: true, valid: true };
          } catch (err) {
            return { exists: true, valid: false, error: err instanceof Error ? err.message : 'parse error' };
          }
        },
        getCliVersion,
        writeTo,
        writeError,
        isCI,
      });

      const exitCode = await doctorCmd.execute({ json: globalOpts.json });
      process.exitCode = exitCode;
    });
}
