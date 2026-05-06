// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// loopzero auth refresh
//
// Closes F-01 + F-03 from /dogfood full report (2026-05-06): JWT TTL=1h
// without an automatic refresh path forces operators to manually re-mint
// tokens via seed-local-user.sh during long sessions.
//
// This command consumes the refresh_token already persisted by
// device-flow login and exchanges it for a fresh access token via the
// cloud service /v1/auth/token/refresh endpoint. On success it
// transparently updates the local config (atomic write), so the next CLI
// invocation uses the fresh credentials.
//
// Local dev (Hydra client_credentials) does not produce a refresh token —
// for that flow the operator must continue using
// `bash infra/helmfile/scripts/seed-local-user.sh`. The error message in
// MISSING_REFRESH_TOKEN points to that script explicitly.

export interface AuthRefreshTokenResponse {
  access_token: string;
  refresh_token: string;
  expires_in: number;
}

export interface AuthRefreshDeps {
  getRefreshToken: () => string | null;
  saveToken: (
    token: string,
    email: string,
    team: string,
    refreshToken?: string,
    expiresIn?: number,
  ) => void;
  getEmail: () => string | null;
  getTeam: () => string | null;
  refreshFn: (refreshToken: string) => Promise<AuthRefreshTokenResponse | null>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface AuthRefreshOptions {
  json?: boolean;
}

export class AuthRefreshCommand {
  constructor(private readonly deps: AuthRefreshDeps) {}

  async execute(opts: AuthRefreshOptions = {}): Promise<number> {
    const json = Boolean(opts.json);

    const refreshToken = this.deps.getRefreshToken();
    if (!refreshToken) {
      const message =
        'No refresh token stored. Run: loopzero login (or bash infra/helmfile/scripts/seed-local-user.sh for local dev).';
      if (json) {
        this.deps.writeTo(
          JSON.stringify({
            ok: false,
            error: message,
            code: 'MISSING_REFRESH_TOKEN',
            exit_code: 4,
          }),
        );
      } else {
        this.deps.writeError(message);
      }
      return 4;
    }

    const tokens = await this.deps.refreshFn(refreshToken);
    if (!tokens) {
      const message =
        'Refresh token expired or invalid. Run: loopzero login (or bash infra/helmfile/scripts/seed-local-user.sh for local dev).';
      if (json) {
        this.deps.writeTo(
          JSON.stringify({
            ok: false,
            error: message,
            code: 'REFRESH_FAILED',
            exit_code: 4,
          }),
        );
      } else {
        this.deps.writeError(message);
      }
      return 4;
    }

    // Reuse the persisted email/team — refresh keeps the same identity,
    // does not switch users.
    const email = this.deps.getEmail() ?? '';
    const team = this.deps.getTeam() ?? '';
    this.deps.saveToken(
      tokens.access_token,
      email,
      team,
      tokens.refresh_token,
      tokens.expires_in,
    );

    if (json) {
      this.deps.writeTo(
        JSON.stringify({
          ok: true,
          expires_in: tokens.expires_in,
          team,
        }),
      );
    } else {
      this.deps.writeTo(
        `Refreshed credentials. New token expires in ${tokens.expires_in} seconds.`,
      );
    }
    return 0;
  }
}
