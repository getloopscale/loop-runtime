// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiError } from '../api/client';

export interface TeamRemoveDeps {
  getToken: () => string | null;
  removeMember: (username: string) => Promise<void>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface TeamRemoveOptions {
  json?: boolean;
}

/**
 * TeamRemoveCommand removes a member from the current team.
 *
 * Does NOT require loopzero.yaml.
 *
 * Exit codes:
 *   0 — member removed
 *   1 — usage error (cannot remove owner, cannot remove self)
 *   3 — operation failed (forbidden)
 *   4 — auth error
 */
export class TeamRemoveCommand {
  private readonly deps: TeamRemoveDeps;

  constructor(deps: TeamRemoveDeps) {
    this.deps = deps;
  }

  async execute(username: string, opts: TeamRemoveOptions = {}): Promise<number> {
    // 1. Auth check
    if (!this.deps.getToken()) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    // 2. Username required
    if (!username || username.trim() === '') {
      return this.exitWithError(opts, 'Username is required. Usage: loopzero team remove <username>', 1, 'MISSING_ARGUMENT');
    }

    // 3. Call API
    try {
      await this.deps.removeMember(username);

      if (opts.json) {
        this.deps.writeTo(JSON.stringify({ ok: true, username, status: 'removed' }));
      } else {
        this.deps.writeTo(`Removed ${username} from team.`);
      }
      return 0;
    } catch (err) {
      return this.handleError(opts, err, username);
    }
  }

  private exitWithError(opts: TeamRemoveOptions, message: string, exitCode: number, code?: string): number {
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

  private handleError(opts: TeamRemoveOptions, err: unknown, username: string): number {
    if (err instanceof ApiError) {
      if (err.exitCode === 4 || err.statusCode === 401) {
        return this.exitWithError(opts, 'Authentication failed. Run: loopzero login', 4, 'AUTH_REQUIRED');
      }
      if (err.statusCode === 403) {
        return this.exitWithError(
          opts,
          'Permission denied. Only owners and admins can remove members.',
          3,
          'FORBIDDEN',
        );
      }
      if (err.statusCode === 409 && err.code === 'OWNER_PROTECTED') {
        return this.exitWithError(opts, 'Cannot remove the team owner.', 1, 'OWNER_PROTECTED');
      }
      if (err.statusCode === 409 && err.code === 'SELF_REMOVE') {
        return this.exitWithError(opts, 'Cannot remove yourself. Transfer ownership first.', 1, 'SELF_REMOVE');
      }
      if (err.statusCode === 404) {
        return this.exitWithError(opts, `${username} is not a team member.`, 1, 'MEMBER_NOT_FOUND');
      }
      return this.exitWithError(opts, `Failed to remove member: ${err.message}. Try again.`, 3, err.code);
    }
    if (err instanceof Error) {
      return this.exitWithError(opts, `Failed to remove member: ${err.message}. Try again.`, 3, 'UNEXPECTED_ERROR');
    }
    return this.exitWithError(opts, 'Failed to remove member. Try again later.', 3, 'UNEXPECTED_ERROR');
  }
}
