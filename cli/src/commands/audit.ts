// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiError } from '../api/client';

export interface AuditEntry {
  action: string;
  resource_type: string;
  resource_id: string;
  actor: string;
  timestamp: string;
  correlation_id?: string;
}

export interface AuditResponse {
  entries: AuditEntry[];
  total: number;
  limit: number;
  offset: number;
}

export interface AuditDeps {
  getToken: () => string | null;
  getAudit: (action?: string, since?: string) => Promise<AuditResponse | null>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface AuditOptions {
  json?: boolean;
  action?: string;
  since?: string;
}

export class AuditCommand {
  private readonly deps: AuditDeps;

  constructor(deps: AuditDeps) {
    this.deps = deps;
  }

  async run(opts: AuditOptions): Promise<number> {
    // Auth guard
    if (!this.deps.getToken()) {
      if (opts.json) {
        this.deps.writeTo(JSON.stringify({ error: 'Not authenticated. Run: loopzero login', exit_code: 4, code: 'AUTH_REQUIRED' }));
      } else {
        this.deps.writeError('Not authenticated. Run: loopzero login');
      }
      return 4;
    }

    // CHG-2026-04-23-AUDIT-14: catch ApiError explicitly so auth failures
    // from the API (expired token) map to exit 4 per Rule 4. Previously the
    // error escaped and the register's withTimeout .catch returned 3.
    let data: AuditResponse | null;
    try {
      data = await this.deps.getAudit(opts.action, opts.since);
    } catch (err) {
      if (err instanceof ApiError && (err.exitCode === 4 || err.statusCode === 401)) {
        if (opts.json) {
          this.deps.writeTo(JSON.stringify({
            error: 'Authentication failed. Run: loopzero login',
            exit_code: 4,
            code: 'AUTH_REQUIRED',
          }));
        } else {
          this.deps.writeError('Authentication failed. Run: loopzero login');
        }
        return 4;
      }
      if (err instanceof ApiError && err.statusCode === 403) {
        this.deps.writeError('Failed to fetch audit log. You may not have permission (requires admin or owner role).');
        return 4;
      }
      if (err instanceof Error) {
        this.deps.writeError(`Failed to fetch audit log: ${err.message}`);
        return 1;
      }
      this.deps.writeError('Failed to fetch audit log. Try again later.');
      return 1;
    }

    if (!data) {
      this.deps.writeError('Failed to fetch audit log. You may not have permission (requires admin or owner role).');
      return 1;
    }

    if (opts.json) {
      this.deps.writeTo(JSON.stringify(data));
      return 0;
    }

    if (data.entries.length === 0) {
      this.deps.writeTo('No audit entries found.');
      return 0;
    }

    this.deps.writeTo(`Recent Activity (${data.total} total):\n`);
    for (const e of data.entries) {
      const time = new Date(e.timestamp).toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' });
      const resource = e.resource_id ? `${e.resource_type}/${e.resource_id}` : e.resource_type;
      this.deps.writeTo(`  ${time}  ${padRight(e.action, 22)} ${padRight(resource, 25)} ${e.actor.slice(0, 12)}`);
    }

    return 0;
  }
}

function padRight(s: string, len: number): string {
  return s.length >= len ? s : s + ' '.repeat(len - s.length);
}
