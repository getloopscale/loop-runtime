// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Custom Domain commands: add, verify, status, list, remove.
// MEET-2026-04-05-033: Custom Domains feature.

import { ApiError } from '../api/client';

// --- API Response types ---

interface DomainCreateResponse {
  id: string;
  domain: string;
  project: string;
  app: string;
  environment: string;
  status: string;
  // v1 fields (TXT verification)
  verification?: {
    txt_name: string;
    txt_value: string;
  };
  cname_target?: string;
  // v2 fields (CF Zone Management)
  nameservers?: string[];
  dns_records_imported?: number;
  dns_records?: Array<{ type: string; name: string; content: string }>;
  action_required?: string;
}

interface DomainVerifyResponse {
  domain: string;
  status: string;
  verified_at?: string;
  verification_error?: string;
  hint?: string;
  message?: string;
}

interface DomainStatusResponse {
  id: string;
  domain: string;
  project: string;
  app: string;
  environment: string;
  status: string;
  tls?: { status: string; expires_at?: string };
  verification?: { txt_name: string; txt_value: string };
  cname_target?: string;
  nameservers?: string[];
  cf_managed?: boolean;
  created_at: string;
  verified_at?: string;
}

interface DomainListResponse {
  domains: DomainStatusResponse[];
  total: number;
  limit: number;
  offset: number;
}

// --- Shared deps ---

interface DNSRecordResponse {
  id: string;
  type: string;
  name: string;
  content: string;
  ttl?: number;
  priority?: number;
  proxied?: boolean;
}

interface DNSListResponse {
  domain: string;
  records: DNSRecordResponse[];
  total: number;
}

export interface DomainDeps {
  getToken: () => string | null;
  createDomain: (body: { domain: string; project: string; app: string; environment: string }) => Promise<DomainCreateResponse>;
  verifyDomain: (domain: string) => Promise<DomainVerifyResponse>;
  getDomain: (domain: string) => Promise<DomainStatusResponse>;
  listDomains: (params: { project?: string; app?: string; environment?: string }) => Promise<DomainListResponse>;
  deleteDomain: (domain: string) => Promise<{ ok: boolean; domain: string; status: string }>;
  listDNSRecords?: (domain: string) => Promise<DNSListResponse>;
  addDNSRecords?: (domain: string, records: Array<{ type: string; name: string; content: string }>) => Promise<{ ok: boolean; created: number }>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

// --- Shared helpers ---

function exitWithError(
  deps: Pick<DomainDeps, 'writeTo' | 'writeError'>,
  json: boolean | undefined,
  message: string,
  exitCode: number,
  code?: string,
): number {
  if (json) {
    const payload: Record<string, unknown> = { error: message, exit_code: exitCode };
    if (code) payload.code = code;
    deps.writeTo(JSON.stringify(payload));
  } else {
    deps.writeError(message);
  }
  return exitCode;
}

function handleApiError(
  deps: Pick<DomainDeps, 'writeTo' | 'writeError'>,
  json: boolean | undefined,
  err: unknown,
  action: string,
): number {
  if (err instanceof ApiError) {
    if (err.exitCode === 4 || err.statusCode === 401) {
      return exitWithError(deps, json, 'Authentication failed. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }
    return exitWithError(deps, json, `${action}: ${err.message}`, 3, err.code);
  }
  if (err instanceof Error) {
    return exitWithError(deps, json, `${action}: ${err.message}`, 3, 'UNEXPECTED_ERROR');
  }
  return exitWithError(deps, json, `${action}. Try again later.`, 3, 'UNEXPECTED_ERROR');
}

// ============================================================
// DomainAddCommand
// ============================================================

export interface DomainAddOptions {
  project: string;
  app: string;
  env: string;
  json?: boolean;
}

export class DomainAddCommand {
  constructor(private readonly deps: DomainDeps) {}

  async execute(domain: string, opts: DomainAddOptions): Promise<number> {
    if (!this.deps.getToken()) {
      return exitWithError(this.deps, opts.json, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    if (!domain || domain.trim() === '') {
      return exitWithError(this.deps, opts.json, 'Domain name is required. Usage: loopzero domain add <domain> --project <p> --app <a> --env <e>', 1, 'MISSING_ARGUMENT');
    }
    if (!opts.project) {
      return exitWithError(this.deps, opts.json, '--project is required', 1, 'MISSING_ARGUMENT');
    }
    if (!opts.app) {
      return exitWithError(this.deps, opts.json, '--app is required', 1, 'MISSING_ARGUMENT');
    }
    if (!opts.env) {
      return exitWithError(this.deps, opts.json, '--env is required', 1, 'MISSING_ARGUMENT');
    }

    try {
      const result = await this.deps.createDomain({
        domain: domain.toLowerCase().trim(),
        project: opts.project,
        app: opts.app,
        environment: opts.env,
      });

      if (opts.json) {
        this.deps.writeTo(JSON.stringify(result));
        return 0;
      }

      // v2: CF Zone Management — show nameservers
      if (result.nameservers && result.nameservers.length > 0) {
        this.deps.writeTo(`✓ Domain registered\n`);
        if (result.dns_records_imported && result.dns_records_imported > 0) {
          this.deps.writeTo(`  ${result.dns_records_imported} DNS records imported (email, DKIM, etc.)\n`);
        }
        this.deps.writeTo(`  To activate, update your nameservers at your DNS provider:\n`);
        this.deps.writeTo(`  ┌─────────────────────────────────────────────┐`);
        for (const ns of result.nameservers) {
          this.deps.writeTo(`  │  ${ns.padEnd(42)} │`);
        }
        this.deps.writeTo(`  └─────────────────────────────────────────────┘\n`);
        this.deps.writeTo(`  Where to find this:`);
        this.deps.writeTo(`    GoDaddy:    Domains → DNS → Nameservers → Change`);
        this.deps.writeTo(`    Namecheap:  Domain List → Manage → Nameservers → Custom DNS`);
        this.deps.writeTo(`    Hostinger:  Domains → DNS/Nameservers → Change Nameservers\n`);
        this.deps.writeTo(`  After changing, run: loopzero domain status ${domain}`);
        return 0;
      }

      // v1: TXT verification — show DNS records to add
      this.deps.writeTo(`✓ Domain registered\n`);
      this.deps.writeTo(`  To activate, add these DNS records:\n`);
      this.deps.writeTo(`  Type   Name                                     Value`);
      this.deps.writeTo(`  ────── ──────────────────────────────────────── ──────────────────────────`);
      if (result.cname_target) {
        this.deps.writeTo(`  CNAME  ${domain.padEnd(40)} ${result.cname_target}`);
      }
      if (result.verification) {
        this.deps.writeTo(`  TXT    ${result.verification.txt_name.padEnd(40)} ${result.verification.txt_value}\n`);
      }
      this.deps.writeTo(`  Then run: loopzero domain verify ${domain}`);
      return 0;
    } catch (err) {
      if (err instanceof ApiError && err.statusCode === 409) {
        return exitWithError(this.deps, opts.json, `Domain ${domain} is already registered.`, 3, 'DOMAIN_ALREADY_EXISTS');
      }
      return handleApiError(this.deps, opts.json, err, 'Failed to register domain');
    }
  }
}

// ============================================================
// DomainVerifyCommand
// ============================================================

export interface DomainVerifyOptions {
  json?: boolean;
}

export class DomainVerifyCommand {
  constructor(private readonly deps: DomainDeps) {}

  async execute(domain: string, opts: DomainVerifyOptions = {}): Promise<number> {
    if (!this.deps.getToken()) {
      return exitWithError(this.deps, opts.json, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }
    if (!domain || domain.trim() === '') {
      return exitWithError(this.deps, opts.json, 'Domain name is required. Usage: loopzero domain verify <domain>', 1, 'MISSING_ARGUMENT');
    }

    try {
      const result = await this.deps.verifyDomain(domain.toLowerCase().trim());

      if (opts.json) {
        this.deps.writeTo(JSON.stringify(result));
        return 0;
      }

      if (result.status === 'verified' || result.status === 'provisioning') {
        this.deps.writeTo(`✓ DNS verified`);
        this.deps.writeTo(`⟳ TLS certificate being provisioned...\n`);
        this.deps.writeTo(`  Check progress: loopzero domain status ${domain}`);
        return 0;
      }

      if (result.status === 'active') {
        this.deps.writeTo(`✓ Domain active — https://${domain}`);
        return 0;
      }

      // Still pending
      this.deps.writeError(`✗ DNS verification pending\n`);
      if (result.verification_error) {
        this.deps.writeError(`  ${result.verification_error}`);
      }
      if (result.hint) {
        this.deps.writeError(`  ${result.hint}`);
      }
      return 0; // not an error — just not ready yet
    } catch (err) {
      return handleApiError(this.deps, opts.json, err, 'Failed to verify domain');
    }
  }
}

// ============================================================
// DomainStatusCommand
// ============================================================

export interface DomainStatusOptions {
  json?: boolean;
}

export class DomainStatusCommand {
  constructor(private readonly deps: DomainDeps) {}

  async execute(domain: string, opts: DomainStatusOptions = {}): Promise<number> {
    if (!this.deps.getToken()) {
      return exitWithError(this.deps, opts.json, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }
    if (!domain || domain.trim() === '') {
      return exitWithError(this.deps, opts.json, 'Domain name is required. Usage: loopzero domain status <domain>', 1, 'MISSING_ARGUMENT');
    }

    try {
      const result = await this.deps.getDomain(domain.toLowerCase().trim());

      if (opts.json) {
        this.deps.writeTo(JSON.stringify(result));
        return 0;
      }

      this.deps.writeTo(`  Domain:      ${result.domain}`);
      this.deps.writeTo(`  Project:     ${result.project}`);
      this.deps.writeTo(`  App:         ${result.app}`);
      this.deps.writeTo(`  Environment: ${result.environment}`);

      const statusIcon = result.status === 'active' ? '✓' : result.status === 'failed' ? '✗' : '⠋';
      this.deps.writeTo(`  Status:      ${statusIcon} ${result.status}`);

      if (result.cf_managed) {
        this.deps.writeTo(`  DNS:         Cloudflare (WAF/DDoS active)`);
        this.deps.writeTo(`  TLS:         ✓ Cloudflare Universal SSL`);
      } else if (result.tls) {
        const tlsIcon = result.tls.status === 'issued' ? '✓' : '✗';
        const expires = result.tls.expires_at ? ` (expires ${result.tls.expires_at.substring(0, 10)})` : '';
        this.deps.writeTo(`  TLS:         ${tlsIcon} ${result.tls.status}${expires}`);
      }

      if (result.nameservers && result.nameservers.length > 0) {
        this.deps.writeTo(`  Nameservers: ${result.nameservers.join(', ')}`);
      }

      if (result.status === 'pending_ns') {
        this.deps.writeTo(`\n  Waiting for nameserver propagation (usually 5-30 minutes).`);
      }

      if (result.status === 'active') {
        this.deps.writeTo(`\n  https://${result.domain}`);
      }

      return 0;
    } catch (err) {
      if (err instanceof ApiError && err.statusCode === 404) {
        return exitWithError(this.deps, opts.json, `Domain ${domain} not found.`, 1, 'NOT_FOUND');
      }
      return handleApiError(this.deps, opts.json, err, 'Failed to get domain status');
    }
  }
}

// ============================================================
// DomainListCommand
// ============================================================

export interface DomainListOptions {
  project?: string;
  app?: string;
  env?: string;
  json?: boolean;
}

export class DomainListCommand {
  constructor(private readonly deps: DomainDeps) {}

  async execute(opts: DomainListOptions = {}): Promise<number> {
    if (!this.deps.getToken()) {
      return exitWithError(this.deps, opts.json, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    try {
      const result = await this.deps.listDomains({
        project: opts.project,
        app: opts.app,
        environment: opts.env,
      });

      if (opts.json) {
        this.deps.writeTo(JSON.stringify(result));
        return 0;
      }

      if (result.domains.length === 0) {
        this.deps.writeTo('No custom domains configured.');
        return 0;
      }

      this.deps.writeTo(`  ${'DOMAIN'.padEnd(30)} ${'APP'.padEnd(10)} ${'ENV'.padEnd(15)} ${'STATUS'.padEnd(12)} TLS`);
      for (const d of result.domains) {
        const tlsInfo = d.tls?.status === 'issued'
          ? `✓ valid${d.tls.expires_at ? ` (expires ${d.tls.expires_at.substring(0, 10)})` : ''}`
          : '✗ pending';
        this.deps.writeTo(
          `  ${d.domain.padEnd(30)} ${d.app.padEnd(10)} ${d.environment.padEnd(15)} ${d.status.padEnd(12)} ${tlsInfo}`
        );
      }
      return 0;
    } catch (err) {
      return handleApiError(this.deps, opts.json, err, 'Failed to list domains');
    }
  }
}

// ============================================================
// DomainRemoveCommand
// ============================================================

export interface DomainRemoveOptions {
  confirm?: string;
  json?: boolean;
}

export class DomainRemoveCommand {
  constructor(private readonly deps: DomainDeps) {}

  async execute(domain: string, opts: DomainRemoveOptions = {}): Promise<number> {
    if (!this.deps.getToken()) {
      return exitWithError(this.deps, opts.json, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }
    if (!domain || domain.trim() === '') {
      return exitWithError(this.deps, opts.json, 'Domain name is required. Usage: loopzero domain remove <domain> --confirm <domain>', 1, 'MISSING_ARGUMENT');
    }

    // Safety: require --confirm matching the domain name
    if (opts.confirm !== domain) {
      return exitWithError(
        this.deps, opts.json,
        `To remove ${domain}, pass --confirm ${domain}`,
        1, 'CONFIRMATION_REQUIRED',
      );
    }

    try {
      const result = await this.deps.deleteDomain(domain.toLowerCase().trim());

      if (opts.json) {
        this.deps.writeTo(JSON.stringify(result));
        return 0;
      }

      this.deps.writeTo(`✓ Domain ${domain} removal in progress.`);
      return 0;
    } catch (err) {
      if (err instanceof ApiError && err.statusCode === 404) {
        return exitWithError(this.deps, opts.json, `Domain ${domain} not found.`, 1, 'NOT_FOUND');
      }
      return handleApiError(this.deps, opts.json, err, 'Failed to remove domain');
    }
  }
}

// ============================================================
// DomainDNSCommand — loopzero domain dns <domain>
// ============================================================

export interface DomainDNSOptions {
  json?: boolean;
}

export class DomainDNSCommand {
  constructor(private readonly deps: DomainDeps) {}

  async execute(domain: string, opts: DomainDNSOptions = {}): Promise<number> {
    if (!this.deps.getToken()) {
      return exitWithError(this.deps, opts.json, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }
    if (!domain || domain.trim() === '') {
      return exitWithError(this.deps, opts.json, 'Domain name is required. Usage: loopzero domain dns <domain>', 1, 'MISSING_ARGUMENT');
    }

    if (!this.deps.listDNSRecords) {
      return exitWithError(this.deps, opts.json, 'DNS management not available for this domain.', 3, 'NOT_AVAILABLE');
    }

    try {
      const result = await this.deps.listDNSRecords(domain.toLowerCase().trim());

      if (opts.json) {
        this.deps.writeTo(JSON.stringify(result));
        return 0;
      }

      this.deps.writeTo(`  DNS records for ${result.domain} (${result.total} records)\n`);
      this.deps.writeTo(`  ${'TYPE'.padEnd(8)} ${'NAME'.padEnd(35)} ${'VALUE'.padEnd(45)} TTL`);
      this.deps.writeTo(`  ${'─'.repeat(8)} ${'─'.repeat(35)} ${'─'.repeat(45)} ${'─'.repeat(6)}`);
      for (const r of result.records) {
        const name = r.name.length > 34 ? r.name.substring(0, 31) + '...' : r.name;
        const content = r.content.length > 44 ? r.content.substring(0, 41) + '...' : r.content;
        const proxied = r.proxied ? ' (CF)' : '';
        this.deps.writeTo(
          `  ${r.type.padEnd(8)} ${name.padEnd(35)} ${content.padEnd(45)} ${String(r.ttl ?? 'auto').padEnd(6)}${proxied}`
        );
      }
      return 0;
    } catch (err) {
      return handleApiError(this.deps, opts.json, err, 'Failed to list DNS records');
    }
  }
}
