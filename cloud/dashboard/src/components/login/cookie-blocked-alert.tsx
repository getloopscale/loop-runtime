import { Alert } from '@loopzero/ui';

/**
 * CookieBlockedAlert — banner shown when the browser is blocking cookies
 * that the OIDC sign-in flow requires. Plan v1.1 Phase 2 Task T2.2.
 *
 * Primary signal: `navigator.cookieEnabled === false`.
 *
 * Secondary signal (EC-8 absorption): `sessionProbeStatus ===
 * "401-after-callback"`. Some browsers (Safari Intelligent Tracking
 * Prevention, Firefox Strict mode) report `cookieEnabled === true` even
 * when third-party cookies are silently dropped at cross-origin boundaries.
 * The post-OIDC `session.whoami` returns 401 in that case; the caller
 * threads that signal here so the alert still renders.
 */
export interface CookieBlockedAlertProps {
  sessionProbeStatus?: 'ok' | '401-after-callback' | null;
}

export function CookieBlockedAlert({ sessionProbeStatus }: CookieBlockedAlertProps = {}) {
  const cookiesDisabled = typeof navigator !== 'undefined' && navigator.cookieEnabled === false;
  const thirdPartyBlocked = sessionProbeStatus === '401-after-callback';
  if (!cookiesDisabled && !thirdPartyBlocked) {
    return null;
  }
  return (
    <Alert
      intent="warning"
      title="Cookies are blocked"
      description="Sign-in requires cookies. Enable cookies for this site in your browser settings, then refresh the page."
    />
  );
}
