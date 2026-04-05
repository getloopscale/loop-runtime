import { useEffect, useState } from 'react';
import { useNavigate, useSearchParams, Link } from 'react-router-dom';
import { motion } from 'framer-motion';
import { Zap, Github, Loader2, AlertCircle } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { getSession } from '@/lib/auth';

const KRATOS_URL = import.meta.env.VITE_API_URL || 'http://localhost:13002';

interface FlowNode {
  group: string;
  attributes: { name?: string; type?: string; value?: string };
  messages?: { text: string; type: string }[];
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

export function RegisterPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [flow, setFlow] = useState<Flow | null>(null);
  const [loading, setLoading] = useState(true);
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    getSession().then(s => {
      if (s) navigate('/', { replace: true });
      else setLoading(false);
    });
  }, [navigate]);

  // Load or create registration flow
  useEffect(() => {
    const flowId = searchParams.get('flow');
    if (flowId) {
      fetch(`${KRATOS_URL}/self-service/registration/flows?id=${flowId}`, {
        credentials: 'include',
        headers: { Accept: 'application/json' },
      })
        .then(r => r.ok ? r.json() : null)
        .then(f => { if (f) setFlow(f); })
        .catch(() => {});
    } else if (!loading) {
      fetch(`${KRATOS_URL}/self-service/registration/browser`, {
        credentials: 'include',
        headers: { Accept: 'application/json' },
        redirect: 'follow',
      })
        .then(r => r.ok ? r.json() : null)
        .then(f => { if (f?.ui) setFlow(f); })
        .catch(() => {});
    }
  }, [searchParams, loading]);

  const submitOIDC = (provider: string) => {
    if (!flow) return;
    const form = document.createElement('form');
    form.method = 'POST';
    form.action = flow.ui.action;
    const csrfNode = flow.ui.nodes.find(n => n.attributes.name === 'csrf_token');
    for (const [k, v] of [
      ['csrf_token', csrfNode?.attributes.value || ''],
      ['method', 'oidc'],
      ['provider', provider],
    ]) {
      const input = document.createElement('input');
      input.type = 'hidden';
      input.name = k;
      input.value = v;
      form.appendChild(input);
    }
    document.body.appendChild(form);
    form.submit();
  };

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
          'traits.email': email,
          'traits.name': name,
          password,
        }),
      });
      const data = await resp.json();
      if (data.redirect_browser_to) { window.location.href = data.redirect_browser_to; return; }
      if (data.session) { navigate('/', { replace: true }); return; }
      if (data.ui) setFlow(data);
    } catch { /* */ } finally { setSubmitting(false); }
  };

  if (loading) {
    return <div className="min-h-screen bg-surface-0 flex items-center justify-center"><Loader2 className="h-6 w-6 text-accent animate-spin" /></div>;
  }

  const flowErrors = flow?.ui?.messages?.filter(m => m.type === 'error') || [];
  const passwordErrors = flow?.ui?.nodes?.find(n => n.attributes.name === 'password')?.messages?.filter(m => m.type === 'error') || [];
  const emailErrors = flow?.ui?.nodes?.find(n => n.attributes.name === 'traits.email')?.messages?.filter(m => m.type === 'error') || [];
  const allErrors = [...flowErrors, ...passwordErrors, ...emailErrors];

  return (
    <div className="min-h-screen bg-surface-0 flex items-center justify-center relative overflow-hidden">
      <div className="absolute top-1/4 -left-32 w-96 h-96 bg-accent/[0.04] rounded-full blur-[120px]" />
      <motion.div initial={{ opacity: 0, y: 20 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.5 }} className="relative w-full max-w-sm">
        <div className="rounded-2xl border border-white/[0.06] bg-surface-1/80 backdrop-blur-xl p-8 shadow-2xl shadow-black/40">
          <div className="flex items-center gap-2.5 mb-8">
            <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-accent/10 border border-accent/20">
              <Zap className="h-5 w-5 text-accent" />
            </div>
            <span className="font-display text-xl font-bold tracking-tight text-white">LoopZero</span>
          </div>

          <h1 className="font-display text-xl font-semibold text-white mb-1">Create account</h1>
          <p className="text-sm text-white/40 mb-6">Start deploying in under 5 minutes.</p>

          {allErrors.length > 0 && (
            <div className="mb-4 rounded-lg bg-danger/10 border border-danger/20 p-3 text-sm text-danger flex items-start gap-2">
              <AlertCircle className="h-4 w-4 mt-0.5 flex-shrink-0" />
              <span>{allErrors[0].text}</span>
            </div>
          )}

          <div className="space-y-3">
            <Button variant="secondary" size="lg" className="w-full justify-center gap-3 h-12" onClick={() => submitOIDC('github')}>
              <Github className="h-4 w-4" />
              Sign up with GitHub
            </Button>
            <Button variant="secondary" size="lg" className="w-full justify-center gap-3 h-12" onClick={() => submitOIDC('google')}>
              <svg className="h-4 w-4" viewBox="0 0 24 24"><path d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92a5.06 5.06 0 0 1-2.2 3.32v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.1z" fill="#4285F4"/><path d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z" fill="#34A853"/><path d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z" fill="#FBBC05"/><path d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z" fill="#EA4335"/></svg>
              Sign up with Google
            </Button>
          </div>

          <div className="relative my-6">
            <div className="absolute inset-0 flex items-center"><div className="w-full border-t border-white/[0.06]" /></div>
            <div className="relative flex justify-center"><span className="bg-surface-1/80 px-3 text-xs text-white/20">or with email</span></div>
          </div>

          <form onSubmit={submitPassword} className="space-y-3">
            <input type="text" value={name} onChange={e => setName(e.target.value)} placeholder="Name" autoComplete="name"
              className="w-full rounded-lg border border-white/[0.08] bg-surface-0 px-3 py-2.5 text-sm text-white placeholder:text-white/20 focus:border-accent/30 focus:outline-none" />
            <input type="email" value={email} onChange={e => setEmail(e.target.value)} required placeholder="Email" autoComplete="email"
              className="w-full rounded-lg border border-white/[0.08] bg-surface-0 px-3 py-2.5 text-sm text-white placeholder:text-white/20 focus:border-accent/30 focus:outline-none" />
            <input type="password" value={password} onChange={e => setPassword(e.target.value)} required placeholder="Password (min. 8 characters)" autoComplete="new-password"
              className="w-full rounded-lg border border-white/[0.08] bg-surface-0 px-3 py-2.5 text-sm text-white placeholder:text-white/20 focus:border-accent/30 focus:outline-none" />
            <Button type="submit" size="lg" className="w-full justify-center h-12" disabled={submitting}>
              {submitting ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Create account'}
            </Button>
          </form>

          <p className="mt-6 text-center text-sm text-white/30">
            Already have an account? <Link to="/login" className="text-accent hover:text-accent/80">Sign in</Link>
          </p>
        </div>
      </motion.div>
    </div>
  );
}
