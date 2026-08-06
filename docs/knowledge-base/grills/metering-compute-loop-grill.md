---
type: grill
title: "Grill: metering-compute-loop"
slug: metering-compute-loop
date: 2026-07-01
questions_asked: 9
decisions_resolved: 9
verdict: READY_FOR_PLAN
prior_art: cross-validation-output/gap_detection/gaps.md (loopzero vs sealos)
cross_repo: loopzero + loopzero-contracts
---

# Grill: metering-compute-loop

Add a periodic per-tenant-namespace compute-resource metering loop (CPU/mem, allocation-based)
that populates loopzero's `usage_records` + `cost_counters` tables, enforces per-plan compute caps
(402), and prices usage via a data-driven model. Derived from the cross-validation vs sealos.

## Codebase-first findings (grounding — verified, not assumed)

1. **The metering tables are inert.** `usage_records` (mig 000013→000033) + `cost_counters`
   (mig 000017) EXIST but have **zero writers** — `cost.sql` has only `GetActiveCostCounter :one`
   (read); no `INSERT INTO cost_counters/usage_records` anywhere; nothing sets `reported_to_stripe`.
   The 402 cost-cap enforcement (`pkg/enforcement/cost_cap.go:73` `CostStore`/`CapChecker`) reads
   counters that stay 0 → cap never fires. Built (plan T1.9 launch-readiness) but never wired to a producer.
2. **`api/internal/aggregator/` is NOT billing** — it's the deploy-timeline aggregator
   (ArgoCD/Loki/LoopZeroApp informer → SSE). The "aggregator" referenced in mig 000017's comment does not exist.
3. **The API already has every primitive the loop needs:** leader election
   (`api/internal/leader/election.go`), periodic ticker pattern (`placement/collector.go:49`,
   `sse/event_pruner.go`), cross-cell pod access (`cellmgr/client_manager.go:50`
   `ClientFor(ctx,cellID)→kubernetes.Interface`), cross-cell fan-out
   (`services/cross_cell_aggregator.go:68` `QueryAllCellsParallel` — the loopzero equivalent of sealos
   `processNamespaceList`).
4. **Cost vocabulary + cap VALUES live in `loopzero-contracts/plan/plan.go`** (decomposed repo):
   `type CostMetric string`, constants `MetricEgressBytes/DOCRPulls/BuildMinutes`, `planCaps` map
   (Starter 100GB/1k/100min → Pro 10× → Team 10× → Enterprise `Unlimited=-1`), `CapFor(plan,metric)`.
   Adding compute caps is a **cross-repo change** governed by the decomposition ADR (`loopzero → loopzero-contracts`, acyclic).
5. **loopzero already has replica caps** (Starter=2/Pro=5/Team=10) + `ComputeMonthlyCost(replicas, podCostPerHour)`
   (`pkg/enforcement/scale.go:135`) used as a UI estimate only.

## Decision tree resolved

1. **Scope / ambition** → FULL: meter compute + enforce compute 402 caps (cross-repo loopzero-contracts)
   + data-driven pricing. (Stripe usage-based reporting still deferred — `reported_to_stripe` bridge is
   trivial once the producer exists + is validated.) *[user override of the recommended enforcement-only scope]*
2. **Where the loop runs** → API-side, leader-elected periodic worker. Reuses `leader.Election` +
   `CrossCellAggregator.QueryAllCellsParallel` + `cellmgr.ClientFor` + `placement/collector.go` ticker
   pattern. NOT an operator (would need billing-PG write access the API owns) and NOT a new binary (YAGNI).
3. **Metric basis + resources** → allocation-based (`pod.Spec.Resources.Requests`, not metrics-server —
   no new dependency, predictable billing). CPU+mem first (`cpu_core_seconds`, `mem_gb_seconds`);
   storage (PVC) as fast-follow.
4. **Enforcement + pricing in scope** → YES (both). New `CostMetric` constants + per-plan cap values in
   loopzero-contracts + 402 wire; data-driven pricing model.
5. **Compute cap VALUES** → derive from existing replica caps (`replica_cap × cpu_request_envelope ×
   period_seconds`) for consistency with the plan model; Enterprise=`Unlimited`. Final numbers marked
   **[SPONSOR SIGN-OFF PENDING]** (same pattern as roadmap "T0.2 sponsor sign-off"). cpu+mem capped first;
   storage cap with storage metering.
6. **Pricing model location** → DB-backed table in **loopzero** (NOT loopzero-contracts). Caps=contract (stable,
   loopzero-contracts); prices=mutable business data (repriceable via SQL, no deploy) — the genuine
   data-driven fix vs "hardcoded in Go". Include `PriceType` (AVG/SUM/DIF) per metric (sealos
   aggregation-semantics-as-data). `ComputeMonthlyCost` becomes a lookup against the pricing table,
   preserving its server-authoritative signature for the scale UI.
7. **Ticker cadence + accumulation** → 60s (configurable) + accumulate by **elapsed time since
   `last_metered_at`** (not fixed-interval): `value += Σ(pod cpu_request) × (now − last_metered_at)`,
   advancing `last_metered_at` only after a successful write → at-least-once safe, self-corrects missed/
   duplicated ticks (more robust than sealos's fixed-interval assumption). UPDATE `cost_counters` in-place;
   finalize a `usage_records` row per period at rollover. Cadence is a granularity/load knob, not a
   correctness factor.
8. **Tenant enumeration + skip set** → PG-driven: `tenants WHERE status=ACTIVE` → join `cell_id` →
   derive namespace via `naming` → read pods via `cellmgr`. Skip SUSPENDED + PENDING_DELETION/DELETING.
   loopzero-idiomatic `filterNormalNamespace` (PG SoT, not k8s-annotation), and the suspension gate respects
   the billing lifecycle (don't meter a non-paying suspended tenant).
9. **Cross-repo sequencing + attribution granularity** → (a) loopzero-contracts change lands + is
   semver-tagged FIRST, then loopzero bumps `go.mod`/`go.work` to consume the new `CostMetric` + `CapFor`
   values (acyclic decomposition order). (b) Per-tenant totals only this feature — enforcement (per
   `tenant,metric,period`) and pricing ($/mo per tenant) don't need per-app attribution; sealos's
   hierarchical `Category/ParentType/ParentName` attribution deferred to a future cost-breakdown UI (YAGNI).

## Q&A log

### Q1: Scope / ambition (root)
**Recommended**: stop at metering-for-enforcement; defer Stripe reporting.
**User decision**: accepted (defer Stripe).

### Q2: Where the loop runs
**Recommended**: API-side leader-elected periodic worker reusing cross_cell_aggregator + cellmgr + ticker.
**User decision**: accepted.

### Q3: Metric basis + resources
**Recommended**: allocation-based; CPU+mem first; storage fast-follow.
**User decision**: accepted.

### Q4: Enforcement + pricing in scope?
**Recommended**: meter-only this feature; defer compute-cap enforcement + pricing.
**User decision**: OVERRIDE — include compute-cap enforcement (new CostMetric + cap values in
loopzero-contracts + 402 wire) AND the data-driven pricing model.

### Q5: Compute cap VALUES
**Recommended**: derive from replica caps + [SPONSOR SIGN-OFF PENDING]; cpu+mem caps first.
**User decision**: accepted.

### Q6: Pricing model location
**Recommended**: DB-backed table in loopzero (not contracts), PriceType AVG/SUM/DIF, replaces ComputeMonthlyCost via lookup.
**User decision**: accepted.

### Q7: Ticker cadence + accumulation
**Recommended**: 60s configurable + elapsed-since-last_metered_at accumulation (robust to crash/missed ticks).
**User decision**: accepted.

### Q8: Tenant enumeration + skip set
**Recommended**: PG-driven (status=ACTIVE), skip SUSPENDED/PENDING_DELETION/DELETING, pods via cellmgr.
**User decision**: accepted.

### Q9: Cross-repo sequencing + attribution granularity
**Recommended**: loopzero-contracts first (tag) then loopzero bump; per-tenant totals only (defer hierarchical attribution).
**User decision**: accepted.

## Open items to carry into /to-plan

- **[SPONSOR SIGN-OFF PENDING]** the concrete compute cap numbers per plan (cpu_core_seconds, mem_gb_seconds).
- **Cross-repo release**: loopzero-contracts PR (new CostMetric + CapFor rows) must tag before loopzero consumes.
- **Migration**: extend `cost_counters.metric` CHECK constraint to allow the new compute metrics; add a
  `last_metered_at` column (or small state table) keyed by (tenant, metric).
- **New pricing table** (loopzero): `{metric, price_type, unit_price, unit_string}` + a lookup service.
- **Governance**: this is a T2+ change (DB schema + shared pkg/contract + billing) — per
  `.claude/rules/pre-release-governance.md` a technical meeting is required before implementation.
