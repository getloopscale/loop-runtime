/**
 * Centralized constants for the LoopZero CLI.
 * All magic numbers and configurable defaults live here.
 *
 * Environment variable overrides are noted where applicable.
 * In CI mode (CI=true), behavior may differ — see individual constant docs.
 */

// --- API Connection ---

/** Default API base URL fallback when LOOPZERO_API_URL is not set. */
export const DEFAULT_API_URL_FALLBACK = 'http://localhost:3001';

/** Returns the API base URL, checking LOOPZERO_API_URL at call time. */
export function getDefaultApiUrl(): string {
  return process.env.LOOPZERO_API_URL || DEFAULT_API_URL_FALLBACK;
}

/** HTTP request timeout for API calls (ms). Override: LOOPZERO_API_TIMEOUT */
export const API_REQUEST_TIMEOUT_MS =
  parseInt(process.env.LOOPZERO_API_TIMEOUT || '30000', 10);

/** HTTP timeout for source bundle upload (ms). Override: LOOPZERO_UPLOAD_TIMEOUT */
export const UPLOAD_TIMEOUT_MS =
  parseInt(process.env.LOOPZERO_UPLOAD_TIMEOUT || '30000', 10);

// --- OAuth ---

/** Local server port for OAuth callback. Override: LOOPZERO_OAUTH_PORT */
export const OAUTH_CALLBACK_PORT =
  parseInt(process.env.LOOPZERO_OAUTH_PORT || '9876', 10);
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
export const DEPLOY_TIMEOUT_MS =
  parseInt(process.env.LOOPZERO_DEPLOY_TIMEOUT || String(10 * 60 * 1000), 10);

/** Maximum environment name length (RFC 1123 label). */
export const MAX_ENVIRONMENT_NAME_LENGTH = 63;

// --- Documentation URLs ---

export const DOCS_BASE_URL = 'https://docs.loopzero.example';
export const DOCS_URL_SECRETS = `${DOCS_BASE_URL}/secrets`;
