// Kratos session-based authentication for the LoopZero dashboard.
//
// The dashboard is a first-party SPA — uses Kratos sessions directly.
// No Hydra, no PKCE, no tokens. Just HTTP-only session cookies.
//
// Hydra is only used for the CLI (Device Code Flow).

const KRATOS_URL = import.meta.env.VITE_API_URL || 'http://localhost:13002';

export interface VerifiableAddress {
  value: string;
  verified: boolean;
  via: string;
}

export interface Session {
  active: boolean;
  identity: {
    id: string;
    traits: {
      email: string;
      name?: string;
    };
    verifiable_addresses?: VerifiableAddress[];
  };
}

/**
 * Check if the user's email is verified.
 * OIDC users (GitHub/Google) don't have verifiable_addresses — treated as verified
 * since the provider already verified their email.
 */
export function isEmailVerified(session: Session): boolean {
  const addrs = session.identity.verifiable_addresses;
  if (!addrs || addrs.length === 0) return true; // OIDC — pre-verified
  return addrs.some(a => a.verified);
}

/** Check if user has an active session. Returns session or null. */
export async function getSession(): Promise<Session | null> {
  try {
    const resp = await fetch(`${KRATOS_URL}/sessions/whoami`, {
      credentials: 'include',
      headers: { Accept: 'application/json' },
    });
    if (!resp.ok) return null;
    const session = await resp.json();
    return session.active ? session : null;
  } catch {
    return null;
  }
}

/** Start login flow — redirect to Kratos login UI. */
export function startLogin(): void {
  window.location.href = `${KRATOS_URL}/self-service/login/browser`;
}

/** Start registration flow. */
export function startRegistration(): void {
  window.location.href = `${KRATOS_URL}/self-service/registration/browser`;
}

/** Get verification URL — redirects to Kratos verification flow. */
export function getVerificationURL(): string {
  return `${KRATOS_URL}/self-service/verification/browser`;
}

/** Logout — destroy Kratos session. */
export async function logout(): Promise<void> {
  try {
    const resp = await fetch(`${KRATOS_URL}/self-service/logout/browser`, {
      credentials: 'include',
      headers: { Accept: 'application/json' },
    });
    if (resp.ok) {
      const data = await resp.json();
      if (data.logout_url) {
        window.location.href = data.logout_url;
        return;
      }
    }
  } catch {
    // Fallback
  }
  window.location.href = '/login';
}
