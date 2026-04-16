// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiError } from '../api/client';

interface MemberItem {
  username: string;
  email: string;
  role: string;
  joined_at: string;
}

export interface TeamListDeps {
  getToken: () => string | null;
  listMembers: () => Promise<{ members: MemberItem[] }>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface TeamListOptions {
  json?: boolean;
}

/**
 * TeamListCommand lists all members of the current team.
 *
 * Does NOT require loopzero.yaml.
 *
 * Exit codes:
 *   0 — success (including empty list)
 *   1 — usage error (API error)
 *   4 — auth error
 */
export class TeamListCommand {
  private readonly deps: TeamListDeps;

  constructor(deps: TeamListDeps) {
    this.deps = deps;
  }

  async execute(opts: TeamListOptions = {}): Promise<number> {
    // 1. Auth check
    if (!this.deps.getToken()) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    // 2. Call API
    let response: { members: MemberItem[] };
    try {
      response = await this.deps.listMembers();
    } catch (err) {
      return this.handleError(opts, err);
    }

    // 3. Display result
    if (opts.json) {
      this.deps.writeTo(JSON.stringify({ members: response.members, total: response.members.length }));
      return 0;
    }

    if (response.members.length === 0) {
      this.deps.writeTo('No team members found.');
      return 0;
    }

    this.deps.writeTo('Team members:');
    this.deps.writeTo('');
    this.deps.writeTo(`  ${pad('USERNAME', 16)}${pad('ROLE', 10)}EMAIL`);
    for (const m of response.members) {
      this.deps.writeTo(`  ${pad(m.username, 16)}${pad(m.role, 10)}${m.email}`);
    }
    this.deps.writeTo('');
    this.deps.writeTo(`${response.members.length} member${response.members.length !== 1 ? 's' : ''}`);

    return 0;
  }

  private exitWithError(opts: TeamListOptions, message: string, exitCode: number, code?: string): number {
    if (opts.json) {
      const payload: Record<string, unknown> = { error: message, exit_code: exitCode };
      if (code) {
        payload.code = code;
      }
      this.deps.writeTo(JSON.stringify(payload));
    } else {
      this.deps.writeError(message);
    }
    return exitCode;
  }

  private handleError(opts: TeamListOptions, err: unknown): number {
    if (err instanceof ApiError) {
      if (err.exitCode === 4 || err.statusCode === 401) {
        return this.exitWithError(opts, 'Authentication failed. Run: loopzero login', 4, 'AUTH_REQUIRED');
      }
      return this.exitWithError(opts, `Failed to list team members: ${err.message}. Try again.`, 1, err.code);
    }
    if (err instanceof Error) {
      return this.exitWithError(opts, `Failed to list team members: ${err.message}. Try again.`, 1, 'UNEXPECTED_ERROR');
    }
    return this.exitWithError(opts, 'Failed to list team members. Try again later.', 1, 'UNEXPECTED_ERROR');
  }
}

function pad(str: string, len: number): string {
  return str.length >= len ? str + ' ' : str + ' '.repeat(len - str.length);
}
