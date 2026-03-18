export interface TokenValidationResult {
  valid: boolean;
  email: string | null;
  teams: string[];
  serverError?: boolean;
}

export interface LoginDeps {
  saveToken: (token: string, email: string, team: string) => void;
  validateToken: (token: string) => Promise<TokenValidationResult>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
  isCI: boolean;
  /** @deprecated Browser OAuth flow removed. Kept for interface compatibility. */
  openBrowser?: (url: string) => Promise<void>;
  /** @deprecated Browser OAuth flow removed. Kept for interface compatibility. */
  startCallbackServer?: () => Promise<{ token: string; email: string; teams: string[]; shutdown: () => void }>;
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
      // CI mode: token is mandatory
      if (this.deps.isCI) {
        return this.exitWithError(opts, 'In CI mode, use: loopzero login --token <token>', 1);
      }

      // @deprecated Browser OAuth flow — not wired in production (no callers pass these deps).
      // Kept because existing tests exercise this path. Will be removed in a future cleanup.
      if (this.deps.openBrowser && this.deps.startCallbackServer) {
        return this.browserOAuthFlow(opts);
      }

      return this.exitWithError(opts, 'Token is required. Use: loopzero login --token <token>', 1);
    }

    const result = await this.deps.validateToken(opts.token);

    if (!result.valid || !result.email) {
      if (result.serverError) {
        return this.exitWithError(opts, 'Service temporarily unavailable. Try again in a moment.', 4);
      } else {
        return this.exitWithError(opts, 'Authentication failed. Check your token and try again.', 4);
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

  /**
   * @deprecated Browser OAuth flow — not wired in production CLI.
   * Kept for test compatibility. Will be removed in a future cleanup.
   */
  private async browserOAuthFlow(opts: LoginOptions): Promise<number> {
    let callbackResult: { token: string; email: string; teams: string[]; shutdown: () => void } | undefined;
    try {
      callbackResult = await this.deps.startCallbackServer!();
      await this.deps.openBrowser!(`http://localhost:9876/auth`);
    } catch {
      return this.exitWithError(opts, 'Authentication failed. Could not complete browser login. Try: loopzero login --token <token>', 4);
    }

    const { token, shutdown } = callbackResult;

    try {
      const result = await this.deps.validateToken(token);

      if (!result.valid || !result.email) {
        if (result.serverError) {
          return this.exitWithError(opts, 'Service temporarily unavailable. Try again in a moment.', 4);
        } else {
          return this.exitWithError(opts, 'Authentication failed. Check your token and try again.', 4);
        }
      }

      const team = this.resolveTeam(result.teams, opts.team);
      if (team === null) {
        return this.handleTeamResolutionFailure(opts, result.teams, opts.team);
      }

      this.deps.saveToken(token, result.email, team);

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
    } finally {
      shutdown();
    }
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
        1
      );
    }

    if (teams.length > 1) {
      return this.exitWithError(
        opts,
        `Multiple teams available. Use --team to select one: ${teams.join(', ')}`,
        1
      );
    }

    return this.exitWithError(opts, 'No teams available for this account.', 1);
  }

  private exitWithError(opts: LoginOptions, message: string, exitCode: number): number {
    if (opts.json) {
      this.deps.writeTo(JSON.stringify({ error: message, exit_code: exitCode }));
    } else {
      this.deps.writeError(message);
    }
    return exitCode;
  }
}
