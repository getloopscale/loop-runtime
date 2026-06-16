import { describe, it, expect, beforeEach, vi } from 'vitest';
import { sanitizeReturnTo } from './return-to';

// EC-3 absorption (OWASP A01 — broken access control / open redirect):
// the `return_to` query param arrives from external links and MUST NOT
// be trusted. `sanitizeReturnTo` is the canonical boundary helper.

describe('sanitizeReturnTo — EC-3 OWASP A01 defense', () => {
  beforeEach(() => {
    // Stable origin for the tests; mirrors dev-public dashboard host.
    vi.stubGlobal('window', {
      location: { origin: 'https://app-dev.loopzero.example' },
    });
  });

  it('returns the fallback for null', () => {
    expect(sanitizeReturnTo(null)).toEqual('/projects');
  });

  it('returns the fallback for empty string', () => {
    expect(sanitizeReturnTo('')).toEqual('/projects');
  });

  it('rejects javascript: protocol (XSS attempt)', () => {
    expect(sanitizeReturnTo('javascript:alert(document.cookie)')).toEqual('/projects');
    // Mixed-case + url-encoded variants also rejected.
    expect(sanitizeReturnTo('JavaScript:alert(1)')).toEqual('/projects');
  });

  it('rejects data: protocol', () => {
    expect(sanitizeReturnTo('data:text/html,<script>alert(1)</script>')).toEqual('/projects');
  });

  it('rejects vbscript: protocol', () => {
    expect(sanitizeReturnTo('vbscript:msgbox(1)')).toEqual('/projects');
  });

  it('rejects off-origin absolute URLs (open redirect)', () => {
    expect(sanitizeReturnTo('https://attacker.example/phish')).toEqual('/projects');
    expect(sanitizeReturnTo('http://localhost:1337/evil')).toEqual('/projects');
  });

  it('accepts a relative path', () => {
    expect(sanitizeReturnTo('/projects/my-project')).toEqual('/projects/my-project');
  });

  it('accepts a same-origin absolute URL', () => {
    expect(sanitizeReturnTo('https://app-dev.loopzero.example/billing')).toEqual(
      'https://app-dev.loopzero.example/billing',
    );
  });

  it('rejects protocol-relative URL (defaults to off-origin)', () => {
    expect(sanitizeReturnTo('//attacker.example/phish')).toEqual('/projects');
  });
});
