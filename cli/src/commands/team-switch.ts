// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiError } from '../api/client';
import { EXIT_CODES } from '../lib/exit-codes';

interface SwitchTeamResponse {
  active_team: string;
  message: string;
}

export interface TeamSwitchDeps {
  getToken: () => string | null;
  switchTeam: (name: string) => Promise<SwitchTeamResponse>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface TeamSwitchOptions {
  json?: boolean;
}

/**
 * TeamSwitchCommand sets the user's active team via POST /api/v1/auth/team/switch.
 *
 * Plan T2.5 D8 full — persistent active-team selection backed by
 * users.active_tenant_id column (migration 000018). Closes F-S2-02
 * silent team swap by making the selection explicit + user-driven.
 *
 * Exit codes (per Rule 4):
 *   0 — success
 *   1 — usage error (missing name)
 *   3 — server error (membership check failed, etc. — DEPLOY_FAILED scope)
 *   4 — auth error
 */
export class TeamSwitchCommand {
  private readonly deps: TeamSwitchDeps;

  constructor(deps: TeamSwitchDeps) {
    this.deps = deps;
  }

  async execute(name: string, opts: TeamSwitchOptions = {}): Promise<number> {
    if (!this.deps.getToken()) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', EXIT_CODES.AUTH_ERROR, 'AUTH_REQUIRED');
    }
    if (!name || name.trim() === '') {
      return this.exitWithError(opts, 'Team name is required. Usage: loopzero team switch <name>', EXIT_CODES.USAGE, 'MISSING_FIELD');
    }

    try {
      const result = await this.deps.switchTeam(name);
      if (opts.json) {
        this.deps.writeTo(JSON.stringify(result));
      } else {
        this.deps.writeTo(`Active team switched to "${result.active_team}".`);
      }
      return EXIT_CODES.SUCCESS;
    } catch (err) {
      if (err instanceof ApiError) {
        // 403 NOT_MEMBER → exit 1 (usage error: user picked wrong team)
        if (err.statusCode === 403) {
          return this.exitWithError(opts, err.message, EXIT_CODES.USAGE, err.code);
        }
        // 401 → auth
        if (err.statusCode === 401 || err.exitCode === EXIT_CODES.AUTH_ERROR) {
          return this.exitWithError(opts, 'Authentication failed. Run: loopzero login', EXIT_CODES.AUTH_ERROR, 'AUTH_REQUIRED');
        }
        // 501 → not configured server-side
        if (err.statusCode === 501) {
          return this.exitWithError(opts, err.message, EXIT_CODES.USAGE, err.code);
        }
      }
      const msg = err instanceof Error ? err.message : 'Failed to switch team. Try again later.';
      return this.exitWithError(opts, msg, EXIT_CODES.DEPLOY_FAILED, 'TEAM_SWITCH_FAILED');
    }
  }

  private exitWithError(opts: TeamSwitchOptions, message: string, exitCode: number, code?: string): number {
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
}
