// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiClient } from '../api/client';
import { DomainAddCommand, DomainVerifyCommand, DomainStatusCommand, DomainListCommand, DomainRemoveCommand, DomainDNSCommand } from '../commands/domain';
import type { DomainDeps } from '../commands/domain';
import { API_REQUEST_TIMEOUT_MS } from '../config/constants';
import { CommandDeps } from './types';

/** Build the standard domain deps object shared across all domain subcommands. */
function makeDomainDeps(apiClient: ApiClient, writeTo: (m: string) => void, writeError: (m: string) => void, getToken: () => string | null): DomainDeps {
  return {
    getToken,
    createDomain: async (body) => apiClient.post('/api/v1/domains', body, { 'Idempotency-Key': 'domain-' + body.domain + '-' + Date.now() }) as any,
    verifyDomain: async (d) => apiClient.post(`/api/v1/domains/${encodeURIComponent(d)}/verify`, {}) as any,
    getDomain: async (d) => apiClient.get(`/api/v1/domains/${encodeURIComponent(d)}`) as any,
    listDomains: async (params) => {
      if (params && (params.project || params.app || params.environment)) {
        const qs = new URLSearchParams();
        if (params.project) qs.set('project', params.project);
        if (params.app) qs.set('app', params.app);
        if (params.environment) qs.set('environment', params.environment);
        return apiClient.get(`/api/v1/domains?${qs.toString()}`) as any;
      }
      return apiClient.get('/api/v1/domains') as any;
    },
    deleteDomain: async (d) => apiClient.del(`/api/v1/domains/${encodeURIComponent(d)}`) as any,
    writeTo,
    writeError,
  };
}

export function registerDomainCommands(deps: CommandDeps): void {
  const { program, configStore, writeTo, writeError, getGlobalOpts, apiUrl, handleUnauthorized, withTimeout } = deps;
  const getToken = () => configStore.getToken();

  const domainCmd = program
    .command('domain')
    .description('Manage custom domains');

  domainCmd
    .command('add <domain>')
    .description('Register a custom domain for an app')
    .requiredOption('--project <name>', 'Project name')
    .requiredOption('--app <name>', 'App name')
    .requiredOption('--env <name>', 'Environment name')
    .action(async (domain: string, opts: { project: string; app: string; env: string }) => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: getToken() ?? undefined, onUnauthorized: handleUnauthorized });
      const cmd = new DomainAddCommand(makeDomainDeps(apiClient, writeTo, writeError, getToken));

      process.exitCode = await withTimeout(
        cmd.execute(domain, { ...opts, json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Domain add timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) { writeTo(JSON.stringify({ code: 'DOMAIN_ADD_TIMEOUT', error: err.message, exit_code: 3 })); }
        else { writeError(err.message); }
        return 3;
      });
    });

  domainCmd
    .command('verify <domain>')
    .description('Verify DNS records for a custom domain')
    .action(async (domain: string) => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: getToken() ?? undefined, onUnauthorized: handleUnauthorized });
      const cmd = new DomainVerifyCommand(makeDomainDeps(apiClient, writeTo, writeError, getToken));

      process.exitCode = await withTimeout(
        cmd.execute(domain, { json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Domain verify timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) { writeTo(JSON.stringify({ code: 'DOMAIN_VERIFY_TIMEOUT', error: err.message, exit_code: 3 })); }
        else { writeError(err.message); }
        return 3;
      });
    });

  domainCmd
    .command('status <domain>')
    .description('Show custom domain status and details')
    .action(async (domain: string) => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: getToken() ?? undefined, onUnauthorized: handleUnauthorized });
      const cmd = new DomainStatusCommand(makeDomainDeps(apiClient, writeTo, writeError, getToken));

      process.exitCode = await withTimeout(
        cmd.execute(domain, { json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Domain status timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) { writeTo(JSON.stringify({ code: 'DOMAIN_STATUS_TIMEOUT', error: err.message, exit_code: 3 })); }
        else { writeError(err.message); }
        return 3;
      });
    });

  domainCmd
    .command('list')
    .description('List custom domains')
    .option('--project <name>', 'Filter by project')
    .option('--app <name>', 'Filter by app')
    .option('--env <name>', 'Filter by environment')
    .action(async (opts: { project?: string; app?: string; env?: string }) => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: getToken() ?? undefined, onUnauthorized: handleUnauthorized });
      const cmd = new DomainListCommand(makeDomainDeps(apiClient, writeTo, writeError, getToken));

      process.exitCode = await withTimeout(
        cmd.execute({ ...opts, json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Domain list timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) { writeTo(JSON.stringify({ code: 'DOMAIN_LIST_TIMEOUT', error: err.message, exit_code: 3 })); }
        else { writeError(err.message); }
        return 3;
      });
    });

  domainCmd
    .command('remove <domain>')
    .description('Remove a custom domain')
    .option('--yes', 'Skip confirmation prompt')
    .action(async (domain: string, opts: { yes?: boolean }) => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: getToken() ?? undefined, onUnauthorized: handleUnauthorized });
      const cmd = new DomainRemoveCommand(makeDomainDeps(apiClient, writeTo, writeError, getToken));

      process.exitCode = await withTimeout(
        cmd.execute(domain, { ...opts, json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Domain remove timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) { writeTo(JSON.stringify({ code: 'DOMAIN_REMOVE_TIMEOUT', error: err.message, exit_code: 3 })); }
        else { writeError(err.message); }
        return 3;
      });
    });

  domainCmd
    .command('dns <domain>')
    .description('List DNS records for a Cloudflare-managed domain')
    .action(async (domain: string) => {
      const globalOpts = getGlobalOpts();
      const apiClient = new ApiClient({ baseURL: apiUrl, token: getToken() ?? undefined, onUnauthorized: handleUnauthorized });

      const cmd = new DomainDNSCommand({
        ...makeDomainDeps(apiClient, writeTo, writeError, getToken),
        listDNSRecords: async (d: string) => apiClient.get(`/api/v1/domains/${encodeURIComponent(d)}/dns-records`),
      });

      process.exitCode = await withTimeout(
        cmd.execute(domain, { json: globalOpts.json }),
        API_REQUEST_TIMEOUT_MS + 10_000,
        'Domain DNS lookup timed out. Check your connection and try again.',
      ).catch((err: Error) => {
        if (globalOpts.json) { writeTo(JSON.stringify({ code: 'DOMAIN_DNS_TIMEOUT', error: err.message, exit_code: 3 })); }
        else { writeError(err.message); }
        return 3;
      });
    });
}
