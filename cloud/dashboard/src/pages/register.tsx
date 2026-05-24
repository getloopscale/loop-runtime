import { useEffect, useState } from 'react';
import { useNavigate, useSearchParams, Link } from 'react-router-dom';
import { Github, Loader2, Mail, Zap } from 'lucide-react';
import { Alert, Button, Card, Input } from '@loopzero/ui';
import { getSession } from '@/lib/auth';
import { API_URL } from '@/lib/config';

const KRATOS_URL = API_URL;

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
  const [registered, setRegistered] = useState(false);

  useEffect(() => {
    getSession().then((s) => {
      if (s) navigate('/', { replace: true });
      else setLoading(false);
    });
  }, [navigate]);

  useEffect(() => {
    const flowId = searchParams.get('flow');
    if (flowId) {
      fetch(`${KRATOS_URL}/self-service/registration/flows?id=${flowId}`, {
        credentials: 'include',
        headers: { Accept: 'application/json' },
      })
        .then((r) => (r.ok ? r.json() : null))
        .then((f) => f && setFlow(f))
        .catch(() => {});
    } else if (!loading) {
      fetch(`${KRATOS_URL}/self-service/registration/browser`, {
        credentials: 'include',
        headers: { Accept: 'application/json' },
        redirect: 'follow',
      })
        .then((r) => (r.ok ? r.json() : null))
        .then((f) => f?.ui && setFlow(f))
        .catch(() => {});
    }
  }, [searchParams, loading]);

  const submitOIDC = (provider: string) => {
    if (!flow) return;
    const form = document.createElement('form');
    form.method = 'POST';
    form.action = flow.ui.action;
    const csrfNode = flow.ui.nodes.find((n) => n.attributes.name === 'csrf_token');
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
    const csrfNode = flow.ui.nodes.find((n) => n.attributes.name === 'csrf_token');
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
      if (data.redirect_browser_to) {
        window.location.href = data.redirect_browser_to;
        return;
      }
      if (data.session) {
        setRegistered(true);
        setTimeout(() => navigate('/', { replace: true }), 3000);
        return;
      }
      if (data.ui) setFlow(data);
    } catch {
      /* network error */
    } finally {
      setSubmitting(false);
    }
  };

  if (loading) {
    return (
      <div className="min-h-screen bg-background flex items-center justify-center">
        <Loader2 className="size-6 text-primary animate-spin" aria-label="Loading" />
      </div>
    );
  }

  if (registered) {
    return (
      <div className="min-h-screen bg-background flex items-center justify-center p-6">
        <Card className="w-full max-w-sm">
          <Card.Body className="p-8 text-center space-y-4">
            <div className="flex size-12 items-center justify-center rounded-xl bg-primary/10 border border-primary/20 mx-auto">
              <Mail className="size-6 text-primary" aria-hidden="true" />
            </div>
            <div className="space-y-1">
              <h1 className="font-display text-title-sm font-semibold text-foreground">
                Check your email
              </h1>
              <p className="text-body-sm text-muted-foreground">
                We sent a verification code to{' '}
                <span className="text-foreground font-medium">{email}</span>.
              </p>
              <p className="text-label text-muted-foreground">
                Redirecting to dashboard...
              </p>
            </div>
            <Loader2 className="size-4 text-primary animate-spin mx-auto" aria-hidden="true" />
          </Card.Body>
        </Card>
      </div>
    );
  }

  const flowErrors = flow?.ui?.messages?.filter((m) => m.type === 'error') ?? [];
  const passwordErrors =
    flow?.ui?.nodes?.find((n) => n.attributes.name === 'password')?.messages?.filter((m) => m.type === 'error') ?? [];
  const emailErrors =
    flow?.ui?.nodes?.find((n) => n.attributes.name === 'traits.email')?.messages?.filter((m) => m.type === 'error') ?? [];
  const allErrors = [...flowErrors, ...passwordErrors, ...emailErrors];

  return (
    <div className="min-h-screen bg-background flex items-center justify-center p-6">
      <Card className="w-full max-w-sm">
        <Card.Body className="p-8 space-y-6">
          <div className="flex items-center gap-2.5">
            <div className="flex size-10 items-center justify-center rounded-xl bg-primary/10 border border-primary/20">
              <Zap className="size-5 text-primary" aria-hidden="true" />
            </div>
            <span className="font-display text-title-sm font-bold tracking-tight text-foreground">
              LoopZero
            </span>
          </div>

          <div className="space-y-1">
            <h1 className="font-display text-title-sm font-semibold text-foreground">
              Create account
            </h1>
            <p className="text-body-sm text-muted-foreground">
              Start deploying in under 5 minutes.
            </p>
          </div>

          {allErrors.length > 0 && (
            <Alert intent="destructive" title={allErrors[0].text} />
          )}

          <div className="space-y-3">
            <Button
              variant="secondary"
              size="lg"
              className="w-full justify-center gap-3"
              onClick={() => submitOIDC('github')}
            >
              <Github className="size-4" aria-hidden="true" />
              Sign up with GitHub
            </Button>
            <Button
              variant="secondary"
              size="lg"
              className="w-full justify-center gap-3"
              onClick={() => submitOIDC('google')}
            >
              <svg className="size-4" viewBox="0 0 24 24" aria-hidden="true" role="img">
                <path d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92a5.06 5.06 0 0 1-2.2 3.32v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.1z" fill="#4285F4" />
                <path d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z" fill="#34A853" />
                <path d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z" fill="#FBBC05" />
                <path d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z" fill="#EA4335" />
              </svg>
              Sign up with Google
            </Button>
          </div>

          <div className="relative">
            <div className="absolute inset-0 flex items-center">
              <div className="w-full border-t border-border/40" />
            </div>
            <div className="relative flex justify-center">
              <span className="bg-card px-3 text-label text-muted-foreground">
                or with email
              </span>
            </div>
          </div>

          <form onSubmit={submitPassword} className="space-y-3">
            <Input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Name"
              autoComplete="name"
              aria-label="Name"
            />
            <Input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
              placeholder="Email"
              autoComplete="email"
              aria-label="Email"
            />
            <Input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              placeholder="Password (min. 8 characters)"
              autoComplete="new-password"
              aria-label="Password"
            />
            <Button
              type="submit"
              variant="primary"
              size="lg"
              className="w-full justify-center"
              disabled={submitting}
            >
              {submitting ? (
                <Loader2 className="size-4 animate-spin" aria-hidden="true" />
              ) : (
                'Create account'
              )}
            </Button>
          </form>

          <p className="text-center text-body-sm text-muted-foreground">
            Already have an account?{' '}
            <Link to="/login" className="text-primary hover:text-primary/80">
              Sign in
            </Link>
          </p>
        </Card.Body>
      </Card>
    </div>
  );
}
