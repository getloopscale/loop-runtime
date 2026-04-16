# 08 — Progressive Load Validation

**Phase:** C (Validation)
**Complexity:** 4/5
**Dependencies:** ALL items 01-07
**Unblocks:** Evidence for 100k pod trajectory
**Cost delta:** Variable per level (see below)
**Governance:** Pre-approved (MEET-046). No T2/T3 gate. Implement immediately. All infra costs authorized — scale up for test windows, scale down after. Validate on DigitalOcean.

---

## Problem

Today the only evidence is L1 at 90% success rate. Every architectural decision is an assertion without proof. The load test plan exists (`docs/plans/load-test-plan.md`), the seed infrastructure exists (`infra/tests/load/`), but the execution is incomplete. Without progressive validation, the roadmap is narrative, not engineering.

The "hard message" directive: **"evidence before narrative."**

## Scope

Execute load tests L2 through L5, with **failure injection at every level**. Each level includes:

1. **Automated provisioning** of required infra (Pulumi config changes)
2. **Seed data** (synthetic tenants via API)
3. **Sustained load** at target rate
4. **Failure injection** (kill API pod, kill controller, PG connection storm)
5. **Metric collection** (Prometheus push gateway)
6. **Pass/fail** against numeric criteria
7. **Written report** with actual numbers (not aspirations)

### Level Definitions

| Level | Tenants | Rate | Duration | Cells | Chaos Scenarios |
|---|---|---|---|---|---|
| L2 | 50 | 5 deploys/min | 30 min | 1 | Kill API pod, kill controller |
| L3 | 100 | 10 deploys/min | 1 hour | 1 | + PG connection storm (fill pool 90%) |
| L4 | 500 | 50 deploys/min | 2 hours | 2 | + Cell ArgoCD failure, PG failover |
| L5 | 1,000 | 50 deploys/min | 4 hours | 2+ | + Cell network partition, tenant migration under load |

### Chaos Tests (implement 6 pending + new)

Chaos tests now have **two layers** (updated 2026-04-10):
1. **Ginkgo specs** in `infra/tests/chaos/chaos_test.go` — real `It()` blocks that `kubectl delete` pods and validate recovery via logs/probes.
2. **Chaos Mesh CRD manifests** in `infra/tests/chaos/manifests/` — declarative fault injection with steady-state PromQL hypotheses. Run via `task test:chaos:<name>`.

| Scenario | Ginkgo | Chaos Mesh | Run | What It Proves |
|---|---|---|---|---|
| Kill API pod during deploy | ✅ `It()` | ✅ `pod-kill-api-leader.yaml` | `task test:chaos:api-leader-kill` | Non-leader takes over < 90s, 0 restarts |
| Kill controller mid-reconcile | ✅ `It()` | ✅ `pod-kill-controller.yaml` | `task test:chaos:controller-kill` | Leader lease re-acquired, builds complete |
| PG connection storm | ✅ `It()` (metric check) | ✅ `pg-connection-storm.yaml` | `task test:chaos:pg-saturation` | Circuit breaker fires at 90%, reads continue |
| Cell ArgoCD crash | ✅ `It()` | ✅ `argocd-crash.yaml` | `task test:chaos:argocd-crash` | Running workloads unaffected |
| PG failover under load | ✅ `It()` (health check) | ⏸ Manual trigger via DO console | n/a | Recovery < 2min, zero lost builds |
| Cell network partition | ✅ `It()` (health check) | ⏸ Needs 2nd cell (Item 05) | n/a | Blast radius contained |
| Gitea outage | — | ✅ `gitea-outage.yaml` | `task test:chaos:gitea-outage` | Fast-fail, typed errors, no stuck state |

### Load Tests

| Test | Status | k6 Script | Run | Level |
|---|---|---|---|---|
| `TestL2_50Tenants` | ✅ k6 script ready | `l2-50-tenants.js` | `task test:load:l2` | L2 |
| `TestL3_100Tenants` | ✅ k6 script ready | `l3-100-tenants.js` | `task test:load:l3` | L3 |
| `TestL4_500Tenants_MultiCell` | ✅ Go skeleton + k6 TODO | `cell_scale_test.go` | `LOOPZERO_MULTI_CELL=1` | L4 |
| `TestL5_1000Tenants_FullScale` | ✅ Go skeleton + k6 TODO | `cell_scale_test.go` | `LOOPZERO_MULTI_CELL=1` | L5 |

## Acceptance Criteria (Per Level)

| Level | Success Rate | Deploy p99 | Recovery Time |
|---|---|---|---|
| L2 (50) | 100% | < 5 min | N/A |
| L3 (100) | > 99.5% | < 7 min | < 2 min |
| L4 (500) | > 99% | < 10 min | < 2 min |
| L5 (1000, multi-cell) | > 99% | < 10 min | < 5 min/cell |

Each level ALSO requires:
- All chaos scenarios pass (system recovers within SLO)
- Written report archived at `docs/load-tests/L{N}-YYYY-MM-DD.md`
- Report includes: actual success rate, p99, resource peaks, chaos results, bottleneck identified

## Cost

| Level | Delta from current | Total/mo | Test window cost |
|---|---|---|---|
| L2 | +$150 | ~$500 | ~$50 (1 week) |
| L3 | +$450 | ~$800 | ~$100 (2 weeks) |
| L4 | +$2,150 | ~$2,500 | ~$200 (1 week) |
| L5 | +$5,650 | ~$6,000 | ~$400 (2 weeks) |

Scale infra up for test window, scale back down after. Do NOT keep L4 infra running permanently.

## SLO per Level

| SLI | L2 | L3 | L4 | L5 |
|---|---|---|---|---|
| Success rate | 100% | > 99.5% | > 99% | > 99% |
| Deploy p99 | < 5 min | < 7 min | < 10 min | < 10 min |
| PG pool % | < 70% | < 75% | < 80% | < 80%/cell |
| Recovery time | N/A | < 2 min | < 2 min | < 5 min |
| Build queue depth peak | < 10 | < 20 | < 50 | < 50/cell |

## Rollback Plan

1. **Infra:** Each level's changes in Pulumi config. `pulumi config set` to previous, `pulumi up`. 5-10 min.
2. **Data cleanup:** `TestSeed_Cleanup` removes all loadtest-* data.
3. **PG sizing:** DO Managed PG resized without downtime. Scale down post-test.
4. **If a level fails:** Do NOT proceed. Fix bottleneck. Re-run. Report documents what broke.

## Gate Rules

- **No level declared "passed" without a written report with actual numbers.**
- **No aspirational reports.** Every metric backed by Grafana link or exported data.
- **No skipping levels.** L4 cannot start until L3 passes.
- **Chaos required.** A level without failure injection does not count as passed.

## DoD

```bash
# Per level (example L2):
LOOPZERO_LOAD_TEST=1 go test ./infra/tests/load/ -v -run TestL2_50Tenants -timeout 60m
# Success rate 100%

# Report exists with real data
test -f docs/load-tests/L2-$(date +%Y-%m-%d).md && echo "REPORT EXISTS"
grep "Success rate:" docs/load-tests/L2-*.md
grep "Deploy p99:" docs/load-tests/L2-*.md
grep "PG pool peak:" docs/load-tests/L2-*.md
# All return lines with real values, not "TBD"

# Chaos test passes
LOOPZERO_LOAD_TEST=1 go test ./infra/tests/chaos/ -v -run TestAPIKillDuringDeploy -timeout 15m
# System recovers, deploy completes
```

## What "Done" Means for the Entire Roadmap

When L5 passes with all chaos scenarios, the team can state:

> **LoopZero operates 1,000 tenants across 2+ cells with < 1% failure rate, < 10 min deploy p99, and contained blast radius. The architecture scales linearly: each cell adds ~500 tenants. 20 cells = 10,000 tenants = trajectory for 100,000 pods.**

This is not aspiration. It is evidence.
