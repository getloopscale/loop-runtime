// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// CHG-2026-04-23-AUDIT-10: API now returns `username` (from users.username
// column, migration 000037). Used as display fallback when email is empty —
// common for OAuth2 client_credentials tokens (service accounts have no
// email claim). Field is optional; older servers that don't return it still
// work (humans typically have email populated).
export interface WhoamiApiResult {
  email: string;
  username?: string;
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
        // API confirmed — use server-side data (authoritative).
        // CHG-2026-04-23-AUDIT-10: fall back to username when email is empty
        // (service-account tokens have no email claim).
        const identity = apiResult.email || apiResult.username || '';
        if (opts.json) {
          const payload: Record<string, unknown> = {
            authenticated: true,
            email: apiResult.email,
            team: apiResult.team,
          };
          if (apiResult.username) {
            payload.username = apiResult.username;
          }
          this.deps.writeTo(JSON.stringify(payload));
        } else {
          this.deps.writeTo(`Authenticated as: ${identity}`);
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
