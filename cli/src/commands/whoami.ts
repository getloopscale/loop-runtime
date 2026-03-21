export interface WhoamiApiResult {
  email: string;
  team: string;
}

export interface WhoamiDeps {
  getToken: () => string | null;
  getEmail: () => string | null;
  getTeam: () => string | null;
  /** Validate token against the API. Returns user info or null if invalid/unreachable. */
  verifyToken?: (token: string) => Promise<WhoamiApiResult | null>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface WhoamiOptions {
  json?: boolean;
}

export class WhoamiCommand {
  private readonly deps: WhoamiDeps;

  constructor(deps: WhoamiDeps) {
    this.deps = deps;
  }

  async execute(opts: WhoamiOptions = {}): Promise<number> {
    const token = this.deps.getToken?.();
    const localEmail = this.deps.getEmail();

    if (!localEmail && !token) {
      if (opts.json) {
        this.deps.writeTo(JSON.stringify({ authenticated: false }));
      } else {
        this.deps.writeError('Not authenticated. Run: loopzero login');
      }
      return 4;
    }

    // Validate token against API if verifyToken is available
    if (token && this.deps.verifyToken) {
      const apiResult = await this.deps.verifyToken(token);
      if (apiResult) {
        // API confirmed — use server-side data (authoritative)
        if (opts.json) {
          this.deps.writeTo(JSON.stringify({
            authenticated: true,
            email: apiResult.email,
            team: apiResult.team,
          }));
        } else {
          this.deps.writeTo(`Authenticated as: ${apiResult.email}`);
          if (apiResult.team) {
            this.deps.writeTo(`Team: ${apiResult.team}`);
          }
        }
        return 0;
      }

      // API rejected the token — not authenticated
      if (opts.json) {
        this.deps.writeTo(JSON.stringify({ authenticated: false, error: 'Token is invalid or expired. Run: loopzero login' }));
      } else {
        this.deps.writeError('Token is invalid or expired. Run: loopzero login');
      }
      return 4;
    }

    // Fallback: no verifyToken dep (backwards compat) — use local data
    const email = localEmail;
    const team = this.deps.getTeam();

    if (!email) {
      if (opts.json) {
        this.deps.writeTo(JSON.stringify({ authenticated: false }));
      } else {
        this.deps.writeError('Not authenticated. Run: loopzero login');
      }
      return 4;
    }

    if (opts.json) {
      this.deps.writeTo(JSON.stringify({ authenticated: true, email, team }));
    } else {
      this.deps.writeTo(`Authenticated as: ${email}`);
      if (team) {
        this.deps.writeTo(`Team: ${team}`);
      }
    }

    return 0;
  }
}
