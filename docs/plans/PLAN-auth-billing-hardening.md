# Auth & Billing Hardening Plan — Vercel-Level Quality

> Executable remediation plan for all remaining items from FAANG production review.
> Reference: MEET-2026-04-07-039 + review sections 1-22.
> Created: 2026-04-07.

**Principle:** No workarounds. No unnecessary complexity. Fix the root cause.

---

## Phase 1 — Billing Model Integrity

> Billing is the area with the most open issues and the highest business risk.
> A billing bug costs real money and erodes trust instantly.

### 1.1 Single Source of Truth for Plan

**Problem:** `tenants.plan`, `subscriptions.plan`, and `plan_limits.plan` create three
potential answers to "what plan is this tenant on?" Webhook updates one, enforcement
reads another, dashboard shows a third.

**Fix:**
- `subscriptions` table is the single source of truth for the current plan.
- Remove `tenants.plan` column entirely (migration 000018).
- All plan reads go through `subscriptions.plan`.
- `tenants` table stores identity and ownership only.
- If tenant has no subscription row, they are on `starter` (code default, not DB default).

**Files:**
- New: `api/internal/db/migrations/000018_remove_tenants_plan.up.sql`
- Edit: `cloud/internal/billing/provisioner.go` — ProvisionStarter creates subscription row with `plan='starter'`
- Edit: `cloud/internal/routes/billing.go` — GetBilling reads from subscriptions only
- Edit: `cloud/internal/auth/tenant.go` — EnsureTenant creates subscription row
- Edit: any code that reads `tenants.plan`

**DoD:**
- [ ] `tenants.plan` column does not exist in schema
- [ ] `SELECT plan FROM subscriptions WHERE tenant_id = $1` is the only plan query
- [ ] `EnsureTenant` creates a `subscriptions` row with `plan='starter', status='active'`
- [ ] `GET /v1/billing` returns plan from subscriptions table
- [ ] Grep for `tenants.plan` returns zero results in Go code
- [ ] Existing tests pass

---

### 1.2 Webhook Error Handling + Rich Event Table

**Problem:** `ProcessEvent` ignores DB errors with `_ =` on both the idempotency check
and the event marking. The `webhook_events` table has only 3 columns — no error tracking,
no replay capability.

**Fix:**
- Handle all DB errors explicitly. Log them. Return error when critical.
- Use a transaction: check + process + mark as one atomic operation.
- Expand `webhook_events` schema with operational fields.

**Files:**
- New: `api/internal/db/migrations/000019_webhook_events_enrichment.up.sql`
- Edit: `cloud/internal/billing/webhook.go` — rewrite ProcessEvent

**Migration 000019:**
```sql
ALTER TABLE webhook_events ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'processed';
ALTER TABLE webhook_events ADD COLUMN IF NOT EXISTS received_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE webhook_events ADD COLUMN IF NOT EXISTS last_error TEXT;
ALTER TABLE webhook_events ADD COLUMN IF NOT EXISTS attempt_count INT NOT NULL DEFAULT 1;
ALTER TABLE webhook_events ADD COLUMN IF NOT EXISTS payload_hash TEXT;
```

**ProcessEvent rewrite pattern:**
```go
// 1. Check idempotency (return error on DB failure, don't ignore)
err := h.db.QueryRow(...).Scan(&processed)
if err != nil {
    return fmt.Errorf("idempotency check: %w", err)
}
if processed { return nil }

// 2. Process event
err = h.handleEvent(event)

// 3. Record event with status
status := "processed"
var lastError *string
if err != nil {
    status = "failed"
    e := err.Error()
    lastError = &e
}
_, markErr := h.db.Exec(
    `INSERT INTO webhook_events (event_id, event_type, status, last_error, payload_hash, received_at, processed_at)
     VALUES ($1, $2, $3, $4, $5, now(), now())
     ON CONFLICT (event_id) DO UPDATE SET attempt_count = webhook_events.attempt_count + 1,
     last_error = EXCLUDED.last_error, status = EXCLUDED.status, processed_at = now()`,
    event.ID, event.Type, status, lastError, hashPayload(event.Data),
)
if markErr != nil {
    h.logger.Error("failed to record webhook event", "event_id", event.ID, "error", markErr)
}
return err
```

**DoD:**
- [ ] Zero `_ =` or `_, _ =` in webhook.go
- [ ] DB errors on idempotency check are logged and returned
- [ ] `webhook_events` has `status`, `last_error`, `attempt_count`, `received_at`, `payload_hash`
- [ ] Failed events are recorded with `status='failed'` and `last_error` populated
- [ ] Retry of same event increments `attempt_count`
- [ ] Tests: successful event recorded, failed event recorded with error, duplicate event idempotent

---

### 1.3 Checkout Completed — Map Real Subscription States

**Problem:** `handleCheckoutCompleted` hardcodes `plan = 'pro'` regardless of actual
price/plan. `handleSubscriptionUpdated` syncs status but not plan name.

**Fix:**
- Derive plan from Stripe price ID (reverse lookup via PlanConfig).
- Map all Stripe subscription statuses to internal states.
- `checkout.session.completed` links subscription ID + sets plan from price.
- `subscription.updated` syncs status AND plan (price can change on upgrade/downgrade).

**Files:**
- Edit: `cloud/internal/billing/webhook.go` — handleCheckoutCompleted, handleSubscriptionUpdated
- Edit: `cloud/internal/billing/plans.go` — add `PlanForPriceID(priceID string) string`

**DoD:**
- [ ] `handleCheckoutCompleted` reads `line_items` or price from session and maps to plan name
- [ ] `handleSubscriptionUpdated` syncs both status AND plan (from price)
- [ ] Plan mapping is driven by `PlanConfig`, not hardcoded strings
- [ ] Subscription status values in DB match Stripe states: `active`, `past_due`, `canceled`, `incomplete`, `trialing`, `unpaid`
- [ ] Tests: checkout with pro price → plan='pro', checkout with team price → plan='team'
- [ ] Tests: subscription updated with status='past_due' → DB reflects 'past_due'

---

### 1.4 Checkout Atomicity + Idempotency

**Problem:** `EnsureStripeCustomer` creates customer in Stripe, then persists ID to DB.
If DB write fails, customer is orphaned. No idempotency key on checkout session creation.

**Fix:**
- Use deterministic idempotency key for Stripe customer creation: `sha256(tenant_id)`.
- Use deterministic idempotency key for checkout session: `sha256(tenant_id + plan + period)`.
- If DB write fails after customer creation, return error (not stale customer ID).

**Files:**
- Edit: `cloud/internal/billing/provisioner.go` — EnsureStripeCustomer
- Edit: `cloud/internal/billing/stripe.go` — add IdempotencyKey to Stripe calls

**DoD:**
- [ ] `CreateCustomer` uses `Idempotency-Key: idem-cust-{sha256(tenant_id)[:16]}`
- [ ] `CreateCheckoutSession` uses `Idempotency-Key: idem-checkout-{sha256(tenant_id+plan)[:16]}`
- [ ] DB write failure in EnsureStripeCustomer returns error (not silent fallback)
- [ ] Tests: EnsureStripeCustomer with DB failure returns error, does not return orphan ID

---

### 1.5 plan_limits — NULL for Unlimited

**Problem:** `999999` sentinel value for unlimited metrics. Leaks into UI, comparisons, reports.

**Fix:**
- Migration: change `999999` values to `NULL` in `plan_limits.hard_limit`.
- Application code: `NULL` means unlimited. Enforcement skips limit check when NULL.
- Dashboard: displays "Unlimited" when limit is null.

**Files:**
- New: `api/internal/db/migrations/000020_plan_limits_null_unlimited.up.sql`
- Edit: `cloud/internal/enforcement/limiter.go` — NULL = unlimited
- Edit: `cloud/internal/routes/billing.go` — GetBilling returns `null` for unlimited

**DoD:**
- [ ] No `999999` values in `plan_limits` table
- [ ] `hard_limit NULL` means unlimited
- [ ] Enforcement: `if limit == nil { return allowed }`
- [ ] API response: `"limit": null` for unlimited metrics
- [ ] Dashboard: shows `∞` when limit is null
- [ ] Tests: unlimited metric passes enforcement, limited metric enforced

---

### 1.6 Usage Drift Reconciler — Active Correction

**Problem:** Drift reconciler is read-only (logs but doesn't correct) and runs weekly.

**Fix:**
- Reconciler corrects drift automatically when within threshold.
- Alerts when drift exceeds threshold (requires manual review).
- Run daily instead of weekly.
- Add correlation between recorded and actual counts.

**Files:**
- Edit: `cloud/internal/metering/reconciler.go` — auto-correct + alert
- Edit: `cloud/cmd/main.go` — change interval from 168h to 24h

**DoD:**
- [ ] Drift < 5%: auto-corrected, logged at INFO
- [ ] Drift >= 5%: logged at WARN, DriftCallback invoked (for alerting)
- [ ] Reconciler runs daily (24h interval)
- [ ] Tests: drift within threshold auto-corrected, drift above threshold triggers callback

---

## Phase 2 — Auth Hardening

> These items make auth production-grade: proper RFC compliance, rotation support, rate limiting.

### 2.1 Device Code Flow — RFC 8628 Compliance

**Problem:** CLI polling doesn't handle `slow_down` response, doesn't implement jitter/backoff,
doesn't limit max retries.

**Fix:**
- Handle `slow_down` response: increase interval by 5 seconds per RFC 8628 §3.5.
- Add jitter (±20%) to polling interval to prevent thundering herd.
- Handle `expired_token` response explicitly.
- Add max retry count (60 retries = 5 minutes at 5s interval).

**Files:**
- Edit: `cli/src/auth/device-flow.ts` — pollDeviceToken, executeDeviceCodeFlow

**DoD:**
- [ ] `slow_down` response increases polling interval by 5s
- [ ] Polling interval includes ±20% jitter
- [ ] `expired_token` response stops polling with clear message
- [ ] Max 60 polling attempts (safety limit alongside timeout)
- [ ] Tests: slow_down increases interval, expired_token stops, jitter is bounded

---

### 2.2 Secret Rotation — kid + Dual-Key Validation

**Problem:** No `kid` header on HMAC tokens. Single secret means rotation requires
coordinated restart with downtime window.

**Fix:**
- Add `kid` claim to HMAC tokens (first 8 chars of SHA256 of secret).
- `SessionTokenIssuer` accepts list of valid secrets: `[current, previous]`.
- Validation tries current secret first, then previous (for rotation window).
- Only current secret is used for issuance.

**Files:**
- Edit: `pkg/jwt/validator.go` — SessionTokenIssuer supports multiple secrets
- Edit: `cloud/internal/config/config.go` — support `SESSION_TOKEN_SECRET_PREVIOUS` env var
- Edit: `cloud/cmd/main.go` — pass both secrets

**DoD:**
- [ ] Issued tokens include `kid` header
- [ ] Validator accepts tokens signed with current OR previous secret
- [ ] New tokens are always signed with current secret
- [ ] After rotation window (2x TTL = 30min), previous secret can be removed
- [ ] Tests: token signed with old secret accepted, token signed with unknown secret rejected

---

### 2.3 Rate Limiting — Per-Identity, Distributed-Ready

**Problem:** Rate limiter is in-memory (`map[string][]time.Time`), IP-only,
not distributed. Multiple replicas have independent state.

**Fix:**
- Rate limit by identity (user ID from session) + IP.
- Use a simple interface `RateLimiter` so implementation can be swapped (in-memory → Redis).
- For now, keep in-memory but key by `userID|IP` instead of IP only.
- Add `X-RateLimit-Remaining` and `X-RateLimit-Reset` response headers.

**Files:**
- Edit: `cloud/internal/routes/session_token.go` — rate limiter keyed by identity+IP
- New: `cloud/internal/ratelimit/limiter.go` — interface + in-memory implementation

**DoD:**
- [ ] Rate limit keyed by `userID|IP` (identity when available, IP as fallback)
- [ ] Response includes `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset` headers
- [ ] `RateLimiter` interface exists for future swap to distributed implementation
- [ ] Tests: same user from different IPs shares limit, different users from same IP have separate limits

---

### 2.4 Schema Constraints — Database Rejects Invalid State

**Problem:** `users.email` not UNIQUE, `role` and `plan` without CHECK constraints,
no format validation on IDs.

**Fix:**
- Add CHECK constraints for enum-like columns.
- Add UNIQUE on `users.email` (email is the identity credential).
- Add CHECK on subscription `status` to match Stripe states.

**Files:**
- New: `api/internal/db/migrations/000021_schema_constraints.up.sql`

**Migration:**
```sql
-- Email uniqueness (one account per email)
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_unique ON users (email);

-- Role validation
ALTER TABLE tenant_members ADD CONSTRAINT chk_role
  CHECK (role IN ('owner', 'admin', 'member'));

-- Plan validation
ALTER TABLE subscriptions ADD CONSTRAINT chk_plan
  CHECK (plan IN ('starter', 'pro', 'team', 'enterprise'));

-- Subscription status validation (matches Stripe states)
ALTER TABLE subscriptions ADD CONSTRAINT chk_subscription_status
  CHECK (status IN ('active', 'past_due', 'canceled', 'incomplete', 'trialing', 'unpaid'));
```

**DoD:**
- [ ] `users.email` has UNIQUE index
- [ ] `tenant_members.role` has CHECK constraint
- [ ] `subscriptions.plan` has CHECK constraint
- [ ] `subscriptions.status` has CHECK constraint matching Stripe states
- [ ] Invalid INSERT (bad role, bad plan, bad status) fails at DB level
- [ ] Tests: invalid role INSERT fails, invalid status INSERT fails

---

## Phase 3 — Frontend Security & UX

> These items affect what users see and how secure their browser session is.

### 3.1 CSP Headers

**Problem:** No Content-Security-Policy header. Only basic security headers from Traefik.

**Fix:**
- Add CSP as Traefik middleware (not application code — infrastructure concern).
- Policy: `default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self' https://checkout.stripe.com; frame-src https://checkout.stripe.com https://js.stripe.com;`
- This blocks XSS from loading external scripts while allowing Stripe checkout iframe.

**Files:**
- Edit: `infra/helm/traefik/middlewares.yaml` — add CSP to security-headers

**DoD:**
- [ ] `Content-Security-Policy` header present on all dashboard responses
- [ ] External scripts blocked except Stripe
- [ ] Dashboard loads and functions correctly (no CSP violations in console)
- [ ] Stripe checkout works through iframe

---

### 3.2 CORS — Explicit Origin Validation

**Problem:** No explicit CORS configuration. Currently works because dashboard and API
share the same domain via Traefik. But if architecture changes, this silently breaks.

**Fix:**
- Add explicit CORS middleware to cloud service.
- Allow only the dashboard origin.
- This makes the boundary explicit and prevents accidental cross-origin access.

**Files:**
- Edit: `cloud/cmd/main.go` — add CORS middleware with explicit allowed origin

**DoD:**
- [ ] CORS middleware configured with `AllowedOrigins: [dashboardURL]`
- [ ] `Access-Control-Allow-Credentials: true` for session cookie forwarding
- [ ] Preflight requests handled correctly
- [ ] Cross-origin requests from unknown origins rejected

---

### 3.3 Dashboard — Tab Coordination via BroadcastChannel

**Problem:** Each browser tab maintains independent token state. Login in one tab
doesn't propagate. Logout in one tab doesn't propagate.

**Fix:**
- Use `BroadcastChannel('loopzero-auth')` to coordinate token state across tabs.
- On token exchange: broadcast `{ type: 'token-refreshed', token, expiresAt }`.
- On logout: broadcast `{ type: 'logged-out' }`.
- On receive `logged-out`: clear local token and redirect to login.
- On receive `token-refreshed`: update local token (avoid redundant exchange).

**Files:**
- Edit: `cloud/dashboard/src/lib/api.ts` — add BroadcastChannel coordination

**DoD:**
- [ ] Login in Tab A → Tab B receives token without calling exchange endpoint
- [ ] Logout in Tab A → Tab B redirects to login page
- [ ] Token refresh in Tab A → Tab B uses refreshed token (no duplicate exchange)
- [ ] Graceful degradation: if BroadcastChannel unavailable, each tab works independently

---

### 3.4 Dashboard — Billing UX After Checkout

**Problem:** After Stripe checkout redirect, user lands on `/billing?success=true` but
sees no feedback. Webhook may not have processed yet.

**Fix:**
- Show "Activating your subscription..." state on `/billing?success=true`.
- Poll `/v1/billing` every 2 seconds for up to 30 seconds.
- When plan changes from starter → paid, show success toast.
- If 30 seconds pass without change, show "This may take a moment" with support link.
- Clear `?success=true` from URL after confirmation.

**Files:**
- Edit: `cloud/dashboard/src/pages/billing.tsx` — post-checkout polling UX

**DoD:**
- [ ] `/billing?success=true` shows "Activating your subscription..." spinner
- [ ] Polls billing endpoint every 2s, max 30s
- [ ] Success: shows confirmation with new plan name
- [ ] Timeout: shows "Taking longer than expected" with support contact
- [ ] URL cleaned up after confirmation
- [ ] Tests: mock polling with delayed response → shows activation flow

---

### 3.5 CLI Login — Rich Post-Auth Output

**Problem:** CLI login shows minimal output. User doesn't know which account, team,
or token validity they have.

**Fix:**
- After successful login, show: account email, team name, token expiry, next command suggestion.
- Match Vercel CLI style: clear, informative, actionable.

**Output format:**
```
  Authenticated as paulo@example.com
  Team: paulo (tn-a1b2c3d4)
  Token expires: in 1 hour
  
  Next: run `loopzero init` to set up a project
```

**Files:**
- Edit: `cli/src/commands/login.ts` — enhance post-login output

**DoD:**
- [ ] Shows email, team name, token expiry duration
- [ ] Shows `loopzero init` suggestion for new users or `loopzero deploy` for existing
- [ ] `--json` output includes `email`, `team`, `team_id`, `expires_in`
- [ ] Tests: post-login output contains email and team

---

### 3.6 Dashboard — Session Expired UX

**Problem:** When session expires and refresh fails, user is hard-redirected to login
with no explanation.

**Fix:**
- On auth failure (refresh fails), show a toast notification: "Session expired. Redirecting to login..."
- 2-second delay before redirect so user understands what happened.
- After redirect, login page shows: "Your session expired. Please sign in again."

**Files:**
- Edit: `cloud/dashboard/src/lib/api.ts` — show toast before redirect
- Edit: `cloud/dashboard/src/pages/login.tsx` — show "session expired" message from query param

**DoD:**
- [ ] Session expiry shows brief notification before redirect
- [ ] Login page shows "Session expired" when redirected from expiry
- [ ] Normal login (not from expiry) shows no extra message

---

## Phase 4 — Observability

> You can't operate what you can't see.

### 4.1 Auth Metrics

**Problem:** No metrics for auth flows. Can't detect token exchange storms, provisioning
failures, or auth errors by type.

**Fix:**
- Add Prometheus counters/histograms to cloud service:
  - `loopzero_auth_token_exchanges_total{result}` — success/failure of session token exchange
  - `loopzero_auth_device_flow_total{result}` — device flow completions
  - `loopzero_tenant_provisioning_total{result}` — success/failure of EnsureTenant
  - `loopzero_tenant_provisioning_duration_seconds` — histogram
  - `loopzero_auth_rejections_total{reason}` — 401/403 by reason (expired, invalid, no_tenant)

**Files:**
- New: `cloud/internal/metrics/auth.go` — auth metric definitions
- Edit: `cloud/internal/auth/middleware.go` — increment on rejection
- Edit: `cloud/internal/routes/session_token.go` — increment on exchange
- Edit: `cloud/internal/auth/tenant.go` — increment on provisioning

**DoD:**
- [ ] 5 metrics registered and exposed on `/metrics`
- [ ] Provisioning failures visible as `loopzero_tenant_provisioning_total{result="error"}`
- [ ] Token exchange latency visible as histogram
- [ ] Auth rejections categorized by reason

---

### 4.2 Billing Metrics

**Problem:** No metrics for billing operations. Webhook failures, drift, checkout
errors are invisible.

**Fix:**
- `loopzero_webhook_events_total{type,status}` — processed/failed by event type
- `loopzero_billing_checkout_total{result}` — checkout initiated/succeeded/failed
- `loopzero_usage_drift_detected_total{metric}` — drift events by metric type
- `loopzero_stripe_api_duration_seconds{operation}` — Stripe API call latency

**Files:**
- New: `cloud/internal/metrics/billing.go` — billing metric definitions
- Edit: `cloud/internal/billing/webhook.go` — increment on event processing
- Edit: `cloud/internal/routes/billing.go` — increment on checkout
- Edit: `cloud/internal/metering/reconciler.go` — increment on drift

**DoD:**
- [ ] 4 metrics registered and exposed
- [ ] Webhook failures visible in Prometheus
- [ ] Drift events counted and alertable
- [ ] Stripe API latency measurable

---

### 4.3 Error Responses — Add request_id

**Problem:** Error responses have `{code, message}` but no `request_id` for support
and tracing.

**Fix:**
- Add `request_id` to all error responses. Use the existing correlation ID middleware.
- Response format: `{"code": "...", "message": "...", "request_id": "corr-xxxx"}`.
- Cloud service uses the same pattern.

**Files:**
- Edit: `cloud/internal/auth/middleware.go` — include request_id in error responses
- Edit: `cloud/internal/routes/*.go` — include request_id in error responses
- Edit: `api/internal/middleware/auth.go` — include request_id in error responses

**DoD:**
- [ ] All error responses include `request_id` field
- [ ] `request_id` matches the `X-Correlation-ID` header
- [ ] CLI shows request_id on error: `"Failed to deploy. Request ID: corr-xxxx"`
- [ ] Tests: error response contains request_id

---

## Phase 5 — Authorization & Identity Cleanup

### 5.1 Authorization Matrix — Role Enforcement

**Problem:** `tenant_members.role` exists but is not enforced. Any member can do anything.

**Fix:**
- Define permission matrix:
  - `owner`: all operations (deploy, delete, manage members, billing)
  - `admin`: deploy, manage secrets, rollback (no billing, no member management)
  - `member`: deploy, view logs, view status (no delete, no secrets, no rollback)
- Add `RequireRole(minRole)` middleware that checks role from UserContext.
- Apply to routes: billing = owner, project-delete = owner/admin, deploy = all.

**Files:**
- New: `cloud/internal/auth/roles.go` — role hierarchy + RequireRole middleware
- Edit: `cloud/cmd/main.go` — apply RequireRole to sensitive routes
- Same pattern for engine API: `api/internal/middleware/roles.go`

**DoD:**
- [ ] `RequireRole("owner")` middleware exists and works
- [ ] Billing routes require owner role
- [ ] Project delete requires admin or owner
- [ ] Deploy requires any role (member+)
- [ ] Wrong role returns 403 with `"message": "Requires owner role"`
- [ ] Tests: member cannot access billing, admin cannot manage members

---

### 5.2 Identity Cleanup — Remove user_identity_map Duplication

**Problem:** `user_identity_map` table coexists with `users.id` being the Kratos UUID.
Two sources of truth for identity linkage.

**Fix:**
- Since `users.id` IS the Kratos UUID for new users, `user_identity_map` is only needed
  for legacy GitHub-auth users that were migrated.
- Add migration that merges any remaining mappings into `users.id`.
- Drop `user_identity_map` table.
- If lazy migration is still needed, keep it but document clearly as temporary.

**Decision needed:** Is there any code that still reads `user_identity_map`?

**Files:**
- New: `api/internal/db/migrations/000022_cleanup_identity_map.up.sql`
- Edit: any code referencing `user_identity_map`

**DoD:**
- [ ] `user_identity_map` table dropped OR clearly documented as deprecated with removal date
- [ ] `users.id` is the single source of truth for user identity
- [ ] No code reads `user_identity_map` for auth decisions
- [ ] Tests pass

---

## Execution Order

```
Phase 1 (Billing) ──────────── ~2 days
  1.1 Single source of truth for plan
  1.2 Webhook error handling
  1.3 Subscription state mapping
  1.4 Checkout atomicity
  1.5 plan_limits NULL
  1.6 Drift reconciler

Phase 2 (Auth Hardening) ──── ~1.5 days
  2.1 RFC 8628 compliance
  2.2 Secret rotation (kid + dual-key)
  2.3 Rate limiting
  2.4 Schema constraints

Phase 3 (Frontend & UX) ───── ~1.5 days
  3.1 CSP headers
  3.2 CORS middleware
  3.3 Tab coordination
  3.4 Billing UX post-checkout
  3.5 CLI login output
  3.6 Session expired UX

Phase 4 (Observability) ───── ~1 day
  4.1 Auth metrics
  4.2 Billing metrics
  4.3 Error response request_id

Phase 5 (Authorization) ───── ~1 day
  5.1 Role enforcement
  5.2 Identity cleanup

Total: ~7 days
```

**Dependencies:**
- 1.1 (plan source of truth) must land before 1.3 (subscription state mapping)
- 2.4 (schema constraints) must land after 1.1 (plan column removed from tenants)
- 3.4 (billing UX) benefits from 1.2 (webhook reliability) landing first
- 4.1-4.2 (metrics) can run in parallel with everything

**Test strategy:** Each task includes its own DoD tests. No task is done without tests.
Integration tests requiring real PostgreSQL use the existing `api/tests/integration/` setup.

---

## Items NOT in This Plan (Intentionally Deferred)

| Item | Reason | When |
|------|--------|------|
| PATs / service accounts / machine tokens | Requires new auth flow design | Sprint after GA launch |
| Refresh token encryption at rest (keychain) | Platform-specific (macOS/Linux/Windows) | Sprint after GA launch |
| Subscription history / audit trail | Nice-to-have, not blocking | Post-launch |
| Routing tests for Traefik | Infrastructure test, separate concern | Infra hardening sprint |
| Background jobs as K8s CronJob | Architecture decision, not a bug | Scale sprint |
| Token revocation (jti + denylist) | Requires Redis or similar. 15min TTL is acceptable for now | Post-launch |
| Auth subdomain separation | Infrastructure change, not code | Scale sprint |

These are tracked but do not block Vercel-level quality for the current feature set.
