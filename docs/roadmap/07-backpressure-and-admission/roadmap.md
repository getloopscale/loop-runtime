# 07 — Backpressure and Admission Control

**Phase:** B (Cell-Ready)
**Complexity:** 4/5
**Dependencies:** 02 (controller extraction), 03 (cell-scoped queries)
**Unblocks:** L3+ without unpredictable failure modes
**Cost delta:** $0 (code changes only)
**Governance:** Pre-approved (MEET-046). No T2/T3 gate. Implement immediately via TDD. Validate on DigitalOcean.
**Status (2026-04-10 iter 9):** Build admission gate and PG pool circuit
breaker shipped in `api/internal/admission/`. Wired into
`api/internal/routes/builds.go` Create handler behind the
`LOOPZERO_ADMISSION_CONTROL` feature flag (default off). Gate returns
HTTP 429 with `Retry-After` header when cell or per-tenant quota is
exceeded. Thresholds sourced from `docs/capacity/cell-budget.md`
(MaxConcurrentPerCell=20 matches the red line, MaxConcurrentPerTenant=3
matches BuildReconcileLoop's MaxBuildsPerTenant default). 22 unit tests
(11 gate + 6 breaker + 5 route adapter) + 3 config flag tests, all green.
Remaining: L3 load run + graceful degradation at 150% + attach the
breaker to mutation middlewares (currently it exposes Tripped() but no
middleware consults it yet).

---

## Problem

The system has no admission control for builds. At L3 (100 tenants), all 100 can submit builds simultaneously. Argo Workflows queues them but the API has no awareness of queue depth, no feedback to the user, and no per-tenant fairness. The rate limiter (`api/internal/middleware/ratelimit.go`) only limits HTTP requests, not build submissions.

Without backpressure, load tests above L2 produce unpredictable failure modes: builds pile up, PG pool saturates, ArgoCD sync queue grows unbounded, and the PENDING release queue (`LIMIT 100` in `FindPendingReleases`) silently drops releases.

## Scope

1. **Build admission gate:** Max N concurrent builds per cell, max M per tenant
2. **Queue depth metric** (Prometheus) + user-facing position in queue via SSE
3. **PG pool saturation circuit breaker:** Reject new mutations when pool > 90%
4. **Argo Workflows parallelism** per cell (already exists as CWT config, needs wiring per cell)
5. **Graceful 503 with Retry-After** when cell is at capacity
6. **PENDING release queue:** Replace `LIMIT 100` with ordered work queue with backpressure propagation
7. **Per-tenant build quota enforcement** (ties into plan limits)

## Acceptance Criteria

1. **Build submission rejected when cell at capacity.** API returns 429 with `Retry-After` and queue position.
2. **Per-tenant fairness.** 1 tenant submitting 50 builds does not starve 49 other tenants.
3. **PG pool circuit breaker fires at 90%.** New mutations return 503; reads continue working.
4. **Queue depth visible.** `loopzero_build_queue_depth` metric in Prometheus. Grafana alert at > 20.
5. **Graceful degradation proven.** Push to 150% of build capacity — system queues and drains, no crash.

## Required Tests

All tests below must be **implemented as part of this item**. None exist today.

| Test | Type | Exists? | What It Proves |
|---|---|---|---|
| `TestBuildAdmission_CellCapacityReject` | Unit | ✅ `TestCheck_CellAtCapacity_Rejects` (2026-04-10) | 429 when cell at max concurrent |
| `TestBuildAdmission_TenantFairness` | Unit | ✅ `TestCheck_TenantQuotaExceeded_WhenCellStillHasRoom` + `TestCheck_TenantBelowQuota_Admits` (2026-04-10) | Noisy tenant does not starve others |
| `TestPGCircuitBreaker_RejectAt90Percent` | Unit | ✅ Implemented (2026-04-10) | Breaker trips at exactly 90% saturation; hysteresis covered by `TestPGPoolBreaker_Hysteresis_HoldsUntilLowWater` |
| `TestGracefulDegradation_150Percent` | Load | ⏸ Pending — needs L3 infra run | System queues, doesn't crash |
| `TestRetryAfter_HeaderPresent` | Unit | ✅ `TestCheck_RetryAfter_ScalesWithSaturation` + `TestCheck_CellAtCapacity_Rejects` (2026-04-10) | 429 result carries a Retry-After hint; adapter + handler translate it to the HTTP header |
| `TestRouteAdapter_*` | Unit | ✅ 5 tests (2026-04-10) | Gate decisions map to the right LoopZeroError codes (CELL_AT_CAPACITY, TENANT_BUILD_QUOTA) |

## SLO Targets

| SLI | Target |
|---|---|
| Build rejection at capacity | 429 returned in < 100ms |
| Queue drain time (from full to empty) | < cell budget × avg build time |
| PG circuit breaker activation time | < 1s from threshold breach |
| False rejection rate (below capacity) | 0% |

## Rollback Plan

Admission control behind feature flag: `LOOPZERO_ADMISSION_CONTROL=false`. When disabled, behavior reverts to current (no admission, no queue awareness). MTTR: 5 minutes.

## DoD

```bash
# 1. Admission tests pass
cd api && go test ./internal/services/ -run TestBuildAdmission -v
cd api && go test ./internal/middleware/ -run TestPGCircuitBreaker -v

# 2. Queue depth metric exists
curl -s http://localhost:9090/api/v1/query?query=loopzero_build_queue_depth | jq '.data.result'
# Returns metric data

# 3. L3 works with admission control
LOOPZERO_LOAD_TEST=1 go test ./infra/tests/load/ -v -run TestL3_100Tenants -timeout 90m
# Success rate > 99.5%
```
