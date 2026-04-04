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
}

export class LoginCommand {
  private readonly deps: LoginDeps;

  constructor(deps: LoginDeps) {
    this.deps = deps;
  }

  async execute(opts: LoginOptions): Promise<number> {
    if (!opts.token) {
      if (this.deps.isCI) {
        return this.exitWithError(opts, 'In CI mode, use: loopzero login --token <token>', 1, 'MISSING_TOKEN');
      }

      // Device Code Flow — interactive browser-based login
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

    // Deprecation warning for GitHub PAT tokens
    // JWT tokens (3 dot-separated segments) are the new standard.
    // PAT tokens (ghp_, gho_, github_pat_) are deprecated.
    if (this.isGitHubPAT(opts.token) && !this.isSuppressed() && !opts.json) {
      this.deps.writeError(
        'Warning: Token-based authentication is deprecated.\n' +
        'Run `loopzero login` (without --token) to switch to the new login flow.\n' +
        'Token support will be removed in a future version.'
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
          'LoopZero requires a GitHub token with read:user scope (Classic PAT) or a GitHub OAuth token.\n' +
          'Create a Classic PAT at: https://github.com/settings/tokens/new?scopes=read:user\n' +
          'Then run: loopzero login --token <new-token>',
          4,
          'AUTH_FAILED'
        );
      }
    }

    const team = this.resolveTeam(result.teams, opts.team);
    if (team === null) {
      return this.handleTeamResolutionFailure(opts, result.teams, opts.team);
    }

    this.deps.saveToken(opts.token, result.email, team);

    if (opts.json) {
      this.deps.writeTo(
        JSON.stringify({
          authenticated: true,
          email: result.email,
          team,
        })
      );
    } else {
      this.deps.writeTo(`Authenticated as ${result.email} (team: ${team})`);
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
      this.deps.writeTo(JSON.stringify({ authenticated: true, email: validation.email, team }));
    } else {
      this.deps.writeTo(`Authenticated as ${validation.email} (team: ${team})`);
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

  private isGitHubPAT(token: string): boolean {
    return token.startsWith('ghp_') || token.startsWith('gho_') || token.startsWith('github_pat_');
  }

  private isSuppressed(): boolean {
    return process.env.LOOPZERO_SUPPRESS_DEPRECATION === '1';
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
