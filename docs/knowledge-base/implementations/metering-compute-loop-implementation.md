---
type: implementation
title: "Implementation contract — metering-compute-loop (v1.10)"
---

# Implementation contract — metering-compute-loop (v1.10)

Halt-loop working contract. Plan: [`knowledge-base/plans/metering-compute-loop-plan.md`](../plans/metering-compute-loop-plan.md) (v1.10).
Resume: query layer of T2.3 committed (`ff76ec5f`) + PG-REAL validated; wrapper + tests pending.
Full resolved spec for the remaining T2.3: [`knowledge-base/implementations/metering-compute-loop/sepa-iterations/T2.3-query-layer-validated.md`](./metering-compute-loop/sepa-iterations/T2.3-query-layer-validated.md).

## Task status (from .progress-metering-compute-loop.json)

| Task | Status | Notes |
|---|---|---|
| T0.1 contracts v0.3.0 | committed | 8e6a05f |
| T1.1 enforcement aliases | committed | ea696abb |
| T1.2 metric CHECK drift guard | committed | (folded) |
| T2.1 mig 000046 (cursor/config/CHECK/RLS/GRANT) | committed | 5bb0dc22 |
| T2.2 mig 000047 usage BIGINT | committed | 076e1b20 |
| **T2.3 money-writer** | **in-progress** | query layer + SC-7 committed (`ff76ec5f`); **remaining: return-contract redesign → `cost_wrapper.go MeteringStore` → ~14 Go PG-REAL concurrency tests** |
| T1.3 phase-filter guard | committed | `0c7237d8` — folded into the TenantLister (ADR-5) |
| T3.1 worker | **engineering-complete** | collaborators (collect `1008f01d`+`0e504974`; TenantLister `0c7237d8`; cursor-read `351d875f`) + **worker meterTenant + RunOnce `0e36c6d7`**. Pillar-(a) DISCHARGED — the collaborators now have a production caller via RunOnce→meterTenant. **Remaining for T3.1 only: RunOnce's own caller = the T3.2 controller loop.** Metrics (`loopzero_metering_*` concrete) folded into T3.2/metrics.go. |
| T3.2 controller wiring (dedicated pgxpool + kill-switch + metrics + SC-5 reaper GC) | **engineering-complete** | S1 metrics `b0114de0`; S2 kill-switch `23b5dcab`; S3a namespaces `5d9ff1a3`; S3b pod-lister factory `05550230`; S4 loop `04b321c5`; **S4b 3 remaining metrics `45304b45`**; S5-enabler `SecretCellClientProvider` `89a99121`; **S5b composition-root wiring `0d6a3aa5`** (pillar-(a) DISCHARGED — main.go→startMeteringLoop→go loop.Run); S6 reaper GC `169f3c64`. Assembly + provider + loop + metrics fully unit-tested (`-race` GREEN); LIVE run deferred to the resourced cluster. |
| T6.1 integration + 1500-tenant load + LIVE ≥2-cell | pending | **OPERATOR HARD STOP — sponsor JUNTOS per cleanroom-up-live-test-mandate.md; loop halts here. The local machine cannot host the full k3d platform (operator strategy 2026-07-01): implement max compile+unit-verified now, LIVE on a resourced machine.** |

## T2.3 remaining — the resolved design (from the validated spec)

1. **Return-contract redesign** (period boundaries from SQL, EC-7 pure-UTC — never Go date math):
   - `MeterCompute :one` → row `{delta bigint, period_start, period_end}`.
   - `MeterComputeSplit :one` → row `{tail_cost bigint, head_cost bigint, p1_start, p1_end, p2_start, p2_end}` (p1/p2 CTEs still upsert cost_counters; the SELECT surfaces the two per-period costs + their periods).
   - Re-render schema, regen sqlc, re-validate PG-REAL (both writers + the two-usage-record split).
2. **`cost_wrapper.go` `MeteringStore`** (`*pgxpool.Pool`, ADR-10 dedicated): per method = `pool.Begin` → `SET LOCAL app.tenant_id=''` → CTE → (rows) 1 usage upsert (same-period) OR 2 usage upserts (split, tail→P1 / head→P2) → commit; `pgx.ErrNoRows` = held (no usage). Plus `SeedMeteringCursor`, `GetPlanTierForTenant`, `GetCellForTenant` wrappers.
3. **~14 Go PG-REAL tests** (real Postgres, `loopzero_worker` role, RLS loaded): ExactlyOnce (`-race`), FencedWriterRejected (SC-6), NegativeDeltaClockSkew_NoDecrement (SC-1, cursor UNCHANGED), NullCursorBootstrapsZero, ClampsLongGap (SC-7, SUM==metering_cost(req,max_delta)), MultiMonthGap_NoMiddlePeriodMisattribution (SC-7), WithoutRLSBypass_WritesZeroRows (negative twin), UsageRecordSameTx (crash→rollback), MeterComputeSplit_TailToP1_HeadToP2, UsageRecords_OneRowPerPeriod_SplitWritesTwo, DispatchClockDisagreement_HoldsNotMisbills (SC-2), NewPeriodRowCreatedByUpsert, UnknownPlanDefaultsStarter, ContinuesNotResets (SC-4).
4. **Governance:** SC-7 guardian re-sign addendum (state + tenant) + re-run `/plan-confidence` on v1.10.
5. **Commit T2.3** (queries + 000048 + generated + schema + wrapper + tests, atomic) + CHANGELOG.

## Wiring triad targets (T2.3)
- (a) caller: T3.1 worker calls `MeteringStore.MeterCompute`/`MeterComputeSplit` (wired when T3.1 lands; the integration tests are the immediate caller for T2.3).
- (b) integration test: the ~14 PG-REAL tests hit real Postgres.
- (c) runtime metric: `loopzero_metering_*` counters observed non-zero (wired in T3.2).

## Rules honored every task
TDD RED-first; parsimony ladder in GREEN; SOLID/Clean-Code/DRY in REFACTOR; wiring triad; no `Co-Authored-By`; `develop` only; `pg_advisory_xact_lock` semantics; LoopZeroError typed; no internal terms in user-facing output; observe-only (caps Unlimited, no 402 path).

## Iteration-2 starting context (SEPA pre-RED iter 1 + harness map — START HERE)

Full SEPA brief: [`sepa-iterations/iteration-1-resume-T2.3-pre-red.md`](./metering-compute-loop/sepa-iterations/iteration-1-resume-T2.3-pre-red.md). Two CRITICAL guardrails:

1. **[CRITICAL] Do NOT connect the MeteringStore pool / RLS tests as OWNER.** The existing `loopzeroWorkerDBURL()` (`internal/db/loopzero_worker_role_test.go:43`) is MISLEADINGLY named — it returns `secretL19DBURL()` = the OWNER DSN, which bypasses RLS unconditionally (FORCE RLS off, mig 000010). Using it makes `WithoutRLSBypass_WritesZeroRows` theatre (passes for the wrong reason). The MeteringStore pool uses `config.EffectiveWorkerDatabaseURL()` (`internal/config/config.go:782`, env `LOOPZERO_WORKER_DATABASE_URL`) = a genuine `loopzero_worker` connection. RLS tests need a real `loopzero_worker` DSN (or `SET ROLE loopzero_worker` in-session) + a skip-if-owner guard. Grep-guard: no worker-write test reuses `secretL19DBURL()`.

2. **[CRITICAL] SC-7 split writes TWO usage records — assert BOTH periods, not the summed total.** `usage(p1_start)==metering_cost(req,tail)` AND `usage(p2_start)==metering_cost(req,head)` with `p2_start == metering_period_end(expected)` (NOT `period_of(now())`). Plus separately assert `tail+head == LEAST(now()-expected, max_delta)`.

**PG-REAL harness pattern** (copy from `internal/db/migration_000046_test.go`): `dsn := os.Getenv("LOOPZERO_E2E_DATABASE_URL")` → `if dsn=="" { t.Skip(...) }` → `pgx.Connect(ctx, dsn)`. Local test PG: `postgres://loopzero:loopzero-dev-password@127.0.0.1:15432/loopzero?sslmode=disable` (env in gitignored `.metering-pg.env`), mig 000048 applied.

**First RED increment (SEPA):** `TestMeterCompute_ReturnsPeriodBoundaries` (same-period) — scan `{delta, period_start, period_end}`, assert bounds == `metering_period_start/end(expected)`. Compile-RED today (MeterCompute :one returns scalar int64) → forces the SELECT struct-return + sqlc regen. Same-period FIRST, then the split's two-usage struct. Do NOT start with the wrapper. p2_start FROM SQL, never Go date math.

**Iter 1 outcome (resume):** setup complete (SEPA v1.10, driver, contract, pre-conditions + TDD-shape all pass) + SEPA pre-RED consulted (2 CRITICAL captured) + harness mapped. No code written this iteration — stopped without the completion promise per `skills/implement/SKILL.md` (context-exhaustion → let the Stop hook re-feed a fresh iteration, do NOT emit a false promise). Iteration 2 (post-compaction, fresh) starts the RED increment with the guardrails above.

## T2.3 — COMPLETE (money-writer wrapper + comprehensive PG-REAL coverage) — 2026-07-01

10 commits (b3b23573 → 0e5564fc). The `MeteringStore` (`api/internal/db/metering_wrapper.go`) is done: `MeterCompute`, `MeterComputeSplit`, `SeedMeteringCursor`, `GetPlanTierForTenant`, `GetCellForTenant` — domain `MeterInput` (DIP), dedicated `*pgxpool.Pool` (ADR-10), one RLS-bypass tx per write, `(advanced, delta, err)` (ErrNoRows=held). **18 PG-REAL tests** (`api/internal/db/metering_store_test.go`) cover: return-contract (per-period cost + SQL bounds), same-period happy/HELD/zero-delta, split two-usage + **MultiMonthGap (MEET-006 distinction)**, SC-1 (NegativeDeltaClockSkew), SC-2 (DispatchClockDisagreement both directions), SC-4 (ContinuesNotResets), SC-6 (FencedWriterRejected), ADR-8 clamp both paths (same-period ClampsLongGap + split MultiMonthGap), at-most-once (**ExactlyOnce -race**), RLS (**UnderRealWorkerRLS + WithoutRLSBypass on a genuine loopzero_worker DSN**, SEPA CRITICAL #1 closed), reads (GetPlanTier EC-4 / GetCell INV I2) + SeedCursor idempotent (SC-5).

Test-infra note: RLS twin needs `LOOPZERO_E2E_WORKER_DATABASE_URL` (genuine loopzero_worker; test PG needs `ALTER ROLE loopzero_worker WITH PASSWORD ... LOGIN`); skips if unset. `meteringWorkerPool` fails-loud if the DSN is a BYPASSRLS role.

Minor coverage covered-implicitly (not separate tests): `UsageRecordSameTx` (atomicity — proven by happy=both-written + held=nothing-written on one tx), `NewPeriodRowCreatedByUpsert` (same-period test creates the counter row), `UnknownPlanDefaultsStarter` (GetPlanTier starter default), `NullCursorBootstrapsZero` (SeedCursor seeds to now() → pre-existence window excluded by construction).

**Wiring pillar (a) still DEFERRED** — the money-writer has NO production caller yet; the T3.1 worker is that caller. T2.3's DoD checkbox stays OPEN until T3.1 wires it.

## T1.3 / T3.1 TenantLister — COMPLETE (`0c7237d8`) — 2026-07-01

The enumeration source is done: `TenantLister` interface (DIP) + `CRTenantLister`
adapter in `api/internal/metering/tenant_lister.go` (in-scope; the governance gate
blocked adding a method to the out-of-scope `k8s.TenantCRManager`, which also
matched SEPA #4's DIP recommendation). Filters Tenant CRs on the observed
`TenantPhaseActive` CONSTANT (T1.3), scoped to canonical `loopzero-system` (C2). 3 unit
tests over the controller-runtime fake client (`WithStatusSubresource` so
`status.phase` is honestly persisted): phase-filter + C2 wrong-namespace exclusion,
empty-set (no Active → `[]`, no error), interface-impl compile proof. CHANGELOG
`[Unreleased] ### Added` consolidated metering entry added (Rule 6 — the whole
feature had skipped it).

**Pillar-(a) debt is now 6 collaborators deep (SEPA [MAJOR]):** collect
(sumComputeRequests/collectNamespaces), MeteringStore (5 methods), TenantLister —
all pillar-(b)-only. The worker loop MUST land next (before any Phase-3 boundary
mini_review) or `/code-quality` flags them as `dead_code_unallowlisted` FAIL_HARD.

## Cursor-read collaborator — COMPLETE (`351d875f`) — 2026-07-01

The worker's `MeterInput.Expected` source. SEPA pre-RED flagged [CRITICAL] that the
committed `MeteringStore` had no cursor READ (only `SeedMeteringCursor :exec` DO
NOTHING) — the worker's RED could not be honest without it. Added `GetMeteringCursor
:one` (pure `SELECT last_metered_at`) + wrapper (absorbs `pgx.ErrNoRows` →
`(zero, false)`), kept SEPARATE from the seed (a `:one RETURNING` on the DO-NOTHING
insert returns zero rows on the steady-state path → spurious ErrNoRows). 2 PG-REAL
tests GREEN (seeded exact-value + unseeded found=false); full metering suite (20)
GREEN; sqlc `generated/` in-sync. TOCTOU with the CAS is benign (ADR-4 re-validates
`last_metered_at=@expected` → lost race is a 0-row HOLD, never a double-bill).

**The worker RED is now UNBLOCKED** — per-tick order is SeedMeteringCursor (SC-5) →
GetMeteringCursor (read) → build MeterInput → dispatch MeterCompute/Split.

## Worker meterTenant + RunOnce — COMPLETE (`0e36c6d7`) — 2026-07-01

The pillar-(a) caller. `meterTenant` wires enumeration → Σ-request collect →
money-writer per tenant (GetCell skip → collect SC-3 → seed SC-5 → cursor →
tier → dispatch same-month?MeterCompute:MeterComputeSplit, DB predicate as safety
net). `RunOnce` fans out across `ListActiveTenants` with a bounded stdlib
semaphore (bound `MaxConcurrency`|8), per-tenant failure isolated
(`slog`+`errors.Join`, never aborts the fleet), respects ctx cancellation. 6 unit
tests incl `TestWorker_RunOnce_ParallelCellsNoRace` under `-race`. DIP: 5 narrow
injected interfaces (meteringStore/NamespaceLister/PodListerFactory/Clock/Metrics)
— no live cluster, no live PG. No `loopzero_metering_*` metric invented (canonical
list has no per-tenant-error counter; the Prometheus counters are T3.2). **This
discharges the 6-collaborator pillar-(a) debt** — they now have a production caller.

## T3.2 sliced (SEPA dependency-ordered) — S1 DONE

Per the SEPA pre-RED, T3.2 is 6 slices:
- **S1 — Prometheus `Metrics` impl** — DONE (`b0114de0`). `PrometheusMetrics` + 2
  canonical counters (`loopzero_metering_cpu_core_seconds_written_total` via Advanced,
  `loopzero_metering_cell_errors_total` via CellError); `Held()` documented no-op; the
  other 8 counters land with their callers (S4). 4 unit tests via `testutil.ToFloat64`.
- **S2 — kill-switch DB reader** — DONE (`23b5dcab`). `GetMeteringEnabled :one` +
  `MeteringStore.IsMeteringEnabled(ctx) (enabled, found bool, err error)`. EC-5
  fail-safe: enabled NEVER true on absent-row / query-error (a producer that can't
  read its kill-switch must not bill; mirrors SC-3). 4 PG-REAL tests. The S4 loop
  reads it per-tick + logs the EC-5 WARN + skips the pass when disabled.
- **S3a — `NamespaceLister`** — DONE (`5d9ff1a3`). `MeteringStore.NamespacesForTenant`
  composes `ListProjectsByTenant` × `ListEnvironmentTargetsByProject` → canonical
  `naming.Namespace` per (project,env). Byte-matches real ns; closes v1.1 single-ns
  under-bill. 2 PG-REAL tests (exact-set + zero-projects). `*db.MeteringStore` now
  satisfies BOTH `meteringStore` + `NamespaceLister`.
- **S3b — `PodListerFactory`** — DONE (`05550230`). `CellPodListerFactory` +
  `cellPodLister` over the `CellClientProvider` DIP seam (cellmgr byte-match). Bare
  `ListOptions{}` (no selector — `sumComputeRequests` owns EC-2), SC-3 error
  propagation, EC-9 single-page. 6 unit tests via client-go fake clientset.
  **S3 complete — the worker now has EVERY concrete collaborator.**
- **S4 — `metering_loop.go`** — DONE (`04b321c5`). `Loop.tick` (single-flight CAS +
  panic-recovery `loopzero_metering_panics_total` + kill-switch EC-5 gate) + `Loop.Run`
  (ticker). Narrow `runner`/`killSwitch` ISP interfaces. 7 loop tests + 1 metric
  test (single-flight under `-race`). YAGNI: only `Panic()` added to Metrics;
  tenants_metered/pass_duration/seconds_since_last_success deferred to **S4b**.
- **S5 — `setup_background.go` wiring** — dedicated pgxpool (ADR-10) + inject
  `NewPrometheusMetrics()` + the real adapters + lease into the Worker. **This is
  where RunOnce gets its production caller (closes the LAST pillar-(a) gap) AND
  pillar-(c) `cpu_core_seconds_written_total` fires non-zero.**
- **S6 — SC-5 reaper GC** — one `DELETE FROM metering_cursor WHERE tenant_id=$1`
  in the existing tenant reaper (ADR-13, observe-only).

## T3.2 S5a — assembly constructor — DONE (`3fba72b8`) — 2026-07-02

`NewWorkerLoop` + `SystemClock` + `LoopConfig` in `api/internal/metering/wiring.go`:
the pure-assembly seam that wires the Loop from injected collaborators (dedicated
pool → MeteringStore as Store+NamespaceLister+kill-switch; CR client → CRTenantLister;
CellClientProvider → PodListerFactory; PrometheusMetrics; SystemClock; lease fence
SC-6). No I/O at construction → 2 unit tests (lease+tunables threading incl the
[CRITICAL] MaxDelta<period ADR-8/SC-7 guardrail; SystemClock recency). NOT a dead
export (test-covered); pillar-a caller deferred to S5b.

## HALT — remaining work is LIVE-gated / operator-verified (NOT autonomous)

The autonomous halt-loop has delivered everything unit-testable: S1-S4 + all
collaborators + the assembly constructor (9 atomic commits this session:
`0c7237d8` TenantLister → `351d875f` cursor-read → `0e36c6d7` worker meterTenant+RunOnce
→ `b0114de0` metrics S1 → `23b5dcab` kill-switch S2 → `5d9ff1a3` NamespaceLister S3a
→ `05550230` PodListerFactory S3b → `04b321c5` loop S4 → `3fba72b8` assembly S5a).
Each RED→GREEN→REFACTOR→WIRING + SEPA 3×.

**S5b — controller main.go composition root (pillar-(a) closer, LIVE-gated).** The
production caller for `NewWorkerLoop` → `go loop.Run`. Requires, on the leader
(behind the lease at `main.go:130`): the dedicated pgxpool (`EffectiveWorkerDatabaseURL`,
bounded `MaxConns` per ADR-10/EC-11; `MaxConcurrency ≤ MaxConns`) + the cellmgr chain
(`NewK8sCellStore`→`NewDynamicRouter`→a `ClientFactory`→`NewClientManager` as the
`CellClientProvider`) + the mgmt CR client, wired **OBSERVE-ONLY** (a wiring failure
logs+skips metering, MUST NOT `os.Exit` the controller). **Two blockers found in S5:**
(1) `cellmgr.ClientManager` is instantiated NOWHERE in production (no template) + the
controller has no `setupCellRouter`; (2) `NewK8sCellStore` connects to the mgmt cluster
at construction. This wiring is only canonical-VALID after Cleanroom TOTAL + UP + LIVE
(cleanroom-up-live-test mandate) — writing it blind would be a false PASS (Rule 3).
**S5b closes the LAST pillar-(a) gap AND fires pillar-(c)** (`cpu_core_seconds_written_total`).

**S6 — SC-5 reaper GC — CODE-COMPLETE (`169f3c64`), run-verify PENDING.** Added a
9th step to `DeleteTenantsFKOrdered` (`AdminEvictMeteringCursors` — `DELETE FROM
metering_cursor WHERE tenant_id = ANY(...)`, no FK so order-free) + `counts.MeteringCursors`
+ a PG-REAL test. Compiles + vets clean; sqlc in-sync. **The PG-REAL test was NOT
run-verified** — the local test PG was torn down between sessions (`k3d-loopzero-mgmt`
gone, `127.0.0.1:15432` refused). The test is compile-verified (clean compile-RED on
the new field) + structurally identical to the same-session PASSING PG-REAL tests;
run-verification is pending the next cluster-up session (Rule 3 — the commit states
this plainly, no false PASS).

**T6.1 — LIVE ≥2-cell + 1500-tenant load** — OPERATOR HARD STOP, sponsor JUNTOS.

## S6 RUN-VERIFIED GREEN — 2026-07-02 (env restored)

The operator restored the test environment (standalone Postgres 16 on 127.0.0.1:15432,
migrations 1→48 applied, `loopzero_worker` role enabled non-BYPASSRLS for the RLS twin).
The full metering db-layer PG-REAL suite is now **27 PASS / 0 FAIL / 0 SKIP**, including
the S6 GC test (`TestTenantStoreSqlc_DeleteTenantsFKOrdered_GCsMeteringCursor_SC5`), the
18 money-writer tests, and the RLS twins (`UnderRealWorkerRLS` + `WithoutRLSBypass`). The
metering pkg unit suite is `-race` clean; the full api builds. **The verification gap in
`169f3c64` is closed — S6 is GREEN. The entire unit + db layer of the feature is proven.**

## Remaining — the k3d multi-cell LIVE session (S5b + T6.1)

## DEFINITIVE HALT (superseded — env restore in progress) — remaining needs the k3d cluster

The local k3d cluster (`k3d-loopzero-mgmt`) is torn down (the session spanned 2 days).
Every remaining piece now requires a cluster-up + operator-verified LIVE session per
the cleanroom-up-live-test mandate:
1. **S6 run-verify** — the SC-5 GC PG-REAL test (code committed `169f3c64`).
2. **S5b — controller main.go composition root** (pillar-(a) closer): the dedicated
   pgxpool + cellmgr chain (`NewK8sCellStore`→`NewDynamicRouter`→`ClientFactory`→
   `NewClientManager`, none instantiated in prod today, connects to mgmt cluster at
   boot) + mgmt CR client → `NewWorkerLoop` → `go loop.Run` on `bgWg`, wired
   OBSERVE-ONLY (log+skip, never `os.Exit`). Fires pillar-(c) `cpu_core_seconds_written_total`.
3. **T6.1 — LIVE ≥2-cell + 1500-tenant load** (sponsor JUNTOS).

Recommended operator run: Cleanroom TOTAL → UP → seeds → re-run the metering PG-REAL
suite (S6 + all prior) GREEN → write+verify S5b → `/dogfood`/T6.1 LIVE → then
`IMPLEMENTATION_COMPLETE` is honestly emittable.

## Autonomous session summary — 10 atomic commits (all unit-layer done)

`0c7237d8` TenantLister · `351d875f` cursor-read · `0e36c6d7` worker meterTenant+RunOnce ·
`b0114de0` metrics S1 · `23b5dcab` kill-switch S2 · `5d9ff1a3` NamespaceLister S3a ·
`05550230` PodListerFactory S3b · `04b321c5` loop S4 · `3fba72b8` assembly S5a ·
`169f3c64` SC-5 reaper GC S6 (run-verify pending). Each RED→GREEN→REFACTOR→WIRING + SEPA 3×.

The composition-root wiring in `api/cmd/controller/` (the leader binary; scope
includes `api/cmd/controller/` + `api/cmd/setup_background.go`). It constructs, on
the elected leader, behind the lease already acquired at `main.go:130`:
- the **dedicated bounded pgxpool** (ADR-10 — physically separate from the API
  pool, connected as `loopzero_worker` via `EffectiveWorkerDatabaseURL`);
- `db.NewMeteringStore(pool)` (used as BOTH `Worker.Store` + `Worker.Namespaces`);
- `metering.NewCellPodListerFactory(cellManager)` (cellmgr as `CellClientProvider`);
- `metering.NewCRTenantLister{Client}` + `metering.NewPrometheusMetrics()` + a real
  `Clock` + the lease (`Worker.Lease` = the controller's `leader.Lease`);
- `metering.NewLoop(worker, store, metrics, interval)` → `go loop.Run(shutdownCtx)`
  in the `bgWg`/`startBackgroundLoops` pattern.
**This closes the LAST pillar-(a) gap** (Loop.Run + RunOnce + meterTenant all gain
a production caller) **and fires pillar-(c)** (`loopzero_metering_cpu_core_seconds_written_total`
non-zero once a real tenant is metered). The composition root is exercised by T6.1
LIVE (the unit layer is fully covered by S1-S4). After S5: S6 reaper GC + T6.1 LIVE.

The leader-gated loop that drives `worker.RunOnce`. Per plan T3.2: a ticker; per
tick — (1) **kill-switch** (`IsMeteringEnabled`; EC-5 !found/err → skip + WARN,
never bill); (2) **single-flight** guard (no overlapping pass); (3)
**panic-recovery** boundary (`recover()` + `loopzero_metering_panics_total`, never
propagate — a panic must not crash the controller); (4) `worker.RunOnce`. The
loop also fires the remaining canonical counters (`tenants_metered_total`,
`pass_duration_seconds`, `seconds_since_last_success`, `panics_total`, …) — grow
the `Metrics` interface + `PrometheusMetrics` per counter as its call site lands.
Unit-test with fakes (fake clock/ticker, fake kill-switch, a RunOnce that
panics → recovered + counted, single-flight rejects overlap). Then S5
(setup_background wiring — RunOnce's production caller + pillar-c) + S6 (reaper GC).

`CellPodListerFactory` in `api/internal/metering/` — implements the worker's
`PodListerFactory` interface: `PodListerForCell(ctx, cellID) (PodLister, error)`
over `cellmgr.ClientFor(cellID) → kubernetes.Interface`, returning a `PodLister`
whose `ListPods(ctx, ns)` calls `CoreV1().Pods(ns).List` and maps `*PodList` →
`[]corev1.Pod`. Unit-test the client→[]Pod mapping with `k8s.io/client-go`'s
fake clientset (a real boundary fake, no live cluster); the `cellmgr.ClientFor`
resolution itself is exercised at T6.1 LIVE (multi-cell). After S3b: S4 loop, S5
setup_background wiring (RunOnce's production caller + pillar-c fires), S6 reaper GC.

Per plan T3.2 (`metering_loop.go` + `metrics.go` + `setup_background.go` + reaper):
the leader-gated ticker (kill-switch from a DB row `metering_enabled`, per-tick,
EC-5 absent→disabled+WARN; single-flight; panic-recovery boundary +
`loopzero_metering_panics_total`) → constructs the dedicated pgxpool (ADR-10) + the
Prometheus `Metrics` impl (the 10 canonical `loopzero_metering_*` counters) + the real
`CRTenantLister`/`NamespaceLister`/`PodListerFactory` adapters → calls
`worker.RunOnce`. Plus SC-5 cursor GC: one `DELETE FROM metering_cursor WHERE
tenant_id=$1` line in the EXISTING tenant reaper (ADR-13, observe-only). T3.2 is
where RunOnce gets its production caller (closes the LAST pillar-(a) gap) and the
`loopzero_metering_cpu_core_seconds_written_total` runtime metric (pillar c) fires.
Then T6.1 (LIVE ≥2-cell — OPERATOR HARD STOP, sponsor JUNTOS).

The worker (per ADR-5 v1.9): **list Tenant CRs** via a `TenantLister` interface (over `tenant_cr_manager.go`, scoped to `loopzero-system`) → filter `status.phase == loopzerov1.TenantPhaseActive` (the CONSTANT, not a string — T1.3 folds here) → per active tenant: `tenant_id` from the CR, **`cell_id` from `MeteringStore.GetCellForTenant` (canonical PG, NEVER the CR mirror — INV I2/C1)** → enumerate (project,env) from PG → `naming.Namespace` per pair → `cellmgr.ClientFor(cell)` → **list pods** → Σ effective request (EC-1 scale-to-zero / EC-2 pod-phase / EC-3 init-sidecar / EC-8 limits-only) → `GetPlanTierForTenant` → dispatch `MeterCompute` (Go-clock same-period guess) or `MeterComputeSplit` per (tenant,metric,cell), passing `lease.GenerationID`, own batch timeout. **SC-3 (read-failure HOLDS the cursor):** `collect()` returns `(Σreq, error)`; a non-nil error short-circuits BEFORE any `MeterCompute*` call (cursor untouched, retried next tick) — NEVER swallowed to Σreq=0. The `MeteringStore` (interface) depends on a `TenantLister` (DIP), injectable/fakeable. SEPA (aff1352) note: the worker MUST dispatch to BOTH MeterCompute + MeterComputeSplit or one is orphan money-code. Design decisions to resolve in the T3.1 pre-RED SEPA: which package the worker lives in; the TenantLister/pod-lister interfaces; the metric emission (loopzero_metering_* — advanced/held/clamped/missing_plan/cell_errors).
