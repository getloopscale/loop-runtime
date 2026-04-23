// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiError } from '../api/client';

import { formatErrorMessage } from '../output/error-message';
export interface TeamRoleDeps {
  getToken: () => string | null;
  changeRole: (username: string, role: string) => Promise<{ username: string; role: string }>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface TeamRoleOptions {
  json?: boolean;
}

const VALID_ROLES = ['member', 'admin'];

/**
 * TeamRoleCommand changes a team member's role.
 *
 * Does NOT require loopzero.yaml.
 *
 * Exit codes:
 *   0 — role changed
 *   1 — usage error (invalid role, missing args)
 *   3 — operation failed (forbidden)
 *   4 — auth error
 */
export class TeamRoleCommand {
  private readonly deps: TeamRoleDeps;

  constructor(deps: TeamRoleDeps) {
    this.deps = deps;
  }

  async execute(username: string, role: string, opts: TeamRoleOptions = {}): Promise<number> {
    // 1. Auth check
    if (!this.deps.getToken()) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    // 2. Username required
    if (!username || username.trim() === '') {
      return this.exitWithError(opts, 'Username is required. Usage: loopzero team role <username> <role>', 1, 'MISSING_ARGUMENT');
    }

    // 3. Role required
    if (!role || role.trim() === '') {
      return this.exitWithError(opts, 'Role is required. Usage: loopzero team role <username> <role>', 1, 'MISSING_ARGUMENT');
    }

    // 4. Validate role
    if (!VALID_ROLES.includes(role)) {
      return this.exitWithError(
        opts,
        `Invalid role "${role}". Valid roles: admin, member. Ownership transfer is a separate operation.`,
        1,
        'INVALID_ROLE',
      );
    }

    // 5. Call API
    try {
      const result = await this.deps.changeRole(username, role);

      if (opts.json) {
        this.deps.writeTo(JSON.stringify({ ok: true, username: result.username, role: result.role }));
      } else {
        this.deps.writeTo(`Changed ${result.username}'s role to ${result.role}.`);
      }
      return 0;
    } catch (err) {
      return this.handleError(opts, err, username);
    }
  }

  private exitWithError(opts: TeamRoleOptions, message: string, exitCode: number, code?: string): number {
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

  private handleError(opts: TeamRoleOptions, err: unknown, username: string): number {
    if (err instanceof ApiError) {
      if (err.exitCode === 4 || err.statusCode === 401) {
        return this.exitWithError(opts, 'Authentication failed. Run: loopzero login', 4, 'AUTH_REQUIRED');
      }
      if (err.statusCode === 403) {
        return this.exitWithError(
          opts,
          'Permission denied. Only the team owner can change roles.',
          3,
          'FORBIDDEN',
        );
      }
      if (err.statusCode === 409 && err.code === 'OWNER_PROTECTED') {
        return this.exitWithError(opts, 'Cannot change the owner\'s role. Transfer ownership first.', 1, 'OWNER_PROTECTED');
      }
      if (err.statusCode === 404) {
        return this.exitWithError(opts, `${username} is not a team member.`, 1, 'MEMBER_NOT_FOUND');
      }
      return this.exitWithError(opts, formatErrorMessage('Failed to change role', err.message, 'Try again'), 3, err.code);
    }
    if (err instanceof Error) {
      return this.exitWithError(opts, formatErrorMessage('Failed to change role', err.message, 'Try again'), 3, 'UNEXPECTED_ERROR');
    }
    return this.exitWithError(opts, 'Failed to change role. Try again later.', 3, 'UNEXPECTED_ERROR');
  }
}
