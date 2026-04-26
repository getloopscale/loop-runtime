# Plan: Operators Deep Review Remediation

> **Version 1.0** — Close the 7 HIGH findings and the operationally critical MEDIUMs surfaced by the 7-phase deep review of `/home/paulo/loopzero/operators` (2026-04-26). The deep review found 0 criticals, 7 high, 21 medium, 16 low across 6 categories, plus 6 threats and 5 toxic combinations. This plan executes the remediation in dependency order, restoring C4 health from 2.83/4.0 to ≥ 3.5/4.0 and unblocking the path to maturity 4.0. Pre-release governance applies: T2/T3 changes must pass meeting + ADR before code.

## Context

The deep review (`/home/paulo/loopzero/review-output/final_report.md`, 717 lines) audited the operators module across baseline, completeness, architecture, code quality, infrastructure, security, and validation phases. Quality gates passed each phase (≥ 0.88). The finding distribution is bimodal: core provisioning (Tenant, Environment, PlatformBootstrap) is production-grade; peripheral systems (Preview, AppSecret, MinIO CB, alerting) are stubs or unwired.

**Concrete evidence motivating this plan:**

- **CODE-001 (HIGH):** `operators/internal/controller/tenant_integration_test.go:266` is currently FAILING. CLAUDE.md claims "41 Go packages passing" — drift confirmed by `go test`.
- **INFRA-001 (HIGH):** `charts/loopzero-tenant-controller/templates/clusterrole.yaml` grants cluster-wide `delete` on `core/v1 Secrets` (verified). Root dependency for TC-1, TC-4, TC-5.
- **VAL-001 (HIGH):** 12 of 47 alert rules reference Prometheus metrics never emitted by any Go file (21% dark zone). Names: `loopzero_break_glass_sessions_active`, `loopzero_build_egress_connectivity`, `loopzero_build_started_timestamp`, `loopzero_build_status`, `loopzero_fallback_active`, `loopzero_git_desired_state_divergence`, `loopzero_gitea_last_backup_timestamp`, `loopzero_git_mirror_status`, `loopzero_tenant_pods_pending_duration_seconds`, `loopzero_tenant_quota_usage_ratio`, plus name-mismatch on `loopzero_reconciler_errors_total` (OBS-001).
- **COMP-001 + COMP-008 (HIGH × 2):** Preview is doubly inoperable — `SetupPreviewController` not called in `cmd/main.go:setupControllers()` (lines 280-343); `wh.OnPullRequest` handler is `return nil` with TODO. Confirmed by VAL-006 end-to-end trace.
- **INFRA-000 (HIGH):** `.github/workflows/build-images.yml` triggers on the same `push` events as `ci.yml` with no `workflow_run` dependency. Failing tests can ship broken images to DOCR.
- **ARCH-001 (HIGH-leverage MEDIUM):** `operators/internal/minio/circuitbreaker_provisioner.go` exists but `cmd/main.go:208-231` returns the raw `AdminProvisioner`. Single-line wiring miss closes TC-3.

**Recurring strategic patterns (not isolated bugs):**

1. **"Implemented but not wired"** — 3 confirmed instances (PreviewController, MinIO CB, PR webhook). Process gap.
2. **"Dual-tier quality structure"** — bimodal: core exemplary, periphery stub. Creates false confidence.
3. **"Inverted test pyramid"** — 0 unit tests, 185 envtest. `internal/gitea/`, `internal/httpclient/` have zero coverage.
4. **"Documented intent ≠ runtime reality"** — CLAUDE.md described preview as "namespace+TTL only" when controller is fully unregistered.

**Reference documents:**
- Final report: `/home/paulo/loopzero/review-output/final_report.md`
- Threat models: `/home/paulo/loopzero/review-output/analysis/threat_models/threat_model_report.md`
- Invariants (5 ✓ / 10 ✗): `/home/paulo/loopzero/review-output/analysis/invariants.md`
- Database (queryable): `/home/paulo/loopzero/review-output/review.db`

## Objective

**Done = all 7 HIGH findings closed, INV-007 + INV-004 + INV-014 restored to validated, C4 health ≥ 3.5/4.0, `go test ./...` green on operators module, alert dark zone < 5%.**

Specific measurable goals:

1. `go test ./...` in `operators/` returns 0 failures (closes CODE-001).
2. `tenant-controller` ClusterRole grants `delete` on Secrets only via namespace-scoped Roles (closes INFRA-001 + TC-1 + TC-4 + TC-5).
3. Alert rules count where `expr:` metric exists in `prometheus.MustRegister` calls = 100% (closes VAL-001 + OBS-001).
4. `cmd/main.go:setupControllers()` calls `SetupPreviewController`; `OnPullRequest` returns HTTP 501 (closes COMP-001 + COMP-008 interim).
5. `setupMinIO()` returns `NewCircuitBreakerProvisioner(adminProv)` (closes ARCH-001 + TC-3).
6. `build-images.yml` waits on `ci.yml` success before pushing to DOCR (closes INFRA-000).
7. CI cross-references PrometheusRule `expr:` against Go metric registrations and fails on orphan (closes VAL-001 systemically).
8. Manager-bootstrap test asserts every exported `Setup*Controller` is called from `setupControllers` (closes Pattern 1 systemically).

## ADRs

### D1 — Pre-release governance applied per-task
**Decision:** Each task is classified T0/T1/T2/T3 per `.claude/rules/pre-release-governance.md`. T2/T3 require `/meeting` + ADR before any code.
**Rationale:** Inviolable rule in pre-release phase. Skipping it has burned the team (per memory `reference_audit_loop_session.md` — 7 audit fixes consecutively because earlier T2 changes shipped without review).
**Consequences:** Tasks T1.1 (RBAC refactor), T1.2 (alert rules removal), T1.5 (CI pipeline), T2.1 (PreviewController wiring) require meetings. Adds 30-60 min per task. T0/T1 (test fixes, single-line wiring) proceed direct.

### D2 — Test-first for every fix (CLAUDE.md Rule 1)
**Decision:** Every fix follows RED-GREEN-REFACTOR. Bug fixes get a regression test BEFORE the fix.
**Rationale:** CODE-001 itself is the proof — a failing test exposed a real production idempotency bug. Tests catch regressions; commitments without tests do not.
**Consequences:** Slower per-fix but eliminates regression class. Aligns with existing TDD culture.

### D3 — RBAC refactor follows kubebuilder-generated namespace pattern
**Decision:** For INFRA-001, decompose `loopzero-tenant-controller` ClusterRole into one ClusterRole (cluster-scoped resources) + 3 namespace-scoped Roles (loopzero-system, argocd, per-tenant). Model: `operators/config/rbac/platformbootstrap_role.yaml` (kubebuilder-generated).
**Rationale:** Pattern is already in-tree, kubebuilder-supported, and proven. Don't invent a new RBAC scheme.
**Consequences:** New chart files: `role-loopzero-system.yaml`, `role-argocd.yaml`, `role-tenant-template.yaml`. ClusterRole shrinks. ClusterRoleBinding subjects unchanged. controller-runtime cache scoped via `cache.Options.DefaultNamespaces` to avoid requiring cluster-wide list/watch on Secrets.

### D4 — Honest interim for inoperable Sprint 5 features
**Decision:** Until full Sprint 5 ships, `OnPullRequest` returns HTTP 501 + body `"Preview environments are in development. Track at #issue"`. Operator startup logs a WARNING when `SetupPreviewController` is registered but PreviewEnvironment workload deployment is incomplete.
**Rationale:** Silent no-op (current state) creates trust gap (TC-2). HTTP 501 is more honest and prevents developers from believing PR webhooks worked.
**Consequences:** GitHub App users see a clear "not implemented" response on every PR. CHANGELOG entry under `Changed` for v0.x. Eliminates TC-2 immediately.

### D5 — CI enforcement of "Implemented but not wired" pattern
**Decision:** Add two CI checks: (a) manager-bootstrap test that uses reflection or AST inspection to assert every exported `Setup*Controller` function under `operators/internal/controller/` is referenced in `cmd/main.go:setupControllers`. (b) Alert rule cross-reference: extract metric names from PrometheusRule `expr:` fields, grep against `prometheus.MustRegister` calls, fail CI on orphans.
**Rationale:** Pattern recurred 3 times. Process fix is cheaper than catching each instance manually. Recurring patterns are by definition systemic.
**Consequences:** Forces discipline. CI may break temporarily-stubbed work; that's a feature — temp stubs need explicit annotation (`//loopzero:audit-allowed-stub`).

### D6 — Defer full preview workload deployment to dedicated Sprint 5 work
**Decision:** This plan does NOT implement preview workload deployment (Application CR generation, build pipeline trigger, ArgoCD sync). It only wires the existing skeleton (COMP-001) and replaces the silent webhook (COMP-008) with HTTP 501.
**Rationale:** Sprint 5 is its own scope. Doing it inside the remediation plan blurs the audit closure with a feature delivery. Keep the remediation surgical.
**Consequences:** TC-2 not fully closed (preview still doesn't work) but trust gap is eliminated (users get 501, not silent success). Sprint 5 picks up the workload logic.

### D7 — Idempotent NetworkPolicy via controllerutil.CreateOrUpdate
**Decision:** Fix CODE-001 by replacing raw `Create` calls in EnvironmentController's `reconcileNetworkPolicies()` with `controllerutil.CreateOrUpdate`. Keep test, fix code.
**Rationale:** controller-runtime's idempotent upsert is the standard pattern; already used elsewhere in the codebase. CR-managed NP creation is the desired semantic.
**Consequences:** EnvironmentController spec-triggered reconciles produce stable NP counts. Test passes. INV-007 restored.

### D8 — MinIO CB wiring is one line, no new abstractions
**Decision:** ARCH-001 fix is `return minio.NewCircuitBreakerProvisioner(adminProv, cbConfig)` in `setupMinIO()`. No new types, no factory, no config refactor.
**Rationale:** YAGNI. The wrapper exists, the CB config is already shared. Adding indirection now is premature.
**Consequences:** Closes TC-3. INV-004 + INV-014 partially restored (still need test for missing-CB regression).

## Dependency Graph

```
Phase 0: Governance
  T0.1 Classify all P1 tasks (T-tier + meeting needs)
  T0.2 Hold meetings for T2/T3 tasks (D1 → D7)
  T0.3 Write ADRs (D1-D8 captured here; meeting outputs may add D9+)
                            │
                            ▼
Phase 1: P1 — 5 HIGH fixes (parallel within phase)
  ├── T1.1  INFRA-001  RBAC scope refactor          [T2 — meeting required]
  ├── T1.2  VAL-001    Remove dark alerts + impl 3   [T2 — meeting required]
  ├── T1.3  CODE-001   NP idempotency (RED→GREEN)   [T1 — direct]
  ├── T1.4  ARCH-001   Wire MinIO CB                [T1 — direct]
  └── T1.5  INFRA-000  CI workflow_run gate         [T2 — meeting required]
                            │
                            ▼
Phase 2: P2 — 9 fixes
  ├── T2.1  COMP-001   Wire SetupPreviewController   [T2 — depends on D6]
  ├── T2.2  COMP-008   PR webhook → HTTP 501         [T1 — direct]
  ├── T2.3  CODE-002   Replace panic() with error    [T1 — direct]
  ├── T2.4  CODE-006   Phase 1 delete error return   [T1 — direct]
  ├── T2.5  COMP-007   Invert TLS default            [T2 — security]
  ├── T2.6  OBS-001    Fix metric name mismatch      [T0 — trivial]
  ├── T2.7  VAL-002    Top 10 runbooks               [T1 — docs]
  ├── T2.8  INFRA-003  SHA-pin GitHub Actions        [T1 — supply chain]
  └── T2.9  COMP-005   Build NP egress allowlist     [T2 — security]
                            │
                            ▼
Phase 3: P3 — 8 fixes
  ├── T3.1  VAL-003    Unit tests for gitea/+httpclient/   [T1]
  ├── T3.2  INFRA-004  CI CRD schema drift check          [T1]
  ├── T3.3  INFRA-005  Trivy scan pre-push                [T2]
  ├── T3.4  CODE-004   madmin-go pin + integration test   [T1]
  ├── T3.5  SEC-002+3  CRD validation markers             [T1]
  ├── T3.6  ARCH-002   State machine for ExposedRoute+App [T2]
  ├── T3.7  INFRA-007  Distroless base image              [T1]
  └── T3.8  D5 (a)     Manager-bootstrap test (systemic)  [T1]
                            │
                            ▼
Phase 4: P4 — 6 fixes (parallel, low risk)
  ├── T4.1  ARCH-003   Typed condition constants
  ├── T4.2  CODE-003   Harbor httpclient consolidation
  ├── T4.3  CODE-005   ArgoCD server-side Apply
  ├── T4.4  INFRA-006  Tilt securityContext parity
  ├── T4.5  INFRA-008  Remove stale RBAC backward-compat
  └── T4.6  SEC-005    Per-cluster TLS bypass audit log
                            │
                            ▼
                    Final verification
  ├── go test ./... in operators/ green
  ├── INV-007, INV-004, INV-014 restored
  ├── C4 health re-scored ≥ 3.5/4.0
  └── /code-audit pass
```

**Parallelism rules:**
- Phase 0 is fully sequential.
- Within Phase 1, T1.3 and T1.4 can run in parallel with each other (independent files); T1.1, T1.2, T1.5 require meetings first but can run in parallel after.
- Within Phase 2, T2.6 (trivial) can ship immediately after Phase 1; T2.2 + T2.3 + T2.4 + T2.7 + T2.8 are independent.
- Phase 3 depends on Phase 2 (T3.6 builds on stabilized state machine pattern; T3.1 builds on closed COMP-007).
- Phase 4 is fully parallel — six low-risk tasks in any order.

---

## Phase 0: Governance

**Objective:** Apply pre-release governance gate before any T2/T3 implementation, producing decision records that authorize the rest of the plan.

### T0.1 — Classify all remediation tasks by T-tier

#### Objective
Produce a definitive T0/T1/T2/T3 classification for every task in this plan, identifying which require `/meeting` + ADR.

#### Evidence
`.claude/rules/pre-release-governance.md` mandates classification before implementation. RBAC, controller wiring, CI pipeline, and CRD validation changes are AUTOMATIC T2 triggers per the governance rules.

#### Files to edit
```
docs/plans/operators-deep-review-remediation-plan.md — add T-tier column to dependency graph (this file)
.claude/decisions/2026-04-26-operators-remediation-classification.md (NEW) — formal classification table
```

#### Deep file dependency analysis
- `operators-deep-review-remediation-plan.md`: this plan; classification updates the dependency graph table. No code change.
- `.claude/decisions/...`: standalone ADR; no downstream coupling.

#### Deep Dives
T-tier triggers from governance rules — relevant to this plan:
- **T2 automatic:** RBAC changes (T1.1, T1.5, T2.5, T2.9), controller reconcile logic (T2.1, T3.6), Helm chart modifications (T1.1), CI changes (T1.5, T3.2, T3.3), CRD changes (T3.5, T3.6), state machine (T3.6).
- **T1 (direct):** Code bug fixes (T1.3, T1.4, T2.3, T2.4, T2.6, T3.4, T4.x), test additions (T3.1, T3.8).
- **T3 (architecture):** None — all changes are local refactors. The "Manager-bootstrap test" (T3.8) is structural enough to be argued as T2.

Edge cases:
- Adding a single test (T1.3 RED phase) is T1 even though the GREEN phase modifies a controller. Reason: the bug fix is local.
- T2.7 (runbooks) is T1 — pure docs, no code.

#### Tasks
1. Read the full final report and confirm 7 HIGH + 21 MEDIUM + 16 LOW count via DB query.
2. For each task, mark T-tier in the dependency graph.
3. Write `.claude/decisions/2026-04-26-operators-remediation-classification.md` with rationale per task.
4. Identify the 8 T2 tasks that need meetings before implementation.

#### TDD
```
RED:   docs review — confirm classification matches governance rules
RED:   classification table covers every task in plan with no T-tier missing
GREEN: ADR written and reviewed by Decision Record Moderator agent
REFACTOR: None expected
VERIFY: grep "^| T" docs/plans/operators-deep-review-remediation-plan.md | grep -cE "T[0-3]" equals total task count
```

#### Acceptance Criteria
- [ ] Every task in this plan has an explicit T-tier annotation in the dependency graph.
- [ ] ADR `.claude/decisions/2026-04-26-operators-remediation-classification.md` exists and lists ≥ 8 T2/T3 tasks.
- [ ] Decision Record Moderator agent has reviewed (sign-off comment in ADR).

#### DoD
- [ ] Classification ADR committed to develop branch.
- [ ] T0.2 (meetings) has the list of meetings to convene.
- [ ] No T2/T3 implementation has started before T0.2.

### T0.2 — Hold meetings for T2/T3 tasks

#### Objective
Convene `/meeting` for each T2/T3 task identified in T0.1, producing decision records that authorize implementation.

#### Evidence
Pre-release governance: "T2/T3: STOP. Do not write code. Instead: produce diagnosis, invoke Change Governor for formal review, wait for meeting approval and validation plan."

#### Files to edit
```
.claude/decisions/2026-04-26-T1-1-rbac-scope.md (NEW) — INFRA-001 meeting outcome
.claude/decisions/2026-04-26-T1-2-dark-alerts.md (NEW) — VAL-001 meeting outcome
.claude/decisions/2026-04-26-T1-5-ci-gate.md (NEW) — INFRA-000 meeting outcome
.claude/decisions/2026-04-26-T2-1-preview-wiring.md (NEW) — COMP-001 meeting outcome
.claude/decisions/2026-04-26-T2-5-tls-default.md (NEW) — COMP-007 security meeting
.claude/decisions/2026-04-26-T2-9-build-np.md (NEW) — COMP-005 build NP allowlist
.claude/decisions/2026-04-26-T3-3-trivy.md (NEW) — INFRA-005 scan ordering
.claude/decisions/2026-04-26-T3-6-state-machines.md (NEW) — ARCH-002 SM expansion
```

#### Deep file dependency analysis
ADRs are atomic decision records; no code coupling. They become inputs to Phase 1-3 task implementations.

#### Deep Dives
Meeting roster per task type:
- **RBAC (T1.1):** platform-security-engineer (veto), security-blast-radius-reviewer (veto), k8s-controller-engineer, sre-engineer.
- **Alert rules (T1.2):** sre-engineer (veto on observability), production-design-reviewer.
- **CI gate (T1.5):** gitops-engineer, dx-engineer, verification-architect.
- **Preview wiring (T2.1):** principal-architect, k8s-controller-engineer, api-domain-architect.
- **TLS default (T2.5):** platform-security-engineer (veto), security-blast-radius-reviewer (veto).
- **Build NP allowlist (T2.9):** platform-security-engineer, k8s-controller-engineer, distributed-systems-engineer.
- **Trivy ordering (T3.3):** gitops-engineer, platform-security-engineer.
- **State machine expansion (T3.6):** principal-architect, k8s-controller-engineer.

Each meeting MUST produce: alternatives compared, objective criteria, explicit trade-offs, owner, validation plan. Format per `decision-record-moderator` agent.

#### Tasks
1. For each T2/T3 task, run `/meeting <topic>` with the rostered specialists.
2. Capture meeting record via `decision-record-moderator`.
3. Verify each record has: decision, rationale, alternatives, trade-offs, validation plan.
4. Block any T2/T3 implementation until corresponding ADR is APPROVED.

#### TDD
```
RED:   ADR file does not exist OR lacks alternatives section
GREEN: ADR file exists with all required sections, signed off by required vetoes
REFACTOR: None
VERIFY: ls .claude/decisions/2026-04-26-T*.md | wc -l == 8 (one per T2/T3)
```

#### Acceptance Criteria
- [ ] 8 ADRs created (one per T2/T3 task).
- [ ] Every ADR includes alternatives, rationale, trade-offs, validation plan.
- [ ] Security/SRE vetoes addressed where applicable (no unresolved blocks).
- [ ] Each ADR explicitly authorizes the corresponding implementation task.

#### DoD
- [ ] All 8 ADRs committed to develop branch.
- [ ] No outstanding veto blocks.
- [ ] Phase 1 tasks unblocked.

### T0.3 — Capture additional ADRs from meeting outputs

#### Objective
Append D9+ ADRs to this plan if meeting decisions in T0.2 produce architectural choices not anticipated in D1-D8.

#### Evidence
Meetings often surface implementation details that constrain other tasks. Capturing them in the plan ADRs section prevents drift.

#### Files to edit
```
docs/plans/operators-deep-review-remediation-plan.md — append D9+ if needed
```

#### Deep file dependency analysis
Plan-internal; no code.

#### Deep Dives
Likely additional ADRs from T0.2 meetings:
- D9 candidate: cache.Options.DefaultNamespaces vs explicit Watch filter (RBAC meeting output).
- D10 candidate: alert removal strategy (delete vs comment-out vs feature-flag).
- D11 candidate: CI gate via `workflow_run` vs in-job test step (build-images meeting output).

#### Tasks
1. After T0.2 completes, read all 8 ADRs.
2. For each architectural decision not already in D1-D8, append a new D-numbered ADR to this plan.
3. Update task references in Phase 1-3 to cite new ADRs where relevant.

#### TDD
```
RED:   Plan ADR section is missing D9+ when corresponding meeting introduced new decisions
GREEN: Plan ADR section captures every meeting-derived decision
REFACTOR: None
VERIFY: Cross-reference each .claude/decisions/2026-04-26-T*.md against plan ADR section; every meeting decision is either in D1-D8 or in D9+
```

#### Acceptance Criteria
- [ ] Plan ADR section reflects all meeting outcomes from T0.2.
- [ ] Every Phase 1-3 task references the controlling ADR(s).

#### DoD
- [ ] Plan committed with updated ADRs.
- [ ] Phase 1 unblocked.

---

## Phase 1: P1 — Five HIGH Fixes

**Objective:** Close all 5 P1 HIGH findings (INFRA-001, VAL-001, CODE-001, ARCH-001, INFRA-000) — within 1 week. Each fix simultaneously closes one or more invariants and toxic combinations.

### T1.1 — INFRA-001: Refactor ClusterRole to namespace-scoped Roles for Secrets

#### Objective
Eliminate cluster-wide `delete` on `core/v1 Secrets` by decomposing the `loopzero-tenant-controller` ClusterRole into one cluster-scoped ClusterRole (no Secrets) + namespace-scoped Roles (loopzero-system, argocd, per-tenant).

#### Evidence
- `charts/loopzero-tenant-controller/templates/clusterrole.yaml` — verified contains rule with `verbs: ["get","list","watch","create","update","delete"]` on `secrets`, no resourceNames or namespace restriction.
- `operators/config/rbac/platformbootstrap_role.yaml` — kubebuilder-generated namespace-scoped pattern (reference per D3).
- INFRA-001 is the root dependency for TC-1 (crash-loop + destructive RBAC), TC-4 (rogue CR namespace overwrite), TC-5 (undetectable ArgoCD key theft). Closing it eliminates 3 toxic combinations.

#### Files to edit
```
charts/loopzero-tenant-controller/templates/clusterrole.yaml — remove Secrets rule; remove delete verb cluster-wide
charts/loopzero-tenant-controller/templates/role-loopzero-system.yaml (NEW) — Role + RoleBinding scoped to loopzero-system, get/create/update on platform-tokens Secret only
charts/loopzero-tenant-controller/templates/role-argocd.yaml (NEW) — Role + RoleBinding scoped to argocd, get/create/update on argocd-repo-* Secrets
charts/loopzero-tenant-controller/templates/role-tenant.yaml (NEW) — Role template applied per tenant namespace via post-install hook or controller code
operators/cmd/main.go — set cache.Options.DefaultNamespaces or scoped Watch for Secrets to avoid cluster-wide list/watch
operators/internal/controller/tenant_controller.go — verify Secret operations target only owned namespaces
```

#### Deep file dependency analysis
- `clusterrole.yaml`: source of all operator RBAC. Removing the Secrets rule means controllers will receive 403 Forbidden on cluster-wide Secret operations. Code already targets specific namespaces, so this should be a no-op for correct code paths.
- New Role files: install-time created; bound to operator ServiceAccount per namespace. ClusterRoleBinding subjects unchanged.
- `cmd/main.go`: controller-runtime cache currently watches Secrets cluster-wide. Must scope to `loopzero-system, argocd, loopzero-{tenant}-*` patterns. Otherwise list/watch cluster-wide will 403.
- `tenant_controller.go`: any unscoped Secret create/update/delete is a runtime regression risk. Must audit.

Downstream files affected:
- `charts/loopzero-tenant-controller/values.yaml` — may need new `rbac.scopedSecretAccess: true` flag for backward-compat fallback.
- `operators/internal/controller/tenant_credentials.go` — Secret CRUD operations; must verify namespace targeting.
- `operators/internal/controller/platformbootstrap_argocd.go` — argocd-repo-* Secret writes.

#### Deep Dives
**Cache scoping (controller-runtime):**
```go
mgr, err := ctrl.NewManager(cfg, ctrl.Options{
  Cache: cache.Options{
    ByObject: map[client.Object]cache.ByObject{
      &corev1.Secret{}: {
        Namespaces: map[string]cache.Config{
          "loopzero-system": {},
          "argocd":      {},
          // tenant namespaces match label selector: loopzero.io/tenant exists
        },
      },
    },
    // OR use DefaultNamespaces for all kinds
  },
})
```

**Why not use a label selector cache filter only:** The operator MUST be able to read tenant-namespace Secrets even when no label is set yet (e.g., during initial creation). Namespace-pattern scoping is more reliable.

**Edge cases:**
- New tenant created at runtime → new namespace must be added to cache. Use a dynamic informer or use label-selector based namespace discovery.
- Backward compat: old deployments with cluster-wide access still work; new deployment removes it. No grace period needed in pre-release.
- Test fixtures may rely on cluster-wide access; verify envtest setup.

**Invariant restored:** INV-012 (`inv_secret_namespace_scoped`).

#### Tasks
1. Implement T0.1 + T0.2 (governance gate).
2. Read existing `clusterrole.yaml` and identify all rules to keep at cluster scope (CRDs, namespaces, priorityclasses) vs namespace scope (Secrets).
3. Write `role-loopzero-system.yaml` with rules scoped to platform-tokens Secret.
4. Write `role-argocd.yaml` with rules scoped to argocd-repo-* Secrets.
5. Write `role-tenant.yaml` template; figure out per-tenant binding mechanism (post-install Job vs controller-side creation in TenantController).
6. Update `clusterrole.yaml`: remove Secrets rule entirely.
7. Update `cmd/main.go`: scope cache for Secrets to expected namespaces.
8. Update TenantController to add/remove tenant namespace from cache scope on tenant create/delete.
9. Run `kubeconform --strict` on all chart YAMLs.
10. Update `operators/config/rbac/role.yaml` (kubebuilder-generated) consistently.

#### TDD
```
RED:    test_operator_cannot_delete_kube_system_secret() — assert RBAC denies Secret delete in kube-system
RED:    test_operator_can_create_argocd_repo_secret() — assert namespace-scoped Role allows it
RED:    test_operator_can_create_tenant_namespace_secret() — assert per-tenant binding works
RED:    test_clusterrole_has_no_secrets_rule() — assert verbs list does not include delete on core/v1 secrets
RED:    test_cache_scope_includes_dynamic_tenant_namespace() — assert new tenant adds namespace to cache
GREEN:  Implement Role files, ClusterRole removal, cache scoping. Run envtest.
REFACTOR: Extract namespace-scope addition into helper if duplicated
VERIFY: cd operators && go test ./internal/controller/... -run RBAC; kubeconform --strict charts/loopzero-tenant-controller/templates/
```

#### Acceptance Criteria
- [ ] `clusterrole.yaml` has zero rules referencing `secrets` resource.
- [ ] Three new Role files exist and pass kubeconform --strict.
- [ ] envtest shows operator has Secret access in loopzero-system, argocd, and tenant namespaces.
- [ ] envtest shows operator does NOT have Secret access in kube-system or default namespaces.
- [ ] INV-012 status flips from VIOLATED to VALIDATED in `analysis/invariants.md`.
- [ ] TC-1, TC-4, TC-5 toxic combinations marked CLOSED in DB.
- [ ] `cd operators && go test ./...` green.
- [ ] `task validate:manifests` passes.
- [ ] golangci-lint zero warnings.

#### DoD
- [ ] PR opened on branch `fix/infra-001-rbac-scope` against develop.
- [ ] ADR `.claude/decisions/2026-04-26-T1-1-rbac-scope.md` referenced in PR description.
- [ ] Security review (platform-security-engineer + security-blast-radius-reviewer) approved.
- [ ] Helm chart upgrade tested via `helm upgrade --dry-run`.
- [ ] CHANGELOG.md updated under `Security`.

### T1.2 — VAL-001: Remove dark alerts and implement 3 critical missing metrics

#### Objective
Eliminate the 21% alert dark zone by (a) removing the 12 alert rules whose `expr:` references metrics never emitted, and (b) implementing the three most critical missing metrics: `loopzero_build_egress_connectivity`, `loopzero_git_mirror_status`, `loopzero_fallback_active`.

#### Evidence
- VAL-001 in `findings/validation/validation_report.md`. Full list of orphan metrics in section 5.6.
- Cross-reference: grep for each metric name in `operators/`, `api/`, `pkg/` returns zero hits.
- 12 rules in `infra/helm/monitoring/manifests/prometheusrule-warning.yaml:44-413`.

#### Files to edit
```
infra/helm/monitoring/manifests/prometheusrule-warning.yaml — remove 12 dark alert rules
operators/internal/metrics/metrics.go — register 3 new metrics
operators/internal/controller/tenant_build.go — emit loopzero_build_egress_connectivity in build NP creation path
api/internal/services/gitops_pusher.go — emit loopzero_git_mirror_status on every gitops push
pkg/circuitbreaker/circuitbreaker.go — emit loopzero_fallback_active when CB is open
.github/workflows/ci.yml — add metric-alert cross-reference check (per D5)
scripts/check-alert-metrics.sh (NEW) — extract metric names from PrometheusRules, grep against Go MustRegister calls
```

#### Deep file dependency analysis
- `prometheusrule-warning.yaml`: removing rules is non-breaking; alerts that were always silent become absent. Operators should not notice.
- `metrics/metrics.go`: adding 3 Counter/Gauge registrations is purely additive. No breaking change.
- `tenant_build.go`: emit metric in existing path (NetworkPolicy creation succeeds → emit `connectivity=1`; failure → `connectivity=0`).
- `gitops_pusher.go`: emit metric in API service (gitops push success/failure).
- `circuitbreaker.go`: emit gauge when state transitions to open.
- CI script: needs `yq` (YAML extraction) and `grep` against repo.

Downstream:
- Grafana dashboards may reference removed alerts; verify no panel breakage.
- Runbooks for removed alerts (none currently exist; VAL-002).

#### Deep Dives
**Metric design:**
```go
// loopzero_build_egress_connectivity
buildEgressConnectivity = prometheus.NewGaugeVec(
  prometheus.GaugeOpts{
    Name: "loopzero_build_egress_connectivity",
    Help: "1 if build NP allows expected egress, 0 otherwise.",
  },
  []string{"tenant", "environment", "destination"},
)

// loopzero_git_mirror_status
gitMirrorStatus = prometheus.NewGaugeVec(
  prometheus.GaugeOpts{
    Name: "loopzero_git_mirror_status",
    Help: "1 if last gitops push succeeded, 0 otherwise.",
  },
  []string{"tenant", "repo"},
)

// loopzero_fallback_active
fallbackActive = prometheus.NewGaugeVec(
  prometheus.GaugeOpts{
    Name: "loopzero_fallback_active",
    Help: "1 if fallback mode is active for this circuit, 0 otherwise.",
  },
  []string{"circuit"},
)
```

**CI cross-reference logic:**
1. Extract all metric names from PrometheusRule YAMLs: `yq '.spec.groups[].rules[].expr' | grep -oE 'loopzero_[a-z_]+'`
2. Extract all registered metrics from Go: `grep -rE 'prometheus.New(Counter|Gauge|Histogram|Summary)' --include="*.go" | sed -nE 's/.*"(loopzero_[a-z_]+)".*/\1/p'`
3. Compare sets; fail CI if any rule metric is not registered.

**Edge cases:**
- Multi-line `expr:` fields with rate() / sum() — extract base metric name, not the full expression.
- Metrics registered via wrapping libraries (controller-runtime exposes some `controller_runtime_*`); whitelist external prefixes.

**Invariant restored:** INV-015 (`inv_alert_metrics_emitted`).

#### Tasks
1. List the 12 orphan alerts from final_report.md section 5.6.
2. For each, decide: remove or implement (3 implemented per D6 / VAL-001 recommendation; 9 removed).
3. Edit `prometheusrule-warning.yaml` — delete rules.
4. Add 3 metric registrations in `metrics.go`.
5. Wire emissions in `tenant_build.go`, `gitops_pusher.go`, `circuitbreaker.go`.
6. Write `scripts/check-alert-metrics.sh`; gate it in `ci.yml`.
7. Test the script catches at least one orphan if reverted.
8. Verify Grafana dashboards do not reference removed alert names.

#### TDD
```
RED:    test_check_alert_metrics_script_fails_on_orphan() — inject fake orphan alert, verify script exits non-zero
RED:    test_build_egress_connectivity_emitted_on_np_create() — assert metric appears in registry
RED:    test_git_mirror_status_emitted_on_push_success() — assert metric is 1 after gitops push
RED:    test_fallback_active_emitted_on_cb_open() — assert metric is 1 when CB transitions to open
RED:    test_no_orphan_alerts_remain() — run script against current manifest, expect zero orphans
GREEN:  Remove 9 dark alerts; implement 3 metrics + emissions; wire CI script.
REFACTOR: Extract metric registration into a single MetricRegistry helper if registrations duplicate
VERIFY: bash scripts/check-alert-metrics.sh; cd operators && go test ./internal/metrics/...
```

#### Acceptance Criteria
- [ ] `prometheusrule-warning.yaml` has zero rules referencing non-emitted metrics.
- [ ] 3 new metrics registered in `loopzero_*` registry and emit on expected events.
- [ ] `scripts/check-alert-metrics.sh` exists and is invoked from `ci.yml`.
- [ ] CI fails on orphan alert injection (smoke test).
- [ ] INV-015 status flips from VIOLATED to VALIDATED.
- [ ] Alert dark zone < 5%.
- [ ] golangci-lint zero warnings.

#### DoD
- [ ] PR opened on branch `fix/val-001-dark-alerts` against develop.
- [ ] ADR `2026-04-26-T1-2-dark-alerts.md` referenced.
- [ ] SRE veto cleared.
- [ ] Grafana dashboards verified intact.
- [ ] CHANGELOG.md updated under `Fixed` and `Added`.

### T1.3 — CODE-001: Fix NetworkPolicy idempotency (failing test → green)

#### Objective
Make `tenant_integration_test.go:266` pass by fixing the underlying idempotency bug in `EnvironmentController.reconcileNetworkPolicies()` and correcting `countResources()` to count NPs in both runtime AND build namespaces.

#### Evidence
- `go test -count=1 ./...` in operators returns 1 FAIL: `tenant_integration_test.go:266` — `policy count should be identical — Expected <int>: 3 to equal <int>: 0`.
- Phase 4 root cause: `countResources()` only checks runtime namespace; `reconcileBuildNetworkPolicy` writes to build namespace via `r.BuildClient`. EnvironmentController also creates additional NPs in runtime ns on each spec-triggered reconcile.

#### Files to edit
```
operators/internal/controller/environment_network.go — replace raw Create with controllerutil.CreateOrUpdate per D7
operators/internal/controller/tenant_integration_test.go — fix countResources to span both namespaces
operators/internal/controller/network_policies_test.go — add explicit idempotency test (3 reconciles → stable count)
```

#### Deep file dependency analysis
- `environment_network.go`: applies NetworkPolicies on EnvironmentController reconcile. Currently uses raw Create which creates duplicates. Change to CreateOrUpdate.
- `tenant_integration_test.go:266`: currently failing. Fix `countResources()` helper to query both `r.Client` (runtime) and `r.BuildClient` namespaces.
- `network_policies_test.go`: add a focused unit-style test for idempotency that does not depend on full integration setup.

Downstream files affected:
- None directly. Idempotent reconcile is a correctness improvement.

#### Deep Dives
**Pattern: controller-runtime CreateOrUpdate**
```go
np := &networkingv1.NetworkPolicy{
  ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
}
if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, np, func() error {
  np.Spec = desiredSpec
  return controllerutil.SetControllerReference(env, np, r.Scheme)
}); err != nil {
  return err
}
```

**countResources fix:**
```go
runtime := r.Client.List(ctx, &nps, client.InNamespace(runtimeNs))
build := r.BuildClient.List(ctx, &nps, client.InNamespace(buildNs))
return len(runtime.Items) + len(build.Items)
```

**Edge cases:**
- Spec mutations (e.g., adding new egress rule): CreateOrUpdate updates existing NP in place. Confirmed-clean path.
- Concurrent reconciles: CreateOrUpdate uses GET → conditional Update, so two reconciles collapse to one update or one Create + one no-op.

**Invariant restored:** INV-007 (`idempotent_reconcile_for_existing_resources`).

#### Tasks
1. Read `environment_network.go` and identify all NP Create call sites.
2. Replace each with controllerutil.CreateOrUpdate.
3. Fix `countResources()` helper in tenant_integration_test.go.
4. Add `TestEnvironmentController_NetworkPolicyIdempotency` in network_policies_test.go (3 spec reconciles → stable count).
5. Run `go test -count=1 ./...` and confirm 0 failures.

#### TDD
```
RED:    Existing test_networkpolicy_idempotency at tenant_integration_test.go:266 — already RED.
RED:    test_three_reconciles_produce_three_nps_not_nine() — explicit assertion in network_policies_test.go
GREEN:  Replace Create with CreateOrUpdate. Fix countResources. Run go test.
REFACTOR: If multiple NP types follow same pattern, extract helper; otherwise keep inline.
VERIFY: cd operators && go test -count=1 -run NetworkPolicy ./... && cd operators && go test -count=1 ./... (must be 0 fails)
```

#### Acceptance Criteria
- [ ] `cd operators && go test -count=1 ./...` returns 0 failures.
- [ ] `network_policies_test.go` has explicit idempotency test passing.
- [ ] `countResources()` helper queries both namespaces.
- [ ] INV-007 status flips from VIOLATED to VALIDATED.
- [ ] No new lint warnings.

#### DoD
- [ ] PR opened on branch `fix/code-001-np-idempotency` against develop.
- [ ] No ADR required (T1 — direct fix).
- [ ] CHANGELOG.md updated under `Fixed`.

### T1.4 — ARCH-001: Wire MinIO CircuitBreakerProvisioner in setupMinIO()

#### Objective
Apply existing `minio.NewCircuitBreakerProvisioner` wrapper in `cmd/main.go:setupMinIO()` matching the pattern already used for Gitea (line 133), ArgoCD (line 149), and Harbor (line 253).

#### Evidence
- ARCH-001 at `cmd/main.go:208-231`: returns raw `AdminProvisioner` instead of CB-wrapped instance.
- `operators/internal/minio/circuitbreaker_provisioner.go` exists — wrapper is implemented but unused.
- TC-3: MinIO outage cascades through reconcile because no CB. INV-004 + INV-014 violated.

#### Files to edit
```
operators/cmd/main.go — wrap AdminProvisioner with NewCircuitBreakerProvisioner in setupMinIO()
operators/cmd/main_test.go — add test asserting setupMinIO returns CB-wrapped provisioner
```

#### Deep file dependency analysis
- `main.go` setupMinIO function: returns `*minio.AdminProvisioner` currently. Must return the interface type (`minio.CredentialProvisioner`?) so that the CB wrapper can substitute. Verify interface compatibility.
- `main_test.go`: may not exist for setupMinIO; add focused test.

Downstream:
- All callers of `deps.minioProvisioner` must accept the interface, not the concrete type. Verify TenantController and other consumers.

#### Deep Dives
**Pattern reference (Gitea, line 133):**
```go
giteaProvisioner := gitea.NewCircuitBreakerProvisioner(rawProv, cbConfig)
```

**Apply to MinIO:**
```go
adminProv := minio.NewAdminProvisioner(...)
return minio.NewCircuitBreakerProvisioner(adminProv, cbConfig), nil
```

**CB config:** Use shared `cbConfig` already defined in main.go (OpenThreshold=5, RetryInterval=120s). No new config.

**Edge cases:**
- If interface type differs from concrete, fix imports.
- Other tests that rely on direct AdminProvisioner methods may break; verify and update.

**Invariants partially restored:** INV-004 (CB on all external provisioners), INV-014 (CB-wrapped external clients).

#### Tasks
1. Read `setupMinIO` in `main.go`.
2. Wrap the returned provisioner with CB.
3. Verify interface satisfaction at call sites.
4. Add `TestSetupMinIO_ReturnsCircuitBreakerWrappedProvisioner`.
5. Run `cd operators && go test ./...`.

#### TDD
```
RED:    test_setupMinIO_returns_cb_wrapped() — assert returned type is *CircuitBreakerProvisioner OR has CB methods
GREEN:  Edit setupMinIO to wrap. Run test.
REFACTOR: None — single line change.
VERIFY: cd operators && go test ./cmd/... -run SetupMinIO
```

#### Acceptance Criteria
- [ ] `setupMinIO()` returns a CB-wrapped provisioner.
- [ ] Test `TestSetupMinIO_ReturnsCircuitBreakerWrappedProvisioner` passes.
- [ ] INV-004 status flips from VIOLATED to VALIDATED.
- [ ] TC-3 marked CLOSED.

#### DoD
- [ ] PR opened on branch `fix/arch-001-minio-cb` against develop.
- [ ] No ADR required (T1 — direct).
- [ ] CHANGELOG.md under `Fixed`.

### T1.5 — INFRA-000: Gate build-images.yml on ci.yml success

#### Objective
Prevent broken images from reaching DOCR by adding `workflow_run` dependency or inline test gate to `.github/workflows/build-images.yml`.

#### Evidence
- INFRA-000: both workflows trigger on the same `push` events, run independently. A commit failing operator tests can succeed in pushing the operator image.
- Cross-reference TC-1 (build artifact + RBAC blast radius if a malicious or buggy image is deployed).

#### Files to edit
```
.github/workflows/build-images.yml — add workflow_run dependency OR inline go test step before docker buildx push
.github/workflows/ci.yml — verify it produces a `ci-gate` success signal that build-images.yml can depend on
```

#### Deep file dependency analysis
- `build-images.yml`: triggers on push to main/develop/release. Currently no test step.
- `ci.yml`: runs `task test:unit` + matrix tests. Already produces a job-level success/failure signal.

Downstream:
- Releases via tag push — verify both workflows still chain correctly.

#### Deep Dives
**Option A: workflow_run trigger (preferred per D5 meeting outcome):**
```yaml
on:
  workflow_run:
    workflows: ["CI"]
    types: [completed]
    branches: [main, develop, release/**]
jobs:
  build-images:
    if: github.event.workflow_run.conclusion == 'success'
```

**Option B: inline test step (simpler, no cross-workflow dep):**
```yaml
- name: Test before build
  run: cd operators && go test ./... -short
- name: Buildx push
  ...
```

**Trade-off:** Option A respects existing CI design and avoids re-running tests. Option B is simpler but duplicates work. Per D5 meeting (T0.2), choose Option A.

**Edge cases:**
- Manual workflow dispatch: maintain ability to override via `workflow_dispatch` trigger.
- Tag push for releases: ensure tag push triggers ci.yml first, then build-images.yml.

#### Tasks
1. Read both workflows.
2. Add `workflow_run` trigger to build-images.yml.
3. Verify gate works via dry-run on a feature branch.
4. Verify release tag flow still triggers correctly.

#### TDD
```
RED:    Trigger workflow with intentionally failing test → confirm build-images skips
RED:    Trigger workflow with passing test → confirm build-images runs
GREEN:  Add workflow_run trigger.
REFACTOR: None.
VERIFY: act --workflows build-images.yml --eventpath fake-success.json (or manual GH dispatch test)
```

#### Acceptance Criteria
- [ ] `build-images.yml` has `workflow_run` dependency on `ci.yml`.
- [ ] Test failures gate prevents image push.
- [ ] Manual dispatch path still works.
- [ ] Release tag flow verified.

#### DoD
- [ ] PR opened on branch `fix/infra-000-ci-gate` against develop.
- [ ] ADR `2026-04-26-T1-5-ci-gate.md` referenced.
- [ ] gitops-engineer + verification-architect approval recorded.
- [ ] CHANGELOG.md under `Fixed`.

---

## Phase 2: P2 — Functional Gaps and Code Quality

**Objective:** Close 9 P2 findings in 1 month — preview wiring (interim), error handling improvements, secure-by-default, observability completeness, supply-chain hardening, and build NetworkPolicy egress allowlist.

### T2.1 — COMP-001: Wire SetupPreviewController in main.go

**Objective:** Add `controller.SetupPreviewController(...)` call to `setupControllers()` so the existing PreviewEnvironment reconcile skeleton actually runs at runtime.

**Evidence:** `cmd/main.go:280-343` lists 7 SetupX calls; `SetupPreviewController` exists at `preview_controller.go:213` but is never invoked. Per D6, this PR wires the controller without implementing full workload deployment.

**Files to edit:**
```
operators/cmd/main.go — add SetupPreviewController call in setupControllers
operators/cmd/main_test.go — assert PreviewController is registered
operators/internal/controller/preview_controller.go — replace `var _ = statemachine.NewPreviewMachine` with actual usage in phase transitions
```

**Deep file dependency analysis:** Adding the call enables reconciliation; phase logic already exists for Creating/Active/Expiring/Deleting. State machine wiring (COMP-002) is bundled here per D6.

**Deep Dives:** Use `previewSM := statemachine.NewPreviewMachine()` and replace direct `Status.Phase = X` with `previewSM.Transition(current, event)`.

**Tasks:**
1. Add `controller.SetupPreviewController(mgr, deps.runtimeClient, baseDomain)` to setupControllers.
2. Replace 4 direct phase assignments with state machine transitions.
3. Add registration assertion test.
4. Verify envtest spec for PreviewEnvironment still passes.

**TDD:**
```
RED:   test_setupControllers_registers_preview_controller — fails before fix
RED:   test_preview_phase_transition_via_state_machine — fails because direct assignment used
GREEN: Wire SetupPreviewController; route phase changes through state machine.
REFACTOR: None.
VERIFY: cd operators && go test ./internal/controller/... -run Preview
```

**Acceptance Criteria:** Manager registers PreviewController; envtest reconciles a PreviewEnvironment CR through Creating→Active. INV-003, INV-009 partially restored.

**DoD:** PR `fix/comp-001-preview-wiring` against develop; ADR `2026-04-26-T2-1-preview-wiring.md`; CHANGELOG `Added`.

### T2.2 — COMP-008: Replace silent PR webhook with HTTP 501

**Objective:** `wh.OnPullRequest` returns HTTP 501 with explanatory body instead of silent 200 + nil.

**Evidence:** `api/cmd/setup_github.go:173-182` body is `logger.Info(...); return nil`. Per D4, return 501 until Sprint 5 builds full path.

**Files to edit:**
```
api/cmd/setup_github.go — replace handler body with 501 response
api/cmd/setup_github_test.go — assert 501 status on OnPullRequest event
```

**Tasks:** 1) Edit handler. 2) Add test. 3) Update CHANGELOG.

**TDD:**
```
RED:   test_pr_webhook_returns_501 — fails before fix
GREEN: Return 501 with body "Preview environments are in development".
REFACTOR: None.
VERIFY: cd api && go test ./cmd/... -run PullRequest
```

**Acceptance:** Webhook returns 501 with non-empty body. TC-2 trust gap eliminated (silent → explicit).

**DoD:** PR `fix/comp-008-pr-webhook-501`; CHANGELOG `Changed`.

### T2.3 — CODE-002: Replace panic() in buildDockerConfigJSON with error return

**Objective:** Eliminate panic risk in reconcile hot path.

**Evidence:** `tenant_harbor.go:268-274` calls `panic()` if json.Marshal fails. Although unlikely, panics in reconcile loops cause goroutine death and crash-loop behavior amplifying TC-1.

**Files to edit:**
```
operators/internal/controller/tenant_harbor.go — change buildDockerConfigJSON signature to (string, error)
operators/internal/controller/tenant_controller.go — handle returned error
operators/internal/controller/tenant_harbor_test.go — add test for marshal failure path
```

**Tasks:** 1) Refactor signature. 2) Update callers. 3) Add error path test.

**TDD:**
```
RED:   test_buildDockerConfigJSON_returns_error_on_invalid_input — fails (currently panics)
GREEN: Refactor to return error.
REFACTOR: None.
VERIFY: cd operators && go test ./internal/controller/... -run Harbor
```

**Acceptance:** No `panic()` calls in reconcile path; INV-013 partially restored.

**DoD:** PR `fix/code-002-no-panic`; CHANGELOG `Fixed`.

### T2.4 — CODE-006: Phase 1 deletion returns error on failure

**Objective:** `tenant_deletion.go:76-91` Phase 1 currently swallows errors; change to return error so reconciler retries with backoff.

**Evidence:** Phase 1 silent failure causes orphaned Environment CRs (CODE-006) and indefinite requeue without progress (VAL-005).

**Files to edit:**
```
operators/internal/controller/tenant_deletion.go — return error from Phase 1 on DeleteApplications failure
operators/internal/controller/tenant_deletion_backoff_test.go — add test for Phase 1 retry on transient failure
```

**Tasks:** 1) Edit Phase 1 to bubble errors. 2) Add backoff test. 3) Verify no infinite loop.

**TDD:**
```
RED:   test_phase1_retries_with_backoff_on_failure — fails before fix
GREEN: Return error; reconciler will requeue with exponential backoff (already wired).
REFACTOR: None.
VERIFY: cd operators && go test ./internal/controller/... -run Deletion
```

**Acceptance:** Phase 1 failures are observable and retried; INV-010 fully restored.

**DoD:** PR `fix/code-006-phase1-error`; CHANGELOG `Fixed`.

### T2.5 — COMP-007: Invert TLS InsecureSkipVerify default

**Objective:** Change default in `internal/httpclient/client.go` from `InsecureSkipVerify=true` to `false`. Allow opt-in via env var or per-cluster config.

**Evidence:** COMP-007 / SEC-001. Currently every operator HTTP client defaults to skipping TLS verification. Inverted secure default.

**Files to edit:**
```
operators/internal/httpclient/client.go — set InsecureSkipVerify=false by default; opt-in via LOOPZERO_TLS_INSECURE env var
operators/internal/httpclient/client_test.go (NEW) — first unit test for httpclient package (closes part of VAL-003)
```

**Tasks:** 1) Read existing default. 2) Invert. 3) Add opt-in flag. 4) Add unit test.

**TDD:**
```
RED:   test_default_tls_verifies_certs — fails before fix
RED:   test_LOOPZERO_TLS_INSECURE_env_var_opts_in_to_skip — passes after fix
GREEN: Invert default; honor env var.
REFACTOR: None.
VERIFY: cd operators && go test ./internal/httpclient/...
```

**Acceptance:** Default is secure; INV-006 restored; first unit test added to httpclient package.

**DoD:** PR `fix/comp-007-tls-default`; ADR `2026-04-26-T2-5-tls-default.md`; security-blast-radius-reviewer approval; CHANGELOG `Security`.

### T2.6 — OBS-001: Fix loopzero_reconciler_errors_total alert metric name

**Objective:** Single-line fix — rename metric in `prometheusrule-warning.yaml` to match registered name `loopzero_operator_reconcile_errors_total`.

**Evidence:** Name mismatch is one of the 12 dark alerts (covered in T1.2 but called out separately due to triviality).

**Files to edit:**
```
infra/helm/monitoring/manifests/prometheusrule-warning.yaml — rename in expr field
```

**Tasks:** 1) Rename. 2) Verify alert evaluates against existing metric.

**TDD:**
```
RED:   T1.2 alert cross-reference script flags this — fails before fix
GREEN: Rename.
VERIFY: bash scripts/check-alert-metrics.sh (no orphan from this row)
```

**Acceptance:** LoopZeroReconcilerUnhealthy alert fires correctly on existing metric.

**DoD:** PR `fix/obs-001-metric-name`; CHANGELOG `Fixed`.

### T2.7 — VAL-002: Write runbooks for top 10 operationally critical alerts

**Objective:** Cover the 10 most-firing or most-critical alerts with runbooks.

**Evidence:** 33 of 47 alerts have no runbook. Top critical (uncovered): LoopZeroDBPoolSaturation, LoopZeroReconcilerUnhealthy, LoopZeroAppCrashLoop, LoopZeroAppDeployStuck.

**Files to edit:**
```
docs/runbooks/alerts/loopzero-db-pool-saturation.md (NEW)
docs/runbooks/alerts/loopzero-reconciler-unhealthy.md (NEW)
docs/runbooks/alerts/loopzero-app-crash-loop.md (NEW)
docs/runbooks/alerts/loopzero-app-deploy-stuck.md (NEW)
docs/runbooks/alerts/loopzero-build-cb-open.md (NEW)
docs/runbooks/alerts/loopzero-tenant-deletion-stuck.md (NEW)
docs/runbooks/alerts/loopzero-platformbootstrap-stuck.md (NEW)
docs/runbooks/alerts/loopzero-argocd-sync-failed.md (NEW)
docs/runbooks/alerts/loopzero-cert-renewal-failed.md (NEW)
docs/runbooks/alerts/loopzero-pulumi-drift.md (NEW)
prometheusrule-warning.yaml — add `runbook_url` annotation pointing to each runbook
```

**Tasks:** 1) Identify top 10. 2) Each runbook: triage + escalation + recovery + verification.

**TDD:** Documentation only; criterion is presence + non-trivial content.

**Acceptance:** 10 runbooks exist with diagnose/mitigate/verify sections.

**DoD:** PR `docs/val-002-runbooks`; SRE review; CHANGELOG `Added`.

### T2.8 — INFRA-003: SHA-pin security-critical GitHub Actions

**Objective:** Pin `sigstore/cosign-installer`, `anchore/sbom-action`, `aquasecurity/trivy-action`, `digitalocean/action-doctl` to commit SHAs.

**Evidence:** INFRA-003: currently mutable tag pins (`@v3`) — supply chain risk.

**Files to edit:** `.github/workflows/build-images.yml`, `.github/workflows/ci.yml` — replace tag pins with `@<sha40>`.

**Tasks:** 1) Resolve current SHA for each action. 2) Replace pins. 3) Add comment with version for human readability.

**TDD:** No test; visual confirm in PR review.

**Acceptance:** Every security-critical action SHA-pinned with comment `# v3.x.x`.

**DoD:** PR `chore/infra-003-sha-pin`; CHANGELOG `Security`.

### T2.9 — COMP-005: Restrict build NetworkPolicy egress to MinIO + K8s API

**Objective:** Replace blanket egress to ports 9000 and 6443 with destination-based selectors (MinIO Service, K8s Service).

**Evidence:** COMP-005 — current NP allows port 9000 + 6443 to ANY destination, exfiltration vector.

**Files to edit:**
```
operators/internal/controller/tenant_build.go — refactor build NP egress rules to use to:[serviceSelector]
operators/internal/controller/tenant_isolation_test.go — assert egress is restricted
```

**Tasks:** 1) Refactor egress block. 2) Add isolation test asserting deny on non-allowlisted destinations.

**TDD:**
```
RED:   test_build_pod_cannot_egress_to_arbitrary_external_host — fails before fix
GREEN: Restrict egress.
VERIFY: cd operators && go test ./internal/controller/... -run Isolation
```

**Acceptance:** Egress matrix proves only MinIO + K8s API + DNS reachable from build pods.

**DoD:** PR `fix/comp-005-build-np`; ADR `2026-04-26-T2-9-build-np.md`; CHANGELOG `Security`.

---

## Phase 3: P3 — Architectural Consistency, Tests, and CI Hardening

**Objective:** 8 fixes to lift test coverage in shared packages, prevent CRD schema drift, harden supply-chain ordering, and enforce the "Implemented but not wired" pattern systemically.

### T3.1 — VAL-003: Add unit tests for gitea/ and httpclient/

**Objective:** Bring test pyramid back to right side up — add unit tests for two zero-coverage packages.

**Files to edit:**
```
operators/internal/gitea/api_test.go (NEW)
operators/internal/httpclient/client_test.go (extend from T2.5)
```

**Tasks:** 1) Test each exported function. 2) Cover error paths.

**TDD:** Each new test is RED before implementation; each implementation is the existing function (no code change unless bug found).

**Acceptance:** ≥ 60% coverage on `gitea/` and `httpclient/` packages.

**DoD:** PR `test/val-003-unit-coverage`; CHANGELOG `Added`.

### T3.2 — INFRA-004: CI CRD schema drift check

**Objective:** Add CI step that regenerates CRDs and fails if `operators/config/crd/bases/` differs from `charts/loopzero-crds/templates/`.

**Files to edit:**
```
.github/workflows/ci.yml — new job `crd-schema-drift`
scripts/check-crd-drift.sh (NEW)
```

**Tasks:** 1) Generate. 2) Diff. 3) Fail on diff.

**TDD:**
```
RED:   Inject schema mismatch → script fails
GREEN: Run.
VERIFY: bash scripts/check-crd-drift.sh
```

**Acceptance:** CRD drift caught at PR time.

**DoD:** PR `ci/infra-004-crd-drift`; CHANGELOG `Added`.

### T3.3 — INFRA-005: Trivy scan pre-push

**Objective:** Move Trivy scan before `docker buildx push`, eliminating the window where vulnerable images sit in DOCR.

**Files to edit:** `.github/workflows/build-images.yml` — reorder Trivy job.

**Tasks:** 1) Move scan. 2) Verify gate functions.

**TDD:** Inject vulnerable image → push fails.

**Acceptance:** Trivy scan blocks push on CRITICAL/HIGH.

**DoD:** PR `ci/infra-005-trivy-pre-push`; ADR `2026-04-26-T3-3-trivy.md`; CHANGELOG `Security`.

### T3.4 — CODE-004: Pin madmin-go and add isNotFoundError integration test

**Objective:** Pin minio/madmin-go version + add a test that confirms `isNotFoundError` matches actual madmin-go error message.

**Files to edit:** `operators/go.mod`, `operators/internal/minio/provisioner_test.go`.

**TDD:** RED test calls real madmin-go API to provoke not-found, asserts isNotFoundError catches it.

**Acceptance:** Pinned + test passes against pinned version.

**DoD:** PR `fix/code-004-madmin-pin`; CHANGELOG `Changed`.

### T3.5 — SEC-002, SEC-003: CRD validation markers

**Objective:** Add kubebuilder Pattern markers for `TenantEnvironmentEntry.Name`, `TenantEnvironmentEntry.Cluster`, `GitRepositoryBinding.RepoURL`.

**Files to edit:**
```
operators/api/v1/tenant_types.go — add +kubebuilder:validation:Pattern
operators/api/v1/gitrepositorybinding_types.go — add Pattern + MaxLength
operators/config/crd/bases/* — regenerate
charts/loopzero-crds/templates/* — regenerate
```

**TDD:** Apply CR with invalid value → API server rejects with 422.

**Acceptance:** Invalid values rejected at API server, before reaching reconcile.

**DoD:** PR `fix/sec-002-003-crd-validation`; CHANGELOG `Security`.

### T3.6 — ARCH-002: State machine for ExposedRoute and LoopZeroApplication

**Objective:** Add `pkg/statemachine/exposedroute.go` and `pkg/statemachine/application.go` so all controllers use state machine.

**Files to edit:**
```
pkg/statemachine/exposedroute.go (NEW)
pkg/statemachine/application.go (NEW)
operators/internal/controller/exposure_controller.go — route phase changes via SM
operators/internal/controller/application_controller.go — route phase changes via SM
pkg/statemachine/exposedroute_test.go (NEW)
pkg/statemachine/application_test.go (NEW)
```

**TDD:** Invalid transition panics; valid transition succeeds.

**Acceptance:** INV-003 and INV-009 fully restored.

**DoD:** PR `feat/arch-002-state-machines`; ADR `2026-04-26-T3-6-state-machines.md`; CHANGELOG `Changed`.

### T3.7 — INFRA-007: Switch operator base image to distroless

**Objective:** Replace `alpine:3.22` with `gcr.io/distroless/static-debian12:nonroot` in operator Dockerfile.

**Files to edit:** `operators/Dockerfile`.

**TDD:** Image still runs; healthcheck still passes (need `HEALTHCHECK` removed since distroless lacks shell — replace with K8s liveness/readiness probe in Helm chart, already present).

**Acceptance:** Image is shell-less; size reduced; CVE surface smaller.

**DoD:** PR `chore/infra-007-distroless`; CHANGELOG `Security`.

### T3.8 — D5(a): Manager-bootstrap test for Setup*Controller registration

**Objective:** Implement systemic check from D5 — assert every exported `Setup*Controller` under `operators/internal/controller/` is referenced in `cmd/main.go:setupControllers`.

**Files to edit:**
```
operators/cmd/registration_test.go (NEW) — uses go/parser AST to enumerate Setup* funcs and confirm each is called
```

**TDD:**
```
RED:   Inject orphan Setup function → test fails
GREEN: Add Setup call.
REFACTOR: None.
VERIFY: cd operators && go test ./cmd/... -run Registration
```

**Acceptance:** Adding any new exported `Setup*Controller` without wiring breaks CI.

**DoD:** PR `test/d5-bootstrap-check`; CHANGELOG `Added`.

---

## Phase 4: P4 — Polish

**Objective:** 6 low-risk hygiene improvements; all parallelizable.

### T4.1 — ARCH-003: Replace freeform condition strings with typed constants

**Files to edit:** `operators/internal/controller/preview_controller.go`, `tenant_deletion.go`, `operators/internal/conditions/conditions.go`.

**TDD:** Existing tests cover; new test asserts no `"NamespaceReady"` string literal in non-conditions/ packages.

**Acceptance:** All condition types use constants.

**DoD:** PR `chore/arch-003-typed-conditions`.

### T4.2 — CODE-003: Harbor httpclient consolidation

**Files:** `operators/internal/harbor/client.go`.

**TDD:** Test confirms Harbor uses shared httpclient (TLS config aligned).

**Acceptance:** Single source of truth for HTTP client config.

**DoD:** PR `chore/code-003-harbor-httpclient`.

### T4.3 — CODE-005: ArgoCD server-side Apply

**Files:** `operators/internal/argocd/real_provisioner.go`.

**TDD:** Concurrent reconcile produces no Conflict errors.

**Acceptance:** EnsureApplication uses `client.Apply` with FieldManager.

**DoD:** PR `chore/code-005-ssa`.

### T4.4 — INFRA-006: Tilt securityContext parity

**Files:** `infra/tilt/operator-deployment.yaml`.

**TDD:** Manifest validation confirms capabilities.drop=[ALL] + seccompProfile RuntimeDefault.

**Acceptance:** Dev-prod parity achieved.

**DoD:** PR `chore/infra-006-tilt-parity`.

### T4.5 — INFRA-008: Remove stale RBAC backward-compat rules

**Files:** `infra/tilt/operator-deployment.yaml`, possibly chart templates.

**TDD:** kubeconform validates remaining rules.

**Acceptance:** No dead RBAC rules.

**DoD:** PR `chore/infra-008-rbac-cleanup`.

### T4.6 — SEC-005: Per-cluster TLS bypass audit log

**Files:** `operators/internal/controller/clustercredential_controller.go`.

**TDD:** Reconciling a cluster with `TLS.Insecure=true` emits a structured WARNING log.

**Acceptance:** Insecure clusters traceable in logs.

**DoD:** PR `fix/sec-005-tls-bypass-log`.

---

## Coverage Matrix

| # | Finding ID | Severity | Task(s) | Resolution |
|---|------------|----------|---------|------------|
| 1 | INFRA-001 | HIGH | T1.1 | Namespace-scoped Roles |
| 2 | VAL-001 | HIGH | T1.2 | Remove 9 dark alerts + impl 3 metrics + CI check |
| 3 | CODE-001 | HIGH | T1.3 | controllerutil.CreateOrUpdate + countResources fix |
| 4 | ARCH-001 | HIGH | T1.4 | Wire MinIO CB |
| 5 | INFRA-000 | HIGH | T1.5 | workflow_run gate |
| 6 | COMP-001 | HIGH | T2.1 | Wire SetupPreviewController |
| 7 | COMP-008 | HIGH | T2.2 | HTTP 501 |
| 8 | VAL-006 | HIGH | T2.1 + T2.2 | Closed via combined fix |
| 9 | CODE-002 | MED | T2.3 | Replace panic with error |
| 10 | CODE-006/VAL-005 | MED | T2.4 | Phase 1 error return |
| 11 | COMP-007/SEC-001 | MED | T2.5 | Invert TLS default |
| 12 | OBS-001 | MED | T2.6 | Metric name fix |
| 13 | VAL-002 | MED | T2.7 | 10 runbooks |
| 14 | INFRA-003 | MED | T2.8 | SHA pin |
| 15 | COMP-005 | MED | T2.9 | NP destination selectors |
| 16 | VAL-003 | MED | T3.1 | Unit tests |
| 17 | INFRA-004 | MED | T3.2 | CRD drift CI |
| 18 | INFRA-005 | MED | T3.3 | Trivy pre-push |
| 19 | CODE-004 | MED | T3.4 | madmin pin |
| 20 | SEC-002, SEC-003 | MED | T3.5 | CRD validation markers |
| 21 | ARCH-002 | LOW | T3.6 | State machines |
| 22 | INFRA-007 | LOW | T3.7 | Distroless |
| 23 | "Pattern 1" systemic | — | T3.8 | Bootstrap test |
| 24 | ARCH-003 | LOW | T4.1 | Typed constants |
| 25 | CODE-003 | LOW | T4.2 | Harbor httpclient |
| 26 | CODE-005 | LOW | T4.3 | Server-side Apply |
| 27 | INFRA-006 | LOW | T4.4 | Tilt parity |
| 28 | INFRA-008 | LOW | T4.5 | RBAC cleanup |
| 29 | SEC-005 | LOW | T4.6 | TLS bypass log |
| 30 | COMP-002 | MED | T2.1 | Bundled (state machine wiring) |
| 31 | COMP-003 | MED | (deferred) | AppSecret controller is a feature, not a bug — needs Sprint planning |
| 32 | COMP-004 | LOW | (deferred) | ReconciliationReport CRD is a placeholder; remove or implement is its own decision |
| 33 | COMP-006 | MED | T2.7 + docs | Documented in clustercredential runbook + comment fix |
| 34 | ARCH-004 | LOW | (deferred) | GitRepositoryBindingController inline HTTP — covered by httpclient consolidation in T4.2 if extended |
| 35 | INFRA-002 | MED | T4.4 | Bundled with Tilt parity |
| 36 | CODE-007 | LOW | T4.1 | Bundled with typed constants |
| 37 | CODE-008 | LOW | (deferred) | Acceptable per finding analysis |
| 38 | OBS-002 | LOW | T2.7 | Bundled with runbook batch |
| 39 | SEC-004 | LOW | T1.4 | Bundled (overlaps ARCH-001) |
| 40 | VAL-004 | POS | — | Positive finding; preserved as-is |
| 41 | VAL-007 | POS | — | Positive finding; preserved as-is |
| 42 | SEC-006 | POS | — | Positive finding; preserved as-is |
| 43 | TC-1 | TOXIC | T1.1 + T2.3 | Closed via RBAC scope + no-panic |
| 44 | TC-2 | TOXIC | T2.1 + T2.2 | Mitigated via HTTP 501 + controller wiring |
| 45 | TC-3 | TOXIC | T1.4 | Closed via MinIO CB |
| 46 | TC-4 | TOXIC | T1.1 | Closed via RBAC scope |
| 47 | TC-5 | TOXIC | T1.1 + T4.6 | Closed via RBAC scope + audit log |

**Coverage: 41 actionable findings of 44 + 5 toxic combinations covered. 3 deferred (COMP-003, COMP-004, ARCH-004 — explicit deferral with rationale).**

**Coverage rate: 93% addressed, 7% explicitly deferred.**

## Global Definition of Done

- [ ] All P1 tasks (T1.1–T1.5) merged to develop within 1 week.
- [ ] All P2 tasks (T2.1–T2.9) merged to develop within 1 month.
- [ ] All P3 tasks (T3.1–T3.8) merged to develop within 1 quarter.
- [ ] All P4 tasks (T4.1–T4.6) merged when low-leverage cycles allow.
- [ ] `cd operators && go test -count=1 ./...` returns 0 failures (CODE-001 closed).
- [ ] `task lint` zero warnings.
- [ ] `task validate:manifests` (kubeconform --strict) passes.
- [ ] `bash scripts/check-alert-metrics.sh` passes.
- [ ] `bash scripts/check-crd-drift.sh` passes.
- [ ] Manager-bootstrap test passes (every Setup*Controller wired).
- [ ] INV-007, INV-004, INV-014, INV-012, INV-013, INV-015 flipped from VIOLATED to VALIDATED.
- [ ] C4 health re-scored ≥ 3.5/4.0 (from 2.83/4.0).
- [ ] CHANGELOG.md updated under `Added`, `Changed`, `Fixed`, `Security` for each merged PR.
- [ ] Decision Records (`.claude/decisions/2026-04-26-T*.md`) signed off for all 8 T2/T3 tasks.
- [ ] No outstanding security/SRE veto blocks.
- [ ] Re-run a focused 2-phase mini-audit (Completeness + Validation) on operators after Phase 1 + Phase 2 — confirm all P1/P2 findings closed in DB.
- [ ] Reach maturity 4.0 (per LoopZero maturity assessment criteria).
