import { useEffect, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { motion } from 'framer-motion';
import { Zap, Loader2, AlertCircle } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { startPKCELogin, handleCallback } from '@/lib/auth';
import { isAuthenticated } from '@/lib/api';

export function LoginPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [status, setStatus] = useState<'idle' | 'processing' | 'error'>('idle');
  const [errorMsg, setErrorMsg] = useState('');

  // Already authenticated → go to dashboard
  useEffect(() => {
    if (isAuthenticated()) {
      navigate('/', { replace: true });
    }
  }, [navigate]);

  // Handle OAuth callback (?code=...&state=...)
  useEffect(() => {
    const code = searchParams.get('code');
    const state = searchParams.get('state');
    const error = searchParams.get('error');

    if (error) {
      setStatus('error');
      setErrorMsg(searchParams.get('error_description') || 'Authentication failed. Please try again.');
      return;
    }

    if (code && state) {
      setStatus('processing');
      handleCallback(code, state).then(success => {
        if (success) {
          navigate('/', { replace: true });
        } else {
          setStatus('error');
          setErrorMsg('Failed to complete sign in. Please try again.');
        }
      });
    }
  }, [searchParams, navigate]);

  if (status === 'processing') {
    return (
      <div className="min-h-screen bg-surface-0 flex items-center justify-center">
        <div className="text-center">
          <Loader2 className="h-8 w-8 text-accent animate-spin mx-auto mb-4" />
          <p className="text-sm text-white/40">Signing you in...</p>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-surface-0 flex items-center justify-center relative overflow-hidden">
      <div className="absolute top-1/4 -left-32 w-96 h-96 bg-accent/[0.04] rounded-full blur-[120px]" />
      <motion.div initial={{ opacity: 0, y: 20 }} animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.5 }} className="relative w-full max-w-sm">
        <div className="rounded-2xl border border-white/[0.06] bg-surface-1/80 backdrop-blur-xl p-8 shadow-2xl shadow-black/40">
          <div className="flex items-center gap-2.5 mb-8">
            <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-accent/10 border border-accent/20">
              <Zap className="h-5 w-5 text-accent" />
            </div>
            <span className="font-display text-xl font-bold tracking-tight text-white">LoopZero</span>
          </div>

          <h1 className="font-display text-xl font-semibold text-white mb-1">Sign in</h1>
          <p className="text-sm text-white/40 mb-6">Deploy your apps in under 5 minutes.</p>

          {status === 'error' && (
            <div className="mb-4 rounded-lg bg-danger/10 border border-danger/20 p-3 text-sm text-danger flex items-start gap-2">
              <AlertCircle className="h-4 w-4 mt-0.5 flex-shrink-0" />
              <span>{errorMsg}</span>
            </div>
          )}

          <Button size="lg" className="w-full justify-center h-12" onClick={() => startPKCELogin()}>
            Sign in with GitHub or Google
          </Button>

          <div className="relative my-8">
            <div className="absolute inset-0 flex items-center"><div className="w-full border-t border-white/[0.06]" /></div>
            <div className="relative flex justify-center"><span className="bg-surface-1/80 px-3 text-xs text-white/20">or use the CLI</span></div>
          </div>
          <div className="rounded-lg bg-surface-0 border border-white/[0.06] p-3 font-mono text-sm text-center">
            <span className="text-accent/60">$</span>
            <span className="text-white/50 ml-2">loopzero login</span>
          </div>
        </div>
      </motion.div>
    </div>
  );
}
