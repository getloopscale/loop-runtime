# Authentication & Billing Architecture

> Technical reference for LoopZero's authentication, authorization, and billing systems.
> This document describes the **implemented** architecture — every claim is backed by code.
> Last updated: 2026-04-07. Source: codebase scan + MEET-2026-04-07-039.

---

## 1. Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────────┐
│  BROWSER                                                                │
│                                                                         │
│  ┌──────────┐    Kratos cookies     ┌──────────┐    Hydra OIDC        │
│  │Dashboard │───────────────────────│  Ory     │◄───────────────────┐  │
│  │  (SPA)   │    session/whoami     │ Kratos   │   GitHub/Google    │  │
│  └────┬─────┘                       └──────────┘                    │  │
│       │ POST /v1/auth/session/token                                 │  │
│       │ (BFF exchange: cookie → JWT)                                │  │
│       ▼                                                              │  │
│  ┌──────────────────────────────────────────────┐                   │  │
│  │  Cloud Service  (cloud/)                      │                   │  │
│  │  - Auth middleware (RequireJWT)                │                   │  │
│  │  - Session token issuer (HMAC HS256)          │                   │  │
│  │  - Hydra consent handler                      │                   │  │
│  │  - Billing routes                             │                   │  │
│  │  - Engine proxy (/engine/*)                   │                   │  │
│  └──────────┬───────────────────────┬────────────┘                   │  │
│             │ X-Service-Auth        │                                │  │
│             │ (shared secret)       │ Stripe API                    │  │
│             ▼                       ▼                                │  │
│  ┌──────────────────┐    ┌──────────────────┐                       │  │
│  │  Engine API       │    │  Stripe          │                       │  │
│  │  (api/)           │    │  (checkout,      │                       │  │
│  │  - RSA-only auth  │    │   webhooks,      │                       │  │
│  │  - Builds/deploy  │    │   portal)        │                       │  │
│  │  - Service auth   │    └──────────────────┘                       │  │
│  └──────────────────┘                                                │  │
│                                                                       │  │
├───────────────────────────────────────────────────────────────────────┤  │
│  CLI                                                                  │  │
│  ┌──────────┐    Device Code Flow (RFC 8628)    ┌──────────┐        │  │
│  │ loopzero     │──────────────────────────────────►│  Ory     │────────┘  │
│  │ CLI      │    access_token (RSA RS256)        │  Hydra   │           │
│  │          │◄──────────────────────────────────│          │           │
│  └──────────┘    aud: [loopzero-cloud, loopzero-engine] └──────────┘           │
│       │                                                                │
│       │ Authorization: Bearer <RSA JWT>                                │
│       ├──► Cloud Service (/v1/auth/me, /v1/billing)                   │
│       └──► Engine API (/api/v1/builds, /api/v1/projects)              │
└─────────────────────────────────────────────────────────────────────────┘
```

### Trust Boundaries

| Boundary | Mechanism | Secret Location |
|----------|-----------|-----------------|
| CLI → Cloud | RSA JWT (aud: `loopzero-cloud`) | Hydra JWKS (public key) |
| CLI → Engine | RSA JWT (aud: `loopzero-engine`) | Hydra JWKS (public key) |
| Dashboard → Cloud | HMAC JWT (aud: `loopzero-cloud`) | `SESSION_TOKEN_SECRET` (cloud only) |
| Dashboard → Engine | **Proxied** via cloud `/engine/*` | `LOOPZERO_SERVICE_AUTH_SECRET` (cloud+engine) |
| Cloud → Engine (internal) | `X-Service-Auth` header | `LOOPZERO_SERVICE_AUTH_SECRET` |
| Stripe → Cloud (webhooks) | HMAC-SHA256 signature | `STRIPE_WEBHOOK_SECRET` |
| Cloud → Kratos (internal) | Session cookie forwarding | Network-level (same cluster) |
| Cloud → Hydra (admin) | No auth (internal network) | NetworkPolicy (cloud pod only) |

### Key Design Decisions

1. **Engine holds zero signing secrets.** Only verifies RSA public keys via JWKS.
2. **Dashboard never talks to engine directly.** All engine calls proxy through cloud.
3. **Token audiences are strict.** Wrong audience = 401. No fallback.
4. **No "default" tenant.** Missing tenant_id = explicit 403 ("Account setup required").
5. **Subscriptions table is the single source of truth for plan.** Not tenants, not claims.

---

## 2. JWT Token Types

Two token types exist, both validated by `pkg/jwt/validator.go`:

### 2.1 Hydra RSA JWT (CLI + API)

| Property | Value |
|----------|-------|
| **Signer** | Ory Hydra |
| **Algorithm** | RS256 (JWKS rotation) |
| **Audience** | `["loopzero-cloud", "loopzero-engine"]` |
| **Issuer** | `https://auth.{domain}/` |
| **TTL** | 1 hour |
| **Consumer** | CLI → cloud service + engine API |
| **Key rotation** | Automatic via JWKS (5-min cache) |

```json
{
  "sub": "kratos-identity-uuid",
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

**Validation** (`pkg/jwt/validator.go:Validate`):
- Checks `alg` is RSA (rejects HMAC)
- Verifies `kid` against JWKS cache (auto-refresh on miss)
- Enforces `iss`, `aud`, `exp`

### 2.2 Session HMAC JWT (Dashboard)

| Property | Value |
|----------|-------|
| **Signer** | Cloud service |
| **Algorithm** | HS256 (HMAC-SHA256) |
| **Audience** | `loopzero-cloud` |
| **Issuer** | `loopzero-cloud` |
| **TTL** | 15 minutes |
| **Consumer** | Dashboard → cloud service only |
| **Key rotation** | Dual-key with `kid` header |

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
  "tenant_id": "tn-a1b2c3d4e5f67890"
}
```

**Token header includes `kid`** (first 8 hex chars of SHA256 of signing secret) for key identification during rotation.

**Validation** (`pkg/jwt/validator.go:ValidateSession`):
- Checks `alg` is HMAC (rejects RSA)
- Tries current secret first, then previous (rotation window)
- Enforces `iss`, `aud`, `exp`

### 2.3 How Each Service Validates

| Service | RSA (Hydra) | HMAC (Session) | Service Auth |
|---------|-------------|----------------|--------------|
| **Cloud** | `aud: loopzero-cloud` | `aud: loopzero-cloud` | N/A (is the proxy) |
| **Engine** | `aud: loopzero-engine` | **Rejected** (nil validator) | `X-Service-Auth` header |

---

## 3. Authentication Flows

### 3.1 CLI — Device Code Flow (RFC 8628)

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
     │◄─────────────────┤                 │                 │
     │                  │                 │                 │
     │ device_code +    │                 │                 │
     │ user_code +      │                 │                 │
     │ verification_uri │                 │                 │
     │                  │                 │                 │
     │  ┌─── User opens browser ─────────────────────────┐
     │  │    Hydra → /auth/device → validate Kratos      │
     │  │    Cloud consent handler auto-approves          │
     │  │    → EnsureTenant (non-fatal)                   │
     │  │    → AcceptConsent(aud: [loopzero-cloud,loopzero-engine])│
     │  └────────────────────────────────────────────────┘
     │                  │                 │                 │
     │ POST /v1/auth/   │ POST /oauth2/   │                 │
     │ device/token     │ token           │                 │
     │ (poll + jitter)  │                 │                 │
     ├─────────────────►├────────────────►│                 │
     │◄─────────────────┤◄────────────────┤                 │
     │                  │                 │                 │
     │ access_token (RSA JWT, aud: [loopzero-cloud, loopzero-engine])
     │ refresh_token                                        │
     │ expires_in: 3600                                     │
     │                  │                 │                 │
     │ Stores in ~/.loopzero/config.json (0600, atomic write)   │
```

**RFC 8628 compliance** (`cli/src/auth/device-flow.ts`):
- `slow_down` → interval increases by 5 seconds
- `expired_token` → polling stops with clear message
- `authorization_pending` → continue polling
- Jitter: ±20% on interval (prevents thundering herd)
- Safety limits: 60 max attempts + 5-minute timeout
- Minimum interval: 5 seconds (respects server preference)

**Post-login CLI output** (Vercel style):
```
  Authenticated as paulo@example.com
  Team: paulo (tn-a1b2c3d4e5f67890)
  Token expires: in 60 minutes

  Next: run `loopzero deploy` to deploy your project
```

**Token storage** (`cli/src/config/store.ts`):
- Location: `$XDG_CONFIG_HOME/loopzero/config.json` or `~/.loopzero/config.json`
- Permissions: `0600` (owner read/write only)
- Write: atomic (temp file + rename)
- Fields: `token`, `email`, `team`, `refresh_token`, `token_expires_at`
- Override: `LOOPZERO_TOKEN` env var takes precedence

**Token refresh** (`cli/src/auth/token-refresh.ts`):
- Endpoint: `POST /v1/auth/token/refresh` with `{refresh_token}`
- Proxied to Hydra's `/oauth2/token` with `grant_type=refresh_token`
- Pre-emptive: refreshes when token is within 60 seconds of expiry

### 3.2 Dashboard — BFF Token Exchange

```
┌──────────┐     ┌──────────┐     ┌─────────────┐
│ Dashboard│     │  Ory     │     │ Cloud       │
│  (SPA)   │     │  Kratos  │     │ Service     │
└────┬─────┘     └────┬─────┘     └──────┬──────┘
     │                │                  │
     │ GET /sessions/ │                  │
     │ whoami         │                  │
     ├───────────────►│                  │
     │◄───────────────┤ (session active) │
     │                │                  │
     │ POST /v1/auth/session/token       │
     │ (Kratos cookie forwarded)         │
     ├──────────────────────────────────►│
     │                │ GET /sessions/   │
     │                │ whoami           │
     │                │◄─────────────────┤
     │                │─────────────────►│
     │                │                  │ EnsureTenant()
     │                │                  │ (advisory lock,
     │                │                  │  UUID tenant ID)
     │◄──────────────────────────────────┤
     │ {access_token (HMAC), expires_in: 900}
     │                │                  │
     │ Token stored in JS memory (never localStorage)
     │ Auto-refresh 60s before expiry    │
     │ Cross-tab sync via BroadcastChannel
```

**Token lifecycle** (`cloud/dashboard/src/lib/api.ts`):
- Stored in-memory only (`let accessToken`). Never localStorage (XSS mitigation).
- TTL: 15 minutes with 60-second refresh buffer.
- Concurrent refresh deduplication: single `refreshPromise` shared by all callers.
- Auto-refresh: `ensureToken()` called before every API request.

**Cross-tab coordination** (BroadcastChannel `loopzero-auth`):
- `token-refreshed` → other tabs update their in-memory token (no redundant exchange)
- `logged-out` → other tabs clear token and redirect to login
- Graceful degradation: works independently if BroadcastChannel unavailable

**Session expired UX**:
- Toast notification: "Session expired. Redirecting to login..." (2-second delay)
- Login page shows: "Your session expired. Please sign in again." (via `?reason=expired`)

**Engine API access**:
- Dashboard calls `engineFetch('/api/v1/projects')` which maps to `cloudFetch('/engine/api/v1/projects')`
- Cloud proxy authenticates to engine via `X-Service-Auth` header
- Dashboard never holds engine-scoped credentials

### 3.3 Engine Proxy (Cloud → Engine)

```
Dashboard → cloudFetch('/engine/api/v1/projects')
         → Cloud service validates HMAC JWT (aud: loopzero-cloud)
         → Proxy strips /engine prefix
         → Sends to ENGINE_API_URL + /api/v1/projects
         → Headers: X-Service-Auth + X-LoopZero-User-ID + X-LoopZero-Tenant + X-LoopZero-Email + X-LoopZero-Role
         → Engine Auth middleware validates X-Service-Auth
         → Creates UserContext from X-LoopZero-* headers
         → Route handler executes with user context
         → Response piped back to dashboard
```

**Implementation** (`cloud/cmd/main.go`):
- Route: `/engine/*` → `cfg.EngineAPIURL` (internal network)
- Auth: `requireJWT` middleware on the cloud side + `X-Service-Auth` on engine side
- User context forwarded via headers (not Bearer token)

**Engine side** (`api/internal/middleware/auth.go:AuthWithTTL`):
- Checks `X-Service-Auth` header **before** Bearer token check
- If valid: creates `UserContext` from `X-LoopZero-*` headers, skips token validation
- If invalid: falls through to normal Bearer auth

---

## 4. Tenant Provisioning

### 4.1 EnsureTenant Flow

Called from two paths: Hydra consent handler (CLI) and session token exchange (dashboard).

```
EnsureTenant(ctx, userID, email)
  │
  ├─ Fast path: SELECT tenant+membership WHERE user_id = $1
  │  └─ Found? → return (tenantID, role)
  │
  ├─ Slow path (new user):
  │  ├─ BEGIN TX
  │  ├─ pg_advisory_xact_lock(hashtext(userID))  ← serialize per user
  │  ├─ Re-check membership inside lock
  │  │  └─ Found? → return (tenantID, role)  ← concurrent request won
  │  ├─ Generate tenant ID: tn-{crypto/rand 16 hex}
  │  ├─ INSERT users (ON CONFLICT → UPDATE email)
  │  ├─ INSERT tenants (id, display_name, slug, owner_id)
  │  │  └─ Slug collision? → append UUID suffix, retry
  │  ├─ Verify RowsAffected > 0
  │  ├─ INSERT tenant_members (tenant_id, user_id, 'owner')
  │  ├─ INSERT subscriptions (plan='starter', status='active')
  │  ├─ COMMIT
  │  └─ return (tenantID, "owner")
```

**Key properties:**
- **Idempotent:** Safe to call on every login (fast path for returning users)
- **Race-free:** `pg_advisory_xact_lock` serializes concurrent creates for the same user
- **UUID IDs:** `tn-{hex16}` — collision-free by construction, no TOCTOU
- **Non-fatal in consent:** If provisioning fails during CLI login, JWT is issued without `tenant_id`. Middleware returns 403 "Account setup required" instead of failing auth entirely.

### 4.2 Tenant Identity

| Field | Purpose | Example |
|-------|---------|---------|
| `tenants.id` | Primary key (UUID-based, immutable) | `tn-a1b2c3d4e5f67890` |
| `tenants.slug` | Human-readable display (UNIQUE, for CLI/URLs) | `paulo` |
| `tenants.display_name` | Cosmetic name | `Paulo` |
| `tenants.owner_id` | FK → users.id | `kratos-uuid-xxx` |

Slug is derived from email local-part at creation (`DeriveTenantSlug`). Collisions append a UUID suffix. Slug is display-only — all internal references use the UUID-based `id`.

---

## 5. Authorization

### 5.1 Role Hierarchy

```
owner (3) → all operations
  │
admin (2) → deploy, secrets, rollback (no billing, no member management)
  │
member (1) → deploy, view logs, view status (no delete, no secrets)
```

**Implementation** (`cloud/internal/auth/roles.go` + `api/internal/middleware/roles.go`):
- `RequireRole(minRole)` middleware checks `UserContext.Role` against hierarchy
- Returns 403 `INSUFFICIENT_ROLE` with specific message: "Requires owner role. Your role: member"
- Applied per-route: billing = owner, project-delete = admin+, deploy = member+

### 5.2 Tenant Isolation

- Every authenticated request carries `tenant_id` (from JWT claim or membership resolution)
- Engine resolves membership from DB on each request (not just from token claims)
- Multi-tenant users: must specify `X-LoopZero-Tenant` header or get 409 `TENANT_REQUIRED`
- No "default" tenant fallback — missing tenant_id = explicit 403

---

## 6. Secret Rotation

### 6.1 HMAC Session Token Rotation (Zero-Downtime)

```
Phase 1: Old secret active
  Cloud: SESSION_TOKEN_SECRET=old   → issues tokens with kid=abc12345
  
Phase 2: Deploy new secret with previous
  Cloud: SESSION_TOKEN_SECRET=new
         SESSION_TOKEN_SECRET_PREVIOUS=old
  → Issues new tokens with kid=def67890
  → Validates both old (kid=abc12345) and new (kid=def67890)
  
Phase 3: After 2×TTL (30 min), remove previous
  Cloud: SESSION_TOKEN_SECRET=new
         SESSION_TOKEN_SECRET_PREVIOUS=  (empty)
  → Only validates new tokens
```

**Implementation** (`pkg/jwt/validator.go:SessionTokenIssuer`):
- `kid` header: first 8 hex chars of SHA256(secret)
- `WithPreviousSecret(old)`: enables dual-key validation
- `ValidateSession`: tries current secret first, falls back to previous
- `Issue`: always uses current secret

### 6.2 RSA Key Rotation (Automatic)

- Hydra rotates RSA keys via JWKS endpoint
- `Validator` caches keys for 5 minutes
- On `kid` miss: auto-refreshes JWKS
- No manual intervention needed

---

## 7. Billing Architecture

### 7.1 Plan Tiers

| Plan | Price | Stripe | Source of Truth |
|------|-------|--------|-----------------|
| **Starter** | Free | DB-only (no Stripe customer) | `subscriptions.plan = 'starter'` |
| **Pro** | $19/mo | `STRIPE_PRICE_PRO` | `subscriptions.plan = 'pro'` |
| **Team** | $49/mo | `STRIPE_PRICE_TEAM` | `subscriptions.plan = 'team'` |
| **Enterprise** | Custom | Custom | `subscriptions.plan = 'enterprise'` |

**Single source of truth:** `subscriptions` table. The `tenants` table does not store plan information.

### 7.2 Checkout Flow (Starter → Paid)

```
Dashboard                Cloud Service              Stripe
   │                         │                        │
   │ POST /v1/billing/       │                        │
   │ checkout {plan:"pro"}   │                        │
   ├────────────────────────►│                        │
   │                         │ EnsureStripeCustomer() │
   │                         │ (lazy, idempotent)     │
   │                         ├───────────────────────►│
   │                         │◄───────────────────────┤
   │                         │ cus_xxx                │
   │                         │                        │
   │                         │ CreateCheckoutSession() │
   │                         │ idem-key: checkout-{tenant}-{price}-{hour}
   │                         ├───────────────────────►│
   │                         │◄───────────────────────┤
   │◄────────────────────────┤ {url: "https://checkout.stripe.com/..."}
   │                         │                        │
   │ window.location = url   │                        │
   │────────────────────────────────────────────────►│
   │  ┌── User enters payment ──┐                    │
   │  │   4242 4242 4242 4242   │                    │
   │  └─────────────────────────┘                    │
   │                         │                        │
   │ Redirect: /billing?success=true                  │
   │◄────────────────────────────────────────────────│
   │                         │                        │
   │ Activation polling      │ POST /v1/webhooks/stripe
   │ (2s interval, 30s max)  │ checkout.session.completed
   │ until plan changes      │◄───────────────────────┤
   │                         │                        │
   │                         │ UPDATE subscriptions   │
   │                         │ SET plan, status,      │
   │                         │     stripe_subscription_id
   │                         │                        │
   │ Plan changed!           │                        │
   │ "Welcome to Pro"        │                        │
```

**Idempotency keys:**
- `CreateCustomer`: `{tenantID}-create-customer` (deterministic, permanent)
- `CreateCheckoutSession`: `checkout-{tenantID}-{priceID}-{hourBucket}` (hourly window: dedup double-clicks, allow retry after session expiry)

**Post-checkout UX** (`cloud/dashboard/src/pages/billing.tsx`):
- Shows "Activating your subscription..." spinner
- Polls `/v1/billing` every 2 seconds, up to 30 seconds
- Compares against actual pre-checkout plan (not hardcoded)
- Waits for initial billing snapshot before starting (guards `undefined`)
- Success: shows "Welcome to {plan}" with updated billing data
- Timeout: shows "Taking longer than expected" with support link

### 7.3 Webhook Processing

**Events handled** (`cloud/internal/billing/webhook.go`):

| Event | Handler | Action |
|-------|---------|--------|
| `checkout.session.completed` | Links `stripe_subscription_id`, sets initial plan | UPDATE subscriptions |
| `customer.subscription.updated` | Syncs status AND plan (from price ID) | UPDATE subscriptions |
| `customer.subscription.deleted` | Marks subscription `canceled` | UPDATE subscriptions |
| `invoice.paid` | Informational log | None |
| `invoice.payment_failed` | Warning log (Stripe handles retries) | None |

**Idempotency** (`webhook_events` table):
- Check: `SELECT EXISTS(... WHERE event_id = $1 AND status = 'processed')`
- Only events with `status='processed'` are considered handled
- Events with `status='failed'` are retryable on Stripe retry
- `attempt_count` incremented on each retry
- `last_error`, `payload_hash` stored for operational visibility

**Signature verification:**
- HMAC-SHA256 of `{timestamp}.{payload}` with webhook secret
- Timestamp tolerance: ±300 seconds (rejects replay AND clock-skewed events)
- Constant-time comparison (`hmac.Equal`)

### 7.4 Plan Enforcement

**Resolution** (`cloud/internal/enforcement/limiter.go`):
```sql
SELECT plan FROM subscriptions WHERE tenant_id = $1 AND status IN ('active', 'trialing') LIMIT 1
```
Falls back to `'starter'` if no row found.

**Limit logic:**
- **Enterprise:** always allowed (bypass)
- **Team with soft_limit:** allowed + `SoftOver=true` (overage billing)
- **Starter/Pro hard_limit:** blocked with 402 + upgrade URL
- **NULL hard_limit:** unlimited (no check)

**Constraint enforcement** (migration 000021):
- `subscriptions.plan` CHECK: `IN ('starter', 'pro', 'team', 'enterprise')`
- `subscriptions.status` CHECK: `IN ('active', 'past_due', 'canceled', 'incomplete', 'trialing', 'unpaid')`

### 7.5 Usage Metering

**Recording** (`cloud/internal/metering/tracker.go`):
- `RecordBuildCompletion(tenantID, durationSeconds)` — increments `builds` count + `build_minutes`
- `RecordStorageSnapshot(tenantID, storageBytes)` — snapshot (replaces, not increments) `storage_mb`
- Atomic: both metrics in one transaction
- Idempotent: `INSERT ... ON CONFLICT DO UPDATE SET quantity = quantity + $increment`

**Reporting** (`cloud/internal/metering/reporter.go`):
- Runs hourly
- Queries unreported records (`reported_to_stripe = false`, limit 100)
- Sends to Stripe via `ReportUsage()`, marks `reported_to_stripe = true`
- Failures left for retry on next run

**Reconciliation** (`cloud/internal/metering/reconciler.go`):
- Runs daily (24h interval)
- Compares `usage_records` against actual `builds` table counts
- Drift < 5%: auto-corrects usage_records
- Drift ≥ 5%: logs warning, invokes `DriftCallback` (for alerting)

---

## 8. Database Schema

### 8.1 Auth Tables

```sql
users
  id             TEXT PK        -- Kratos UUID or legacy github-{username}
  email          TEXT NOT NULL UNIQUE
  github_id      BIGINT NULL    -- NULL for Kratos users (legacy, nullable)
  display_name   TEXT
  created_at     TIMESTAMPTZ
  updated_at     TIMESTAMPTZ

tenants
  id             TEXT PK        -- "tn-{hex16}" UUID-based (collision-free)
  display_name   TEXT
  slug           TEXT UNIQUE    -- Human-readable (for CLI/URLs)
  owner_id       TEXT FK → users(id)
  cell_id        TEXT
  created_at     TIMESTAMPTZ
  updated_at     TIMESTAMPTZ

tenant_members
  tenant_id      TEXT FK → tenants(id)  ┐
  user_id        TEXT FK → users(id)    ├─ PK
  role           TEXT CHECK (IN owner/admin/member)
  created_at     TIMESTAMPTZ
  updated_at     TIMESTAMPTZ

user_identity_map  -- DEPRECATED: removal target 2026-Q3
  user_id            TEXT FK → users(id)
  kratos_identity_id TEXT PK
  created_at         TIMESTAMPTZ
```

### 8.2 Billing Tables

```sql
subscriptions                          -- Single source of truth for plan
  id                     TEXT PK
  tenant_id              TEXT UNIQUE FK → tenants(id)
  stripe_customer_id     TEXT NULL      -- NULL for starter (DB-only)
  stripe_subscription_id TEXT NULL      -- NULL until checkout completes
  plan                   TEXT NOT NULL DEFAULT 'starter'
                         CHECK (IN starter/pro/team/enterprise)
  status                 TEXT NOT NULL DEFAULT 'active'
                         CHECK (IN active/past_due/canceled/incomplete/trialing/unpaid)
  current_period_start   TIMESTAMPTZ
  current_period_end     TIMESTAMPTZ
  created_at             TIMESTAMPTZ
  updated_at             TIMESTAMPTZ

plan_limits
  plan        TEXT  ┐
  metric      TEXT  ├─ PK
  hard_limit  INT NULL    -- NULL = unlimited
  soft_limit  INT NULL    -- NULL = no soft limit (Team plan only)

usage_records
  id                 TEXT PK
  tenant_id          TEXT FK → tenants(id)
  metric             TEXT
  quantity           INT DEFAULT 0
  period_start       TIMESTAMPTZ
  period_end         TIMESTAMPTZ
  reported_to_stripe BOOL DEFAULT false
  created_at         TIMESTAMPTZ
  UNIQUE (tenant_id, metric, period_start)

webhook_events
  event_id      TEXT PK
  event_type    TEXT NOT NULL
  status        TEXT NOT NULL DEFAULT 'processed'
  received_at   TIMESTAMPTZ
  last_error    TEXT
  attempt_count INT DEFAULT 1
  payload_hash  TEXT
  processed_at  TIMESTAMPTZ
```

---

## 9. Observability

### 9.1 Metrics (`cloud/internal/metrics/metrics.go`)

Exposed on `GET /metrics` in Prometheus text format:

**Auth metrics:**
- `loopzero_auth_token_exchanges_total{result}` — session token exchange success/failure
- `loopzero_auth_device_flow_total{result}` — device flow completions
- `loopzero_tenant_provisioning_total{result}` — EnsureTenant success/failure
- `loopzero_tenant_provisioning_duration_ms_avg` — average provisioning latency
- `loopzero_auth_rejections_total{reason}` — 401/403 by reason (expired, invalid, no_tenant)

**Billing metrics:**
- `loopzero_webhook_events_total{status}` — webhook events processed/failed
- `loopzero_billing_checkout_total{result}` — checkout started/error
- `loopzero_usage_drift_detected_total` — drift reconciliation events

### 9.2 Error Responses

All error responses include `request_id` for tracing:
```json
{
  "code": "AUTH_REQUIRED",
  "message": "Not authenticated. Run: loopzero login",
  "request_id": "req-a1b2c3d4"
}
```

### 9.3 Rate Limiting Headers

Token exchange endpoint returns standard headers:
```
X-RateLimit-Limit: 10
X-RateLimit-Remaining: 7
X-RateLimit-Reset: 1712345678
```

---

## 10. Security Headers

**CSP** (Traefik middleware, `infra/helm/traefik/middlewares.yaml`):
```
default-src 'self';
script-src 'self' https://js.stripe.com;
style-src 'self' 'unsafe-inline';
connect-src 'self' https://api.stripe.com https://checkout.stripe.com;
frame-src https://checkout.stripe.com https://js.stripe.com;
img-src 'self' data: https:;
font-src 'self' data:;
```

**Other headers:** `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `HSTS: 1 year`, `X-XSS-Protection: 1; mode=block`.

**CORS** (`cloud/cmd/main.go`):
- Allowed origins: `BillingReturnURL` + `DASHBOARD_CORS_ORIGIN` env var
- `Access-Control-Allow-Credentials: true` for session cookie forwarding
- Explicit `OPTIONS` preflight handling

---

## 11. Configuration Reference

### Cloud Service

| Variable | Required | Default | Purpose |
|----------|----------|---------|---------|
| `KRATOS_PUBLIC_URL` | prod | `http://ory-kratos-public.loopzero-system:80` | Browser-facing Kratos |
| `KRATOS_INTERNAL_URL` | prod | same as public | Server-to-server Kratos |
| `KRATOS_ADMIN_URL` | prod | `http://ory-kratos-admin.loopzero-system:80` | Identity management |
| `HYDRA_PUBLIC_URL` | prod | `http://ory-hydra-public.loopzero-system:4444` | OAuth2 endpoints |
| `HYDRA_ADMIN_URL` | prod | `http://ory-hydra-admin.loopzero-system:4445` | Consent/login admin |
| `HYDRA_CLIENT_ID` | no | `loopzero-cli` | OAuth2 client ID |
| `JWT_ISSUER` | no | `{HYDRA_PUBLIC_URL}/` | Expected JWT issuer |
| `SESSION_TOKEN_SECRET` | prod (≥32 chars) | auto-generated | HMAC signing key |
| `SESSION_TOKEN_SECRET_PREVIOUS` | no | empty | Previous key for rotation |
| `LOOPZERO_SERVICE_AUTH_SECRET` | prod | auto-generated | Cloud→engine proxy auth |
| `ENGINE_API_URL` | no | `http://loopzero-api.loopzero-system:8080` | Engine API internal URL |
| `STRIPE_API_KEY` | for billing | empty | Stripe API key |
| `STRIPE_WEBHOOK_SECRET` | for billing | empty | Webhook signature secret |
| `BILLING_RETURN_URL` | no | `http://localhost:3000` | Post-checkout redirect |
| `DASHBOARD_CORS_ORIGIN` | no | empty | Additional CORS origin |
| `STRIPE_PRICE_PRO` | for billing | empty | Pro plan Stripe price ID |
| `STRIPE_PRICE_TEAM` | for billing | empty | Team plan Stripe price ID |

### Engine API

| Variable | Required | Default | Purpose |
|----------|----------|---------|---------|
| `JWKS_URL` | prod | empty | Hydra JWKS endpoint |
| `JWT_ISSUER` | prod | empty | Expected JWT issuer |
| `LOOPZERO_SERVICE_AUTH_SECRET` | prod | empty | Cloud proxy auth secret |

---

## 12. File Map

```
pkg/jwt/
  validator.go              ← Validator (RSA), SessionTokenIssuer (HMAC+kid+rotation)

cloud/
  cmd/main.go               ← Service wiring, CORS, engine proxy, routes
  internal/
    auth/
      middleware.go          ← RequireJWT (RSA+HMAC), UserContext, no "default" fallback
      tenant.go              ← EnsureTenant (UUID ID, advisory lock, subscription creation)
      hydra.go               ← HydraClient (device flow, consent with audience)
      migration.go           ← IdentityMigrator (legacy GitHub→Kratos, DEPRECATED)
      roles.go               ← RequireRole middleware (owner > admin > member)
    billing/
      stripe.go              ← StripeClient (idempotency keys, 15s timeout)
      webhook.go             ← ProcessEvent (idempotent, status tracking, retry-safe)
      provisioner.go         ← ProvisionStarter (DB-only), EnsureStripeCustomer (lazy)
      plans.go               ← PlanConfig, PriceIDForPlan, PlanForPriceID
    consent/
      handler.go             ← Hydra login/consent/device callbacks
    enforcement/
      limiter.go             ← CheckLimit (NULL=unlimited, soft/hard, enterprise bypass)
    metering/
      tracker.go             ← RecordBuildCompletion, RecordStorageSnapshot
      reconciler.go          ← ReconcileBuilds (daily, auto-correct <5%, alert ≥5%)
      reporter.go            ← ReportPending (hourly → Stripe)
      scheduler.go           ← Background job intervals (1h reporter, 24h reconciler)
    ratelimit/
      limiter.go             ← Limiter interface, InMemoryLimiter (sliding window)
    metrics/
      metrics.go             ← Auth + billing Prometheus counters
    config/
      config.go              ← All env vars, production validation
    routes/
      billing.go             ← GET /v1/billing, POST /checkout, GET /portal, POST /webhooks/stripe
      session_token.go       ← POST /v1/auth/session/token (BFF exchange, rate limited)
      device.go              ← Device Code Flow endpoints

  dashboard/src/
    lib/api.ts               ← cloudFetch, engineFetch (proxied), BroadcastChannel, logout
    lib/auth.ts              ← Kratos session, email verification, logout
    hooks/use-auth.ts        ← useRequireAuth (session + token exchange)
    pages/billing.tsx         ← Post-checkout polling, unlimited metrics (null), activation UX
    pages/login.tsx           ← Session expired message (?reason=expired)

api/
  cmd/main.go                ← RSA-only validator (aud: loopzero-engine), ServiceAuthSecret
  internal/middleware/
    auth.go                  ← Auth (Bearer + X-Service-Auth), UserContext, cache
    jwt_validator.go         ← JWTOnlyValidator (RSA-only, nil HMAC)
    roles.go                 ← RequireRole middleware (same hierarchy)

cli/src/
  auth/device-flow.ts        ← RFC 8628 (jitter, slow_down, expired_token, max retries)
  auth/token-refresh.ts      ← Refresh token flow
  config/store.ts            ← ~/.loopzero/config.json (0600, atomic, XDG support)
  commands/login.ts          ← Rich post-auth output (Vercel style)
```
