import { useEffect } from 'react';
import { useNavigate, useSearchParams, Link } from 'react-router-dom';
import { motion } from 'framer-motion';
import { Zap, LogIn, Loader2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { startPKCELogin, handleCallback } from '@/lib/auth';
import { isAuthenticated } from '@/lib/api';

// MEET-2026-04-05-032: Dashboard is a pure PKCE client.
// Login UI is rendered by Kratos Self-Service UI at accounts.{domain}.
// Dashboard only handles: (1) start PKCE, (2) receive callback with code.

export function LoginPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();

  // Handle OAuth callback (?code=...&state=...)
  useEffect(() => {
    const code = searchParams.get('code');
    const state = searchParams.get('state');
    if (code && state) {
      handleCallback(code, state).then(success => {
        navigate(success ? '/' : '/login?error=auth_failed', { replace: true });
      });
    }
  }, [searchParams, navigate]);

  // Already authenticated — redirect to dashboard
  useEffect(() => {
    if (isAuthenticated() && !searchParams.get('code')) {
      navigate('/', { replace: true });
    }
  }, [navigate, searchParams]);

  const isCallback = !!searchParams.get('code');
  const error = searchParams.get('error');

  // Show loading while processing callback
  if (isCallback) {
    return (
      <div className="min-h-screen bg-surface-0 flex items-center justify-center">
        <Loader2 className="h-8 w-8 text-accent animate-spin" />
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-surface-0 flex items-center justify-center relative overflow-hidden">
      <div className="absolute top-1/4 -left-32 w-96 h-96 bg-accent/[0.04] rounded-full blur-[120px]" />
      <div className="absolute bottom-1/4 -right-32 w-96 h-96 bg-loopzero-500/[0.03] rounded-full blur-[120px]" />

      <motion.div
        initial={{ opacity: 0, y: 20, scale: 0.98 }}
        animate={{ opacity: 1, y: 0, scale: 1 }}
        transition={{ duration: 0.5, ease: [0.25, 0.46, 0.45, 0.94] }}
        className="relative w-full max-w-sm"
      >
        <div className="rounded-2xl border border-white/[0.06] bg-surface-1/80 backdrop-blur-xl p-8 shadow-2xl shadow-black/40">
          <motion.div
            initial={{ scale: 0.8, opacity: 0 }}
            animate={{ scale: 1, opacity: 1 }}
            transition={{ delay: 0.1, type: 'spring', stiffness: 300, damping: 25 }}
            className="flex items-center gap-2.5 mb-8"
          >
            <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-accent/10 border border-accent/20">
              <Zap className="h-5 w-5 text-accent" />
            </div>
            <span className="font-display text-xl font-bold tracking-tight text-white">LoopZero</span>
          </motion.div>

          <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} transition={{ delay: 0.2 }}>
            <h1 className="font-display text-xl font-semibold text-white mb-1">Welcome back</h1>
            <p className="text-sm text-white/40 mb-6">Sign in to your deployment platform.</p>

            {error && (
              <div className="mb-4 rounded-lg bg-danger/10 border border-danger/20 p-3 text-sm text-danger">
                Authentication failed. Please try again.
              </div>
            )}
          </motion.div>

          <motion.div initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.3 }} className="space-y-3">
            <Button size="lg" className="w-full justify-center gap-3 h-12" onClick={() => startPKCELogin()}>
              <LogIn className="h-4 w-4" />
              Sign in
            </Button>

            <p className="text-center text-xs text-white/30 mt-2">
              Sign in with GitHub, Google, or email
            </p>
          </motion.div>

          <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} transition={{ delay: 0.5 }}>
            <div className="relative my-8">
              <div className="absolute inset-0 flex items-center">
                <div className="w-full border-t border-white/[0.06]" />
              </div>
              <div className="relative flex justify-center">
                <span className="bg-surface-1/80 px-3 text-xs text-white/20">or use the CLI</span>
              </div>
            </div>
            <div className="rounded-lg bg-surface-0 border border-white/[0.06] p-3 font-mono text-sm text-center">
              <span className="text-accent/60">$</span>
              <span className="text-white/50 ml-2">loopzero login</span>
            </div>
          </motion.div>
        </div>
      </motion.div>
    </div>
  );
}
