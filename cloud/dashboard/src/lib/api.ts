// Real API client for the LoopZero Cloud service.
// All dashboard API calls go through this client with real authentication.
//
// Security: access and refresh tokens are kept in memory only (not localStorage)
// to prevent XSS exfiltration. Page refresh requires re-authentication via PKCE
// (Hydra session makes this transparent — no password re-entry).

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:13002';
const ENGINE_API = import.meta.env.VITE_ENGINE_URL || 'http://localhost:13001';

// In-memory token storage — cleared on page refresh (XSS-safe)
let accessToken: string | null = null;
let refreshToken: string | null = null;

export function getApiBase() { return API_BASE; }
export function getEngineBase() { return ENGINE_API; }

export async function cloudFetch<T>(path: string, options: RequestInit = {}): Promise<T | null> {
  return apiFetch<T>(API_BASE, path, options);
}

export async function engineFetch<T>(path: string, options: RequestInit = {}): Promise<T | null> {
  return apiFetch<T>(ENGINE_API, path, options);
}

async function apiFetch<T>(base: string, path: string, options: RequestInit = {}): Promise<T | null> {
  const token = getToken();
  if (!token && !path.startsWith('/v1/auth/')) {
    window.location.href = '/login';
    return null;
  }

  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
    ...(options.headers as Record<string, string> || {}),
  };

  try {
    const resp = await fetch(`${base}${path}`, { ...options, headers });
    if (resp.status === 401) {
      clearAuth();
      window.location.href = '/login';
      return null;
    }
    if (!resp.ok) return null;
    return await resp.json() as T;
  } catch {
    return null;
  }
}

export function getToken(): string | null {
  return accessToken;
}

export function getRefreshToken(): string | null {
  return refreshToken;
}

export function setAuth(token: string, refresh: string, email: string, name?: string) {
  accessToken = token;
  refreshToken = refresh;
  // Non-sensitive user info — safe in localStorage for display after refresh
  localStorage.setItem('loopzero_email', email);
  if (name) localStorage.setItem('loopzero_name', name);
}

export function clearAuth() {
  accessToken = null;
  refreshToken = null;
  localStorage.removeItem('loopzero_email');
  localStorage.removeItem('loopzero_name');
}

export function getEmail(): string | null {
  return localStorage.getItem('loopzero_email');
}

export function getName(): string | null {
  return localStorage.getItem('loopzero_name');
}

export function isAuthenticated(): boolean {
  return !!getToken();
}
