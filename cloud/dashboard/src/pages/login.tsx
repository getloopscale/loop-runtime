import { useEffect, useState } from 'react';
import { useNavigate, useSearchParams, Link } from 'react-router-dom';
import { motion } from 'framer-motion';
import { Github, Mail, Zap, Loader2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { startPKCELogin, handleCallback } from '@/lib/auth';
import { isAuthenticated } from '@/lib/api';
import { type Flow, getFlow, getCsrfToken } from '@/lib/kratos';

const ACCOUNTS_URL = import.meta.env.VITE_ACCOUNTS_URL || 'http://localhost:4433';

export function LoginPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [flow, setFlow] = useState<Flow | null>(null);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

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

  // Load Kratos flow when ?flow= is present
  useEffect(() => {
    const flowId = searchParams.get('flow');
    if (flowId) {
      getFlow('login', flowId).then(setFlow).catch(() => {
        setError('Session expired. Please try again.');
      });
    }
  }, [searchParams]);

  // If no flow yet, start PKCE which will redirect through Hydra → Kratos → back here with ?flow=
  const handleStartLogin = () => {
    if (flow) return; // Already have a flow
    startPKCELogin();
  };

  // Submit OIDC login (GitHub/Google) via Kratos flow
  const handleOIDCLogin = (provider: string) => {
    if (!flow) {
      // No flow yet — start PKCE which goes through Hydra → Kratos → back here with flow
      startPKCELogin();
      return;
    }
    // Submit to Kratos flow action with OIDC method
    const form = document.createElement('form');
    form.method = 'POST';
    form.action = flow.ui.action;

    const csrf = document.createElement('input');
    csrf.type = 'hidden';
    csrf.name = 'csrf_token';
    csrf.value = getCsrfToken(flow);
    form.appendChild(csrf);

    const method = document.createElement('input');
    method.type = 'hidden';
    method.name = 'method';
    method.value = 'oidc';
    form.appendChild(method);

    const providerInput = document.createElement('input');
    providerInput.type = 'hidden';
    providerInput.name = 'provider';
    providerInput.value = provider;
    form.appendChild(providerInput);

    document.body.appendChild(form);
    form.submit();
  };

  // Submit password login via Kratos flow
  const handlePasswordLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!flow) return;
    setLoading(true);
    setError('');

    try {
      const resp = await fetch(flow.ui.action, {
        method: flow.ui.method,
        headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
        credentials: 'include',
        body: JSON.stringify({
          csrf_token: getCsrfToken(flow),
          method: 'password',
          identifier: email,
          password,
        }),
      });

      if (resp.status === 422 || resp.status === 200) {
        const data = await resp.json();
        if (data.redirect_browser_to) {
          // Kratos wants browser redirect (e.g., back to Hydra to complete OAuth)
          window.location.href = data.redirect_browser_to;
          return;
        }
        if (data.ui?.messages) {
          const msgs = data.ui.messages.filter((m: { type: string }) => m.type === 'error');
          setError(msgs.map((m: { text: string }) => m.text).join('. ') || 'Login failed.');
          setFlow(data);
        }
      } else if (resp.status >= 300 && resp.status < 400) {
        // Redirect — login succeeded, follow it
        const location = resp.headers.get('Location');
        if (location) window.location.href = location;
      }
    } catch {
      setError('Something went wrong. Please try again.');
    } finally {
      setLoading(false);
    }
  };

  const errorParam = searchParams.get('error');
  const hasFlow = !!flow;

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

            {(error || errorParam) && (
              <div className="mb-4 rounded-lg bg-danger/10 border border-danger/20 p-3 text-sm text-danger">
                {error || 'Authentication failed. Please try again.'}
              </div>
            )}
          </motion.div>

          <motion.div initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.3 }} className="space-y-3">
            {/* Social login buttons — use Kratos OIDC when flow exists, PKCE to start */}
            <Button variant="secondary" size="lg" className="w-full justify-center gap-3 h-12" onClick={() => handleOIDCLogin('github')}>
              <Github className="h-4 w-4" />
              Continue with GitHub
            </Button>

            <Button variant="secondary" size="lg" className="w-full justify-center gap-3 h-12" onClick={() => handleOIDCLogin('google')}>
              <svg className="h-4 w-4" viewBox="0 0 24 24"><path d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92a5.06 5.06 0 0 1-2.2 3.32v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.1z" fill="#4285F4"/><path d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z" fill="#34A853"/><path d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z" fill="#FBBC05"/><path d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z" fill="#EA4335"/></svg>
              Continue with Google
            </Button>

            {/* Email/password form — only show when Kratos flow is loaded */}
            {hasFlow && (
              <>
                <div className="relative my-4">
                  <div className="absolute inset-0 flex items-center">
                    <div className="w-full border-t border-white/[0.06]" />
                  </div>
                  <div className="relative flex justify-center">
                    <span className="bg-surface-1/80 px-3 text-xs text-white/20">or with email</span>
                  </div>
                </div>

                <form onSubmit={handlePasswordLogin} className="space-y-3">
                  <input type="email" value={email} onChange={e => setEmail(e.target.value)} required placeholder="Email"
                    className="w-full rounded-lg border border-white/[0.08] bg-surface-0 px-3 py-2.5 text-sm text-white placeholder:text-white/20 focus:border-accent/30 focus:outline-none" />
                  <input type="password" value={password} onChange={e => setPassword(e.target.value)} required placeholder="Password"
                    className="w-full rounded-lg border border-white/[0.08] bg-surface-0 px-3 py-2.5 text-sm text-white placeholder:text-white/20 focus:border-accent/30 focus:outline-none" />
                  <Button type="submit" size="lg" className="w-full justify-center h-12" disabled={loading}>
                    {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Sign in'}
                  </Button>
                </form>
              </>
            )}

            {/* If no flow yet, show email button that starts the flow */}
            {!hasFlow && (
              <Button variant="secondary" size="lg" className="w-full justify-center gap-3 h-12" onClick={handleStartLogin}>
                <Mail className="h-4 w-4" />
                Continue with Email
              </Button>
            )}
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

            <div className="mt-4 flex justify-between text-sm text-white/30">
              <Link to="/register" className="text-accent hover:text-accent/80">Create account</Link>
              <Link to="/recovery" className="hover:text-white/50">Forgot password?</Link>
            </div>
          </motion.div>
        </div>
      </motion.div>
    </div>
  );
}
