import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { motion } from 'framer-motion';
import { CheckCircle2, Loader2, Mail, AlertCircle } from 'lucide-react';
import { Button } from '@/components/ui/button';

import { API_URL } from '@/lib/config';
const KRATOS_URL = API_URL;

export function VerificationPage() {
  const [searchParams] = useSearchParams();
  const [status, setStatus] = useState<'loading' | 'input' | 'success' | 'error'>('loading');
  const [code, setCode] = useState(searchParams.get('code') || '');
  const [error, setError] = useState('');

  const flowId = searchParams.get('flow');

  // Auto-submit if code is in URL (from email link)
  useEffect(() => {
    const urlCode = searchParams.get('code');
    if (flowId && urlCode) {
      submitCode(flowId, urlCode);
    } else if (flowId) {
      setStatus('input');
    } else {
      // No flow — start one
      window.location.href = `${KRATOS_URL}/self-service/verification/browser`;
    }
  }, [flowId, searchParams]);

  const submitCode = async (flow: string, verificationCode: string) => {
    setStatus('loading');
    try {
      // Get flow to find action URL and CSRF token
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
      const csrfNode = flowData.ui?.nodes?.find((n: { attributes: { name?: string } }) => n.attributes.name === 'csrf_token');

      // Submit the code
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
    <div className="min-h-screen bg-surface-0 flex items-center justify-center">
      <motion.div initial={{ opacity: 0, y: 20 }} animate={{ opacity: 1, y: 0 }} className="w-full max-w-sm">
        <div className="rounded-2xl border border-white/6 bg-surface-1/80 backdrop-blur-xl p-8 shadow-2xl shadow-black/40 text-center">

          {status === 'loading' && (
            <>
              <Loader2 className="h-8 w-8 text-accent animate-spin mx-auto mb-4" />
              <p className="text-sm text-white/40">Verifying your email...</p>
            </>
          )}

          {status === 'success' && (
            <>
              <CheckCircle2 className="h-12 w-12 text-accent mx-auto mb-4" />
              <h1 className="font-display text-xl font-semibold text-white mb-2">Email verified</h1>
              <p className="text-sm text-white/40 mb-6">Your account is now fully active.</p>
              <a href="/">
                <Button size="lg" className="w-full justify-center h-12">Go to dashboard</Button>
              </a>
            </>
          )}

          {status === 'error' && (
            <>
              <AlertCircle className="h-12 w-12 text-danger mx-auto mb-4" />
              <h1 className="font-display text-xl font-semibold text-white mb-2">Verification failed</h1>
              <p className="text-sm text-white/40 mb-6">{error}</p>
              <a href="/">
                <Button size="lg" className="w-full justify-center h-12">Go to dashboard</Button>
              </a>
            </>
          )}

          {status === 'input' && (
            <>
              <Mail className="h-12 w-12 text-accent/60 mx-auto mb-4" />
              <h1 className="font-display text-xl font-semibold text-white mb-2">Enter verification code</h1>
              <p className="text-sm text-white/40 mb-6">Check your email for a 6-digit code.</p>
              {error && (
                <div className="mb-4 rounded-lg bg-danger/10 border border-danger/20 p-3 text-sm text-danger">
                  {error}
                </div>
              )}
              <form onSubmit={handleSubmit} className="space-y-4">
                <input type="text" value={code} onChange={e => setCode(e.target.value)} required
                  placeholder="Enter code" maxLength={6} autoFocus
                  className="w-full rounded-lg border border-white/8 bg-surface-0 px-3 py-2.5 text-sm text-white placeholder:text-white/20 focus:border-accent/30 focus:outline-hidden text-center tracking-widest text-lg" />
                <Button type="submit" size="lg" className="w-full justify-center h-12">
                  Verify
                </Button>
              </form>
            </>
          )}
        </div>
      </motion.div>
    </div>
  );
}
