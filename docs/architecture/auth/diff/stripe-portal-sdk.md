---
name: stripe-portal-sdk
description: PROPOSED — POST /api/v1/billing/portal returns real Stripe Customer Portal URL via SDK call (replaces hardcoded placeholder). T2.7 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap.
type: decision
created_at: 2026-05-27
updated_at: 2026-05-27
updated_by: plan T1.5 + T2.7 design
---

<!-- PROPOSED STATE — depends on operator T2.7 HARD STOP (Sponsor + Stripe prod key) -->

# Stripe Portal SDK Integration — Auth/Billing Diff

> **Plan reference:** T2.7 + T1.5 of `docs/plans/2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md`
>
> **Depends on:** Sponsor review for Stripe production key usage (T2 — API contract change, billing flow).

## AS-IS (Sprint 11 placeholder)

```
CLI: loopzero billing portal
   │
   ▼
API: POST /api/v1/billing/portal
   │
   ▼
Hardcoded response:
{
  "url": "https://dashboard.stripe.com",
  "message": "Open the Stripe dashboard to manage billing"
}
   │
   ▼
User opens Stripe global dashboard (NOT their portal)
   → must navigate manually to find their account
   → poor UX
```

**Code location:** `api/internal/routes/billing.go:180-215` (placeholder body); Stripe SDK call commented with "Sprint 11".

## PROPOSED (post-T2.7)

```
CLI: loopzero billing portal
   │
   ▼
API: POST /api/v1/billing/portal
   │
   ▼
Look up tenant's stripe_customer_id
   │
   ▼
Idempotency check: Redis cache key = customer-id + hourBucket
   │ (cache hit) → return cached URL
   │ (cache miss) → proceed
   │
   ▼
stripeClient.BillingPortalSession.New({
  Customer: stripeCustomerID,
  ReturnURL: "https://dashboard.loopzero.example/billing"
})
   │
   ▼
Returns: Stripe-issued Customer Portal URL (one-time, 5min expiry)
   │
   ▼
Cache URL in Redis (1h TTL, same idempotency key)
   │
   ▼
CLI opens browser to Stripe Portal scoped to customer
   → user sees their invoices, payment methods, subscription
```

## What changes

| Surface | AS-IS | PROPOSED |
|---------|-------|----------|
| Response URL | Hardcoded global Stripe dashboard | Customer-scoped Portal URL |
| Stripe SDK call | Commented placeholder | Live `BillingPortalSession.New` |
| Idempotency | None | Redis cache key `portal:<customer-id>:<hourBucket>` (1h TTL) |
| Error: no stripe_customer_id | Returns 200 with dashboard URL | Returns 400 `NO_STRIPE_CUSTOMER` |
| Error: Stripe API down | Returns 200 with stale URL | Returns 5xx with Retry-After |
| Cloud/billing webhook coupling | Existing webhook handler unchanged | Webhook still owned by cloud (idempotency via Stripe events) |

## Error handling matrix

| Scenario | HTTP code | Body |
|----------|-----------|------|
| Success | 200 | `{"url": "<stripe portal url>", "expires_at": "<ISO>"}` |
| Tenant on Free plan (no Stripe customer) | 400 | `{"code": "NO_STRIPE_CUSTOMER", "message": "Upgrade to Pro plan to access billing portal"}` |
| Stripe API timeout | 503 | `{"code": "BILLING_SERVICE_UNAVAILABLE", "message": "...", "retry_after": 30}` |
| Idempotency collision (same customer-id within 1h, different params) | 422 | `{"code": "IDEMPOTENCY_MISMATCH"}` |
| Auth missing | 401 | `{"code": "UNAUTHORIZED"}` |

## Stripe SDK pinning (per EC-13 of edge-case-review)

```go
// cloud/internal/billing/stripe.go
import "github.com/stripe/stripe-go/v82"  // pin to v82 — v83 has breaking BillingPortal API
```

Renovate config: pin `github.com/stripe/stripe-go` to v82.x; major bump requires manual ADR.

## Operator HARD STOP

1. Sponsor reviews + approves Stripe prod key usage
2. Operator obtains Stripe prod restricted API key (subscriptions:read + billing_portal:write scopes)
3. Operator sets ESO ExternalSecret with `STRIPE_PROD_API_KEY`
4. Engineering implements per T2.7 plan tasks (4h)
5. Integration test runs against Stripe test mode (separate test key)
6. Soak test 24h on staging (verify no API rate-limit hits)
7. Flip to prod (Helm value `stripe.environment: production`)

## Related

- `api/internal/routes/billing.go` (placeholder location)
- `cloud/internal/billing/stripe.go` (existing Stripe client — extend with BillingPortalSession.New)
- `cloud/internal/billing/webhook.go` (webhook idempotency reference)
- Stripe SDK docs: https://docs.stripe.com/api/customer_portal
- MEET-2026-05-12 D4 (Pro plan = customer-portal feature target)
