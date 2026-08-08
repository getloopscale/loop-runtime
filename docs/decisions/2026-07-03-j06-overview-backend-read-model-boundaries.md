---
type: decision
title: "J-06 Overview backend: read-model boundaries + price source of truth"
---

# ADR — J-06 Overview backend: read-model boundaries + price source of truth

- **Date:** 2026-07-03
- **Status:** ACCEPTED
- **Tier:** T2 (billing + api-boundaries)
- **Meeting:** [MEET-2026-07-03-003](../meetings/records/MEET-2026-07-03-003.md)
- **Milestone:** M1 — Project Lifecycle (J-06 Overview backend enablers)

## Context

The dashboard Overview home (plan J-06) needs a "plan price this cycle" money KPI
and a cross-project deploys aggregate (7d count + 30d by-day sparkline). Neither
had a backend source. Two design questions carried a real architectural
divergence and a money-correctness trap, so they went through a T2 meeting.

## Decision 1 — Price is materialized from Stripe by the webhook; the read observes

- The Stripe **Price object is the source of truth** for the plan amount. A Go
  `plan→amount` constant is **rejected** — it recreates the #157 divergence (a
  hardcoded Pro price shipped wrong at $19 vs canonical $29). A **live Stripe call
  on the `/v1/billing` read path is rejected** — `/v1/billing` is the busiest
  authenticated endpoint (Overview home); a synchronous Stripe call there adds
  latency, a new failure domain, and consumes the Stripe rate-limit budget that
  the revenue path (`/v1/billing/checkout`) needs.
- **Chosen:** the existing Stripe webhook (loopzero-cloud `internal/billing/webhook.go`)
  — which today parses only `Price.ID` — is **extended** to also parse
  `price.unit_amount` + `currency` on `customer.subscription.{created,updated}`
  and **persist** them onto the `subscriptions` row (new columns
  `amount_minor BIGINT NULL`, `currency TEXT NULL`, engine-owned migration).
  `GET /v1/billing` **observes** the row — zero live Stripe call, zero hardcode,
  zero drift; Stripe stays SoT via the reconciling controller.

### Naming honesty (Unbreakable Rule 3)

`price.unit_amount` is the plan's **recurring list price**, not true metered spend
(it ignores coupons, tax, proration, metered add-ons). Labeling it "Spend MTD"
would be dishonest. The field is named **`plan_price`** and the dashboard KPI is
labeled **"Plan"** ("$29/mo"), not "Spend". If true billed spend is ever needed,
the honest SoT is `invoice.paid.amount_paid` — a future ADR, not this one.

- **Shape (additive, nested value object):**
  ```jsonc
  "plan_price": { "amount_minor": 2900, "currency": "usd", "interval": "month" }
  ```
  `starter` → `amount_minor: 0` (a known $0, present). `enterprise` / unknown /
  not-yet-persisted (NULL column) → **field OMITTED** (the handler's existing
  omit-when-absent idiom — never a fabricated `$0` for a paid plan).

## Decision 2 — Server-side single-resource aggregation is a resource query, not a BFF-leak

**This is the divergence that required this ADR** (governance rule 5: architect vs
distributed-systems divergence → mandatory ADR).

- The purist "generic resource API, client aggregates" position would ship a raw
  cross-project `GET /api/v1/builds?range=30d` list and let the client `GROUP BY`.
  **Rejected:** it creates a new unbounded collection surface, transfers up to N
  rows per Overview load (worst for the most active = most valuable tenants), and
  forces every client to re-implement day-bucketing + deploy-status semantics —
  which then **drift across clients** exactly like the hardcoded price did.
- A screen-named endpoint (`/api/v1/overview/deploy-stats`) is **also rejected**:
  naming an engine endpoint after a dashboard screen couples the resource API to a
  view and rots the URL namespace into a view catalog.
- **Chosen:** `GET /api/v1/builds/stats?window={7d,30d,90d}` — a **domain-named,
  resource-scoped, server-side aggregation**. `builds` is the resource; `stats` is
  a named projection. Aggregation (UTC day-bucketing, zero-fill, the definition of
  a "deploy") lives **once, server-side**, so it cannot drift. Cross-resource
  *screen* composition (KPI cards + chart + recent list) stays in **loopzero-cloud**
  (the BFF that already fronts `/engine/*`) — the engine never grows an `/overview`.

**Principle (the ADR's durable rule):** *A time-bucketed aggregation of a single
resource is a resource query and belongs in the engine API. Cross-resource
composition for a screen belongs in the BFF (loopzero-cloud). The engine API is named
by domain resources, never by screens.*

- **Shape:**
  ```jsonc
  {
    "window_days": 30, "timezone": "UTC",
    "by_day": [ { "date": "2026-06-04", "count": 0 }, ... ],  // dense, zero-filled, ascending, exactly window_days entries
    "last_7d_count": 12,   // derived from the SAME query — can never disagree with by_day
    "total_count": 47
  }
  ```

## Binding conditions (from the meeting — MUST hold to ship)

**Security / tenant isolation (the #106/#111 class — RLS is NOT a backstop; engine pool is BYPASSRLS):**
1. `/api/v1/builds/stats` carries an explicit `WHERE tenant_id = $1` bound to
   `middleware.GetUserContext(ctx).TenantID` — first predicate, always present.
   Authored in `api/internal/db/queries/builds.sql` via sqlc, **NOT**
   `tenantpredicate:exempt` (the CI gate then enforces it).
2. **Fail-closed** on nil/empty `TenantID` → 401; never run the aggregate unscoped
   (do NOT inherit the rate-limiter's fail-open behavior).
3. `window` is a **closed enum** mapped server-side to a **bound** interval param
   (never string-interpolated into SQL); non-enumerated → typed 400.
4. Response leaks no `price_id`, no `stripe_customer_id`, no secret — keep the
   explicit allow-list map in `GetBilling`; do not spread the subscription row.

**Money correctness:**
5. Amount captured at webhook write time **after** signature verification; stored
   `(amount_minor int, currency text)`, currency normalized lowercase.
6. `plan_price` OMITTED when the column is NULL (pre-migration rows / not-yet-persisted),
   not just for enterprise — every existing subscriber must not see a broken `$0`.
7. Spend derives from the JWT tenant's **account** subscription
   (`account_id = (SELECT account_id FROM tenants WHERE id = $jwt)`), never a
   client-supplied id, never a shared cache (per the M2 account model).

**Cross-repo seam:**
8. The `subscriptions` migration is **engine-owned** (`loopzero/api/internal/db/migrations/`).
   `GET /v1/billing` must **omit** `plan_price` (not 500) when the columns don't
   yet exist / are NULL — protecting the window where one repo is ahead of the other.

## Consequences

- New engine migration (`subscriptions.amount_minor`, `.currency`) + a new sqlc
  `BuildStatsByDay` query (this is new-feature schema/sqlc work — the M9-refactor
  MA-6/MA-8 "no-regen/no-schema" anchors were scoped to that behavior-preserving
  refactor, not to new M1 features).
- New engine route `GET /api/v1/builds/stats`; additive `plan_price` on `/v1/billing`.
- loopzero-cloud webhook extended to persist the amount; frontend `useOverviewKPIs`
  aggregator + KPI strip labels "Plan", not "Spend".

## Alternatives rejected

| Option | Why rejected |
|---|---|
| Go `plan→amount` constant | Recreates #157 drift; a second pricing SoT. |
| Live Stripe call on `/v1/billing` read | Latency + failure domain + rate-limit starvation on the busiest read. |
| Stripe upcoming-invoice `amount_due` | Proration is false precision on a flat-fee plan; live call on hot path. |
| Raw `/api/v1/builds?range=30d` + client aggregation | Unbounded payload; bucketing/status drift across clients. |
| `/api/v1/overview/deploy-stats` (screen name) | Couples the engine API to a dashboard screen; namespace rot. |
| Field named `current_spend` / "Spend MTD" | Dishonest — it is the list price, not billed spend (Rule 3). |
