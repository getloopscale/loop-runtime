// Runtime configuration loader.
//
// In production, the container entrypoint generates /__config.js from env vars.
// In development, the file in public/__config.js has empty defaults (same-origin).
// This module reads from window.__LOOPZERO_CONFIG__ (set by the script tag in index.html).

interface LoopZeroConfig {
  API_URL: string;
  ENGINE_URL: string;
  AUTH_URL: string;
  ACCOUNTS_URL: string;
}

declare global {
  interface Window {
    __LOOPZERO_CONFIG__?: LoopZeroConfig;
  }
}

function getConfig(): LoopZeroConfig {
  return window.__LOOPZERO_CONFIG__ ?? {
    API_URL: "",
    ENGINE_URL: "",
    AUTH_URL: "",
    ACCOUNTS_URL: "",
  };
}

/** Base URL for API calls (cloud service). Empty = same-origin. */
export const API_URL = getConfig().API_URL;

/** Base URL for engine API. Empty = same-origin. */
export const ENGINE_URL = getConfig().ENGINE_URL;

/** Base URL for auth (Kratos). Empty = same-origin. */
export const AUTH_URL = getConfig().AUTH_URL;

/** Base URL for accounts. Empty = same-origin. */
export const ACCOUNTS_URL = getConfig().ACCOUNTS_URL;
