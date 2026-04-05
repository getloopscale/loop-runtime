import { useEffect, useState } from 'react';
import { useSearchParams, Link } from 'react-router-dom';
import { motion } from 'framer-motion';
import { CheckCircle2, XCircle, Loader2, Mail } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { type Flow, getFlow, submitFlow, getCsrfToken, getFlowMessages } from '@/lib/kratos';

const ACCOUNTS_URL = import.meta.env.VITE_API_URL || 'http://localhost:13002';

export function VerificationPage() {
  const [searchParams] = useSearchParams();
  const [flow, setFlow] = useState<Flow | null>(null);
  const [status, setStatus] = useState<'loading' | 'input' | 'success' | 'error'>('loading');
  const [email, setEmail] = useState('');
  const [errorMessage, setErrorMessage] = useState('');

  useEffect(() => {
    const flowId = searchParams.get('flow');
    if (!flowId) {
      // No flow ID — show form to request new verification email
      setStatus('input');
      return;
    }
    getFlow('verification', flowId)
      .then(f => {
        if (f.state === 'passed_challenge') {
          setStatus('success');
        } else {
          setFlow(f);
          setStatus('input');
        }
      })
      .catch(() => {
        setErrorMessage('This verification link has expired.');
        setStatus('error');
      });
  }, [searchParams]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!flow && !email) return;

    if (!flow) {
      // Start new verification flow
      window.location.href = `${ACCOUNTS_URL}/self-service/verification/browser?return_to=${encodeURIComponent(window.location.origin + '/verification')}`;
      return;
    }

    try {
      const result = await submitFlow(flow.ui.action, flow.ui.method, {
        csrf_token: getCsrfToken(flow),
        email,
        method: 'code',
      });
      if (result.state === 'passed_challenge') {
        setStatus('success');
      } else {
        setFlow(result);
        const messages = getFlowMessages(result);
        if (messages.length > 0) setErrorMessage(messages[0]);
      }
    } catch {
      setErrorMessage('Something went wrong. Please try again.');
    }
  };

  return (
    <div className="min-h-screen bg-surface-0 flex items-center justify-center">
      <motion.div initial={{ opacity: 0, y: 20 }} animate={{ opacity: 1, y: 0 }} className="w-full max-w-sm">
        <div className="rounded-2xl border border-white/[0.06] bg-surface-1/80 backdrop-blur-xl p-8 shadow-2xl shadow-black/40 text-center">
          {status === 'loading' && (
            <Loader2 className="h-8 w-8 text-accent animate-spin mx-auto" />
          )}

          {status === 'success' && (
            <>
              <CheckCircle2 className="h-12 w-12 text-accent mx-auto mb-4" />
              <h1 className="font-display text-xl font-semibold text-white mb-2">Email verified</h1>
              <p className="text-sm text-white/40 mb-6">Your account is now active. You can sign in.</p>
              <Link to="/login">
                <Button size="lg" className="w-full justify-center h-12">Sign in</Button>
              </Link>
            </>
          )}

          {status === 'error' && (
            <>
              <XCircle className="h-12 w-12 text-danger mx-auto mb-4" />
              <h1 className="font-display text-xl font-semibold text-white mb-2">Verification failed</h1>
              <p className="text-sm text-white/40 mb-6">{errorMessage || 'This link is invalid or has expired.'}</p>
              <Button size="lg" className="w-full justify-center h-12" onClick={() => setStatus('input')}>
                Resend verification email
              </Button>
            </>
          )}

          {status === 'input' && (
            <>
              <Mail className="h-12 w-12 text-accent/60 mx-auto mb-4" />
              <h1 className="font-display text-xl font-semibold text-white mb-2">Verify your email</h1>
              <p className="text-sm text-white/40 mb-6">Enter your email to receive a verification link.</p>
              <form onSubmit={handleSubmit} className="space-y-4">
                <input type="email" value={email} onChange={e => setEmail(e.target.value)} required placeholder="you@example.com"
                  className="w-full rounded-lg border border-white/[0.08] bg-surface-0 px-3 py-2.5 text-sm text-white placeholder:text-white/20 focus:border-accent/30 focus:outline-none" />
                {errorMessage && <p className="text-xs text-danger">{errorMessage}</p>}
                <Button type="submit" size="lg" className="w-full justify-center h-12">Send verification email</Button>
              </form>
              <p className="mt-6 text-sm text-white/30">
                <Link to="/login" className="text-accent hover:text-accent/80">Back to login</Link>
              </p>
            </>
          )}
        </div>
      </motion.div>
    </div>
  );
}
