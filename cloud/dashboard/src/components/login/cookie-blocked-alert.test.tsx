import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen } from '@/test/render';
import { CookieBlockedAlert } from './cookie-blocked-alert';

// T2.2 — RED specs for CookieBlockedAlert. Plan v1.1 Phase 2 Task T2.2.
// EC-8 absorption: third-party cookie block on Safari/Firefox strict — the
// secondary signal `sessionProbeStatus="401-after-callback"` triggers the
// alert even when navigator.cookieEnabled returns true.

describe('CookieBlockedAlert', () => {
  let originalNavigator: PropertyDescriptor | undefined;

  beforeEach(() => {
    originalNavigator = Object.getOwnPropertyDescriptor(global, 'navigator');
  });

  afterEach(() => {
    if (originalNavigator) {
      Object.defineProperty(global, 'navigator', originalNavigator);
    }
    vi.restoreAllMocks();
  });

  function mockCookiesEnabled(enabled: boolean) {
    Object.defineProperty(global, 'navigator', {
      value: { ...global.navigator, cookieEnabled: enabled },
      configurable: true,
      writable: true,
    });
  }

  it('test_CookieBlockedAlert_hidden_when_cookies_enabled', () => {
    mockCookiesEnabled(true);
    render(<CookieBlockedAlert />);
    // The renderer wraps with a Sonner toast region; assert no Alert title
    // is rendered (the component returns null).
    expect(screen.queryByText(/cookies are blocked/i)).toBeNull();
  });

  it('test_CookieBlockedAlert_visible_when_cookies_disabled', () => {
    mockCookiesEnabled(false);
    render(<CookieBlockedAlert />);
    expect(screen.getByText(/cookies are blocked/i)).toBeInTheDocument();
  });

  it('test_CookieBlockedAlert_includes_enable_cookies_instructions', () => {
    mockCookiesEnabled(false);
    render(<CookieBlockedAlert />);
    // Look for instructional copy steering user to enable cookies.
    expect(screen.getByText(/enable cookies/i)).toBeInTheDocument();
  });

  it('test_CookieBlockedAlert_handles_third_party_block_via_whoami_401', () => {
    // EC-8 absorption: navigator.cookieEnabled returns true (first-party OK)
    // but the post-OIDC session.whoami returns 401 because the cross-origin
    // Set-Cookie was silently dropped. CookieBlockedAlert must STILL render
    // via the secondary signal.
    mockCookiesEnabled(true);
    render(<CookieBlockedAlert sessionProbeStatus="401-after-callback" />);
    expect(screen.getByText(/cookies are blocked/i)).toBeInTheDocument();
  });
});
