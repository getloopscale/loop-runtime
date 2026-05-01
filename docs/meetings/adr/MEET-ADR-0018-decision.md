# MEET-ADR-0018 — Decision Record (T2 Governance Review)

**Date:** 2026-04-29
**Topic:** ADR-0018 — Tenant CR Single Source of Truth (namespace `loopzero-system`)
**Type:** `architecture` + `controllers` + `security` (cross-cutting)
**Tier:** T2 (Tenant isolation change — Automatic T2 trigger per CLAUDE.md `pre-release-governance.md`)
**Scope reviewed:** T3.1 (operator/API hard-code) + T3.2 (ValidatingAdmissionPolicy)
**Out of scope:** T3.3 migration script (already shipped 2026-04-29 as T1 data-cleanup)

**Agents convened:** platform-architect, production-design-reviewer, change-validation-planner, k8s-controller-engineer
**Roles:**
- **Sponsor:** platform-architect (single-source-of-truth alignment)
- **Challenger:** production-design-reviewer (operational complexity / blast radius)
- **Owner (post-approval):** api-engineer + operator-engineer + infra-engineer (joint)

**Verdict:** **APPROVED_WITH_CONDITIONS**

---

## Stage 1 — Context

**Problem:** Two Tenant CRs with divergent `.spec.plan` (`enterprise` in `default`, `starter` in `loopzero-system`) coexisted post-dogfood iter 7-8. Plan enforcement (billing, quota, RBAC, cell placement) became non-deterministic because tenant-controller cluster-wide watch processed BOTH CRs and both reconciled to `Active`. Iter 3 incident demonstrated that removing finalizer on the stale CR cascaded destructive cleanup of resources SHARED with the canonical CR (HTTP 200 → HTTP 404 instantly).

**Decision Required:** Approve / reject T3.1 (hard-code namespace in operator/API code) and T3.2 (native VAP rejecting Tenant/Environment CREATE/UPDATE outside `loopzero-system`).

**Constraints:**
- Pre-release phase — backward break is acceptable when accompanied by migration path.
- 1 customer-impacting incident (iter 3 dogfood, recovered).
- T3.3 migration script already shipped (`infra/scripts/migrate-tenant-namespace.sh`, 229 LOC) — preserves invariant for existing clusters.
- Mgmt cluster pinned to K8s 1.32 → native VAP available (no cert-manager dependency).
- 6 VAPs already in `infra/helm/admission-policies/templates/` — adding a 7th is incremental.
- 1 customer testing identified — they need a clear migration window.

**Known Context (artifacts read by orchestrator):**
- `docs/decisions/ADR-0018-tenant-namespace-sot.md` (177 lines)
- `infra/scripts/migrate-tenant-namespace.sh` (229 lines, dry-run default, skip-reconcile guard from iter 3 incident)
- `api/internal/k8s/tenant_cr_manager.go:25-33` — `tenantNamespace` already a `var` defaulting `loopzero-system` BUT overridable via `TENANT_NAMESPACE` env var (the gap that allowed iter 7-8 misplacement was operator-side, not API-side; API has been correct since inception).
- `operators/cmd/main.go:436-444` — `ctrl.NewManager` created WITHOUT `Cache.DefaultNamespaces` restriction → cluster-wide watch.
- `operators/internal/controller/tenant_controller.go:283` — `r.List(..., client.InNamespace(tenant.Namespace))` already namespace-scoped per-Tenant for Environment GC, but `For(&loopzerov1.Tenant{})` at line 323 has no namespace filter.
- `infra/helm/admission-policies/values.yaml` — toggles pattern (`vaps.disallowLatestTag: true`, etc.) — adding `vaps.tenantNamespace: true` is consistent.
- `operators/internal/controller/tenant_isolation_test.go` exists — extending it for namespace-bound watch is feasible.
- 1 customer mentioned (referenced in ADR-0018 §Negative). Migration path: T3.3 must run BEFORE operator upgrade with T3.1 active.

**Proposal under review:** ADR-0018 §Decision (Layers 1+2). Layer 3 already merged.

---

## Stage 2 — Analyses

### platform-architect (Sponsor)

**Assessment (boundaries / source of truth):**

The decision is correct in principle. ADR-0018 brings Tenant + Environment CRs into alignment with secrets (`secret_k8s_manager.go:64` already hard-codes `mgmtNamespace = "loopzero-system"`) and with the four-planes architecture (CLAUDE.md §Architecture: Control Plane lives in `loopzero-system`). Allowing platform CRs in arbitrary namespaces violates the "platform-scoped vs user-scoped" boundary that the four-planes model depends on.

**The deeper issue is not "two CRs in two namespaces" — it is "platform CRDs admitted into user namespaces at all".** A user creating a Pod in `default` is normal; a user creating a Tenant in `default` is the platform losing control of its own boundaries. T3.2 (admission rejection) is the architectural fix; T3.1 (code hard-code) is the defense-in-depth.

**Risks identified:**
1. The `TENANT_NAMESPACE` env-var escape hatch (`tenant_cr_manager.go:28-33`) MUST be removed by T3.1, not preserved. Keeping it as "configurable" recreates the iter 7-8 misplacement vector via misconfigured env in a different way.
2. ADR-0018 currently says "hard-code" — but if a future multi-tenant-isolated cluster topology emerges (one operator per tenant in tenant-scoped namespace), the constant becomes a constraint. This is not a current concern (we are pre-release, single mgmt namespace), but the ADR should explicitly state "if this changes, requires new ADR".
3. ADR-0018 does not specify what happens to existing Tenant CRs in `default` if T3.3 was NOT run pre-upgrade. With T3.1 hard-coded + T3.2 enforcing, the operator simply ignores them and they become orphans (resources continue to exist, never reconciled). This is a fail-open silently — needs explicit handling.

**Objections:** None to the core decision. Two amendments required (see Conditions).

**Recommendation:** APPROVED with conditions.

**Conditions for approval:**
- C1: Remove `TENANT_NAMESPACE` env var entirely in T3.1; replace with a Go `const`. Document removal in CHANGELOG `[Removed]`.
- C2: Add an `Active`-state precondition check in tenant-controller: at startup, list Tenants cluster-wide once and emit a `MisplacedTenantDetected` warning event + Prometheus counter for any non-`loopzero-system` Tenant. Fail-loud, not fail-silent. (Operability requirement — see production-design-reviewer C5.)
- C3: ADR-0018 §Constraints must be amended: "future multi-namespace tenant topology requires a new ADR superseding this one". (Boundary lock-in is intentional but must be reversible.)

**Impact if ignored:**
Without C1 → iter 7-8-equivalent regression possible via operator misconfiguration in any future deployment.
Without C2 → orphan Tenant CRs in user namespaces continue running (zombie state) without operator visibility.
Without C3 → if a future requirement demands per-tenant namespace isolation, this ADR blocks it without forcing a re-review.

---

### production-design-reviewer (Challenger)

**Assessment (operational complexity / failure modes):**

The plan is sound but **the failure mode during the migration window is the riskiest moment**, not steady state. Steady state is simpler than what we have today (good). But the upgrade path from current → new world has at least three operator/admin failure modes that must be eliminated before merge.

**Failure modes identified:**

1. **VAP enabled before T3.3 ran** → existing Tenants in `default` cannot be UPDATED (because `UPDATE` is in `matchConstraints.operations`). Tenant-controller's status writes will be blocked by the API server. Tenant-controller will retry-loop, fill logs, possibly trigger leader election thrashing under sustained load. **This is a self-inflicted DoS.**

2. **T3.1 hard-code merged before T3.2 VAP** → API server now writes Tenants only to `loopzero-system`, but customer/admin self-applied Tenants in `default` remain reconciliation-active under cluster-wide watch. Plan enforcement determinism is NOT achieved; we just stopped CREATING duplicates. The state ADR-0018 promises requires BOTH layers active simultaneously OR the operator must restrict its watch. (This connects to k8s-controller-engineer's analysis — see below.)

3. **VAP `UPDATE` rule** rejects status subresource writes? Need to verify. If `matchConstraints.resources: ["tenants", "environments"]` includes status subresource on `UPDATE`, every reconcile cycle is blocked. K8s VAP typically excludes status by default, but this MUST be verified with a deliberate test, not assumed. This is a known footgun in admission policies.

4. **Migration script T3.3 ran in dry-run** is checked, but T3.3 exit code 2 ("manual decision required") is silent if pipeline does not gate on it. Sprint 8 runbooks reference exit codes — need to verify migration is in the upgrade runbook's pre-flight check.

5. **Helmfile rollout order** — admission-policies chart is in the L2 layer (per CHG-2026-04-28-LAYERED-BOOTSTRAP). Operator chart is L3+. If admission-policies upgrades first with VAP enabled but operator still has cluster-wide watch + old binary, behavior is split-brain. Needs explicit Helmfile dependency or runbook ordering.

**Risks identified:**
- DoS via misordered upgrade (high severity, high probability if no guard rails).
- Status subresource lock (medium severity, low probability but catastrophic — operator stops working).
- T3.3 silent skip in CI/CD pipeline (medium severity, medium probability).
- One-customer disruption window (low severity, but contractual — needs comms).

**Objections:**
1. ADR-0018 §Implementation Plan does not specify ORDER of T3.1 vs T3.2 deployment. Order matters — see Failure Mode #1 and #2.
2. No rollback procedure documented for T3.2 (VAP) beyond "Helm overlay toggle off". If VAP is rejecting valid traffic, a Helm rollback takes 30-90s — what happens to the rejected traffic during that window? (Answer: it's rejected. CLI surfaces "forbidden" error. Acceptable but must be in runbook.)
3. No explicit pre-flight gate in `task setup` / `helmfile apply` that runs T3.3 dry-run first.

**Recommendation:** APPROVED with conditions (operational guard rails).

**Conditions for approval:**
- C4: Mandatory deployment ORDER documented and enforced in Helmfile / runbook:
  - Step 1: Apply T3.3 migration (`task migrate:tenants -- --apply`) — exit 0 required.
  - Step 2: Roll out operator with T3.1 hard-code + namespace-scoped watch.
  - Step 3: Apply T3.2 VAP. (NOT before — VAP applied with stale Tenants in `default` causes DoS per Failure Mode #1.)
- C5: Add Prometheus metric `loopzero_misplaced_tenant_total{namespace}` exported by tenant-controller, alert if ≥1 for >5min. (Operability — covers C2 from platform-architect.)
- C6: VAP `matchConstraints` MUST explicitly exclude status subresource. Add `matchPolicy: Equivalent` and confirm via envtest spec that status writes pass through. Reference: K8s admission policy docs on subresources.
- C7: Add a runbook `docs/runbooks/adr-0018-rollout.md` with step-by-step procedure, abort criteria, and rollback steps for each layer. Reference from this decision record.
- C8: One-customer disruption: 24h advance notice + scheduled migration window with on-call attendance.

**Impact if ignored:**
- C4 ignored → DoS on production rollout.
- C6 ignored → operator stops reconciling all Tenants the moment VAP applies.
- C7 ignored → on-call has no playbook during incident.

---

### k8s-controller-engineer

**Assessment (controller-runtime watch + RBAC implications):**

The current operator manager (`operators/cmd/main.go:436-444`) has NO `Cache.DefaultNamespaces` set. This means:
- The Tenant informer watches **cluster-wide**.
- `r.List(ctx, &envList, client.InNamespace(tenant.Namespace))` at `tenant_controller.go:283` filters per-Tenant — so if a Tenant lands in `default`, the list call is correctly scoped to `default`. **This is why iter 7-8 worked**: the operator did process the `default` Tenant correctly (which is the bug — it should have refused).
- ClusterRole permissions for `loopzero.io/tenants,environments` are cluster-scoped.

**T3.1 must explicitly restrict the watch.** Hard-coding the constant in API is necessary but insufficient. The controller-runtime fix is:

```go
mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
    // ...
    Cache: cache.Options{
        DefaultNamespaces: map[string]cache.Config{
            "loopzero-system": {},
        },
    },
})
```

This bounds the cache + watch to `loopzero-system`. Tenant CRs in `default` become invisible to the operator (informer never sees them). RBAC can then be tightened from ClusterRole to namespace-scoped Role + RoleBinding in `loopzero-system` for the `loopzero.io/tenants,environments,applications,...` resources. Cluster-scoped ClusterRole still required for namespace creation, NetworkPolicy management, and other Workload Plane resources — those are by design cluster-scoped. **Only the platform CRDs (Tenant, Environment) can be namespace-bound.**

**Risks identified:**
1. **Multi-namespace Application/PreviewEnvironment CRs**: LoopZeroApplication CRs are deployed PER-tenant in tenant-scoped namespaces (`loopzero-tenant-{id}`). If we narrow `DefaultNamespaces` to ONLY `loopzero-system`, the operator stops watching Applications. Need `MultiNamespacedCacheBuilder`-equivalent or per-controller cache scopes (controller-runtime v0.18+ supports `cache.Options{ByObject: ...}` with per-Kind namespace lists).
2. **Owner references across namespaces**: Tenant in `loopzero-system` cannot own Environment in `loopzero-tenant-{id}` — owner refs are namespace-local. Verify that current architecture does NOT rely on cross-namespace owner refs. (Code at `tenant_controller.go:270` says `controllerutil.SetControllerReference(tenant, envCR, r.Scheme)` — if envCR is in tenant ns and tenant in loopzero-system, this fails.) **Need to read the actual ownership topology before tightening.**
3. **Test infrastructure**: `tenant_isolation_test.go` exists but envtest does NOT enforce VAPs by default. Need to add a separate VAP test using a real K8s API server (kind) or integration env that supports admission policies.

**Objections:**
1. T3.1 as currently scoped in ADR-0018 (only `tenant_cr_manager.go` hard-code) is **incomplete**. The operator-side watch restriction is not mentioned but is required for the ADR to deliver its promise.
2. RBAC tightening is mentioned as "Positive consequence" but no concrete change is specified. Either commit to it (with separate ADR for RBAC) or remove from §Positive — don't claim a benefit you don't deliver.

**Recommendation:** APPROVED with conditions (scope expansion for completeness).

**Conditions for approval:**
- C9: T3.1 must include `cache.Options{ByObject: ...}` with explicit per-Kind namespace scopes:
  - `Tenant`, `Environment` → `loopzero-system` only.
  - `LoopZeroApplication`, `PreviewEnvironment`, `ExposedRoute` → cluster-wide (workload namespaces are dynamic).
  - `PlatformBootstrap`, `GitRepositoryBinding`, `ClusterCredential` → `loopzero-system`.
- C10: Verify owner reference topology BEFORE merge. If `Tenant loopzero-system/foo` owns `Environment loopzero-system/foo-prod` (both in same ns), no problem. If Environment lives in tenant ns, Tenant must not be the owner — use labels/annotations for the link instead. Document the ownership graph in ADR-0018 §Implementation Plan.
- C11: Add envtest spec at `operators/internal/controller/tenant_namespace_isolation_test.go`:
  - Given operator started with `loopzero-system`-only Tenant cache,
  - When a Tenant CR is created in `default` (bypass VAP),
  - Then the controller does NOT reconcile it (no events, no status update).
- C12: RBAC tightening is **out of scope** of this decision — defer to a follow-up ADR-0019. Remove "RBAC simplification" from ADR-0018 §Positive consequences to avoid scope creep, or commit to ADR-0019 within Sprint 9.

**Impact if ignored:**
- C9 ignored → operator stops reconciling user Applications, breaks deploy flow.
- C10 ignored → `controllerutil.SetControllerReference` returns error, every Environment reconcile fails.
- C11 ignored → regression possible without test coverage.

---

### change-validation-planner

**Assessment (test plan / regression strategy):**

ADR-0018 §Implementation Plan §4 lists 3 test layers (unit, integration, e2e) at high level. The plan is directionally correct but **insufficiently specific to ratify**. Below is the minimum test matrix that must be GREEN before merge.

**Test matrix (mandatory):**

| ID | Layer | Test | Owner | Blocks merge? |
|----|-------|------|-------|---------------|
| T-01 | unit | `tenant_cr_manager_test.go::TestEnsureTenant_AlwaysUsesLoopZeroSystemNamespace` — assert namespace=loopzero-system regardless of any input. | api-engineer | YES |
| T-02 | unit | `tenant_cr_manager_test.go::TestEnvVarOverrideRemoved` — confirm `TENANT_NAMESPACE` env has NO effect (compile-time `const`). | api-engineer | YES |
| T-03 | envtest | `tenant_namespace_isolation_test.go::TestOperatorIgnoresTenantsOutsideLoopZeroSystem` — per C11. | operator-engineer | YES |
| T-04 | envtest | `tenant_controller_test.go::TestEnvironmentOwnershipAcrossNamespaces` — verify owner-ref topology per C10. | operator-engineer | YES |
| T-05 | helm/kubeconform | `task validate:helm-charts` includes new VAP — schema valid, CEL parseable. | infra-engineer | YES |
| T-06 | integration (kind) | `infra/tests/admission/vap-tenant-namespace_test.go` — apply Tenant in `default`, expect HTTP 403/forbidden with the exact error message from ADR-0018 §Layer 2. | infra-engineer | YES |
| T-07 | integration (kind) | VAP allows status subresource writes (C6 verification). | infra-engineer | YES |
| T-08 | integration (kind) | VAP allows updates to existing Tenant in `loopzero-system` (does not block reconcile loop). | infra-engineer | YES |
| T-09 | e2e | `infra/tests/e2e/migrate_tenants_test.go` — provision cluster with Tenant in `default`, run T3.3 `--apply`, verify single CR remains in `loopzero-system` with correct plan, no orphan ArgoCD Apps, no orphan namespaces. | qa | YES |
| T-10 | e2e | `loopzero init` in fresh cluster with VAP active → Tenant created in `loopzero-system`, no admission errors, `loopzero deploy` succeeds end-to-end. | qa | YES |
| T-11 | regression | All 1079 CLI tests + 41 Go packages + 200+ envtest specs PASS post-merge. | ci | YES |
| T-12 | observability | New metric `loopzero_misplaced_tenant_total` exported and scraped by Prometheus (per C5). | sre | NO (post-merge) |
| T-13 | runbook | Manual exercise of `docs/runbooks/adr-0018-rollout.md` on dev cluster — all steps execute, rollback works. | sre | YES (before prod rollout) |

**Regression strategy:**
- The migration script T3.3 was already exercised in iter 3 dogfood. We have ONE incident as ground truth. The skip-reconcile guard added post-incident must be tested again BEFORE T3.1+T3.2 merge to confirm it still works after operator code changes.
- Tenant_isolation_test.go is the existing closest test — extend it, do NOT create a parallel fixture (DRY).

**Performance / scale validation:**
- Cluster-wide → namespace-scoped cache reduces operator memory footprint. Validate via `kubectl top pod` before/after on dev cluster. Expect modest improvement; not a goal.

**Risks identified:**
- VAP CEL expression syntax errors fail at admission time, not at chart install. T-05 catches schema but NOT CEL eval. Need T-06 to actually exercise the policy on a real API server.
- envtest does NOT support VAP enforcement (it does not run admission webhook chain by default for VAPs in older controller-runtime). Verify version compatibility. If unsupported, T-06/T-07/T-08 must run in kind, not envtest.

**Objections:**
1. ADR-0018 §4 e2e test ("`loopzero init` in fresh cluster") is the only e2e mentioned. Need T-09 (migration path) explicitly — without it we have no validation that the upgrade ORDER (C4) actually works.
2. No regression test for the iter 3 incident itself (skip-reconcile guard). This is the highest-severity past failure — mandatory regression coverage.

**Recommendation:** APPROVED with conditions (test plan ratified above as the minimum bar).

**Conditions for approval:**
- C13: Test matrix T-01 through T-11 all GREEN before merge. T-12 within 7 days post-merge. T-13 before production rollout.
- C14: Add explicit regression test for the iter 3 incident: `migrate_tenant_namespace_iter3_regression_test.sh` or equivalent that asserts skip-reconcile annotation prevents cascade cleanup.

**Impact if ignored:** Test gaps allow regressions; the system becomes harder to trust under change pressure.

---

## Stage 3 — Confrontation

### Conflict 1: Should `TENANT_NAMESPACE` env var be removed or preserved?

| Agent | Position |
|-------|----------|
| platform-architect | Remove (C1). Configurable namespace recreates the misplacement vector. |
| production-design-reviewer | Remove. Hard `const` is also simpler — KISS. |
| k8s-controller-engineer | Remove. Cache namespace scope is now also hard-coded; env var override would create inconsistency between cache and code paths. |
| change-validation-planner | Remove (T-02 enforces). |

**Resolution:** Unanimous. **REMOVE** `TENANT_NAMESPACE` env var. T3.1 implements `const tenantNamespace = "loopzero-system"` (Go `const`, not `var`).

---

### Conflict 2: Order of T3.1 vs T3.2 deployment

| Agent | Position |
|-------|----------|
| production-design-reviewer | T3.3 → T3.1 → T3.2 mandatory. VAP last, after operator no longer creates duplicates and migration cleared backlog. |
| k8s-controller-engineer | Agrees. Cache scope must be active before VAP, otherwise operator might still see + reconcile stale Tenants while VAP rejects new writes — split-brain. |
| platform-architect | No strong preference, but agrees the ORDER must be deterministic and runbook-enforced. |
| change-validation-planner | T-09 + T-13 must validate the ORDER, not just the end state. |

**Resolution:** Mandatory order: **T3.3 (data clean) → T3.1 (operator + API code) → T3.2 (VAP)**. Encoded in `docs/runbooks/adr-0018-rollout.md` (C7) and Helmfile dependency / pre-install hooks.

---

### Conflict 3: Is RBAC tightening in scope of this ADR?

| Agent | Position |
|-------|----------|
| platform-architect | In scope as a "Positive consequence" but does not need to ship in T3.1. |
| production-design-reviewer | Out of scope for T3.1+T3.2. Defer. |
| k8s-controller-engineer | Out of scope. Defer to ADR-0019. Remove from §Positive consequences. |
| change-validation-planner | Out of scope. Adding RBAC change here doubles the test surface. |

**Resolution:** RBAC tightening is **deferred to ADR-0019** (Sprint 9). ADR-0018 §Positive consequences amended to remove "RBAC simplificado" or annotate it as "follow-up ADR-0019". (C12.)

---

### Conflict 4: Is ADR-0018 scope sufficient as written?

| Agent | Position |
|-------|----------|
| platform-architect | Sufficient with C1, C2, C3. |
| production-design-reviewer | Sufficient with C4-C8 (operability). |
| k8s-controller-engineer | **Insufficient as written.** Operator-side watch restriction is the actual mechanism — ADR mentions it parenthetically but does not specify cache scoping. C9, C10, C11 must amend the ADR. |
| change-validation-planner | Test plan in ADR §4 is too high-level — needs T-01 through T-13. C13, C14. |

**Resolution:** **ADR-0018 must be amended** before final approval to incorporate:
- Removal of env var (C1)
- Cache scoping per-Kind (C9)
- Owner reference topology verification (C10)
- Mandatory test matrix T-01..T-14 (C13, C14)
- Explicit deployment order (C4)
- Operability metrics (C5)
- Status subresource exclusion (C6)
- Rollout runbook (C7)
- Customer notice (C8)
- ADR-0019 follow-up reference (C12)

These are not blockers — they are mandatory pre-implementation amendments. The decision is APPROVED conditional on the ADR being updated to v2 with these conditions before T3.1 work begins.

---

### Self-challenge (consensus check by orchestrator)

All four agents converged on APPROVED. Per Stage 3 protocol I challenge the consensus:

**What could go wrong that none of the agents flagged?**

1. **Cosign / image signing dependency.** policy-controller is in deny-by-default mode (CLAUDE.md Sprint 6 status). Adding a 7th VAP does not interact with image policy — VAPs are validating, ClusterImagePolicy is signing — but if both fire on the same admission request, ordering could matter. **Verified:** they target different `matchConstraints` resource kinds (Tenant vs Pod), no interaction.

2. **API contract drift.** If `loopzero init` and `loopzero deploy` CLI flows assume Tenant lives in a specific namespace, hard-coding propagates correctly. **Verified:** CLI does not address Tenant namespace explicitly — API hides it. No CLI surface change. JSON contract unaffected. Exit codes unaffected. Rule 3 (no internal terms) — VAP rejection error message says "loopzero-system" which is an internal term. **NEW FINDING:** Surfacing VAP error to user via CLI must NOT show "loopzero-system" — it should map to product language. Add this as C15.

3. **Disaster recovery / backup restore.** If a backup is restored that has Tenants in `default` (PG-restored-then-cluster-recovered), VAP will block any update to those Tenants. **Mitigation:** restore procedure must run T3.3 as part of recovery runbook. Add as C16.

**These are real gaps.** Adding C15 and C16.

---

## Stage 4 — Decision

**Chosen approach:** Implement T3.1 + T3.2 as specified in ADR-0018 with mandatory conditions C1-C16 below. Veto-status-by-default (no Security veto, no SRE veto, no Architect-vs-Distributed-Systems divergence requiring separate ADR — see "Verdict" below).

**Alternatives rejected:**

| Alternative | Reason for rejection |
|-------------|----------------------|
| Status quo (do nothing) | Iter 3 incident proved this is unsafe. Plan enforcement is non-deterministic. Reject. |
| Layer 1 only (code hard-code, no VAP) | Customers / admins applying CRs manually still bypass. Defense-in-depth requires both. Reject. |
| Layer 2 only (VAP, no code hard-code) | Code path is the primary write surface; relying solely on admission means a misconfigured operator with elevated RBAC could still write outside the policy via subresources, server-side apply, or future code changes drifting. Reject. |
| ValidatingWebhookConfiguration (custom webhook) instead of native VAP | Adds cert-manager dep, adds operator pod load, adds failure mode (webhook unavailable → API down). Native VAP runs in API server. Reject. |
| Move all CRs to a per-tenant namespace topology | Current architecture is single-namespace platform CRs. Multi-namespace topology is a larger architectural change requiring a separate ADR. Reject (out of scope). |
| Defer to Sprint 9 | The migration script (T3.3) is shipped. Hardening should follow within Sprint 8 to close the loop. The longer T3.1+T3.2 wait, the more chance of a second iter-3-style incident. Reject deferral. |

**Trade-offs consciously accepted:**
1. Backward break for self-hosted customers with Tenants in non-`loopzero-system` namespaces. Mitigated by T3.3 migration script + 24h notice (C8).
2. Loss of operator deployment flexibility (cannot easily run multi-mgmt-namespace topology in future). Mitigated by ADR-0018 §Constraints amendment (C3) — future change requires new ADR.
3. Incremental complexity in admission-policies chart (one more VAP to maintain). Mitigated by following established pattern (6 VAPs already in chart) and Helm toggle (C consistent with existing chart values pattern).

**Risks accepted (with mitigation and owner):**

| Risk | Severity | Mitigation | Owner |
|------|----------|------------|-------|
| VAP applied before T3.3 → DoS | High | C4 mandatory order + runbook gating | sre / on-call |
| Status subresource blocked by VAP | Medium | C6 explicit exclusion + T-07 test | infra-engineer |
| Customer disruption window | Low | C8 24h notice + scheduled migration | account ops |
| Backup restore reintroduces misplaced Tenants | Low | C16 DR runbook update | sre |
| VAP rejection message leaks "loopzero-system" via CLI | Low | C15 CLI error mapping | api-engineer + cli-engineer |

**Risks NOT accepted (block until mitigated):**
- None. All risks have mitigations, owners, and validation tests in the matrix.

**Mandatory actions before implementation merge:**

1. **[ADR amendment]** Update `docs/decisions/ADR-0018-tenant-namespace-sot.md` to v2 incorporating conditions C1-C16. Status: DRAFT → APPROVED. Owner: **decision-record-moderator**.
2. **[Runbook]** Create `docs/runbooks/adr-0018-rollout.md` with deployment order, abort criteria, rollback, DR-restore handling. Owner: **sre / production-design-reviewer**.
3. **[Tests T-01..T-08]** Pre-implementation: write failing tests for unit + envtest + chart validation. Owner: **change-validation-planner + agent:test**.
4. **[Code T3.1]** Implement: `const tenantNamespace = "loopzero-system"` (remove env var); cache scoping in operator manager per C9; verify owner-ref topology per C10. Owner: **api-engineer + operator-engineer**.
5. **[Chart T3.2]** Implement: `infra/helm/admission-policies/templates/vap-tenant-namespace.yaml` + `vaps.tenantNamespace` toggle in values.yaml; verify status subresource exclusion. Owner: **infra-engineer**.
6. **[Tests T-09, T-10]** E2E validation post-implementation, before merge. Owner: **qa**.
7. **[Observability]** Implement metric `loopzero_misplaced_tenant_total{namespace}` + alert (C5). Owner: **sre**.
8. **[CLI]** Map VAP rejection to product-language error per Rule 3 (C15). Owner: **cli-engineer**.
9. **[Customer comms]** 24h migration window notice to identified customer (C8). Owner: **account ops**.
10. **[Follow-up ADR]** Create `ADR-0019-tenant-controller-rbac-tightening.md` placeholder for Sprint 9 (C12). Owner: **platform-architect**.

---

## Stage 5 — Execution Contract

**Owner (joint):** api-engineer + operator-engineer + infra-engineer + sre + cli-engineer

**Deliverables:**

| # | Artifact | Path | Owner |
|---|----------|------|-------|
| D1 | ADR-0018 v2 | `docs/decisions/ADR-0018-tenant-namespace-sot.md` | decision-record-moderator |
| D2 | Rollout runbook | `docs/runbooks/adr-0018-rollout.md` | sre |
| D3 | Code: hard-coded const + cache scope | `api/internal/k8s/tenant_cr_manager.go`, `operators/cmd/main.go`, related cmd setup | api/operator-engineer |
| D4 | VAP chart template | `infra/helm/admission-policies/templates/vap-tenant-namespace.yaml` | infra-engineer |
| D5 | VAP toggle | `infra/helm/admission-policies/values.yaml` (`vaps.tenantNamespace: true`) | infra-engineer |
| D6 | Test matrix T-01..T-11 | `api/internal/k8s/*_test.go`, `operators/internal/controller/tenant_namespace_isolation_test.go`, `infra/tests/admission/`, `infra/tests/e2e/migrate_tenants_test.go` | change-validation-planner + agent:test |
| D7 | Metric + alert | tenant-controller export + Prometheus rule | sre |
| D8 | CLI error mapping | `cli/src/...` error handler | cli-engineer |
| D9 | CHANGELOG entry | `CHANGELOG.md` `[Unreleased]` `[Removed]` env var, `[Added]` VAP | doc owner |
| D10 | ADR-0019 placeholder | `docs/decisions/ADR-0019-tenant-controller-rbac-tightening.md` | platform-architect |

**Validations:**

- `task test:unit` GREEN (T-01, T-02 included).
- `task test` GREEN (envtest T-03, T-04 included).
- `task validate:manifests` + `task validate:helm-charts` GREEN (T-05).
- Integration tests (kind) GREEN (T-06, T-07, T-08).
- E2E (T-09, T-10) GREEN against dev cluster.
- Manual runbook exercise on dev cluster (T-13) GREEN.
- Pre-existing 1079 CLI tests + 41 Go packages + 200+ envtest specs + 42 pulumi specs PASS (T-11).
- `task lint` PASS.
- Rule 3 audit (no `loopzero-system`, `argocd`, `gitea`, etc. leaked to CLI output) PASS — VAP error message mapped via C15.

**Definition of Done (checklist):**

- [ ] ADR-0018 v2 merged with all conditions C1-C16 incorporated.
- [ ] Rollout runbook published.
- [ ] T3.3 migration script verified GREEN (regression for iter 3 incident).
- [ ] T3.1 code merged: `const tenantNamespace`, cache scoping, owner-ref topology verified.
- [ ] T3.2 VAP merged with toggle + status subresource exclusion test GREEN.
- [ ] Test matrix T-01..T-11 GREEN; T-13 manually exercised on dev cluster.
- [ ] Metric + alert deployed.
- [ ] CLI error mapping done.
- [ ] CHANGELOG updated.
- [ ] Customer notice sent + migration window scheduled.
- [ ] ADR-0019 placeholder created.
- [ ] Production rollout per runbook order: T3.3 apply → operator upgrade → VAP enable.
- [ ] Post-rollout: zero `loopzero_misplaced_tenant_total > 0` alerts for 7 days.

---

## Verdict — Final

**APPROVED_WITH_CONDITIONS**

- No Security veto (no agent from security committee was required for this scope; tenant isolation is architectural, the security perimeter is unchanged — VAP strengthens it).
- No SRE veto (production-design-reviewer raised operability conditions C4-C8, all accepted as conditions, not blockers).
- No platform-architect vs distributed-systems divergence (single-cluster control plane decision; no cross-cluster coordination implication).
- DX impact: **C15** raised (CLI error message must avoid "loopzero-system" leak). Accepted as condition, not blocker — work item is small.
- Verification can propose tests: **YES** (test matrix T-01..T-14 ratified).

**Conditions C1-C16 are mandatory pre-implementation. Implementation may begin only after:**
1. ADR-0018 v2 amendment merged.
2. Test matrix T-01..T-08 written and FAILING (TDD red).
3. Rollout runbook drafted.

**This decision record is binding. Any deviation requires a new T2 meeting.**

---

## References

- Source ADR: `/home/paulo/loopzero/docs/decisions/ADR-0018-tenant-namespace-sot.md`
- Companion script: `/home/paulo/loopzero/infra/scripts/migrate-tenant-namespace.sh`
- Code touched: `/home/paulo/loopzero/api/internal/k8s/tenant_cr_manager.go`, `/home/paulo/loopzero/operators/cmd/main.go`, `/home/paulo/loopzero/operators/internal/controller/tenant_controller.go`
- Chart: `/home/paulo/loopzero/infra/helm/admission-policies/`
- Existing isolation test: `/home/paulo/loopzero/operators/internal/controller/tenant_isolation_test.go`
- Governance rule: `/home/paulo/loopzero/.claude/rules/pre-release-governance.md` (Tenant isolation = T2 trigger)
- Sibling ADRs: ADR-0015 (adopt-or-create), ADR-0017 (auto-sync evidence)
- Sibling meeting records: MEET-2026-04-27-060, MEET-2026-04-27-061 (Sprint 8 v3.2)
