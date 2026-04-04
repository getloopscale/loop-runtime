// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { getDefaultApiUrl, API_REQUEST_TIMEOUT_MS } from '../config/constants';

/** Device authorization response from the cloud service. */
export interface DeviceAuthResponse {
  device_code: string;
  user_code: string;
  verification_uri: string;
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

/** Pending response while user hasn't authorized yet. */
export interface PendingResponse {
  status: 'pending';
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

/**
 * Executes the Device Code Flow (RFC 8628):
 * 1. Request device code from cloud service
 * 2. Display verification URL and user code
 * 3. Open browser automatically
 * 4. Poll for token until authorized or timeout
 */
export async function executeDeviceCodeFlow(deps: DeviceFlowDeps): Promise<TokenResponse | null> {
  const baseURL = deps.baseURL || getDefaultApiUrl();

  // Step 1: Request device code
  const authResp = await requestDeviceCode(deps.fetchFn, baseURL);
  if (!authResp) {
    deps.writeError('Failed to start authentication. Check your connection and try again.');
    return null;
  }

  // Step 2: Display instructions
  deps.writeTo('');
  deps.writeTo(`  Open this URL in your browser:`);
  deps.writeTo(`  ${authResp.verification_uri}`);
  deps.writeTo('');
  deps.writeTo(`  Enter this code: ${authResp.user_code}`);
  deps.writeTo('');
  deps.writeTo('  Waiting for authorization...');

  // Step 3: Try to open browser
  try {
    await deps.openBrowser(authResp.verification_uri);
  } catch {
    // Browser open failed — user will open manually (already shown URL)
  }

  // Step 4: Poll for token
  const interval = Math.max((authResp.interval || 5) * 1000, 5000);
  const deadline = Date.now() + DEVICE_AUTH_TIMEOUT_MS;

  while (Date.now() < deadline) {
    await deps.sleep(interval);

    const result = await pollDeviceToken(deps.fetchFn, baseURL, authResp.device_code);
    if (result === null) {
      // Error — stop polling
      deps.writeError('Authentication failed. Try again.');
      return null;
    }
    if ('status' in result && result.status === 'pending') {
      continue; // Keep polling
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
): Promise<TokenResponse | PendingResponse | null> {
  try {
    const resp = await fetchFn(`${baseURL}/v1/auth/device/token`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ device_code: deviceCode }),
      signal: AbortSignal.timeout(API_REQUEST_TIMEOUT_MS),
    });

    if (!resp.ok) return null;
    const data = await resp.json() as Record<string, unknown>;

    if (data.status === 'pending') {
      return { status: 'pending' };
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
