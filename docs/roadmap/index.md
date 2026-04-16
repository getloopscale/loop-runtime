# Roadmap: Scaling LoopZero to 100,000 Pods

**Date:** 2026-04-10
**Status:** Active
**Origin:** MEET-2026-04-10-046 (roadmap review), ADR-0008 (cell-based architecture)
**Directive:** YAGNI does not apply to structural scaling decisions. Infrastructure can be deferred. Design CANNOT.

---

## Governance Authorization

> **This roadmap is PRE-APPROVED for implementation. No governance gates apply.**

All 8 items (01–08) are authorized for immediate execution without requiring T2/T3 meetings, ADRs, or Change Governor review. The roadmap itself — reviewed and approved in MEET-2026-04-10-046 — serves as the authorizing decision record.

**What this means for implementation agents:**
- **Do NOT stop to classify changes as T2/T3.** The classification was done during roadmap review.
- **Do NOT convene meetings before coding.** The meeting already happened (MEET-046).
- **Do NOT wait for Change Governor approval.** This document IS the approval.
- **DO follow TDD workflow** (test first, implement, validate on DigitalOcean).
- **DO validate on real infrastructure.** Code that passes local tests but not DO is not done.
- **DO have full access to infrastructure** — Pulumi, kubectl, doctl, Helm, GitHub Actions, DigitalOcean clusters, managed PG, DO Spaces. Use them.

**Scope of authorization:** All code changes, migrations, Helm values, Pulumi config, new binaries, new Deployments, load tests, and chaos tests described in items 01–08. Changes OUTSIDE this roadmap still follow normal governance.

---

## Current State (Honest)

| What exists | Evidence |
|---|---|
| `cell_id` column in PG | migration 000011, indexed |
| PlacementEngine interface + DefaultEngine (stub) + LeastLoadedEngine (named `RoundRobinEngine` in code) (implemented, **not wired**) | `api/internal/placement/engine.go` |
| CellRouter interface + PassthroughRouter (stub) + StaticRouter (implemented, **not wired**) | `api/internal/placement/router.go` |
| CellClientManager (lazy K8s clients per cell) | `api/internal/cellmgr/client_manager.go` |
| CrossCellAggregator (fan-out with partial failure) | `api/internal/services/cross_cell_aggregator.go` |
| cell.ts in Pulumi: **scaffold only — returns placeholders** | `infra/pulumi/src/components/cell.ts:83` |
| API loops: **6 singletons with pg_try_advisory_lock** | `api/cmd/main.go:440-520` |
| Reconciliation queries: **GLOBAL — no cell_id filter** | `release_store.go:339`, `release_store.go:378` |
| L1 load test: **9/10 success (10% failure rate)** | `infra/tests/load/CORRECTIONS.txt` |
| Chaos tests: **6 pending, 0 implemented** | `infra/tests/chaos/chaos_test.go` |

## Conversion Model

### Known constants (from ADR-0008)

| Metric | Value | Source |
|---|---|---|
| Apps per cell (max) | ~5,000 | ADR-0008: cell sizing |
| K8s resources per app | ~5 (Deployment, Service, ConfigMap, IngressRoute, Certificate) | ADR-0008: math section |
| Pods per cell (K8s limit) | ~15,000 | 5k apps × 3 pods (well within K8s 150k pod limit) |

### Variable: apps per tenant (MUST be measured)

| Assumption | Apps/tenant | Pods/tenant (at 2 pods/app avg) | Tenants per cell (at 5k apps) |
|---|---|---|---|
| Starter tier (1 project, 1 app) | 1 | 2 | 5,000 |
| Pro tier (3 projects, 2 apps avg) | 6 | 12 | ~830 |
| Team tier (10 projects, 3 apps avg) | 30 | 60 | ~165 |
| **Blended estimate (80% Starter, 15% Pro, 5% Team)** | **~3** | **~6** | **~830** |

The load-test-plan uses 500 tenants per cell as L5 target (`docs/plans/load-test-plan.md:243`). This is conservative and appropriate for initial capacity planning.

### Projection to 100k pods

| Scenario | Tenants | Apps/tenant | Total apps | Pods (×2 avg) | Cells needed |
|---|---|---|---|---|---|
| Starter-heavy (MVP) | 10,000 | 1 | 10,000 | 20,000 | 2-4 |
| Blended (growth) | 5,000 | 3 | 15,000 | 30,000 | 3-6 |
| Pro-heavy (revenue) | 3,500 | 6 | 21,000 | 42,000 | 5-9 |
| **100k pods target** | **varies** | **varies** | **~50,000** | **100,000** | **10-20** |

**Critical unknown:** Actual apps/tenant distribution. Must be measured from real production data starting at L3 (100 tenants). Until then, use 500 tenants/cell as conservative planning number.

## Roadmap Items

### Phase A — Foundation

Items 01-03 run in parallel. They have no dependencies on each other. Together they fix the base.

| # | Item | What it does | Complexity | Unblocks |
|---|---|---|---|---|
| 01 | [L1 Defect Remediation](01-l1-defect-remediation/roadmap.md) | Fix 13 CORRECTIONS.txt defects. Prove 10-tenant 0% failure. | 3/5 | L1 at 100%, all subsequent items |
| 02 | [API Loop Extraction](02-api-loop-extraction/roadmap.md) | Extract 6 singleton loops into `loopzero-controller`. API becomes stateless. | 4/5 | Multi-replica API, L2+ |
| 03 | [Cell-Scoped Queries](03-cell-scoped-queries/roadmap.md) | Add `WHERE cell_id` to all reconciliation queries. | 3/5 | Multi-cell without cross-cell contamination |

### Phase B — Cell-Ready

Items depend on Phase A and on each other as shown in the dependency graph below. 04 and 07 are co-dependent: 04 defines capacity limits, 07 enforces them. They should be developed together.

| # | Item | What it does | Complexity | Dependencies | Unblocks |
|---|---|---|---|---|---|
| 04 | [Single-Cell Hardening](04-single-cell-hardening/roadmap.md) | Define cell capacity budget. Tune ArgoCD/Workflows/PG. Prove limits. | 4/5 | 01, 02, 03, **07** (enforcement) | L2-L3 |
| 07 | [Backpressure and Admission](07-backpressure-and-admission/roadmap.md) | Build queue admission, PG backpressure, graceful degradation. | 4/5 | 02, 03, **04** (budget defines thresholds) | L3+ without unpredictable failures |
| 05 | [Cell Provisioning](05-cell-provisioning/roadmap.md) | Implement cell.ts for real. Provision 2nd cell on DigitalOcean. | 5/5 | 01, 03 | Multi-cell existence |
| 06 | [Placement and Routing](06-placement-and-routing/roadmap.md) | Wire LeastLoadedEngine + StaticRouter. Tenants placed in cells. | 3/5 | 03, 05 | L4-L5 |

### Phase C — Validation

| # | Item | What it does | Complexity | Unblocks |
|---|---|---|---|---|
| 08 | [Progressive Load Validation](08-progressive-load-validation/roadmap.md) | Execute L2→L5 with failure injection. Prove the architecture works. | 4/5 | Evidence for 100k trajectory |

## Dependency Graph

```
Phase A (parallel):
  [01] l1-defect-remediation ──────────────┐
  [02] api-loop-extraction ────────────────┤
  [03] cell-scoped-queries ────────────────┤
                                           │
Phase B:                                   │
  [04] single-cell-hardening ←─────────────┤ (requires 01, 02, 03)
       ↕ co-dependent                      │
  [07] backpressure-and-admission ←────────┤ (requires 02, 03; co-develops with 04)
                                           │
  [05] cell-provisioning ←─────────────────┤ (requires 01, 03; parallel with 04/07)
  [06] placement-and-routing ←─────────────┘ (requires 03, 05)

Phase C:
  [08] progressive-load-validation ←───────── (requires ALL above)
```

**Note on 04↔07:** Item 04 defines cell capacity limits. Item 07 enforces them via admission control. You cannot prove "graceful degradation at 110% of budget" (04) without the admission mechanism (07). They must be developed as a pair, not sequentially.

## Load Test Level Mapping

Since items 04 and 07 are co-dependent (04 defines limits, 07 enforces them), both are required from L2 onward.

| Load Level | Tenants | Requires Items | Cost/mo |
|---|---|---|---|
| L1 (current, fix to 100%) | 10 | 01 | $350 |
| L2 | 50 | 01, 02, 03, 04+07 | ~$500 |
| L3 | 100 | 01, 02, 03, 04+07 | ~$800 |
| L4 | 500 | 01-07 (2 cells) | ~$2,500 |
| L5 | 1,000 | 01-08 (multi-cell) | ~$6,000 |

## Cost Projection

| Phase | Infra Delta | Revenue at Target |
|---|---|---|
| A (Foundation) | $0 | — |
| B (Cell-Ready, L3) | +$450/mo | 100 × $29 = $2,900/mo |
| B (Cell-Ready, L4) | +$2,150/mo | 500 × $29 = $14,500/mo |
| C (Validation, L5) | +$5,650/mo | ~$35,000/mo |

Unit economics positive at ~50 Pro tenants ($1,450/mo revenue vs ~$800 infra).

## Tracked Risks with Escalation Triggers

These are not separate roadmap items today. They become roadmap items when their trigger fires.

### Gitea Bottleneck

**Risk:** Gitea is a shared singleton. At 500 tenants = 1,000 repos, the load-test-plan (`docs/plans/load-test-plan.md:200`) identifies disk I/O as the expected bottleneck. ADR-0008 rates this LOW and defers to Phase 3.

**Trigger:** L4 load test (500 tenants). If Gitea write latency p99 > 2s OR Gitea OOM occurs during L4, this escalates to a roadmap item.

**Owner:** Infra Engineer
**Pre-trigger deliverable (due before L4 execution):**
1. Add Prometheus metric for git push latency (`loopzero_gitea_push_duration_seconds` histogram in gitops pusher)
2. Add Grafana alert: Gitea memory > 80% limit, git push p99 > 1s
3. Document Gitea HA evaluation in `docs/decisions/gitea-scaling-options.md` — evaluate HA, per-cell Gitea, and direct GitHub integration. This document must exist BEFORE L4 runs, even if the trigger hasn't fired. The options must be ready to execute, not researched after the bottleneck hits.

**Post-trigger execution plan:**
| Option | Complexity | Migration effort | Decision criteria |
|---|---|---|---|
| Gitea HA (clustering) | 3/5 | Zero — same repos, HA replicas | If bottleneck is memory/CPU, not disk I/O |
| Per-cell Gitea | 4/5 | Repo migration per tenant | If bottleneck is disk I/O and repo count |
| Direct GitHub integration | 5/5 | Remove Gitea entirely, rewrite source/gitops layers | If Gitea is fundamentally limiting AND GitHub App is already primary flow |

**Monitoring:** `gitea_repository_count`, Gitea pod memory, `loopzero_gitea_push_duration_seconds` (to be added).

### PostgreSQL Partitioning by cell_id

**Risk:** ADR-0008 mandates PG partitioning by `cell_id` (`docs/meetings/adr/0008-cell-based-architecture.md:139`). Item 03 (cell-scoped queries) adds `WHERE cell_id` filtering with denormalized column, which is sufficient for correctness. But at 5,000+ tenants, query performance on unpartitioned tables may degrade.

**Trigger:** L4 load test (500 tenants). If `FindPendingReleases` query latency p99 > 50ms under sustained load, this escalates.

**Owner:** API Engineer + DBA
**Pre-trigger deliverable (due before L4 execution):**
1. Item 03 already adds `cell_id` column to `builds` and `releases` (denormalization migration)
2. Add query latency histograms per store function: `loopzero_db_query_duration_seconds{function="FindPendingReleases"}` etc.
3. Write migration script (tested in staging, NOT applied): `ALTER TABLE builds PARTITION BY LIST (cell_id)` with default partition. Keep script at `api/internal/db/migrations/NNNN_partition_by_cell_id.up.sql` ready but not applied.

**Post-trigger execution plan:**
1. Apply partition migration during maintenance window (T2 meeting required)
2. Validate: `EXPLAIN ANALYZE` shows partition pruning for cell-scoped queries
3. Monitor: seq_scan count drops to zero for partitioned tables

**Monitoring:** `loopzero_db_query_duration_seconds` per function, `pg_stat_user_tables` seq_scan and idx_scan counts.

### PgBouncer Compatibility with Advisory Locks

**Risk:** The load-test-plan proposes PgBouncer at L4 (500 tenants) for connection multiplexing. But `pg_advisory_xact_lock` (Rule 7) requires session-aware routing. PgBouncer in transaction mode silently breaks advisory locks — builds hang or duplicate without clear error.

**Trigger:** Before L4 planning begins (item 08, L4 execution).

**Owner:** API Engineer + Infra Engineer
**Pre-trigger deliverable (due before L4 execution):**
1. Write compatibility analysis in `docs/decisions/pgbouncer-advisory-lock-compat.md` — test PgBouncer transaction-mode vs session-mode with `pg_advisory_xact_lock`. Document: does the lock survive connection return to pool? Test with real pgxpool + PgBouncer.
2. Prototype split-pool approach: direct PG connection for lock-acquiring code paths (`withEnvironmentLock`), PgBouncer for read-only queries. Measure connection savings.
3. Evaluate alternative: skip PgBouncer entirely, use DO PG scaling (larger instance tier) instead. Compare cost.

**Post-trigger execution plan:**

| Option | Complexity | Connection savings | Advisory lock safe? | Decision criteria |
|---|---|---|---|---|
| Session-mode PgBouncer | 2/5 | Low (~20% multiplex) | Yes | If connection count is moderate bottleneck |
| Split-pool (direct + PgBouncer) | 3/5 | Medium (~50% for reads) | Yes (locks bypass bouncer) | If reads dominate and locks are < 30% of traffic |
| Skip PgBouncer, scale PG tier | 1/5 | None | N/A | If DO PG tier increase is cheaper than PgBouncer operational cost |

**Decision required:** T2 meeting before item 08 L4 execution. Analysis document must exist and be reviewed.

**Monitoring:** `pg_stat_activity` connection count by state, `loopzero_lock_acquisition_duration_seconds` histogram.

## Test Infrastructure Status

This roadmap defines tests as DoD gates. Most do not exist today. Each item is responsible for implementing its own tests as part of the work, not as a separate effort.

| Item | Tests that exist today | Tests to implement |
|---|---|---|
| 01 | `TestL1_10Tenants`, `TestL1_WebhookBurst` | `TestManifestGate_*` (2 tests) |
| 02 | None | 8 tests (leader election, fencing, dedup, multi-replica, gitops ordering) |
| 03 | None | 6 tests (cell-scoped queries, cross-cell isolation, EXPLAIN) |
| 04 | `TestL2_50Tenants` (partial) | 4 tests (ArgoCD, PG pool, build queue, degradation) |
| 05 | None | 6 tests (provisioning, destroy, deploy, rollback, credential) |
| 06 | `TestPlacement_*` (unit, exist in placement_test.go) | 3 tests (TOCTOU, routing, backward compat) |
| 07 | None | 5 tests (admission, fairness, circuit breaker, degradation) |
| 08 | `TestL1_*` (partial), chaos suite (6 PIt, 0 implemented) | L2-L5 load tests, 6+ chaos tests |

**Rule:** No item is "done" until its tests pass. Tests are deliverables, not prerequisites.

## Governing Principles

From `docs/validation/2026-04-10-hard-message-to-team-100k-direction.md`:

Every solution must answer YES to all 6:

1. Continues valid with many cells?
2. Continues valid with multiple API replicas?
3. Continues valid under real DB/queue/reconciliation contention?
4. Continues valid when a cell fails?
5. Continues valid with thousands of tenants?
6. Continues valid without frequent manual intervention?

**If the answer is "no, but we'll refactor later" — the solution is wrong.**

## References

- [ADR-0008: Cell-Based Architecture](../meetings/adr/0008-cell-based-architecture.md)
- [Load Test Plan (L0-L7)](../plans/load-test-plan.md)
- [API Scaling Runbook](../runbooks/api-scaling.md)
- [Cell Operations Runbook](../runbooks/cell-operations.md)
- [L1 Corrections](../../infra/tests/load/CORRECTIONS.txt)
- [Hard Message to Team](../validation/2026-04-10-hard-message-to-team-100k-direction.md)
- [MEET-2026-04-10-046](../meetings/records/MEET-2026-04-10-046.md)
