import { useEffect, useState } from 'react';
import { useSearchParams, Link } from 'react-router-dom';
import { CheckCircle2, KeyRound } from 'lucide-react';
import { Alert, Button, Card, Input } from '@loopzero/ui';
import {
  type Flow,
  getFlow,
  submitFlow,
  getCsrfToken,
  getFlowMessages,
  enrichKratosError,
} from '@/lib/kratos';
import { API_URL } from '@/lib/config';

const ACCOUNTS_URL = API_URL;

type Phase = 'email' | 'code' | 'password' | 'success';

export function RecoveryPage() {
  const [searchParams] = useSearchParams();
  const [flow, setFlow] = useState<Flow | null>(null);
  const [phase, setPhase] = useState<Phase>('email');
  const [email, setEmail] = useState('');
  const [code, setCode] = useState('');
  const [loading, setLoading] = useState(false);
  const [errorMessage, setErrorMessage] = useState('');

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
          window.location.href = `${ACCOUNTS_URL}/self-service/recovery/browser?return_to=${encodeURIComponent(window.location.origin + '/recovery')}`;
        });
    } else {
      window.location.href = `${ACCOUNTS_URL}/self-service/recovery/browser?return_to=${encodeURIComponent(window.location.origin + '/recovery')}`;
    }
  }, [searchParams]);

  const handleEmailSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!flow) return;
    setLoading(true);
    setErrorMessage('');
    try {
      const result = await submitFlow(flow.ui.action, flow.ui.method, {
        csrf_token: getCsrfToken(flow),
        email,
        method: 'code',
      });
      setFlow(result);
      const messages = getFlowMessages(result);
      if (messages.some((m) => m.includes('error') || m.includes('Error'))) {
        // EC-02: enrich generic Kratos messages com TCIA action context
        setErrorMessage(enrichKratosError(messages[0], 'recovery'));
      } else {
        setPhase('code');
      }
    } catch {
      // T0.1 TCIA — sponsor critério #6
      setErrorMessage(
        'Recovery email request failed. Check your email address and network connection, then retry. Contact support@loopzero.example if it persists.',
      );
    } finally {
      setLoading(false);
    }
  };

  const handleCodeSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!flow) return;
    setLoading(true);
    setErrorMessage('');
    try {
      const result = await submitFlow(flow.ui.action, flow.ui.method, {
        csrf_token: getCsrfToken(flow),
        code,
        method: 'code',
      });
      if (result.state === 'passed_challenge') {
        setPhase('success');
      } else {
        setFlow(result);
        const errors = getFlowMessages(result);
        if (errors.length > 0) setErrorMessage(enrichKratosError(errors[0], 'recovery'));
      }
    } catch {
      setErrorMessage(
        'Invalid or expired code. Request a new recovery email from the sign-in page, or contact support if it persists.',
      );
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen bg-background flex items-center justify-center p-6">
      <Card className="w-full max-w-sm">
        <Card.Body className="p-8 space-y-6">
          <div className="flex size-12 items-center justify-center rounded-xl bg-primary/10 border border-primary/20 mx-auto">
            <KeyRound className="size-6 text-primary" aria-hidden="true" />
          </div>

          {phase === 'email' && (
            <>
              <div className="space-y-1 text-center">
                <h1 className="font-display text-title-sm font-semibold text-foreground">
                  Reset your password
                </h1>
                <p className="text-body-sm text-muted-foreground">
                  Enter your email and we'll send you a recovery code.
                </p>
              </div>
              {errorMessage && <Alert intent="destructive" title={errorMessage} />}
              <form onSubmit={handleEmailSubmit} className="space-y-4">
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
            </>
          )}

          {phase === 'code' && (
            <>
              <div className="space-y-1 text-center">
                <h1 className="font-display text-title-sm font-semibold text-foreground">
                  Enter recovery code
                </h1>
                <p className="text-body-sm text-muted-foreground">
                  Check your email for a 6-digit code.
                </p>
              </div>
              {errorMessage && <Alert intent="destructive" title={errorMessage} />}
              <form onSubmit={handleCodeSubmit} className="space-y-4">
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
            </>
          )}

          {phase === 'success' && (
            <div className="text-center space-y-4">
              <CheckCircle2
                className="size-12 text-success mx-auto"
                aria-hidden="true"
              />
              <div className="space-y-1">
                <h1 className="font-display text-title-sm font-semibold text-foreground">
                  Password reset
                </h1>
                <p className="text-body-sm text-muted-foreground">
                  Your password has been updated. You can now sign in.
                </p>
              </div>
              <Button asChild variant="primary" size="lg"
 className="w-full justify-center">
                <Link to="/login">Sign in</Link>
              </Button>
            </div>
          )}

          <p className="text-center text-body-sm">
            <Link to="/login" className="text-primary hover:text-primary/80">
              Back to login
            </Link>
          </p>
        </Card.Body>
      </Card>
    </div>
  );
}
