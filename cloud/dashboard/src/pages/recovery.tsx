import { useCallback, useEffect, useRef, useState } from 'react';
import { useSearchParams, Link } from 'react-router-dom';
import { CheckCircle2, Github, KeyRound } from 'lucide-react';
import { Alert, Button, Card, Input } from '@loopzero/ui';
import { GoogleBrandIcon } from '@/components/google-brand-icon';
import {
  type Flow,
  getFlow,
  submitFlow,
  initFlow,
  getCsrfToken,
  getFlowMessages,
  getFieldErrors,
  enrichKratosError,
} from '@/lib/kratos';
import { API_URL } from '@/lib/config';

const ACCOUNTS_URL = API_URL;

/**
 * RecoveryPage — Journey J-04 (Vercel-like password reset).
 *
 * Plan: .claude/knowledge-base/plans/2026-06-05-dashboard-journey-j04-recovery-plan.md
 *
 * Phases (4 visual):
 *   email    → user enters email (Kratos `recovery` flow)
 *   code     → user pastes the 6-digit code from email
 *   password → user sets new password (Kratos `settings` flow after passed_challenge)
 *   success  → confirmation + sign-in link
 *
 * Edge cases addressed (EC-1..EC-9 from edge-case review):
 *   - EC-1: initFlow('settings') wrapped in try/catch with Retry fallback (D2)
 *   - EC-2: identifier_similarity mapped via enrichKratosError → product-language
 *   - EC-3: cascading getFlow→initFlow failure → "Reload page" fallback (D5b)
 *   - EC-4: lastSubmit currying (reads current state, not snapshot) (D7)
 *   - EC-5: disabled={loading} guard on ALL submits (double-click race)
 *   - EC-6: cooldown uses Date.now() wall-clock, not setInterval tick count
 *   - EC-7: getFlow 410 OR 404 both trigger silent reinit (D5 generic catch)
 *   - EC-8: reload zeroes cooldown (documented, accepted per Kratos rate-limit)
 *   - EC-9: out of scope for client (handled by T5.1 bash pre-flight)
 *
 * D1 (account_enumeration.mitigate=true): educational OIDC link always shown,
 * never detective. D6: 5xx → product-language without leaking Kratos JSON.
 */

const SERVICE_UNAVAILABLE_MESSAGE =
  'Recovery service temporarily unavailable. Try again in a minute, or contact support@loopzero.example.';

type Phase = 'email' | 'code' | 'password' | 'success';

function OidcEducationalLinks(): React.ReactElement {
  return (
    <div className="space-y-2 pt-2">
      <p className="text-body-sm text-muted-foreground text-center">
        Did you sign up with GitHub or Google?
      </p>
      <div className="grid grid-cols-2 gap-2">
        <Button
          asChild
          variant="secondary"
          size="md"
          className="w-full justify-center gap-2"
        >
          <Link to="/login?provider=github">
            <Github className="size-3.5" aria-hidden="true" />
            Sign in with GitHub
          </Link>
        </Button>
        <Button
          asChild
          variant="secondary"
          size="md"
          className="w-full justify-center gap-2"
        >
          <Link to="/login?provider=google">
            <GoogleBrandIcon />
            Sign in with Google
          </Link>
        </Button>
      </div>
    </div>
  );
}

export function RecoveryPage() {
  const [searchParams] = useSearchParams();
  const [flow, setFlow] = useState<Flow | null>(null);
  const [phase, setPhase] = useState<Phase>('email');
  const [email, setEmail] = useState('');
  const [code, setCode] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [newPasswordConfirm, setNewPasswordConfirm] = useState('');
  const [loading, setLoading] = useState(false);
  const [errorMessage, setErrorMessage] = useState('');
  const [showReloadFallback, setShowReloadFallback] = useState(false);

  // EC-4 (D7): refs mirror state so handlers ALWAYS read latest values at
  // execution time, even when stored in lastSubmit from an earlier render.
  // Without these refs, useCallback closures capture stale state and Retry
  // would re-submit with the OLD input from when the failure originally fired.
  const flowRef = useRef<Flow | null>(null);
  const emailRef = useRef('');
  const codeRef = useRef('');
  const newPasswordRef = useRef('');
  const newPasswordConfirmRef = useRef('');
  flowRef.current = flow;
  emailRef.current = email;
  codeRef.current = code;
  newPasswordRef.current = newPassword;
  newPasswordConfirmRef.current = newPasswordConfirm;

  // EC-4 (D7): lastSubmit stores a REFERENCE to the handler. Since the
  // handler reads state via *Ref.current at execution time, Retry always
  // uses the CURRENT input — not a snapshot from when setLastSubmit ran.
  const [lastSubmit, setLastSubmit] = useState<(() => Promise<void>) | null>(null);

  // EC-6 (D3): cooldown end timestamp + tick state forces re-render every 1s.
  // Display uses Date.now() comparison — robust to browser tab throttle.
  const [cooldownEndsAt, setCooldownEndsAt] = useState<number | null>(null);
  const [, setTick] = useState(0);

  useEffect(() => {
    if (cooldownEndsAt === null) return;
    const id = setInterval(() => {
      if (Date.now() >= cooldownEndsAt) {
        setCooldownEndsAt(null);
      } else {
        setTick((t) => t + 1);
      }
    }, 1000);
    return () => clearInterval(id);
  }, [cooldownEndsAt]);

  // D5 + D5b + EC-3 + EC-7: stale flow (410/404/any) triggers re-init; cascading
  // failure shows Reload Page fallback. Generic catch covers all error codes.
  useEffect(() => {
    const flowId = searchParams.get('flow');
    if (flowId) {
      getFlow('recovery', flowId)
        .then((f) => {
          setFlow(f);
          const hasCodeInput = f.ui.nodes.some(
            (n) => n.attributes.name === 'code',
          );
          if (hasCodeInput) setPhase('code');
        })
        .catch(() => {
          // EC-7: 410 OR 404 OR other — try silent re-init
          initFlow('recovery')
            .then((newFlow) => {
              setFlow(newFlow);
              try {
                window.history.replaceState(
                  null,
                  '',
                  `/recovery?flow=${newFlow.id}`,
                );
              } catch {
                // history.replaceState is no-op in tests/CSP edge cases
              }
            })
            .catch(() => {
              // EC-3 (D5b): cascading failure → product-language + Reload fallback, no loop
              setErrorMessage(SERVICE_UNAVAILABLE_MESSAGE);
              setShowReloadFallback(true);
            });
        });
    } else {
      // No flow ID → bootstrap browser flow (canonical Kratos redirect)
      window.location.href = `${ACCOUNTS_URL}/self-service/recovery/browser?return_to=${encodeURIComponent(window.location.origin + '/recovery')}`;
    }
  }, [searchParams]);

  // Detect HTTP 5xx vs network error vs Kratos 4xx in catch blocks.
  // Network/5xx → product-language D6. Kratos 4xx → enrichKratosError.
  const is5xxOrNetwork = (err: unknown): boolean => {
    if (err instanceof TypeError) return true; // network failure
    const msg = err instanceof Error ? err.message : String(err);
    return /\b5\d\d\b/.test(msg);
  };

  // EC-4 + D7 currying: handlers are STABLE refs (useCallback deps=[]). They
  // read all state via *Ref.current at execution time. setLastSubmit stores
  // `() => handler()` — when Retry fires, the handler reads the CURRENT input
  // not a snapshot from when setLastSubmit was called.
  const handleEmailSubmit = useCallback(async (): Promise<void> => {
    const f = flowRef.current;
    if (!f) return;
    setLoading(true);
    setErrorMessage('');
    setShowReloadFallback(false);
    try {
      const result = await submitFlow(f.ui.action, f.ui.method, {
        csrf_token: getCsrfToken(f),
        email: emailRef.current,
        method: 'code',
      });
      setFlow(result);
      const messages = getFlowMessages(result);
      if (messages.some((m) => m.toLowerCase().includes('error'))) {
        setErrorMessage(enrichKratosError(messages[0], 'recovery'));
      } else {
        setPhase('code');
      }
      setLastSubmit(null);
    } catch (err) {
      setLastSubmit(() => handleEmailSubmit);
      if (is5xxOrNetwork(err)) {
        setErrorMessage(SERVICE_UNAVAILABLE_MESSAGE);
      } else {
        setErrorMessage(SERVICE_UNAVAILABLE_MESSAGE);
      }
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleCodeSubmit = useCallback(async (): Promise<void> => {
    const f = flowRef.current;
    if (!f) return;
    setLoading(true);
    setErrorMessage('');
    setShowReloadFallback(false);
    try {
      const result = await submitFlow(f.ui.action, f.ui.method, {
        csrf_token: getCsrfToken(f),
        code: codeRef.current,
        method: 'code',
      });

      // Kratos canonical behavior on code verified: HTTP 422 with
      // `redirect_browser_to: https://app-dev.loopzero.example/settings?flow=<new>`
      // (canonical "browser_location_change_required" per ory-error-id header).
      // This means the recovery code was VALID — Kratos created a temporary
      // recovery session cookie and wants the browser to continue at the
      // settings flow to set a new password. We extract the new flow ID and
      // load it inline (single-page wizard per D2) instead of redirecting.
      const errResult = result as unknown as {
        error?: { id?: string; reason?: string };
        redirect_browser_to?: string;
      };
      const isBrowserRedirect =
        errResult.error?.id === 'browser_location_change_required' ||
        Boolean(errResult.redirect_browser_to);
      if (isBrowserRedirect) {
        const redirectUrl =
          errResult.redirect_browser_to || errResult.error?.reason || '';
        // Match any flow ID format (UUID, alphanumeric+hyphen) up to next & or end
        const match = String(redirectUrl).match(/[?&]flow=([^&\s]+)/);
        const settingsFlowId = match?.[1];
        if (settingsFlowId) {
          try {
            const settingsFlow = await getFlow('settings', settingsFlowId);
            setFlow(settingsFlow);
            setPhase('password');
            setLastSubmit(null);
            return;
          } catch {
            // EC-1: settings flow GET failed — preserve Retry path
            setErrorMessage(
              'Recovery verified but session setup failed. Try the recovery email link again.',
            );
            setLastSubmit(() => handleCodeSubmit);
            return;
          }
        }
      }

      if (result.state === 'passed_challenge') {
        // Fallback path for API-mode flows (no redirect): initFlow('settings')
        try {
          const settingsFlow = await initFlow('settings');
          setFlow(settingsFlow);
          setPhase('password');
          setLastSubmit(null);
        } catch {
          setErrorMessage(
            'Recovery verified but session setup failed. Try the recovery email link again.',
          );
          setLastSubmit(() => handleCodeSubmit);
        }
      } else {
        setFlow(result);
        const errors = getFlowMessages(result);
        if (errors.length > 0) {
          setErrorMessage(enrichKratosError(errors[0], 'recovery'));
        } else if (errResult.error) {
          // 4xx error with no UI messages — generic
          setErrorMessage(
            'Invalid or expired code. Request a new recovery email from the sign-in page, or contact support if it persists.',
          );
        }
      }
    } catch (err) {
      setLastSubmit(() => handleCodeSubmit);
      if (is5xxOrNetwork(err)) {
        setErrorMessage(SERVICE_UNAVAILABLE_MESSAGE);
      } else {
        setErrorMessage(
          'Invalid or expired code. Request a new recovery email from the sign-in page, or contact support if it persists.',
        );
      }
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handlePasswordSubmit = useCallback(async (): Promise<void> => {
    const f = flowRef.current;
    if (!f) return;
    if (newPasswordRef.current !== newPasswordConfirmRef.current) {
      setErrorMessage('Passwords do not match.');
      return;
    }
    setLoading(true);
    setErrorMessage('');
    try {
      const result = await submitFlow(f.ui.action, f.ui.method, {
        csrf_token: getCsrfToken(f),
        password: newPasswordRef.current,
        method: 'password',
      });
      if (result.state === 'success') {
        setPhase('success');
        setLastSubmit(null);
      } else {
        setFlow(result);
        // EC-2: identifier_similarity mapped via enrichKratosError
        const fieldErrors = getFieldErrors(result, 'password');
        const flowErrors = getFlowMessages(result);
        const firstError = fieldErrors[0] || flowErrors[0];
        if (firstError) {
          setErrorMessage(enrichKratosError(firstError, 'recovery'));
        }
      }
    } catch (err) {
      setLastSubmit(() => handlePasswordSubmit);
      if (is5xxOrNetwork(err)) {
        setErrorMessage(SERVICE_UNAVAILABLE_MESSAGE);
      } else {
        setErrorMessage(SERVICE_UNAVAILABLE_MESSAGE);
      }
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleRetry = useCallback(async () => {
    if (lastSubmit) {
      await lastSubmit();
    }
  }, [lastSubmit]);

  const handleResend = useCallback(() => {
    if (cooldownEndsAt !== null) return;
    setCooldownEndsAt(Date.now() + 60_000);
    window.location.href = `${ACCOUNTS_URL}/self-service/recovery/browser?return_to=${encodeURIComponent(window.location.origin + '/recovery')}`;
  }, [cooldownEndsAt]);

  const cooldownSeconds =
    cooldownEndsAt !== null
      ? Math.max(0, Math.ceil((cooldownEndsAt - Date.now()) / 1000))
      : 0;

  const isCooldownActive = cooldownEndsAt !== null && cooldownSeconds > 0;

  return (
    <div className="min-h-screen bg-background flex items-center justify-center p-6">
      <Card className="w-full max-w-sm">
        <Card.Body className="p-8 space-y-6">
          <div className="flex size-12 items-center justify-center rounded-xl bg-primary/10 border border-primary/20 mx-auto">
            <KeyRound className="size-6 text-primary" aria-hidden="true" />
          </div>

          {/* EC-3 (D5b): cascading failure → product-language + Reload Page */}
          {showReloadFallback && (
            <>
              <Alert intent="destructive" title={errorMessage} />
              <Button
                variant="secondary"
                size="lg"
                className="w-full justify-center"
                onClick={() => window.location.reload()}
              >
                Reload page
              </Button>
            </>
          )}

          {phase === 'email' && !showReloadFallback && (
            <>
              <div className="space-y-1 text-center">
                <h1 className="font-display text-title-sm font-semibold text-foreground">
                  Reset your password
                </h1>
                <p className="text-body-sm text-muted-foreground">
                  Enter your email and we'll send you a recovery code.
                </p>
              </div>
              {errorMessage && (
                <>
                  <Alert intent="destructive" title={errorMessage} />
                  {lastSubmit && (
                    <Button
                      variant="secondary"
                      size="md"
                      className="w-full justify-center"
                      onClick={handleRetry}
                      disabled={loading}
                    >
                      Retry
                    </Button>
                  )}
                </>
              )}
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  void handleEmailSubmit();
                }}
                className="space-y-4"
              >
                <Input
                  size="lg"
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  required
                  placeholder="you@example.com"
                  autoComplete="email"
                  aria-label="Email"
                />
                <Button
                  type="submit"
                  variant="primary"
                  size="lg"
                  className="w-full justify-center"
                  disabled={loading}
                >
                  {loading ? 'Sending...' : 'Send recovery code'}
                </Button>
              </form>

              <OidcEducationalLinks />
            </>
          )}

          {phase === 'code' && !showReloadFallback && (
            <>
              <div className="space-y-1 text-center">
                <h1 className="font-display text-title-sm font-semibold text-foreground">
                  Enter recovery code
                </h1>
                <p className="text-body-sm text-muted-foreground">
                  Check your email for a 6-digit code.
                </p>
              </div>
              {errorMessage && (
                <>
                  <Alert intent="destructive" title={errorMessage} />
                  {lastSubmit && (
                    <Button
                      variant="secondary"
                      size="md"
                      className="w-full justify-center"
                      onClick={handleRetry}
                      disabled={loading}
                    >
                      Retry
                    </Button>
                  )}
                </>
              )}
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  void handleCodeSubmit();
                }}
                className="space-y-4"
              >
                <Input
                  size="lg"
                  type="text"
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                  required
                  placeholder="Enter code"
                  aria-label="Recovery code"
                  className="text-center tracking-widest"
                />
                <Button
                  type="submit"
                  variant="primary"
                  size="lg"
                  className="w-full justify-center"
                  disabled={loading}
                >
                  {loading ? 'Verifying...' : 'Verify code'}
                </Button>
              </form>
              <button
                type="button"
                onClick={handleResend}
                disabled={isCooldownActive}
                className="text-label text-muted-foreground hover:text-foreground underline-offset-2 hover:underline mt-2 block mx-auto disabled:opacity-50 disabled:cursor-not-allowed disabled:no-underline"
              >
                {isCooldownActive
                  ? `Resend in ${cooldownSeconds}s`
                  : "Didn't receive the email? Resend"}
              </button>
            </>
          )}

          {phase === 'password' && !showReloadFallback && (
            <>
              <div className="space-y-1 text-center">
                <h1 className="font-display text-title-sm font-semibold text-foreground">
                  Set a new password
                </h1>
                <p className="text-body-sm text-muted-foreground">
                  Choose a password with at least 10 characters.
                </p>
              </div>
              {errorMessage && <Alert intent="destructive" title={errorMessage} />}
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  void handlePasswordSubmit();
                }}
                className="space-y-4"
              >
                <Input
                  size="lg"
                  type="password"
                  value={newPassword}
                  onChange={(e) => setNewPassword(e.target.value)}
                  required
                  placeholder="New password"
                  autoComplete="new-password"
                  aria-label="New password"
                />
                <Input
                  size="lg"
                  type="password"
                  value={newPasswordConfirm}
                  onChange={(e) => setNewPasswordConfirm(e.target.value)}
                  required
                  placeholder="Confirm password"
                  autoComplete="new-password"
                  aria-label="Confirm password"
                />
                <Button
                  type="submit"
                  variant="primary"
                  size="lg"
                  className="w-full justify-center"
                  disabled={loading}
                >
                  {loading ? 'Updating...' : 'Update password'}
                </Button>
              </form>
            </>
          )}

          {phase === 'success' && !showReloadFallback && (
            <div className="text-center space-y-4">
              <CheckCircle2
                className="size-12 text-success mx-auto"
                aria-hidden="true"
              />
              <div className="space-y-1">
                <h1 className="font-display text-title-sm font-semibold text-foreground">
                  Password reset successful
                </h1>
                <p className="text-body-sm text-muted-foreground">
                  Sign in to continue.
                </p>
              </div>
              <Button
                asChild
                variant="primary"
                size="lg"
                className="w-full justify-center"
              >
                <Link to="/login">Sign in</Link>
              </Button>

              <OidcEducationalLinks />
            </div>
          )}

          {!showReloadFallback && (
            <p className="text-center text-body-sm">
              <Link to="/login" className="text-primary hover:text-primary/80">
                Back to login
              </Link>
            </p>
          )}
        </Card.Body>
      </Card>
    </div>
  );
}
