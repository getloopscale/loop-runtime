// API client for the LoopZero Cloud service.
// All dashboard API calls go through this client.

const API_BASE = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:13002';

export async function apiFetch<T>(path: string, options: RequestInit = {}): Promise<T | null> {
  const token = getToken();
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
    ...(options.headers as Record<string, string> || {}),
  };

  try {
    const resp = await fetch(`${API_BASE}${path}`, { ...options, headers });
    if (resp.status === 401) {
      // Token expired — redirect to login
      if (typeof window !== 'undefined') {
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

function getToken(): string | null {
  if (typeof window === 'undefined') return null;
  return document.cookie
    .split('; ')
    .find(c => c.startsWith('loopzero_token='))
    ?.split('=')[1] || null;
}

export function setToken(token: string) {
  document.cookie = `loopzero_token=${token}; path=/; max-age=86400; SameSite=Strict; Secure`;
}

export function clearToken() {
  document.cookie = 'loopzero_token=; path=/; max-age=0';
}
