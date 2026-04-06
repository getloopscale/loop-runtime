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

export interface AuthenticationMethod {
  method: string;
  provider?: string;
  completed_at?: string;
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
  authentication_methods?: AuthenticationMethod[];
}

// OIDC providers whose email verification we trust (MEET-037 security mandate).
const TRUSTED_OIDC_PROVIDERS = ['github', 'google'];

/**
 * Check if the user's email is verified.
 *
 * Three cases:
 * 1. No verifiable_addresses → OIDC user without Kratos-managed email → verified
 * 2. Authenticated via trusted OIDC provider (GitHub/Google) → verified
 *    (provider already verified the email; Kratos marks it unverified because
 *    the mapper doesn't pass email_verified — MEET-037)
 * 3. Otherwise → check verifiable_addresses for at least one verified entry
 */
export function isEmailVerified(session: Session): boolean {
  const addrs = session.identity.verifiable_addresses;
  if (!addrs || addrs.length === 0) return true;

  // If any address is already verified in Kratos, short-circuit
  if (addrs.some(a => a.verified)) return true;

  // Check if authenticated via a trusted OIDC provider
  const methods = session.authentication_methods;
  if (methods) {
    const hasTrustedOIDC = methods.some(
      m => m.method === 'oidc' && TRUSTED_OIDC_PROVIDERS.includes(m.provider ?? ''),
    );
    if (hasTrustedOIDC) return true;
  }

  return false;
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
