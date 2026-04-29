// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiClient, ApiError } from '../api/client';
import { LoginCommand } from '../commands/login';
import { LogoutCommand } from '../commands/logout';
import { WhoamiCommand } from '../commands/whoami';
import { executeDeviceCodeFlow } from '../auth/device-flow';
import { openBrowser } from '../auth/open-browser';
import { promptConfirm } from '../output/prompt-confirm';
import { CommandDeps } from './types';

export function registerAuthCommands(deps: CommandDeps): void {
  const { program, configStore, isCI, writeTo, writeError, getGlobalOpts, apiUrl, cloudUrl } = deps;

  // --- login ---
  program
    .command('login')
    .description('Authenticate with your account')
    .option('--token <token>', 'Authentication token')
    .option('--team <team>', 'Select team')
    .action(async (cmdOpts) => {
      const globalOpts = getGlobalOpts();

      const loginCmd = new LoginCommand({
        saveToken: (token, email, team, refreshToken?, expiresIn?) =>
          configStore.saveToken(token, email, team, refreshToken, expiresIn),
        executeDeviceFlow: () => executeDeviceCodeFlow({
          fetchFn: globalThis.fetch,
          baseURL: cloudUrl,
          openBrowser,
          writeTo,
          writeError,
          sleep: (ms: number) => new Promise(r => setTimeout(r, ms)),
        }),
        validateToken: async (token: string) => {
          // Try cloud service /v1/auth/me first (Hydra JWTs from platform
          // sign-up). Falls back to engine API /api/v1/auth/whoami for
          // service tokens issued internally.
          try {
            const cloudResp = await globalThis.fetch(`${cloudUrl}/v1/auth/me`, {
              headers: { Authorization: `Bearer ${token}` },
              signal: AbortSignal.timeout(5000),
            });
            if (cloudResp.ok) {
              const me = await cloudResp.json() as { email?: string; name?: string; team?: string; user_id?: string };
              if (me.email) {
                return { valid: true, email: me.email, teams: me.team ? [me.team] : ['default'] };
              }
            }
          } catch { /* cloud service unavailable -- try engine API */ }

          try {
            const client = new ApiClient({
              baseURL: apiUrl,
              token,
            });
            const result = await client.get<{
              user_id: string;
              email: string;
              team: string;
              role: string;
            }>('/api/v1/auth/whoami');
            const username = result.user_id.replace(/^github-/, '');
            const displayName = result.email || `${username}@users.noreply.github.com`;
            return {
              valid: true,
              email: displayName,
              teams: [result.team],
            };
          } catch (err) {
            const errMsg = (err as Error).message || '';
            if (errMsg.includes('ECONNREFUSED') || errMsg.includes('ETIMEDOUT') ||
                errMsg.includes('ENOTFOUND') || errMsg.includes('fetch failed')) {
              return { valid: false, email: null, teams: [], serverError: true };
            }
            const apiErr = err as { statusCode?: number };
            if (apiErr.statusCode !== undefined && apiErr.statusCode >= 500) {
              return { valid: false, email: null, teams: [], serverError: true };
            }
            return { valid: false, email: null, teams: [] };
          }
        },
        writeTo,
        writeError,
        isCI,
      });

      const exitCode = await loginCmd.execute({
        token: cmdOpts.token,
        team: cmdOpts.team,
        json: globalOpts.json,
      });
      process.exitCode = exitCode;
    });

  // --- logout ---
  program
    .command('logout')
    .description('Remove stored credentials')
    .option('-y, --yes', 'Skip confirmation prompt')
    .action(async (cmdOpts) => {
      const globalOpts = getGlobalOpts();

      const logoutCmd = new LogoutCommand({
        clearToken: () => configStore.clearToken(),
        // F-06 (CHG-2026-04-29-DOGFOOD T1.2) — passar checagem de sessão
        // para que LogoutCommand distinga "cancelled" de "no session".
        hasToken: () => Boolean(configStore.getToken()),
        writeTo,
        // CHG-2026-04-23-AUDIT-9: shared promptConfirm with non-TTY safety.
        confirm: (message: string) => promptConfirm(message + ' '),
        isCI: process.env.CI === 'true',
      });

      const exitCode = await logoutCmd.execute({
        json: globalOpts.json,
        yes: cmdOpts.yes,
      });
      process.exitCode = exitCode;
    });

  // --- whoami ---
  program
    .command('whoami')
    .description('Show current authenticated user')
    .action(async () => {
      const globalOpts = getGlobalOpts();

      const whoamiCmd = new WhoamiCommand({
        getToken: () => configStore.getToken(),
        getEmail: () => configStore.getEmail(),
        getTeam: () => configStore.getTeam(),
        verifyToken: async (token: string) => {
          const client = new ApiClient({ baseURL: apiUrl, token });
          try {
            // AUDIT-10: forward username so the command can fall back when
            // email is empty (service-account tokens).
            const result = await client.get<{ email: string; username?: string; team: string }>('/api/v1/auth/whoami');
            return { email: result.email, username: result.username, team: result.team };
          } catch (err) {
            // CHG-2026-04-23-AUDIT-22: only collapse to null for genuine
            // auth failures (401/403). Network / DNS / timeout / 5xx are
            // re-thrown so whoami can say "cannot reach API" instead of
            // the misleading "token expired". Prior `catch { return null }`
            // made any connection error look like a credential problem.
            if (err instanceof ApiError && (err.statusCode === 401 || err.statusCode === 403)) {
              return null;
            }
            throw err;
          }
        },
        writeTo,
        writeError,
      });

      const exitCode = await whoamiCmd.execute({
        json: globalOpts.json,
      });
      process.exitCode = exitCode;
    });
}
