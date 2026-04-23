// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiError } from '../api/client';

import { formatErrorMessage } from '../output/error-message';
export interface TeamAddDeps {
  getToken: () => string | null;
  addMember: (username: string, role: string) => Promise<{ username: string; role: string }>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface TeamAddOptions {
  role?: string;
  json?: boolean;
}

const VALID_ROLES = ['member', 'admin'];

/**
 * TeamAddCommand adds a member to the current team by GitHub username.
 *
 * Does NOT require loopzero.yaml.
 *
 * Exit codes:
 *   0 — member added (or already a member — idempotent)
 *   1 — usage error (invalid role, user not found)
 *   3 — operation failed (forbidden)
 *   4 — auth error
 */
export class TeamAddCommand {
  private readonly deps: TeamAddDeps;

  constructor(deps: TeamAddDeps) {
    this.deps = deps;
  }

  async execute(username: string, opts: TeamAddOptions = {}): Promise<number> {
    // 1. Auth check
    if (!this.deps.getToken()) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    // 2. Username required
    if (!username || username.trim() === '') {
      return this.exitWithError(opts, 'Username is required. Usage: loopzero team add <username> [--role member|admin]', 1, 'MISSING_ARGUMENT');
    }

    // 3. Validate role
    const role = opts.role ?? 'member';
    if (!VALID_ROLES.includes(role)) {
      return this.exitWithError(
        opts,
        `Invalid role "${role}". Valid roles: member, admin. Cannot add as owner.`,
        1,
        'INVALID_ROLE',
      );
    }

    // 4. Call API
    try {
      const result = await this.deps.addMember(username, role);

      if (opts.json) {
        this.deps.writeTo(JSON.stringify({ ok: true, username: result.username, role: result.role }));
      } else {
        this.deps.writeTo(`Added ${result.username} as ${result.role}.`);
      }
      return 0;
    } catch (err) {
      return this.handleError(opts, err, username);
    }
  }

  private exitWithError(opts: TeamAddOptions, message: string, exitCode: number, code?: string): number {
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

  private handleError(opts: TeamAddOptions, err: unknown, username: string): number {
    if (err instanceof ApiError) {
      if (err.exitCode === 4 || err.statusCode === 401) {
        return this.exitWithError(opts, 'Authentication failed. Run: loopzero login', 4, 'AUTH_REQUIRED');
      }
      if (err.statusCode === 404) {
        return this.exitWithError(
          opts,
          `User ${username} not found. They must run 'loopzero login' first.`,
          1,
          'USER_NOT_FOUND',
        );
      }
      if (err.statusCode === 403) {
        return this.exitWithError(
          opts,
          'Permission denied. Only owners and admins can add members.',
          3,
          'FORBIDDEN',
        );
      }
      if (err.statusCode === 409) {
        // Already a member — idempotent success
        if (opts.json) {
          this.deps.writeTo(JSON.stringify({ ok: true, username, role: 'existing', already_member: true }));
        } else {
          this.deps.writeTo(`${username} is already a member.`);
        }
        return 0;
      }
      return this.exitWithError(opts, formatErrorMessage('Failed to add member', err.message, 'Try again'), 3, err.code);
    }
    if (err instanceof Error) {
      return this.exitWithError(opts, formatErrorMessage('Failed to add member', err.message, 'Try again'), 3, 'UNEXPECTED_ERROR');
    }
    return this.exitWithError(opts, 'Failed to add member. Try again later.', 3, 'UNEXPECTED_ERROR');
  }
}
