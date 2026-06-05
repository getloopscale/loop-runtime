import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { AlertCircle, CheckCircle2, Loader2, Mail } from 'lucide-react';
import { Alert, Button, Card, PinInput } from '@loopzero/ui';
import { API_URL, AUTH_URL } from '@/lib/config';
import { enrichKratosError } from '@/lib/kratos';

const KRATOS_URL = AUTH_URL || API_URL;

type Status = 'loading' | 'input' | 'success' | 'error';

export function VerificationPage() {
  const [searchParams] = useSearchParams();
  const [status, setStatus] = useState<Status>('loading');
  const [code, setCode] = useState(searchParams.get('code') ?? '');
  const [error, setError] = useState('');

  const flowId = searchParams.get('flow');

  useEffect(() => {
    const urlCode = searchParams.get('code');
    if (flowId && urlCode) {
      submitCode(flowId, urlCode);
    } else if (flowId) {
      // Browser-created flow is empty: Kratos hasn't queued an email yet
      // because nobody submitted the email field. If the user has an active
      // session, look up their email and submit it automatically so the
      // courier dispatches a code BEFORE we show PinInput.
      ensureCodeRequested(flowId).finally(() => setStatus('input'));
    } else {
      window.location.href = `${KRATOS_URL}/self-service/verification/browser`;
    }
    // WAI-ARIA-equivalent: submitCode intentionally NOT in deps to avoid
    // infinite re-render loop (function identity changes each render).
    // flowId + searchParams are the actual change triggers per D-9 v1.2.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [flowId, searchParams]);

  /**
   * Ensure Kratos has queued a verification email for this flow.
   *
   * Kratos browser-mode creates an empty flow on GET; it only enqueues a
   * mail after a POST that includes the `email` field. The dashboard UI
   * surfaces PinInput directly (no email-prompt screen), so without this
   * auto-POST users see an input box waiting for a code that was never
   * sent. We pull the email from /sessions/whoami (set when the user is
   * logged in) and submit it. If there is no session, we fall through
   * silently — there is nothing to send the code to.
   *
   * Idempotent: re-POSTing the same email on a flow that already has a
   * code pending is a no-op on Kratos's side (it returns the same flow
   * with the code-input node).
   */
  const ensureCodeRequested = async (flow: string) => {
    try {
      const sessionResp = await fetch(`${KRATOS_URL}/sessions/whoami`, {
        credentials: 'include',
        headers: { Accept: 'application/json' },
      });
      if (!sessionResp.ok) return;
      const session = await sessionResp.json();
      const email: string | undefined = session?.identity?.traits?.email;
      if (!email) return;

      const flowResp = await fetch(
        `${KRATOS_URL}/self-service/verification/flows?id=${flow}`,
        { credentials: 'include', headers: { Accept: 'application/json' } },
      );
      if (!flowResp.ok) return;
      const flowData = await flowResp.json();

      // Skip if the flow is already past the email step (has a `code` node).
      const hasCodeNode = flowData.ui?.nodes?.some(
        (n: { attributes: { name?: string } }) => n.attributes.name === 'code',
      );
      if (hasCodeNode) return;

      const csrfNode = flowData.ui?.nodes?.find(
        (n: { attributes: { name?: string } }) => n.attributes.name === 'csrf_token',
      );
      await fetch(flowData.ui.action, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
        body: JSON.stringify({
          csrf_token: csrfNode?.attributes?.value || '',
          method: 'code',
          email,
        }),
      });
    } catch {
      // Non-fatal: PinInput still renders, user can manually re-request via
      // the "Resend" link below the submit button.
    }
  };

  const submitCode = async (flow: string, verificationCode: string) => {
    setStatus('loading');
    try {
      const flowResp = await fetch(`${KRATOS_URL}/self-service/verification/flows?id=${flow}`, {
        credentials: 'include',
        headers: { Accept: 'application/json' },
      });
      if (!flowResp.ok) {
        setError('Verification link expired. Please request a new one.');
        setStatus('error');
        return;
      }
      const flowData = await flowResp.json();
      const csrfNode = flowData.ui?.nodes?.find(
        (n: { attributes: { name?: string } }) => n.attributes.name === 'csrf_token',
      );

      const resp = await fetch(flowData.ui.action, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
        body: JSON.stringify({
          csrf_token: csrfNode?.attributes?.value || '',
          method: 'code',
          code: verificationCode,
        }),
      });

      const data = await resp.json();

      if (data.state === 'passed_challenge') {
        setStatus('success');
      } else if (data.ui?.messages) {
        const errors = data.ui.messages.filter((m: { type: string }) => m.type === 'error');
        // EC-02: enrich generic Kratos messages com TCIA action context
        setError(
          enrichKratosError(
            errors[0]?.text || 'Invalid code. Request a new verification link from the sign-in page.',
            'verification',
          ),
        );
        setStatus('input');
      } else {
        setError(
          'Verification failed. Request a new verification link from the sign-in page, or contact support if it persists.',
        );
        setStatus('input');
      }
    } catch {
      // T0.1 TCIA — sponsor critério #6
      setError(
        'Verification request failed. Your network may be unreachable or the link expired. Request a new verification link from the sign-in page.',
      );
      setStatus('error');
    }
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (flowId && code) submitCode(flowId, code);
  };

  return (
    <div className="min-h-screen bg-background flex items-center justify-center p-6">
      <Card className="w-full max-w-sm">
        <Card.Body className="p-8 text-center space-y-4">
          {status === 'loading' && (
            <>
              <Loader2
                className="size-8 text-primary animate-spin mx-auto"
                aria-label="Verifying"
              />
              <p className="text-body-sm text-muted-foreground">
                Verifying your email...
              </p>
            </>
          )}

          {status === 'success' && (
            <>
              <CheckCircle2
                className="size-12 text-success mx-auto"
                aria-hidden="true"
              />
              <div className="space-y-1">
                <h1 className="font-display text-title-sm font-semibold text-foreground">
                  Email verified
                </h1>
                <p className="text-body-sm text-muted-foreground">
                  Your account is now fully active.
                </p>
              </div>
              <Button asChild variant="primary" size="lg"
 className="w-full justify-center">
                <a href="/">Go to dashboard</a>
              </Button>
            </>
          )}

          {status === 'error' && (
            <>
              <AlertCircle
                className="size-12 text-destructive mx-auto"
                aria-hidden="true"
              />
              <div className="space-y-1">
                <h1 className="font-display text-title-sm font-semibold text-foreground">
                  Verification failed
                </h1>
                <p className="text-body-sm text-muted-foreground">{error}</p>
              </div>
              <Button
                variant="primary"
                size="lg"
                className="w-full justify-center"
                onClick={() => {
                  window.location.href = `${KRATOS_URL}/self-service/verification/browser`;
                }}
              >
                Resend verification email
              </Button>
              <Button asChild variant="secondary" size="lg" className="w-full justify-center">
                <a href="/">Go to dashboard</a>
              </Button>
            </>
          )}

          {status === 'input' && (
            <>
              <Mail className="size-12 text-primary mx-auto" aria-hidden="true" />
              <div className="space-y-1">
                <h1 className="font-display text-title-sm font-semibold text-foreground">
                  Enter verification code
                </h1>
                <p className="text-body-sm text-muted-foreground">
                  Check your email for a 6-digit code.
                </p>
              </div>
              {error && <Alert intent="destructive" title={error} />}
              <form onSubmit={handleSubmit} className="space-y-4">
                {/* Brief #5 PinInput — 6-box OTP per DSS § 4 + Apple/Stripe/Clerk convention.
                    Closes Deep Review § 2.12 P2. onComplete auto-submits when 6 digits typed. */}
                <PinInput
                  length={6}
                  value={code}
                  onChange={setCode}
                  onComplete={(v) => flowId && submitCode(flowId, v)}
                  inputMode="numeric"
                  size="lg"
                  autoFocus
                  error={!!error}
                  aria-label="Verification code"
                />
                <Button
                  type="submit"
                  variant="primary"
                  size="lg"
                  className="w-full justify-center"
                  disabled={code.length !== 6}
                >
                  Verify
                </Button>
              </form>
              <button
                type="button"
                onClick={() => {
                  window.location.href = `${KRATOS_URL}/self-service/verification/browser`;
                }}
                className="text-label text-muted-foreground hover:text-foreground underline-offset-2 hover:underline mt-2"
              >
                Didn't receive the email? Resend
              </button>
            </>
          )}
        </Card.Body>
      </Card>
    </div>
  );
}
