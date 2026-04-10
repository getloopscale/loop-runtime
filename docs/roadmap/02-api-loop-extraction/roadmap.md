# 02 — API Loop Extraction

**Phase:** A (Foundation)
**Complexity:** 4/5
**Dependencies:** None (parallel with 01, 03)
**Unblocks:** Multi-replica API, L2+
**Cost delta:** $0 (reuses existing mgmt cluster)
**Governance:** Pre-approved (MEET-046). No T2/T3 gate. Implement immediately via TDD. Validate on DigitalOcean.
**Status (2026-04-10):** Fencing primitives complete (leader generation package
+ PG-backed processing claims). Controller binary extraction, Helm chart, and
integration tests against real PG still pending.

---

## Problem

The API process runs 6 singleton background loops protected by `pg_try_advisory_lock` leader election. The API scaling runbook (`docs/runbooks/api-scaling.md`) explicitly states: **"Do NOT scale the API Deployment to 2+ replicas until reconcile loops are extracted."**

At 50+ concurrent requests, the leader pod handles HTTP traffic AND reconciliation simultaneously. This is a structural bottleneck that cannot be solved by adding CPU.

Loops to extract (from `api/cmd/main.go:440-520`):

1. `BuildReconcileLoop` — polls Argo Workflow CRDs, transitions build states
2. `BuildReaper` — enforces build timeout state machine transitions
3. `ReleaseReaper` — enforces release timeout state machine transitions
4. `ReleaseConvergenceLoop` — polls ArgoCD status, activates releases
5. `IdempotencyCleaner` — prunes expired idempotency keys
6. `SSEEventPruner` — removes old SSE events

Plus 3 additional loops (DomainProvisioner, DomainHealthChecker, CellMetrics).

## Scope

1. **New binary:** `cmd/controller/main.go` — runs all background loops with its own leader election
2. **API becomes stateless HTTP** — serves requests only, scales freely via HPA
3. **Helm chart / Deployment** for `loopzero-controller` (1 replica, leader-elected)
4. **Replace `sync.Map` deduplication** with PG-backed dedup (e.g., `processing_by` column with worker ID + timestamp) — `sync.Map` is process-local and lost on extraction
5. **Feature flag:** `LOOPZERO_LEGACY_LOOPS=true` on API for 2-week rollback window
6. **Integration test:** API + controller running simultaneously, zero duplicate processing

### Leader Election and Fencing Design

The current leader election uses `pg_try_advisory_lock` (session-scoped) on a dedicated connection (`api/cmd/main.go:536-570`). The runbook (`docs/runbooks/api-scaling.md:19`) documents this as session-scoped intentionally for process-lifetime semantics. This is acceptable for a single binary, but extraction introduces split-brain risk.

**Split-brain scenario:** PG connection flaps for 11-90s (within TCP keepalive detection). Old leader's connection appears alive to PG but is actually dead. New leader acquires lock on a new connection. Both believe they are leader. Old leader continues writing until kernel detects the dead connection.

**Fencing design (mandatory for this item):**

1. **Generation counter in PG.** `leader_generations` table with monotonically increasing `generation_id`. New leader increments on acquisition. Every side-effecting operation (gitops push, state transition) includes `WHERE leader_generation = $current`. Stale leader's writes silently fail the WHERE clause.

2. **Heartbeat with generation.** Controller heartbeats every 5s. Heartbeat checks `SELECT pg_try_advisory_lock($1)` AND `SELECT generation_id FROM leader_generations WHERE holder = $me`. If either fails, controller exits immediately (`os.Exit(1)`) — same as current behavior, but with generation validation.

3. **Gitops push includes generation trailer.** Commit to Gitea includes `Leader-Generation: N` trailer. If a stale leader pushes, the next leader's push overwrites with higher generation. ArgoCD syncs the latest commit (last-writer-wins is safe for gitops).

4. **PG-backed dedup replaces sync.Map.** `builds` table gets `processing_by TEXT, processing_since TIMESTAMP`. Controller sets `processing_by = controller-{pod-name}` before processing a build. If another controller instance finds `processing_by IS NOT NULL AND processing_since < now() - interval '2 min'`, it considers the build orphaned and takes over. This is crash-safe: if a controller dies mid-processing, the next leader detects the stale claim after 2 minutes.

**What this does NOT require:** Distributed consensus (Raft/Paxos). PG is the single coordination point. The generation counter is a simple monotonic sequence, not a distributed protocol.

### GitOps Ordering Contract

The fencing design above protects PG state transitions. But gitops pushes to Gitea are external side-effects with their own ordering semantics. The claim "last-writer-wins is safe for gitops" requires a formal contract:

**Contract: GitOps Push Ordering per (tenant, project, environment)**

1. **Serialization point:** `withEnvironmentLock(tenant, project, env)` (Rule 6). All gitops pushes for a given (tenant, project, env) are serialized by the advisory lock. Two controllers cannot push manifests for the same environment concurrently.

2. **Stale push safety:** If a stale leader pushes manifests after a new leader has already pushed newer manifests for the same environment:
   - The stale push creates a Gitea commit with an older `Leader-Generation` trailer.
   - The new leader's next reconciliation tick detects the divergence (git log shows older generation as HEAD) and re-pushes the correct manifests.
   - ArgoCD syncs to HEAD, which is the re-pushed correct state.
   - **Net effect:** Temporary divergence (one reconcile interval, ~10s), self-healing.

3. **Cancellation/rollback ordering:** Cancellation and rollback go through the state machine (`pkg/statemachine/`) which transitions the build/release state in PG first, then pushes manifests. The fencing generation on the PG transition is the ordering authority. If a stale leader attempts a cancellation after a new leader has already activated a release, the PG transition fails (generation check), and no gitops push occurs.

4. **Reaper ordering:** Reapers only transition builds/releases in timeout states. The transition is guarded by `WHERE leader_generation = $current`. A stale reaper cannot transition a build that a new leader has already claimed.

**What is NOT safe without the advisory lock:**
- Two controllers pushing manifests for the SAME environment simultaneously → merge conflict in Gitea, corrupt manifests. This is prevented by the environment lock, not by fencing alone.

**Test required:** `TestGitOpsPush_StaleLeaderOverwritten` — Simulate: leader A pushes manifests, leader B (higher generation) pushes newer manifests, leader A pushes again (stale). Assert: after one reconcile tick, HEAD matches leader B's state.

## Acceptance Criteria

1. **API binary contains zero background loops.** `grep -c "reconcileLoop\|reaper\|convergenceLoop" api/cmd/main.go` returns 0 (excluding comments).
2. **Controller runs as separate Deployment.** `kubectl get deployment loopzero-controller -n loopzero-system` shows 1/1 Ready.
3. **API scales to 4 replicas with zero duplicates.** 4 API replicas + 1 controller, 20 concurrent deploys. Each build transitions exactly once per state.
4. **Controller crash recovery works.** Kill controller pod. All RUNNING builds reach terminal state. No builds stuck.
5. **API p99 improves under load.** p99 with 4 replicas < p99 with 1 replica at same 20-deploy load.

## Required Tests

All tests below must be **implemented as part of this item**. None exist today.

| Test | Type | Exists? | What It Proves |
|---|---|---|---|
| `TestAPI_NoBackgroundLoops` | Unit | ✅ Implemented (2026-04-10) `tests/unit/api_no_loops_test.go` | Config flag disables loops; live-validated on DO (4 smoke deploys with loops OFF, HTTP 200, zero loop-start lines) |
| `TestController_LeaderElection` | Integration | ❌ Pending — needs real PG | Only 1 controller processes at a time |
| `TestController_CrashRecovery` | Integration | ❌ Pending — needs controller binary | Kill controller, builds still complete |
| `TestAPI_MultiReplica_NoDuplicateProcessing` | Load | ❌ Pending — needs DO + controller | 4 replicas, 20 deploys, zero duplicate transitions |
| `TestController_IdempotentTransitions` | Unit | ✅ Implemented (2026-04-10) `tests/unit/controller_idempotent_test.go` | Same build processed twice = no state corruption |
| `TestLeaderGeneration_StaleWriteRejected` | Unit | ✅ Implemented (2026-04-10) `internal/leader/generation_test.go` | Stale leader's transition fails via ErrFenced |
| `TestProcessingClaim_OrphanDetection` | Unit | ✅ Implemented (2026-04-10) `internal/processing/claim_store_test.go` | Orphaned processing_by detected after stale window |
| `TestGitOpsPush_StaleLeaderOverwritten` | Unit | ✅ Implemented (2026-04-10) `tests/unit/gitops_stale_leader_test.go` | Stale gitops push self-heals in one reconcile tick (in-memory git store, no Gitea needed) |

### Fencing primitives delivered (2026-04-10)

The leader-generation and PG-backed-claim primitives described in the design
sections above are now in the tree. The extracted controller binary will
compose them; until the binary exists, the API process can also adopt them
incrementally behind a feature flag.

**`api/internal/leader/generation.go`** — `Acquirer` with `Acquire`,
`Heartbeat`, `Validate`, `Release`. Lease includes `GenerationID` which must
be bound into every side-effecting write. 16 unit tests covering new
acquisition, live-peer rejection, stale-heartbeat takeover, fenced
heartbeat, fenced validate, wrong-holder release, default config, SQL
arg-order, and driver-error wrapping.

**`api/internal/processing/claim_store.go`** — `ClaimStore` with `Claim`,
`Release`, `FindOrphanedClaims` against the `builds` / `releases` tables.
Replaces `sync.Map` dedup with a PG-level CAS on `processing_by` /
`processing_since`. 12 unit tests.

**Migrations:**
- `000030_leader_generations.up.sql` — cell-scoped per-loop generation counter table
- `000031_build_release_processing_claim.up.sql` — processing_by/processing_since columns + partial indexes on builds and releases

## SLO Targets

| SLI | Target |
|---|---|
| API p99 (4 replicas, 20 concurrent deploys) | < 500ms |
| Duplicate state transitions | 0 |
| Controller failover time | < 15s |
| Build completion rate (controller restart during load) | 100% |

## Rollback Plan

1. Set `LOOPZERO_LEGACY_LOOPS=true` on API Deployment to re-enable loops in API.
2. Scale controller to 0 replicas.
3. MTTR: 10 minutes (env var change + pod restart).

## DoD

```bash
# 1. API has no background goroutines (verified by test, not just grep)
cd api && go test ./cmd/ -run TestAPI_NoBackgroundLoops -v
# PASS

# 2. Controller running with leader election
kubectl get deployment loopzero-controller -n loopzero-system -o jsonpath='{.status.readyReplicas}'
# 1
kubectl logs -l app=loopzero-controller -n loopzero-system --tail=5 | grep "leader lock acquired"
# Shows generation number

# 3. API scales to 4 replicas cleanly
kubectl scale deployment loopzero-api -n loopzero-system --replicas=4
kubectl rollout status deployment/loopzero-api -n loopzero-system --timeout=120s
# success

# 4. Zero duplicate processing under multi-replica API
cd api && go test ./tests/integration/ -run TestMultiReplica_NoDuplicateProcessing -v -timeout 10m
# PASS — verifies via SELECT COUNT(*) from build state transitions

# 5. Fencing generation counter works
cd api && go test ./internal/services/ -run TestLeaderGeneration_StaleWriteRejected -v
# PASS — stale leader's state transition fails WHERE clause

# 6. PG-backed dedup works
cd api && go test ./internal/services/ -run TestProcessingClaim_OrphanDetection -v
# PASS — orphaned processing_by claim detected after timeout

# 7. L1 still passes with new architecture
LOOPZERO_LOAD_TEST=1 go test ./infra/tests/load/ -v -run TestL1_10Tenants -timeout 30m
# 10/10 success
```
