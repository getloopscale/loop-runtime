import { useEffect, useRef, useState } from 'react';
import { useNavigate, useSearchParams, Link } from 'react-router-dom';
import { Github, Loader2 } from 'lucide-react';
import {
  Alert,
  Button,
  Card,
  CodeBlock,
  FormField,
  Input,
} from '@loopzero/ui';
import { GoogleBrandIcon } from '@/components/google-brand-icon';
import { getSession, startLogin } from '@/lib/auth';
import { clearToken } from '@/lib/api';
import { API_URL, AUTH_URL } from '@/lib/config';
import {
  reportEvent,
  viewportBucket,
  type ActivationEvent,
  type AuthFailureReason,
  type AuthMethod,
  type AuthTelemetryPayload,
} from '@/lib/rum';

const KRATOS_URL = AUTH_URL || API_URL;

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
 * safeErrorDetail — OWASP A07: never expose raw Kratos error text to UI.
 *
 * Priority order (first match wins, EC-2 from edge-case review):
 *   1. csrf            → "Your session expired. Try again."
 *   2. locked          → "This account is locked. Reset your password to continue."
 *   3. provider (OIDC) → "Try a different sign-in method."
 *   4. default         → undefined (no description, generic title only)
 *
 * EC-3: tolerates null/empty text without TypeError via (text ?? '').
 */
function safeErrorDetail(
  errors: { text: string | null }[] | undefined,
): string | undefined {
  if (!errors || errors.length === 0) return undefined;
  const haystack = errors
    .map((e) => (e.text ?? '').toLowerCase())
    .join(' ');
  if (haystack.includes('csrf')) return 'Your session expired. Try again.';
  if (haystack.includes('locked'))
    return 'This account is locked. Reset your password to continue.';
  if (haystack.includes('provider')) return 'Try a different sign-in method.';
  return undefined;
}

/**
 * Classify Kratos validation error into a discriminated reason for telemetry.
 * Used by emitAuthTelemetry to populate the `reason` field on failure events.
 */
function classifyKratosErrorReason(
  errors: { text: string | null }[] | undefined,
): AuthFailureReason {
  if (!errors || errors.length === 0) return 'kratos_validation';
  const haystack = errors
    .map((e) => (e.text ?? '').toLowerCase())
    .join(' ');
  if (haystack.includes('csrf')) return 'csrf_expired';
  return 'kratos_validation';
}

export function LoginPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [flow, setFlow] = useState<Flow | null>(null);
  const [loading, setLoading] = useState(true);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [networkError, setNetworkError] = useState(false);
  // Per-attempt correlation context — funnel-stitching invariant.
  // Captured at attempt time; consumed on outcome events to share correlation_id.
  const authCtxRef = useRef<{
    method: AuthMethod;
    correlation_id: string;
    started_at: number;
  } | null>(null);

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
   * emitAuthTelemetry — FAANG-grade observable auth funnel.
   *
   * PII-FREE INVARIANT enforced by spec (`test_login_payload_pii_free`):
   * payload NEVER contains email, password, sessionID, userID, tenantID,
   * or referrer. See lib/rum.ts AuthTelemetryPayload doc block.
   */
  const emitAuthTelemetry = (
    event: Extract<ActivationEvent, `auth.method.${string}`>,
    ctx: { method: AuthMethod; correlation_id: string; started_at: number },
    extras: { reason?: AuthFailureReason } = {},
  ): void => {
    const payload: AuthTelemetryPayload = {
      v: 1,
      method: ctx.method,
      correlation_id: ctx.correlation_id,
      latency_ms:
        event === 'auth.method.attempt'
          ? undefined
          : Math.max(0, Date.now() - ctx.started_at),
      reason: extras.reason,
      viewport_bucket: viewportBucket(),
      timestamp: Date.now(),
    };
    reportEvent(event, payload as unknown as Record<string, unknown>);
  };

  const startAuthAttempt = (method: AuthMethod): {
    method: AuthMethod;
    correlation_id: string;
    started_at: number;
  } => {
    const ctx = {
      method,
      // RFC 4122 v4, browser-native, zero deps.
      correlation_id: crypto.randomUUID(),
      started_at: Date.now(),
    };
    authCtxRef.current = ctx;
    emitAuthTelemetry('auth.method.attempt', ctx);
    return ctx;
  };

  /**
   * CRITICAL EC-4 (v1 login.tsx): preserve vanilla DOM form.submit() —
   * Kratos requires browser-level form POST to follow OIDC redirect.
   * Substituting fetch breaks GitHub/Google login (302 returned but
   * browser never navigates).
   *
   * OIDC ATTEMPT-ONLY: success/failure de OIDC vem do callback handler
   * (provider → Kratos → /login?flow=X com error nodes OU navegação para /).
   * Aqui só emitimos 'attempt'. Outcome telemetry para OIDC é responsabilidade
   * do callback handler (fora deste plano).
   */
  const submitOIDC = (provider: 'github' | 'google') => {
    startAuthAttempt(provider);
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
    setNetworkError(false);

    const ctx = startAuthAttempt('password');

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

      // EC-1 MUST FIX: HTTP 5xx from Kratos doesn't throw at fetch layer.
      // Without this gate, the handler returned silently (same silent-error
      // bug that the visible-network-error work was supposed to fix).
      if (!resp.ok) {
        setNetworkError(true);
        emitAuthTelemetry('auth.method.failure', ctx, { reason: 'kratos_5xx' });
        return;
      }

      const data = await resp.json();

      if (data.redirect_browser_to) {
        // Treat redirect (verification, step-up) as success-in-progress.
        emitAuthTelemetry('auth.method.success', ctx);
        window.location.href = data.redirect_browser_to;
        return;
      }
      if (data.session) {
        emitAuthTelemetry('auth.method.success', ctx);
        navigate('/', { replace: true });
        return;
      }
      if (data.ui) {
        const errors = (data.ui.messages ?? []).filter(
          (m: { type: string }) => m.type === 'error',
        );
        emitAuthTelemetry('auth.method.failure', ctx, {
          reason: classifyKratosErrorReason(errors),
        });
        setFlow(data);
      }
    } catch {
      // EC-1 secondary: covers fetch throw AND resp.json() throw (e.g. HTML body).
      setNetworkError(true);
      emitAuthTelemetry('auth.method.failure', ctx, { reason: 'network' });
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
          {/* Brand — stacked centered: pixel-art mascot logo on top,
              wordmark "LoopZero" below. Logo at 60×60 (50% larger than the
              prior 40×40 to anchor the card visual hierarchy).
              <picture> ships WebP (~2 KB) to modern browsers, falls back to
              PNG (~6 KB) for Safari < 14 / Firefox < 65 / IE. alt="" because
              the wordmark below already provides the accessible name
              (avoids screen-reader duplication). */}
          <div className="flex flex-col items-center gap-2">
            <picture>
              <source srcSet="/logo-128.webp" type="image/webp" />
              <img
                src="/logo-128.png"
                alt=""
                width={60}
                height={60}
                className="size-[60px]"
              />
            </picture>
            <span className="font-display text-title-sm font-bold tracking-tight text-foreground">
              LoopZero
            </span>
          </div>

          {/* T1.4 microcopy: universal welcome, locked-narrative subtitle. */}
          <div className="space-y-1">
            <h1 className="font-display text-title-sm font-semibold text-foreground">
              Sign in to LoopZero
            </h1>
            <p className="text-body-sm text-muted-foreground">
              Deploy, monitor, and operate your workloads.
            </p>
          </div>

          {/* Session expired notice (§3.6) */}
          {searchParams.get('reason') === 'expired' && allErrors.length === 0 && !networkError && (
            <Alert
              intent="warning"
              description="Your session expired. Please sign in again."
              title="Session expired"
            />
          )}

          {/* T1.3: network/HTTP-5xx feedback (EC-1). networkError wins over Kratos errors. */}
          {networkError && (
            <Alert
              intent="warning"
              title="Network error. Check your connection and try again."
            />
          )}

          {/* T1.1: OWASP-aligned generic title + safe-detail allow-list (D2). */}
          {!networkError && allErrors.length > 0 && (
            <Alert
              intent="destructive"
              title="We couldn't sign you in."
              description={safeErrorDetail(allErrors)}
            />
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

          {/* Email/password form — T2.1 FormField + visible Label (D4) */}
          <form onSubmit={submitPassword} className="space-y-3">
            <FormField>
              <FormField.Label>Email</FormField.Label>
              <FormField.Control>
                <Input
                  size="lg"
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  required
                  placeholder="you@company.com"
                  /* T3.1 D6: passkey via autofill, no dedicated button.
                     Browser without WebAuthn ignores the token silently. */
                  autoComplete="email webauthn"
                  /* T1.2 EC-6: accepts mobile keyboard pop as tradeoff (Vercel/Linear-style). */
                  autoFocus
                />
              </FormField.Control>
            </FormField>
            <FormField>
              <FormField.Label>Password</FormField.Label>
              <FormField.Control>
                <Input
                  size="lg"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  required
                  placeholder="••••••••"
                  autoComplete="current-password"
                />
              </FormField.Control>
            </FormField>
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
