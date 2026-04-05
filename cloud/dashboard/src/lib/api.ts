// API client for the LoopZero Cloud dashboard.
//
// Token storage strategy:
// - Access token in sessionStorage (survives redirects, cleared on tab close)
// - NOT localStorage (XSS risk for long-lived tokens)
// - NOT in-memory (lost on OAuth redirects)

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

// --- Auth state (sessionStorage — survives OAuth redirects) ---

export function getToken(): string | null {
  return sessionStorage.getItem('loopzero_token');
}

export function setAuth(token: string, refresh: string, email: string, name?: string) {
  sessionStorage.setItem('loopzero_token', token);
  sessionStorage.setItem('loopzero_refresh', refresh);
  sessionStorage.setItem('loopzero_email', email);
  if (name) sessionStorage.setItem('loopzero_name', name);
}

export function clearAuth() {
  sessionStorage.removeItem('loopzero_token');
  sessionStorage.removeItem('loopzero_refresh');
  sessionStorage.removeItem('loopzero_email');
  sessionStorage.removeItem('loopzero_name');
}

export function getEmail(): string | null {
  return sessionStorage.getItem('loopzero_email');
}

export function getName(): string | null {
  return sessionStorage.getItem('loopzero_name');
}

export function isAuthenticated(): boolean {
  return !!getToken();
}
