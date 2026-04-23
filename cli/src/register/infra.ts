// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as childProcess from 'child_process';
import { ApiClient } from '../api/client';
import { BuildListCommand } from '../commands/build-list';
import { BuildInspectCommand } from '../commands/build-inspect';
import { SecretSetCommand, SecretListCommand, SecretDeleteCommand } from '../commands/secret';
import { BillingCommand } from '../commands/billing';
import { ConnectCommand, DisconnectCommand } from '../commands/connect';
import { runAdminEvict, AdminEvictInput, AdminEvictResponse } from '../commands/admin-evict';
import { API_REQUEST_TIMEOUT_MS } from '../config/constants';
import { getGitRoot } from '../adapters/git';
import { findLoopZeroYaml } from '../adapters/loopzero-yaml';
import { openBrowser } from '../auth/open-browser';
import { CommandDeps } from './types';

export function registerInfraCommands(deps: CommandDeps): void {
  const { program, configStore, isCI, writeTo, writeError, getGlobalOpts, apiUrl, cloudUrl, handleUnauthorized, withTimeout } = deps;

  // --- build ---
  const buildCmd = program
    .command('build')
    .description('Manage builds');

  buildCmd
    .command('list')
    .description('List recent builds')
    .option('--env <name>', 'Filter by environment')
    .option('--limit <n>', 'Max builds to show')
    .action(async (cmdOpts: { env?: string; limit?: string }) => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const buildListCmd = new BuildListCommand({
        getToken: () => configStore.getToken(),
        findLoopZeroYaml: () => {
          const gitRoot = getGitRoot() ?? process.cwd();
          return findLoopZeroYaml(process.cwd(), gitRoot);
        },
        listBuilds: async (project: string, env?: string, limit?: number) => {
          const params = new URLSearchParams();
          if (env) params.set('env', env);
          if (limit) params.set('limit', String(limit));
          const query = params.toString();
          const path = `/api/v1/projects/${encodeURIComponent(project)}/builds${query ? `?${query}` : ''}`;
          return apiClient.get(path);
        },
        writeTo,
        writeError,
      });

      const limit = cmdOpts.limit ? parseInt(cmdOpts.limit, 10) : undefined;
      const exitCode = await withTimeout(
        buildListCmd.execute({ env: cmdOpts.env, limit, json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Build list request timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'BUILD_LIST_TIMEOUT', error: err.message, exit_code: 1 }));
        } else {
          writeError(err.message);
        }
        return 1;
      });
      process.exitCode = exitCode;
    });

  buildCmd
    .command('inspect <build-id>')
    .description('Show build details')
    .action(async (buildId: string) => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const buildInspectCmd = new BuildInspectCommand({
        getToken: () => configStore.getToken(),
        getBuild: async (id: string) => {
          return apiClient.get(`/api/v1/builds/${encodeURIComponent(id)}`);
        },
        writeTo,
        writeError,
      });

      const exitCode = await withTimeout(
        buildInspectCmd.execute(buildId, { json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Build inspect request timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'BUILD_INSPECT_TIMEOUT', error: err.message, exit_code: 1 }));
        } else {
          writeError(err.message);
        }
        return 1;
      });
      process.exitCode = exitCode;
    });

  // --- secret ---
  const secretCmd = program
    .command('secret')
    .description('Manage application secrets');

  secretCmd
    .command('set <nameValue>')
    .description('Set a secret (format: NAME=value)')
    .option('--env <name>', 'Target environment', 'production')
    .option('--app <name>', 'Target app')
    .action(async (nameValue: string, cmdOpts: { env?: string; app?: string }) => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const cmd = new SecretSetCommand({
        getToken: () => configStore.getToken(),
        findLoopZeroYaml: () => {
          const gitRoot = getGitRoot() ?? process.cwd();
          return findLoopZeroYaml(process.cwd(), gitRoot);
        },
        setSecret: async (project: string, name: string, value: string, env: string, app: string) => {
          return apiClient.post<{ name: string; environment: string; app: string; created: boolean }>(`/api/v1/secrets?project=${encodeURIComponent(project)}`, {
            name, value, environment: env, app,
          });
        },
        listSecrets: async () => ({ secrets: [] as Array<{ name: string; environment: string; app: string }> }),
        deleteSecret: async () => ({ name: '', deleted: false }),
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.execute({ nameValue, env: cmdOpts.env, app: cmdOpts.app, json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Secret set timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'SECRET_TIMEOUT', error: err.message, exit_code: 3 }));
        } else {
          writeError(err.message);
        }
        return 3;
      });
    });

  secretCmd
    .command('list')
    .description('List secret names (never values)')
    .option('--env <name>', 'Target environment', 'production')
    .action(async (cmdOpts: { env?: string }) => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const cmd = new SecretListCommand({
        getToken: () => configStore.getToken(),
        findLoopZeroYaml: () => {
          const gitRoot = getGitRoot() ?? process.cwd();
          return findLoopZeroYaml(process.cwd(), gitRoot);
        },
        setSecret: async () => ({ name: '', environment: '', app: '', created: false }),
        listSecrets: async (project: string, env: string) => {
          return apiClient.get<{ secrets: Array<{ name: string; environment: string; app: string }> }>(`/api/v1/secrets?project=${encodeURIComponent(project)}&env=${encodeURIComponent(env)}`);
        },
        deleteSecret: async () => ({ name: '', deleted: false }),
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.execute({ env: cmdOpts.env, json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Secret list timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'SECRET_TIMEOUT', error: err.message, exit_code: 3 }));
        } else {
          writeError(err.message);
        }
        return 3;
      });
    });

  secretCmd
    .command('delete <name>')
    .description('Delete a secret')
    .option('--env <name>', 'Target environment', 'production')
    .option('--app <name>', 'Target app')
    .option('--yes', 'Skip confirmation')
    .action(async (name: string, cmdOpts: { env?: string; app?: string; yes?: boolean }) => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const yamlResult = (() => {
        const gitRoot = getGitRoot() ?? process.cwd();
        return findLoopZeroYaml(process.cwd(), gitRoot);
      })();

      const project = yamlResult?.content?.project ?? '';

      const cmd = new SecretDeleteCommand({
        getToken: () => configStore.getToken(),
        findLoopZeroYaml: () => yamlResult,
        setSecret: async () => ({ name: '', environment: '', app: '', created: false }),
        listSecrets: async () => ({ secrets: [] as Array<{ name: string; environment: string; app: string }> }),
        deleteSecret: async (_proj: string, secretName: string, env: string, app: string) => {
          return apiClient.del<{ name: string; deleted: boolean }>(
            `/api/v1/secrets/${encodeURIComponent(secretName)}?project=${encodeURIComponent(project)}&environment=${encodeURIComponent(env)}&app=${encodeURIComponent(app)}`,
          );
        },
        confirm: async (message: string) => {
          const readline = await import('readline');
          const rl = readline.createInterface({ input: process.stdin, output: process.stdout });
          return new Promise<boolean>((resolve) => {
            rl.question(message, (answer) => {
              rl.close();
              resolve(answer.toLowerCase() === 'y' || answer.toLowerCase() === 'yes');
            });
          });
        },
        writeTo,
        writeError,
        isCI,
      });

      process.exitCode = await withTimeout(
        cmd.execute({ name, env: cmdOpts.env, app: cmdOpts.app, yes: cmdOpts.yes, json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Secret delete timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) {
          writeTo(JSON.stringify({ code: 'SECRET_TIMEOUT', error: err.message, exit_code: 3 }));
        } else {
          writeError(err.message);
        }
        return 3;
      });
    });

  // --- connect (GitHub App auto-deploy) ---
  program
    .command('connect')
    .description('Connect a GitHub repository for auto-deploy on push')
    .option('--repo <owner/repo>', 'GitHub repository (auto-detected from git remote)')
    .option('--branch <branch>', 'Default branch for production deploys', 'main')
    .option('--installation-id <id>', 'GitHub App installation ID')
    .action(async (opts: { repo?: string; branch?: string; installationId?: string }) => {
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const cmd = new ConnectCommand({
        getToken: () => configStore.getToken(),
        getGitRemote: () => {
          try {
            return childProcess.execSync('git remote get-url origin', { encoding: 'utf-8' }).trim();
          } catch { return null; }
        },
        getProjectName: () => {
          const yamlResult = findLoopZeroYaml(process.cwd(), process.cwd());
          return yamlResult?.content?.project ?? null;
        },
        connectRepo: async (project, body) => apiClient.post(`/api/v1/projects/${encodeURIComponent(project)}/connect`, body),
        disconnectRepo: async (project) => apiClient.del(`/api/v1/projects/${encodeURIComponent(project)}/connect`),
        getConnection: async (project) => apiClient.get<any>(`/api/v1/projects/${encodeURIComponent(project)}/connect`).catch(() => null),
        writeTo,
        writeError,
      });

      process.exitCode = await cmd.execute({
        repo: opts.repo,
        branch: opts.branch,
        installationId: opts.installationId ? parseInt(opts.installationId, 10) : undefined,
        json: getGlobalOpts().json,
      });
    });

  program
    .command('disconnect')
    .description('Disconnect GitHub repository (stop auto-deploy)')
    .action(async () => {
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const cmd = new DisconnectCommand({
        getToken: () => configStore.getToken(),
        getGitRemote: () => null,
        getProjectName: () => {
          const yamlResult = findLoopZeroYaml(process.cwd(), process.cwd());
          return yamlResult?.content?.project ?? null;
        },
        connectRepo: async () => { throw new Error('not used'); },
        disconnectRepo: async (project) => apiClient.del(`/api/v1/projects/${encodeURIComponent(project)}/connect`),
        getConnection: async () => null,
        writeTo,
        writeError,
      });

      process.exitCode = await cmd.execute({ json: getGlobalOpts().json });
    });

  // --- billing ---
  const billingCmd = program
    .command('billing')
    .description('View plan and usage, or open the billing portal');

  billingCmd
    .command('portal')
    .description('Open the billing portal in your browser')
    .action(async () => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: cloudUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const cmd = new BillingCommand({
        getBilling: async () => apiClient.get('/v1/billing'),
        getPortalURL: async () => apiClient.get('/v1/billing/portal'),
        openBrowser,
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.execute({ json: globalOpts.json, subcommand: 'portal' }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Billing portal request timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) { writeTo(JSON.stringify({ code: 'BILLING_TIMEOUT', error: err.message, exit_code: 3 })); }
        else { writeError(err.message); }
        return 3;
      });
    });

  billingCmd
    .action(async () => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: cloudUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const cmd = new BillingCommand({
        getBilling: async () => apiClient.get('/v1/billing'),
        getPortalURL: async () => apiClient.get('/v1/billing/portal'),
        openBrowser,
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.execute({ json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Billing request timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) { writeTo(JSON.stringify({ code: 'BILLING_TIMEOUT', error: err.message, exit_code: 3 })); }
        else { writeError(err.message); }
        return 3;
      });
    });

  // --- audit ---
  program
    .command('audit')
    .description('View audit log (requires admin or owner role)')
    .option('--action <action>', 'Filter by action (e.g., project.created)')
    .option('--since <date>', 'Filter by date (e.g., 2026-04-01)')
    .action(async (cmdOpts: { action?: string; since?: string }) => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: configStore.getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const { AuditCommand } = await import('../commands/audit');
      const cmd = new AuditCommand({
        getToken: () => configStore.getToken(),
        getAudit: async (action?: string, since?: string) => {
          const params = new URLSearchParams();
          if (action) params.set('action', action);
          if (since) params.set('since', since);
          params.set('limit', '20');
          return apiClient.get(`/api/v1/audit?${params.toString()}`);
        },
        writeTo,
        writeError,
      });

      process.exitCode = await withTimeout(
        cmd.run({
          json: globalOpts.json,
          action: cmdOpts.action,
          since: cmdOpts.since,
        }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Audit log request timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) { writeTo(JSON.stringify({ code: 'AUDIT_TIMEOUT', error: err.message, exit_code: 3 })); }
        else { writeError(err.message); }
        return 3;
      });
    });

  // --- admin (operator-only; gated by LOOPZERO_SERVICE_AUTH) ---
  const adminCmd = program
    .command('admin')
    .description('Operator-only commands (require LOOPZERO_SERVICE_AUTH)');
  const adminTenantsCmd = adminCmd
    .command('tenants')
    .description('Bulk tenant operations');
  adminTenantsCmd
    .command('evict')
    .description('Bulk tenant eviction — dry-run by default unless --yes')
    .option('--pattern <sqlLike>', "SQL LIKE pattern against tenants.id (e.g., 'tn-lt-%')")
    .option('--ids <csv>', 'Comma-separated tenant IDs (alternative to --pattern)')
    .option('--dry-run', 'Show matches without evicting')
    .option('--yes', 'Skip confirmation prompt')
    .action(async (cmdOpts: { pattern?: string; ids?: string; dryRun?: boolean; yes?: boolean }) => {
      const globalOpts = getGlobalOpts();
      const serviceAuth = process.env.LOOPZERO_SERVICE_AUTH ?? '';

      const adminPost = async (input: AdminEvictInput): Promise<AdminEvictResponse> => {
        const res = await fetch(`${apiUrl}/api/v1/admin/tenants/evict`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'X-Service-Auth': serviceAuth,
          },
          body: JSON.stringify(input),
        });
        if (!res.ok) {
          const body = await res.text();
          throw new Error(`admin evict HTTP ${res.status}: ${body}`);
        }
        return (await res.json()) as AdminEvictResponse;
      };

      const { createInterface } = await import('readline');
      const ask = (question: string): Promise<string> =>
        new Promise((resolve) => {
          const rl = createInterface({ input: process.stdin, output: process.stdout });
          rl.question(question, (answer) => {
            rl.close();
            resolve(answer);
          });
        });

      const exitCode = await runAdminEvict(
        {
          pattern: cmdOpts.pattern,
          ids: cmdOpts.ids,
          dryRun: cmdOpts.dryRun,
          yes: cmdOpts.yes,
          json: globalOpts.json,
        },
        {
          getServiceAuth: () => (serviceAuth ? serviceAuth : null),
          postEvict: adminPost,
          writeTo,
          writeError,
          prompt: ask,
        },
      );
      process.exitCode = exitCode;
    });
}
