# MEET-INFRA-NORMALIZE-T3 — Decision Record (T3 Governance Review)

**Date:** 2026-04-30
**Topic:** Operator topology forward-only migration (`docs/plans/infra-normalize-multicell-plan.md` ADR D1)
**Type:** `architecture` + `controllers` + `pipeline` + `security` (cross-cutting)
**Tier:** T3 (operator binary topology + Helm chart cutover + Helmfile mutation)
**Scope:** Plan Phase −1 / T-1.1 (this meeting), governing Phase 2 / T2.1–T2.5
**Out of scope:** Pulumi consolidation (D2), multi-cell activation (D3), CLI cluster-real tests (D4)

**Agents convened:** platform-architect (Sponsor) · production-design-reviewer (Challenger) · platform-security-engineer (Veto) · sre-engineer (Veto) · verification-architect · k8s-controller-engineer

**Owner (post-approval):** infra-engineer + operator-engineer (joint, sre-engineer co-signs runbook)

**Verdict:** **APPROVED_WITH_CONDITIONS**

---

## Stage 1 — Context

PR #51 (`refactorhelms`, head `29b4d392`, merged into `fix/parity-on-refactorhelms`) shipped four split operator binaries (`operators/cmd/{tenant,app,preview,platform}-operator/main.go`) and four published charts under `charts/loopzero-{tenant,app,preview,platform}-operator/`. `Tiltfile` (lines 134/148/162/176) deploys those four split pods. However `infra/helmfile/mgmt.helmfile.yaml.gotmpl` still declares two **monolithic** releases — `tenant-controller` (line 355) and `loopzero-controller` (line 341) — both running the legacy `operators/cmd/main.go` binary which wires all 8 reconcilers in one process.

Net effect: `tilt up` runs 4 split pods with narrow ClusterRoles + 4 distinct LeaderElectionIDs; `loopzero platform install -f values.yaml` (canonical install per ADR-0020 D2) runs 1 monolithic pod with `loopzero.io/*` wildcard ClusterRole. This is a falsifiable violation of **CLAUDE.md Rule 10** (Local equals production). The "Helm Chart Dual System" (CLAUDE.md lines 218–227) classified the split charts as "forward-looking preparation for binary split (Sprint 9+)" — a transition tolerance, not an end state. PR #51 has now built the end state in `charts/` and `Tiltfile`; the helmfile cutover is the only remaining step.

**Decision required:** adopt **Option B — forward-only migration** (plan ADR D1 line 65) where one helmfile change atomically removes the two legacy releases and adds the four split releases. No `splitOperators` flag. Rollback via reverse-patch commit (CLAUDE.md `.claude/rules/workflow.md` line 33 forbids both `git checkout` and `git revert` — see C7).

**Veto criteria from plan ADR D1.**
- Security (line 68): "4 split ClusterRoles must be least-privilege subset of monolith. Veto if RBAC widens vs monolith." Plus "shared SA across operators" prohibited (line 69).
- SRE (line 69): "4 leases must coexist without thrashing. Veto if pod restarts > 5/h during steady state." Plus resource limits realistic (line 694, "≤2× monolith").
- Production-design (line 70): "Cutover must be atomic (no window where 0 reconcilers run >30s)."

**Phase 2 sub-tasks (T2.1–T2.5)** are blocked on this meeting's APPROVED status. Plan T2.5 explicitly defers chart-dir deletion 1 sprint after T2.3 GREEN.

---

## Stage 2 — Analyses

### platform-architect (Sponsor)

Option B is architecturally correct for pre-release. Option A (flag) would create permanent CLI flag-aware logic and helmfile fork — exactly the dual-system anti-pattern CLAUDE.md already documents. Option C (defer) is rejected by sunk cost: PR #51 is merged, Tiltfile already runs split pods, every day helmfile lags Tiltfile the Rule 10 violation deepens.

Risks: (1) finalizers set by monolith must be recognized by split binaries — likely structural since both import `operators/internal/...`, but must be asserted by test (C3); (2) helmfile `needs:` for `smoke-test` currently references the 2 legacy releases (lines 387–391) — cutover must update them or sync fails (C1); (3) LOOPZERO_COMPONENTS removal of `tenant-controller` + `loopzero-controller` is a CLI `--json` breaking change requiring CHANGELOG `[Removed]` (C2). **Recommendation: APPROVED with C1, C2, C3.**

### production-design-reviewer (Challenger)

The plan's atomicity claim rests on helmfile's own ordering, which is **NOT atomic across release add+remove**. `helmfile sync` only adds/updates — when releases are removed from the file, helmfile leaves them running. Cutover therefore produces a window where 1 monolith pod + 4 split pods run concurrently, two leader-election leases reconciling the same Tenant CR, finalizer cleanup possibly double-executing — exactly the iter-3 incident pattern (memory ref `reference_sprint8_l3_iteration_2026_04_28.md` B12).

Mitigation must be an explicit migration script (`infra/scripts/migrate-operators-monolith-to-split.sh`) that (i) helmfile sync of new releases, (ii) wait until 4 split pods Ready, (iii) `helm uninstall` legacy releases, (iv) verify CR status converges. Plan T2.2 conflates chart-DIR deletion (T2.5, deferred) with chart-RELEASE uninstall (must happen at T2.2). Plan rollback ("`git revert <merge-sha>` + helmfile sync") is a forbidden command per `.claude/rules/workflow.md` line 33 AND would leave split releases running. Resource defaults exceed the 2× cap on limits (640Mi total vs ~256Mi monolith). T2.4 (Tilt → helmfile output) cannot be deferred — Tilt vs helmfile drift is the same Rule 10 violation from another direction.

**Recommendation: APPROVED with C4 (migration script), C5 (rollback script), C6 (no-window proof), C7 (reverse-patch rollback method), C8 (resource calibration), C9 (T2.4 same-PR).**

### platform-security-engineer (Veto holder — RBAC + SA)

I diff'd each split ClusterRole against the monolith baselines (`infra/helm/{tenant-controller,loopzero-controller}/templates/rbac.yaml`).

**`loopzero-app-operator`:** clean strict subset. PASS.
**`loopzero-preview-operator`:** clean strict subset (no update/patch on namespaces; no delete on quotas). PASS.
**`loopzero-platform-operator`:** **net security improvement.** Cluster-wide secrets is `[get,list,watch,create,update]` (no `delete`). Two namespace-scoped Roles narrow `delete` on secrets to `resourceNames: [platform-tokens]` in `loopzero-system` and to `secrets` in `argocd`. PASS.
**`loopzero-tenant-operator`:** **PARTIALLY VIOLATED.** 5 widenings vs monolith:
1. `scheduling.k8s.io: priorityclasses` `[get,list,watch,create,update]` — NOT in monolith.
2. `apps: deployments` `[get,list,create,update,patch]` — NOT in monolith.
3. `core: pods, pods/log` `[get,watch,patch,list]` — NOT in monolith.
4. `argoproj.io: workflowtaskresults, workflowtasksets` `[create,patch,get,list]` — NOT in monolith.
5. `argoproj.io: workflows, workflowtemplates, clusterworkflowtemplates` full CRUD — NOT in monolith.

Plus a possible REGRESSION: monolith grants `clusterroles, clusterrolebindings`; split tenant-operator does NOT. If tenant code creates cluster-scoped RBAC, split will fail to reconcile.

**ServiceAccount audit:** each split chart's SA name is `{include "loopzero-common.serviceAccountName" .}` resolving to `.Release.Name` ⇒ four distinct SAs (`loopzero-tenant-operator`, `loopzero-app-operator`, `loopzero-preview-operator`, `loopzero-platform-operator`). **No SA sharing. SA criterion (line 69) SATISFIED.**

**Veto position: PARTIAL VETO** lifted CONDITIONAL on **C10** (RBAC reconciliation per widening — code reference or removal — committed as `docs/runbooks/operator-split-rbac-audit.md`), **C11** (verify whether monolith's `clusterroles/clusterrolebindings` grant is dead RBAC or a missing grant in split), **C12** (ImagePullSecret propagation across 4 environments × 4 split releases).

### sre-engineer (Veto holder — leases + atomicity + budget)

**Lease coexistence (line 69):** four distinct LeaderElectionIDs verified (`loopzero-{tenant,app,preview,platform}-operator.loopzero.io`). With `replicaCount: 1` each lease is held perpetually. 4× lease load is trivial. **Steady-state criterion SATISFIED.**

**Atomicity (line 70):** `replicaCount: 1` + Deployment rolling update + 30s lease duration produces a >30s reconciler gap on routine pod restarts (image rotation, ConfigMap hash). **FAILS line 70 for hot-path operators.** Cutover-window atomicity: I co-sign production-design's analysis — without C4 migration script, helmfile sync leaves split-brain.

**Resource budget (line 694):** chart defaults total req=250m/160Mi, lim=1100m/640Mi. Monolith production typical lim ≈ 1 CPU/512Mi. 640Mi total ≈ 1.25× monolith — within 2× cap. **Criterion SATISFIED at defaults**, but C8 calibration against observed runtime data is correct in spirit.

**Veto position: LIFTED CONDITIONAL on C4–C6, C8, C13** (`replicaCount: 2` for tenant-operator + app-operator on dev/prod via env-values overrides; standalone keeps 1; preview/platform stay at 1 with documented sub-30s gap accepted), **C14** (PDB renders only when replicas≥2; else node drain hangs forever), **C15** (24h post-cutover restart-rate watcher → Prometheus alert; blocks T2.5 chart-dir deletion if breached).

### k8s-controller-engineer

Each split binary imports the SAME `operators/internal/...` packages as the monolith, so finalizer constants are structurally identical (closes platform-architect risk #1). Each binary's manager does NOT set `Cache.DefaultNamespaces`. **However**, ADR-0018 C9 (MEET-ADR-0018-decision.md) requires per-Kind cache scoping for `Tenant`/`Environment` to `loopzero-system` only. The split tenant-operator's main.go does NOT visibly set this. **If `tenant.SetupTenantController` does not internally enforce ADR-0018 C9, this cutover SILENTLY REGRESSES a previously-decided invariant.** Cross-decision dependency NOT flagged in plan.

Also: each binary `_ import "operators/internal/metrics"` registers metrics globally. `MustRegister` would panic on init if any name collides. Verify `Register` (returns error) vs `MustRegister`.

**Recommendation: APPROVED CONDITIONAL on C16** (cache scoping per ADR-0018 verified by envtest), **C17** (metrics registration audit + test), **C18** (cross-decision invariants matrix `docs/decisions/operator-split-invariants.md` referencing ADR-0017/0018/0015).

### verification-architect

Plan T2.1 lists 4 unit/lint tests; minimum bar is 18-test matrix (T-01..T-18 below). Plan T2.3's "iterate until GREEN" has no time-box.

**Test matrix (mandatory pre-merge unless noted):**

| ID | Layer | What it validates | Owner |
|----|-------|-------------------|-------|
| T-01 | unit | Each binary's `LeaderElectionID` is unique + matches expected | operator-engineer |
| T-02 | unit | Finalizer string constants stable (regression for C3) | operator-engineer |
| T-03 | unit | Metrics registration: all 4 cmd packages init without panic (C17) | operator-engineer |
| T-04 | helm/lint | `helm lint` exits 0 for all 4 charts | infra-engineer |
| T-05 | helm/kubeconform | `helm template <chart> | kubeconform --strict` exits 0 | infra-engineer |
| T-06 | helm/policy | RBAC subset checker tool (C20) — split rules ⊆ monolith rules | infra-engineer |
| T-07 | envtest | ADR-0018 cache scoping preserved: Tenant in `default` ns NOT reconciled by split tenant-operator (C16) | operator-engineer |
| T-08 | envtest | Split tenant-operator recognizes monolith-set finalizers + can remove them | operator-engineer |
| T-09 | helmfile | `helmfile -e standalone-multi template` renders 4 split + 0 monolith | infra-engineer |
| T-10 | helmfile | `smoke-test` `needs:` references 4 split operators (C1) | infra-engineer |
| T-11 | integration | `task parity:k3d-virgin` GREEN: 4 operators Ready, 0 monolith pods, all E2E specs PASS | qa |
| T-12 | integration | Migration script: monolith → split, zero-Ready window per controller ≤30s (C6 watcher), CR statuses preserved | qa + sre |
| T-13 | integration | Rollback script: split → monolith, ≤5 min wall-clock (C5) | qa + sre |
| T-14 | regression | All 1079 CLI tests + 41 Go packages + 200+ envtest specs + parity tests PASS | ci |
| T-15 | observability | `loopzero_operator_reconcile_duration_seconds{operator}` exported + scraped + dashboard panels (post-merge ≤7d) | sre |
| T-16 | observability | `OperatorTopologyMixed` alert fires if both topologies coexist >5min (post-merge ≤7d) | sre |
| T-17 | restart-rate | 24h post-cutover restart-rate ≤5/h per operator (post-merge, blocks T2.5) | sre |
| T-18 | runbook | Manual exercise of `docs/runbooks/operator-topology-cutover.md` on dev k3d (before prod rollout) | sre |

**Recommendation: APPROVED CONDITIONAL on C19** (T-01..T-14 GREEN before merge), **C20** (`infra/scripts/rbac-subset-check.sh` committed and wired into `.github/workflows/ci.yml`), **C21** (T2.3 3-iteration time-box, escalate to T2 supplementary meeting if RED).

---

## Stage 3 — Confrontation

### Conflict 1 — Helmfile sync atomicity vs explicit migration script

All six agents agree: **helmfile sync alone is NOT atomic**; without C4 migration script, split-brain is the default outcome. Resolution: **C4 + C5 are mandatory blockers**. Plan ADR D1 line 65 amended: "cutover is one PR, executed via `migrate-operators-monolith-to-split.sh`, with `rollback-operators-split-to-monolith.sh` as the reverse op."

### Conflict 2 — RBAC widening in tenant-operator

Security partial-veto unanimous. Each of the 5 widenings must be either (a) justified by code reference + mirrored in monolith baseline before cutover, or (b) removed from the split chart. Investigation committed as `docs/runbooks/operator-split-rbac-audit.md` BEFORE any helmfile cutover. Resolution: **Security veto lifted ONLY after C10 evidence committed.**

### Conflict 3 — `replicaCount: 1` vs line 70 atomicity criterion

SRE veto on default replicaCount. platform-architect counter-points: standalone (single-node self-hosting) gets no availability gain from 2 replicas. Resolution: **C13 — `replicaCount: 2` for tenant-operator + app-operator on dev/prod ONLY** (env-values overrides). Standalone keeps 1; preview-operator + platform-operator keep 1 with documented sub-30s gap acceptance.

### Conflict 4 — Rollback method (forbidden git command vs reverse-patch commit)

Plan T2.2 line 773 invokes a forbidden git command per `.claude/rules/workflow.md` line 33. Resolution: **C7 — rollback via reverse-patch commit** (`git diff <cutover-sha>~1 <cutover-sha> -- <files> | git apply -R` then `git add` + `git commit`). Plan T2.2 line 773 amended.

### Conflict 5 — T2.4 (Tilt → helmfile output) deferred or same-PR?

Production-design + architect + verification all argue same-PR; Tilt drift is the same Rule 10 violation from another angle. Resolution: **C9 — T2.4 in the SAME PR as T2.2 cutover.**

### Self-challenge (orchestrator)

Three additional gaps surfaced: (1) `.github/workflows/build-images.yml` must build all 4 new Dockerfiles or cutover ImagePullBackOff → **C22**; (2) `loopzero platform install` invoked during cutover may show `unhealthy` because it polls deployments being uninstalled → **C23** (drain step in runbook); (3) local chart deps (`file://../loopzero-common`) require explicit `helmfile deps` → **C24**.

---

## Stage 4 — Decision

**Chosen approach.** **Option B — forward-only migration** with mandatory conditions C1–C24.

**Alternatives considered & rejected.**

| Alternative | Rejection reason |
|-------------|------------------|
| **Option A — Feature flag `splitOperators`** | Permanent flag debt; CLI `getPlatformStatus` flag-aware forever; helmfile dual-rendering forever. Pre-release governance principle (`.claude/rules/pre-release-governance.md` line 96): "We CAN break things and fix them properly" — flag preserves a bad design. **REJECTED.** |
| **Option B — Forward-only migration** (CHOSEN) | Zero new debt; rollback via reverse-patch commit; simpler test surface; matches pre-release bias. Cutover risk mitigable via C4–C9. |
| **Option C — Defer entirely to Sprint 9+** | PR #51 charts + Tiltfile + Dockerfiles already merged. Reverting them is more disruptive than completing cutover. Every day helmfile lags Tiltfile, Rule 10 violation deepens. **REJECTED.** |

**Trade-offs consciously accepted.**
1. Steady-state pod count: 1 → 6 (2 hot-path × 2 replicas + 2 cold-path × 1 replica) on dev/prod; standalone keeps 4 × 1.
2. Operational surface: 4 deployments + 4 ClusterRoles + 4 leases + 4 PDBs + 4 ServiceMonitors. Mitigated by C13/C14/C15/C20.
3. CHANGELOG breaking change on CLI `--json` component names. Mitigated by C2 explicit `[Removed]` + `[Added]`.
4. Standalone (self-hosting) loses single-deployment simplicity. Acceptable: parity (Rule 10) > minimalism; ~640Mi bounded.

**Risks accepted (with mitigation + owner).**

| Risk | Sev | Prob | Mitigation | Owner |
|------|-----|------|------------|-------|
| Split-brain reconcile window during cutover | HIGH | HIGH w/o script | C4 migration script + C6 watcher | infra + sre |
| RBAC widening unaccounted (5 rules in tenant-op) | HIGH | confirmed | C10 audit + C20 CI checker | infra + security |
| ADR-0018 cache scoping silently regressed | MED | MED | C16 envtest + code change | operator |
| Replica=1 default fails atomicity on routine pod restart | MED | HIGH | C13 replicas=2 hot-path | infra |
| PDB + replica=1 hangs node drain | MED | LOW | C14 conditional PDB | infra |
| Rollback ≤5 min unverified | MED | MED | C5 script + T-13 dev rehearsal | sre |
| Build pipeline missing 4 new Dockerfiles | HIGH | LOW | C22 CI matrix update | infra |
| Active CLI install collides with cutover | LOW | LOW | C23 drain step | sre |
| `helmfile deps` not run for local loopzero-common | MED | MED | C24 task install:mgmt | infra |
| Resource defaults are guesses | LOW | confirmed | C8 7d calibration | sre |
| Restart rate regression unnoticed | MED | LOW | C15 watcher; T2.5 gate | sre |
| Finalizer drift across refactors | MED | LOW | C3 unit test (T-02) | operator |
| Metrics MustRegister panic | LOW | LOW | C17 audit (T-03) | operator |
| `getPlatformStatus` JSON contract change | LOW | confirmed | C2 CHANGELOG entries | cli + doc |
| Smoke-test `needs:` stale references | LOW | confirmed | C1 update | infra |

**Risks NOT accepted (block until mitigated):** none — every risk has mitigation, owner, validation test.

**Mandatory actions before implementation merge.**

1. **C1.** `smoke-test` release `needs:` references 4 split operators.
2. **C2.** CHANGELOG `[Unreleased]` `[Removed]` + `[Added]` entries.
3. **C3.** Finalizer-stability unit test (T-02).
4. **C4.** `infra/scripts/migrate-operators-monolith-to-split.sh` — `--dry-run`/`--apply`, exit codes 0/2/3, includes Ready-wait + explicit `helm uninstall` of legacy + status snapshot diff.
5. **C5.** `infra/scripts/rollback-operators-split-to-monolith.sh` — ≤5 min wall-clock, exercised on dev k3d.
6. **C6.** Zero-Ready-window watcher integrated into T-12.
7. **C7.** Plan T2.2 line 773 amended: rollback via reverse-patch commit (per `.claude/rules/workflow.md` line 33).
8. **C8.** Resource budget calibrated against 7-day observed monolith data.
9. **C9.** T2.4 (Tilt → helmfile output) merged in SAME PR as T2.2.
10. **C10.** `docs/runbooks/operator-split-rbac-audit.md` reconciles 5 tenant-op widenings.
11. **C11.** tenant-op `clusterroles/clusterrolebindings` grant decision documented.
12. **C12.** ImagePullSecret propagation across 4 environments × 4 split releases.
13. **C13.** `replicaCount: 2` for tenant-op + app-op on dev/prod.
14. **C14.** PDB conditional on `replicaCount >= 2`.
15. **C15.** 24h post-cutover restart watcher + Prometheus alert; blocks T2.5.
16. **C16.** Cache scoping per ADR-0018 verified by envtest (T-07).
17. **C17.** `operators/internal/metrics` registration audit + test (T-03).
18. **C18.** `docs/decisions/operator-split-invariants.md` cross-references ADR-0017/0018/0015.
19. **C19.** Test matrix T-01..T-14 GREEN before merge.
20. **C20.** `infra/scripts/rbac-subset-check.sh` committed + wired into `.github/workflows/ci.yml`.
21. **C21.** Plan T2.3 amended: 3-iteration time-box; escalate to T2 supplementary meeting.
22. **C22.** `.github/workflows/build-images.yml` builds all 4 split Dockerfiles.
23. **C23.** Cutover runbook includes "drain active control-plane CLI operations" step.
24. **C24.** `task install:mgmt` includes `helmfile deps` for local `file://` chart deps.

---

## Stage 5 — Execution Contract

**Owner (joint).** infra-engineer (lead) + operator-engineer + sre-engineer + cli-engineer + doc owner.

**Deliverables.**

| # | Artifact | Path | Owner |
|---|----------|------|-------|
| D1 | This decision record | `docs/decisions/MEET-INFRA-NORMALIZE-T3-operator-split.md` | orchestrator |
| D2 | Migration script | `infra/scripts/migrate-operators-monolith-to-split.sh` | infra |
| D3 | Rollback script | `infra/scripts/rollback-operators-split-to-monolith.sh` | infra |
| D4 | Cutover runbook | `docs/runbooks/operator-topology-cutover.md` | sre |
| D5 | RBAC audit | `docs/runbooks/operator-split-rbac-audit.md` | security + infra |
| D6 | RBAC subset checker | `infra/scripts/rbac-subset-check.sh` | infra |
| D7 | Cross-decision invariants | `docs/decisions/operator-split-invariants.md` | k8s-controller + architect |
| D8 | Helmfile cutover | `infra/helmfile/mgmt.helmfile.yaml.gotmpl` (−2 monolith, +4 split, smoke-test `needs:`) | infra |
| D9 | Tilt cutover | `Tiltfile` (helmfile-rendered output; 8 orphan yamls deleted per T2.4) | infra |
| D10 | CLI status update | `cli/src/installer/status.ts` (LOOPZERO_COMPONENTS = 4 split names) | cli |
| D11 | CLI tests | `cli/tests/unit/getPlatformStatus-charts.test.ts` | cli |
| D12 | Replica defaults | `infra/helmfile/environments/{dev,prod}.yaml` | infra |
| D13 | Resource calibration | `charts/loopzero-{tenant,app,preview,platform}-operator/values.yaml` | sre + infra |
| D14 | PDB conditional | `charts/loopzero-common/templates/_pdb.tpl` | infra |
| D15 | Cache scoping fix | `operators/cmd/tenant-operator/main.go` (ADR-0018 C9) | operator |
| D16 | Tests T-01..T-14 | per matrix | per matrix |
| D17 | CI build matrix | `.github/workflows/build-images.yml` | infra |
| D18 | Prometheus alerts | `infra/helm/monitoring/values-prometheus.yaml` (`OperatorReconcileSlowdown`, `OperatorTopologyMixed`) | sre |
| D19 | Dashboard | `infra/helm/monitoring/dashboards/operators.json` (4 panels) | sre |
| D20 | CHANGELOG | `CHANGELOG.md` `[Unreleased]` | doc |

**Validations.**
- `task test` GREEN (T-01..T-08).
- `task lint` + `task validate:manifests` + `task validate:helm-charts` GREEN (T-04, T-05).
- `infra/scripts/rbac-subset-check.sh` exit 0 (T-06).
- `helmfile -e standalone-multi template` renders 4 split + 0 monolith (T-09).
- `task parity:k3d-virgin` GREEN (T-11).
- Migration + rollback scripts exercised on dev k3d (T-12, T-13).
- Pre-existing 1079 CLI tests + 41 Go packages + parity tests PASS (T-14).
- 24h post-cutover restart-rate ≤5/h per operator (T-17, blocks T2.5).
- `OperatorTopologyMixed` alert NOT firing post-merge (T-16).

**Definition of Done.**

- [ ] All 24 conditions C1–C24 satisfied with committed evidence.
- [ ] Test matrix T-01..T-14 GREEN before merge.
- [ ] T-15..T-17 GREEN within 7 days post-merge; T2.5 blocked until then.
- [ ] T-18 (runbook manual exercise) GREEN before production rollout.
- [ ] CHANGELOG `[Unreleased]` updated.
- [ ] PR description references this decision record.
- [ ] Production rollout per `docs/runbooks/operator-topology-cutover.md`. Out-of-order execution rejected.
- [ ] Post-rollout: zero `OperatorTopologyMixed` alerts for 7 days; restart count delta ≤5/h per operator.
- [ ] After 1 sprint of GREEN, T2.5 deletion of `infra/helm/{tenant-controller,loopzero-controller}/` proceeds in dedicated commit.

---

## Migration Playbook (summary; full steps in D4 runbook)

Cutover sequence on a target cluster:

1. **Pre-flight.** `loopzero platform status --json` confirms 1 monolith pod Ready. Set MAINTENANCE_MODE on API (C23 drain). Snapshot CR statuses to `/tmp/loopzero-pre-cutover-cr-state.json`.
2. **Build.** `.github/workflows/build-images.yml` GREEN for all 4 split Dockerfiles (C22). Images present in registry.
3. **Migrate.** Run `infra/scripts/migrate-operators-monolith-to-split.sh --apply` (C4):
   - `helmfile -e <env> sync` adds 4 split releases (legacy still running).
   - C6 watcher polls `kubectl get tenant -A` every 1s; abort if zero-Ready window per controller > 30s.
   - Once 4 split pods Ready: `helm uninstall tenant-controller loopzero-controller -n loopzero-system`.
   - Verify CR statuses match pre-cutover snapshot.
   - Emit summary report → exit 0.
4. **Validate.** Run T-09, T-10, T-11. Smoke-test PASSES.
5. **Observe.** Restart-rate watcher (C15) starts. `OperatorTopologyMixed` must NOT fire.
6. **Lift maintenance.** API back online.
7. **Hold T2.5.** Legacy chart dirs (`infra/helm/{tenant-controller,loopzero-controller}/`) deletion deferred 1 sprint per plan T2.5.

---

## Rollback Procedure (≤5 min per cluster, per C5 + C7)

Triggered if C4 returns exit 2/3, or if production observation reveals split-brain:

1. **Runtime rollback (≤2 min).** Run `infra/scripts/rollback-operators-split-to-monolith.sh` (C5):
   - `helm uninstall loopzero-tenant-operator loopzero-app-operator loopzero-preview-operator loopzero-platform-operator -n loopzero-system`.
   - Restore monolith helmfile entries (via reverse-patch — see step 2), then `helmfile -e <env> sync`.
   - Verify monolith pod Ready.
2. **Source-of-truth rollback (≤2 min).** From clean working tree at the cutover SHA: produce reverse-patch via `git diff <cutover-sha>~1 <cutover-sha> -- infra/helmfile/ Tiltfile cli/src/installer/status.ts | git apply -R`, then `git add` + `git commit -m "rollback: cutover MEET-INFRA-NORMALIZE-T3"`. Push as a NEW commit. **Do NOT use the forbidden git commands per `.claude/rules/workflow.md` line 33.**
3. **Verify (≤1 min).** `loopzero platform status` returns Ready with 1 monolith pod. CRs healthy.
4. **Post-mortem.** Document root cause; new T2 meeting required before re-attempting cutover.

---

## Verdict — Final

**APPROVED_WITH_CONDITIONS.**

- **Security veto (PARTIAL):** lifted CONDITIONAL on **C10 + C11 + C12 + C20**. Without C10 audit evidence, security veto is REINSTATED.
- **SRE veto:** lifted CONDITIONAL on **C4 + C5 + C6 + C8 + C13 + C14 + C15**.
- **Production-design veto:** lifted CONDITIONAL on **C4 + C5 + C7 + C9 + C18 + C23**. Atomicity claim accepted ONLY via script — NOT via raw helmfile sync.
- **Architect veto:** none. Forward-only is the architecturally correct posture.
- **Verification veto:** lifted CONDITIONAL on **C19 + C20 + C21**.
- **DX impact:** CHANGELOG breaking-change (C2) acceptable in pre-release.

**Phase 2 implementation may begin only after:**
1. This decision record committed to `docs/decisions/`.
2. Plan `infra-normalize-multicell-plan.md` updated to reference C1–C24 in T2.1, T2.2, T2.3, T2.4 task lists.
3. Test matrix T-01..T-14 written in failing state (TDD red).
4. Migration script (C4) drafted and dev-cluster-rehearsed.
5. RBAC audit (C10) committed.
6. Cutover runbook (C7 method, C23 drain step) drafted.

**This decision record is binding. Any deviation requires a new T3 supplementary meeting.**

---

## References

- **Plan:** `/home/paulo/loopzero/docs/plans/infra-normalize-multicell-plan.md` (ADR D1 lines 56–73; Phase 2 lines 627–959).
- **Helmfile:** `/home/paulo/loopzero/infra/helmfile/mgmt.helmfile.yaml.gotmpl` (393 lines).
- **Tilt:** `/home/paulo/loopzero/Tiltfile` (210 lines).
- **Split charts:** `/home/paulo/loopzero/charts/loopzero-{tenant,app,preview,platform}-operator/`
- **Monolith baseline:** `/home/paulo/loopzero/infra/helm/{tenant-controller,loopzero-controller}/templates/{deployment.yaml,rbac.yaml}`
- **Operator binaries:**
  - `/home/paulo/loopzero/operators/cmd/tenant-operator/main.go` (LeaderElectionID=`loopzero-tenant-operator.loopzero.io`)
  - `/home/paulo/loopzero/operators/cmd/app-operator/main.go` (`loopzero-app-operator.loopzero.io`)
  - `/home/paulo/loopzero/operators/cmd/preview-operator/main.go` (`loopzero-preview-operator.loopzero.io`)
  - `/home/paulo/loopzero/operators/cmd/platform-operator/main.go` (`loopzero-platform-operator.loopzero.io`)
  - `/home/paulo/loopzero/operators/cmd/main.go` (legacy monolith, `loopzero-operators.loopzero.io`)
- **Project rules:**
  - `/home/paulo/loopzero/CLAUDE.md` (Rule 1, Rule 5, Rule 10, "Helm Chart Dual System" lines 218–227).
  - `/home/paulo/loopzero/.claude/rules/workflow.md` (Git Rules — Inviolable; line 33).
  - `/home/paulo/loopzero/.claude/rules/pre-release-governance.md` (T3 trigger; pre-release bias).
- **Sibling decisions:**
  - `/home/paulo/loopzero/docs/decisions/MEET-ADR-0018-decision.md` (template + C16 cross-decision dependency).
  - `/home/paulo/loopzero/docs/decisions/ADR-0020-infra-parity-decisions.md` (D2: `loopzero platform install` is canonical install).
