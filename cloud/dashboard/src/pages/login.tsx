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
import { LoginErrorBanner } from '@/components/login/login-error-banner';
import { CookieBlockedAlert } from '@/components/login/cookie-blocked-alert';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { getSession, startLogin } from '@/lib/auth';
import { clearToken } from '@/lib/api';
import { API_URL, AUTH_URL } from '@/lib/config';
import { safeErrorDetail, isEnumerationCollision } from '@/lib/auth/kratos-error-detail';
import { mapKratosError } from '@/lib/auth/error-mapper';
import { isSafeRedirectUrl } from '@/lib/auth/safe-redirect';
import type { Flow } from '@/lib/auth/kratos-flow';
import {
  reportEvent,
  viewportBucket,
  type ActivationEvent,
  type AuthFailureReason,
  type AuthMethod,
  type AuthTelemetryPayload,
} from '@/lib/rum';

export { classifyKratosErrorReason } from '@/lib/auth/kratos-error-detail';

const KRATOS_URL = AUTH_URL || API_URL;

export function LoginPage() {
  useSetPageTitle('Login');
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
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

  // T0.1 (J-04 plan): `?provider=github|google` autofocuses the matching OIDC
  // button when the user arrives via recovery educational link (J-04 D4).
  // Refs + post-mount focus() instead of autoFocus prop to avoid conflict with
  // the email Input's existing autoFocus (last DOM autoFocus wins by default).
  const providerParam = searchParams.get('provider')?.toLowerCase();
  const githubButtonRef = useRef<HTMLButtonElement | null>(null);
  const googleButtonRef = useRef<HTMLButtonElement | null>(null);
  const emailFocusEnabled = providerParam !== 'github' && providerParam !== 'google';

  useEffect(() => {
    clearToken();
  }, []);

  useEffect(() => {
    if (loading) return;
    if (providerParam === 'github' && githubButtonRef.current) {
      githubButtonRef.current.focus();
    } else if (providerParam === 'google' && googleButtonRef.current) {
      googleButtonRef.current.focus();
    }
  }, [loading, providerParam]);

  useEffect(() => {
    getSession().then((s) => {
      if (s) navigate('/', { replace: true });
      else setLoading(false);
    });
  }, [navigate]);

  useEffect(() => {
    if (loading) return;

    // T6.1 of plan 2026-06-08-pre-release-test-rigor — defense-in-depth flow
    // hydration. Kratos rejects flows that are stale (TTL expired) OR whose
    // CSRF cookie no longer matches (rotated config, cleared cookies, new
    // browser session). When this happens, returning `null` from the fetch
    // chain left the UI in a phantom state with no way to recover. Canonical
    // FAANG fix: detect stale-flow responses (403 / 404 / 410 / 422 OR JSON
    // without ui.action) and silently fall back to /self-service/login/browser
    // which issues a fresh flow ID + new CSRF cookie. The stale `?flow=` in
    // the URL is replaced so refreshes don't re-trigger the broken path.
    const flowId = searchParams.get('flow');

    const hydrateFresh = async () => {
      const res = await fetch(`${KRATOS_URL}/self-service/login/browser`, {
        credentials: 'include',
        headers: { Accept: 'application/json' },
        redirect: 'follow',
      });
      if (!res.ok) return null;
      const body = (await res.json().catch(() => null)) as Flow | null;
      if (body?.ui?.action && body.id) {
        // Mirror the fresh flow id into the URL so reload doesn't re-hit the
        // stale id. `replace: true` keeps history clean.
        setSearchParams({ flow: body.id }, { replace: true });
        return body;
      }
      return null;
    };

    const hydrate = async () => {
      if (flowId) {
        const res = await fetch(
          `${KRATOS_URL}/self-service/login/flows?id=${flowId}`,
          {
            credentials: 'include',
            headers: { Accept: 'application/json' },
          },
        );
        // 403 CSRF / 404 not found / 410 expired / 422 OIDC validation —
        // all signal "stale; rebuild from scratch".
        if (res.ok) {
          const body = (await res.json().catch(() => null)) as Flow | null;
          if (body?.ui?.action) {
            setFlow(body);
            return;
          }
        }
        const fresh = await hydrateFresh();
        if (fresh) setFlow(fresh);
        return;
      }
      const fresh = await hydrateFresh();
      if (fresh) setFlow(fresh);
    };

    hydrate().catch(() => {
      // Last-resort fallback: try once more without flow param.
      hydrateFresh()
        .then((fresh) => {
          if (fresh) setFlow(fresh);
        })
        .catch(() => {});
    });
  }, [searchParams, loading, setSearchParams]);

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

  // SECURITY: submit password login via top-level HTML form POST instead of
  // XHR fetch — same pattern as submitOIDC above. Top-level navigation sends
  // SameSite=Lax cookies (including the anti-CSRF cookie Kratos requires).
  // Cross-origin XHR drops Lax cookies even between same-site subdomains
  // (app.loopzero.example → auth.loopzero.example), causing 403 CSRF rejection.
  // Fix 2026-06-01 — "configuração mais segura possível" mandate; preferred
  // over weakening SameSite to None (which would expose the flow to CSRF from
  // third-party sites).
  //
  // Telemetry / error UX change: outcome events now emit from the LANDING
  // page after navigation (root '/' for success, '/login?flow=X' for failure
  // — the page re-fetches the flow and renders error nodes; classification
  // moves from inline to flow.ui.messages on subsequent mount). Kept attempt
  // event so the funnel still shows method+latency.
  const submitPassword = (e: React.FormEvent) => {
    e.preventDefault();
    if (!flow) return;
    setSubmitting(true);
    setNetworkError(false);

    startAuthAttempt('password');

    const csrfNode = flow.ui.nodes.find(
      (n) => n.attributes.name === 'csrf_token',
    );

    try {
      const form = document.createElement('form');
      form.method = 'POST';
      form.action = flow.ui.action;
      for (const [k, v] of [
        ['csrf_token', csrfNode?.attributes.value || ''],
        ['method', 'password'],
        ['identifier', email],
        ['password', password],
      ]) {
        const input = document.createElement('input');
        input.type = 'hidden';
        input.name = k;
        input.value = v;
        form.appendChild(input);
      }
      document.body.appendChild(form);
      form.submit();
      return;
    } catch {
      // form.submit() doesn't throw; this branch covers DOM/CSP edge cases.
      setNetworkError(true);
    }
    // setSubmitting stays true: form.submit() initiates navigation;
    // the next render happens on the landing page.
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

          {/* CookieBlockedAlert (T2.2 + EC-8 absorption): renders when
              navigator.cookieEnabled === false OR post-OIDC session.whoami
              returns 401 (Safari ITP / Firefox strict mode third-party
              cookie drop). */}
          <CookieBlockedAlert />

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

          {/* T3.2 (EC-J01-1 collision): canonical LoginErrorBanner via
              mapKratosError when the error set indicates a pre-seeded
              identity collision; otherwise pre-existing Alert taxonomy. */}
          {!networkError && allErrors.length > 0 && isEnumerationCollision(allErrors) && (
            <LoginErrorBanner error={mapKratosError({ error: { id: 'account_enumeration_mitigation' } })} />
          )}
          {!networkError && allErrors.length > 0 && !isEnumerationCollision(allErrors) && (
            <Alert intent="destructive" title="We couldn't sign you in." description={safeErrorDetail(allErrors)} />
          )}

          {/* Social login — T0.1 J-04: `?provider=` autofocus + visual ring */}
          <div className="space-y-3">
            <Button
              ref={githubButtonRef}
              variant="secondary"
              size="lg"
              className={`w-full justify-center gap-3${providerParam === 'github' ? ' ring-2 ring-primary ring-offset-2' : ''}`}
              onClick={() => submitOIDC('github')}
            >
              <Github className="size-4" aria-hidden="true" />
              Continue with GitHub
            </Button>

            <Button
              ref={googleButtonRef}
              variant="secondary"
              size="lg"
              className={`w-full justify-center gap-3${providerParam === 'google' ? ' ring-2 ring-primary ring-offset-2' : ''}`}
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
                  /* T1.2 EC-6: accepts mobile keyboard pop as tradeoff (Vercel/Linear-style).
                     T0.1 J-04: disabled when ?provider=github|google so OIDC button receives focus instead. */
                  autoFocus={emailFocusEnabled}
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
