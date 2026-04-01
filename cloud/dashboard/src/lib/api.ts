// Real API client for the LoopZero Cloud service.
// All dashboard API calls go through this client with real authentication.

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

// Token storage — HttpOnly cookie preferred, localStorage as fallback for SPA
export function getToken(): string | null {
  return localStorage.getItem('loopzero_token');
}

export function setAuth(token: string, refreshToken: string, email: string) {
  localStorage.setItem('loopzero_token', token);
  localStorage.setItem('loopzero_refresh_token', refreshToken);
  localStorage.setItem('loopzero_email', email);
}

export function clearAuth() {
  localStorage.removeItem('loopzero_token');
  localStorage.removeItem('loopzero_refresh_token');
  localStorage.removeItem('loopzero_email');
}

export function getEmail(): string | null {
  return localStorage.getItem('loopzero_email');
}

export function isAuthenticated(): boolean {
  return !!getToken();
}
