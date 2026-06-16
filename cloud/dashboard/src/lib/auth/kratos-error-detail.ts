import type { AuthFailureReason } from '@/lib/rum';

/**
 * Kratos error-detail helpers extracted from pages/login.tsx (T3.1 of
 * plan 2026-06-16-m0-j01-login-polish) per ADR D6 (login.tsx LOC budget).
 *
 * `safeErrorDetail` and `classifyKratosErrorReason` are sibling helpers:
 * one returns user-facing copy, the other returns a discriminated reason
 * for telemetry. Both are pure functions; they don't replace
 * mapKratosError (lib/auth/error-mapper.ts) — they operate on the
 * `ui.messages` array shape that Kratos returns mid-flow, whereas
 * mapKratosError handles the top-level error envelope at submit time.
 */

/**
 * safeErrorDetail — OWASP A07: never expose raw Kratos error text to UI.
 *
 * Priority order (first match wins, EC-2 from edge-case review):
 *   1. csrf            → "Your session expired. Try again."
 *   2. locked          → "This account is locked. Reset your password to continue."
 *   3. provider (OIDC) → "Try a different sign-in method."
 *   4. default         → undefined (no description, generic title only)
 *
 * EC-3: tolerates null/empty text without TypeError via (text ?? '').
 */
export function safeErrorDetail(
  errors: { text: string | null }[] | undefined,
): string | undefined {
  if (!errors || errors.length === 0) return undefined;
  const haystack = errors
    .map((e) => (e.text ?? '').toLowerCase())
    .join(' ');
  if (haystack.includes('csrf')) return 'Your session expired. Try again.';
  if (haystack.includes('locked'))
    return 'This account is locked. Reset your password to continue.';
  if (haystack.includes('provider')) return 'Try a different sign-in method.';
  return undefined;
}

/**
 * Classify Kratos validation error into a discriminated reason for telemetry.
 * Currently unused because submitPassword switched from inline-XHR to
 * top-level form POST (2026-06-01 SameSite-Lax security fix); the landing
 * page after submission classifies via flow.ui.messages on remount.
 * Kept exported so the type contract is still available for the post-mount
 * classification code path when it lands.
 */
export function classifyKratosErrorReason(
  errors: { text: string | null }[] | undefined,
): AuthFailureReason {
  if (!errors || errors.length === 0) return 'kratos_validation';
  const haystack = errors
    .map((e) => (e.text ?? '').toLowerCase())
    .join(' ');
  if (haystack.includes('csrf')) return 'csrf_expired';
  return 'kratos_validation';
}

/**
 * Detect the pre-seeded identity / OIDC-credential-missing collision
 * scenario (EC-J01-1 absorption from blueprint Coverage Corner 4).
 * Kratos surfaces this via several phrasings; we match on stable
 * substrings rather than exact IDs.
 */
export function isEnumerationCollision(
  errors: { text: string | null }[] | undefined,
): boolean {
  if (!errors || errors.length === 0) return false;
  const haystack = errors
    .map((e) => (e.text ?? '').toLowerCase())
    .join(' ');
  return (
    haystack.includes('enumeration') ||
    haystack.includes('does not match') ||
    haystack.includes('account exists')
  );
}
