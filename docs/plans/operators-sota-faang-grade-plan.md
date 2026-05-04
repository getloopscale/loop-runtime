# Plan: Operators SOTA FAANG-Grade Improvement

> **Version 1.1** — Updated 2026-05-04 per MEET-2026-05-04-068 governance gate (T3 review). 5 ADRs ratified (0023-0027). 23 conditions integrated from 6 specialist reviewers (platform-architect, sre-engineer, platform-security-engineer, k8s-controller-engineer, verification-architect, dx-engineer). See **Governance Conditions Appendix** below for the binding adjustments to v1.0 task descriptions.
>
> **Version 1.0** — This plan executes every recommendation from `docs/technical/operators/improvement-roadmap.md` in a phased, dependency-aware sequence to take the operators module from SOTA score 6.0/10 (Operator SDK Capability Level III) to 8.5/10 (Level IV declared, with measurable progress toward Level V). It eliminates ~5800 LOC of duplicated controllers between `internal/controller/` and `internal/{tenant,app,preview,platform}/`, adopts kubebuilder/kstatus/kpt conventions universally (predicates, Ready/Reconciling/Stalled, RBAC markers), declares operator capability levels formally, and lays the groundwork for post-launch evolutions (multicluster-runtime, Korifi-style runtime abstraction, CRD evolution webhooks). All phases are TDD-driven, with each task gated by RED-GREEN-REFACTOR cycles, code-audit checks, and the mandatory `/dogfood full` validation at the end.

---

## Governance Conditions Appendix (v1.1, MEET-2026-05-04-068)

These conditions are BINDING amendments to the v1.0 task descriptions below. Where a condition contradicts v1.0 task text, the condition wins. ADR references in this appendix point to the source of authority.

### Phase 1 ordering revisions (per ADR-0023 + sre-engineer veto)

- **T5.4 HOISTED to Phase 1.** Cache scoping congruence test runs between T1.3 and T1.4 (NOT in Phase 5). Rationale: deleting monolith without proving split scopes are congruent ships regression discovered only in dogfood.
- **AP-3 (silent error swallow at `application_controller.go:179-181`) HOISTED to Phase 1.** Either as new T1.0.5 (pre-migration cleanup) OR as T1.6 explicit verification "no E2E test depends on AP-3 swallow." Plan v1.0 buried this in T2.4.
- **T1.5 atomic helmfile swap REJECTED.** Replaced with 4-stage rollout per sre-engineer:
  - Stage A: Coexistence (4 split + 1 monolith both running, shared lease names, no reconcile splits).
  - Stage B: Per-controller cutover, ascending blast radius (PlatformBootstrap → Preview → Application → Tenant). 60min observation per controller.
  - Stage C: 24h soak per cell. cell-1 first; cell-2 only after cell-1 GREEN.
  - Stage D: Delete monolith chart only after 30d `OperatorRestartRate24h` silent.
- **T1.4 deletes `cmd/main.go` only at Stage D.** Local development can delete sooner; production deletion is the final stage.
- **Production rollout SCOPED OUT.** Phase 1 in this plan ends at local k3d migration + envtest + E2E smoke GREEN. Production cutover handled via separate `CHG-NNNN-OPERATOR-SPLIT-PROD` change record.

### Verification mandates (per verification-architect 5 vetos)

1. **Drift baseline document** committed BEFORE T1.1 starts: `docs/plans/operators-sota-faang-grade-plan/drift-baseline.md`. Per-pair diff (monolith vs split) with explicit "mechanical / semantic-port-N-lines" classification. Already known: ClusterCredential 78 LOC.
2. **T4.3 analyzer fixture suite** committed alongside `hack/validate-rbac/`: `testdata/{positive,negative,false-positive}/` minimum 5+3+3 cases.
3. **Dogfood Special Validation Steps** codified as committed scripts: `infra/tests/dogfood/operators-sota/{blast-radius,kstatus-interop,rbac-drift-gate,capability-matrix-audit}.sh`. Manual bash in markdown is NOT a gate.
4. **E2E baseline file** committed: `docs/plans/operators-sota-faang-grade-plan/e2e-baseline.txt` — exact 25 currently-green test names. Prevents silent substitution.
5. **Helm rollback runbook with failure injection.** `infra/scripts/operator-cutover-rollback.sh` MUST be executed end-to-end against dev cluster with proof in PR (not just pre-flight steps 1-2 as current runbook does).

### Security mandates (per ADR-0026 + platform-security-engineer 7 conditions)

1. Marker style guide BANS `resources=*` and `verbs=*`; CI enforces.
2. AST analyzer distinguishes `r.Client` (local, marker-checked) from `r.RuntimeClientFactory.For(cellID)` and `r.BuildClient` (cross-cluster). Cross-cluster gate via `kubectl auth can-i` matrix in CI.
3. T4.1 PR includes 3-way diff: `current-handedited.yaml` vs `generated.yaml` vs `intersection.yaml`. Net-new permissions require explicit per-rule justification.
4. Documented analyzer blind-spot list: `hack/validate-rbac/KNOWN_GAPS.md` with negative test fixtures.
5. Phase 1 SA rename safety: `extraSubjects` knob enables dual-binding (`tenant-controller` + `loopzero-tenant-operator`) on cell ClusterRoleBindings during rollout. NetworkPolicy `podSelector` widened. Pre-flight inventory `grep -r "tenant-controller\|loopzero-controller" infra/ charts/ operators/` audited.
6. cell-1/cell-2 ClusterCredential Secrets re-validated post-rollout via `task k3d:heal` family + `kubectl auth can-i` matrix BEFORE legacy chart removed.
7. `controller-gen paths=` scoped per binary (`./internal/tenant/...`), NOT `./operators/...` as v1.0 said.

### SRE / Observability mandates (per ADR-0024 + sre-engineer)

1. ServiceMonitor selector migration in T1.5 PR — `infra/helm/monitoring/manifests/servicemonitor-tenant-controller.yaml` MUST update selector before cutover (current `app: tenant-controller` matches zero new pods → silent metrics blackout).
2. New required metrics:
   - `loopzero_status_update_conflicts_total{controller}` (D2 conflict observability)
   - `controller_runtime_active_workers{controller, binary}` (saturation alert)
   - `workqueue_depth{controller}` per binary
   - `leader_election_master_status` per binary (split-brain detection)
   - `up{job="loopzero-*-operator"} == 0 for 2m` page per binary
3. Health probes: liveness MUST NOT depend on cache sync; readiness MUST wait for cache sync (`mgr.AddReadyzCheck("informer-sync", mgr.GetCache().WaitForCacheSync)`).
4. **Dogfood Gate enhanced (per sre-engineer):**
   - Minimum 4-hour soak with continuous deploy traffic at production baseline (~30/h auto-deploy).
   - Chaos injection (API-server 429s via Chaos Mesh, network partition mgmt↔workload, PG advisory lock contention).
   - cell-2 isolation regression test re-run.
   - `operator-cutover-rollback.sh` executed against dirty environment with proof.
   - SLO assertion: `loopzero deploy P95 < 705ms` (cell-2 baseline per CHG-2026-05-03).
   - T1.5 dogfood gate is `full`, NOT `smoke` — pick one (resolves T1.5 vs T1.6 vs Final Gate ambiguity).

### kubebuilder convention mandates (per ADR-0024 + ADR-0025 + k8s-controller-engineer)

1. **D2 helper:** `defer ctx.Err()` short-circuit; max-attempts cap with `wait.Backoff{Steps: 5, Cap: 2*time.Second}`; spec-mutation guard via deep-copy reset; SSA evaluation deferred to post-launch.
2. **D3 SetReadinessSummary:** `defer` pattern in each Reconcile (runs even on panic); idempotency test FIRST; ClusterCredential `Test_ClusterCredential_SecretNotFoundExcludesReady` mandatory.
3. **D4 dead-marker analyzer:** every marker verb MUST have at least one matching `r.Client.X` call. `Test_RBACAnalyzer_DetectsExtraMarker` mandatory.
4. **T5.4 cache scoping:** add Runtime layer `TestCacheScopingCongruence_Runtime` (boot manager via envtest, inspect `mgr.GetCache().GetInformersByGVK()`) alongside Static AST layer.
5. **D2 missing tests added:** `Test_UpdateStatusWithRetry_MutateModifiesSpec_IsRejected`, `Test_UpdateStatusWithRetry_ContextCanceledMidLoop`, `Test_UpdateStatusWithRetry_GenericNonConflictError_NoRetry`, `Test_UpdateStatusWithRetry_RaceWith3Writers` (envtest).

### DX mandates (per dx-engineer 7 conditions)

1. Shared label `app.kubernetes.io/component: loopzero-operator` on all 4 deployments (Tilt + Helm). Enables `kubectl logs -l app.kubernetes.io/component=loopzero-operator -n loopzero-system --all-containers --max-log-requests=4 -f`.
2. `task doctor` extended with `=== Operator Pods ===` section (`kubectl rollout status deploy/{tenant,app,preview,platform}-operator -n loopzero-system`).
3. `task logs:operators` (all) and `task logs:operator -- <name>` helpers in `taskfiles/dev.yml`.
4. `task debug:operator -- <name>` with `kubectl port-forward 2345:2345` + `dlv connect` instructions.
5. T1.5 enumerates 6 fan-out points (ServiceMonitor, NetworkPolicy, alert rules, cutover-rollback.sh, rbac-subset-check.sh, CI workflow path filters) and updates them in same PR.
6. `capability-matrix-audit.sh` CI gate added to `validate:docs` before T5.1 closes (greps every `Setup*`, asserts matrix coverage with file:line + test refs).
7. `docs/technical/operators/README.md` + per-binary runbooks updated with "binary → CRD → debug command" table.

### Phase 6 SCOPED OUT of this plan (per platform-architect)

`multicluster-runtime` adoption (T6.1) is load-bearing for cell-2 LIVE today. Phase 6 split into separate plan document. Listed here as RFC stubs only — implementation requires separate Change Governor flow gated on Sprint 9 launch + 30-day post-launch stability. **This plan's Global DoD covers Phases 0-5 only.**

---

## Context

The operators module is the heart of LoopZero's control plane: 8 controllers + 10 CRDs orchestrating Tenant, Environment, Application, Exposure, Preview, ClusterCredential, GitRepositoryBinding, and PlatformBootstrap lifecycles. As of 2026-05-04, audit (see `docs/technical/operators/improvement-roadmap.md`) identified the following blocking and non-blocking gaps:

**Blocking (Sprint 9 prerequisite):**
- `~5,802 LOC duplicated` between `operators/internal/controller/*` (monolithic, deployed in production) and `operators/internal/{tenant,app,preview,platform}/*` (split, "forward-looking"). The pairs differ only in `package` declaration and a tracing helper indirection. **The split tree has 0 test files**; `operatortest/setup.go:24` imports only the monolith. ClusterCredential pair already diverged 78 lines — drift in progress.
- Test coverage of split tree: 0 (vs 21 test files in monolith).
- `cmd/registration_test.go` validates wiring only for the monolith, not the 4 split binaries.

**Non-blocking but FAANG-bar lacking:**
- **Zero predicates** (`predicate.GenerationChangedPredicate`) across all controllers — every `Status().Update` retriggers reconcile (50–500x amplification per kubebuilder docs).
- **kpt convention partial**: `ReasonReconciling` exists but no `TypeStalled`, no universal `TypeReady` summary. `kubectl wait --for=condition=Ready` works inconsistently.
- **Zero `kubebuilder:rbac` markers**; `config/rbac/role.yaml` is hand-edited (drift risk).
- **`retry.RetryOnConflict`** used in only 1 file (`argocd/real_provisioner.go:237`) — most Status updates produce log noise on contention.
- No formal **Operator Capability Level** declaration; LoopZero has Level IV ingredients (43+ metrics, KEDA, application reaper, heal scripts) but nothing maps them.

**Anti-patterns flagged with file:line refs in roadmap §AP-1..AP-5**, including hardcoded condition strings (`application_controller.go:269-282`), silent error swallow (`application_controller.go:179-181`), and competing periodic-poll-vs-watch in ClusterCredential.

**Source of truth:** `docs/technical/operators/improvement-roadmap.md` (the SOTA research artifact); `docs/technical/operators/sota-gates.md` (now contains gates OP-G01–G21 after the research update).

## Objective

"Done" means: all 21 SOTA gates (OP-G01–G21) MET with verifiable evidence, the operators module has a single source of truth for every controller, kstatus-compliant status across all CRDs, generated RBAC, declared capability levels, and `/dogfood full` passes on clean infrastructure with zero S0/S1 findings.

Specific measurable goals:

1. **-5800 LOC duplicated** → 0 LOC duplicated. Single canonical implementation per controller.
2. **Predicate filtering**: ratio `controller_runtime_reconcile_total` / `spec_changes` < 5x (down from 50–500x).
3. **kstatus compliance**: `kubectl wait --for=condition=Ready` works for all 10 CRDs.
4. **RBAC generated**: `git diff config/rbac/role.yaml` is empty after `task generate:rbac` for any non-RBAC PR.
5. **Capability Level IV declared**: ADR published with evidence matrix (controller → level → file:line + test).
6. `/dogfood full` PASS with 0 S0, 0 S1 findings related to operators scope.

## ADRs

### D1 — Split tree is canonical; monolith deprecated

**Decision:** `operators/internal/{tenant,app,preview,platform}/` becomes the canonical implementation. `operators/internal/controller/` is removed, and `operators/cmd/main.go` is removed alongside it. The 4 split binaries (`tenant-operator`, `app-operator`, `preview-operator`, `platform-operator`) become the only operator binaries.

**Rationale:** Per CLAUDE.md, the monolith deploys today via `infra/helm/tenant-controller`, but the split charts in `charts/loopzero-*-operator/` are already prepared. Cache scoping per-Kind (ADR-0018 v2 §C9) is more granular in split binaries; blast radius isolation is concrete (an OOM in app-operator does not impact tenant-operator). The duplication exists only because we never finished the migration. Three alternatives considered:

- **A: Keep monolith, delete split.** Discards cache-scoping wins, blast-radius wins, and the 4 prepared Helm charts. Aligns with current production but accumulates technical debt.
- **B: Keep both, eliminate code duplication via shared package.** Refactor `internal/controller/` and `internal/tenant/` to both consume `internal/{domain}/core/` shared types. Adds indirection without solving the original "two binaries to test" problem.
- **C (chosen): Migrate to split, delete monolith.** Single canonical path forward; production already supports it via existing Helm charts.

**Consequences:**
- Enables: per-domain leader election, granular cache scoping, smaller binaries (~16 MB each vs 63 MB monolith), independent deploy cadence.
- Constrains: production rollout requires Helm chart swap (`infra/helm/tenant-controller` → 4 charts) coordinated via ArgoCD; mid-migration period requires both running. Mitigated by gradual rollout (Phase 1 of this plan).

### D2 — `retry.RetryOnConflict` is mandatory for all Status updates

**Decision:** Every `r.Status().Update(ctx, obj)` call MUST be wrapped in `retry.RetryOnConflict(retry.DefaultRetry, func() error {...})`. Bare Status updates are forbidden in new code (CI gate via static analysis).

**Rationale:** Per kubebuilder good-practices (§Conflict handling), retry-on-conflict is the standard idiom for status updates under contention. Production faces 30+ concurrent webhooks (GitHub auto-deploy) hitting the same controller, generating "the object has been modified" log noise that masks real bugs. LoopZero already uses this pattern in `argocd/real_provisioner.go:237` — it works; we just don't apply it consistently.

**Consequences:**
- Enables: clean logs in production, faster operator recovery, lower P95 reconcile latency under load.
- Constrains: each Status update site grows from 1 line to ~5 lines. Mitigated by helper `conditions.UpdateStatusWithRetry(ctx, c, obj, mutate)` introduced in Phase 2.

### D3 — kstatus types (Ready/Reconciling/Stalled) are universal

**Decision:** All 10 LoopZero CRDs expose `Ready`, `Reconciling`, and `Stalled` condition types in `status.conditions`. The semantics follow kpt CRD Status Convention. A helper `conditions.SetReadinessSummary(...)` computes the three summary conditions from per-step detail conditions and is the LAST status mutation in every Reconcile.

**Rationale:** kstatus is the de-facto standard polled by Argo Rollouts, Flux, kubectl wait, and Crossplane composition functions. Universal adoption unlocks `kubectl wait --for=condition=Ready loopzeroapplications/foo` ergonomics for users and integrators. LoopZero already has half of this (per-step conditions like `TypeServiceResolved`, `TypeApplicationHealthy`); we just need the summary layer.

**Consequences:**
- Enables: K8s-native status consumption by external tooling without custom integrations.
- Constrains: every controller must call `SetReadinessSummary` at the end of Reconcile. Mitigated by helper API and lint rule.

### D4 — RBAC is generated from `kubebuilder:rbac` markers; hand-edits are forbidden

**Decision:** `config/rbac/role.yaml` and `charts/loopzero-*-operator/templates/role.yaml` are generated by `controller-gen rbac` from markers placed on each Reconciler struct. CI gate fails any PR that hand-edits the generated files OR adds `r.Client.{Get,List,Create,Update,Patch,Delete}` calls without corresponding markers.

**Rationale:** Crossplane, CloudNativePG, Korifi, and Kueue all generate RBAC. Hand-editing is a known anti-pattern: privilege drift is silent, and `git blame` only catches malice, not omission. Generated RBAC is a compile-time guarantee that the operator's permissions match its observable behavior.

**Consequences:**
- Enables: least-privilege RBAC by construction; auditable supply chain.
- Constrains: developers must add markers BEFORE adding kubectl calls. Mitigated by lint rule + CI gate that points to the missing marker.

### D5 — Operator Capability Levels are declared per-controller, not globally

**Decision:** Each of the 8 controllers gets an explicit Capability Level (I-V) declaration in `docs/technical/operators/capability-matrix.md`, with a 3-column evidence matrix (capability → file:line → verifying test). The module has no single "LoopZero is Level X" claim.

**Rationale:** CloudNativePG (the FAANG-bar reference for Level V declaration) maps capabilities per-feature. LoopZero controllers are heterogeneous: PreviewEnvironment is auto-cleanup (Level V evidence), PlatformBootstrap is one-shot (Level III). A global claim hides this nuance.

**Consequences:**
- Enables: honest external communication ("Tenant: L4, Application: L4, Preview: L5") + internal targeting ("PlatformBootstrap needs L4 metrics to advance").
- Constrains: matrix must be kept current (DoD of every controller PR includes "level evidence updated if capability changed").

### D6 — Plan execution requires LoopZero governance gates (per pre-release-governance.md)

**Decision:** Phase 1 (split-tree consolidation) is T3 — requires Change Governor + Meeting Orchestrator + ADR-NNNN before code is written. Phases 2–4 (Quick Wins + kstatus + RBAC pipeline) are T2 — require ADR but can use lighter Meeting flow. Phases 5–6 (anti-pattern remediation + post-launch) are T1/T2 mix per task.

**Rationale:** CLAUDE.md and `.claude/rules/pre-release-governance.md` mandate that controller reconcile changes, state machine changes, and shared library changes are all T2+. Skipping the meeting flow would violate the governance contract.

**Consequences:**
- Enables: auditable decisions, alignment with platform-architect/sre/security veto authority.
- Constrains: Phase 1 cannot start until ADR signed off.

### D7 — `internal/shared/` is the canonical home for cross-controller helpers; no domain-specific helpers in `internal/controller/` survive the migration

**Decision:** All tracing helpers (`startReconcileSpan`, `recordError`, `setSuccess`, `reconcileOutcome`) and runtime client factories live ONLY in `operators/internal/shared/`. Phase 1 deletes the local copies in `internal/controller/tracing.go` as part of the migration.

**Rationale:** Per AP-1 in the roadmap, having two helper sets (one local, one shared) is the root cause of the dual-tree drift. Consolidating into `shared/` is a pre-condition for Phase 1 success, not an outcome.

**Consequences:** Enables single edit-site for cross-cutting concerns. Constrains: each Reconcile call site must import `shared` package (already done in split tree).

## Dependency Graph

```
Phase 0: Governance Gate ──────────────────────────────────────┐
   (ADRs + meeting + plan ratification)                        │
                                                                │
                                                                ▼
Phase 1: Single Source Of Truth (S9-T1, OP-G21) ─────────────────┐
   1.1 Move tests to split tree                                  │
   1.2 Move operatortest fakes                                   │
   1.3 Multi-binary registration_test                            │
   1.4 Delete internal/controller/                               │
   1.5 Helm chart swap                                           │
   1.6 E2E re-validation                                         │
                                                                  │
                                                                  ▼
Phase 2: Convention Adoption (Quick Wins) ───────────┬─────┬────┬──┐
   2.1 Predicates everywhere (QW-1, OP-G17)         │     │    │  │
   2.2 kstatus types (QW-2, OP-G18 base)            │     │    │  │
   2.3 retry.RetryOnConflict helper (QW-5, D2)      │     │    │  │
   2.4 Anti-patterns AP-2, AP-3 (typed conditions)  │     │    │  │
                                                     │     │    │  │
                                ┌────────────────────┘     │    │  │
                                │ (parallel after 2.1+2.2 done) │  │
                                ▼                                ▼  ▼
Phase 3: kstatus Universal (S9-T2, OP-G18 full)            Phase 4: RBAC Pipeline (S9-T5, QW-3, OP-G19)
   3.1 SetReadinessSummary helper                              4.1 kubebuilder:rbac markers per controller
   3.2 Apply summary in all 8 controllers                      4.2 controller-gen integration
   3.3 envtest: kubectl wait works                             4.3 CI gate against drift
                                                               4.4 Charts roleY.yaml regen
                                ┌──────────────────────────────────┘
                                │ (both done)
                                ▼
Phase 5: Capability Level Declaration (S9-T3, OP-G20) + Final Anti-Patterns
   5.1 capability-matrix.md
   5.2 Level IV evidence per controller
   5.3 AP-4 (clustercredential watch consolidation)
   5.4 AP-5 (cache scoping congruence test)
                                │
                                ▼
Phase 6: Post-Launch Foundations (PL-1..PL-5, OP-G16 enhancements)
   6.1 multicluster-runtime adoption
   6.2 Korifi-style BuildWorkload CRD design
   6.3 Crossplane Operations for PlatformBootstrap
   6.4 Conversion webhook scaffold
   6.5 CNPG-grade observability gaps

                                │
                                ▼
                       /dogfood full GATE
```

**Sequential blockers:** Phase 0 → Phase 1 → Phase 2. Phase 3 and Phase 4 parallelize after Phase 2. Phase 5 needs both. Phase 6 is post-launch (after Sprint 9 cut).

**Parallelization opportunities:** Within Phase 1, tasks 1.1, 1.2, 1.3 are sequential (each depends on the prior); 1.4 needs 1.1+1.2+1.3; 1.5+1.6 sequential. Within Phase 2, tasks 2.1, 2.2, 2.3, 2.4 can parallelize (independent files). Phase 3 and Phase 4 fully parallel.

---

## Phase 0: Governance Gate

**Objective:** Open the meeting + ADR flow so Phase 1 (T3 change) can legitimately start.

### T0.1 — Submit Change Request via Change Governor

#### Objective
Classify the entire plan as T3 (architecture change), open the formal meeting flow, get sign-off from platform-architect, sre-engineer, security-engineer, change-validation-planner.

#### Evidence
- `.claude/rules/pre-release-governance.md` §"Implementation Agent Gate": "If T2/T3: STOP. Do not write code. Instead: Produce a diagnosis, invoke the Change Governor for formal review, wait for meeting approval and validation plan, only implement after explicit APPROVED status."
- This plan touches: 8 controllers, 4 binaries, RBAC, Helm charts, CRD status semantics, tests. Every automatic T2/T3 trigger applies.

#### Files to edit
```
docs/meetings/MEET-NNNN-operators-sota-faang.md (NEW) — meeting record
docs/meetings/adr/NNNN-split-tree-canonical.md (NEW) — D1 ADR
docs/meetings/adr/NNNN-status-update-retry.md (NEW) — D2 ADR
docs/meetings/adr/NNNN-kstatus-universal.md (NEW) — D3 ADR
docs/meetings/adr/NNNN-rbac-from-markers.md (NEW) — D4 ADR
docs/meetings/adr/NNNN-capability-level-per-controller.md (NEW) — D5 ADR
```

#### Deep file dependency analysis
- `docs/meetings/MEET-NNNN-*.md`: new meeting record, no downstream code dependency yet.
- `docs/meetings/adr/NNNN-*.md`: 5 new ADRs. They are referenced by every subsequent task's "Evidence" section. Their numbering is determined by the next available NNNN at the time of the meeting.

#### Deep Dives
- ADRs D1–D5 are listed in the Plan ADRs section above. Each must be fleshed out with: alternatives evaluated, vote outcomes per reviewer, explicit "Approved by: ..." line, and a "Reversibility: <how to undo>" line.
- The Meeting Orchestrator pattern requires minimum 3 reviewers per meeting; for T3, all of: platform-architect, sre-engineer, platform-security-engineer, k8s-controller-engineer, verification-architect, plus dx-engineer for split-binary operability.

#### Tasks
1. Invoke `/change` (Change Governor) with this plan as the change request.
2. Change Governor classifies as T3, opens meeting via meeting-orchestrator.
3. Meeting Orchestrator convenes 6 specialist reviewers above + Decision Record Moderator.
4. Each ADR (D1–D5) drafted, debated, finalized.
5. Validation plan drafted by change-validation-planner (test strategy summary referencing this plan's TDD sections).
6. APPROVED status published; commit ratifies meeting + ADRs in one PR.
7. Plan v1.0 → v1.1 commit references the new ADR IDs.

#### TDD
```
N/A — governance phase, no production code.
The "test" is the meeting record + ADR ratification.
VERIFY: docs/meetings/adr/NNNN-*.md committed with status="Approved"
```

#### Acceptance Criteria
- [ ] 5 ADRs (D1–D5) committed with status="Approved" and reviewer signatures
- [ ] MEET-NNNN record committed
- [ ] Plan version bumped to v1.1 with reference to ADR IDs
- [ ] Pass: zero specialist agents block (no veto from security or sre)

#### DoD
- [ ] All ADRs reviewed and approved
- [ ] Validation plan from change-validation-planner committed
- [ ] Plan v1.1 published

---

## Phase 1: Single Source Of Truth (S9-T1, OP-G21)

**Objective:** Eliminate the ~5800 LOC duplication between `internal/controller/*` (monolith) and `internal/{tenant,app,preview,platform}/*` (split). Make the split tree canonical. Delete the monolith.

**Evidence:** roadmap §S9-T1, gates OP-G21, anti-pattern AP-1.

### T1.1 — Move tests from monolith to split tree

#### Objective
Migrate all 21 `_test.go` files from `operators/internal/controller/` to the corresponding split package, keeping coverage intact.

#### Evidence
- `find operators/internal/controller -name "*_test.go" | wc -l` = 21
- `find operators/internal/{tenant,app,preview,platform} -name "*_test.go" | wc -l` = 0
- The split tree has identical Reconciler logic (verified in Phase 0 SOTA research — diff between controllers is package-name-only + tracing import).

#### Files to edit
```
operators/internal/controller/tenant_controller_test.go → operators/internal/tenant/tenant_controller_test.go (MOVE+RENAME package)
operators/internal/controller/tenant_isolation_test.go → operators/internal/tenant/tenant_isolation_test.go
operators/internal/controller/tenant_integration_test.go → operators/internal/tenant/tenant_integration_test.go
operators/internal/controller/tenant_deletion_backoff_test.go → operators/internal/tenant/tenant_deletion_backoff_test.go
operators/internal/controller/tenant_harbor_test.go → operators/internal/tenant/tenant_harbor_test.go
operators/internal/controller/environment_controller_test.go → operators/internal/tenant/environment_controller_test.go
operators/internal/controller/network_policies_test.go → operators/internal/tenant/network_policies_test.go
operators/internal/controller/resource_quotas_test.go → operators/internal/tenant/resource_quotas_test.go
operators/internal/controller/registry_auth_merge_test.go → operators/internal/tenant/registry_auth_merge_test.go
operators/internal/controller/exposure_controller_test.go → operators/internal/app/exposure_controller_test.go
operators/internal/controller/preview_controller_envtest_test.go → operators/internal/preview/preview_controller_envtest_test.go
operators/internal/controller/preview_controller_unit_test.go → operators/internal/preview/preview_controller_unit_test.go
operators/internal/controller/platformbootstrap_controller_test.go → operators/internal/platform/platformbootstrap_controller_test.go
operators/internal/controller/gitrepositorybinding_controller_test.go → operators/internal/platform/gitrepositorybinding_controller_test.go
operators/internal/controller/clustercredential_controller_test.go → operators/internal/platform/clustercredential_controller_test.go (if exists)
operators/internal/controller/ingress_namespace_test.go → operators/internal/tenant/ingress_namespace_test.go
operators/internal/controller/suite_test.go → split per-package suite_test.go (4 copies, one per split package)
operators/internal/controller/fake_provisioners_test.go → operators/internal/tenant/fake_provisioners_test.go
```

#### Deep file dependency analysis
- Each `_test.go` file declares `package controller`. Move + rename to target package (e.g., `package tenant`, `package app`).
- Each test imports `loopzerov1`, `conditions`, `constants` — these don't change package. Move-only.
- `suite_test.go` is shared envtest setup. Currently one copy under `controller/`. After split, each package needs its own `suite_test.go` (or shared via `operatortest/setup.go` re-exports). Decision: keep one per split package, with shared bootstrap in `operatortest/`.
- Tests reference internal helper symbols (e.g., `startReconcileSpan` referenced by tests is now `shared.StartReconcileSpan`) — already aligned in split tree per D7.

#### Deep Dives
- **Move semantics:** use `git mv` to preserve blame. `git mv operators/internal/controller/tenant_controller_test.go operators/internal/tenant/tenant_controller_test.go`.
- **Package rename:** the only line that changes is `package controller` → `package tenant` (or `app`/`preview`/`platform`). All other lines untouched.
- **Imports:** if the test imported `controller.SetupTenantController`, change to `tenant.SetupTenantController` (no path qualifier needed in same package — bare `SetupTenantController`).
- **Edge case — fake_provisioners_test.go:** lives under `controller/` only (production package, used by tests in same package). Move to `tenant/` because the fakes' primary consumer is tenant tests.

#### Tasks
1. For each file in the table, run `git mv` with target path.
2. Edit the package declaration (1 line per file).
3. Resolve any `controller.X` references to bare `X` (within the same package) or to the correct cross-package import.
4. Verify each split package compiles: `go build ./operators/internal/tenant/...` etc.
5. Run tests in the new location: `go test ./operators/internal/tenant/... -v`.
6. If any test references a symbol not yet in the split tree, surface it (this is the "drift detection" — likely zero or near-zero given the diff was mechanical).

#### TDD
```
RED:    No new tests; existing tests must pass IN THE NEW LOCATION.
GREEN:  Tests should pass without code changes (because the diff between trees was mechanical).
        If a test fails, it surfaced an actual semantic divergence (ClusterCredential is the suspect).
        Fix by porting the relevant logic from monolith to split version (D1).
REFACTOR: None expected at this layer.
VERIFY: go test ./operators/internal/tenant/... ./operators/internal/app/... \
                ./operators/internal/preview/... ./operators/internal/platform/... -v
```

#### Acceptance Criteria
- [ ] All 21 `_test.go` files moved to split tree
- [ ] `go test ./operators/internal/tenant/...` PASS
- [ ] `go test ./operators/internal/app/...` PASS
- [ ] `go test ./operators/internal/preview/...` PASS
- [ ] `go test ./operators/internal/platform/...` PASS
- [ ] `git log --follow operators/internal/tenant/tenant_controller_test.go` shows full history (blame preserved)
- [ ] Pass: /code-audit complexity check (cyclomatic complexity ≤ 10) per moved file
- [ ] Pass: /code-audit coverage check (coverage ≥ 90%) per package
- [ ] Pass: /code-audit lint check (zero golangci-lint warnings)

#### DoD
- [ ] Tasks 1-6 completed
- [ ] All tests passing in new locations
- [ ] `task lint` exit 0
- [ ] No drift surface — if a test failed, the divergence is documented in a follow-up issue

### T1.2 — Move `operatortest/setup.go` fakes to use split-tree imports

#### Objective
`operators/operatortest/setup.go` currently imports `internal/controller`. Re-point to split-tree imports so external test modules use the canonical implementations.

#### Evidence
- `operators/operatortest/setup.go:24`: `"github.com/loopzero/loopzero/operators/internal/controller"`
- After T1.1, `internal/controller/*` will be deleted in T1.4. Without T1.2, T1.4 breaks compilation of `operatortest`.

#### Files to edit
```
operators/operatortest/setup.go — import controller → tenant/app/preview/platform per func
```

#### Deep file dependency analysis
- `operatortest/setup.go` exports public-facing `SetupTenantController`, `SetupClusterCredentialController`, `SetupEnvironmentController`, `SetupApplicationController`, `SetupExposureController`, `SetupGitRepositoryBindingController`, `SetupPlatformBootstrapController`. Each delegates to `controller.X`.
- After T1.2, each delegate goes to its split package: `tenant.SetupTenantController`, `platform.SetupClusterCredentialController`, etc.
- Downstream: `infra/tests/e2e/` imports `operatortest`. No change required there because the public facade is preserved.

#### Deep Dives
- This task is purely import-rewriting. No behavior change.
- The Setup function naming is identical across trees (verified in research phase).
- Edge case: if a Setup function exists in monolith but NOT split (e.g., monolith-only helper), Phase 1.1 should have caught it via test failure. Re-verify before this task.

#### Tasks
1. Edit `operators/operatortest/setup.go` import block: split `internal/controller` into 4 imports (tenant, app, preview, platform).
2. For each `func Setup*Controller`, change `controller.X` → `<pkg>.X`.
3. `go build ./operators/operatortest/...` — must compile.
4. Run E2E tests: `task test:e2e` or `cd infra/tests/e2e && go test -v`.

#### TDD
```
RED:    operators/operatortest/setup.go fails to compile after T1.4 deletes internal/controller
        (this RED is forced by the dependency on T1.4 — but T1.2 runs FIRST to avoid that).
GREEN:  Imports rewritten, compilation passes.
REFACTOR: None expected.
VERIFY: go build ./operators/operatortest/...
        go test ./operators/operatortest/... (if it has unit tests)
        cd infra/tests/e2e && go test -v -count=1 -run TestE2E_BasicReconcile
```

#### Acceptance Criteria
- [ ] `operators/operatortest/setup.go` imports only split packages (no `internal/controller`)
- [ ] `go build ./operators/operatortest/...` PASS
- [ ] `task test:e2e` PASS
- [ ] Pass: /code-audit lint check on `operatortest/setup.go`

#### DoD
- [ ] Imports rewritten
- [ ] E2E tests still passing
- [ ] No production behavior change (this is import refactor)

### T1.3 — Extend `cmd/registration_test.go` to multi-binary matrix (QW-4, OP-G13 amplification)

#### Objective
Replace single-binary wiring test (validates only `cmd/main.go` against `internal/controller/`) with a matrix test that validates each of the 4 split binaries against its corresponding split package.

#### Evidence
- `cmd/registration_test.go:30-58`: hardcoded `"../internal/controller"` and `"main.go"`.
- After T1.4, `cmd/main.go` and `internal/controller/` are deleted; this test breaks.
- Per OP-G13 (Wiring Completeness), every controller exported `Setup*` must be wired in its binary's `main`. The split binaries lack this gate.

#### Files to edit
```
operators/cmd/registration_test.go — extend to matrix
```

#### Deep file dependency analysis
- The current test parses Go AST to find exported `Setup*` funcs in `../internal/controller` and call sites in `main.go::setupControllers`.
- After this task, the test iterates over a `[]struct{name, pkgDir, mainPath, setupFunc}` matrix: 4 rows for the 4 split binaries.
- For split binaries, the wiring happens in the binary's `main()` function (no separate `setupControllers` per-binary).

#### Deep Dives
- **Matrix structure:**
  ```go
  var binaryWiringMatrix = []struct {
      name        string
      pkgDir      string
      mainPath    string
      setupFunc   string
  }{
      {"tenant",   "../internal/tenant",   "tenant-operator/main.go",   "main"},
      {"app",      "../internal/app",      "app-operator/main.go",      "main"},
      {"preview",  "../internal/preview",  "preview-operator/main.go",  "main"},
      {"platform", "../internal/platform", "platform-operator/main.go", "main"},
  }
  ```
- **Setup pattern in split binaries:** wiring happens in `main()` directly, not in a `setupControllers` helper. The AST walker must accept either pattern.
- **Cache scoping congruence (related to AP-5):** also gate that each CRD type appears in cache scoping of exactly ONE binary's manager (no overlap, no orphan). Defer this check to T5.4.

#### Tasks
1. Refactor `wiredSetupControllerCalls` to accept `setupFuncName string` parameter (was hardcoded `"setupControllers"`).
2. Refactor `exportedSetupControllerFuncs` to accept arbitrary `pkgDir`.
3. Add `binaryWiringMatrix` slice + `t.Run(bin.name, ...)` loop in both `TestSetupControllersWiresAllControllers` and `TestSetupControllersHasNoOrphanCalls`.
4. For each entry, parse the binary's main file + its package dir, assert wiring completeness.
5. Update `SetupAllowlist` map: keys may need binary qualification (e.g., `"tenant:SetupCacheReader"`) to allow legitimate exceptions per binary.
6. Run: `go test ./operators/cmd/ -v`.

#### TDD
```
RED:    TestSetupControllersWiresAllControllers/tenant — fails if any tenant Setup* not wired
        TestSetupControllersWiresAllControllers/app — fails if any app Setup* not wired
        TestSetupControllersWiresAllControllers/preview — fails if any preview Setup* not wired
        TestSetupControllersWiresAllControllers/platform — fails if any platform Setup* not wired
        (4 sub-tests; all should be GREEN if the split tree's wiring is already correct;
         the test files should be re-runnable to surface regressions)
        TestSetupControllersHasNoOrphanCalls/<binary> — same pattern, opposite direction.
GREEN:  Implementation: matrix-driven loop in registration_test.go.
REFACTOR: Extract `parseSetupFuncs(t, pkgDir, mainPath, setupFunc)` helper; both tests share it.
VERIFY: go test ./operators/cmd/ -run TestSetupControllers -v
        Output should show 4 sub-tests per top-level test (8 total) all PASS.
```

#### Acceptance Criteria
- [ ] `TestSetupControllersWiresAllControllers` runs as 4 sub-tests (one per binary)
- [ ] `TestSetupControllersHasNoOrphanCalls` runs as 4 sub-tests
- [ ] All 8 sub-tests PASS on green branch
- [ ] Negative test: deliberately add `func SetupBogusController()` to `internal/tenant/`, verify the test fails for tenant binary
- [ ] Pass: /code-audit complexity check (cyclomatic ≤ 10) — refactored helpers should remain simple
- [ ] Pass: /code-audit lint check

#### DoD
- [ ] Matrix-driven tests in place
- [ ] All sub-tests passing
- [ ] Negative test path verified locally
- [ ] Allowlist semantics documented in comments

### T1.4 — Delete `internal/controller/` and `cmd/main.go`

#### Objective
Remove the deprecated monolith tree. -8000 LOC.

#### Evidence
- After T1.1+T1.2+T1.3, all references to `internal/controller/` and `cmd/main.go` are migrated.
- D1 mandates this deletion as the source-of-truth migration's terminal step.

#### Files to edit
```
operators/internal/controller/ (DELETE entire directory) — 22 .go files + 21 _test.go files
operators/cmd/main.go (DELETE) — 574 lines
operators/cmd/pprof_test.go (KEEP if it tests cmd/-shared logic, MOVE if it tests main.go specifically)
operators/Dockerfile (DELETE) — was for the monolith; the 4 Dockerfile.{tenant,app,preview,platform}-operator already exist
operators/internal/controller/tracing.go (DELETE) — duplicate of internal/shared/tracing.go (per D7)
```

#### Deep file dependency analysis
- `internal/controller/` directory deletion: removes ~5800 LOC.
- `cmd/main.go` deletion: removes 574 lines, the monolith's wiring entrypoint.
- `Dockerfile` (top-level, monolith): `infra/helm/tenant-controller/values.yaml` references this image. After deletion, that Helm chart must reference the split charts (handled in T1.5).
- `cmd/pprof_test.go`: examine — if it tests the pprof setup logic (which exists in shared), move; if it tests monolith-specific `main.go::pprofEnabled()`, delete.
- Downstream verification: `git grep -rn "internal/controller"` MUST return zero results in `operators/`, `api/`, `cli/`, `pkg/`.

#### Deep Dives
- **Order of operations:**
  1. `git rm -r operators/internal/controller/`
  2. `git rm operators/cmd/main.go`
  3. `git rm operators/Dockerfile` (after T1.5 confirms charts reference the 4 split Dockerfiles)
  4. Decide on `cmd/pprof_test.go`: if it imports `main` package functions, delete; else keep.
  5. `go build ./...` — must compile.
  6. `go test ./...` — must pass.
- **Edge case — `internal/controller/tracing.go` duplication of `internal/shared/tracing.go`:** delete both at once; verify no caller of `controller.startReconcileSpan` exists (split tree uses `shared.StartReconcileSpan`).

#### Tasks
1. Verify zero references: `git grep -l "operators/internal/controller" operators/ pkg/ api/ cli/` = empty.
2. Verify zero references: `git grep -l "controller\." operators/cmd/` = empty (no remaining call sites).
3. `git rm -r operators/internal/controller`.
4. `git rm operators/cmd/main.go`.
5. `git rm operators/Dockerfile`.
6. Decide on `cmd/pprof_test.go` (likely DELETE if tied to monolith; verify).
7. `go build ./operators/...` — must compile.
8. `go test ./operators/... -count=1` — must pass.
9. `task lint` — must pass.

#### TDD
```
RED:    go build ./operators/... — must FAIL right after `git rm` of internal/controller (RED proves
        we have remaining references before deletion is "clean"). If GREEN immediately, T1.1+T1.2 was complete.
GREEN:  go build ./operators/... PASS after the deletion (T1.1+T1.2+T1.3 prerequisites already met).
REFACTOR: None — this IS the refactor.
VERIFY: 
  git diff --stat | grep "internal/controller" → shows ~5800 deletions
  go build ./operators/... → exit 0
  go test ./operators/... → exit 0
  task lint → exit 0
```

#### Acceptance Criteria
- [ ] `operators/internal/controller/` directory does not exist
- [ ] `operators/cmd/main.go` does not exist
- [ ] `operators/Dockerfile` does not exist
- [ ] `git diff --stat HEAD~1 HEAD` shows ~-5800 lines in operators/
- [ ] `go build ./operators/...` exit 0
- [ ] `go test ./operators/... -count=1` exit 0
- [ ] `task lint` exit 0
- [ ] Pass: /code-audit size check (each remaining controller file ≤ 500 lines, may require split per controller)

#### DoD
- [ ] Monolith deleted
- [ ] All tests passing in split tree
- [ ] No regressions in E2E (`task test:e2e`)
- [ ] Helm chart migration prepped for T1.5

### T1.5 — Helm chart migration: `infra/helm/tenant-controller` → 4 split charts

#### Objective
Production today deploys the monolith via `infra/helm/tenant-controller`. After T1.4 deletes the monolith binary, this chart must be replaced (or repointed) to deploy the 4 split charts (`charts/loopzero-{tenant,app,preview,platform}-operator/`).

#### Evidence
- CLAUDE.md: "The operator binary is **monolithic** (`operators/cmd/main.go`): all 8 controllers run in one process. `infra/helm/tenant-controller` deploys this binary."
- Charts already exist in `charts/`: `loopzero-app-operator`, `loopzero-platform-operator`, `loopzero-tenant-operator`, `loopzero-preview-operator` (per `operators/Dockerfile.*-operator` files).
- After D1, the deploy path must use these.

#### Files to edit
```
infra/helm/tenant-controller/values.yaml — DELETE OR repoint to split charts (decision: delete)
infra/helm/tenant-controller/templates/*.yaml — DELETE
infra/helm/tenant-controller/Chart.yaml — DELETE
infra/helmfile/helmfile.yaml — replace tenant-controller release with 4 releases
charts/loopzero-tenant-operator/values.yaml — verify defaults match prior monolith values
charts/loopzero-app-operator/values.yaml — verify
charts/loopzero-preview-operator/values.yaml — verify
charts/loopzero-platform-operator/values.yaml — verify
infra/helm/admission-policies/* — verify cluster image policy still includes 4 new image refs
```

#### Deep file dependency analysis
- `infra/helmfile/helmfile.yaml` orchestrates Layer 2 (per CHG-2026-04-28-LAYERED-BOOTSTRAP). After T1.5, it deploys 4 operator releases instead of 1.
- `charts/loopzero-*-operator` charts already have RBAC, Deployment, Service templates. Verify each chart's `values.yaml` has the env vars from monolith's deployment (GITEA_URL, ARGOCD_NAMESPACE, BUILD_KUBECONFIG, etc., scoped to the binary that needs them).
- ArgoCD's `loopzero-platform-bootstrap` ApplicationSet (Layer 3) references the chart locations — verify those references.

#### Deep Dives
- **Env var distribution across 4 binaries:**
  - tenant-operator needs: GITEA_URL, GITEA_TOKEN, GITEA_ORG, ARGOCD_NAMESPACE, BUILD_KUBECONFIG, RUNTIME_KUBECONFIG, MINIO_*, HARBOR_*, REGISTRY_CIDRS, DIGITALOCEAN_TOKEN, REGISTRY_DOCKER_CONFIG, SPACES_*, WORKLOAD_CLUSTER_URL, LOOPZERO_TENANT_MAX_CONCURRENT
  - app-operator needs: BASE_DOMAIN
  - preview-operator needs: RUNTIME_KUBECONFIG, BASE_DOMAIN, INGRESS_NAMESPACE
  - platform-operator needs: ARGOCD_NAMESPACE (for ClusterCredential controller)
- **Cache scoping congruence (will be tested in T5.4):** each binary's manager scopes the cache to only the Kinds it reconciles. Verify in each chart's Deployment that the relevant flags or ConfigMap reflect this.
- **Pre-flight before delete `infra/helm/tenant-controller`:** ensure ArgoCD has rolled out the new charts; ensure E2E env validates 4 pods running (tenant-operator, app-operator, preview-operator, platform-operator) instead of 1.

#### Tasks
1. Audit `charts/loopzero-tenant-operator/values.yaml` against current `infra/helm/tenant-controller/values.yaml` — diff env vars, resource limits, RBAC.
2. Add missing env vars to each split chart's `values.yaml` per the distribution above.
3. Update `infra/helmfile/helmfile.yaml` to deploy 4 releases.
4. `helmfile diff` — review the diff manually before apply.
5. `task dev:up` (deploys to k3d) — verify 4 operator pods Running.
6. `task test:e2e` — verify reconcile flows work with 4 binaries.
7. `git rm -r infra/helm/tenant-controller/`.
8. `task validate:helm-charts` — must pass.

#### TDD
```
RED:    helmfile diff after edits — should show 4 new releases + 1 release deletion (the monolith).
        BEFORE this task, helmfile diff would show no change.
GREEN:  After edits, `helmfile sync` succeeds; `kubectl get pods -n loopzero-system -l app.kubernetes.io/component=operator` shows 4 pods Running.
REFACTOR: None expected; this is config refactor.
VERIFY:
  helmfile -f infra/helmfile/helmfile.yaml diff
  helmfile -f infra/helmfile/helmfile.yaml sync
  kubectl wait --for=condition=Available -n loopzero-system deployment/loopzero-tenant-operator deployment/loopzero-app-operator deployment/loopzero-preview-operator deployment/loopzero-platform-operator --timeout=300s
  task test:e2e -- --run TestE2E_TenantProvisioning
```

#### Acceptance Criteria
- [ ] `infra/helm/tenant-controller/` directory deleted
- [ ] 4 operator deployments Running and Ready in mgmt cluster
- [ ] `helmfile sync` exit 0
- [ ] All E2E tests pass against the 4-binary deployment
- [ ] kubeconform validates all rendered manifests
- [ ] Pass: /code-audit on chart YAML (no `latest` tags, resource limits set, probes set)

#### DoD
- [ ] Helmfile orchestrates 4 releases
- [ ] Production-equivalent local env (`task dev:up`) runs 4 operators
- [ ] E2E + integration tests pass
- [ ] No regressions in dogfood smoke (`task dogfood:smoke`)

### T1.6 — Re-validate full E2E + dogfood smoke against split deploy

#### Objective
Prove the migration introduced no behavior regression by running the full E2E suite + dogfood smoke against the now-split-only deploy.

#### Evidence
- CLAUDE.md: "E2E Validation (2026-04-23): 25/30 pass (83%)." — this is the baseline to preserve.
- D1 explicitly requires E2E re-validation as part of the migration.

#### Files to edit
```
(no production code edits — pure validation)
```

#### Deep file dependency analysis
None.

#### Deep Dives
- **What "pass" means:** all 25 previously-green E2E tests stay green. The 5 currently-red ones (per CLAUDE.md "Loki cross-cluster streaming") are out of scope for this task — but should not regress further.
- **Dogfood smoke vs full:** smoke is fast (~5 min, validates basic deploy). Full is the real gate (Phase 6 prerequisite).
- **Multi-cell impact:** the split binaries must continue to honor cell-aware reconcile (per CLAUDE.md cell-2 activation work). Verify `loopzero deploy` in cell-2 still works.

#### Tasks
1. Fresh k3d: `task teardown && task setup && tilt up`
2. `task doctor` — health check.
3. `task test:e2e -count=1 -v` — full E2E suite.
4. `task dogfood:smoke` — smoke audit.
5. Compare E2E results to baseline (25/30 pass): identify any new regressions.
6. If regressions found, root-cause, fix, re-run. If no regressions, mark T1.6 done.

#### TDD
```
RED:    task test:e2e — establish baseline pre-T1 (25/30 pass).
        After T1.4+T1.5, re-run. If any of the 25 previously-green tests now fail, that's the RED.
GREEN:  Fix the regression. Iterate until all 25 are green again.
REFACTOR: None expected.
VERIFY:
  task test:e2e | tee /tmp/e2e-after-split.log
  diff /tmp/e2e-baseline.log /tmp/e2e-after-split.log → no new failures
  task dogfood:smoke
```

#### Acceptance Criteria
- [ ] All 25 baseline E2E tests pass
- [ ] No new regressions vs baseline
- [ ] Dogfood smoke reports zero S0/S1 findings
- [ ] Multi-cell deploy (cell-2) verified via `loopzero deploy --cell cell-2` end-to-end

#### DoD
- [ ] E2E suite green
- [ ] Dogfood smoke clean
- [ ] Multi-cell paths verified
- [ ] Phase 1 ready for sign-off

---

## Phase 2: Convention Adoption (Quick Wins)

**Objective:** Adopt kubebuilder/kstatus patterns universally across the (now consolidated) split tree. Establishes the foundation for Phase 3 (kstatus universal) and Phase 4 (RBAC pipeline).

**Evidence:** roadmap §QW-1, §QW-2, §QW-5; gates OP-G17 base, OP-G18 base; anti-patterns AP-2, AP-3.

### T2.1 — Add `GenerationChangedPredicate` to all 8 controllers (QW-1, OP-G17 base)

#### Objective
Stop reconcile storms triggered by controller's own status updates by filtering events at the watch level.

#### Evidence
- Phase 0 SOTA research: zero predicates anywhere in `operators/internal/`.
- kubebuilder docs: `ResourceVersionChangedPredicate` (default) fires on EVERY change including status — typically 50-500x amplification.
- Production cost: each unnecessary reconcile = 1 K8s API List call (cache refresh in some cases) + 1 Reconcile execution + N child resource ensures. Multi-cell × 100 tenants × 50x amplification = real CPU.

#### Files to edit
```
operators/internal/tenant/tenant_controller.go — SetupTenantController
operators/internal/tenant/environment_controller.go — SetupEnvironmentController + WithFactory variant
operators/internal/app/application_controller.go — SetupApplicationController
operators/internal/app/exposure_controller.go — SetupExposureController
operators/internal/preview/preview_controller.go — SetupPreviewController
operators/internal/platform/platformbootstrap_controller.go — SetupPlatformBootstrapController
operators/internal/platform/gitrepositorybinding_controller.go — SetupGitRepositoryBindingController
operators/internal/platform/clustercredential_controller.go — SetupClusterCredentialController
```

#### Deep file dependency analysis
- Each `SetupXController` function calls `ctrl.NewControllerManagedBy(mgr).For(&loopzerov1.X{}).Owns(...).Complete(...)`.
- Insertion point: between `For(...)` and `Owns/Watches/Complete`. Pattern:
  ```go
  ctrl.NewControllerManagedBy(mgr).
      For(&loopzerov1.X{}, builder.WithPredicates(
          predicate.Or(
              predicate.GenerationChangedPredicate{},
              predicate.AnnotationChangedPredicate{},
              predicate.LabelChangedPredicate{},
          ),
      )).
      Owns(&loopzerov1.Y{}).
      ...
  ```
- ClusterCredential is special: it has a `Watches(&corev1.Secret{}, ...)` that should NOT be filtered by Generation (Secrets don't have generation). Watches keeps default predicate.
- Tests in T1.1 already moved — they test reconcile counters. After this change, status-update tests should show counter == 1 even after 100 status writes.

#### Deep Dives
- **Why `Or(Generation, Annotation, Label)`:** Some Reconciles legitimately need to react to label/annotation changes (e.g., `loopzero.io/cell-id` annotation updates from `loopzero admin tenants pin`). Generation alone is too restrictive.
- **Edge case — initial create:** `GenerationChangedPredicate.Create` returns `true` (always reconcile new objects). Verified in controller-runtime source.
- **Edge case — finalizer add:** when controller adds finalizer (which is a metadata update via `r.Update(ctx, &cr)`), generation does NOT bump (finalizer is in metadata, not spec). But `predicate.AnnotationChangedPredicate` won't fire either. Solution: the controller's `r.Update` after adding finalizer triggers a watch event regardless (because resource version changes). Predicate filters event types, not API operations. Test envtest must verify this end-to-end.
- **Predicate composition gotcha:** `predicate.Or(...)` in controller-runtime returns true if ANY child returns true. For `Update` events, Generation changes alone gate (no label/annot needed). For Create/Delete events, all predicates default to true — generic events pass.

#### Tasks
1. Add `import "sigs.k8s.io/controller-runtime/pkg/builder"` and `"sigs.k8s.io/controller-runtime/pkg/predicate"` to each Setup file.
2. Wrap each `For(&loopzerov1.X{})` call with `builder.WithPredicates(predicate.Or(...))`.
3. Run unit tests: `go test ./operators/internal/tenant/... -run TestReconciler -v`.
4. Run envtest: `go test ./operators/internal/tenant/... -run TestEnvtest -v`.
5. Add new envtest spec: "given a Tenant, when status is updated 100x without spec change, reconcile counter increments at most 2x" (1 for the spec, 1 for finalizer add).

#### TDD
```
RED:    TestPredicate_StatusUpdateNotReconciled (NEW)
        — given Tenant CR, update Status 100x without changing Spec
        — assert reconcile counter ≤ 2 (initial + finalizer)
        Currently FAILS because no predicate; reconcile counter would be 100+.

RED:    TestPredicate_AnnotationChangeReconciled (NEW)
        — given Tenant CR, update Spec.Annotations once
        — assert reconcile counter increments by 1
        Currently passes (Annotation changes do trigger).

RED:    TestPredicate_LabelChangeReconciled (NEW)
        — given Tenant CR, update Spec.Labels once
        — assert reconcile counter increments by 1

RED:    TestPredicate_SpecChangeReconciled (NEW)
        — given Tenant CR, update Spec.Plan once
        — assert reconcile counter increments by 1

GREEN:  Add `builder.WithPredicates(predicate.Or(...))` to each SetupXController.
REFACTOR: Extract `defaultControllerPredicates() builder.OptionFunc` helper if pattern repeats verbatim.
VERIFY: 
  go test ./operators/internal/tenant/... -run TestPredicate -v
  go test ./operators/internal/app/... -run TestPredicate -v
  go test ./operators/internal/preview/... -run TestPredicate -v
  go test ./operators/internal/platform/... -run TestPredicate -v
```

#### Acceptance Criteria
- [ ] All 8 SetupXController functions use `builder.WithPredicates(...)`
- [ ] 4 new envtest specs (one per binary minimum) prove status-update reconcile filtering works
- [ ] No regressions in existing envtest specs
- [ ] Pass: /code-audit complexity check
- [ ] Pass: /code-audit lint check (no unused imports)

#### DoD
- [ ] Predicates added everywhere
- [ ] New envtest specs pass
- [ ] Existing tests still green
- [ ] Counter-validation test demonstrates ≥10x reduction in reconcile count under status-update load

### T2.2 — Add kstatus types `Reconciling`, `Stalled`, `Ready` to `internal/conditions/types.go` (QW-2, OP-G18 base)

#### Objective
Define the universal kstatus condition types as Go constants. Phase 3 will use them; this task makes them available.

#### Evidence
- Phase 0 SOTA research: zero `TypeStalled`, zero universal `TypeReady`. `ReasonReconciling` exists but no `TypeReconciling`.
- kpt CRD Status Convention: Reconciling/Stalled are the canonical types.
- D3 mandates universal adoption.

#### Files to edit
```
operators/internal/conditions/types.go — add TypeReady, TypeReconciling, TypeStalled, ReasonDegraded, ReasonStalled
operators/internal/conditions/types_test.go (NEW or existing) — assert constants exist with correct string values
```

#### Deep file dependency analysis
- Adding constants is non-breaking. No existing call sites change.
- T3.1 (next phase) creates `SetReadinessSummary` helper that consumes these.
- Test file `conditions_test.go` exists in monolith form; ensure it lives in `internal/conditions/` after T1 migration.

#### Deep Dives
- **Constant values:**
  ```go
  const (
      TypeReady       = "Ready"        // True = fully reconciled
      TypeReconciling = "Reconciling"  // True = controller actively working
      TypeStalled     = "Stalled"      // True = stuck, manual intervention needed
  )
  const (
      ReasonReady       = "Ready"
      ReasonReconciling = "Reconciling"
      ReasonStalled     = "Stalled"
      ReasonDegraded    = "Degraded"   // (resolves AP-2 hardcoded "Degraded" string)
      ReasonProgressing = "Progressing"
  )
  ```
- **Polarity convention:** Ready, Reconciling — positive (True = good for Ready, True = working for Reconciling). Stalled — negative (True = bad).
- **kstatus library expectation:** `kstatus.Compute(obj)` returns `Current`, `InProgress`, `Failed`, `NotFound`. It looks for Reconciling=True (→ InProgress), Stalled=True (→ Failed), or Ready=True (→ Current). LoopZero's adoption must match these semantics exactly.

#### Tasks
1. Add 3 type constants + 5 reason constants to `internal/conditions/types.go`.
2. Add unit test `TestKstatusTypes_AreCorrectStrings` asserting string values (catches typos).
3. Verify no existing constant collides (no other `Ready` somewhere already in `types.go`).

#### TDD
```
RED:    TestKstatusTypes_AreCorrectStrings (NEW)
        — assert TypeReady == "Ready"
        — assert TypeReconciling == "Reconciling"
        — assert TypeStalled == "Stalled"
        — assert ReasonDegraded == "Degraded"
        Currently FAILS (constants don't exist).
GREEN:  Add the 3+5 constants.
REFACTOR: None expected.
VERIFY: go test ./operators/internal/conditions/... -run TestKstatus -v
```

#### Acceptance Criteria
- [ ] 3 type constants added
- [ ] 5 reason constants added
- [ ] Unit test passes
- [ ] `go vet ./operators/internal/conditions/...` clean
- [ ] Pass: /code-audit lint

#### DoD
- [ ] Constants in place
- [ ] Tests passing
- [ ] No collision with existing constants

### T2.3 — `conditions.UpdateStatusWithRetry` helper (QW-5, D2)

#### Objective
Provide a single helper to wrap `r.Status().Update()` with `retry.RetryOnConflict`, eliminating the boilerplate per call site.

#### Evidence
- D2: every Status update must use retry.
- Phase 0 SOTA research: only `argocd/real_provisioner.go:237` uses RetryOnConflict; all 28+ controller files do bare `Status().Update()`.

#### Files to edit
```
operators/internal/conditions/retry.go (NEW) — UpdateStatusWithRetry helper
operators/internal/conditions/retry_test.go (NEW) — unit test
```

#### Deep file dependency analysis
- New file in `internal/conditions/`. Imports `client.Client`, `retry`.
- Used by Phase 3 task T3.2 (apply summary) and progressively by every Status update across controllers.

#### Deep Dives
- **API design:**
  ```go
  // UpdateStatusWithRetry re-fetches the object, applies the mutation, and
  // calls r.Status().Update wrapped in retry.RetryOnConflict. The mutation
  // function operates on a freshly-fetched copy each retry attempt.
  func UpdateStatusWithRetry[T client.Object](
      ctx context.Context,
      c client.Client,
      key client.ObjectKey,
      newObj func() T,
      mutate func(T) error,
  ) error {
      return retry.RetryOnConflict(retry.DefaultRetry, func() error {
          obj := newObj()
          if err := c.Get(ctx, key, obj); err != nil {
              return err
          }
          if err := mutate(obj); err != nil {
              return err
          }
          return c.Status().Update(ctx, obj)
      })
  }
  ```
- **Generic vs interface{}:** Go 1.25 generics fit naturally here. T must satisfy `client.Object`.
- **Edge case — IsNotFound on Get during retry:** mutation should not run; helper returns the NotFound error (caller decides if it's terminal).

#### Tasks
1. Create `internal/conditions/retry.go` with the generic helper.
2. Create `internal/conditions/retry_test.go` with tests:
   - Test_UpdateStatusWithRetry_Success (no conflict, single iteration)
   - Test_UpdateStatusWithRetry_ConflictThenSuccess (mock conflict on first call, succeed on second)
   - Test_UpdateStatusWithRetry_PersistentConflict (max retries exceeded)
   - Test_UpdateStatusWithRetry_NotFound (Get fails NotFound, mutate not called)
3. Run: `go test ./operators/internal/conditions/...`.

#### TDD
```
RED:    Test_UpdateStatusWithRetry_Success (NEW) — fake client succeeds; assert mutate called once.
RED:    Test_UpdateStatusWithRetry_ConflictThenSuccess (NEW) — fake client returns conflict on iter 1, success on iter 2.
RED:    Test_UpdateStatusWithRetry_PersistentConflict (NEW) — fake always returns conflict; assert error after max retries.
RED:    Test_UpdateStatusWithRetry_NotFound (NEW) — fake returns NotFound on Get; assert mutate not called, error returned.
GREEN:  Implement UpdateStatusWithRetry.
REFACTOR: None expected — single small function.
VERIFY: go test ./operators/internal/conditions/... -run TestUpdateStatusWithRetry -v
```

#### Acceptance Criteria
- [ ] `UpdateStatusWithRetry` exported and documented
- [ ] All 4 unit tests pass
- [ ] Generic constraint `client.Object` enforces type safety
- [ ] Pass: /code-audit complexity (function ≤ 10 cyclomatic)
- [ ] Pass: /code-audit coverage (≥ 90% of helper code)

#### DoD
- [ ] Helper in place, tested
- [ ] Documented in package doc comment
- [ ] Ready for use by T3.2 and progressive adoption

### T2.4 — Resolve anti-patterns AP-2 (hardcoded condition strings) and AP-3 (silent error swallow)

#### Objective
Fix the two file:line-level anti-patterns flagged in roadmap §AP-2, AP-3.

#### Evidence
- AP-2: `application_controller.go:269-282` — hardcoded `"RouteReachable"`, `"False"`, `"Failed"`, `"Degraded"` strings.
- AP-3: `application_controller.go:179-181` — `r.Get(ctx, routeKey, route)` error silently swallowed.

#### Files to edit
```
operators/internal/app/application_controller.go — replace strings with constants; surface Get errors
operators/internal/app/application_controller_test.go (was monolith's; moved in T1.1) — add regression tests
```

#### Deep file dependency analysis
- Reaches `internal/conditions/types.go`: requires `ReasonDegraded` constant (added in T2.2).
- Affects status semantics: changing the condition reason from hardcoded "Degraded" to `conditions.ReasonDegraded` is a same-string-different-source — no observable change.
- AP-3 fix changes status: previously app.Status.Reachable stays stale; after fix, `TypeRouteReachable` gets `metav1.ConditionUnknown` with ReasonNotFound. Tests must assert this.

#### Deep Dives
- **AP-2 fix:**
  ```go
  for _, c := range app.Status.Conditions {
      if c.Type == conditions.TypeRouteReachable && c.Status == metav1.ConditionFalse {
          switch c.Reason {
          case conditions.ReasonFailed:
              return loopzerov1.ApplicationPhaseDown
          case conditions.ReasonDegraded:
              return loopzerov1.ApplicationPhaseDegraded
          default:
              return loopzerov1.ApplicationPhaseProgressing
          }
      }
  }
  ```
- **AP-3 fix:**
  ```go
  if err := r.Get(ctx, routeKey, route); err != nil {
      if !errors.IsNotFound(err) {
          log.FromContext(ctx).Error(err, "failed to read ExposedRoute for status sync",
              "route", routeKey)
      }
      conditions.SetCondition(&app.Status.Conditions, conditions.TypeRouteReachable,
          metav1.ConditionUnknown, conditions.ReasonNotFound,
          "ExposedRoute not yet observable", app.Generation)
      return
  }
  ```

#### Tasks
1. Edit `application_controller.go::computePhase` (~lines 269-282) — use constants.
2. Edit `application_controller.go::reconcileRouteStatus` (~lines 179-181) — surface Get error.
3. Add regression tests:
   - `TestComputePhase_DegradedReason` — asserts `Degraded` reason returns `ApplicationPhaseDegraded`.
   - `TestComputePhase_FailedReason` — asserts `Failed` returns `ApplicationPhaseDown`.
   - `TestReconcileRouteStatus_RouteNotFound` — asserts `TypeRouteReachable` gets `ConditionUnknown`/`ReasonNotFound`.
   - `TestReconcileRouteStatus_GetTransientError` — asserts Get error is logged AND condition set.

#### TDD
```
RED:    TestComputePhase_DegradedReason — currently passes (uses hardcoded "Degraded" string), but the fix must also pass.
        TestReconcileRouteStatus_RouteNotFound (NEW) — currently FAILS (no condition set on swallow).
GREEN:  Apply fixes per Deep Dives.
REFACTOR: None.
VERIFY: go test ./operators/internal/app/... -run TestComputePhase -v
        go test ./operators/internal/app/... -run TestReconcileRouteStatus -v
```

#### Acceptance Criteria
- [ ] Zero hardcoded `"RouteReachable"`, `"Failed"`, `"Degraded"` strings in `application_controller.go`
- [ ] Get error in reconcileRouteStatus surfaces as condition + log
- [ ] 4 new tests pass
- [ ] Existing application tests still pass

#### DoD
- [ ] AP-2 + AP-3 closed
- [ ] Tests cover both fixes
- [ ] Lint clean

---

## Phase 3: kstatus Universal (S9-T2, OP-G18 full)

**Objective:** Apply Ready/Reconciling/Stalled summary in every controller; verify `kubectl wait` works for all 10 CRDs.

### T3.1 — `SetReadinessSummary` helper

#### Objective
Centralize the logic that computes `Ready`, `Reconciling`, `Stalled` summary from per-step detail conditions.

#### Evidence
- D3: helper must be the LAST status mutation in every Reconcile.
- T2.2 added the constants; this task adds the function that uses them.

#### Files to edit
```
operators/internal/conditions/summary.go (NEW) — SetReadinessSummary
operators/internal/conditions/summary_test.go (NEW) — unit tests
```

#### Deep file dependency analysis
- New file. Imports `metav1`, uses `SetCondition` from same package.
- Consumed by all 8 controllers in T3.2.

#### Deep Dives
- **Algorithm:**
  ```go
  // SetReadinessSummary computes Ready/Reconciling/Stalled summary from
  // detailTypes provided. Semantics:
  //   - Ready=True iff all detailTypes have status True (with matching observedGeneration).
  //   - Reconciling=True iff any detailType has status False with reason in {Reconciling, Progressing, Waiting}.
  //   - Stalled=True iff any detailType has status False with reason in {Failed, Stalled}.
  // Mutually exclusive: at most one of (Ready, Reconciling, Stalled) is True at a time.
  func SetReadinessSummary(conds *[]metav1.Condition, generation int64, detailTypes []string) { ... }
  ```
- **Edge cases:**
  - Empty detailTypes → Ready=True (no work to do).
  - All detailTypes Unknown → Reconciling=True with reason="Initializing".
  - Mix of False{Failed} and True → Stalled=True wins (failure takes priority).
  - Spec change (generation bump) → all summary conditions stay False until detailTypes' observedGeneration catches up.

#### Tasks
1. Create `internal/conditions/summary.go` with the function.
2. Test cases (≥10):
   - All True → Ready=True
   - One False{Reconciling} → Reconciling=True
   - One False{Failed} → Stalled=True
   - Empty list → Ready=True
   - Generation mismatch → Ready=False even if all True (stale)
   - Both False{Failed} and False{Reconciling} → Stalled wins
3. Run: `go test ./operators/internal/conditions/...`.

#### TDD
```
RED:    Test_SetReadinessSummary_AllTrue (NEW) — Ready=True
RED:    Test_SetReadinessSummary_OneReconciling (NEW) — Reconciling=True, Stalled=False, Ready=False
RED:    Test_SetReadinessSummary_OneFailed (NEW) — Stalled=True, others False
RED:    Test_SetReadinessSummary_EmptyDetail (NEW) — Ready=True
RED:    Test_SetReadinessSummary_GenerationMismatch (NEW) — Ready=False (stale)
RED:    Test_SetReadinessSummary_FailedWinsOverReconciling (NEW)
RED:    Test_SetReadinessSummary_PreservesLastTransitionTime (NEW) — re-run with same status doesn't bump time
RED:    Test_SetReadinessSummary_BumpsTransitionTimeOnChange (NEW)
RED:    Test_SetReadinessSummary_AppendsIfMissing (NEW) — first call creates conditions
RED:    Test_SetReadinessSummary_UpdatesIfPresent (NEW) — second call updates not appends
GREEN:  Implement.
REFACTOR: Extract `classifyDetail(c metav1.Condition) summaryClass` helper if logic gets nested.
VERIFY: go test ./operators/internal/conditions/... -run TestSetReadinessSummary -v
```

#### Acceptance Criteria
- [ ] All 10 unit tests pass
- [ ] Function ≤ 50 lines (KISS)
- [ ] Function cyclomatic complexity ≤ 8
- [ ] Pass: /code-audit complexity, coverage, lint

#### DoD
- [ ] Helper implemented + tested
- [ ] Edge cases enumerated and covered

### T3.2 — Apply `SetReadinessSummary` in all 8 controllers

#### Objective
Every Reconciler ends with a call to `conditions.SetReadinessSummary(...)` listing the detailTypes for that resource.

#### Files to edit
```
operators/internal/tenant/tenant_controller.go — Reconcile epilogue
operators/internal/tenant/environment_controller.go — same
operators/internal/app/application_controller.go — same
operators/internal/app/exposure_controller.go — same
operators/internal/preview/preview_controller.go — same
operators/internal/platform/platformbootstrap_controller.go — same
operators/internal/platform/gitrepositorybinding_controller.go — same
operators/internal/platform/clustercredential_controller.go — same
```

#### Deep file dependency analysis
- Each controller has different detailTypes:
  - Tenant: `TypeGitRepoReady, TypeQuotaEnforced, TypeBuildNamespaceReady, TypePriorityClassReady, TypeArgoCDAppReady, TypeHarborProjectReady`
  - Environment: `TypeEnvNamespaceReady, TypeEnvRBACReady, TypeEnvNetworkPolicyReady, TypeEnvQuotaReady` (excluding TypePullSecretReady which is informational per `types.go:44`)
  - Application: `TypeServiceResolved, TypeIngressReady, TypeCertificateIssued, TypeRouteReachable, TypeApplicationHealthy`
  - ExposedRoute: `TypeIngressReady, TypeCertificateIssued, TypeRouteReachable`
  - Preview: `TypePreviewNamespaceReady, TypePreviewNetworkPolicyReady, TypePreviewResourceQuotaReady, TypePreviewLimitRangeReady, TypePreviewBuildSubmitted, TypePreviewBuildCompleted, TypePreviewWorkloadHealthy`
  - PlatformBootstrap: `TypeGiteaReady, TypeArgoCDReady, TypeDatabaseSeeded, TypeBootstrapComplete`
  - GitRepositoryBinding: `TypeCredentialsResolved, TypeRepositoryRegistered, TypeRepositoryReachable, TypeConsumerHealthy`
  - ClusterCredential: `TypeSecretNotFound (inverted), TypeConnected`

#### Deep Dives
- **ClusterCredential semantic note:** `TypeSecretNotFound=True` is BAD (secret missing), unlike most positive-polarity conditions. Need polarity awareness. Resolution: ClusterCredential's detailType list is `[TypeConnected]` only, since TypeSecretNotFound is internal diagnostic.
- **Order of mutations:** SetReadinessSummary MUST come AFTER all individual SetCondition calls and BEFORE `Status().Update`/`UpdateStatusWithRetry`.

#### Tasks
1. For each controller's Reconcile function, identify the final `r.Status().Update` call site.
2. Just before it, insert:
   ```go
   conditions.SetReadinessSummary(&obj.Status.Conditions, obj.Generation, []string{
       conditions.TypeX, conditions.TypeY, ...
   })
   ```
3. Wrap the Status().Update in `conditions.UpdateStatusWithRetry` (T2.3 helper).
4. Add envtest spec per controller asserting `kubectl wait --for=condition=Ready` works.

#### TDD
```
RED:    TestEnvtest_TenantReady (NEW) — eventually(condition Ready=True, 30s)
RED:    TestEnvtest_EnvironmentReady (NEW)
RED:    TestEnvtest_ApplicationReady (NEW)
RED:    TestEnvtest_PreviewReady (NEW)
RED:    TestEnvtest_PlatformBootstrapReady (NEW)
        Currently FAIL (no Ready condition emitted by controllers).
GREEN:  Add SetReadinessSummary call in each Reconcile epilogue.
REFACTOR: Extract per-resource `readinessDetailTypes()` helper if list is reused.
VERIFY: go test ./operators/internal/tenant/... -run TestEnvtest_TenantReady -v
        ... (one per controller)
```

#### Acceptance Criteria
- [ ] All 8 controllers call SetReadinessSummary
- [ ] 8 envtest specs verify `kubectl wait --for=condition=Ready` works
- [ ] All existing tests still pass

#### DoD
- [ ] Universal kstatus emission
- [ ] envtest gate green
- [ ] No regression

### T3.3 — E2E: kstatus library reads LoopZero CRDs

#### Objective
Prove via integration test that `sigs.k8s.io/cli-utils/pkg/kstatus.Compute(obj)` returns sensible Status (Current/InProgress/Failed) for each LoopZero CRD.

#### Files to edit
```
infra/tests/integration/kstatus_compliance_test.go (NEW) — exercises kstatus on all 10 CRDs
```

#### Deep Dives
- Test uses real envtest (full CRD apply) + the kstatus library directly.
- Each CRD: create with valid spec, wait for reconcile, call `kstatus.Compute(obj)`, assert returned Status matches expected lifecycle stage.

#### Tasks
1. Write integration test using kstatus library.
2. For each of 10 CRDs: create → wait → assert.

#### TDD
```
RED:    TestKstatusCompliance_AllCRDs (NEW) — currently FAILS for any CRD without Ready emission.
GREEN:  After T3.2, this test passes.
VERIFY: go test ./infra/tests/integration/ -run TestKstatusCompliance -v
```

#### Acceptance Criteria
- [ ] Integration test for all 10 CRDs passes
- [ ] kstatus.Compute returns expected Status values

#### DoD
- [ ] kstatus compliance proven by external library
- [ ] LoopZero CRDs interop with Argo Rollouts/Flux

---

## Phase 4: RBAC Pipeline (S9-T5, QW-3, OP-G19)

**Objective:** Generate RBAC from `kubebuilder:rbac` markers; eliminate hand-edited `role.yaml`.

### T4.1 — Add `kubebuilder:rbac` markers to all Reconcilers

#### Objective
Annotate each Reconciler struct with markers covering its actual K8s API usage.

#### Files to edit
```
operators/internal/tenant/tenant_controller.go — markers above struct
operators/internal/tenant/environment_controller.go — markers
operators/internal/app/application_controller.go — markers
operators/internal/app/exposure_controller.go — markers
operators/internal/preview/preview_controller.go — markers
operators/internal/platform/platformbootstrap_controller.go — markers
operators/internal/platform/gitrepositorybinding_controller.go — markers
operators/internal/platform/clustercredential_controller.go — markers
```

#### Deep Dives
- **Marker format:**
  ```go
  // +kubebuilder:rbac:groups=loopzero.io,resources=tenants,verbs=get;list;watch;update;patch
  // +kubebuilder:rbac:groups=loopzero.io,resources=tenants/status,verbs=get;update;patch
  // +kubebuilder:rbac:groups=loopzero.io,resources=tenants/finalizers,verbs=update
  // +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch;delete
  // +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
  type TenantReconciler struct { ... }
  ```
- **Audit per controller:** grep for every `r.Client.Get`, `r.Client.List`, `r.Client.Create`, `r.Client.Update`, `r.Client.Patch`, `r.Client.Delete` call; map each to the marker that covers it.
- **Cross-cluster clients:** `BuildClient` and `RuntimeClient` need separate ClusterRoles in the target clusters. Markers don't auto-generate cross-cluster RBAC; document this separately.

#### Tasks
1. For each controller, audit all K8s API calls.
2. Add markers covering each.
3. Compare against current `config/rbac/role.yaml` — identify discrepancies (likely some present-but-unused permissions).
4. Document cross-cluster ClusterRoles in `charts/loopzero-tenant-operator/templates/cross-cluster-rbac.yaml` (separate file, hand-maintained but auditable).

#### TDD
```
RED:    No tests at this layer; markers are declarative.
GREEN:  Markers added; `controller-gen rbac` (run by T4.2) emits expected role.yaml.
VERIFY: 
  cd operators && controller-gen rbac:roleName=loopzero-system paths=./internal/tenant/... \
    output:rbac:dir=/tmp/generated-rbac
  diff /tmp/generated-rbac/role.yaml config/rbac/role.yaml | head -50
  # Expect: only intended permissions, no extras.
```

#### Acceptance Criteria
- [ ] All 8 Reconciler structs have markers
- [ ] Cross-cluster RBAC documented separately
- [ ] Generated RBAC ⊂ existing RBAC (no privilege escalation by markers)

#### DoD
- [ ] Markers in place
- [ ] Audit complete

### T4.2 — `controller-gen rbac` integration; `task generate:rbac`

#### Objective
Wire `controller-gen rbac` into the Taskfile; auto-update `config/rbac/role.yaml` and `charts/*/templates/role.yaml` from markers.

#### Files to edit
```
Taskfile.yml — add task generate:rbac
operators/Makefile — add `make rbac` target (existing infra)
charts/loopzero-tenant-operator/templates/role.yaml — generated, header comment "DO NOT EDIT"
charts/loopzero-app-operator/templates/role.yaml — generated
charts/loopzero-preview-operator/templates/role.yaml — generated
charts/loopzero-platform-operator/templates/role.yaml — generated
config/rbac/role.yaml — generated (root level)
```

#### Tasks
1. Add `task generate:rbac` calling `controller-gen rbac:roleName=loopzero-system paths=./operators/...`.
2. Pipe output to per-binary chart template with appropriate Helm placeholders.
3. Add CI gate in `.github/workflows/ci.yaml`: run generate:rbac, fail if `git diff` shows changes.

#### TDD
```
RED:    CI workflow check (NEW) — `task generate:rbac && git diff --exit-code` fails before T4.2.
GREEN:  After T4.2, the diff is empty.
VERIFY: 
  task generate:rbac
  git diff --exit-code config/rbac/role.yaml charts/loopzero-*-operator/templates/role.yaml
```

#### Acceptance Criteria
- [ ] `task generate:rbac` exists and works
- [ ] CI gate fails PRs that have stale RBAC vs markers
- [ ] All 4 chart role.yamls generated
- [ ] Pass: kubeconform on all generated yamls

#### DoD
- [ ] Pipeline in place
- [ ] CI gate active

### T4.3 — CI gate enforces markers ↔ usage

#### Objective
Static analysis: PR that adds `r.Client.Delete(ctx, ...)` without a corresponding `verbs=delete` marker → CI fails with explicit error.

#### Files to edit
```
.github/workflows/ci.yaml — add step `task validate:rbac`
hack/validate-rbac.sh (NEW) — semantic linter
```

#### Deep Dives
- **Approach:** parse Go AST for `r.Client.X(ctx, ...)` calls; correlate with `+kubebuilder:rbac` markers on the enclosing struct.
- **Out of scope for v1:** dynamic client calls (e.g., `dynClient.Resource(gvr).Get`) — flag as warning, not error.

#### Tasks
1. Write Go static analyzer in `hack/validate-rbac/main.go`.
2. Wire into Taskfile + CI.

#### TDD
```
RED:    Test_RBACAnalyzer_DetectsMissingMarker (NEW) — feed sample with Delete call but no delete marker; expect error.
GREEN:  Implement analyzer.
VERIFY: go test ./hack/validate-rbac/...
        task validate:rbac
```

#### Acceptance Criteria
- [ ] Analyzer detects missing markers for all CRUD verbs
- [ ] CI gate active
- [ ] False positive rate documented

#### DoD
- [ ] Static analysis in place
- [ ] CI enforcing

---

## Phase 5: Capability Level + Final Anti-Patterns

**Objective:** Declare formal capability levels per controller; close remaining anti-patterns.

### T5.1 — `capability-matrix.md`

#### Objective
Per-controller declaration of Operator Capability Level with evidence matrix.

#### Files to edit
```
docs/technical/operators/capability-matrix.md (NEW)
docs/meetings/adr/NNNN-operator-capability-levels.md (NEW)
```

#### Deep Dives
- Format: 3 columns per row (Capability → file:line → verifying test).
- Per controller: list which Levels (I-V) it achieves and which capabilities of each level have evidence.

#### Tasks
1. For each of 8 controllers, audit current capabilities against [Operator SDK Capability Levels](https://sdk.operatorframework.io/docs/overview/operator-capabilities/).
2. Map evidence: file:line + test name proving each capability.
3. Identify gaps for Level IV/V (e.g., PlatformBootstrap currently Level III; what would make it IV?).
4. Write ADR formalizing.

#### TDD
```
N/A — documentation phase.
VERIFY: ADR + matrix committed; reviewed by k8s-controller-engineer + sre-engineer.
```

#### Acceptance Criteria
- [ ] Matrix covers all 8 controllers
- [ ] Each capability has file:line evidence
- [ ] ADR signed off

#### DoD
- [ ] Matrix published
- [ ] ADR ratified

### T5.2 — Level IV-specific metrics

#### Objective
Add Prometheus metrics that prove "deep insights" capability per Operator SDK Level IV.

#### Files to edit
```
operators/internal/metrics/metrics.go — add new counters/gauges
```

#### Deep Dives
- New metrics:
  - `loopzero_self_heal_total{controller, action}` — count auto-heal events (e.g., reaper transitions).
  - `loopzero_orphan_cr_total{kind}` — count orphan CRs detected.
  - `loopzero_finalizer_blocked_seconds{kind}` — histogram of how long finalizer holds delete.
  - `loopzero_reconcile_queue_depth{controller}` — current queue depth.

#### Tasks
1. Define metrics in `metrics.go`.
2. Wire emission points (application reaper → SelfHeal counter; finalizer code → blocked_seconds histogram).
3. Add Grafana dashboard panels.

#### TDD
```
RED:    Test_Metrics_SelfHealTotalIncremented (NEW) — runs reaper action; asserts counter ≥ 1.
GREEN:  Wire emission.
VERIFY: go test ./operators/internal/metrics/... -v
```

#### Acceptance Criteria
- [ ] 4 new metrics exposed
- [ ] Emission verified in tests
- [ ] Grafana dashboard updated

#### DoD
- [ ] Metrics in production
- [ ] Dashboard live

### T5.3 — AP-4: ClusterCredential periodic-poll vs Watches consolidation

#### Objective
Resolve the three competing update paths flagged in AP-4: ClusterCredential reconcile RequeuePeriodicCheck (5min), Secret Watches (immediate), K8sCellStore poll (60s).

#### Files to edit
```
operators/internal/platform/clustercredential_controller.go — remove RequeuePeriodicCheck
api/internal/placement/k8s_cell_store.go — verify polls CR (not direct probe)
```

#### Deep Dives
- After analysis, decide: SoT is the CR. K8sCellStore reads CR status. ClusterCredential controller drops periodic poll; relies entirely on Watches + reconcile-on-change.

#### Tasks
1. Audit `K8sCellStore.LoadCells` — does it read CR or probe directly?
2. If CR: remove `RequeuePeriodicCheck` from controller.
3. If direct probe: refactor to read CR + only probe in dedicated controller path.

#### TDD
```
RED:    Test_ClusterCredentialController_NoRequeueOnSuccess (NEW) — assert no requeue when reconcile succeeds.
GREEN:  Remove the requeue.
VERIFY: envtest spec asserts watch-driven reconcile only.
```

#### Acceptance Criteria
- [ ] ClusterCredential controller doesn't requeue on success
- [ ] Watches handle all change detection
- [ ] No staleness regression

#### DoD
- [ ] AP-4 closed
- [ ] Telemetry confirms reduced reconcile rate

### T5.4 — AP-5: Cache scoping congruence test (cross-binary)

#### Objective
Test that each platform CRD's cache scope is declared in EXACTLY ONE binary's manager.

#### Files to edit
```
operators/cmd/registration_test.go — add TestCacheScopingCongruence
```

#### Deep Dives
- Parse each `cmd/*-operator/main.go` for `cache.Options.ByObject` entries.
- Assert: each CRD type appears in cache scoping of exactly ONE binary.
- Assert: each Setup*Controller wiring's reconciled type IS in the same binary's cache scope.

#### Tasks
1. Extend `cmd/registration_test.go` matrix test with congruence check.
2. Implement AST walker for cache.Options.

#### TDD
```
RED:    TestCacheScopingCongruence (NEW) — currently FAILS or PASSES depending on current state; deliberately add overlap to verify it catches.
GREEN:  Test passes after fixing any overlap.
VERIFY: go test ./operators/cmd/ -run TestCacheScopingCongruence -v
```

#### Acceptance Criteria
- [ ] Test detects overlap and orphan CRDs
- [ ] No legitimate overlap exists in current code (or documented exception)

#### DoD
- [ ] AP-5 closed
- [ ] Cache scoping verified

---

## Phase 6: Post-Launch Foundations

**Objective:** Lay groundwork for post-launch evolution (PL-1..PL-5). Lower priority — to be scheduled after Sprint 9 cut and external customer onboarding.

**Note:** Phase 6 tasks are listed for completeness per the user's request "TODOS OS PONTOS." Each is to be revisited via separate Change Governor flow once Sprint 9 ships.

### T6.1 — Adopt `sigs.k8s.io/multicluster-runtime` (PL-1)

**Status:** RFC-only in Sprint 9. Implementation post-launch.

**Objective:** Replace custom multi-cluster wiring (`internal/shared/cluster.go`, `runtime_client_factory.go`) with the SIG-maintained library.

**Files to edit:** `operators/go.mod`, `internal/shared/cluster.go`, `cmd/tenant-operator/main.go`, `cmd/preview-operator/main.go`.

**TDD:** Test_MultiClusterRuntime_FactoryEquivalence — assert new wrapping returns same Client API.

**Acceptance:** all current tests pass; -400 LOC custom; multicluster-runtime semantics documented in ADR.

### T6.2 — Korifi-style `BuildWorkload` CRD (PL-2)

**Status:** YAGNI until second builder requested. RFC-only.

**Objective:** Decouple API from Argo Workflow direct-submission via a `BuildWorkload` CR + runner controller.

**Files to edit:** `operators/api/v1/buildworkload_types.go` (NEW), `operators/internal/build/runner_controller.go` (NEW), `api/internal/services/build_service.go` (refactor).

**TDD:** RED specs covering CR creation → runner observation → Argo Workflow submission.

**Acceptance:** when triggered, refactor preserves all current build behaviors.

### T6.3 — Crossplane v2 Operations pattern for PlatformBootstrap (PL-3)

**Status:** Conditional — only if PlatformBootstrap grows beyond 4 phases.

**Objective:** Replace reconcile-driven bootstrap with finite-execution function pipeline.

### T6.4 — CRD conversion webhook scaffold (OP-G16, PL-4)

**Objective:** Add infrastructure for v1 → v2 CRD migrations without delete-recreate.

**Files to edit:** `operators/api/v1beta1/` (NEW package), `config/webhook/manifests.yaml`, `cmd/*-operator/main.go` (register webhook).

**TDD:** Test_ConversionWebhook_v1ToV2 — fake CR in v1, request as v2, assert correct conversion.

**Acceptance:** webhook scaffold ready; v2 schema decisions deferred to actual breaking change.

### T6.5 — CNPG-grade observability gaps (PL-5)

**Objective:** Reach 60+ metrics parity with CloudNativePG.

**Files to edit:** `operators/internal/metrics/metrics.go`, Grafana dashboards.

**Acceptance:** 17 new metrics added; alert rules updated.

---

## Coverage Matrix

| # | Roadmap Item | Task(s) | Resolution |
|---|---|---|---|
| 1 | QW-1 GenerationChangedPredicate | T2.1 | Predicates added to all 8 controllers |
| 2 | QW-2 Reconciling/Stalled types | T2.2 | Constants added; T3.1 helper consumes |
| 3 | QW-3 kubebuilder:rbac markers | T4.1, T4.2, T4.3 | Markers + pipeline + CI gate |
| 4 | QW-4 multi-binary registration_test | T1.3 | Matrix test covers all 4 binaries |
| 5 | QW-5 retry.RetryOnConflict | T2.3, T3.2 | Helper + applied in all Status updates |
| 6 | S9-T1 dual-tree consolidation | T1.1, T1.2, T1.3, T1.4, T1.5, T1.6 | Phase 1 entirely |
| 7 | S9-T2 kstatus universal | T3.1, T3.2, T3.3 | SetReadinessSummary + apply + integration test |
| 8 | S9-T3 Capability Level IV declared | T5.1, T5.2 | Matrix + Level IV metrics |
| 9 | S9-T4 predicates universal (consolidation) | T2.1 (entry), T5.4 (cache congruence) | Combined in Phase 2 + 5 |
| 10 | S9-T5 RBAC pipeline | T4.1, T4.2, T4.3 | Phase 4 entirely |
| 11 | PL-1 multicluster-runtime | T6.1 | Post-launch RFC |
| 12 | PL-2 BuildWorkload CRD | T6.2 | Post-launch RFC |
| 13 | PL-3 Crossplane Operations | T6.3 | Post-launch conditional |
| 14 | PL-4 conversion webhook | T6.4 | Post-launch scaffold |
| 15 | PL-5 CNPG observability | T6.5 | Post-launch metrics expansion |
| 16 | OP-G17 Predicate Filtering | T2.1 | Gate met by Phase 2 |
| 17 | OP-G18 kstatus Compliance | T2.2, T3.1, T3.2, T3.3 | Gate met by Phase 3 |
| 18 | OP-G19 RBAC From Markers | T4.1, T4.2, T4.3 | Gate met by Phase 4 |
| 19 | OP-G20 Capability Level Declaration | T5.1, T5.2 | Gate met by Phase 5 |
| 20 | OP-G21 Single Source Of Truth | T1.1-T1.6 | Gate met by Phase 1 |
| 21 | AP-1 tracing helpers duplication | Absorbed by T1.4 | Resolved via D7 + monolith deletion |
| 22 | AP-2 hardcoded condition strings | T2.4 | Constants replace strings |
| 23 | AP-3 silent error swallow | T2.4 | Get error surfaced as condition + log |
| 24 | AP-4 ClusterCredential poll vs watch | T5.3 | Periodic poll removed |
| 25 | AP-5 cache scoping congruence | T5.4 | Test gate added |
| 26 | OP-G16 upgrade safety (existing gate) | T6.4 | Conversion webhook scaffold |

**Coverage: 26/26 items (100%)**

## Global Definition of Done

- [ ] All 6 phases completed (Phases 0-5 in Sprint 9; Phase 6 post-launch tracked separately)
- [ ] All 26 items in Coverage Matrix resolved
- [ ] All Go tests passing: `go test ./operators/... -count=1` exit 0
- [ ] All TypeScript tests passing: `cd cli && npm test` exit 0 (no breakage from operator changes)
- [ ] Zero golangci-lint warnings: `task lint` exit 0
- [ ] Zero ESLint warnings (if any TS touched): `cd cli && npm run lint` exit 0
- [ ] kubeconform clean: `task validate:manifests` exit 0
- [ ] code-audit checks passing across all modified Go packages
- [ ] All 21 SOTA gates (OP-G01–G21) MET with verifiable evidence committed in `docs/technical/operators/sota-gates.md` notes
- [ ] -5800 LOC duplicated → 0 LOC duplicated (verify via `find operators/internal -name "*_controller.go" | xargs basename | sort | uniq -d` returns empty)
- [ ] `kubectl wait --for=condition=Ready` works for all 10 LoopZero CRDs in envtest
- [ ] `task generate:rbac && git diff --exit-code` clean on every PR (CI gate active)
- [ ] Capability Level IV declared per controller in `docs/technical/operators/capability-matrix.md`
- [ ] All ADRs (D1–D5) ratified
- [ ] Plan version bumped to v1.x with reference to ADR IDs in commit
- [ ] **`/dogfood full` PASSED** (see Dogfood Gate below)

---

## Dogfood Gate (MANDATORY — NON-NEGOTIABLE)

> **No plan is considered DONE without passing `/dogfood full`.** Unit tests, integration tests, envtest, and lint are necessary but NOT sufficient. Only real end-to-end validation via dogfood proves the operators module survives `loopzero deploy` / `loopzero promote` / `loopzero rollback` / `loopzero preview` / multi-cell + cell-2 isolation under realistic load.

After ALL phases (0–5) are complete and ALL tests pass, the final validation is a full dogfood audit on clean infrastructure.

### Execution

```bash
# 1. Fresh infrastructure (4 split operators must boot before dogfood starts)
task teardown && task setup && tilt up
task doctor  # verify mgmt + build + workload + workload-2 clusters healthy
kubectl get pods -n loopzero-system -l app.kubernetes.io/component=operator
# Must show 4 pods Running: loopzero-tenant-operator, loopzero-app-operator,
#                            loopzero-preview-operator, loopzero-platform-operator

# 2. Run full dogfood (sprint-1 through latest sprint scope)
/dogfood full
```

### Pass Criteria

- [ ] `/dogfood full` executed on clean infrastructure
- [ ] Pre-Flight Check: ALL checks PASS (4 operators Running + Ready)
- [ ] Zero S0 (Bloqueante) findings
- [ ] Zero S1 (Grave) findings related to this plan's scope (operators)
- [ ] Prova de Vida passes for every deploy performed during dogfood
- [ ] Prova de Morte passes for every delete performed during dogfood
- [ ] No regressions in previously passing flows
- [ ] Observability check passes: `loopzero_self_heal_total`, `loopzero_reconcile_total{controller}`, kstatus conditions all queryable
- [ ] Rule 3 clean: zero internal terms in any CLI output during dogfood
- [ ] Multi-cell scenarios: tenant placed on cell-2 reconciles correctly under split-binary deploy
- [ ] `kubectl wait --for=condition=Ready loopzeroapplications/<name>` works for live deploys

### On Failure

If `/dogfood full` finds S0 or S1 issues:

1. **Triage immediately** — identify which task in this plan introduced the regression.
2. **Open hotfix PR** — fix root cause, not symptom.
3. **Add regression test** — prevent recurrence (TDD: RED that reproduces the bug, then GREEN).
4. **Re-run `/dogfood full`** on fresh infrastructure.
5. **Repeat** until zero S0/S1.

S2-S4 findings are documented as follow-up issues but do NOT block plan completion.

### Special Validation Steps

Beyond standard dogfood, this plan requires:

1. **Split-binary blast radius validation:**
   ```bash
   # Kill app-operator pod; assert tenant-operator and preview-operator unaffected
   kubectl delete pod -n loopzero-system -l app.kubernetes.io/name=loopzero-app-operator
   loopzero deploy --project /tmp/sample-app  # Should still queue (tenant-operator handles)
   # Wait for app-operator to recover; assert deploy completes
   ```

2. **kstatus library interop:**
   ```bash
   # Use kstatus CLI to validate LoopZero CRDs render correct status
   kustomize build manifests/ | kstatus --status=current --timeout=300s
   ```

3. **RBAC drift gate:**
   ```bash
   # Make a trivial Reconcile change (add a r.Client.List call)
   # WITHOUT updating the +kubebuilder:rbac marker
   # Assert CI fails the PR with explicit error
   ```

4. **Capability matrix audit:**
   ```bash
   # Verify every entry in capability-matrix.md has a passing test
   grep -E "test:.*Test_" docs/technical/operators/capability-matrix.md | \
     while read entry; do
       go test -run "$entry" ./operators/... -v
     done
   ```
