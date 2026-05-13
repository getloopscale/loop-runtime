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
 * MEET-040: Single URL for all CLI operations.
 * Traefik routes /v1/auth/* → cloud service, /* → engine API.
 *
 * Priority: LOOPZERO_URL > LOOPZERO_API_URL (deprecated) > fallback.
 * Local dev: port 8080 matches the Traefik-less direct API server.
 * Production: set LOOPZERO_URL=https://api.dev.loopzero.example
 */
export const DEFAULT_API_URL_FALLBACK = 'http://localhost:8080';

/** Returns the unified API URL. Checks LOOPZERO_URL first, then legacy LOOPZERO_API_URL. */
export function getDefaultApiUrl(): string {
  const url = process.env.LOOPZERO_URL || process.env.LOOPZERO_API_URL || DEFAULT_API_URL_FALLBACK;
  if (process.env.LOOPZERO_API_URL && !process.env.LOOPZERO_URL) {
    // Legacy env var — accepted but deprecated
  }
  if (!url.startsWith('http://') && !url.startsWith('https://')) {
    throw new Error(`Invalid LOOPZERO_URL: must start with http:// or https://. Got: ${url}`);
  }
  return url;
}

/** HTTP request timeout for API calls (ms). Override: LOOPZERO_API_TIMEOUT */
export const API_REQUEST_TIMEOUT_MS = envInt('LOOPZERO_API_TIMEOUT', 30000);

/** HTTP timeout for source bundle upload (ms). Override: LOOPZERO_UPLOAD_TIMEOUT */
export const UPLOAD_TIMEOUT_MS = envInt('LOOPZERO_UPLOAD_TIMEOUT', 120000);

// --- Cloud Service ---

/**
 * MEET-040: getCloudUrl returns the same base URL as getDefaultApiUrl.
 * Traefik routes /v1/auth/* to cloud service. No separate URL needed.
 * Legacy LOOPZERO_CLOUD_URL accepted for backward compatibility.
 */
export function getCloudUrl(): string {
  if (process.env.LOOPZERO_CLOUD_URL) {
    const url = process.env.LOOPZERO_CLOUD_URL;
    if (!url.startsWith('http://') && !url.startsWith('https://')) {
      throw new Error(`Invalid LOOPZERO_CLOUD_URL: must start with http:// or https://. Got: ${url}`);
    }
    return url;
  }
  // Default: same as API URL (Traefik handles routing)
  return getDefaultApiUrl();
}

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

