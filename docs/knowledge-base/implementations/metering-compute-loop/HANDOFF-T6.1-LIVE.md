# HANDOFF PROMPT — metering-compute-loop T6.1 LIVE validation

> Paste everything below the line into the fresh instance on the **resourced machine**.
> It is self-contained: role, state, the one task, the gates, the invariants, and the
> exact commands. Written 2026-07-02 by the implementing instance (local machine —
> could not host the full k3d platform, so LIVE was deferred per operator strategy).

---

## Your role

You are a Staff engineer resuming the `metering-compute-loop` plan on a machine with
enough resources to run the full 4-cluster k3d platform. **Everything up to T6.1 is
engineering-complete, committed, and pushed to `origin/develop`.** Your job is the
final milestone: **T6.1 — LIVE validation of the compute-metering producer** (multi-cell,
observe-only), then — only if it genuinely passes — emit the plan's completion promise.

Work language: pt-BR for prose to the sponsor; code/identifiers stay as-is.

## What this feature is (one paragraph)

An **observe-only** backend producer that runs on the leader-elected `loopzero-controller`.
Every ~60s it meters the **reserved** compute (CPU/mem from `pod.Spec.Resources.Requests`,
allocation-based — NOT live usage) of every **active** tenant, and writes
`cpu_core_seconds` / `mem_gb_seconds` into the existing `usage_records` (append-only →
Stripe) + `cost_counters` (mutable → 402 enforcement) per cell, feeding the billing
pipeline. **No enforcement / no 402 path at this stage** — it only records. It ships
**disabled** (kill-switch off); an operator flips it on after LIVE validation.

## State on `origin/develop` (pull first)

```bash
git fetch origin && git switch develop && git pull origin develop
git rev-parse --short develop   # expect eeae461f or later
```

- **T0–T2 (contracts, migrations, money-writer):** committed + PG-REAL validated (27 real-Postgres tests, incl. RLS twins under a genuine `loopzero_worker` role, `-race` at-most-once, period-split, SC-7 clamp).
- **T3.1 worker** (`api/internal/metering/worker.go`): `meterTenant` + `RunOnce` — committed, unit-tested with fakes.
- **T3.2 controller wiring — engineering-complete:** metrics (`b0114de0`), kill-switch reader (`23b5dcab`), namespaces (`5d9ff1a3`), pod-lister factory (`05550230`), loop tick (`04b321c5`), **3 remaining metrics S4b (`45304b45`)**, `SecretCellClientProvider` (`89a99121`), **composition-root wiring S5b (`0d6a3aa5`)**, SC-5 reaper GC (`169f3c64`).
- **Pillar-(a) is discharged statically:** `api/cmd/controller/main.go` → `startMeteringLoop` → `go loop.Run`. The producer has a real production caller on the elected leader.
- **NOT done:** T6.1 LIVE. `<promise>IMPLEMENTATION_COMPLETE</promise>` has **NOT** been emitted (would be false — Rule 3).

Full task table + SHAs: `knowledge-base/implementations/metering-compute-loop-implementation.md`.
Plan: `knowledge-base/plans/metering-compute-loop-plan.md`.

## The ONE task: T6.1 LIVE validation

**Pre-condition — INQUEBRÁVEL (`.claude/rules/cleanroom-up-live-test-mandate.md`):**
LIVE validation runs ONLY after **Cleanroom TOTAL → UP → sponsor JUNTOS**. Warm infra
does NOT count. Do NOT run the LIVE assertion against a warm cluster and call it done.

### Step 1 — Cleanroom TOTAL + UP (~40 min)

```bash
task clean:all         # destroy 4 k3d clusters + registry + docker network
task setup             # recreate from scratch (provides ≥2 cells + managed PG)
tilt up &              # dev loop
task doctor            # GATE: PASS=38+ FAIL=0 before proceeding
```

Capture a steady-state baseline before + after per `.claude/rules/steady-state-baseline.md`.

### Step 2 — Seed a multi-cell fleet with real pods

The producer only meters **active tenants that have pods with resource requests on their
cell**. You need ≥2 cells populated. Use the existing dogfood/journey tooling to create
tenants + deploy apps across cells (so `tenants.cell_id` spreads), OR the load harness for
the 1500-tenant scale target. Confirm cells are connected:

```bash
kubectl get clustercredentials -A          # ≥2, all Connected
# spot-check a tenant's pods have Spec.Resources.Requests set (allocation source)
```

### Step 3 — Enable the kill-switch (it ships OFF)

The producer embarks **observe-only disabled** (migration 000046 seeds
`metering_config.enabled = false`). Flip it on the controller's PG:

```sql
UPDATE metering_config SET enabled = true WHERE id = true;
```

The loop reads this every tick (`IsMeteringEnabled`); no controller restart needed —
the next tick (≤60s) starts metering.

### Step 4 — Assert LIVE (the T6.1 success criteria)

Watch the controller's Prometheus surface (all metrics are `loopzero_metering_*`):

| Metric | Expected LIVE | Meaning |
|---|---|---|
| `cpu_core_seconds_written_total` | **fires non-zero** within ~2 ticks of enabling | **pillar-(c)** — the wiring-triad proof: real tenants are being metered |
| `tenants_metered_total` | tracks the active-tenant count each pass (no persistent shortfall) | subset-miss detector — a lagging rate means a slice is silently skipped |
| `pass_duration_seconds` | p95 well under the 60s interval at 1500-tenant load | informer-ceiling — if p95 → interval, widen interval / shard the loop |
| `seconds_since_last_success` | hovers ~interval, never climbs past 3×interval | freshness — a climbing value = the loop is stuck |
| `cell_errors_total` | ~0 (occasional transient OK) | SC-3 read-failure holds; sustained non-zero = a cell is unreachable |
| `panics_total` | **0** | any panic is an alarm — the pass is not completing |

Then verify the money side landed correctly (multi-cell, at-most-once):

```sql
-- usage_records grew for metered tenants, one row per (tenant, metric, period, cell)
SELECT tenant_id, metric, cell_id, count(*) FROM usage_records
  WHERE metric IN ('cpu_core_seconds','mem_gb_seconds') GROUP BY 1,2,3 ORDER BY 1;
-- cost_counters advanced monotonically; the cursor never went backwards
SELECT tenant_id, metric, cell_id, cursor FROM metering_cursor ORDER BY 1;
```

**T6.1 passes when:** `cpu_core_seconds_written_total` is non-zero across ≥2 cells,
the money tables reflect it with no double-counting, the 1500-tenant pass completes
within one interval, `panics_total == 0`, and the **sponsor witnesses it JUNTOS**.

### Step 5 — Flip the checkbox / close the plan

Only after Step 4 genuinely passes with the sponsor present:
- Update `knowledge-base/implementations/metering-compute-loop-implementation.md` (T6.1 → committed, with the LIVE evidence path).
- Capture the post-validation steady-state baseline + evidence under `docs/operations/baselines/`.
- Emit `<promise>IMPLEMENTATION_COMPLETE</promise>` **only now**.

## Invariants that MUST hold (do not violate)

**Money-writer safety (the whole point of the design):**
- **SC-1** never decrement a counter (clock skew → HOLD, cursor unchanged).
- **SC-3** a cell/collect read failure HOLDS the cursor (never billed as Σreq=0) → `cell_errors_total`.
- **SC-6** the write is fenced by the leader lease `GenerationID` — a stale leader's writes reject.
- **SC-7** a stale-cursor catch-up is clamped to `MaxDelta` (1h < one billing period).
- At-most-once under concurrency (already `-race`-tested; the LIVE proof is no duplicate `usage_records`).

**Placement:** the tenant's cell is read from **canonical PG** (`GetCellForTenant`), NEVER the
Tenant CR mirror (INV I2). Pods are listed on the cell PG says the tenant lives on.

**Observe-only:** no enforcement, no 402. If you find yourself adding a plan-limit check,
STOP — that is out of scope for this plan.

**Git / process (INQUEBRÁVEL):**
- Work on `develop`. NEVER `main`. NEVER `git checkout` / `git revert` / `git reset --hard` / `git push --force`.
- NEVER a `Co-Authored-By` trailer on any commit.
- NEVER secrets in commits/logs/issues.
- Honest BLOCKED > false PASS. Do NOT emit the completion promise until T6.1 truly passes.

## If LIVE reveals a bug

Do NOT patch blindly. For each defect:
1. Write a **failing regression test first** (unit if it reproduces without a cluster; else document the LIVE repro).
2. Fix on `develop`, minimal change, keep the money-writer invariants.
3. Re-run Cleanroom TOTAL + the LIVE assertion (warm re-run does not count).
4. Commit atomically, reference the SC/EC it closes, update the CHANGELOG `[Unreleased]`.

The metering pkg is fully unit-tested — `cd api && go test -race ./internal/metering/`
should stay GREEN throughout. The controller composition root is
`api/cmd/controller/setup_metering.go` (observe-only, non-fatal — a metering wiring
failure logs + skips, never crashes the controller).

## Environment notes

- The dedicated **worker pool** connects as `loopzero_worker` via `LOOPZERO_WORKER_DATABASE_URL`
  (`config.EffectiveWorkerDatabaseURL()`) — physically separate from the API/controller
  pool so the RLS bypass in the money-writer is load-bearing. The full `task setup`
  provisions this; confirm the env is set for the controller.
- The mgmt client (reads Tenant + ClusterCredential CRs + kubeconfig Secrets) resolves
  via in-cluster config first, else `MgmtKubeconfig` / `MgmtKubeContext` from config.
- Constants (in `api/cmd/controller/setup_metering.go`): interval 60s, MaxDelta 1h,
  worker pool MaxConns 4, per-tenant fan-out concurrency 4.
- PG-REAL isolated tests (optional, without full k3d) need `LOOPZERO_E2E_DATABASE_URL`
  (owner) + `LOOPZERO_E2E_WORKER_DATABASE_URL` (genuine `loopzero_worker`, non-BYPASSRLS). The
  local `.metering-pg.env` is gitignored and did NOT transfer — set your own, or rely on
  the k3d-provided PG.

## Reference files

- Contract + task table: `knowledge-base/implementations/metering-compute-loop-implementation.md`
- Plan (v1.10): `knowledge-base/plans/metering-compute-loop-plan.md`
- LIVE mandate: `.claude/rules/cleanroom-up-live-test-mandate.md`
- Baseline rule: `.claude/rules/steady-state-baseline.md`
- Producer: `api/internal/metering/` (worker.go, metering_loop.go, metrics.go, wiring.go, secret_cell_client.go, pod_lister.go, collect.go, tenant_lister.go)
- Composition root: `api/cmd/controller/setup_metering.go` + `main.go`
- Money-writer: `api/internal/db/metering_wrapper.go` + `queries/cost.sql` + migrations 000046/000047/000048
