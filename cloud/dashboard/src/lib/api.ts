// API client for the LoopZero dashboard.
//
// Auth flow (BFF pattern):
//   1. Dashboard authenticates via Kratos (session cookies)
//   2. POST /v1/auth/session/token exchanges cookies → JWT
//   3. All API calls use Authorization: Bearer <jwt>
//
// §3.3: BroadcastChannel coordinates token state across browser tabs.
// §3.6: Session expiry shows notification before redirect.

// MEET-040: Single origin. Dashboard served at cloud.{domain} — all API calls are relative.
// Runtime config via /__config.js (empty in prod = same-origin, set per-env in container).
import { API_URL } from './config';
import { mocksEnabled } from './dev-mocks';
const API_BASE = API_URL;

export function getApiBase() { return API_BASE; }

// In-memory JWT token (never localStorage — XSS risk)
let accessToken: string | null = null;
let tokenExpiresAt = 0;
let refreshPromise: Promise<boolean> | null = null; // Dedup concurrent refreshes

// §3.3: BroadcastChannel for cross-tab coordination.
// SECURITY: the JWT is NEVER broadcast. The token lives only in this tab's
// memory; broadcasting it would let any same-origin XSS subscribe to the channel
// and exfiltrate tokens from every open tab. Each tab mints its own token from
// its own Kratos session cookie via exchangeSessionToken(). Only logout (which
// carries no secret) is coordinated across tabs.
type AuthMessage = { type: 'logged-out' };

let authChannel: BroadcastChannel | null = null;
try {
  authChannel = new BroadcastChannel('loopzero-auth');
  authChannel.onmessage = (event: MessageEvent<AuthMessage>) => {
    const msg = event.data;
    if (msg.type === 'logged-out') {
      // Another tab logged out — clear this tab's token and redirect.
      accessToken = null;
      tokenExpiresAt = 0;
      redirectToLogin('expired');
    }
  };
} catch {
  // BroadcastChannel not available (e.g., some workers) — graceful degradation
}

/** Exchange Kratos session cookie for a JWT. Called after login. */
export async function exchangeSessionToken(): Promise<boolean> {
  // Visual-validation mock layer (dev-only, gated by VITE_USE_MOCKS).
  // Set a fake token so hasValidToken() returns true and the dashboard
  // proceeds without trying to hit the real BFF endpoint.
  if (mocksEnabled()) {
    accessToken = 'mock-jwt-for-visual-validation';
    tokenExpiresAt = Date.now() + 3600 * 1000;
    return true;
  }

  // Dedup: if a refresh is already in flight, reuse it
  if (refreshPromise) return refreshPromise;

  refreshPromise = (async () => {
    try {
      const resp = await fetch(`${API_BASE}/v1/auth/session/token`, {
        method: 'POST',
        credentials: 'include', // Send Kratos session cookie
        headers: { 'Content-Type': 'application/json' },
      });
      if (!resp.ok) return false;

      const data = await resp.json() as { access_token: string; expires_in: number };
      accessToken = data.access_token;
      tokenExpiresAt = Date.now() + (data.expires_in * 1000) - 60000; // 60s buffer

      // SECURITY: do NOT broadcast the token. Each tab exchanges its own token
      // from its Kratos session cookie on demand (see ensureToken).
      return true;
    } catch {
      return false;
    } finally {
      refreshPromise = null;
    }
  })();

  return refreshPromise;
}

/** Check if we have a valid token. */
export function hasValidToken(): boolean {
  return accessToken !== null && Date.now() < tokenExpiresAt;
}

/** Ensure we have a valid token, refreshing if needed. */
async function ensureToken(): Promise<string | null> {
  if (hasValidToken()) return accessToken;
  const ok = await exchangeSessionToken();
  return ok ? accessToken : null;
}

/**
 * Exposed for SSE consumers (use-log-stream) that need the JWT inline in
 * the URL query string because EventSource cannot set headers.
 * T6.0 of plan 2026-05-27-backend-frontend-integration v1.1 (EC-04).
 */
export async function ensureSSEToken(): Promise<string | null> {
  return ensureToken();
}

let redirecting = false;

/** §3.6: Show notification before redirect, with reason in query param. */
function redirectToLogin(reason?: string): void {
  if (redirecting) return;
  redirecting = true;

  const params = reason ? `?reason=${reason}` : '';

  // §3.6: Brief delay so user sees the notification
  if (reason === 'expired') {
    // Show a visible indicator before redirect
    const toast = document.createElement('div');
    toast.style.cssText = 'position:fixed;top:16px;right:16px;background:#1a1a1a;color:white;padding:12px 20px;border-radius:8px;z-index:9999;font-size:14px;box-shadow:0 4px 12px rgba(0,0,0,0.3);';
    toast.textContent = 'Session expired. Redirecting to login...';
    document.body.appendChild(toast);
    setTimeout(() => { window.location.href = `/login${params}`; }, 2000);
  } else {
    window.location.href = `/login${params}`;
  }
}

/** Clear token (on logout or login page visit). */
export function clearToken(): void {
  accessToken = null;
  tokenExpiresAt = 0;
}

/** Logout — clears token and broadcasts to other tabs. */
export function logout(): void {
  clearToken();
  // §3.3: Broadcast logout to all tabs
  authChannel?.postMessage({ type: 'logged-out' } satisfies AuthMessage);
}

// MEET-040: Unified API fetch. All paths relative to same origin.
// /v1/billing, /v1/auth/* → cloud service (direct, same origin)
// /engine/api/v1/* → cloud service proxy → engine API (BFF pattern)
export async function cloudFetch<T>(path: string, options: RequestInit = {}): Promise<T | null> {
  // Plan v1.0 (2026-05-26): mock mode no longer short-circuits via FIXTURES
  // map. Instead, lib/mocks/server.ts wraps window.fetch and intercepts all
  // /api/v1/* paths transparently — covering 47+ endpoints vs the old 5.
  // The cloudFetch flow stays identical in mock + prod modes; the difference
  // is purely WHO answers the network call.
  //
  // Plan dashboard-memory-bff-server-side-key-vault v1.2 § ADR D5 + T4.1:
  // Browser-trust calls to /v1/memory/* are transparently rewritten to
  // /v1/dashboard/memory/* where the cloud BFF layer mints a server-side
  // loopzero_live_* key — the browser NEVER sees that key.
  // SDK consumers (non-browser) still hit /v1/memory/* directly with their own key.
  //
  // EC-6 path traversal defense: reject paths containing '..' to prevent
  // /v1/memory/../admin/something escaping the rewrite intent. The predicate
  // is conservative — any '..' substring blocks the rewrite AND falls through
  // to the original path which downstream chi-router will 404 (correct fail-fast).
  if (path.startsWith('/v1/memory/') && !path.includes('..')) {
    path = '/v1/dashboard/memory/' + path.slice('/v1/memory/'.length);
  }
  const token = await ensureToken();
  if (!token) {
    redirectToLogin('expired');
    return null;
  }
  return fetchWithAuth<T>(API_BASE, path, token, options, true);
}

// Alias for backward compatibility during migration
export { cloudFetch as apiFetch };

// Engine API calls proxied through cloud service.
export async function engineFetch<T>(path: string, options: RequestInit = {}): Promise<T | null> {
  return cloudFetch<T>(`/engine${path}`, options);
}

async function fetchWithAuth<T>(base: string, path: string, token: string, options: RequestInit = {}, redirectOnAuthFailure = true): Promise<T | null> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${token}`,
    ...(options.headers as Record<string, string> || {}),
  };

  // T5.0 of plan 2026-05-27-backend-frontend-integration v1.1 (EC-09).
  // Auto-generate Idempotency-Key for every mutating verb so retries on
  // network blips do not duplicate state. Caller can override with an
  // explicit `headers['Idempotency-Key']` to make a deliberate replay
  // (e.g., resume across reload).
  const method = (options.method || 'GET').toUpperCase();
  if (['POST', 'PATCH', 'PUT', 'DELETE'].includes(method) && !headers['Idempotency-Key']) {
    headers['Idempotency-Key'] = crypto.randomUUID();
  }

  try {
    const resp = await fetch(`${base}${path}`, { ...options, headers });
    if (resp.status === 401) {
      if (!redirectOnAuthFailure) return null;
      // Token expired — try refresh once
      accessToken = null;
      const newToken = await ensureToken();
      if (!newToken) {
        redirectToLogin('expired');
        return null;
      }
      // Retry with new token
      headers['Authorization'] = `Bearer ${newToken}`;
      const retry = await fetch(`${base}${path}`, { ...options, headers });
      if (!retry.ok) return null;
      return await retry.json() as T;
    }
    if (!resp.ok) return null;
    return await resp.json() as T;
  } catch {
    return null;
  }
}
// build trigger 1775591972
