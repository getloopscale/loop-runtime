import { useEffect, useState } from 'react';
import { useSearchParams, Link } from 'react-router-dom';
import { motion } from 'framer-motion';
import { KeyRound, CheckCircle2 } from 'lucide-react';
import { Button } from '@loopzero/ui';
import { type Flow, getFlow, submitFlow, getCsrfToken, getFlowMessages } from '@/lib/kratos';

import { API_URL } from '@/lib/config';
const ACCOUNTS_URL = API_URL;

export function RecoveryPage() {
  const [searchParams] = useSearchParams();
  const [flow, setFlow] = useState<Flow | null>(null);
  const [phase, setPhase] = useState<'email' | 'code' | 'password' | 'success'>('email');
  const [email, setEmail] = useState('');
  const [code, setCode] = useState('');
  const [loading, setLoading] = useState(false);
  const [errorMessage, setErrorMessage] = useState('');

  useEffect(() => {
    const flowId = searchParams.get('flow');
    if (flowId) {
      getFlow('recovery', flowId).then(f => {
        setFlow(f);
        // If flow has code input, we're in the code phase
        const hasCodeInput = f.ui.nodes.some(n => n.attributes.name === 'code');
        if (hasCodeInput) setPhase('code');
      }).catch(() => {
        window.location.href = `${ACCOUNTS_URL}/self-service/recovery/browser?return_to=${encodeURIComponent(window.location.origin + '/recovery')}`;
      });
    } else {
      // Start new recovery flow
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
      if (messages.some(m => m.includes('error') || m.includes('Error'))) {
        setErrorMessage(messages[0]);
      } else {
        setPhase('code');
      }
    } catch {
      setErrorMessage('Something went wrong. Please try again.');
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
        if (errors.length > 0) setErrorMessage(errors[0]);
      }
    } catch {
      setErrorMessage('Invalid or expired code. Please try again.');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen bg-surface-0 flex items-center justify-center">
      <motion.div initial={{ opacity: 0, y: 20 }} animate={{ opacity: 1, y: 0 }} className="w-full max-w-sm">
        <div className="rounded-2xl border border-white/6 bg-surface-1/80 backdrop-blur-xl p-8 shadow-2xl shadow-black/40">
          <div className="flex h-12 w-12 items-center justify-center rounded-xl bg-accent/10 border border-accent/20 mx-auto mb-6">
            <KeyRound className="h-6 w-6 text-accent" />
          </div>

          {phase === 'email' && (
            <>
              <h1 className="font-display text-xl font-semibold text-white mb-1 text-center">Reset your password</h1>
              <p className="text-sm text-white/40 mb-6 text-center">Enter your email and we'll send you a recovery code.</p>
              <form onSubmit={handleEmailSubmit} className="space-y-4">
                <input type="email" value={email} onChange={e => setEmail(e.target.value)} required placeholder="you@example.com"
                  className="w-full rounded-lg border border-white/8 bg-surface-0 px-3 py-2.5 text-sm text-white placeholder:text-white/20 focus:border-accent/30 focus:outline-hidden" />
                {errorMessage && <p className="text-xs text-danger">{errorMessage}</p>}
                <Button type="submit" size="lg" className="w-full justify-center h-12" disabled={loading}>
                  {loading ? 'Sending...' : 'Send recovery code'}
                </Button>
              </form>
            </>
          )}

          {phase === 'code' && (
            <>
              <h1 className="font-display text-xl font-semibold text-white mb-1 text-center">Enter recovery code</h1>
              <p className="text-sm text-white/40 mb-6 text-center">Check your email for a 6-digit code.</p>
              <form onSubmit={handleCodeSubmit} className="space-y-4">
                <input type="text" value={code} onChange={e => setCode(e.target.value)} required placeholder="Enter code"
                  className="w-full rounded-lg border border-white/8 bg-surface-0 px-3 py-2.5 text-sm text-white placeholder:text-white/20 focus:border-accent/30 focus:outline-hidden text-center tracking-widest text-lg" />
                {errorMessage && <p className="text-xs text-danger text-center">{errorMessage}</p>}
                <Button type="submit" size="lg" className="w-full justify-center h-12" disabled={loading}>
                  {loading ? 'Verifying...' : 'Verify code'}
                </Button>
              </form>
            </>
          )}

          {phase === 'success' && (
            <div className="text-center">
              <CheckCircle2 className="h-12 w-12 text-accent mx-auto mb-4" />
              <h1 className="font-display text-xl font-semibold text-white mb-2">Password reset</h1>
              <p className="text-sm text-white/40 mb-6">Your password has been updated. You can now sign in.</p>
              <Link to="/login">
                <Button size="lg" className="w-full justify-center h-12">Sign in</Button>
              </Link>
            </div>
          )}

          <p className="mt-6 text-center text-sm text-white/30">
            <Link to="/login" className="text-accent hover:text-accent/80">Back to login</Link>
          </p>
        </div>
      </motion.div>
    </div>
  );
}
