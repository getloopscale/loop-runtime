/**
 * sanitizeReturnTo — OWASP A01 boundary helper.
 *
 * Plan v1.1 Phase 2 Task T2.3 / EC-3 absorption (return_to XSS + open
 * redirect). The `return_to` query param arrives from external links
 * (email, social share) controlled by attackers; treating it as
 * trusted enables (a) javascript:/data:/vbscript: XSS payloads and
 * (b) cross-origin redirects to phishing pages after a successful
 * login. This helper is the canonical sanitization boundary.
 *
 * Rules (per plan ADR D6 + EC-3):
 *  - null/undefined/empty → fallback `/projects`
 *  - javascript:/data:/vbscript: (case-insensitive) → fallback
 *  - off-origin absolute URLs → fallback
 *  - protocol-relative URLs (`//host/...`) → fallback (default to off-origin)
 *  - relative path (`/...`) → returned as-is
 *  - same-origin absolute URL → returned as-is
 *
 * The fallback `/projects` is the canonical dashboard landing.
 */
const FALLBACK = '/projects';

// Protocols banned outright. Case-insensitive prefix match after trim.
const DANGEROUS_PROTOCOLS = ['javascript:', 'data:', 'vbscript:', 'file:'];

export function sanitizeReturnTo(raw: string | null | undefined): string {
  if (raw == null || raw === '') {
    return FALLBACK;
  }
  const trimmed = raw.trim();
  if (trimmed === '') {
    return FALLBACK;
  }
  const lower = trimmed.toLowerCase();
  for (const proto of DANGEROUS_PROTOCOLS) {
    if (lower.startsWith(proto)) {
      return FALLBACK;
    }
  }
  // Protocol-relative URL — assume off-origin and reject. (If the operator
  // genuinely wants `//host/path` they can drop the leading slash.)
  if (trimmed.startsWith('//')) {
    return FALLBACK;
  }
  // Relative path (canonical safe case).
  if (trimmed.startsWith('/')) {
    return trimmed;
  }
  // Absolute URL — parse and verify same origin.
  try {
    const url = new URL(trimmed);
    if (typeof window !== 'undefined' && url.origin === window.location.origin) {
      return trimmed;
    }
  } catch {
    // Unparseable — fall through to fallback.
  }
  return FALLBACK;
}
