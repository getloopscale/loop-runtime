// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

export interface TokenValidationResult {
  valid: boolean;
  email: string | null;
  teams: string[];
  serverError?: boolean;
}

export interface DeviceFlowResult {
  access_token: string;
  refresh_token: string;
  expires_in: number;
}

export interface LoginDeps {
  saveToken: (token: string, email: string, team: string, refreshToken?: string, expiresIn?: number) => void;
  validateToken: (token: string) => Promise<TokenValidationResult>;
  executeDeviceFlow?: () => Promise<DeviceFlowResult | null>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
  isCI: boolean;
}

export interface LoginOptions {
  token?: string;
  team?: string;
  json?: boolean;
  // Companion flags to --token for CI / automation scenarios where the caller
  // already has a refresh_token in hand (e.g., minted out-of-band via Hydra
  // admin API for dogfood tests). When both are present, behavior matches the
  // device-flow path: configStore stores access + refresh + expires_in, and
  // the existing `onUnauthorized → ensureFreshToken` pipeline can renew the
  // access_token silently on 401 without re-running `loopzero login`.
  // Added 2026-05-13 per CTO-mandate T2 amendment (test↔prod auth-refresh
  // parity, finding-2026-05-13-e2e-token-refresh-divergence.md).
  refreshToken?: string;
  expiresIn?: number;
  // Plan J-05 — explicit opt-in to the OAuth2 Device Code Flow (RFC 8628).
  // The implicit "no --token + non-CI" path is preserved; this flag adds
  // discoverability so `loopzero login --device-flow` is a documented public
  // contract referenced in the J-05 DoD. Honored in CI too (operator opts
  // in deliberately) — bypasses the "use --token in CI" guard.
  deviceFlow?: boolean;
}

export class LoginCommand {
  private readonly deps: LoginDeps;

  constructor(deps: LoginDeps) {
    this.deps = deps;
  }

  async execute(opts: LoginOptions): Promise<number> {
    // Plan J-05 — explicit --device-flow flag wins. Honored even in CI
    // (the operator deliberately opted in), so the "must use --token in CI"
    // guard below is skipped on this branch.
    if (opts.deviceFlow) {
      if (!this.deps.executeDeviceFlow) {
        return this.exitWithError(
          opts,
          'Device authentication is not available in this build. ' +
          'Use: loopzero login --token <token>',
          4,
          'DEVICE_FLOW_UNAVAILABLE'
        );
      }
      return this.executeDeviceFlow(opts);
    }

    if (!opts.token) {
      if (this.deps.isCI) {
        return this.exitWithError(opts, 'In CI mode, use: loopzero login --token <token>', 1, 'MISSING_TOKEN');
      }

      // Device Code Flow — interactive browser-based login (legacy implicit path).
      if (this.deps.executeDeviceFlow) {
        return this.executeDeviceFlow(opts);
      }

      return this.exitWithError(
        opts,
        'Token is required. Use: loopzero login --token <token>\n' +
        'Generate a token at your account settings or ask your team admin.',
        1,
        'MISSING_TOKEN'
      );
    }

    const result = await this.deps.validateToken(opts.token);

    if (!result.valid || !result.email) {
      if (result.serverError) {
        return this.exitWithError(opts, 'Service temporarily unavailable. Try again in a moment.', 4, 'SERVICE_UNAVAILABLE');
      } else {
        return this.exitWithError(
          opts,
          'Authentication failed. Your token may be expired, revoked, or missing required scopes.\n' +
          'Sign up or sign in at https://app.dev.loopzero.example to obtain a token, then run: loopzero login --token <token>',
          4,
          'AUTH_FAILED'
        );
      }
    }

    const team = this.resolveTeam(result.teams, opts.team);
    if (team === null) {
      return this.handleTeamResolutionFailure(opts, result.teams, opts.team);
    }

    this.deps.saveToken(opts.token, result.email, team, opts.refreshToken, opts.expiresIn);

    if (opts.json) {
      this.deps.writeTo(
        JSON.stringify({
          authenticated: true,
          email: result.email,
          team,
          team_id: team,
        })
      );
    } else {
      // §3.5: Rich post-auth output (Vercel CLI style)
      this.deps.writeTo('');
      this.deps.writeTo(`  Authenticated as ${result.email}`);
      this.deps.writeTo(`  Team: ${team}`);
      this.deps.writeTo('');
      this.deps.writeTo('  Next: run `loopzero deploy` to deploy your project');
    }

    return 0;
  }

  private async executeDeviceFlow(opts: LoginOptions): Promise<number> {
    const result = await this.deps.executeDeviceFlow!();
    if (!result) {
      return this.exitWithError(opts, 'Authentication failed. Try again with: loopzero login', 4, 'AUTH_FAILED');
    }

    // Validate the received token to get user info
    const validation = await this.deps.validateToken(result.access_token);
    if (!validation.valid || !validation.email) {
      return this.exitWithError(opts, 'Authentication succeeded but token validation failed. Try again.', 4, 'AUTH_FAILED');
    }

    const team = this.resolveTeam(validation.teams, opts.team);
    if (team === null) {
      return this.handleTeamResolutionFailure(opts, validation.teams, opts.team);
    }

    this.deps.saveToken(result.access_token, validation.email, team, result.refresh_token, result.expires_in);

    if (opts.json) {
      this.deps.writeTo(JSON.stringify({
        authenticated: true,
        email: validation.email,
        team,
        team_id: team,
        expires_in: result.expires_in,
      }));
    } else {
      // §3.5: Rich post-auth output (Vercel CLI style)
      const expiry = result.expires_in ? `in ${Math.round(result.expires_in / 60)} minutes` : 'in 1 hour';
      this.deps.writeTo('');
      this.deps.writeTo(`  Authenticated as ${validation.email}`);
      this.deps.writeTo(`  Team: ${team}`);
      this.deps.writeTo(`  Token expires: ${expiry}`);
      this.deps.writeTo('');
      this.deps.writeTo('  Next: run `loopzero deploy` to deploy your project');
    }
    return 0;
  }

  private resolveTeam(teams: string[], requestedTeam?: string): string | null {
    if (requestedTeam) {
      return teams.includes(requestedTeam) ? requestedTeam : null;
    }

    if (teams.length === 1) {
      return teams[0];
    }

    if (teams.length === 0) {
      return null;
    }

    // Multiple teams, no --team flag — handled by caller
    if (this.deps.isCI) {
      return null;
    }

    // In interactive mode with multiple teams and no --team flag,
    // for now return null (interactive prompt not yet implemented)
    return null;
  }

  private handleTeamResolutionFailure(
    opts: LoginOptions,
    teams: string[],
    requestedTeam?: string
  ): number {
    if (requestedTeam && !teams.includes(requestedTeam)) {
      return this.exitWithError(
        opts,
        `Team "${requestedTeam}" not found. Available teams: ${teams.join(', ')}`,
        1,
        'TEAM_NOT_FOUND'
      );
    }

    if (teams.length > 1) {
      return this.exitWithError(
        opts,
        `Multiple teams available. Use --team to select one: ${teams.join(', ')}`,
        1,
        'TEAM_REQUIRED'
      );
    }

    return this.exitWithError(opts, 'No teams available for this account.', 1, 'NO_TEAMS');
  }

  private exitWithError(opts: LoginOptions, message: string, exitCode: number, code: string): number {
    if (opts.json) {
      this.deps.writeTo(JSON.stringify({ error: message, exit_code: exitCode, code }));
    } else {
      this.deps.writeError(message);
    }
    return exitCode;
  }
}
