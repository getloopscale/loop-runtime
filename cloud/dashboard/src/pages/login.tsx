import { useEffect, useState } from 'react';
import { useNavigate, useSearchParams, Link } from 'react-router-dom';
import { motion } from 'framer-motion';
import { Zap, Github, Loader2, AlertCircle } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { getSession, startLogin } from '@/lib/auth';
import { clearToken } from '@/lib/api';

import { API_URL } from '@/lib/config';
const KRATOS_URL = API_URL;

interface FlowNode {
  group: string;
  attributes: { name?: string; type?: string; value?: string };
  messages?: { text: string; type: string }[];
  meta?: { label?: { text?: string } };
}

interface Flow {
  id: string;
  ui: {
    action: string;
    method: string;
    nodes: FlowNode[];
    messages?: { text: string; type: string }[];
  };
}

export function LoginPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [flow, setFlow] = useState<Flow | null>(null);
  const [loading, setLoading] = useState(true);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [submitting, setSubmitting] = useState(false);

  // Clear stale JWT on login page visit (user may have been redirected here)
  useEffect(() => {
    clearToken();
  }, []);

  // Check if already authenticated
  useEffect(() => {
    getSession().then(s => {
      if (s) navigate('/', { replace: true });
      else setLoading(false);
    });
  }, [navigate]);

  // Load or create Kratos login flow
  useEffect(() => {
    const flowId = searchParams.get('flow');
    if (flowId) {
      fetch(`${KRATOS_URL}/self-service/login/flows?id=${flowId}`, {
        credentials: 'include',
        headers: { Accept: 'application/json' },
      })
        .then(r => r.ok ? r.json() : null)
        .then(f => { if (f) setFlow(f); })
        .catch(() => {});
    } else if (!loading) {
      fetch(`${KRATOS_URL}/self-service/login/browser`, {
        credentials: 'include',
        headers: { Accept: 'application/json' },
        redirect: 'follow',
      })
        .then(r => r.ok ? r.json() : null)
        .then(f => { if (f?.ui) setFlow(f); })
        .catch(() => {});
    }
  }, [searchParams, loading]);

  // Submit OIDC provider (GitHub/Google)
  const submitOIDC = (provider: string) => {
    if (!flow) { startLogin(); return; }
    const form = document.createElement('form');
    form.method = 'POST';
    form.action = flow.ui.action;
    const csrfNode = flow.ui.nodes.find(n => n.attributes.name === 'csrf_token');
    for (const [name, value] of [
      ['csrf_token', csrfNode?.attributes.value || ''],
      ['method', 'oidc'],
      ['provider', provider],
    ]) {
      const input = document.createElement('input');
      input.type = 'hidden';
      input.name = name;
      input.value = value;
      form.appendChild(input);
    }
    document.body.appendChild(form);
    form.submit();
  };

  // Submit password login
  const submitPassword = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!flow) return;
    setSubmitting(true);

    const csrfNode = flow.ui.nodes.find(n => n.attributes.name === 'csrf_token');

    try {
      const resp = await fetch(flow.ui.action, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
        body: JSON.stringify({
          csrf_token: csrfNode?.attributes.value || '',
          method: 'password',
          identifier: email,
          password,
        }),
      });

      const data = await resp.json();

      if (data.redirect_browser_to) {
        window.location.href = data.redirect_browser_to;
        return;
      }

      if (data.session) {
        // Login succeeded — session created
        navigate('/', { replace: true });
        return;
      }

      // Errors — update flow to show them
      if (data.ui) {
        setFlow(data);
      }
    } catch {
      // Network error
    } finally {
      setSubmitting(false);
    }
  };

  if (loading) {
    return (
      <div className="min-h-screen bg-surface-0 flex items-center justify-center">
        <Loader2 className="h-6 w-6 text-accent animate-spin" />
      </div>
    );
  }

  // Collect errors from flow
  const flowErrors = flow?.ui?.messages?.filter(m => m.type === 'error') || [];
  const passwordErrors = flow?.ui?.nodes
    ?.find(n => n.attributes.name === 'password')
    ?.messages?.filter(m => m.type === 'error') || [];
  const identifierErrors = flow?.ui?.nodes
    ?.find(n => n.attributes.name === 'identifier')
    ?.messages?.filter(m => m.type === 'error') || [];
  const allErrors = [...flowErrors, ...passwordErrors, ...identifierErrors];

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

          {/* §3.6: Session expired notification when redirected from auth failure */}
          {searchParams.get('reason') === 'expired' && allErrors.length === 0 && (
            <div className="mb-4 rounded-lg bg-yellow-500/10 border border-yellow-500/20 p-3 text-sm text-yellow-300 flex items-start gap-2">
              <AlertCircle className="h-4 w-4 mt-0.5 flex-shrink-0" />
              <span>Your session expired. Please sign in again.</span>
            </div>
          )}

          {allErrors.length > 0 && (
            <div className="mb-4 rounded-lg bg-danger/10 border border-danger/20 p-3 text-sm text-danger flex items-start gap-2">
              <AlertCircle className="h-4 w-4 mt-0.5 flex-shrink-0" />
              <span>{allErrors[0].text}</span>
            </div>
          )}

          {/* Social login */}
          <div className="space-y-3">
            <Button variant="secondary" size="lg" className="w-full justify-center gap-3 h-12"
              onClick={() => submitOIDC('github')}>
              <Github className="h-4 w-4" />
              Continue with GitHub
            </Button>

            <Button variant="secondary" size="lg" className="w-full justify-center gap-3 h-12"
              onClick={() => submitOIDC('google')}>
              <svg className="h-4 w-4" viewBox="0 0 24 24"><path d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92a5.06 5.06 0 0 1-2.2 3.32v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.1z" fill="#4285F4"/><path d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z" fill="#34A853"/><path d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z" fill="#FBBC05"/><path d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z" fill="#EA4335"/></svg>
              Continue with Google
            </Button>
          </div>

          {/* Divider */}
          <div className="relative my-6">
            <div className="absolute inset-0 flex items-center"><div className="w-full border-t border-white/[0.06]" /></div>
            <div className="relative flex justify-center"><span className="bg-surface-1/80 px-3 text-xs text-white/20">or with email</span></div>
          </div>

          {/* Email/password form */}
          <form onSubmit={submitPassword} className="space-y-3">
            <input type="email" value={email} onChange={e => setEmail(e.target.value)} required
              placeholder="Email" autoComplete="email"
              className="w-full rounded-lg border border-white/[0.08] bg-surface-0 px-3 py-2.5 text-sm text-white placeholder:text-white/20 focus:border-accent/30 focus:outline-none" />
            <input type="password" value={password} onChange={e => setPassword(e.target.value)} required
              placeholder="Password" autoComplete="current-password"
              className="w-full rounded-lg border border-white/[0.08] bg-surface-0 px-3 py-2.5 text-sm text-white placeholder:text-white/20 focus:border-accent/30 focus:outline-none" />
            <Button type="submit" size="lg" className="w-full justify-center h-12" disabled={submitting}>
              {submitting ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Sign in'}
            </Button>
          </form>

          {/* Footer links */}
          <div className="mt-6 flex justify-between text-sm text-white/30">
            <Link to="/register" className="text-accent hover:text-accent/80">Create account</Link>
            <Link to="/recovery" className="hover:text-white/50">Forgot password?</Link>
          </div>

          {/* CLI hint */}
          <div className="relative my-6">
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
