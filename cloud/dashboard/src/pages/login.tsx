import { useEffect, useState } from 'react';
import { useNavigate, useSearchParams, Link } from 'react-router-dom';
import { Github, Loader2, Zap } from 'lucide-react';
import {
  Alert,
  Button,
  Card,
  CodeBlock,
  Input,
} from '@loopzero/ui';
import { GoogleBrandIcon } from '@/components/google-brand-icon';
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

/**
 * Google logo as Lucide-equivalent inline SVG WAS at line 200 of v1 —
 * removed per D-7 v1.2 anti-pattern fix (.llms.txt line 118: never inline
 * SVG). Replaced by Lucide Chrome icon (proxy — there is no official
 * Lucide Google icon; Chrome is the closest brand-neutral proxy).
 */

export function LoginPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [flow, setFlow] = useState<Flow | null>(null);
  const [loading, setLoading] = useState(true);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    clearToken();
  }, []);

  useEffect(() => {
    getSession().then((s) => {
      if (s) navigate('/', { replace: true });
      else setLoading(false);
    });
  }, [navigate]);

  useEffect(() => {
    const flowId = searchParams.get('flow');
    if (flowId) {
      fetch(`${KRATOS_URL}/self-service/login/flows?id=${flowId}`, {
        credentials: 'include',
        headers: { Accept: 'application/json' },
      })
        .then((r) => (r.ok ? r.json() : null))
        .then((f) => {
          if (f) setFlow(f);
        })
        .catch(() => {});
    } else if (!loading) {
      fetch(`${KRATOS_URL}/self-service/login/browser`, {
        credentials: 'include',
        headers: { Accept: 'application/json' },
        redirect: 'follow',
      })
        .then((r) => (r.ok ? r.json() : null))
        .then((f) => {
          if (f?.ui) setFlow(f);
        })
        .catch(() => {});
    }
  }, [searchParams, loading]);

  /**
   * CRITICAL EC-4 v1.1 + D-7 v1.2: preserve vanilla DOM form.submit() —
   * Kratos requires browser-level form POST to follow OIDC redirect.
   * Substituting fetch breaks GitHub/Google login (302 returned but
   * browser never navigates).
   */
  const submitOIDC = (provider: string) => {
    if (!flow) {
      startLogin();
      return;
    }
    const form = document.createElement('form');
    form.method = 'POST';
    form.action = flow.ui.action;
    const csrfNode = flow.ui.nodes.find(
      (n) => n.attributes.name === 'csrf_token',
    );
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

  const submitPassword = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!flow) return;
    setSubmitting(true);

    const csrfNode = flow.ui.nodes.find(
      (n) => n.attributes.name === 'csrf_token',
    );

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
        navigate('/', { replace: true });
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
        <Loader2
          className="size-6 text-primary animate-spin"
          aria-label="Loading"
        />
      </div>
    );
  }

  const flowErrors =
    flow?.ui?.messages?.filter((m) => m.type === 'error') ?? [];
  const passwordErrors =
    flow?.ui?.nodes
      ?.find((n) => n.attributes.name === 'password')
      ?.messages?.filter((m) => m.type === 'error') ?? [];
  const identifierErrors =
    flow?.ui?.nodes
      ?.find((n) => n.attributes.name === 'identifier')
      ?.messages?.filter((m) => m.type === 'error') ?? [];
  const allErrors = [...flowErrors, ...passwordErrors, ...identifierErrors];

  return (
    <div className="min-h-screen bg-background flex items-center justify-center p-6">
      <Card className="w-full max-w-sm">
        <Card.Body className="p-8 space-y-6">
          {/* Brand */}
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
              Sign in
            </h1>
            <p className="text-body-sm text-muted-foreground">
              Deploy your apps in under 5 minutes.
            </p>
          </div>

          {/* Session expired notice (§3.6) */}
          {searchParams.get('reason') === 'expired' && allErrors.length === 0 && (
            <Alert
              intent="warning"
              description="Your session expired. Please sign in again."
              title="Session expired"
            />
          )}

          {allErrors.length > 0 && (
            <Alert intent="destructive" title={allErrors[0].text} />
          )}

          {/* Social login */}
          <div className="space-y-3">
            <Button
              variant="secondary"
              size="lg"
              className="w-full justify-center gap-3"
              onClick={() => submitOIDC('github')}
            >
              <Github className="size-4" aria-hidden="true" />
              Continue with GitHub
            </Button>

            <Button
              variant="secondary"
              size="lg"
              className="w-full justify-center gap-3"
              onClick={() => submitOIDC('google')}
            >
              <GoogleBrandIcon />
              Continue with Google
            </Button>
          </div>

          {/* Divider */}
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

          {/* Email/password form */}
          <form onSubmit={submitPassword} className="space-y-3">
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
              placeholder="Password"
              autoComplete="current-password"
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
                'Sign in'
              )}
            </Button>
          </form>

          {/* Footer links */}
          <div className="flex justify-between text-body-sm">
            <Link
              to="/register"
              className="text-primary hover:text-primary/80"
            >
              Create account
            </Link>
            <Link
              to="/recovery"
              className="text-muted-foreground hover:text-foreground"
            >
              Forgot password?
            </Link>
          </div>

          {/* CLI hint */}
          <CodeBlock
            caption="or use the CLI"
            code="loopzero login"
            terminal
            copyable
          />
        </Card.Body>
      </Card>
    </div>
  );
}
