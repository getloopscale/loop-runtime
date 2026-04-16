// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { getDefaultApiUrl, API_REQUEST_TIMEOUT_MS } from '../config/constants';

/** Device authorization response from the cloud service. */
export interface DeviceAuthResponse {
  device_code: string;
  user_code: string;
  verification_uri: string;
  verification_uri_complete?: string;
  expires_in: number;
  interval: number;
}

/** Token response after successful authorization. */
export interface TokenResponse {
  access_token: string;
  refresh_token: string;
  token_type: string;
  expires_in: number;
}

/** Intermediate response during device flow polling (RFC 8628). */
export interface PollStatusResponse {
  status: 'pending' | 'slow_down' | 'expired_token';
}

export interface DeviceFlowDeps {
  fetchFn: (url: string, init?: RequestInit) => Promise<Response>;
  baseURL?: string;
  openBrowser: (url: string) => Promise<void>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
  sleep: (ms: number) => Promise<void>;
}

const DEVICE_AUTH_TIMEOUT_MS = 5 * 60 * 1000; // 5 minutes
const MAX_POLL_ATTEMPTS = 60; // Safety limit (§2.1)
const SLOW_DOWN_INCREMENT_MS = 5000; // RFC 8628 §3.5: increase interval by 5s on slow_down

/**
 * Adds ±20% jitter to an interval to prevent thundering herd (§2.1).
 */
function withJitter(intervalMs: number): number {
  const jitter = intervalMs * 0.2 * (Math.random() * 2 - 1); // ±20%
  return Math.max(1000, Math.round(intervalMs + jitter));
}

/**
 * Executes the Device Code Flow (RFC 8628):
 * 1. Request device code from cloud service
 * 2. Display verification URL and user code
 * 3. Open browser automatically
 * 4. Poll for token until authorized or timeout
 *
 * RFC 8628 compliance (§2.1):
 * - Handles slow_down: increases interval by 5s per §3.5
 * - Handles expired_token: stops with clear message
 * - Jitter on polling interval prevents thundering herd
 * - Max retry count as safety limit alongside timeout
 */
export async function executeDeviceCodeFlow(deps: DeviceFlowDeps): Promise<TokenResponse | null> {
  const baseURL = deps.baseURL || getDefaultApiUrl();

  // Step 1: Request device code
  const authResp = await requestDeviceCode(deps.fetchFn, baseURL);
  if (!authResp) {
    deps.writeError('Failed to start authentication. Check your connection and try again.');
    return null;
  }

  // Step 2: Display instructions (Vercel/Railway CLI style)
  const browserURL = authResp.verification_uri_complete || authResp.verification_uri;
  deps.writeTo('');
  deps.writeTo(`  Visit ${browserURL}`);
  deps.writeTo(`  Code: ${authResp.user_code}`);
  deps.writeTo('');

  // Step 3: Open browser automatically
  await deps.openBrowser(browserURL);
  deps.writeTo('  Waiting for authentication...');

  // Step 4: Poll for token with RFC 8628 compliance
  let intervalMs = Math.max((authResp.interval || 5) * 1000, 5000);
  const deadline = Date.now() + DEVICE_AUTH_TIMEOUT_MS;
  let attempts = 0;

  while (Date.now() < deadline && attempts < MAX_POLL_ATTEMPTS) {
    await deps.sleep(withJitter(intervalMs));
    attempts++;

    const result = await pollDeviceToken(deps.fetchFn, baseURL, authResp.device_code);
    if (result === null) {
      // Unrecoverable error — stop polling
      deps.writeError('Authentication failed. Try again with `loopzero login`.');
      return null;
    }
    if ('status' in result) {
      if (result.status === 'slow_down') {
        // RFC 8628 §3.5: increase interval by 5 seconds
        intervalMs += SLOW_DOWN_INCREMENT_MS;
        continue;
      }
      if (result.status === 'expired_token') {
        deps.writeError('Authentication request expired. Run `loopzero login` to try again.');
        return null;
      }
      if (result.status === 'pending') {
        continue;
      }
    }
    // Success
    return result as TokenResponse;
  }

  deps.writeError('Authentication timed out. Run `loopzero login` to try again.');
  return null;
}

async function requestDeviceCode(
  fetchFn: DeviceFlowDeps['fetchFn'],
  baseURL: string,
): Promise<DeviceAuthResponse | null> {
  try {
    const resp = await fetchFn(`${baseURL}/v1/auth/device/authorize`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      signal: AbortSignal.timeout(API_REQUEST_TIMEOUT_MS),
    });

    if (!resp.ok) return null;
    return await resp.json() as DeviceAuthResponse;
  } catch {
    return null;
  }
}

async function pollDeviceToken(
  fetchFn: DeviceFlowDeps['fetchFn'],
  baseURL: string,
  deviceCode: string,
): Promise<TokenResponse | PollStatusResponse | null> {
  try {
    const resp = await fetchFn(`${baseURL}/v1/auth/device/token`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ device_code: deviceCode }),
      signal: AbortSignal.timeout(API_REQUEST_TIMEOUT_MS),
    });

    if (!resp.ok) return null;
    const data = await resp.json() as Record<string, unknown>;

    // RFC 8628 §3.5: handle authorization_pending, slow_down, expired_token
    if (data.status === 'pending' || data.error === 'authorization_pending') {
      return { status: 'pending' };
    }
    if (data.error === 'slow_down' || data.status === 'slow_down') {
      return { status: 'slow_down' };
    }
    if (data.error === 'expired_token' || data.status === 'expired_token') {
      return { status: 'expired_token' };
    }
    if (data.access_token && data.refresh_token && typeof data.expires_in === 'number') {
      return {
        access_token: String(data.access_token),
        refresh_token: String(data.refresh_token),
        token_type: String(data.token_type || 'bearer'),
        expires_in: Number(data.expires_in),
      };
    }
    return null;
  } catch {
    return null;
  }
}

/**
 * Refreshes an access token using a refresh token.
 * Returns new tokens on success, null on failure.
 */
export async function refreshAccessToken(
  fetchFn: DeviceFlowDeps['fetchFn'],
  baseURL: string,
  refreshToken: string,
): Promise<TokenResponse | null> {
  try {
    const resp = await fetchFn(`${baseURL}/v1/auth/token/refresh`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: refreshToken }),
      signal: AbortSignal.timeout(API_REQUEST_TIMEOUT_MS),
    });

    if (!resp.ok) return null;
    const data = await resp.json() as Record<string, unknown>;
    if (data.access_token && data.refresh_token && typeof data.expires_in === 'number') {
      return {
        access_token: String(data.access_token),
        refresh_token: String(data.refresh_token),
        token_type: String(data.token_type || 'bearer'),
        expires_in: Number(data.expires_in),
      };
    }
    return null;
  } catch {
    return null;
  }
}
