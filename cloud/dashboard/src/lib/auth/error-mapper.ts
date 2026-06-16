// Maps auth-service error IDs to product-language messages for the login UI.
//
// Plan: .claude/knowledge-base/plans/2026-06-16-m0-j01-login-polish-plan.md
// v1.1 (SHIPPABLE 100.0), Phase 1 Task T1.1.
//
// ADR D1: pure function over hook (single concern — table lookup + fallback).
// ADR D2: console.error the raw input BEFORE returning the product-language
//         tuple, so operators debugging a LIVE-caught user report can paste
//         the DevTools output. Defense vs the J-05 silent-502 pattern.
// ADR D3: pre-seeded identity collision (EC-J01-1) returns a distinct
//         message, NOT the generic "Login failed" fallback.
// ADR D4: 422 + redirect_browser_to chain is detected here so the caller
//         can follow the next-step URL instead of treating 422 as failure.
//
// EC-1 input contract: tolerates null / undefined / missing-error /
// partial-error without throwing. The contract type is exported for callers.

/**
 * The shape of the raw error/response payload returned by the auth service.
 * Kratos browser-mode produces at least 3 shapes: { error: {...} } for the
 * canonical error envelope, { redirect_browser_to: '...' } for 422+redirect,
 * and null/undefined for network failures. All three branches are honored.
 */
export interface KratosErrorInput {
  error?: { id?: string; code?: string | number; message?: string };
  redirect_browser_to?: string;
}

/**
 * The product-language tuple returned to UI consumers. `redirectTo` is
 * populated only for the 422 redirect-chain branch (ADR D4); otherwise
 * undefined. `retryable=true` signals to the UI that a retry CTA should
 * be offered (e.g., the NetworkRetryButton).
 */
export interface KratosErrorMapped {
  message: string;
  suggestion: string;
  retryable: boolean;
  redirectTo?: string;
}

const GENERIC_FALLBACK: KratosErrorMapped = {
  message: 'We could not complete the request. Try again or contact support.',
  suggestion: 'Refresh the page and retry. If the problem persists, contact support.',
  retryable: false,
};

/**
 * Canonical table — every documented v26 error ID maps to a product-language
 * tuple. See plan Appendix A for the catalog (12 IDs: 6 starter + 6
 * extensions). Drift between the table and the catalog is asserted by T1.2.
 *
 * Rule 3 compliance: no internal terms (auth-service names, third-party
 * vendor names) leak into the message/suggestion fields. The user sees
 * product copy, never wire-protocol nouns.
 */
export const KRATOS_ERROR_MAP: Record<string, KratosErrorMapped> = {
  browser_location_change_required: {
    message: 'Redirecting to complete sign-in.',
    suggestion: 'Wait a moment while the browser redirects.',
    retryable: false,
  },
  session_inactive: {
    message: 'Your session expired.',
    suggestion: 'Sign in again to continue.',
    retryable: true,
  },
  security_csrf_violation: {
    message: 'Security check failed.',
    suggestion: 'Refresh the page and try signing in again.',
    retryable: true,
  },
  security_identity_mismatch: {
    message: 'This account does not match the active session.',
    suggestion: 'Sign out and sign back in with the right account.',
    retryable: false,
  },
  oauth2_authorization_denied: {
    message: 'Authorization was denied by the provider.',
    suggestion: 'Try again, or use a different sign-in method.',
    retryable: true,
  },
  account_enumeration_mitigation: {
    message: 'This email is registered without social login. Use a different email or contact support.',
    suggestion: 'Sign in with the original method, or contact support to link your account.',
    retryable: false,
  },
  self_service_flow_expired: {
    message: 'Your sign-in attempt expired.',
    suggestion: 'Start a fresh sign-in.',
    retryable: true,
  },
  session_already_available: {
    message: 'You are already signed in.',
    suggestion: 'Continue to your dashboard.',
    retryable: false,
  },
  session_aal2_required: {
    message: 'Additional verification is required for this action.',
    suggestion: 'Complete the second-factor challenge to continue.',
    retryable: true,
  },
  session_refresh_required: {
    message: 'For your security, please sign in again.',
    suggestion: 'Re-enter your credentials to confirm it is you.',
    retryable: true,
  },
  internal_server_error: {
    message: 'Auth service temporarily unavailable. Try again.',
    suggestion: 'Wait a moment and retry. If the problem persists, contact support.',
    retryable: true,
  },
  rate_limit_exceeded: {
    message: 'Too many attempts. Please wait before retrying.',
    suggestion: 'Wait a minute, then try again.',
    retryable: true,
  },
};

/**
 * Map a raw auth-service error payload to a product-language tuple.
 *
 * ADR D2: writes the raw payload to console.error BEFORE returning, for
 * DevTools debuggability.
 *
 * EC-1: tolerates every documented input shape (null/undefined/missing/
 * partial). Returns the GENERIC_FALLBACK for any unrecognized branch
 * with retryable=false to prevent retry storms.
 *
 * ADR D4: when the input carries a redirect_browser_to, the caller is
 * expected to follow the URL. The mapper surfaces it via `redirectTo`
 * but the caller (login.tsx T3.3) is responsible for same-origin
 * validation via `safe-redirect.ts` BEFORE navigating.
 */
export function mapKratosError(input: KratosErrorInput | null | undefined): KratosErrorMapped {
  console.error('[auth.error-mapper]', input);

  if (input == null) {
    return { ...GENERIC_FALLBACK };
  }

  if (typeof input.redirect_browser_to === 'string' && input.redirect_browser_to.length > 0) {
    return {
      message: 'Redirecting to complete sign-in.',
      suggestion: 'Wait a moment while the browser redirects.',
      retryable: false,
      redirectTo: input.redirect_browser_to,
    };
  }

  const id = input.error?.id;
  if (typeof id === 'string' && id in KRATOS_ERROR_MAP) {
    return { ...KRATOS_ERROR_MAP[id] };
  }

  return { ...GENERIC_FALLBACK };
}
