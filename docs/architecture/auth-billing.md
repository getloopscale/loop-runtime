# Authentication & Billing Architecture

> Technical reference for LoopZero's authentication, authorization, and billing systems.
> Every claim is verified against the codebase. Last updated: 2026-04-08.

---

## 1. Routing Architecture

LoopZero uses two browser-facing hosts and one OAuth2 host, all fronted by Traefik IngressRoutes.

```
                 ┌─────────────────────────────────────────────────────────┐
                 │               Traefik (mgmt cluster)                    │
                 └───────┬──────────────────┬──────────────┬───────────────┘
                         │                  │              │
              cloud.{domain}         api.{domain}    auth.{domain}
              (browser-facing)       (CLI-facing)    (OAuth2 server)
                         │                  │              │
          ┌──────────────┴────────┐   ┌─────┴─────┐   ┌───┴───┐
          │  P300: Hydra          │   │ P100:     │   │ Hydra │
          │  P200: Kratos         │   │ Cloud svc │   │ public│
          │  P100: Cloud service  │   │ P1:       │   └───────┘
          │  P1:   Dashboard SPA  │   │ Engine API│
          └───────────────────────┘   └───────────┘
```

### cloud.{domain} — Browser Gateway

Everything the dashboard and browser auth flows need, on one origin. No CORS.

| Priority | Path | Service | Purpose |
|----------|------|---------|---------|
| 300 | `/oauth2/*`, `/.well-known/*` | Hydra (4444) | JWKS, OAuth2 endpoints |
| 200 | `/self-service/*`, `/sessions/*`, `/schemas/*` | Kratos (80) | Login, signup, recovery, session |
| 100 | `/v1/auth/*`, `/v1/billing/*`, `/v1/webhooks/*`, `/auth/*`, `/engine/*`, `/metrics`, `/health` | Cloud (13002) | Auth, billing, engine proxy |
| 1 | `/*` | Dashboard (80) | SPA fallback (React) |

### api.{domain} — CLI Gateway

The CLI sends all requests here. Traefik routes auth paths to cloud service, everything else to the engine.

| Priority | Path | Service | Purpose |
|----------|------|---------|---------|
| 100 | `/v1/auth/*`, `/v1/billing/*`, `/v1/webhooks/*`, `/auth/*`, `/health` | Cloud (13002) | CLI login, billing |
| 1 | `/*` | Engine (8080) | Builds, deploys, projects |

### auth.{domain} — OAuth2 Server

Hydra's public endpoint. Used as the OAuth2 issuer URL (`iss` claim in JWTs). Accessed by Hydra Device Code Flow for verification.

---

## 2. Token Types

Two JWT types exist. Both validated by `pkg/jwt/validator.go`.

### RSA JWT (CLI)

| Field | Value |
|-------|-------|
| Signer | Ory Hydra |
| Algorithm | RS256 |
| Audience | `["loopzero-cloud", "loopzero-engine"]` |
| Issuer | `https://auth.{domain}/` |
| TTL | 1 hour |
| Consumers | CLI → cloud service, CLI → engine API |
| Rotation | Automatic via JWKS (5-min cache) |

```json
{
  "sub": "kratos-uuid",
  "iss": "https://auth.dev.loopzero.example/",
  "aud": ["loopzero-cloud", "loopzero-engine"],
  "exp": 1712345678,
  "ext": {
    "email": "user@example.com",
    "name": "User Name",
    "tenant_id": "tn-a1b2c3d4e5f67890"
  }
}
```

Hydra places consent session claims in `ext`. The cloud middleware reads `TenantID` from both top-level and `Ext.TenantID` (fallback).

### HMAC Session JWT (Dashboard)

| Field | Value |
|-------|-------|
| Signer | Cloud service |
| Algorithm | HS256 |
| Audience | `loopzero-cloud` |
| Issuer | `loopzero-cloud` |
| TTL | 15 minutes |
| Consumers | Dashboard → cloud service only |
| Rotation | `kid` header + dual-key (`SESSION_TOKEN_SECRET` + `SESSION_TOKEN_SECRET_PREVIOUS`) |

```json
{
  "sub": "kratos-uuid",
  "iss": "loopzero-cloud",
  "aud": ["loopzero-cloud"],
  "exp": 1712345678,
  "email": "user@example.com",
  "name": "User Name",
  "tenant_id": "tn-a1b2c3d4e5f67890"
}
```

### Token Acceptance by Service

| Service | RSA (Hydra) | HMAC (Session) | X-Service-Auth |
|---------|-------------|----------------|----------------|
| Cloud | `aud: loopzero-cloud` | `aud: loopzero-cloud` | N/A |
| Engine | `aud: loopzero-engine` | Rejected | Accepted (proxy) |

---

## 3. Authentication Flows

### 3.1 CLI — Device Code Flow (RFC 8628)

```
CLI                    Cloud Service           Hydra              Kratos
 │                         │                     │                   │
 │ POST /v1/auth/          │                     │                   │
 │ device/authorize        │ POST /oauth2/       │                   │
 ├────────────────────────►│ device/auth         │                   │
 │                         ├────────────────────►│                   │
 │◄────────────────────────┤                     │                   │
 │ device_code, user_code  │                     │                   │
 │ verification_uri        │                     │                   │
 │                         │                     │                   │
 │ [User opens browser, visits verification_uri]  │                   │
 │ [Hydra redirects to cloud /auth/device]        │                   │
 │ [Cloud validates Kratos session]               │                   │
 │ [Consent handler: EnsureTenant + AcceptConsent] │                   │
 │ [aud: ["loopzero-cloud","loopzero-engine"] granted]     │                   │
 │                         │                     │                   │
 │ POST /v1/auth/          │ POST /oauth2/token  │                   │
 │ device/token (polling)  ├────────────────────►│                   │
 │◄────────────────────────┤◄────────────────────┤                   │
 │ access_token (RSA)      │                     │                   │
 │ refresh_token           │                     │                   │
 │                         │                     │                   │
 │ Saved: ~/.loopzero/config.json (0600, atomic write)                    │
```

**RFC 8628 compliance:**
- `slow_down` → interval increases by 5 seconds
- `expired_token` → polling stops with message
- Jitter: +/- 20% on interval
- Max 60 attempts + 5-minute timeout

**Post-login CLI output:**
```
  Authenticated as paulo@example.com
  Team: paulo (tn-a1b2c3d4e5f67890)
  Token expires: in 60 minutes

  Next: run `loopzero deploy` to deploy your project
```

### 3.2 Dashboard — BFF Token Exchange

```
Dashboard (SPA)         Kratos              Cloud Service
 │                        │                      │
 │ GET /sessions/whoami   │                      │
 ├───────────────────────►│                      │
 │◄───────────────────────┤ (session active)     │
 │                        │                      │
 │ POST /v1/auth/session/token                   │
 │ (sends Kratos cookie)  │                      │
 ├──────────────────────────────────────────────►│
 │                        │ GET /sessions/whoami │
 │                        │◄─────────────────────┤
 │                        │─────────────────────►│
 │                        │                      │ EnsureTenant()
 │◄──────────────────────────────────────────────┤
 │ { access_token (HMAC), expires_in: 900 }      │
 │                        │                      │
 │ Token in JS memory (never localStorage)       │
 │ Auto-refresh 60s before expiry                │
 │ Cross-tab sync via BroadcastChannel           │
```

**Dashboard → engine calls** go through the cloud proxy at `/engine/*`:
```
Dashboard → cloudFetch('/engine/api/v1/projects')
         → Cloud validates HMAC JWT
         → Proxy: X-Service-Auth + X-LoopZero-User-ID + X-LoopZero-Tenant
         → Engine validates service auth, resolves role from DB
         → Response piped back
```

### 3.3 Secret Rotation (Zero-Downtime)

**HMAC session tokens:**
```
Phase 1: Old secret active
  Issue tokens with kid=abc12345

Phase 2: Deploy new secret
  SESSION_TOKEN_SECRET=new
  SESSION_TOKEN_SECRET_PREVIOUS=old
  → Issues with kid=def67890
  → Validates both kids

Phase 3: After 2x TTL (30 min)
  Remove SESSION_TOKEN_SECRET_PREVIOUS
  → Only validates new kid
```

**Service auth (cloud→engine):** Same pattern with `LOOPZERO_SERVICE_AUTH_SECRET` + `LOOPZERO_SERVICE_AUTH_SECRET_PREVIOUS`.

---

## 4. Tenant Provisioning

Called from two paths: Hydra consent handler (CLI) and session token exchange (dashboard).

```
EnsureTenant(ctx, userID, email)
  │
  ├─ Fast path: SELECT membership WHERE user_id = ?
  │  └─ Found → return (tenantID, role)
  │
  ├─ Slow path:
  │  ├─ BEGIN TX
  │  ├─ pg_advisory_xact_lock(hashtext(userID))
  │  ├─ Re-check membership inside lock
  │  │  └─ Found → return (concurrent request won)
  │  ├─ Generate: tn-{crypto/rand 16 hex}
  │  ├─ INSERT users (ON CONFLICT → UPDATE email)
  │  ├─ INSERT tenants (id, slug, owner_id)
  │  │  └─ Slug collision → append UUID suffix, retry
  │  ├─ INSERT tenant_members (owner)
  │  ├─ INSERT subscriptions (plan='starter', status='active')
  │  ├─ COMMIT
  │  └─ return (tenantID, "owner")
```

**Key properties:**
- Tenant ID: `tn-{hex16}` — UUID-based, collision-free
- Advisory lock prevents duplicate tenant creation for concurrent logins
- Slug: human-readable, derived from email, for display only
- Non-fatal in consent: if provisioning fails, JWT issued without `tenant_id`. Middleware returns 403.

---

## 5. Authorization

### Role Hierarchy

```
owner (3) → all operations
admin (2) → deploy, secrets, rollback (no billing, no member management)
member (1) → deploy, view logs, view status (no delete, no secrets)
```

`RequireRole(minRole)` middleware enforces at route level.

### Tenant Isolation

- Every request carries `tenant_id` (from JWT or membership resolution)
- Engine resolves role from `tenant_members` DB table on every request — never trusts headers
- Multi-tenant users must specify `X-LoopZero-Tenant` header or get 409

---

## 6. Billing

### Plan Tiers

| Plan | Price | Stripe | Projects | Builds/mo | Environments |
|------|-------|--------|----------|-----------|--------------|
| Starter | Free | DB-only | 2 | 100 | 1 |
| Pro | $19/mo | `STRIPE_PRICE_PRO` | 10 | 1000 | 3 |
| Team | $49/mo | `STRIPE_PRICE_TEAM` | unlimited | unlimited | 10 |
| Enterprise | Custom | Custom | unlimited | unlimited | unlimited |

**Single source of truth:** `subscriptions` table. The `tenants` table does not store plan.

### Checkout Flow

```
Dashboard → POST /v1/billing/checkout {plan:"pro"}
         → EnsureStripeCustomer (lazy, idempotent)
         → CreateCheckoutSession (idem-key: tenant+plan+hour)
         → {url: "https://checkout.stripe.com/..."}
         → Browser redirect to Stripe
         → Payment → Stripe webhook → UPDATE subscriptions
         → Dashboard polls /v1/billing until plan changes
```

### Webhook Events

| Event | Action |
|-------|--------|
| `checkout.session.completed` | Link subscription ID, set plan |
| `customer.subscription.updated` | Sync status + plan from price ID |
| `customer.subscription.deleted` | Mark canceled |
| `invoice.paid` | Informational log |
| `invoice.payment_failed` | Warning log (Stripe handles retries) |

**Idempotency:** `webhook_events` table with `status`, `attempt_count`, `last_error`, `payload_hash`. Only `status='processed'` events are skipped on retry.

### Enforcement

Plan resolved from `subscriptions` table. Enterprise always allowed. Team has soft limits (overage tracking). Starter/Pro blocked at hard limit. `NULL` = unlimited.

### Metering

| Job | Interval | Action |
|-----|----------|--------|
| Reporter | 1 hour | Send unreported usage to Stripe |
| Reconciler | 24 hours | Compare recorded vs actual, auto-correct < 5%, alert >= 5% |

---

## 7. Observability

### Metrics (cloud service `/metrics`)

| Metric | Type | Purpose |
|--------|------|---------|
| `loopzero_auth_token_exchanges_total{result}` | counter | Session token exchange success/failure |
| `loopzero_auth_device_flow_total{result}` | counter | Device flow completions |
| `loopzero_tenant_provisioning_total{result}` | counter | EnsureTenant success/failure |
| `loopzero_auth_rejections_total{reason}` | counter | 401/403 by reason |
| `loopzero_webhook_events_total{status}` | counter | Webhook processed/failed |
| `loopzero_billing_checkout_total{result}` | counter | Checkout started/error |
| `loopzero_subscription_fallback_total` | counter | Missing subscription → starter default |

### Alerts

| Alert | Severity | Condition |
|-------|----------|-----------|
| `LoopZeroTraefikDown` | critical | Traefik unreachable for 1 min |
| `LoopZeroTraefikHighErrorRate` | warning | 5xx rate > 5% for 5 min |

### Error Responses

All errors include `request_id` for tracing:
```json
{"code": "AUTH_REQUIRED", "message": "Not authenticated. Run: loopzero login", "request_id": "req-xxx"}
```

---

## 8. Security Headers

**CSP** (Traefik middleware):
```
default-src 'self'; script-src 'self' https://js.stripe.com;
style-src 'self' 'unsafe-inline'; connect-src 'self' https://api.stripe.com https://checkout.stripe.com;
frame-src https://checkout.stripe.com https://js.stripe.com;
base-uri 'self'; form-action 'self';
```

**Other:** `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `HSTS: 1 year`.

---

## 9. Database Schema

### Auth Tables

```
users
  id             TEXT PK         -- Kratos UUID
  email          TEXT NOT NULL UNIQUE
  github_id      BIGINT NULL     -- Legacy, nullable
  display_name   TEXT

tenants
  id             TEXT PK         -- tn-{hex16} (UUID-based)
  display_name   TEXT
  slug           TEXT UNIQUE     -- Human-readable
  owner_id       TEXT FK → users

tenant_members
  (tenant_id, user_id) PK
  role           TEXT CHECK (owner/admin/member)
```

### Billing Tables

```
subscriptions                    -- Single source of truth for plan
  id             TEXT PK
  tenant_id      TEXT UNIQUE FK → tenants
  stripe_customer_id     TEXT NULL
  stripe_subscription_id TEXT NULL
  plan           TEXT CHECK (starter/pro/team/enterprise)
  status         TEXT CHECK (active/past_due/canceled/incomplete/trialing/unpaid)

plan_limits
  (plan, metric) PK
  hard_limit     INT NULL        -- NULL = unlimited
  soft_limit     INT NULL        -- Team plan only

usage_records
  (tenant_id, metric, period_start) UNIQUE
  quantity       INT
  reported_to_stripe BOOL

webhook_events
  event_id       TEXT PK
  event_type     TEXT
  status         TEXT             -- processed/failed
  attempt_count  INT
  last_error     TEXT
  payload_hash   TEXT
```

---

## 10. Configuration

### Cloud Service

| Variable | Default | Purpose |
|----------|---------|---------|
| `SESSION_TOKEN_SECRET` | auto-generated (dev) | HMAC signing key (>= 32 chars in prod) |
| `SESSION_TOKEN_SECRET_PREVIOUS` | empty | Previous key for rotation |
| `LOOPZERO_SERVICE_AUTH_SECRET` | auto-generated (dev) | Cloud→engine proxy auth |
| `KRATOS_PUBLIC_URL` | `https://cloud.{domain}` | Browser-facing Kratos |
| `HYDRA_PUBLIC_URL` | `https://auth.{domain}` | OAuth2 server |
| `BILLING_RETURN_URL` | `https://cloud.{domain}` | Post-checkout redirect |
| `STRIPE_API_KEY` | empty | Stripe API |
| `STRIPE_WEBHOOK_SECRET` | empty | Webhook signature |

### Engine API

| Variable | Default | Purpose |
|----------|---------|---------|
| `JWKS_URL` | `https://auth.{domain}/.well-known/jwks.json` | RSA key fetch |
| `JWT_ISSUER` | `https://auth.{domain}/` | Expected JWT issuer |
| `LOOPZERO_SERVICE_AUTH_SECRET` | empty | Accept cloud proxy auth |
| `LOOPZERO_SERVICE_AUTH_SECRET_PREVIOUS` | empty | Previous key for rotation |

### CLI

| Variable | Default | Purpose |
|----------|---------|---------|
| `LOOPZERO_URL` | `http://localhost:8080` | Single URL for all operations |
| `LOOPZERO_API_URL` | (deprecated fallback) | Legacy, accepted with warning |
| `LOOPZERO_CLOUD_URL` | (deprecated fallback) | Legacy, returns same as LOOPZERO_URL |

---

## 11. File Map

```
pkg/jwt/
  validator.go              ← RSA Validator + HMAC SessionTokenIssuer (kid, dual-key)

cloud/
  cmd/main.go               ← Wiring: CORS removed, engine proxy, metrics endpoint
  internal/
    auth/
      middleware.go          ← RequireJWT (RSA+HMAC, ext.TenantID fallback)
      tenant.go              ← EnsureTenant (UUID, advisory lock, subscription)
      hydra.go               ← HydraClient (device flow, consent with audience)
      roles.go               ← RequireRole (owner > admin > member)
      migration.go           ← IdentityMigrator (DEPRECATED, removal Q3 2026)
    billing/
      stripe.go              ← StripeClient (idempotency keys)
      webhook.go             ← ProcessEvent (status tracking, retry-safe)
      provisioner.go         ← ProvisionStarter, EnsureStripeCustomer
      plans.go               ← PlanConfig, PlanForPriceID (reverse lookup)
    enforcement/
      limiter.go             ← CheckLimit (NULL=unlimited, subscription fallback metric)
    metering/
      tracker.go             ← RecordBuildCompletion, RecordStorageSnapshot
      reconciler.go          ← ReconcileBuilds (daily, auto-correct <5%)
      scheduler.go           ← 1h reporter, 24h reconciler
    ratelimit/
      limiter.go             ← Limiter interface, InMemoryLimiter
    metrics/
      metrics.go             ← Auth + billing Prometheus counters
    consent/
      handler.go             ← Hydra login/consent/device (non-fatal EnsureTenant)
    routes/
      billing.go             ← GET /v1/billing, POST /checkout, GET /portal
      session_token.go       ← POST /v1/auth/session/token (rate limited)

  dashboard/src/
    lib/api.ts               ← cloudFetch, engineFetch (proxy), BroadcastChannel
    lib/auth.ts              ← Kratos session, logout
    lib/kratos.ts            ← Self-service flow helpers

api/
  cmd/main.go                ← RSA-only, ServiceAuthSecret
  internal/middleware/
    auth.go                  ← Auth (Bearer + X-Service-Auth, role from DB)
    jwt_validator.go         ← JWTOnlyValidator (RSA + HMAC)
    roles.go                 ← RequireRole

cli/src/
  config/constants.ts        ← LOOPZERO_URL (single), legacy fallbacks
  auth/device-flow.ts        ← RFC 8628 (jitter, slow_down, max retries)
  auth/token-refresh.ts      ← Refresh token flow
  config/store.ts            ← ~/.loopzero/config.json (0600, atomic)
```
