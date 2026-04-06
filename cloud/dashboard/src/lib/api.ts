// API client for the LoopZero dashboard.
//
// Auth flow (BFF pattern):
//   1. Dashboard authenticates via Kratos (session cookies)
//   2. POST /v1/auth/session/token exchanges cookies → JWT
//   3. All API calls use Authorization: Bearer <jwt>
//
// This normalizes auth: CLI (Hydra JWT) and dashboard (session JWT)
// both use Bearer tokens. One auth method for all endpoints.

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:13002';
const ENGINE_API = import.meta.env.VITE_ENGINE_URL || 'http://localhost:13001';

export function getApiBase() { return API_BASE; }
export function getEngineBase() { return ENGINE_API; }

// In-memory JWT token (never localStorage — XSS risk)
let accessToken: string | null = null;
let tokenExpiresAt = 0;

/** Exchange Kratos session cookie for a JWT. Called after login. */
export async function exchangeSessionToken(): Promise<boolean> {
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
    return true;
  } catch {
    return false;
  }
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

/** Clear token (on logout). */
export function clearToken(): void {
  accessToken = null;
  tokenExpiresAt = 0;
}

export async function cloudFetch<T>(path: string, options: RequestInit = {}): Promise<T | null> {
  const token = await ensureToken();
  if (!token) {
    window.location.href = '/login';
    return null;
  }
  return apiFetch<T>(API_BASE, path, token, options);
}

export async function engineFetch<T>(_path: string, _options: RequestInit = {}): Promise<T | null> {
  // Engine API not yet integrated with dashboard JWT — return null gracefully
  return null;
}

async function apiFetch<T>(base: string, path: string, token: string, options: RequestInit = {}): Promise<T | null> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${token}`,
    ...(options.headers as Record<string, string> || {}),
  };

  try {
    const resp = await fetch(`${base}${path}`, { ...options, headers });
    if (resp.status === 401) {
      // Token expired — try refresh once
      accessToken = null;
      const newToken = await ensureToken();
      if (!newToken) {
        window.location.href = '/login';
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
