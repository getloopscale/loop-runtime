/**
 * isSafeRedirectUrl — OWASP A01 boundary helper for server-supplied
 * redirect URLs (e.g., Kratos 422 + redirect_browser_to).
 *
 * Plan v1.1 Phase 3 Task T3.3 / EC-2 absorption (open-redirect via
 * 422+redirect_to). Kratos itself enforces allowlisting at the config
 * level, but client-side defense-in-depth is canonical: a misconfigured
 * auth service OR an injected response cannot phishing-redirect the user.
 *
 * Allowlist rule:
 *   - URL parses successfully via `new URL(url)`, AND
 *   - protocol is `https:` (canonical hosts) OR matches `window.location.origin`, AND
 *   - origin is one of `SAFE_REDIRECT_ORIGINS` OR equals `window.location.origin`
 *
 * The allowlist is exported as a const for grep-auditability — code
 * review can verify the set without parsing the function body.
 */
export const SAFE_REDIRECT_ORIGINS: readonly string[] = [
  // Dev-public canonical hosts (per Caddy / Cloudflare wildcard chain).
  'https://auth-dev.loopzero.example',
  'https://app-dev.loopzero.example',
  'https://cloud-dev.loopzero.example',
  // Production hosts (future-ready; harmless on dev because window.origin won't match).
  'https://auth.loopzero.example',
  'https://app.loopzero.example',
  'https://cloud.loopzero.example',
];

export function isSafeRedirectUrl(raw: string | null | undefined): boolean {
  if (raw == null || raw === '') {
    return false;
  }
  let url: URL;
  try {
    url = new URL(raw);
  } catch {
    return false;
  }
  // Reject any non-https scheme against the allowlist; same-origin can
  // also be http during local dev — handled by the origin compare below.
  if (url.protocol !== 'https:' && typeof window !== 'undefined') {
    // Same-origin http (local dev) is honored ONLY when the page itself
    // is on http (mirrors the location origin). On https pages, http
    // redirects are not safe.
    if (window.location.origin !== url.origin) {
      return false;
    }
  }
  if (typeof window !== 'undefined' && url.origin === window.location.origin) {
    return true;
  }
  if (url.protocol === 'https:' && SAFE_REDIRECT_ORIGINS.includes(url.origin)) {
    return true;
  }
  return false;
}
