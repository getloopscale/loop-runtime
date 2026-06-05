// Kratos session-based authentication for the LoopZero dashboard.
//
// The dashboard is a first-party SPA — uses Kratos sessions directly.
// No Hydra, no PKCE, no tokens. Just HTTP-only session cookies.
//
// Hydra is only used for the CLI (Device Code Flow).
//
// SW INVARIANT (T4.2 + EC-04): Service Worker at vite.config.ts MUST never
// cache auth/session endpoints (/api/v1/auth/*, /sessions/*, /self-service/*,
// /oauth2/*). Stale token = security hole (UI shows logged-in but session is
// gone). Always NetworkOnly handler for these patterns.

// AUTH_URL is the dedicated Kratos URL. Falls back to API_URL when AUTH_URL
// isn't set (dev / same-origin). Production with split domains (Kratos at
// accounts.loopzero.dev vs API at api.loopzero.dev) requires AUTH_URL set explicitly
// via /__config.js — without this fallback the auth flow breaks silently.
import { API_URL, AUTH_URL } from './config';
import { mocksEnabled, getMockSession } from './dev-mocks';
const KRATOS_URL = AUTH_URL || API_URL;

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

/** Check if user has an active session. Returns session or null.
 *
 * SECURITY (2026-06-01): Calls Kratos directly via cross-subdomain XHR with
 * credentials:include. Cookie is scoped Domain=loopzero.example so it travels on
 * same-site (eTLD+1) requests; SameSite=Lax allows this per RFC6265bis §5.5.7.
 * CORS Access-Control-Allow-Origin includes the dashboard origin.
 *
 * If 401 with a stale cookie or missing cookie, returns null and caller
 * (useRequireAuth) routes to /login. Console diagnostics emitted to make
 * "why am I being kicked to /login" actionable in DevTools.
 */
export async function getSession(): Promise<Session | null> {
  // Visual-validation mock layer (dev-only, gated by VITE_USE_MOCKS env var,
  // dead-code-eliminated in production builds). See lib/dev-mocks.ts.
  if (mocksEnabled()) return getMockSession();

  try {
    const resp = await fetch(`${KRATOS_URL}/sessions/whoami`, {
      credentials: 'include',
      headers: { Accept: 'application/json' },
    });
    if (!resp.ok) {
      // eslint-disable-next-line no-console
      console.warn(
        `[auth] getSession: Kratos returned HTTP ${resp.status} (no active session). ` +
          `If you just registered/logged in but see this, the session cookie may not have been ` +
          `set (open DevTools → Application → Cookies → loopzero.example and look for ory_kratos_session). ` +
          `Clear cookies and re-login to reset.`,
      );
      return null;
    }
    const session = await resp.json();
    if (!session.active) {
      // eslint-disable-next-line no-console
      console.warn('[auth] getSession: Kratos returned session but active=false', session);
      return null;
    }
    return session;
  } catch (err) {
    // eslint-disable-next-line no-console
    console.error('[auth] getSession: network/parse error', err);
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

/** Logout — destroy Kratos session, clear JWT, and notify other tabs. */
export async function logout(): Promise<void> {
  // Clear in-memory JWT and broadcast to other tabs (§3.3 + Fix #5)
  const { logout: apiLogout } = await import('./api');
  apiLogout();

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
