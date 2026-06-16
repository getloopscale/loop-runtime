import { describe, it, expect, beforeEach, vi } from 'vitest';
import { isSafeRedirectUrl, SAFE_REDIRECT_ORIGINS } from './safe-redirect';

// EC-2 absorption (OWASP A01 — open redirect via Kratos 422+redirect_to):
// the auth service can return a `redirect_browser_to` URL that the client
// follows via window.location.assign. Without same-origin + canonical-host
// allowlisting, a misconfigured or injected response can phishing-redirect
// the user. `isSafeRedirectUrl` is the canonical client-side allowlist.

describe('isSafeRedirectUrl — EC-2 OWASP A01 defense', () => {
  beforeEach(() => {
    vi.stubGlobal('window', {
      location: { origin: 'https://app-dev.loopzero.example' },
    });
  });

  it('returns false for empty / null / undefined', () => {
    expect(isSafeRedirectUrl('')).toBe(false);
    expect(isSafeRedirectUrl(null)).toBe(false);
    expect(isSafeRedirectUrl(undefined)).toBe(false);
  });

  it('blocks javascript: protocol', () => {
    expect(isSafeRedirectUrl('javascript:alert(1)')).toBe(false);
    expect(isSafeRedirectUrl('JavaScript:alert(1)')).toBe(false);
  });

  it('blocks data: protocol', () => {
    expect(isSafeRedirectUrl('data:text/html,<script>alert(1)</script>')).toBe(false);
  });

  it('blocks off-origin absolute URLs', () => {
    expect(isSafeRedirectUrl('https://attacker.example/phish')).toBe(false);
    expect(isSafeRedirectUrl('http://localhost:1337/evil')).toBe(false);
  });

  it('accepts same-origin absolute URL', () => {
    expect(isSafeRedirectUrl('https://app-dev.loopzero.example/billing')).toBe(true);
  });

  it('accepts canonical auth-dev host (Kratos public)', () => {
    expect(
      isSafeRedirectUrl(
        'https://auth-dev.loopzero.example/self-service/login/methods/oidc/callback/github',
      ),
    ).toBe(true);
  });

  it('accepts canonical prod hosts (future-ready)', () => {
    expect(isSafeRedirectUrl('https://app.loopzero.example/projects')).toBe(true);
    expect(isSafeRedirectUrl('https://auth.loopzero.example/self-service/recovery/browser')).toBe(true);
  });

  it('rejects http: against canonical hosts (https-only allowlist)', () => {
    expect(isSafeRedirectUrl('http://app-dev.loopzero.example/billing')).toBe(false);
  });

  it('rejects malformed URL', () => {
    expect(isSafeRedirectUrl('not a url')).toBe(false);
    expect(isSafeRedirectUrl('://broken')).toBe(false);
  });

  it('SAFE_REDIRECT_ORIGINS exports the allowlist for grep-auditability', () => {
    expect(SAFE_REDIRECT_ORIGINS).toContain('https://auth-dev.loopzero.example');
    expect(SAFE_REDIRECT_ORIGINS).toContain('https://app-dev.loopzero.example');
    expect(SAFE_REDIRECT_ORIGINS).toContain('https://auth.loopzero.example');
    expect(SAFE_REDIRECT_ORIGINS).toContain('https://app.loopzero.example');
  });
});
