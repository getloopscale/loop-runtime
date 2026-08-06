---
type: plan
title: "Plan: Compute-Resource Metering PRODUCER (observe-only) — v1.10"
slug: metering-compute-loop
created_at: 2026-07-01
updated_at: 2026-07-01
version: 1.10
edge_cases: knowledge-base/reviews/metering-compute-loop-edge-cases-2026-07-01.md (EC-1..EC-11 absorbed)
goal: Ship a leader-elected, transactionally-correct compute-metering PRODUCER (observe-only) that writes exact cpu_core_seconds/mem_gb_seconds into each ACTIVE tenant's cost_counters + usage_records across all its project/env namespaces per cell.
cross_repo: loopzero + loopzero-contracts
grill: knowledge-base/grills/metering-compute-loop-grill.md
prior_art: cross-validation-output/gap_detection/gaps.md
meetings: [docs/meetings/records/MEET-2026-07-01-001.md (v1.0 DEFERRED), docs/meetings/records/MEET-2026-07-01-002.md (v1.1 APPROVED_WITH_CONDITIONS), docs/meetings/records/MEET-2026-07-01-003.md (data-source KEEP ADR-2 + tenant-guardian ✅), docs/meetings/records/MEET-2026-07-01-004.md (state-guardian SIGN_OFF on v1.6), docs/meetings/records/MEET-2026-07-01-005.md (tenant-guardian SIGN_OFF on v1.9), docs/meetings/records/MEET-2026-07-01-006.md (4-agent unanimous SC-7 split-clamp — APPROVED_WITH_CONDITIONS, folded v1.10)]
governance_tier: T2+ — vetoes lifted on v1.1; v1.2 folds the 21 MEET-002 conditions; v1.6 folds the 6 MEET-004 conditions; v1.9 folds the 3 MEET-005 conditions; v1.10 folds SC-7 (MEET-006, split total-clamp — surfaced + PG-REAL validated during /implement T2.3). BOTH guardians re-signed (tenant MEET-005 ✅ + state MEET-004 ✅); SC-7 guardian re-sign addendum pending on v1.10
scope: PRODUCER only. Enforcement (402) → follow-up metering-compute-enforcement. Pricing → follow-up metering-compute-pricing.
---

# Plan: Compute-Resource Metering PRODUCER (observe-only) — v1.10

> **v1.10** — folds **SC-7** from **MEET-2026-07-01-006** (4-agent unanimous: state-guardian, tenant-guardian, distributed-systems, verification-architect). Surfaced during `/implement` T2.3 when the money-writer query layer was PG-REAL validated: `MeterComputeSplit` (ADR-9) billed the raw `now()-expected` gap — the `head` was NOT clamped by `max_delta`, unlike `MeterCompute`'s `elapsed`. Two bugs: (1) **unbounded over-bill** on a leader resurrection whose resumption tick straddles the month boundary (the most common failover window); (2) **multi-boundary misattribution** — a ≥2-month outage keyed `head` into `period_of(now())` and never opened the middle-period rows. This directly contradicted ADR-9's own invariant (`tail+head==elapsed`, where `elapsed` is the CLAMPED quantity) and ADR-8's `k×interval < one period` mandate. **Fix (Option 1, anchor-at-expected):** clamp the TOTAL to `max_delta`, split the clamped window `[expected, LEAST(now(), expected+max_delta)]` at the boundary; P2 keyed off `metering_period_end(expected)` (NOT `now()`). Encapsulated as `metering_split_tail`/`metering_split_head` STABLE SQL fns (000048) — DRY + sqlc-parseable. `MeterCompute`/`MeterComputeSplit` became `:one` returning the clamped delta (option b) so the wrapper threads the SAME bounded quantity into `usage_records`. **PG-REAL validated:** same-period `delta=5` + HELD-unchanged; split `delta=10==metering_cost(500,max_delta)`, `P1=5@prior`/`P2=5@current`, SUM bounded, cursor→now. The same-period `MeterCompute` path (MEET-004) is unchanged. No new billing scope; still observe-only.
>
> **v1.9** — folds the 3 tenant-guardian conditions from the v1.8 ADR-5 re-review (which SIGN_OFF-preserved INV-4/INV-5 attribution but WITHHELD on a cell-resolution bug the v1.8 revision itself introduced). **C1 (BLOCKER, INV I2):** v1.8 deleted the cell-bearing query, forcing cell from the mutable CR `spec.cellID` mirror — but `tenants.cell_id` (PG) is the canonical placement SoT; a producer reading the mirror on divergence lists pods on the wrong cell → silent under-bill + double-cursor drift (Risk #6). Fixed: `GetCellForTenant` (PG canonical) added; ADR-5/T3.1 resolve cell from PG, never the CR. **C2 (HIGH):** `TenantLister` scoped to canonical `loopzero-system` + filters the observed `status.phase`. **C3 (LOW):** drain-window under-count documented in § Drawbacks. The CR list now supplies ACTIVE-filter + tenant_id ONLY; cell + plan_tier come from PG. tenant-guardian re-sign pending on v1.9.
>
> **v1.8** — FOUNDATIONAL FIX caught by `/implement` iter 6 before writing the money-writer ([`BLOCKER-adr5-tenant-status-source.md`](../implementations/metering-compute-loop/BLOCKER-adr5-tenant-status-source.md)): ADR-5 / T2.3 `ListActiveTenantsWithCell` / T3.1 / T1.3 / MEET-002's `'Active'`-literal fix all assumed `SELECT ... FROM tenants WHERE status='Active'`, but **`tenants` has no status column** — tenant lifecycle status is the Tenant CR `status.phase` (K8s), not PG. **Fix (enumeration source only; the money-writer CTEs are UNCHANGED):** ADR-5 enumerates active tenants from the Tenant CR phase via the API's `tenant_cr_manager.go`; `ListActiveTenantsWithCell` removed (→ `GetPlanTierForTenant` for plan_tier only); T3.1 lists+filters CRs via a `TenantLister` (DIP); T1.3 becomes a phase-filter-constant guard. tenant-guardian re-review required (enumeration is their domain). The `Active` literal was right; the SOURCE was the defect (Rule 12 — grep the schema, not memory).
>
> **v1.7** — pre-`/implement` correction caught by the SEPA initial brief (2026-07-01): the plan said "tag/consume **v0.2.0**", but v0.2.0 is ALREADY a released loopzero-contracts tag (M3 account_id, 2026-06-27). Corrected to **v0.3.0** (next free additive minor) throughout. The v0.1.0→v0.3.0 bump crosses v0.2.0's breaking `SessionTokenIssuer.Issue` 5th-param; T1.1 expanded to adapt the only 3 affected call-sites (all in `jwt_validator_test.go` — no production caller). Mechanical version fix + a 3-line test fan-out; no architectural change, no re-sign needed. Demonstrates the pre-RED SEPA gate: a burned duplicate tag avoided before any code.
>
> **v1.6** — folds the 6 conditions from **MEET-2026-07-01-004** (state-guardian re-sign WITHHOLD; the state-machine half of the `/implement` guardian gate, symmetric to MEET-003's tenant-guardian half). Two were BLOCKERs on the `metering_cursor` state semantics: **SC-1** — the ADR-4/ADR-9 CTE now carries a monotonicity guard (`now() > last_metered_at` + `GREATEST(elapsed, 0)`) so a backward clock can never decrement a counter or rewind the cursor; **SC-2** — the split-vs-same-period dispatch is now governed by a DB-`now()` period predicate (`=` in ADR-4, `<>` in ADR-9) instead of a Go clock, so a wrong dispatch guess at the period boundary is a 0-row HOLD, never a mis-bill. Plus SC-3 (read-failure HOLDS the cursor, explicit), SC-4 (post-split continuation test), SC-5 (cursor seed-if-absent + GC in the tenant reaper — one line, observe-only), SC-6 (fence single-source-bound). ADR-4/ADR-9 SQL re-spelled at matching fidelity; ADR-13 (NEW) + 6 Coverage rows (17-22). No new billing scope. (v1.4/v1.5 were risk-doc refinements — bottleneck analysis + AWS RDS/RDS Proxy — folded into § Drawbacks & Risks.)
>
> **v1.3** — absorbs the 11 edge cases (EC-1..EC-11) from `/edge-case-plan` ([`knowledge-base/reviews/metering-compute-loop-edge-cases-2026-07-01.md`](../reviews/metering-compute-loop-edge-cases-2026-07-01.md)) — all allocation-model correctness edges the two meetings did not cover (they attacked the write path; these live in the Kubernetes pod-request math): **EC-1** KEDA scale-to-zero cursor advance, **EC-2** pod-phase filter, **EC-3** init/sidecar container math, **EC-4** plan_tier source, + EC-5..EC-11 as tests/notes. None required architectural rework. See § Edge cases absorbed at the foot.
>
> **v1.2** — folds all 21 conditions from MEET-2026-07-01-002 (which lifted the 3 vetoes but WITHHELD guardian sign-off on two real bugs). Changes vs v1.1: (a) **ADR-4** rewritten as a two-table data-modifying CTE (`metering_cursor` CAS+fence → `cost_counters` upsert → `usage_records` upsert, one tx); (b) **ADR-9 (NEW)** period-rollover split + upsert-per-period; (c) **namespace enumeration fixed** — PG-derived `naming.Namespace(tenant,project,env)` across ALL a tenant's project/env namespaces (v1.1 metered the wrong single `loopzero-{tenant}` ns → systematic under-billing); (d) **state literal fixed** — `status='Active'` (canonical `TenantActive`, mixed-case; v1.1 used `'ACTIVE'` → zero rows → fleet metering loss) + drift guard; (e) **ADR-10 (NEW)** dedicated bounded pgxpool for the worker (pool isolation) + 1,500-tenant load test as gated DoD; (f) attribution negative tests + PG-REAL test mandate + negative RLS twin + panic-recovery/single-flight + kill-switch-via-DB-row + multi-cell LIVE; (g) **pricing (old Phase 4) carved out** to a follow-up to shrink the LIVE diff. Observe-only: new compute metrics default `Unlimited` (no 402 possible).

## Goal

> Enable LoopZero to observe reserved compute per tenant by shipping a leader-elected metering producer that writes **exact** `cpu_core_seconds` into each ACTIVE tenant's `cost_counters` + `usage_records` summed across **all its project/env namespaces on a cell**, measured by `TestMeteringLoop_ExactCPUAcrossMultipleNamespaces` asserting `cpu_core_seconds == Σ(pod cpu_request) × interval_seconds` (a 2-project tenant, one 500m pod each, over one injected interval, single `(tenant,metric,cell)` cursor advancing once) to float epsilon, **against real PostgreSQL**.

Observe-only: no 402 wiring; new compute `CostMetric`s default `Unlimited`. Enforcement + pricing are hard-gated follow-ups.

## Context

Cross-validation vs sealos: Metering & Billing 3.5/5 (HIGH: no compute loop; MEDIUM: hardcoded pricing). Tables exist with zero writers. v1.0 (meter+enforce+price, non-atomic) DEFERRED; v1.1 (observe-only, atomic) lifted 3 vetoes but guardians found two shipping-bugs (M2 namespace enumeration; `'ACTIVE'` literal). v1.2 folds all conditions. Full forensic in the two meeting records.

## Baseline Context (deep review of current state)

### Files that will be touched

| File | LoC / status | Last commit | Why it exists | Invariants to preserve |
|---|---|---|---|---|
| `loopzero-contracts/plan/plan.go` | 130 | `73d3fad` (2026-06-16) | `CostMetric`, `planCaps`, `CapFor`, `Unlimited=-1` | existing metrics/caps unchanged; new compute metrics default `Unlimited` |
| `pkg/enforcement/cost_cap.go` | 137 | `b5a40a09` (2026-06-16) | `CostStore`/`CapChecker`; aliases | read interface stable |
| `pkg/statemachine/tenant.go` | — | — | `TenantActive = "Active"` (mixed-case) + PROVISIONING/SUSPENDED/PENDING_DELETION/DELETING | **enumerate by `"Active"`, NOT `"ACTIVE"`** |
| `pkg/naming/naming.go` | — | — | `Namespace(tenant,project,env)→loopzero-{t}-{p}-{e}` (:102); `TenantNamespace`→single (:90, WRONG for M2); `StandardLabels` stamps mutable `loopzero.io/tenant` (:239) | derive ns via `Namespace`, never trust the pod label |
| `api/internal/db/queries/projects.sql` | — | — | `ListProjectsByTenant :many` (:32) | PG source for a tenant's projects |
| `api/internal/db/queries/cost.sql` | 29 | `393fa9f4` (2026-05-29) | only `GetActiveCostCounter :one` | existing read stable |
| `api/internal/db/migrations/000017_cost_counters.up.sql` | 58 | `a6e8b895` (2026-05-12) | `cost_counters` (BIGINT `value`, PK `(tenant,metric,period_start)`, RLS policy) | RLS policy; upsert on the PK |
| `api/internal/db/migrations/000023_loopzero_worker_role.up.sql` | — | — | `loopzero_worker` non-owner; RLS APPLIES; cross-tenant scans MUST `SET LOCAL app.tenant_id` NULL/'' (:127-142); GRANTs cost_counters/usage_records | worker = `loopzero_worker` |
| `api/internal/db/migrations/000001_initial_schema.up.sql` | — | — | `usage_records{quantity INTEGER, period, reported_to_stripe}` — no RLS | quantity INTEGER → BIGINT |
| `api/internal/leader/generation.go` | — | — | `Lease.GenerationID int64` fencing (:63-69); contract: pass into every side-effecting WHERE (:31-36); `ErrFenced` | fencing consumed by the write |
| `api/internal/admission/pg_breaker.go` | — | — | `PGPoolBreaker.Allow() bool` (:127) | — (worker uses its OWN pool per ADR-10) |
| `api/internal/cellmgr/client_manager.go` | 80 | `1e547cc2` (2026-06-17) | `ClientFor(ctx,cellID)→kubernetes.Interface` (:50) via `spec.cellID` | read-only; attest read-scoped credential |
| `api/cmd/controller/main.go` | — | — | leader binary; background loops :146; fencing via `acquirer.Acquire` :130 | leader gate; `bgWg`/`shutdownCtx`; panic must not crash it |
| `api/internal/metering/` (NEW) | 0 | — | producer package | — |
| `migrations 000046, 000047` (NEW) | 0 | — | CHECK widen + `metering_cursor`; `usage_records` BIGINT | forward-only |

(The pricing table migration + `pkg/enforcement/pricing.go` + `ComputeMonthlyCost` refactor are MOVED to the follow-up `metering-compute-pricing`, which will take the next free migration number at that time.)

### Current callers / dependents
- `CapFor`: `billing.go`, `cost_cap.go` — additive.
- `CostStore`/`CostEnforcer`: untouched (no 402 in v1.2).
- `ComputeMonthlyCost`: untouched (pricing follow-up).

### Domain glossary
+ **PG-derived namespace enumeration** — a tenant's namespaces = `naming.Namespace(tenant, project, env)` for each (project, env) from PG (`ListProjectsByTenant` × the project's environments), NOT `TenantNamespace`. + **two-table CTE** — `metering_cursor` CAS/fence advance → `cost_counters`/`usage_records` upsert, one atomic statement/tx. + **dedicated worker pool** — a small `pgxpool` owned by the metering worker, physically separate from the API pool (ADR-10). + **period-rollover split** (ADR-9) — a straddling interval writes tail→P1, head→P2, same tx, cursor advances once.

### Architecture boundaries affected
- Worker (application) depends on interfaces: `MeteringStore`, cell `ClientProvider`, `Clock`, `leader.Lease` (DIP).
- loopzero→loopzero-contracts acyclic: metrics land + tag v0.3.0 (caps `Unlimited`) before loopzero bumps.
- Worker = `loopzero_worker` role → RLS APPLIES → `SET LOCAL app.tenant_id = ''`.
- Worker owns a **separate pgxpool** (ADR-10) — never the API pool.
- Rule 3: no internal terms (no user-facing output in observe-only).

## Prior Art & Related Work
- Internal: `cross-validation-output/gap_detection/gaps.md`; MEET-2026-07-01-001/002.
- Reference (read-only): sealos `monitor_controller.go:393/283/299/458`.
- LoopZero house patterns: `leader.Lease.GenerationID` fencing (`generation.go:31-36`), mig 000023 RLS-bypass contract, `cmd/controller/main.go:146` background wiring, `ListProjectsByTenant`.
- No `*-patterns` skill matches.

## ADRs

### ADR-1 — Producer in the leader-elected controller binary (unchanged).
### ADR-2 — Allocation-based (`pod.Spec.Requests`), not metrics-server (unchanged).
### ADR-3 — Compute caps in loopzero-contracts default `Unlimited`; observe-only (unchanged).

### ADR-4 (REVISED v1.6 — MEET-004 SC-1/SC-2/SC-6) — Two-table atomic data-modifying CTE (same-period writer)
**Decision:** the same-period meter advance is ONE `WITH` statement in ONE tx (after `SET LOCAL app.tenant_id = ''`). Every guard is IN the CTE (SQL, not Go), governed by the single DB `now()` that performs the advance:
```
WITH adv AS (
  UPDATE metering_cursor SET last_metered_at = now()
  WHERE tenant_id=$1 AND metric=$2 AND cell_id=$3
    AND last_metered_at = $expected                                     -- CAS: exactly-once (lost race → 0 rows → defer)
    AND now() > metering_cursor.last_metered_at                         -- SC-1 monotonicity: backward clock → 0 rows (cursor HELD, no rewind/decrement)
    AND period_of(now()) = period_of($expected)                         -- SC-2 same-period predicate: a straddle → 0 rows → dispatch to MeterComputeSplit next tick
    AND $gen = (SELECT generation_id FROM leader_generations
                WHERE name=$loop AND cell_id=$leaderCell)               -- SC-6 fence: $loop/$leaderCell single-source-bound to the SAME lease providing $gen
  RETURNING GREATEST(LEAST(now() - $expected, $maxDelta), interval '0') AS elapsed)  -- SC-1 lower-clamp (defense-in-depth): elapsed can never be negative
INSERT INTO cost_counters(tenant_id,metric,period_start,period_end,plan_tier,value)
  SELECT $1,$2, period_start($expected), period_end($expected), $plan, $reqMillicore * EXTRACT(EPOCH FROM elapsed)/1000 FROM adv  -- MEET-004 NOTE 1: labels SQL-computed from $expected (airtight-by-construction), not Go params
  ON CONFLICT (tenant_id,metric,period_start) DO UPDATE SET value = cost_counters.value + EXCLUDED.value, plan_tier = EXCLUDED.plan_tier;
```
+ a same-tx `UpsertUsageRecord`. **CAS** (0 rows → skip), **monotonicity-guarded** (SC-1 — no backward move can decrement or rewind), **same-period-predicated** (SC-2 — a straddle self-defers to the split writer; a wrong Go dispatch guess is a 0-row HOLD, never a mis-bill), **fenced** by the loop's OWN `Lease.GenerationID` (SC-6), delta + advance + period-decision all from the **same DB `now()`**, **upsert** so a new-period row is created.
**Rationale:** exactly-once-per-interval AND monotone; closes split-write / two-leader / **backward-clock decrement (SC-1)** / **dual-clock split misattribution (SC-2)** / NULL-bootstrap / new-period-row-missing. The v1.5 CTE had no lower bound (SC-1) and left the split decision to a Go clock (SC-2) — both money-corrupting at the audited period boundary (MEET-004).
**Rejected:** single-table UPDATE (can't span cursor+counter); 4 separate `:exec` (v1.0, non-atomic); Go-side monotonicity/period check (a second clock disagreeing with the advance `now()` — the SC-1/SC-2 bug).

### ADR-5 (REVISED v1.8 — MEET-BLOCKER: active-tenant source is the Tenant CR, not PG) — CR-phase enumeration + PG-derived namespaces + targeted cell
**Decision:** the active-tenant SET comes from the **Tenant CR `status.phase == Active`** (`loopzerov1.TenantPhaseActive`, `operators/api/v1/tenant_types.go`), read via the API's existing Tenant CR client (`api/internal/k8s/tenant_cr_manager.go`, scoped to the canonical `loopzero-system` namespace — ADR-0018) on the mgmt cluster — **NOT** a PG `tenants.status` column (which does not exist; the PG `tenants` table has no lifecycle-status column — verified against the real schema). The CR list supplies the **ACTIVE filter (observed `status.phase`, not `spec`) + `tenant_id` ONLY**. **`cell_id` is resolved from the canonical PG `tenants.cell_id` (`GetCellForTenant`), NEVER the CR `spec.cellID`** — per multi-cell INV I2, `tenants.cell_id` is the canonical placement SoT and the CR `spec.cellID` is a mirror that can diverge (partial pin / pre-reconcile window); a billing producer reading the mirror on divergence would list pods on the wrong cell → 0 pods → advance the cursor billing 0 → silent under-bill + double-cursor drift (Risk #6) [tenant-guardian C1]. For each active tenant: enumerate its (project, env) pairs from PG (`ListProjectsByTenant`) and derive namespaces via `naming.Namespace`; on the tenant's **PG-resolved** cell, list pods **per namespace**, sum into ONE `(tenant, metric, cell)` delta. Skip Provisioning/Suspended/PendingDeletion/Deleting phases.
**Rationale:** v1.1's `TenantNamespace` metered a single empty ns → systematic under-billing of multi-project tenants (tenant-guardian INV-4). Per-cell cursor keeps partial-cell failure lossless. **v1.7's `tenants WHERE status='Active'` PG query referenced a phantom column** — tenant lifecycle status is authoritative on the Tenant CR (managed by the tenant operator), never persisted as a PG `tenants` column. The `Active` LITERAL was right (`TenantPhaseActive = "Active"`); the SOURCE (PG vs CR) was the defect. See [`knowledge-base/implementations/metering-compute-loop/BLOCKER-adr5-tenant-status-source.md`](../implementations/metering-compute-loop/BLOCKER-adr5-tenant-status-source.md).
**Rejected:** `TenantNamespace` single-ns (wrong for M2); `subscriptions.status` as the active-source (that is BILLING status — a suspended tenant with an active subscription would be wrongly metered); label-selector across cells (trusts mutable label, cross-tenant leak); all-cells fan-out (v1.0); a NEW PG `tenants.status` column (duplicates the CR's authoritative phase → drift risk).

### ADR-6 — Data-source SoT: meter materialized pod.Spec (dynamic replicas) (unchanged).

### ADR-7 — Observe-only; enforcement + pricing decoupled to follow-ups (extended v1.2 to also carve pricing).

### ADR-8 — Gap direction: clamp `LEAST(elapsed, k×interval)` + metric; NULL-bootstrap = 0 + seed. **k×interval MUST be < one billing period** (else ADR-9 must handle N boundaries).

### ADR-9 (REVISED v1.10 — MEET-006 SC-7 total-clamp; v1.6 MEET-004 SC-2/SC-4) — Period-rollover split (spelled to ADR-4 fidelity)
**Decision:** the split writer is ONE `WITH` in ONE tx, dispatched only when the DB `now()` straddles the boundary `B = period_end($expected)`. **SC-7:** it clamps the TOTAL gap to `$max_delta` (symmetric with ADR-4's `elapsed` clamp) and splits the CLAMPED window `[$expected, LEAST(now(), $expected+$max_delta)]` at `B` via the `metering_split_tail`/`metering_split_head` STABLE fns (mig 000048). The `adv` CTE advances the cursor ONCE (to raw `now()`, SC-4) and returns BOTH clamped portions; two upserts consume it. **P2 is keyed off `period_end($expected)` (the period right after `$expected`), NEVER `period_of(now())`** — a long clamped gap must not misattribute `head` into now()'s period:
```
WITH adv AS (
  UPDATE metering_cursor SET last_metered_at = now()                    -- SC-4 CONTINUATION: advance to raw now(), NEVER B / clamped_end
  WHERE tenant_id=$1 AND metric=$2 AND cell_id=$3
    AND last_metered_at = $expected                                     -- CAS
    AND now() > metering_cursor.last_metered_at                         -- SC-1 monotonicity
    AND period_of(now()) <> period_of($expected)                        -- SC-2 split predicate: fires ONLY on a real straddle (else 0 rows → same-period writer)
    AND $gen = (SELECT generation_id FROM leader_generations
                WHERE name=$loop AND cell_id=$leaderCell)               -- SC-6 fence
  RETURNING
    metering_split_tail($expected, $max_delta) AS tail,                 -- SC-7 clamped P1 portion (in $expected's period)
    metering_split_head($expected, $max_delta) AS head),               -- SC-7 clamped P2 portion (in period_end($expected)'s period); tail+head == LEAST(now()-$expected, $max_delta)
p1 AS (
  INSERT INTO cost_counters(tenant_id,metric,period_start,period_end,plan_tier,value)
    SELECT $1,$2, period_start($expected), period_end($expected), $plan, metering_cost($reqMillicore, tail) FROM adv
    ON CONFLICT (tenant_id,metric,period_start) DO UPDATE SET value = cost_counters.value + EXCLUDED.value, plan_tier = EXCLUDED.plan_tier)
p2 AS (
  INSERT INTO cost_counters(tenant_id,metric,period_start,period_end,plan_tier,value)   -- P2 keyed off period_end($expected), NOT now() (SC-7 no-misattribution)
    SELECT $1,$2, period_end($expected), period_end(period_end($expected)), $plan, metering_cost($reqMillicore, head) FROM adv
    ON CONFLICT (tenant_id,metric,period_start) DO UPDATE SET value = cost_counters.value + EXCLUDED.value, plan_tier = EXCLUDED.plan_tier)
SELECT (metering_cost($reqMillicore, tail) + metering_cost($reqMillicore, head))::bigint AS delta FROM adv;  -- :one — clamped total delta (0 rows on HELD → ErrNoRows)
```
+ a same-tx `UpsertUsageRecord` fed the CLAMPED `delta`. `period_of`/`period_start`/`period_end` = deterministic pure-UTC billing-period fns (EC-7), never read from `cost_counters`. **SC-7 clamp < one period (ADR-8) ⇒ at most one boundary straddled ⇒ `tail + head == LEAST(now()-$expected, $max_delta)` AND P2 is exactly `period($expected)+1` (never a middle month); the cursor advances exactly once to raw `now()`.** Accepted drawback (symmetric to ADR-4's `ClampsLongGap`): a genuine multi-hour reservation across the boundary while the meter was down is under-billed beyond `max_delta` — the platform (cause of the outage) eats it; under-bill is the safe direction. PG-REAL validated 2026-07-01.
**Rationale:** the single-period write of v1.1 misattributed a straddling interval and (as UPDATE) lost the new-period opening (state-guardian INV-2/INV-3). **SC-2 (MEET-004):** the split-vs-same-period choice is now governed by the DB `now()` predicate (`<>` here, `=` in ADR-4), NOT a Go clock — a wrong Go dispatch guess is a 0-row HOLD that self-corrects next tick, never a misattribution or negative `head`. **SC-4:** the cursor advances to `now()` (continuation), NOT to `B` (reset) — proving C9's "no reset at rollover"; `metering_cursor` stays period-independent (PK has no period column).
**Rejected:** single-period attribution (misbills at month-end, the common leader-failover time); Go-clock split dispatch (SC-2 dual-clock bug); `last_metered_at = B` reset (would re-meter `[B, now()]` next tick — the SC-4 hole).

### ADR-10 (NEW) — Dedicated bounded pgxpool for the worker (pool isolation)
**Decision:** the metering worker owns a **separate small `pgxpool`** (2-4 conns), NOT the API pool; a bounded-concurrency writer caps in-flight writes; the API `pg_breaker` is a backstop, not the primary defense.
**Rationale:** SRE R1 + PDR C1 — a per-tick `Allow()` pre-gate does not bound the burst; at 1,500 tenants an unbounded burst on the shared pool trips the API breaker → 503 on the deploy path. A separate pool makes that physically impossible.
**Rejected:** shared pool + per-tick breaker gate only (v1.1) — reduces odds, not blast path.

### ADR-11 (NEW) — Raw pod LIST now; informer migration gated on a duration ceiling
**Decision:** ship namespace-scoped raw `List` (least-privilege-friendly) for observe-only at current cell scale; `loopzero_metering_pass_duration_seconds` carries a documented ceiling that triggers informer migration.
**Rationale:** SRE R2 — a conscious choice (same rigor as ADR-6), not an unexamined default.

### ADR-12′ (DECIDED, MEET-2026-07-01-003) — KEEP ADR-2 as billing SoT; Prometheus (KSM) is a read-only reconciliation oracle only
**Status:** DECIDED — **KEEP ADR-2** (direct k8s pod LIST, PG-derived namespace attribution) as the billing source of truth. #128's cross-cluster Prometheus/KSM federation is adopted **only** as an optional, non-authoritative **reconciliation oracle**, NOT a data-source switch. Unanimous (SRE + distributed-systems + tenant-guardian).
**Why NOT switch the billing producer to Prometheus** (the tempting simplification, rejected):
- **Correctness (dist-sys, decisive):** an exactly-once monotonic-cursor accumulator needs read-failure to be an explicit, scoped error so the cursor can HOLD. A `pod.Spec` LIST provides that; a Prometheus **instant query structurally cannot** — partial-result-as-success (short vector = `err==nil`), 5-min staleness, discarded Warnings (`data_plane_poller.go:52`) → a scrape gap advances the cursor over an under-measured interval → **permanent silent under-bill**. It downgrades exactly-once → at-most-once. The "#128 preserve-last-known-good applies" claim is a false analogy — that preserves a gauge (harmless stale badge), not a moving-cursor money counter.
- **Production (SRE):** ADR-12 only dissolves the *read* half of R1; the *write* burst (→ `pg_breaker` 503) is unchanged, so ADR-10 + the load test stay regardless. A down cell → zero series in a globally-successful query → silent under-metering of a whole cell.
- **Attribution (tenant-guardian):** label-based attribution (even renderer-set) is a probability argument, not a boundary argument — a PG-derived namespace name IS the isolation boundary; a label can diverge (webhook/HPA/controller/renderer-bug/`kubectl label`) → silent cross-tenant mis-bill into a Stripe-reported table. Re-opens INV-4/INV-5 and partially defeats EC-1 (scale-to-zero → zero KSM samples ≡ label-absence).
- ADR-2 already captures Prometheus's ONE real win — KSM-grade pod-math — by vendoring `k8s.io/component-helpers/resource.PodRequests` (§ Dependencies), WITHOUT the Prometheus dependency.
**Reconciliation oracle (OPTIONAL — may defer to a follow-up):** query the same KSM data label-attributed and compare per-`(tenant,project,env)` against the PG-namespace-attributed meter. **C-attr-1:** writes ZERO billing rows — reuse the `DataPlanePoller`/`env_targets` snapshot shape, never `cost_counters`/`usage_records`; divergence never gates/corrects billing. **C-attr-2:** wire a `MeteringLabelDrift` alert (`|oracle_label_sum − meter_PG_sum| > ε`) — the standing INV-5 production detector (a Deployment whose `loopzero.io/tenant` diverged from its namespace's true PG owner). Safe by construction (a Prometheus outage can only fail to reconcile, never mis-bill).
**Sanctioned scale-out escape hatch:** ADR-12′ may become the *write* source ONLY IF a future redesign makes query-incompleteness (Warnings / short vector / stale-beyond-threshold / per-cell expected-series count) a **per-cell READ FAILURE that HOLDS the cursor** — gated on the same `loopzero_metering_pass_duration_seconds` ceiling as the ADR-11 informer migration.
**Owner:** MEET-2026-07-01-003 (record: `docs/meetings/records/MEET-2026-07-01-003.md`).

### ADR-13 (NEW v1.6 — MEET-004 SC-3/SC-5) — Read-failure-holds invariant + cursor lifecycle at Tenant boundaries
**Decision (SC-3 — read-failure HOLDS the cursor):** `collect()` returns `(Σreq, error)`. A non-nil error (cell unreachable, LIST timeout) MUST short-circuit the tick for that (tenant, cell) **BEFORE any `MeterCompute*` call** — the write-tx is never issued, so the cursor is untouched (HELD, deferred to next tick). Rule 8: a read failure is a typed error, NEVER swallowed to `Σreq=0`. EC-1's "invoke unconditionally every tick, even at Σreq=0" is scoped to a **successful empty list only** (a genuine scale-to-zero), never to a failed read. A failed read that still advanced the cursor over an unmeasured interval is the exact MEET-003 dist-sys silent-under-bill — forbidden.
**Decision (SC-5 — cursor lifecycle across the canonical Tenant machine):** `SeedMeteringCursor` **seeds-if-absent** to `now()` on first sight (EC-10 — 0 charge for the pre-existence window). On tenant DELETE the cursor rows are **GC'd** by the existing tenant reaper (add a `DELETE FROM metering_cursor WHERE tenant_id=$1` to the reaper's cleanup, alongside its existing per-tenant deletes) so a reused/reprovisioned `tenant_id` cannot bill a stale `[old_T, now()]` window (resurrection double-bill). Suspend→Unsuspend resumes from the frozen cursor (the clamp bounds the frozen gap; ADR-8). **Scope note:** this is a minimal producer-side integration — one `DELETE` line in the existing reaper cleanup, NO new billing scope, NO enforcement path. It stays observe-only.
**Rationale:** SC-3 makes the HOLD invariant explicit (v1.5 stated "no double" but not "cursor UNCHANGED on read failure"); SC-5 closes the cursor's undefined lifecycle at the Tenant state boundaries the state-guardian flagged (orphan-on-delete + reuse double-bill).
**Rejected:** error-as-zero-Σreq (swallows the read failure → silent under-bill, Rule 8 violation); leaving cursor rows orphaned on tenant delete (resurrection double-bill on `tenant_id` reuse).

### Conditions-folded map (MEET-2026-07-01-004 → v1.6)
| Cond | Where folded | Test |
|---|---|---|
| SC-1 (BLOCKER) monotonicity guard | ADR-4 + ADR-9 CTE (`now() > last_metered_at` + `GREATEST(...,0)` clamp) | `TestMeterCompute_NegativeDeltaClockSkew_NoDecrement` (asserts `last_metered_at` UNCHANGED) + `TestMeterCompute_BackwardSkew_CursorNotRewound` |
| SC-2 (BLOCKER) DB-clock split dispatch | ADR-4 (`period_of(now())=period_of($expected)`) + ADR-9 (`<>`) | `TestMeterCompute_DispatchClockDisagreement_HoldsNotMisbills` |
| SC-3 read-failure-holds | ADR-13 + Task 3.1 EC-1 | `TestMeteringLoop_PartialCellFailure_NoLostNoDouble` (asserts failed cell `last_metered_at` UNCHANGED) |
| SC-4 period-independence behavioral | ADR-9 (`SET last_metered_at = now()`) | `TestMeterComputeSplit_ContinuesNotResets` (post-split `last_metered_at == now()`, not `== B`) |
| SC-5 cursor lifecycle | ADR-13 + Task 2.3 + Task 3.2 reaper | `TestMeteringCursor_TenantDelete_GCsRow` + `TestMeteringCursor_ReprovisionedTenantId_SeedsFresh` |
| SC-6 fence single-source | ADR-4 + ADR-9 (`$loop/$leaderCell` bound to the loop's own lease) | `TestMeterCompute_FencedWriterRejected` (stale gen from the metering loop's OWN lease name) |
| SC-7 (BLOCKER) split total-clamp (MEET-006) | ADR-9 (`metering_split_tail`/`metering_split_head`, mig 000048 — clamp the TOTAL to `max_delta`, split the clamped window; P2 off `metering_period_end(expected)`, never `now()`) | `TestMeterComputeSplit_ClampsLongGap` (SUM(both rows) == `metering_cost(req, max_delta)`) + `TestMeterComputeSplit_MultiMonthGap_NoMiddlePeriodMisattribution` |

## Dependency Graph
```
Phase 0 (loopzero-contracts: metrics, caps=Unlimited, tag v0.3.0)  ← FIRST
      ↓
Phase 1 (loopzero: go.mod bump + aliases + metric-enum drift guard + state-literal drift guard)
      ↓
Phase 2 (DB: mig 000046 CHECK+metering_cursor; 000047 usage_records BIGINT; ADR-4 CTE + ADR-9 split writers; upsert)
      ↓
Phase 3 (worker: PG-derived per-cell enumeration + dedicated pool + panic-recovery/single-flight + kill-switch-DB-row + metrics/alerts)
      ↓
Phase 6 (Integration Validation: PG-REAL exact-value multi-namespace + multi-cell LIVE + load test)
```
Pricing (old Phase 4) → follow-up. Enforcement → follow-up.

## Phases

### Phase 0 — loopzero-contracts (caps = Unlimited)
**Task 0.1** — `MetricCPUCoreSeconds`/`MetricMemGBSeconds` + `planCaps` = `Unlimited` all tiers; tag v0.3.0.
- TDD: `TestCapFor_ComputeMetrics_DefaultUnlimited`. DoD: `go test ./... && go vet`; tag; CHANGELOG.

### Phase 1 — consume v0.3.0 + drift guards
**Task 1.1** — bump go.mod (api+pkg) `loopzero-contracts` v0.1.0 → **v0.3.0** + enforcement aliases. **The bump crosses v0.2.0's breaking change** (`SessionTokenIssuer.Issue` gained a 5th param `accountID string`, released 2026-06-27, M3 — loopzero never consumed it). Only **3 call-sites** break, all in `api/internal/middleware/jwt_validator_test.go` (lines 28/59/75, the 4-arg `.Issue(...)` form) — adapt each to pass a 5th test-value `accountID` (e.g. `"acct-test"`). No production `.Issue` caller exists (verified `grep`). TDD `TestEnforcement_ComputeMetricAliasesResolve` + the adapted jwt_validator tests stay GREEN. DoD `go build ./... && go test ./api/internal/middleware/...`.
**Task 1.2** — metric-enum drift guard (SQL CHECK ≡ `plan.CostMetric` set) in `task quality:gates`. TDD `TestMetricCheckConstraint_MatchesContractsConstants`.
**Task 1.3** (REVISED v1.8 — folds into T3.1) — **enumeration-phase-filter guard**: assert the tenant enumeration filters on the `loopzerov1.TenantPhaseActive` CONSTANT (not a hardcoded `"Active"` string), so renaming the phase constant cannot silently drift the metered set. TDD `TestMeteringEnumeration_FiltersOnTenantPhaseActiveConstant` (the fake `TenantLister` returns tenants across all phases; assert only `Active` is metered; **C2: assert the filter reads the observed `status.phase` (not `spec`) and that a CR in a non-canonical namespace is NOT enumerated**). **Replaces the v1.7 SQL state-literal guard** — there is no SQL status literal to guard (the phantom `tenants.status` column was the ADR-5 defect fixed in v1.8; tenant lifecycle status is the CR phase). Folds into T3.1 (where the enumeration lives).

### Phase 2 — DB schema + atomic fenced CTE writers
**Task 2.1** — mig 000046: widen `cost_counters.metric` CHECK + `CREATE TABLE metering_cursor(tenant_id,metric,cell_id, last_metered_at TIMESTAMPTZ, PRIMARY KEY(tenant_id,metric,cell_id))` + a `metering_config(enabled BOOLEAN NOT NULL DEFAULT false)` single-row kill-switch table **seeded `false`** (EC-5 — observe-only ships disabled; operator flips on after LIVE validation) + GRANT `loopzero_worker`. **metering_cursor + metering_config RLS = disabled by design** (control-plane, not tenant-facing — explicit line). TDD `TestMigration000046_*`. Failure(PG): dirty replay `IF NOT EXISTS`.
**Task 2.2** — mig 000047: `usage_records.quantity` INTEGER→BIGINT. TDD `TestMigration000047_UsageQuantityBigint` (insert > 2^31); document DOWN is lossy-after-data.
**Task 2.3** — atomic writers (ADR-4 + ADR-9):
- `cost.sql`: `MeterCompute :one` (the ADR-4 CTE, same-period — returns the clamped delta, `pgx.ErrNoRows` on HELD), `MeterComputeSplit :one` (ADR-9 two-period — **SC-7/MEET-006: clamps the TOTAL to `@max_delta` via `metering_split_tail`/`metering_split_head` (000048), splits the clamped window, P2 off `metering_period_end(@expected)`; returns the clamped total delta**), `SeedMeteringCursor :exec`, `UpsertUsageRecord :exec`, `GetPlanTierForTenant :one` (`SELECT COALESCE(plan, 'starter') FROM subscriptions WHERE tenant_id = $1 AND status = 'active'` — EC-4; billing status `'active'`, NOT tenant lifecycle; missing/none → `'starter'` + `loopzero_metering_missing_plan_total` WARN), **`GetCellForTenant :one` (`SELECT cell_id FROM tenants WHERE id = $1` — the canonical placement SoT per INV I2; tenant-guardian C1; run in the `SET LOCAL app.tenant_id=''` RLS-bypass context like the other worker reads; **NoRows → skip + metric, not fatal** — a CR Active but its PG `tenants` row deleted mid-race has 0 projects anyway, tenant-guardian note 1)**, regenerate. **(v1.8: `ListActiveTenantsWithCell` REMOVED — the active-tenant set comes from the Tenant CR list in T3.1 per ADR-5, not a PG query on a phantom `tenants.status` column. The money-writer CTEs are UNCHANGED — they take `(tenant_id, metric, cell_id, …)` as params regardless of where the tenant list originates.)**
- **EC-4 (plan_tier):** the CTE's `plan_tier=$plan` is sourced from the enumeration join; a tenant with no/unknown subscription defaults to `'starter'` + `loopzero_metering_missing_plan_total` WARN — the write MUST NOT abort (else the tenant stalls every tick). `ON CONFLICT DO UPDATE` also refreshes `plan_tier` (mid-period plan change).
- **EC-7 (period fn):** `period_start`/`period_end` come from a pure **UTC** billing-period function (never server-local / DST).
- **EC-6 (fencing authority):** the metering loop's `leader_generations` row (its own lease `name`/`cell_id`) MUST exist before the first meter write (written on `Acquire`, SRE R4); a NULL fencing subquery is surfaced loudly (`loopzero_metering_fence_unresolved_total`), never a silent 0-rows no-op.
- `cost_wrapper.go`: `MeteringStore` interface; each method runs `SET LOCAL app.tenant_id = ''` + the CTE + usage upsert **in one tx**.
- **TDD (all PG-REAL — real Postgres, RLS policy loaded, `loopzero_worker` role):**
  - `TestMeterCompute_ExactlyOnce_TwoConcurrentPasses` (`-race`, CAS → once).
  - `TestMeterCompute_FencedWriterRejected` (**SC-6** — stale generation from the metering loop's OWN lease name → 0 rows).
  - `TestMeterCompute_NullCursorBootstrapsZero`.
  - `TestMeterCompute_ClampsLongGap` (+ `loopzero_metering_clamped_total`).
  - `TestMeter_ClampCannotSpanTwoBoundaries` (ADR-8 clamp < period).
  - `TestMeterCompute_NegativeDeltaClockSkew_NoDecrement` (**SC-1** — backward `now()` → 0 rows, no counter decrement, **AND asserts `last_metered_at` UNCHANGED**).
  - `TestMeterCompute_BackwardSkew_CursorNotRewound` (**SC-1** — cursor never moves backward).
  - `TestMeterCompute_DispatchClockDisagreement_HoldsNotMisbills` (**SC-2** — Go picks the wrong writer at the boundary → the DB period predicate yields 0 rows → cursor HELD, self-corrects next tick, no misattribution/negative head).
  - `TestMeterComputeSplit_ContinuesNotResets` (**SC-4** — post-split `last_metered_at == now()`, NOT `== B`).
  - `TestMeteringCursor_ReprovisionedTenantId_SeedsFresh` (**SC-5** — after GC, a reused `tenant_id` seeds to `now()`, never bills a stale window).
  - `TestMeterCompute_RLSBypass_WritesAllTenants` **+ negative twin** `TestMeterCompute_WithoutRLSBypass_WritesZeroRows` (proves `SET LOCAL` load-bearing — CVP).
  - `TestMeterCompute_UsageRecordSameTx` (crash → all rollback, cursor unadvanced).
  - `TestMeterComputeSplit_TailToP1_HeadToP2_CursorAdvancesOnce` (ADR-9; **large `max_delta` regime** — asserts `value(P1) == metering_cost(req, period_end(expected)-expected)` EXACT (seed-controlled `tail`) + `value(P2) > 0` at `period(expected)+1` + cursor advances once). PG-REAL validated 2026-07-01.
  - `TestMeterComputeSplit_ClampsLongGap` (**SC-7/MEET-006** — leader-outage straddle, `expected = period_start(now())-10s`, `max_delta=20s` → `SUM(both period rows) == metering_cost(req, max_delta)`, prior+current period rows, cursor→`now()`, clamp metric. **RED against an unclamped split, GREEN with `metering_split_*`.** PG-REAL validated 2026-07-01: `delta=10`, `P1=5@prior`, `P2=5@current`, SUM bounded).
  - `TestMeterComputeSplit_MultiMonthGap_NoMiddlePeriodMisattribution` (**SC-7/MEET-006** — a ≥2-boundary raw gap bills ≤ `max_delta`, no third-period lump, P2 keyed at `metering_period_end(expected)` = `period(expected)+1`, never `period_of(now())`).
  - `TestMeterCompute_NewPeriodRowCreatedByUpsert`.
  - `TestMeterCompute_UnknownPlanDefaultsStarter` (EC-4 — write succeeds, `plan_tier='starter'`, WARN metric, no abort).
  - `TestMeterCompute_NoGenerationRow_IsLoudNotSilent` (EC-6 — NULL fencing subquery → typed error / metric, not silent skip).
  - `TestPeriodBoundary_IsUTCDeterministic` (EC-7 — pure UTC period fn; month-end at local 23:30 classifies correctly).
- Concurrency: two-concurrent-passes + fenced-writer, `-race`.
- Failure(PG): connection drop mid-tx → full rollback, cursor unadvanced (at-most-once-per-interval).
- DoD: `go test -race ./api/internal/db/... -run Meter` **against real PG**; sqlc diff-clean.

### Phase 3 — worker + controller wiring + operability
**Task 3.1** — `api/internal/metering`: the pass.
- **list Tenant CRs** via the `TenantLister` interface (over `tenant_cr_manager.go`, **scoped to the canonical `loopzero-system` namespace** — C2; a CR in a non-canonical ns is NOT enumerated) → **filter on the observed `status.phase == loopzerov1.TenantPhaseActive`** (the CONSTANT, not a string; skip Provisioning/Suspended/PendingDeletion/Deleting) → for each active tenant: `tenant_id` from the CR; **`cell_id` from `GetCellForTenant(tenant_id)` — canonical PG `tenants.cell_id`, NEVER the CR `spec.cellID` mirror (INV I2, C1)** → enumerate (project,env) from PG → `naming.Namespace` per pair → `cellmgr.ClientFor(cell)` → **list pods per namespace** → compute the tenant's Σ effective request → `GetPlanTierForTenant(tenant_id)` for `plan_tier` → `MeterCompute`/`MeterComputeSplit` per (tenant,metric,cell), passing `lease.GenerationID`, **own batch timeout** (not 5s). The `MeteringStore` (interface) depends on a `TenantLister` (DIP over the CR client) — injectable/fakeable in tests. **(v1.8/v1.9 ADR-5: active-tenant SET from CR phase; cell from canonical PG — not a phantom `tenants.status` query, not the CR cell mirror.)**
- **EC-1 (scale-to-zero) + SC-3 (read-failure-holds):** `collect()` returns `(Σreq, error)`. On a **successful** list (incl. a genuine empty one — KEDA min=0), invoke `MeterCompute` per (tenant,metric,cell) — Σreq=0 is a valid delta of 0 → cursor advances, 0 billed → a scale-up bills only from `now`, never the idle window. On a **non-nil error** (cell unreachable / LIST timeout), **short-circuit BEFORE any `MeterCompute*` call** — cursor HELD (untouched), skip + `loopzero_metering_cell_errors_total`, retried next tick. Rule 8: the read failure is a typed error, NEVER swallowed to `Σreq=0` (that would advance the cursor over an unmeasured interval → silent under-bill, the MEET-003 dist-sys hole). "Unconditional every tick" is scoped to a successful empty list only.
- **EC-2 (pod phase):** `collect` counts a pod ONLY if `pod.Spec.NodeName != ""` AND phase ∈ {Running, scheduled-Pending} AND NOT Succeeded/Failed (mirror sealos `monitorPodResourceUsage`). Unscheduled/completed/terminating pods are not reserved compute.
- **EC-3 (container math):** effective pod request = `max(Σ initContainers, Σ regularContainers + Σ native-sidecars)` — NOT a naive container sum. Reuse `k8s.io/component-helpers/resource.PodRequests` if available, else implement the `max`.
- **EC-8 (limits-only):** when a container's `Requests` is empty but `Limits` is set, fall back to the limit (k8s admission defaults requests→limits) so a limits-only pod is not billed 0.
- Files: `worker.go`, `collect.go` (pure — EC-1/2/3/8 live here), `namespaces.go` (PG-derived), tests.
- Deep dep: interfaces only; does NOT reuse `QueryAllCellsParallel` (returns `*CellStatus`); attribution strictly from PG-enumerated namespaces, never the pod label.
- **TDD:**
  - `TestMeteringLoop_ExactCPUAcrossMultipleNamespaces` (**Goal**: 2-project tenant, one 500m pod each → `Σ×interval` exact, single cursor once) — PG-REAL.
  - `TestMeteringLoop_MetersAllProjectEnvNamespaces_SingleCursorNoDoubleCount` (tenant-guardian C1) — PG-REAL.
  - `TestMeteringLoop_IgnoresMutablePodTenantLabel` (forged `loopzero.io/tenant` in A's ns bills A — tenant-guardian C2).
  - `TestCollect_RejectsForeignNamespacePods` (never lists outside the PG-derived set — tenant-guardian C3).
  - `TestMeteringLoop_SkipsNonActiveTenants` (Active-only — via the CR-phase `TenantLister`).
  - `TestMeteringEnumeration_ResolvesCellFromPGNotCR` (**C1** — a tenant whose CR `spec.cellID` diverges from PG `tenants.cell_id` is metered against the **PG** cell; pods listed on the PG cell, cursor keyed on the PG cell_id; closes Risk #6 double-cursor-on-pin) — PG-REAL.
  - `TestMeteringLoop_PartialCellFailure_NoLostNoDouble` (per-cell cursor; **SC-3** — asserts the failed cell's `last_metered_at` is UNCHANGED, i.e. HELD not advanced-to-0).
  - `TestCollect_SumsPodRequests` (pure; no-requests → 0).
  - `TestMeteringLoop_ScaleToZeroAdvancesCursorBillsZero` (EC-1 — 0 pods → cursor advances, 0 billed; scale-up next tick bills only from now).
  - `TestCollect_SkipsUnscheduledAndTerminalPods` (EC-2 — Pending-unscheduled / Succeeded / Terminating contribute 0).
  - `TestCollect_InitAndSidecarContainerMath` (EC-3 — `max(Σinit, Σregular+Σsidecar)`; heavy-init pod not double-billed).
  - `TestCollect_LimitsOnlyPodUsesLimit` (EC-8).
  - `TestMeteringWorker_ReadOnlyTenantState` (emits no Tenant Event / no Transition — state-guardian C5).
- Concurrency: `TestWorker_RunOnce_ParallelCellsNoRace` `-race`.
- Failure(k8s,PG): cell unreachable → skip + `loopzero_metering_cell_errors_total`, cursor unadvanced; slow ≠ down.
- DoD: `go test -race ./api/internal/metering/...` PASS (PG-REAL for the store tests).

**Task 3.2** — controller wiring + isolation + operability.
- Files:
  - `metering_loop.go` (NEW): ticker; per tick: **kill switch** (`metering_enabled` from a **DB row**, per-tick read; **EC-5**: absent row → default **disabled** + WARN, never a nil-deref — mig 000046 seeds the row) → **single-flight** guard (no overlapping pass) → **panic-recovery** boundary (`recover()` + `loopzero_metering_panics_total`, never propagate) → `worker.RunOnce` using the **dedicated pgxpool** (ADR-10) → bounded-concurrency writer.
  - `main.go`: construct the worker's own pool + `go runMeteringLoop(...)` after `acquirer.Acquire` (leader-only), passing `lease.GenerationID`.
  - `metrics.go` (NEW): `loopzero_metering_cpu_core_seconds_written_total` (wiring-triad metric), `loopzero_metering_seconds_since_last_success` (freshness, global+per-cell), `loopzero_metering_tenants_metered_total` (vs active count — subset-miss detector), `loopzero_metering_pass_duration_seconds` (informer-ceiling signal), `loopzero_metering_cell_errors_total`, `loopzero_metering_clamped_total`, `loopzero_metering_writes_shed_total`, `loopzero_metering_panics_total`, `loopzero_metering_missing_plan_total` (EC-4), `loopzero_metering_fence_unresolved_total` (EC-6).
  - monitoring chart: alerts `MeteringStalled` (freshness > 3×interval), `MeteringCellStarved`, `MeteringCoverageGap` (metered < active count); runbook `docs/runbooks/alerts/metering.md` — includes **rollback correcting `cost_counters` AND resetting `metering_cursor`** (not only usage_records).
  - **SC-5 (cursor GC):** add `DELETE FROM metering_cursor WHERE tenant_id=$1` to the EXISTING tenant reaper's per-tenant cleanup (one line, no new component, observe-only — ADR-13). A deleted tenant leaves no orphan cursor row → a reused `tenant_id` seeds fresh.
- TDD: `TestMeteringLoop_StopsOnShutdown`, `TestMeteringLoop_KillSwitchDisablesWithinOneTick` (DB row), `TestMeteringLoop_DisabledWhenFlagRowMissing` (EC-5 — safe default, no nil-deref), `TestMeteringLoop_PanicRecovered_ControllerSurvives`, `TestMeteringLoop_SingleFlight_NoOverlap`, `TestMeteringLoop_UsesDedicatedPool`, `TestMeteringCursor_TenantDelete_GCsRow` (**SC-5** — reaper deletes the cursor row on tenant DELETE; PG-REAL).
- Concurrency: `TestMeteringLoop_LeaderOnly`, ticker cancel `-race`.
- DoD: `go test -race ./api/cmd/controller/... -run Metering`; metrics unique; alerts lint.

### Phase 6 — Final Phase: Integration Validation
**Task 6.1** — full-chain + PG-REAL gate + multi-cell LIVE.
- `integration_test.go` (PG-REAL + envtest fake cells): the exact-value Goal (multi-namespace), `TestMeteringLoop_RestartDoesNotDoubleMeter`, `TestMeteringLoop_SplitsDeltaAcrossPeriodBoundary` (ADR-9), leader-down-across-boundary.
- **Load test (gated DoD, ADR-10):** 1,500-tenant burst against the dedicated pool proving the **API `pg_breaker` does not trip** (no 503 on deploy path).
- **LIVE (per cleanroom-up-live-test-mandate.md, ≥2-cell):** seed a tenant whose workloads span ≥2 project/env namespaces across ≥2 cells; assert counters populate ≈ `Σ×elapsed`, one usage_records row/period, single cursor per (tenant,metric,cell) no double-count; kill-switch flip stops within one tick; `loopzero_metering_writes_shed_total` under pressure with API breaker not tripped; **zero-402** (billing UI unchanged, caps Unlimited); rollback runbook rehearsed on a seeded bad window; sponsor JUNTOS.
- DoD: `task test:unit && task lint && go build ./...`; `go test -race` (touched, PG-REAL for store); `/code-quality` ∉ {FAIL_HARD, INVALID}; CHANGELOG (both repos); **guardian re-sign (tenant + state ✅)**; re-confirm MEET APPROVED.

## Coverage Matrix

| # | Requirement (source: MEET-002 actions + EC review) | Task(s) | Verified by |
|---|---|---|---|
| 1 | Compute CostMetric + per-plan Unlimited caps (observe-only, contracts) | T0.1 | `TestCapFor_ComputeMetrics_DefaultUnlimited` |
| 2 | Consume contracts v0.3.0 + enforcement aliases | T1.1 | `TestEnforcement_ComputeMetricAliasesResolve` |
| 3 | Metric-enum drift guard (SQL CHECK ≡ contracts constants) | T1.2 | `TestMetricCheckConstraint_MatchesContractsConstants` |
| 4 | Enumeration filters on `TenantPhaseActive` CONSTANT (CR phase — not a phantom PG `tenants.status`) | T1.3 | `TestMeteringEnumeration_FiltersOnTenantPhaseActiveConstant` |
| 5 | Migration: CHECK widen + `metering_cursor` (RLS-off) + `metering_config` seed (MEET act 17; EC-5) | T2.1 | `TestMigration000046_*` |
| 6 | Migration: `usage_records` INTEGER→BIGINT | T2.2 | `TestMigration000047_UsageQuantityBigint` |
| 7 | Atomic two-table CTE write + period-rollover split + upsert (MEET act 1,2; ADR-4, ADR-9) | T2.3 | `TestMeterComputeSplit_TailToP1_HeadToP2_CursorAdvancesOnce` |
| 8 | Clamp < period + fencing subquery + PG-REAL mandate + negative RLS twin (MEET act 3,4,19,20; EC-6,EC-7) | T2.3 | `TestMeterCompute_WithoutRLSBypass_WritesZeroRows` |
| 9 | PG-derived namespace enumeration + attribution negative tests (MEET act 5,6) | T3.1 | `TestMeteringLoop_IgnoresMutablePodTenantLabel` |
| 10 | Pod-request math: KEDA scale-to-zero, pod-phase, init/sidecar, limits-only, plan_tier (EC-1..EC-4,EC-8) | T3.1 | `TestMeteringLoop_ScaleToZeroAdvancesCursorBillsZero` |
| 11 | Read-only tenant-state attestation + least-privilege note (MEET act 7,10) | T3.1 | `TestMeteringWorker_ReadOnlyTenantState` |
| 12 | Controller wiring: dedicated pool + load test, panic-recovery, single-flight, kill-switch DB row, coverage metric, rollback runbook, raw-LIST-vs-informer (MEET act 9,11,12,13,14,16; ADR-10, ADR-11) | T3.2 | `TestMeteringLoop_KillSwitchDisablesWithinOneTick` |
| 13 | Cursor period-independence lock — schema AND behavior (state-guardian C9; SC-4) | T2.1 | `TestMeterComputeSplit_ContinuesNotResets` |
| 14 | Enforcement + pricing carved to follow-ups (observe-only; ADR-7) | T0.1 | caps `Unlimited` (no 402 path) |
| 15 | Integration validation: exact-value multi-ns Goal + ≥2-cell LIVE + Stripe-gating (MEET act 18,21) | T6.1 | `TestMeteringLoop_ExactCPUAcrossMultipleNamespaces` |
| 16 | EC-9/10/11 documented (paging, mid-tick create, pool stall) | T3.1 | § Edge cases absorbed |
| 17 | SC-1 cursor monotonicity guard in the CTE (no backward decrement/rewind) | T2.3 | `TestMeterCompute_NegativeDeltaClockSkew_NoDecrement` |
| 18 | SC-2 split dispatch governed by DB `now()` (wrong Go guess → HOLD, not mis-bill) | T2.3 | `TestMeterCompute_DispatchClockDisagreement_HoldsNotMisbills` |
| 19 | SC-3 read-failure HOLDS the cursor (no error-as-zero swallow) | T3.1 | `TestMeteringLoop_PartialCellFailure_NoLostNoDouble` |
| 20 | SC-4 period-independence behavioral (post-split continuation, not reset) | T2.3 | `TestMeterComputeSplit_ContinuesNotResets` |
| 21 | SC-5 cursor lifecycle (seed-if-absent + GC on tenant delete) | T3.2 | `TestMeteringCursor_TenantDelete_GCsRow` |
| 22 | SC-6 fence single-source-bound to the loop's own lease | T2.3 | `TestMeterCompute_FencedWriterRejected` |
| 23 | Active-tenant source = Tenant CR phase (not phantom PG col); cell from canonical PG (INV I2, C1) | T3.1 | `TestMeteringEnumeration_ResolvesCellFromPGNotCR` |
| 24 | TenantLister scoped to loopzero-system + filters observed status.phase (C2) | T1.3 | `TestMeteringEnumeration_FiltersOnTenantPhaseActiveConstant` |
| 25 | SC-7 split total-clamp: bound over-bill to `max_delta` + no middle-period misattribution (MEET-006; `metering_split_tail`/`head`, P2 off `period_end(expected)`) | T2.3 | `TestMeterComputeSplit_ClampsLongGap` + `TestMeterComputeSplit_MultiMonthGap_NoMiddlePeriodMisattribution` |

## Edge cases absorbed (from `/edge-case-plan` 2026-07-01)

**MUST FIX → sub-tasks/tests** (EC-1 scale-to-zero, EC-2 pod-phase, EC-3 container-math, EC-4 plan_tier) folded into T3.1/T2.3 above. **SHOULD TEST** (EC-5..EC-8) added to the TDD of their tasks. **DOCUMENTED (accepted risk):**
- **EC-9** — very large pod count per namespace: the raw `List` uses `limit`/`continue` paging; scale beyond the current cell count triggers the ADR-11 informer migration via the `loopzero_metering_pass_duration_seconds` ceiling.
- **EC-10** — namespace/project/env created mid-tick: metered on the next tick (delay ≤ one interval); cursor bootstraps to `now()` (0 charge) on first sight. Bounded, acceptable.
- **EC-11** — dedicated worker pool (2-4 conns) stall under contention: isolated blast radius (ADR-10); a stalled write starves only the metering pass (surfaced by `MeteringStalled`) and self-heals next tick; worker writes carry a context timeout.

## Dependencies

### Existing — use as-is (all present, verified in `api/go.mod`)

| Package | Version | Ecosystem | Why |
|---|---|---|---|
| `github.com/jackc/pgx/v5` (pgxpool) | `v5.9.0` | go | dedicated worker pool (ADR-10) + all PG writes |
| `github.com/prometheus/client_golang` | `v1.23.2` | go | `loopzero_metering_*` runtime metrics (wiring triad) |
| `k8s.io/client-go` | `v0.35.2` | go | per-cell pod LIST via `cellmgr.ClientFor` |
| `k8s.io/api` | `v0.35.2` | go | `corev1.Pod` / resource types |
| internal `pkg/naming`, `pkg/statemachine`, `pkg/enforcement`, `api/internal/{leader,cellmgr,admission,db}` | — | go (in-repo) | enumeration, fencing, cell access, breaker, sqlc store |

### New — to be introduced

| Package | Version | Ecosystem | Rule 9 rationale (alternatives evaluated) | Why this one |
|---|---|---|---|---|
| `k8s.io/component-helpers/resource` (NEW) | `v0.35.2` (pin to the k8s.io family already vendored) | go | Evaluated: (a) hand-roll `max(Σinit, Σregular+Σsidecar)` — rejected: pod-request math has subtle rules (native sidecars via `restartPolicy=Always` init containers, in-place resize status, overhead) that k8s SIG-node maintains + tests; reinventing it is the Rule 9 anti-pattern and risks the exact EC-3 mis-billing. (b) copy the ~40-line formula in-repo — rejected: same maintenance/correctness burden without upstream test coverage. | `PodRequests` is the canonical, tested k8s helper; same-family `v0.35.2` (matches `k8s.io/api`/`client-go` already present); zero CVE; trivial `go get`. |

### Removed
| (none) | | |

## Drawbacks & Risks
| # | Risk | Sev | Mitigation | Owner |
|---|---|---|---|---|
| 1 | Producer writes real counters (pollution even observe-only) | HIGH | atomic fenced CTE + PG-REAL failure gate + LIVE before merge; caps Unlimited; auditable usage_records | api-engineer |
| 2 | usage_records is Stripe-reported — observe-only bounds 402 not invoice | HIGH | confirm no Stripe reporter consumes compute rows until the enforcement follow-up; rollback runbook | sponsor |
| 3 | Cross-repo coupling (contracts v0.3.0 first) | MED | Phase 0 gates Phase 1; CI builds against the pinned tag | platform-sdk-engineer |
| 4 | Corrected enumeration multiplies k8s LIST fan-out (O(tenants×proj×env)) | MED | ADR-11 duration ceiling → informer; dedicated pool caps PG side | sre-engineer |
| 5 | Guardian re-sign still required before /implement | MED | 6.1 DoD gates on tenant+state ✅ | Change Governor |
| 6 | Sticky-placement stale cursor after pin/evict (old cell cursor lingers) | LOW | documented; targeted resolution only meters `tenants.cell_id` | api-engineer |
| 7 | **Worker write burst contends the API PG pool** (`config.go:356` `DB_MAX_CONNS=25`; breaker trips at 0.90 → 503 `pg_breaker.go:76`) — O(tenants) writes/tick could shed API mutations | MED | **ADR-10 dedicated bounded pgxpool** — the worker never draws from the API pool; **1,500-tenant load test** (MEET-002 act 9) proves the burst stays under budget; shed metric. **Prod (AWS RDS, ADR 2026-06-17):** the 25-conn ceiling is fronted by **RDS Proxy** (managed transaction-mode) — loopzero is already pooler-safe (Rule 7 xact-scoped locks + `set_config(...,true)`/`SET LOCAL` everywhere, **zero session-level SET** verified `environment_lock.go:88`, `audit_log_pruner.go:127`; worker RLS bypass `SET LOCAL app.tenant_id=''` does NOT pin). Cutover caveat: validate `pgx` prepared-statement protocol vs RDS Proxy pinning (AWS milestone, not this plan) | sre-engineer |
| 8 | **All background work serializes on ONE leader replica** (this worker + DataPlanePoller + reapers; `NeedLeaderElection()=true`) — latency SPOF, a slow tick delays the whole billing loop | LOW | serialization **is** the exactly-once correctness property, not a defect (do NOT parallelize); **kill-switch DB row** (MEET-002 act 12) disables without redeploy; `loopzero_metering_pass_duration_seconds` alerts before saturation | sre-engineer |
| 10 | **Active→Suspended drain-window under-count** (C3) — a tenant transitioning to Suspended whose pods have not yet scaled down is NOT metered during the drain (skip-non-Active) | LOW | intentional (suspended = don't bill); made explicit so it is a decision, not a silent gap; ADR-8 clamp bounds any resume gap | tenant-guardian |
| 9 | **Naive per-pod sequential UPDATE loop** — the `data_plane_poller.go:128-147` N+1 write pattern would N-round-trip the leader conn each tick | MED | **ADR-4 mandates ONE atomic two-table CTE per tenant-tick** (batched, never N UPDATEs) — the write path deliberately avoids the #128 anti-pattern the bottleneck analysis flagged | api-engineer |

> **Note (v1.4):** risks 7-9 were surfaced by a static bottleneck analysis of the service (2026-07-01) that mapped the metering worker's contention exposure onto the existing PG-pool spine. Each maps to a mitigation **already present** in this plan (ADR-4 / ADR-10 / kill-switch / load-test) — this is a risk-documentation refinement, **no scope, DoD, or task change** (observe-only producer, unchanged). The service-wide bottlenecks the analysis also found (batching the #128 poller's own N+1 loop, ArgoCD App-per-env) are **out of scope for this plan** and tracked separately.
> **Note (v1.5):** prod DB is **AWS RDS** (re-platform ADR `2026-06-17-infra-state-decomposition-aws`); R7's pool relief in prod is **RDS Proxy** (managed transaction-mode pooler), for which loopzero is already architected — Rule 7 (`pg_advisory_xact_lock` only) + `SET LOCAL` / `set_config(...,true)` discipline with **zero session-level SET** (verified). RDS Proxy adoption itself is a **Class A** resource in `loopzero-infra-live`, **not this plan**; the only cutover task this plan cares about is validating `pgx`'s prepared-statement protocol against RDS Proxy pinning.

## Unresolved Questions
- Batch timeout value (measured LIST latency) — `/edge-case-plan`.
- usage_records retention beyond N periods — enforcement/pricing follow-up.
- `metering_enabled` DB-row schema (single global flag vs per-cell) — `/edge-case-plan`.

## Failure scenarios (external I/O summary)
Kubernetes API (cellmgr) + PostgreSQL. Covered: cell unreachable (skip+metric+cursor unadvanced), slow≠down (own timeout), PG write fail (full rollback, at-most-once), NULL bootstrap (0+seed), long gap (clamp+metric), clock skew (0, no decrement), period straddle (ADR-9 split), RLS mis-context (negative twin), breaker/pool pressure (dedicated pool + shed metric), panic (recover, controller survives). Exercised in Task 6.1.

## Global DoD
- [ ] `go build ./...` GREEN (loopzero + loopzero-contracts).
- [ ] `go test -race ./...` PASS (touched); **store/meter tests run against real PostgreSQL with RLS policy under `loopzero_worker`** (not sqlmock).
- [ ] `task lint` + `internal-terms.py` clean.
- [ ] sqlc regen diff-clean; metric-enum + state-literal drift guards green in `task quality:gates`.
- [ ] Each changed file ≤ 500 LoC.
- [ ] `/code-quality` ∈ {PASS, PASS_WITH_CAVEATS}.
- [ ] CHANGELOG updated (loopzero + loopzero-contracts).
- [ ] Wiring triad: controller loop caller + integration exact-value test + `loopzero_metering_cpu_core_seconds_written_total`.
- [ ] Dedicated pool + 1,500-tenant load test (API breaker not tripped) + panic-recovery + kill-switch DB-row + freshness/coverage alerts shipped in the same change.
- [ ] Rollback runbook `docs/runbooks/alerts/metering.md` (cost_counters + metering_cursor + usage_records).
- [ ] LIVE per `cleanroom-up-live-test-mandate.md`, **≥2-cell**, tenant spanning cells, zero-402 assertion, rollback rehearsal, sponsor JUNTOS.
- [x] **tenant-guardian + state-guardian re-sign ✅** and MEET APPROVED before `/implement`. (tenant: MEET-2026-07-01-003 + **MEET-2026-07-01-005 re-sign on v1.9 ADR-5 revision**; state: MEET-2026-07-01-004 SIGN_OFF on v1.6.)
