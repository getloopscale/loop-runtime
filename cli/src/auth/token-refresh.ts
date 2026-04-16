// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { refreshAccessToken } from './device-flow';
import { getCloudUrl } from '../config/constants';

export interface TokenRefreshDeps {
  getToken: () => string | null;
  getRefreshToken: () => string | null;
  isTokenExpired: () => boolean;
  saveToken: (token: string, email: string, team: string, refreshToken?: string, expiresIn?: number) => void;
  getEmail: () => string | null;
  getTeam: () => string | null;
  fetchFn: (url: string, init?: RequestInit) => Promise<Response>;
  writeError: (msg: string) => void;
}

/**
 * Ensures the access token is fresh before making API calls.
 * If the token is expired and a refresh token is available,
 * silently refreshes it. Returns the valid token or null.
 */
export async function ensureFreshToken(deps: TokenRefreshDeps): Promise<string | null> {
  const token = deps.getToken();
  if (!token) return null;

  // If token is not expired, use it as-is
  if (!deps.isTokenExpired()) {
    return token;
  }

  // Token is expired — try to refresh
  const refreshToken = deps.getRefreshToken();
  if (!refreshToken) {
    deps.writeError('Session expired. Run `loopzero login` to authenticate again.');
    return null;
  }

  const baseURL = getCloudUrl();
  const result = await refreshAccessToken(deps.fetchFn, baseURL, refreshToken);
  if (!result) {
    deps.writeError('Session expired. Run `loopzero login` to authenticate again.');
    return null;
  }

  // Save refreshed tokens
  const email = deps.getEmail() || '';
  const team = deps.getTeam() || '';
  deps.saveToken(result.access_token, email, team, result.refresh_token, result.expires_in);

  return result.access_token;
}
