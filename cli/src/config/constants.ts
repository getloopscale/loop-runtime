// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

/**
 * Centralized constants for the LoopZero CLI.
 * All magic numbers and configurable defaults live here.
 *
 * Environment variable overrides are noted where applicable.
 * In CI mode (CI=true), behavior may differ — see individual constant docs.
 */

/** Parse an integer from an environment variable, falling back to defaultValue on missing or invalid input. */
function envInt(name: string, defaultValue: number): number {
  const raw = process.env[name];
  if (!raw) return defaultValue;
  const parsed = parseInt(raw, 10);
  return isNaN(parsed) ? defaultValue : parsed;
}

// --- API Connection ---

/**
 * Default API base URL fallback when LOOPZERO_API_URL is not set.
 * Port 13001 matches the k3d mgmt cluster NodePort mapping (host:13001 → NodePort:30001).
 * For production, set LOOPZERO_API_URL to the real API endpoint.
 */
export const DEFAULT_API_URL_FALLBACK = 'http://localhost:13001';

/** Returns the API base URL, checking LOOPZERO_API_URL at call time. Validates scheme. */
export function getDefaultApiUrl(): string {
  const url = process.env.LOOPZERO_API_URL || DEFAULT_API_URL_FALLBACK;
  if (!url.startsWith('http://') && !url.startsWith('https://')) {
    throw new Error(`Invalid LOOPZERO_API_URL: must start with http:// or https://. Got: ${url}`);
  }
  return url;
}

/**
 * Returns true if no explicit LOOPZERO_API_URL was provided,
 * meaning the CLI is using the dev-only localhost fallback.
 * Commands can use this to warn users in non-dev contexts.
 */
export function isUsingDefaultApiUrl(): boolean {
  return !process.env.LOOPZERO_API_URL;
}

/** HTTP request timeout for API calls (ms). Override: LOOPZERO_API_TIMEOUT */
export const API_REQUEST_TIMEOUT_MS = envInt('LOOPZERO_API_TIMEOUT', 30000);

/** HTTP timeout for source bundle upload (ms). Override: LOOPZERO_UPLOAD_TIMEOUT */
export const UPLOAD_TIMEOUT_MS = envInt('LOOPZERO_UPLOAD_TIMEOUT', 120000);

// --- OAuth ---

/** Local server port for OAuth callback. Override: LOOPZERO_OAUTH_PORT */
export const OAUTH_CALLBACK_PORT = envInt('LOOPZERO_OAUTH_PORT', 9876);
export const OAUTH_CALLBACK_HOST = 'localhost';
export const OAUTH_CALLBACK_PATH = '/auth';
export const OAUTH_CALLBACK_URL =
  `http://${OAUTH_CALLBACK_HOST}:${OAUTH_CALLBACK_PORT}${OAUTH_CALLBACK_PATH}`;

// --- SSE Client ---

/** Delay between SSE reconnect attempts (ms). */
export const SSE_RECONNECT_DELAY_MS = 1000;

/** Polling interval when SSE falls back to polling (ms). */
export const SSE_POLL_INTERVAL_MS = 5000;

/** Maximum SSE reconnect attempts before giving up. */
export const SSE_MAX_RETRIES = 15;

// --- Deploy ---

/** Total deploy timeout (ms). Override: LOOPZERO_DEPLOY_TIMEOUT */
export const DEPLOY_TIMEOUT_MS = envInt('LOOPZERO_DEPLOY_TIMEOUT', 10 * 60 * 1000);

/** Maximum environment name length (RFC 1123 label). */
export const MAX_ENVIRONMENT_NAME_LENGTH = 63;

/** Maximum source bundle size in bytes (500MB). Fail fast before upload. */
export const MAX_BUNDLE_SIZE_BYTES = 500 * 1024 * 1024;

// --- Documentation URLs ---

export const DOCS_BASE_URL = 'https://docs.loopzero.example';
export const DOCS_URL_SECRETS = `${DOCS_BASE_URL}/secrets`;
