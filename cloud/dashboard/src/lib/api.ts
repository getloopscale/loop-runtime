// API client for the LoopZero dashboard.
// Uses Kratos session cookies — no Bearer tokens.

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:13002';
const ENGINE_API = import.meta.env.VITE_ENGINE_URL || 'http://localhost:13001';

export function getApiBase() { return API_BASE; }
export function getEngineBase() { return ENGINE_API; }

export async function cloudFetch<T>(path: string, options: RequestInit = {}): Promise<T | null> {
  return apiFetch<T>(API_BASE, path, options);
}

export async function engineFetch<T>(path: string, options: RequestInit = {}): Promise<T | null> {
  return apiFetch<T>(ENGINE_API, path, options);
}

async function apiFetch<T>(base: string, path: string, options: RequestInit = {}): Promise<T | null> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options.headers as Record<string, string> || {}),
  };

  try {
    const resp = await fetch(`${base}${path}`, {
      ...options,
      headers,
      credentials: 'include', // Send session cookie
    });
    if (resp.status === 401) {
      window.location.href = '/login';
      return null;
    }
    if (!resp.ok) return null;
    return await resp.json() as T;
  } catch {
    return null;
  }
}
