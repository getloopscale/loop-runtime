# Authentication & Billing Architecture

> Technical design document for LoopZero's authentication and billing system.
> Last updated: 2026-04-07.

---

## 1. Overview

LoopZero uses a **JWT-only authentication model** across all services. There are no PAT tokens, no session cookies on API endpoints, and no mixed auth methods. Every API call carries a `Authorization: Bearer <jwt>` header.

Two JWT types exist, both validated by the same shared library (`pkg/jwt`):

| JWT Type | Signer | Algorithm | Audience | TTL | Consumer |
|----------|--------|-----------|----------|-----|----------|
| **Hydra RSA JWT** | Ory Hydra | RS256 (JWKS) | Hydra client | 1h | CLI → cloud + engine API |
| **Session HMAC JWT** | Cloud service | HS256 | `loopzero-cloud` | 15min | Dashboard → cloud + engine API |

---

## 2. Authentication Flows

### 2.1 CLI — Device Code Flow (RFC 8628)

```
┌──────────┐     ┌─────────────┐     ┌──────────┐     ┌──────────┐
│  loopzero    │     │ Cloud       │     │  Ory     │     │  Ory     │
│  CLI     │     │ Service     │     │  Hydra   │     │  Kratos  │
└────┬─────┘     └──────┬──────┘     └────┬─────┘     └────┬─────┘
     │                  │                 │                 │
     │ POST /v1/auth/   │                 │                 │
     │ device/authorize  │                 │                 │
     ├─────────────────►│ POST /oauth2/   │                 │
     │                  │ device/auth     │                 │
     │                  ├────────────────►│                 │
     │                  │◄────────────────┤                 │
     │◄─────────────────┤                 │                 │
     │                  │                 │                 │
     │ device_code +    │                 │                 │
     │ user_code +      │                 │                 │
     │ verification_uri │                 │                 │
     │                  │                 │                 │
     │  ┌─── User opens browser, visits verification_uri ──┐
     │  │    Hydra redirects to /auth/device               │
     │  │    Cloud service validates Kratos session ────────┼──► /sessions/whoami
     │  │    Accepts device challenge                       │
     │  │    User sees "CLI authorized!" page               │
     │  └──────────────────────────────────────────────────┘
     │                  │                 │                 │
     │ POST /v1/auth/   │                 │                 │
     │ device/token     │                 │                 │
     │ (polling)        │ POST /oauth2/   │                 │
     ├─────────────────►│ token           │                 │
     │                  ├────────────────►│                 │
     │                  │◄────────────────┤                 │
     │◄─────────────────┤                 │                 │
     │                  │                 │                 │
     │ access_token (RSA JWT)             │                 │
     │ refresh_token                      │                 │
     │ expires_in                         │                 │
     │                  │                 │                 │
     │ Stores in ~/.loopzero/config.json      │                 │
     │ (0600 permissions, atomic write)   │                 │
```

**JWT claims (Hydra RSA):**
```json
{
  "sub": "kratos-identity-uuid",
  "iss": "https://auth.dev.loopzero.example/",
  "aud": [],
  "exp": 1712345678,
  "ext": {
    "email": "user@example.com",
    "name": "User Name",
    "tenant_id": "tn-username"
  }
}
```

The consent handler (`cloud/internal/consent/handler.go`) injects `email`, `name`, and `tenant_id` into the JWT via the `ext` claim. The `tenant_id` is resolved by `EnsureTenant()` which creates the user, tenant, and membership if they don't exist.

### 2.2 Dashboard — BFF Token Exchange

```
┌──────────┐     ┌──────────┐     ┌─────────────┐     ┌──────────┐
│ Dashboard│     │  Ory     │     │ Cloud       │     │  Engine  │
│  (SPA)   │     │  Kratos  │     │ Service     │     │  API     │
└────┬─────┘     └────┬─────┘     └──────┬──────┘     └────┬─────┘
     │                │                  │                  │
     │ GET /sessions/ │                  │                  │
     │ whoami         │                  │                  │
     ├───────────────►│                  │                  │
     │◄───────────────┤                  │                  │
     │ (no session)   │                  │                  │
     │                │                  │                  │
     │ Redirect to /login               │                  │
     │ User authenticates (OIDC or password)                │
     │───────────────►│                  │                  │
     │◄───────────────┤                  │                  │
     │ (session cookie set)              │                  │
     │                │                  │                  │
     │ POST /v1/auth/session/token       │                  │
     │ (sends Kratos session cookie)     │                  │
     ├──────────────────────────────────►│                  │
     │                │                  │                  │
     │                │ GET /sessions/   │                  │
     │                │ whoami           │                  │
     │                │◄─────────────────┤                  │
     │                │─────────────────►│                  │
     │                │ (validates session, returns identity)│
     │                │                  │                  │
     │                │                  │ EnsureTenant()   │
     │                │                  │ (creates user +  │
     │                │                  │  tenant + member │
     │                │                  │  if needed)      │
     │                │                  │                  │
     │◄──────────────────────────────────┤                  │
     │ access_token (HMAC JWT)           │                  │
     │ expires_in: 900 (15min)           │                  │
     │                │                  │                  │
     │ Stores JWT in memory (never localStorage)            │
     │                │                  │                  │
     │ GET /v1/billing                   │                  │
     │ Authorization: Bearer <hmac-jwt>  │                  │
     ├──────────────────────────────────►│                  │
     │◄──────────────────────────────────┤                  │
     │                │                  │                  │
     │ GET /api/v1/projects              │                  │
     │ Authorization: Bearer <hmac-jwt>  │                  │
     ├─────────────────────────────────────────────────────►│
     │◄─────────────────────────────────────────────────────┤
```

**JWT claims (Session HMAC):**
```json
{
  "sub": "kratos-identity-uuid",
  "iss": "loopzero-cloud",
  "aud": ["loopzero-cloud"],
  "exp": 1712345678,
  "nbf": 1712344778,
  "iat": 1712344778,
  "email": "user@example.com",
  "name": "User Name",
  "tenant_id": "tn-username"
}
```

**Token lifecycle:**
- Issued on first page load after login (session → JWT exchange)
- TTL: 15 minutes (short to reduce stale tenant risk)
- Auto-refresh: dashboard calls `/v1/auth/session/token` when token expires
- Concurrent refresh deduplication: single in-flight promise shared by all callers
- Cleared on logout (`clearToken()` + Kratos session destroy)

### 2.3 Token Validation (Both Services)

Both the cloud service and the engine API validate JWTs using the same shared library:

```
pkg/jwt/
  ├── Validator            — RSA JWKS validation (Hydra JWTs)
  ├── SessionTokenIssuer   — HMAC issue + validate (session JWTs)
  ├── Claims               — RSA JWT claims struct
  └── SessionClaims        — HMAC JWT claims struct
```

**Cloud service** (`cloud/internal/auth/middleware.go`):
- `RequireJWT` chi middleware tries RSA then HMAC
- Sets `UserContext` on request context
- Handlers read `auth.GetUser(ctx)`

**Engine API** (`api/internal/middleware/jwt_validator.go`):
- `JWTOnlyValidator` tries RSA then HMAC
- Implements `TokenValidator` interface
- Used by existing `Auth` middleware

---

## 3. Tenant Provisioning

When a user authenticates for the first time, `EnsureTenant()` creates the full chain in one transaction:

```sql
BEGIN;
-- 1. User record (Kratos UUID → users table)
INSERT INTO users (id, email, github_id, display_name, ...)
  VALUES ($kratosID, $email, 0, $slug, ...)
  ON CONFLICT (id) DO UPDATE SET email = EXCLUDED.email;

-- 2. Tenant (derived from email local part)
INSERT INTO tenants (id, display_name, plan, owner_id, ...)
  VALUES ('tn-{slug}', $slug, 'starter', $kratosID, ...)
  ON CONFLICT (id) DO NOTHING;

-- 3. Membership
INSERT INTO tenant_members (tenant_id, user_id, role, ...)
  VALUES ('tn-{slug}', $kratosID, 'owner', ...)
  ON CONFLICT DO NOTHING;
COMMIT;
```

**FK chain:** `tenant_members.user_id → users.id` — user must exist first.

**Called from:**
- Session token exchange (`/v1/auth/session/token`) — dashboard path
- Consent handler (`/auth/consent`) — CLI path (injects tenant_id into Hydra JWT)

---

## 4. Billing Architecture

### 4.1 Plan Tiers

| Plan | Price | Stripe | Projects | Environments | Builds/mo | Seats |
|------|-------|--------|----------|--------------|-----------|-------|
| **Starter** | Free | DB-only (no Stripe) | 2 | 1 | 100 | 1 |
| **Pro** | $19/mo | `price_1TJGIuA0ZwssSKrVEMKZsiMj` | 10 | 3 | 1000 | 5 |
| **Team** | $49/mo | `price_1TJGJ5A0ZwssSKrVy3mjsal6` | ∞ | 10 | ∞ | 20 |
| **Enterprise** | Custom | Custom | ∞ | ∞ | ∞ | ∞ |

### 4.2 Checkout Flow (Starter → Paid)

```
┌──────────┐     ┌─────────────┐     ┌──────────┐
│ Dashboard│     │ Cloud       │     │  Stripe  │
│          │     │ Service     │     │          │
└────┬─────┘     └──────┬──────┘     └────┬─────┘
     │                  │                 │
     │ POST /v1/billing/│                 │
     │ checkout          │                 │
     │ {"plan":"pro"}   │                 │
     ├─────────────────►│                 │
     │                  │                 │
     │                  │ EnsureStripe    │
     │                  │ Customer()      │
     │                  │                 │
     │                  │ POST /v1/       │
     │                  │ customers       │
     │                  ├────────────────►│
     │                  │◄────────────────┤
     │                  │ cus_xxx         │
     │                  │                 │
     │                  │ UPDATE subscriptions
     │                  │ SET stripe_customer_id
     │                  │                 │
     │                  │ POST /v1/       │
     │                  │ checkout/sessions│
     │                  ├────────────────►│
     │                  │◄────────────────┤
     │                  │ session.url     │
     │◄─────────────────┤                 │
     │ {"url":"https://checkout.stripe.com/..."}
     │                  │                 │
     │ window.location.href = url        │
     │─────────────────────────────────► │
     │                  │                 │
     │  ┌─── User enters payment info ──┐│
     │  │    4242 4242 4242 4242        ││
     │  │    Stripe creates subscription ││
     │  └───────────────────────────────┘│
     │                  │                 │
     │                  │ POST /v1/webhooks/stripe
     │                  │ checkout.session.completed
     │                  │◄────────────────┤
     │                  │                 │
     │                  │ UPDATE subscriptions
     │                  │ SET stripe_subscription_id,
     │                  │     plan = 'pro',
     │                  │     status = 'active'
     │                  │ WHERE stripe_customer_id = ?
     │                  │                 │
     │                  │ 200 OK          │
     │                  ├────────────────►│
     │                  │                 │
     │ Redirect to /billing?success=true │
     │◄──────────────────────────────────│
     │                  │                 │
     │ GET /v1/billing  │                 │
     │ → plan: "Pro"    │                 │
```

### 4.3 Webhook Events

| Event | Handler | Action |
|-------|---------|--------|
| `checkout.session.completed` | `handleCheckoutCompleted` | Links `stripe_subscription_id` to DB, sets plan |
| `customer.subscription.updated` | `handleSubscriptionUpdated` | Syncs status, period start/end |
| `customer.subscription.deleted` | `handleSubscriptionDeleted` | Marks subscription canceled |
| `invoice.paid` | `handleInvoicePaid` | Informational log |
| `invoice.payment_failed` | `handlePaymentFailed` | Warning log (Stripe handles retries) |

**Webhook security:**
- HMAC-SHA256 signature verification (`Stripe-Signature` header)
- Timestamp tolerance: ±300 seconds (rejects past AND future)
- Idempotency: `webhook_events` table tracks processed event IDs
- Body size limit: 64KB (`io.LimitReader`)
- No JWT auth (Stripe signs with its own secret)

### 4.4 Plan Management (Existing Subscriber)

Users with an active Stripe subscription use the **Customer Portal** for:
- Upgrade/downgrade (Pro ↔ Team)
- Cancel subscription (at period end)
- Update payment method
- View invoice history

```
Dashboard → GET /v1/billing/portal
  → EnsureStripeCustomer (lazy)
  → POST /v1/billing_portal/sessions (Stripe API)
  → Returns portal URL
  → window.open(url)
```

### 4.5 Billing API Response

```
GET /v1/billing
Authorization: Bearer <jwt>

Response:
{
  "plan": "Pro",
  "team": "tn-username",
  "usage": {
    "projects":             { "used": 3,  "limit": 10 },
    "builds_per_month":     { "used": 42, "limit": 1000 },
    "environments":         { "used": 1,  "limit": 3 },
    "seats":                { "used": 2,  "limit": 5 },
    "secrets_per_env":      { "used": 8,  "limit": 50 },
    "services_per_project": { "used": 4,  "limit": 10 },
    ...
  },
  "period_start": "2026-04-01T00:00:00Z",
  "period_end":   "2026-05-01T00:00:00Z"
}
```

- `plan`: User-facing name from `PlanDisplayName()`
- `usage[metric].limit`: From `plan_limits` table
- `usage[metric].used`: From `usage_records` table (current billing period)
- For unlimited metrics (team/enterprise): `limit >= 999999`, dashboard shows `∞`

---

## 5. Database Schema

### 5.1 Auth Tables

```
users
  id             TEXT PK        -- Kratos identity UUID or legacy github-* ID
  email          TEXT NOT NULL
  github_id      BIGINT
  display_name   TEXT
  created_at     TIMESTAMPTZ
  updated_at     TIMESTAMPTZ

tenants
  id             TEXT PK        -- "tn-{slug}" derived from email
  display_name   TEXT
  plan           TEXT           -- starter/pro/team/enterprise
  owner_id       TEXT → users(id)
  cell_id        TEXT
  created_at     TIMESTAMPTZ
  updated_at     TIMESTAMPTZ

tenant_members
  tenant_id      TEXT → tenants(id)
  user_id        TEXT → users(id)
  role           TEXT           -- owner/admin/member
  created_at     TIMESTAMPTZ
  updated_at     TIMESTAMPTZ

user_identity_map
  user_id            TEXT
  kratos_identity_id TEXT
  created_at         TIMESTAMPTZ
```

### 5.2 Billing Tables

```
subscriptions
  id                     TEXT PK
  tenant_id              TEXT UNIQUE → tenants(id)
  stripe_customer_id     TEXT NULL     -- NULL for free tier
  stripe_subscription_id TEXT NULL     -- NULL until checkout completes
  plan                   TEXT DEFAULT 'starter'
  status                 TEXT DEFAULT 'active'
  current_period_start   TIMESTAMPTZ
  current_period_end     TIMESTAMPTZ
  created_at             TIMESTAMPTZ
  updated_at             TIMESTAMPTZ

plan_limits
  plan        TEXT   -- starter/pro/team/enterprise
  metric      TEXT   -- projects, builds_per_month, environments, etc.
  hard_limit  INT
  soft_limit  INT    -- NULL for hard-only, set for soft limits (team)
  PRIMARY KEY (plan, metric)

usage_records
  id                 TEXT PK
  tenant_id          TEXT → tenants(id)
  metric             TEXT
  quantity           INT DEFAULT 0
  period_start       TIMESTAMPTZ
  period_end         TIMESTAMPTZ
  reported_to_stripe BOOL DEFAULT false
  created_at         TIMESTAMPTZ
  UNIQUE (tenant_id, metric, period_start)

webhook_events
  event_id     TEXT PK    -- Stripe event ID (idempotency)
  event_type   TEXT
  processed_at TIMESTAMPTZ
```

---

## 6. Infrastructure

### 6.1 Routing (Traefik IngressRoute)

All traffic enters via `cloud.dev.loopzero.example`:

| Path | Service | Priority | Auth |
|------|---------|----------|------|
| `/oauth2/*`, `/.well-known/*` | Hydra (4444) | 300 | None |
| `/self-service/*`, `/sessions/*` | Kratos (80) | 200 | Cookies |
| `/v1/auth/*`, `/v1/billing/*`, `/v1/webhooks/*`, `/auth/*` | Cloud service (13002) | 100 | JWT (except auth flow + webhooks) |
| `/*` (fallback) | Dashboard nginx (80) | 1 | None (SPA) |

Engine API at `api.dev.loopzero.example`:

| Path | Service | Auth |
|------|---------|------|
| `/api/v1/*` | Engine API (8080) | JWT |

### 6.2 Secrets

| Secret | Location | Rotation |
|--------|----------|----------|
| `SESSION_TOKEN_SECRET` | K8s Secret (`loopzero-cloud-secrets` + `loopzero-api-secrets`) | Manual restart |
| `STRIPE_API_KEY` | K8s Secret (`loopzero-cloud-secrets`) | Stripe dashboard |
| `STRIPE_WEBHOOK_SECRET` | K8s Secret (`loopzero-cloud-secrets`) | Stripe dashboard |
| `DATABASE_URL` | K8s Secret (both services) | Pulumi managed |

All secrets injected via `envFrom` in Kubernetes deployments. Never hardcoded. Dev mode generates random `SESSION_TOKEN_SECRET` per startup.

### 6.3 Background Jobs

| Job | Interval | Purpose |
|-----|----------|---------|
| Usage reporter | 1 hour | Sends unreported usage to Stripe |
| Drift reconciler | 168 hours (weekly) | Compares recorded vs actual build counts |

Both use `time.Ticker` with `context.Context` for graceful shutdown. No cron library.

---

## 7. Security Decisions

| Decision | Rationale |
|----------|-----------|
| JWT in memory, never localStorage | Protects against XSS reading tokens from storage APIs |
| 15-minute session JWT TTL | Limits blast radius of stolen tokens, forces frequent re-validation |
| HMAC-SHA256 with 64-byte secret | Minimum 32 bytes required in production, validated at startup |
| Webhook timestamp ±300s | Stripe recommendation; rejects both old (replay) and future (forge) events |
| `Idempotency-Key` on all Stripe mutations | Prevents duplicate customers/subscriptions on crash/retry |
| Rate limit on token exchange (10/min/IP) | Prevents brute-force session hijacking |
| Free tier = DB-only, no Stripe | Reduces API surface, no payment data for free users |
| `EnsureTenant` never returns "default" | Fails loud with 500 instead of issuing JWT with wrong tenant |
| Single JWT library (`pkg/jwt`) | One implementation to patch, no divergence risk |

---

## 8. Error Response Schema

All API endpoints return errors in the same format:

```json
{
  "code": "ERROR_CODE",
  "message": "User-facing message."
}
```

Error codes follow the pattern `DOMAIN_ERROR`:
- `AUTH_REQUIRED`, `AUTH_FAILED`, `AUTH_SERVICE_UNAVAILABLE`
- `BILLING_ERROR`, `BILLING_UNAVAILABLE`, `CHECKOUT_ERROR`
- `INVALID_REQUEST`, `INVALID_PLAN`, `INVALID_SIGNATURE`, `INVALID_PAYLOAD`
- `RATE_LIMITED`, `TENANT_ERROR`, `TOKEN_ERROR`, `PROCESSING_ERROR`

No internal terms (Stripe, Hydra, Kratos, webhook) in user-facing messages.

---

## 9. File Map

```
pkg/jwt/
  validator.go              ← Validator (RSA), SessionTokenIssuer (HMAC), Claims, SessionClaims

cloud/
  cmd/main.go               ← Service wiring, middleware, routes
  internal/
    auth/
      middleware.go          ← RequireJWT chi middleware
      tenant.go              ← EnsureTenant (user + tenant + membership provisioning)
    billing/
      stripe.go              ← StripeClient (customer, checkout, portal, usage)
      webhook.go             ← WebhookHandler (signature verification, event processing)
      provisioner.go         ← ProvisionStarter, EnsureStripeCustomer
      plans.go               ← PlanConfig, PlanDisplayName
    consent/
      handler.go             ← Hydra login/consent/device callbacks
    routes/
      billing.go             ← GET /v1/billing, GET /v1/billing/portal, POST /v1/billing/checkout
      session_token.go       ← POST /v1/auth/session/token (BFF exchange)
      device.go              ← Device Code Flow endpoints
      me.go                  ← GET /v1/auth/me

  dashboard/src/
    lib/api.ts               ← cloudFetch, engineFetch, token exchange, in-memory JWT
    lib/auth.ts              ← Kratos session check, isEmailVerified, logout
    hooks/use-auth.ts        ← useRequireAuth (session check + token exchange)
    pages/billing.tsx         ← Billing page (plan, usage, upgrade, portal)
    pages/login.tsx           ← Login page (OIDC + password)

api/
  cmd/main.go                ← JWTOnlyValidator wiring
  internal/middleware/
    auth.go                  ← Auth middleware (token validation + tenant resolution)
    jwt_validator.go         ← JWTOnlyValidator (RSA + HMAC, uses pkg/jwt)
```
