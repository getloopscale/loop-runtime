import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { AlertCircle, CheckCircle2, Loader2, Mail } from 'lucide-react';
import { Alert, Button, Card, Input } from '@loopzero/ui';
import { API_URL } from '@/lib/config';

const KRATOS_URL = API_URL;

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
      setStatus('input');
    } else {
      window.location.href = `${KRATOS_URL}/self-service/verification/browser`;
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [flowId, searchParams]);

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
        setError(errors[0]?.text || 'Invalid code. Please try again.');
        setStatus('input');
      } else {
        setError('Verification failed. Please try again.');
        setStatus('input');
      }
    } catch {
      setError('Something went wrong. Please try again.');
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
              <Button asChild variant="primary" size="lg" className="w-full justify-center">
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
              <Button asChild variant="primary" size="lg" className="w-full justify-center">
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
                <Input
                  type="text"
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                  required
                  placeholder="Enter code"
                  maxLength={6}
                  autoFocus
                  aria-label="Verification code"
                  className="text-center tracking-widest text-body-sm"
                />
                <Button
                  type="submit"
                  variant="primary"
                  size="lg"
                  className="w-full justify-center"
                >
                  Verify
                </Button>
              </form>
            </>
          )}
        </Card.Body>
      </Card>
    </div>
  );
}
