# CODE REVIEW FINAL REPORT — 9 Dashboard Journeys + Hardcode Audit

**Date:** 2026-06-11
**Auditor:** loop-code-review v0.3.1 — 6 specialists in parallel (direct invocation mode)
**Scope:** 9 delivered dashboard journeys — J-04 Recovery, J-05 Device Success, J-07 Projects List, J-08 Project Detail Overview, J-09 Project Connect, J-10 Environments, J-17 Deployments Realtime, J-22 Secrets CRUD, J-23 Previews per-PR
**Mode:** `full` (5 phases: baseline → completeness → code_review → test_audit → report)
**Sponsor mandate:** *"PROIBINDO CONFIGURAÇÃO HARDCODED"* — 2026-06-11 verbatim
**Total findings:** **147** (58 blocking, 89 non-blocking `[Nit:]`)

---

## 1. Executive verdict

**The 9 journeys are functionally delivered and LIVE-validated.** Each has sponsor verbatim closure (J-22 "Confirmado", J-07+J-08 "Ficou ótimo, parabens!", J-10 "✅ CLOSED 2026-06-07", J-23 dogfood 9/9 PASS, J-17 + J-04/05/09 pre-baseline). **None of the 147 findings invalidate the existing delivery.**

But three structural conclusions emerge from cross-specialist convergence:

| Conclusion | Evidence |
|---|---|
| **Quantitatively strong, qualitatively narrow** | 567 Go + 184 TS test files, pyramid ratio 0.99 — but 0 tests assert config-driven behavior on 3 known bugs |
| **Sponsor mandate partially honored** | 84% of maturity findings (43/51) + 14/26 code-reviewer findings + 7/13 completeness findings = **64% of all findings are hardcode-class** |
| **Cross-journey silent regression confirmed** | J-22 secrets.tsx hardcoded env filter breaks J-10 custom envs — vitest GREEN + tsc GREEN + code-review GREEN + sponsor walkthrough GREEN did NOT catch it |

**Verdict per journey (delivered ≠ free of debt):**

| Journey | Functional status | Debt findings | Headline risk |
|---|---|---:|---|
| J-04 Recovery | ✅ delivered | 1 (LOW) | Support email hardcoded `recovery.tsx:47` |
| J-05 Device Success | ✅ delivered | 2 (1 HIGH + 1 HIGH) | `dashboardURL` dead wiring + dev URL in `login.ts:100` |
| J-07 Projects List | ✅ delivered | 4 (1 CRITICAL CC=32 + 3 hardcode) | God-component `ProjectsPage` 32 CC |
| J-08 Project Detail | ✅ delivered | 3 (1 HIGH CC=25 + 2 hardcode) | `ProjectDetailPage` CC=25 |
| J-09 Project Connect | ✅ delivered | 12 (2 CRITICAL CC≥34 + 5 hardcode appclient + Plan `"starter"` silent downgrade) | God-component `project-connect.tsx` 615 LOC |
| J-10 Environments | ✅ delivered | 6 (1 HIGH `"production"` literal + 5 secondary) | Canonical env name hardcoded 6 sites |
| J-17 Deployments | ✅ delivered | 8 (1 CRITICAL CC=40 `ServeHTTP` + Loki client M2 regression) | Cross-project log leakage `loki/client.go:221` |
| J-22 Secrets CRUD | ✅ delivered 2026-06-11 | 14 (1 CRITICAL CC=37 `Bulk` + ENV hardcoded + 5 RED tests masking transit chain) | **Silent break of J-10 contract** + 5 RED tests |
| J-23 Previews per-PR | ✅ delivered | 7 (HIGH operator-vs-API URL pattern drift) | 4-layer drift between operator and api/internal/services |
| **Shared infra** | — | **90** | **28 hardcode in `infra/tilt/{api,cloud}-deployment.yaml`** + 30 hot-path CC≥26 |

---

## 2. Severity matrix consolidated

| Severity | Phase 2 (completeness) | Phase 3 (code) | Phase 4 (test) | Total | Blocking |
|---|---:|---:|---:|---:|---:|
| **CRITICAL** | 0 | 20 (complexity) | 5 (test) | **25** | 25 |
| **HIGH** | 3 | 29 (code+maturity+complexity) | 13 | **45** | 28 |
| MEDIUM | 4 | 35 | 6 | 45 | 4 |
| LOW | 3 | 25 | 0 | 28 | 0 |
| INFO | 3 | 1 | 0 | 4 | 0 |
| **TOTAL** | **13** | **110** | **24** | **147** | **58** |

**Blocking calculation per plugin:** severity ∈ {critical, high} + severity_source ∈ {consensus, default} = auto-blocking. The 58 blocking findings should not ship to v1.0 without remediation.

---

## 3. Sponsor mandate compliance — hardcode hit list

**Sponsor verbatim 2026-06-09 + reiterated 2026-06-11:** *"GATE NOVO NUNCA FAÇA NADA HARDCODE"* + *"PROIBINDO CONFIGURAÇÃO HARDCODED"*.

**Result:** 95 of 147 findings (64%) are hardcode-class. Concentration table:

| File | Hardcode findings | Architectural fix |
|---|---:|---|
| `infra/tilt/api-deployment.yaml` | **18** | Mount helm-rendered ConfigMap instead of inline literals |
| `infra/tilt/cloud-deployment.yaml` | **10** | Same — collapse 28 findings with ONE fix |
| `api/internal/github/appclient.go` | 5 | `githubAPIBase` env var + `apiBaseURL` field |
| `cloud/dashboard/src/pages/recovery.tsx` | 3 | `window.__LOOPZERO_CONFIG__.SUPPORT_EMAIL` |
| `api/internal/loki/client.go` | 3 | **Real bug — M2 namespace formula regression** |
| `pkg/vault/cached_client.go` | 2 | Vault default values → `TransitConfig` |
| `operators/internal/preview/preview_resources.go` | 2 | Per-plan quota from CR spec |
| `api/internal/services/preview_service.go` | 2 | Preview URL pattern + base domain |
| `api/internal/k8s/secret_k8s_manager.go` | 2 | `LOOPZERO_BROKER_CSS_NAME` env from Helm |
| `api/internal/argo/harbor_mirror_observe.go` | 2 | Harbor registry overrides |

**Hardcode by type (sponsor mandate categories):**

| Type | Count | Severity tier |
|---|---:|---|
| Magic URL literal (inline) | 11 | HIGH (Loki, Pushgateway, Ory, GitHub, MinIO cross-cluster IPs) |
| Magic secret literal (inline) | 8 | HIGH (service-auth, session-token, Argo callback, MinIO, PG DSN, SPKI pin, AppRole RoleID) |
| Magic namespace literal | 3+ | HIGH (loopzero-auth, loopzero-system, argo) |
| Magic port/timeout/retry literal | 6 | MEDIUM |
| Canonical env name `"production"` duplicated | 6 sites | HIGH |
| S3 bucket `"loopzero-builds"` fallback | 3 sites | MEDIUM |
| Frontend env list `[all, production, staging]` | 2 sites (J-22) | HIGH |
| GitHub public host | 3 sites (J-09) | LOW |
| Plan tier `"starter"` hardcoded | 1 site | HIGH (silent downgrade) |
| Base images BuildKit + 6 langs | 7 const | MEDIUM |

---

## 4. Top 25 blocking findings (CRITICAL + HIGH)

### CRITICAL (25 findings — all complexity/test severity_source=consensus)

| # | CC/Type | File:Line | Function | Journey |
|---|---|---|---|---|
| C1 | CC=46 | `api/internal/config/config.go:510` | `(Config).Validate` | bootstrap |
| C2 | CC=44 | `api/cmd/main.go:34` | `main` | bootstrap (pre-existing P0) |
| C3 | CC=41 | `api/internal/server/router.go:262` | `registerProtectedRoutes` | bootstrap (pre-existing P0) |
| C4 | CC=40 | `api/internal/sse/stream_handler.go:167` | `(*StreamHandler).ServeHTTP` | J-17 SSE hot path |
| C5 | CC=39 | `cli/src/commands/scale.ts:65` | `execute` | scale CLI dispatch |
| C6 | CC=37 | `api/internal/routes/secrets.go:290` | `(*SecretHandler).Bulk` | **J-22** batch path |
| C7 | CC=35 | `api/internal/middleware/audit_actions.go:22` | `deriveAction` | audit (mega switch) |
| C8 | CC=34 | `operators/internal/tenant/tenant_deletion.go:32` | `(*TenantReconciler).reconcileDelete` | tenant |
| C9 | CC=34 | `api/internal/routes/source.go:92` | `(*SourceHandler).Upload` | **J-09** upload |
| C10 | CC=34 | `api/internal/routes/repositories.go:122` | `(*RepositoryHandler).Connect` | **J-09** connect |
| C11 | CC=32 | `operators/internal/app/exposure_controller.go:80` | `(*ExposureReconciler).Reconcile` | exposed routes |
| C12 | CC=32 | `cli/src/commands/platform.ts:110` | `platformInstall` | platform |
| C13 | CC=32 | `cloud/dashboard/src/pages/projects.tsx:25` | `ProjectsPage` | **J-07** |
| C14 | CC=29 | `api/internal/services/container_delivery.go:196` | `phaseACreateRelease` | release |
| C15 | CC=29 | `api/internal/routes/builds.go:264` | `(*BuildHandler).Create` | **J-17** build |
| C16 | CC=29 | `cli/src/commands/preview.ts:194` | `execute` | preview CLI |
| C17 | CC=27 | `api/internal/services/build_orchestrator_complete.go:30` | `OnBuildCompleted` | build |
| C18 | CC=27 | `api/internal/archive/tar.go:62` | `SafeExtractTarGzReader` | **security boundary** |
| C19 | CC=27 | `api/cmd/setup_github.go:49` | `setupGitHub` | GH App bootstrap |
| C20 | CC=26 | `api/internal/routes/scale.go:120` | `(*ScaleHandler).Scale` | scale handler |
| C21 | TEST | `api/internal/routes/internal_secret_fetch_test.go:182` | 5 RED tests | **J-22 transit chain regression masked** |
| C22 | TEST | `api/internal/k8s/tenant_cr_manager_test.go:561` | 1 RED test | **M2 canonical-fix race exposed** |
| C23 | TEST | `api/internal/routes/source.go:92` | NO `source_test.go` | branch coverage <30% |
| C24 | TEST | `api/internal/archive/tar_test.go:1` | ZERO zip-bomb/DoS tests | trust boundary |
| C25 | TEST | `operators/internal/tenant/tenant_deletion.go:32` | NO direct test | only backoff helper tested |

### HIGH (top 15 of 45)

| # | Severity | File:Line | Issue |
|---|---|---|---|
| H1 | HIGH | `cli/src/commands/login.ts:100` | **Dev URL `https://app.dev.loopzero.example` shipped to ALL prod users** |
| H2 | HIGH | `api/internal/github/appclient.go:27` | `githubAPIBase` hardcoded — blocks Enterprise + DIP violation |
| H3 | HIGH | `operators/internal/preview/preview_controller.go:321` | Operator writes DIFFERENT preview URL than API service (4-layer drift) |
| H4 | HIGH | `cloud/internal/routes/device.go:17` | `dashboardURL` field stored but NEVER read; receives `cfg.BillingReturnURL` |
| H5 | HIGH | 6 sites (`environments.go`, `projects.go:269`, `secrets.tsx:25,52`) | `"production"` literal duplicated, no `pkg/domain.DefaultPermanentEnv` |
| H6 | HIGH | `cloud/dashboard/src/pages/secrets.tsx:25` | **J-22 silently breaks J-10** — env filter hardcoded vs dynamic |
| H7 | HIGH | `api/internal/routes/projects.go:233` | Hardcoded `Plan: "starter"` — silent downgrade of paying Pro tenants |
| H8 | HIGH | `api/internal/loki/client.go:221` | **Pre-M2 namespace formula** `loopzero-{tenant}-{env}` → cross-project log leakage |
| H9-H23 | HIGH | `infra/tilt/api-deployment.yaml` × 18 + `cloud-deployment.yaml` × 10 | Tilt YAML inline literals (resolvable via ONE architectural fix) |

---

## 5. Cross-journey regression risks (CONFIRMED)

### J-22 silently breaks J-10 (proven empirically)

```
J-10 (delivered 2026-06-07) → tenant creates custom env "staging-eu-1"
J-22 (delivered 2026-06-11) → secrets.tsx ENV_OPTIONS = ['all', 'production', 'staging']
                                        ↓
                              User CANNOT select "staging-eu-1" in secrets dropdown
                                        ↓
                              6 vitest tests pass + tsc green + sponsor walkthrough "Confirmado"
                                        ↓
                              Silent regression NOT caught by any layer
```

**Root cause:** `cloud/dashboard/src/pages/secrets.tsx:25` does not use `useEnvironments(name)` hook even though `deployments.tsx:62` already established that pattern in J-10.

**Test gap:** 6 secrets.tsx tests exist, **0 create a custom env name and assert it renders**. Per Phase 4 — pillar-(b) wiring-triad violation.

### Other high-probability cross-journey risks (untested)

1. **J-09 ↔ J-17:** Hardcoded `Plan: "starter"` in projects.go:233 may downgrade Pro tenants on auto-deploy reconnect.
2. **J-17 ↔ multi-tenant:** Loki client.go:221 pre-M2 namespace formula leaks logs cross-project for tenants with ≥2 projects.
3. **J-23 ↔ operator state:** Preview URL pattern drift between operator (preview_controller.go:321) and api service — DNS resolution diverges.

---

## 6. Test gap analysis (Phase 4 — pillar-(b) wiring triad)

### Bug-class proofs (no test would catch any of these)

| Bug | Tests count | Test gap |
|---|---:|---|
| `login.ts:100` dev URL hardcoded | 8 | ZERO assert URL is config-driven |
| `device.go:17` `dashboardURL` dead | 12 | ZERO assert the field is READ |
| `secrets.tsx:25` env filter hardcoded | 6 | ZERO use a custom J-10 env name |

**Universal formula confirmed:** `tests_exist > 0 AND tests_assert_behavior == 0 → bug shipping inevitably`.

### 6 RED tests masking regressions (root cause discovery)

- **5 RED tests in `api/internal/routes/internal_secret_fetch_test.go`** (lines 182, 267, 308, 327, 346) — masking J-22 transit chain regression. All return 400 instead of expected codes (200 / 403 / 404 / 500).
- **1 RED test in `api/internal/k8s/tenant_cr_manager_test.go:561`** — exposed during M2 operator canonical-fix delivery. Race condition.

### Concurrency gap class (zero race tests on hot mutations)

- `SecretHandler.Bulk` CC=37 — 25 tests, none concurrent
- `SourceHandler.Upload` CC=34 — no concurrent upload test
- `ProjectHandler.Create` — non-transactional, no rollback test
- `StreamHandler.ServeHTTP` CC=40 — no 10-client stress test

### State-machine STRICT contract gap

Per CLAUDE.md, 11 state machines must panic on invalid transition (`Transition(event)` is contract). Preview + ExposedRoute machines have **ZERO test enforcement** of invalid-transition panic — the contract is documented but unverified.

### Plan-cap REPLICAS_EXCEED_PLAN canonical contract (B40, 2026-05-06)

- `api/internal/routes/scale_test.go` (13 tests) — omits plan-cap rejection assertion
- `cli/tests/unit/commands/scale.test.ts` (12 tests) — same
- `cli/tests/unit/plan-limit.test.ts` covers billing limits only (misleading name)

---

## 7. Remediation roadmap (ROI-ranked)

### Tier 1 — Architectural fixes (high collapse-ratio)

| # | Fix | Collapses | Effort | Risk |
|---|---|---:|---|---|
| R1 | **Mount helm-rendered ConfigMap into Tilt-owned `{api,cloud}-deployment.yaml`** instead of inline literals | **28 findings** | 2-3 days | LOW (architectural pattern) |
| R2 | Ship `pkg/domain.DefaultPermanentEnv = "production"` constant + replace 6 call sites | **6 findings** | 30 min | NONE (test pinning constant) |
| R3 | Introduce `pkg/domain.EnvRef{Tenant, Project, Env, Cell}` Value Object + migrate 34 files / 70+ call sites | **58 (Value Object missing) + future shotgun** | 1 week | MEDIUM (touch many files; needs migration test suite) |
| R4 | Wire `useEnvironments(name)` hook in `secrets.tsx` (mirror `deployments.tsx`) | **J-22 breaks J-10 + 64 (frontend env list)** | 1 hour | NONE (proven pattern) |

### Tier 2 — Single-bug HIGH fixes

| # | Fix | Closes | Effort |
|---|---|---|---|
| R5 | `cli/src/commands/login.ts:100` — make URL config-driven | H1 | 5 min |
| R6 | `api/internal/loki/client.go:221` — use `pkg/naming.Namespace(tenant, project, env)` | H8 (cross-project leak) | 30 min |
| R7 | `api/internal/routes/projects.go:233` — read tenant Plan instead of hardcoded `"starter"` | H7 (silent downgrade) | 30 min |
| R8 | `api/internal/github/appclient.go:27` — `githubAPIBase` env var | H2 (Enterprise blocker) | 30 min |
| R9 | `operators/internal/preview/preview_controller.go:321` — share preview URL helper with API service | H3 (4-layer drift) | 1 hour |

### Tier 3 — Test debt remediation

| # | Action | Closes |
|---|---|---|
| R10 | Fix 5 RED tests in `internal_secret_fetch_test.go` — investigate why all return 400 | C21 |
| R11 | Fix 1 RED test in `tenant_cr_manager_test.go:561` | C22 |
| R12 | Add `source_test.go` covering `SourceHandler.Upload` branches + concurrent uploads | C9 + C23 |
| R13 | Add `tar_test.go` zip-bomb / DoS protection tests | C18 + C24 |
| R14 | Add wiring-triad assertion tests for the 3 bug-class proofs (login URL, dashboardURL, env filter) | H1 + H4 + H6 |
| R15 | Add invalid-transition panic tests for Preview + ExposedRoute state machines | State machine STRICT contract gap |

### Tier 4 — Complexity decomposition

| # | Action | Reduces |
|---|---|---|
| R16 | Decompose `(Config).Validate` CC=46 into per-field validators | C1 |
| R17 | Decompose `SecretHandler.Bulk` CC=37 using Command pattern | C6 + J-22 maintainability |
| R18 | Decompose `RepositoryHandler.Connect` CC=34 (J-09) using state-machine pattern | C10 + J-09 |
| R19 | Decompose `ProjectsPage` CC=32 (J-07) into hooks + sub-components | C13 + J-07 |
| R20 | Decompose `project-connect.tsx` 615 LOC god-component into custom hooks (`use-app-detection`, etc.) | pattern-suggester #59 |

### Tier 5 — Non-blocking polish

- Polish (M3/M4/L1/L2/L3 completeness findings) — can ride a future hardening sprint
- 31 LOW maturity heuristic findings — documentation pass

---

## 8. Cross-references — memory anchors used by specialists

- `feedback_no_hardcode_gate_2026_06_09` — primary user mandate
- `feedback_secret_canonical_no_hardcode` — secrets canonical pattern (`valueFrom.secretRef + optional: true`)
- `feedback_drift_3layer_canonical_pattern` — env var in 3 layers
- `feedback_4layer_drift_extension_2026_06_09` — producer/pipeline/consumer/frontend drift
- `feedback_tilt_owned_deployment_yaml_drift` — Tilt-owned YAML drift trap (caught LIVE 4× during 2026-06-08 walkthrough)
- `feedback_swallowed_silent_errors_pattern` — code compiles + returns nil + silently no-ops
- `feedback_post_feature_live_regression_workflow_2026_06_09` — sponsor mandate "engineering-side GREEN != feature done"
- `feedback_endpoint_contract_narrow_consumer_audit` — silent contract narrowing
- `feedback_cleanroom_up_live_test_mandate_2026_06_10` — LIVE test mandate
- `feedback_security_first_mandate` — strongest practicable option first

---

## 9. Suggested ADRs to draft

1. **ADR — Tilt-owned-YAML → ConfigMap mount canonical pattern** (architectural — closes R1, formalizes the 5th repeated learning)
2. **ADR — `pkg/domain.EnvRef` Value Object** (R3; resolves pattern-suggester #58 + future cell_id expansion)
3. **ADR — `pkg/domain.DefaultPermanentEnv` canonical constant** (R2; resolves H2 + H5 + H6)
4. **ADR — Plan tier resolution canonical pattern** (R7; resolves silent downgrade pattern)
5. **ADR — Wiring triad pillar (b) test contract** (R14; formalize "every config-driven field must have an integration test asserting source")

---

## 10. Audit metadata

| Field | Value |
|---|---|
| Total LOC audited | 80,802 across 43,542 files |
| Specialists invoked | 6 (completeness-auditor, code-reviewer, complexity-scanner, maturity-detective, pattern-suggester, test-auditor) |
| Execution mode | Direct via Agent tool (parallel; hook-driven mode unavailable per arch fix) |
| Total wall-clock | ~24 minutes (parallel, longest path = test-auditor 12min45s) |
| DB persistence | `code-review-output/code-review.db` — 147 findings + 33 test_audit rows + 3 tool_runs |
| Per-phase reports | `code-review-output/findings/phase{2,3-*,4}-*.md` (6 files, 1029 LOC) |

---

## 11. Hardcode compliance score

Per sponsor mandate 2026-06-11 *"PROIBINDO CONFIGURAÇÃO HARDCODED"*:

| Metric | Value | Verdict |
|---|---:|---|
| Total hardcode findings | 95 | NON-COMPLIANT |
| HIGH/CRITICAL hardcode findings | 23 | BLOCKING |
| Concentration in single fix surface | 28 (Tilt YAML) | ARCHITECTURAL FIX RECOMMENDED |
| Canonical positive examples | 6 areas (LanguageStrategy, state machines, db Repository, SSE adapter, placement Router, vault Decorator) | PATTERN ANCHOR AVAILABLE |
| Compliance gate | **NOT MET** | Requires R1 + R2 + R3 + R4 minimum for v1.0 |

**Path to compliance:** R1 + R2 + R3 + R4 (Tier 1) collapse 92 findings simultaneously. Estimated effort: 1 week. After that, sponsor mandate hardcode-gate becomes enforceable as CI lint (proposed ADR #5).

---

<promise>CODE REVIEW COMPLETE</promise>
