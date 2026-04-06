// API client for the LoopZero dashboard.
// Uses Kratos session cookies — no Bearer tokens.

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:13002';
const ENGINE_API = import.meta.env.VITE_ENGINE_URL || 'http://localhost:13001';

export function getApiBase() { return API_BASE; }
export function getEngineBase() { return ENGINE_API; }

export async function cloudFetch<T>(path: string, options: RequestInit = {}): Promise<T | null> {
  return apiFetch<T>(API_BASE, path, { ...options, redirectOnAuth: true });
}

export async function engineFetch<T>(path: string, options: RequestInit = {}): Promise<T | null> {
  // Engine API uses PAT tokens, not Kratos sessions.
  // Don't redirect to login on 401 — the session may be valid but
  // the engine doesn't accept session cookies. Treat as data error.
  return apiFetch<T>(ENGINE_API, path, { ...options, redirectOnAuth: false });
}

interface FetchOptions extends RequestInit {
  redirectOnAuth?: boolean;
}

async function apiFetch<T>(base: string, path: string, options: FetchOptions = {}): Promise<T | null> {
  const { redirectOnAuth = true, ...fetchOpts } = options;
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(fetchOpts.headers as Record<string, string> || {}),
  };

  try {
    const resp = await fetch(`${base}${path}`, {
      ...fetchOpts,
      headers,
      credentials: 'include',
    });
    if (resp.status === 401) {
      if (redirectOnAuth) {
        window.location.href = '/login';
      }
      return null;
    }
    if (!resp.ok) return null;
    return await resp.json() as T;
  } catch {
    return null;
  }
}
