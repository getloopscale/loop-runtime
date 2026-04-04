import { useEffect, useState } from 'react';
import { useSearchParams, Link } from 'react-router-dom';
import { motion } from 'framer-motion';
import { Zap, UserPlus } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { type Flow, getFlow, submitFlow, getCsrfToken, getFlowMessages, getFieldErrors } from '@/lib/kratos';

const ACCOUNTS_URL = import.meta.env.VITE_ACCOUNTS_URL || 'http://localhost:4433';

export function RegisterPage() {
  const [searchParams] = useSearchParams();
  const [flow, setFlow] = useState<Flow | null>(null);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [name, setName] = useState('');
  const [loading, setLoading] = useState(false);
  const [success, setSuccess] = useState(false);

  useEffect(() => {
    const flowId = searchParams.get('flow');
    if (flowId) {
      getFlow('registration', flowId).then(setFlow).catch(() => {
        // Flow expired — start a new one
        window.location.href = `${ACCOUNTS_URL}/self-service/registration/browser?return_to=${encodeURIComponent(window.location.origin + '/register')}`;
      });
    } else {
      window.location.href = `${ACCOUNTS_URL}/self-service/registration/browser?return_to=${encodeURIComponent(window.location.origin + '/register')}`;
    }
  }, [searchParams]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!flow) return;
    setLoading(true);
    try {
      const result = await submitFlow(flow.ui.action, flow.ui.method, {
        csrf_token: getCsrfToken(flow),
        'traits.email': email,
        'traits.name': name,
        password,
        method: 'password',
      });
      if (result.ui?.messages?.some(m => m.type === 'error')) {
        setFlow(result);
      } else {
        setSuccess(true);
      }
    } catch {
      // Unexpected error
    } finally {
      setLoading(false);
    }
  };

  if (success) {
    return (
      <div className="min-h-screen bg-surface-0 flex items-center justify-center">
        <motion.div initial={{ opacity: 0, y: 20 }} animate={{ opacity: 1, y: 0 }} className="w-full max-w-sm">
          <div className="rounded-2xl border border-white/[0.06] bg-surface-1/80 backdrop-blur-xl p-8 shadow-2xl shadow-black/40 text-center">
            <div className="flex h-12 w-12 items-center justify-center rounded-xl bg-accent/10 border border-accent/20 mx-auto mb-4">
              <UserPlus className="h-6 w-6 text-accent" />
            </div>
            <h1 className="font-display text-xl font-semibold text-white mb-2">Check your email</h1>
            <p className="text-sm text-white/40 mb-6">We sent a verification link to <span className="text-white/60">{email}</span>. Click it to activate your account.</p>
            <Link to="/login" className="text-sm text-accent hover:text-accent/80">Back to login</Link>
          </div>
        </motion.div>
      </div>
    );
  }

  const flowMessages = flow ? getFlowMessages(flow) : [];
  const emailErrors = flow ? getFieldErrors(flow, 'traits.email') : [];
  const passwordErrors = flow ? getFieldErrors(flow, 'password') : [];

  return (
    <div className="min-h-screen bg-surface-0 flex items-center justify-center relative overflow-hidden">
      <div className="absolute top-1/4 -left-32 w-96 h-96 bg-accent/[0.04] rounded-full blur-[120px]" />
      <motion.div initial={{ opacity: 0, y: 20 }} animate={{ opacity: 1, y: 0 }} className="relative w-full max-w-sm">
        <div className="rounded-2xl border border-white/[0.06] bg-surface-1/80 backdrop-blur-xl p-8 shadow-2xl shadow-black/40">
          <div className="flex items-center gap-2.5 mb-8">
            <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-accent/10 border border-accent/20">
              <Zap className="h-5 w-5 text-accent" />
            </div>
            <span className="font-display text-xl font-bold tracking-tight text-white">LoopZero</span>
          </div>

          <h1 className="font-display text-xl font-semibold text-white mb-1">Create your account</h1>
          <p className="text-sm text-white/40 mb-6">Start deploying in under 5 minutes.</p>

          {flowMessages.map((msg, i) => (
            <div key={i} className="mb-4 rounded-lg bg-danger/10 border border-danger/20 p-3 text-sm text-danger">{msg}</div>
          ))}

          <form onSubmit={handleSubmit} className="space-y-4">
            <div>
              <label className="block text-xs text-white/40 mb-1.5">Name</label>
              <input type="text" value={name} onChange={e => setName(e.target.value)} placeholder="Your name"
                className="w-full rounded-lg border border-white/[0.08] bg-surface-0 px-3 py-2.5 text-sm text-white placeholder:text-white/20 focus:border-accent/30 focus:outline-none" />
            </div>
            <div>
              <label className="block text-xs text-white/40 mb-1.5">Email</label>
              <input type="email" value={email} onChange={e => setEmail(e.target.value)} required placeholder="you@example.com"
                className="w-full rounded-lg border border-white/[0.08] bg-surface-0 px-3 py-2.5 text-sm text-white placeholder:text-white/20 focus:border-accent/30 focus:outline-none" />
              {emailErrors.map((err, i) => <p key={i} className="mt-1 text-xs text-danger">{err}</p>)}
            </div>
            <div>
              <label className="block text-xs text-white/40 mb-1.5">Password</label>
              <input type="password" value={password} onChange={e => setPassword(e.target.value)} required placeholder="Min. 8 characters"
                className="w-full rounded-lg border border-white/[0.08] bg-surface-0 px-3 py-2.5 text-sm text-white placeholder:text-white/20 focus:border-accent/30 focus:outline-none" />
              {passwordErrors.map((err, i) => <p key={i} className="mt-1 text-xs text-danger">{err}</p>)}
            </div>
            <Button type="submit" size="lg" className="w-full justify-center h-12" disabled={loading}>
              {loading ? 'Creating account...' : 'Create account'}
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
