# Phase 4 — Test Audit (FAANG-grade)

**Auditor:** test-auditor (Phase 4)
**Target:** `/home/paulo/loopzero/.claude/worktrees/glowing-growing-babbage`
**Scope:** 9 delivered dashboard journeys (J-04, J-05, J-07, J-08, J-09, J-10, J-17, J-22, J-23) + hot-paths surfaced by Phase 2 complexity-scanner and Phase 3 code-reviewer/maturity-detective.
**Inputs:** 123 prior findings, 33 test files inspected, full hot-path test-coverage cross-check.
**Outputs:** 33 `test_audit` rows + 24 new `findings` rows (ids 124–147).

---

## Top-line verdict

**The codebase has a quantitatively strong test pyramid but a qualitatively narrow one.** 567 Go test files + 184 TS test files exist; nearly every hot-path file has a corresponding `*_test.go`. The gap is not coverage volume, it is **branch depth + concurrency + state-machine STRICT contract enforcement**. Several CRITICAL hot-paths (CC > 27) ship with shallow happy-path tests + ZERO concurrency tests, and the contracts CLAUDE.md declares canonical (plan-cap REPLICAS_EXCEED_PLAN, panic on invalid state transition, env-lock on every mutation) are not enforced at the unit/integration layer.

**Six RED tests in main** mask regressions across `internal_secret_fetch` (J-22 transit chain) and `tenant_cr_manager` (M2 operator canonical fix). Both are HIGH-traffic LIVE-validated journeys. Per CLAUDE.md `feedback_post_feature_live_regression_workflow_2026_06_09`, RED in main violates test discipline.

**Three bug-class examples** prove the test gap shipped real bugs:
1. `cli/src/commands/login.ts:100` hardcodes `https://app.dev.loopzero.example` → no test asserts URL is config-driven (#31, finding 124 below).
2. `cloud/internal/routes/device.go:17` dead field `dashboardURL` → no test asserts the field is READ (#26 + #36, finding 125).
3. `cloud/dashboard/src/pages/secrets.tsx:25` hardcodes `[production, staging]` → no test creates a J-10 custom env (#64, finding 126).

All three are pillar-(b) wiring-triad violations (CLAUDE.md `feedback_endpoint_contract_narrow_consumer_audit`): feature has UNIT tests but no INTEGRATION test asserts the boundary between symbol and user-visible behavior.

---

## Pyramid metrics

| Layer | Count | Tool | Health |
|---|---|---|---|
| Go production sources | 572 | `find api operators cloud pkg -name '*.go' ! -name '*_test.go'` | — |
| Go test files | 567 | `find api operators cloud pkg -name '*_test.go'` | 0.99 ratio test:source |
| TS production sources | 220 | cli/src + cloud/dashboard/src | — |
| TS test files | 184 | `*.test.{ts,tsx}` | 0.84 ratio |
| envtest (integration) operators | 20 | `grep -l envtest operators/*_test.go` | OK |
| Ginkgo e2e | 34 files | `infra/tests/e2e` | OK |
| LIVE bash `dev-public-*_test.sh` | 15 | per-journey LIVE chain | OK (per sponsor mandate) |
| Concurrent-build integration | 1 file (`api/tests/integration/concurrent_builds_test.go`) | 4 tests — Test{Concurrent,Idempotency,LockReleased} | CANONICAL — DO NOT REGRESS |

**Pyramid is correct in shape** (many unit, moderate integration via envtest, few E2E + LIVE bash). The gap is **depth per package**.

---

## RED tests in main (6 total — CRITICAL)

```
go test ./api/internal/routes/... → 5 FAIL:
  TestSecretFetch_HappyPath                          (want 200, got 400)
  TestSecretFetch_EC07_CellTenantBindingMismatch_403 (want 403, got 400)
  TestSecretFetch_TenantNotFound_404                  (want 404, got 400)
  TestSecretFetch_SecretNotFound_404                  (want 404, got 400)
  TestSecretFetch_TenantBindingError_500             (want 500, got 400)

go test ./api/internal/k8s/... → 1 FAIL:
  TestTenantCRManager_RemoveEnvironment_OnlyRemovesMatchingProject
  (resourceVersion conflict — M2 canonical-fix EC-1 implicated)
```

Findings 124 (J-22 transit-chain regression mask) and 125 (M2 canonical fix race) cover them. **Either fix or remove before next release per Rule 11 (claims need evidence).**

---

## Hot-path test gap matrix (CC > 25 by finding rank)

| # | Function | CC | Tests | Status |
|---|---|---|---|---|
| #1 | `(Config).Validate` (config.go:510) | 46 | 28 | 41% branch gap → finding 145 (medium) |
| #2 | `main` (cmd/main.go:34) | 44 | implicit via `go build` | OUT OF SCOPE |
| #3 | `registerProtectedRoutes` (router.go:262) | 41 | implicit via every route test | OK |
| #4 | `(*StreamHandler).ServeHTTP` (sse) | 40 | 18 | NO concurrency test → finding 146 (medium) |
| #5 | `(*SecretHandler).Bulk` (J-22) | 37 | 25 (STRONG) | NO concurrency test → finding 133 (high) |
| #7 | `(*TenantReconciler).reconcileDelete` | 34 | 0 direct (only backoff helper) | **CRITICAL** → finding 128 |
| #8 | `(*SourceHandler).Upload` | 34 | 5 (shallow) | **CRITICAL** → finding 126 |
| #9 | `(*RepositoryHandler).Connect` (J-09) | 34 | 22 (STRONG) | OK |
| #11 | `phaseACreateRelease` (container_delivery) | 29 | 6 | Preview path missing → finding 140 (high) |
| #12 | `(*BuildHandler).Create` | 29 | 9 + integration | OK |
| #13 | `OnBuildCompleted` | 27 | 8 | LIVE failure-mode `wf.phase=Succeeded+clone-repo=Failed` not encoded |
| #14 | `SafeExtractTarGzReader` (tar.go) | 27 | 4 | **CRITICAL** — zero zip-bomb → finding 127 |
| #16 | `(*ScaleHandler).Scale` | 26 | 13 | NO plan-cap test → finding 132 (high) |
| #17 | `execute` (cli scale.ts) | 39 | 12 + integration | NO plan-cap → mirrors #16 |
| #20 | `ProjectsPage` (dashboard) | 32 | 4 | LIVE T5.3 caught 6 bugs tests missed → finding 138 (high) |
| #21 | `ProjectDetailPage` | 25 | **0 (no test file)** | finding (covered in audit row) |

---

## Top 15 risks (ordered by regression-cost × probability)

1. **5 RED tests in `internal_secret_fetch_test.go`** — J-22 LIVE-validated 2026-06-11, tests broken since (finding 124, CRITICAL).
2. **RED test in `tenant_cr_manager_test.go`** — M2 canonical fix race (finding 125, CRITICAL).
3. **`SourceHandler.Upload` (CC=34, branch coverage <30%)** — user upload boundary, concurrent + malformed + S3-failure paths uncovered (finding 126, CRITICAL).
4. **`SafeExtractTarGzReader` zero zip-bomb** — security trust boundary (finding 127, CRITICAL).
5. **`TenantReconciler.reconcileDelete` (CC=34) untested at orchestrator level** — finalizer leaks, stuck deletes (finding 128, CRITICAL).
6. **CLI login hardcoded URL #31** — bug shipped because no test asserts config-driven behavior (finding 129, HIGH).
7. **Device handler dead field #26/#36** — dead-wire shipped invisibly (finding 130, HIGH).
8. **`secrets.tsx` hardcoded env list #64** — J-22 silently breaks J-10 custom envs (finding 131, HIGH).
9. **Scale plan-cap REPLICAS_EXCEED_PLAN** — canonical B40 contract has ZERO test enforcement on API + CLI (finding 132, HIGH).
10. **`SecretHandler.Bulk` concurrency** — composite uniqueness invariant has no race test (finding 133, HIGH).
11. **`SourceHandler.Upload` concurrent path** — env-lock 1×202+1×409 contract missing at upload boundary (finding 134, HIGH).
12. **Preview + ExposedRoute state-machine STRICT contract not tested** — invalid-transitions panic contract is documented in CLAUDE.md but tests never assert it (finding 135, HIGH).
13. **`ProjectHandler.Create` non-transactional regression #46** — no rollback test (finding 136, HIGH).
14. **Stripe webhook order-independence not tested** — CLAUDE.md documents LIVE bug class (finding 137, HIGH).
15. **`projects.tsx` CC=32 with 4 tests after 6 LIVE bugs in T5.3 walkthrough** — feature lacked guardrail tests (finding 138, HIGH).

---

## Findings registered (ids 124–147)

| id | Severity | Title (short) |
|---|---|---|
| 124 | CRITICAL | 5 RED in `internal_secret_fetch_test.go` masking J-22 |
| 125 | CRITICAL | RED in `tenant_cr_manager_test.go` masking M2 canonical fix |
| 126 | CRITICAL | `SourceHandler.Upload` CC=34 with 5 superficial tests |
| 127 | CRITICAL | `SafeExtractTarGzReader` zero zip-bomb / DoS tests |
| 128 | CRITICAL | `TenantReconciler.reconcileDelete` CC=34 untested |
| 129 | HIGH | CLI login error URL hardcoded — bug #31 shipped because no test |
| 130 | HIGH | `device.dashboardURL` dead field — no test pins it READ |
| 131 | HIGH | `secrets.tsx` hardcoded env list — silent J-22 ↔ J-10 break |
| 132 | HIGH | Scale plan-cap REPLICAS_EXCEED_PLAN — ZERO test enforcement |
| 133 | HIGH | `SecretHandler.Bulk` no concurrency test |
| 134 | HIGH | `SourceHandler.Upload` no concurrent test |
| 135 | HIGH | Preview + ExposedRoute state-machine STRICT contract not tested |
| 136 | HIGH | `ProjectHandler.Create` rollback (#46) untested |
| 137 | HIGH | Stripe webhook order-independence not tested |
| 138 | HIGH | `projects.tsx` CC=32 with only 4 vitest tests |
| 139 | HIGH | `plan-limit.test.ts` misleading filename — scale plan-cap absent |
| 140 | HIGH | Container delivery preview-aware path no regression test |
| 141 | HIGH | `environment_ensurer` 3-tuple no canonical type test |
| 142 | MEDIUM | `source.go` has no dedicated `source_test.go` |
| 143 | MEDIUM | `cli/src/commands` has 0 co-located `.test.ts` |
| 144 | MEDIUM | cloud/internal/routes pyramid inversion (1 inline vs 28 in cloud/tests) |
| 145 | MEDIUM | `Config.Validate` 41% branch gap |
| 146 | MEDIUM | `StreamHandler.ServeHTTP` no concurrent stream stress test |
| 147 | MEDIUM | GitHub webhook signature handler no timing-attack test |

---

## Cross-references to existing findings

- **#1 (Config.Validate CC=46)** → finding 145 (this audit) — 28 tests for 48 branches.
- **#4 (StreamHandler ServeHTTP CC=40)** → finding 146 — no concurrency.
- **#5 (SecretHandler.Bulk CC=37)** → finding 133 — no race test.
- **#7 (TenantReconciler.reconcileDelete CC=34)** → finding 128 — ZERO direct test.
- **#8 (SourceHandler.Upload CC=34)** → finding 126 (depth) + 134 (concurrency) + 142 (filename hygiene).
- **#14 (SafeExtractTarGzReader CC=27)** → finding 127 — security DoS gap.
- **#16 + #17 (Scale)** → finding 132 + 139 — plan-cap.
- **#20 (projects.tsx CC=32)** → finding 138 — T5.3 walkthrough mismatch.
- **#26 + #36 (device dashboardURL dead)** → finding 130 — dead wire shipped.
- **#31 (login.ts dev URL)** → finding 129 — hardcoded URL shipped.
- **#46 (Project.Create non-transactional)** → finding 136 — no rollback test.
- **#58 (env tuple value-object missing)** → finding 141 — drift risk.
- **#64 (secrets.tsx hardcoded env list)** → finding 131 — J-22 ↔ J-10 silent break.

---

## What the existing test discipline does well

- **`api/tests/integration/concurrent_builds_test.go`** is the canonical reference for env-lock testing — keep as canonical.
- **`api/internal/github/webhook/handler_test.go`** is exemplary for signature validation — use as pattern.
- **`operators/internal/tenant/tenant_controller_test.go`** has 63 Ginkgo `It()` blocks — best-tested controller.
- **`api/internal/routes/secrets_test.go`** (J-22) has 25 tests including EC-01..EC-18 absorptions — strong harness; only concurrency missing.
- **Per-journey LIVE bash chain** (15 `dev-public-*_test.sh`) embodies the sponsor mandate `feedback_cleanroom_up_live_test_mandate_2026_06_10` — DO NOT regress.

---

## Recommended next actions (in order)

1. **HARD STOP:** Fix or remove 6 RED tests before next release. CLAUDE.md Rule 11 + memory `feedback_post_feature_live_regression_workflow_2026_06_09` forbid shipping with RED.
2. **Add zip-bomb + DoS protection** to `SafeExtractTarGzReader` AND tests (finding 127). Security-first per memory `feedback_security_first_mandate`.
3. **Wire-triad pillar (b) audit on all 9 journeys** — for every hardcoded literal (config URL, dashboard URL, env name), assert a test pins the configuration source.
4. **State-machine STRICT contract harness** — single table-driven test that enumerates every forbidden transition for every machine, asserts panic (defer recover).
5. **Concurrency-test harness** — extend `api/tests/integration/concurrent_builds_test.go` pattern to Secrets.Bulk, Source.Upload, Project.Create, Scale (plan-cap).
6. **Plan-cap enforcement** — encode B40 contract in unit + integration on both API and CLI side.

---

## Loop-back signal

The findings above suggest Phase 3 missed the **wiring-triad pillar (b)** audit class — features have implementations + unit tests but lack the boundary test that would have caught real bugs at write-time. Three concrete examples (login URL, device dashboardURL, secrets.tsx env list) shipped invisibly.

A targeted re-run of Phase 3 with explicit pillar-(b) lens (every public symbol must have a test asserting an OBSERVABLE behavior at the user boundary) would likely yield 10–20 additional findings of the same class across the 9 journeys.

<!-- LOOP_BACK_TO_CODE_REVIEW -->
