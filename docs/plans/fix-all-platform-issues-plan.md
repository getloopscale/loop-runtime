# Plan: Fix All Platform Issues — Post Cell-2 Activation Cleanup

> **Version 1.0** — Comprehensive cleanup plan addressing every defect surfaced during cell-2 activation iter 4 LIVE validation that is OUTSIDE the cell-2-activation-plan ADR scope but inside the user mandate "ENCONTRAR BUGS E CORRIGIR ATE QUE TUDO ESTEJA 100% FUNCIONAL". Covers: (1) operator split-binary parity for the cell-aware factory fix, (2) ClusterCredential controller bug that requires the H15 workaround, (3) 11 orphaned LoopZeroApplication CRs stuck in Progressing for 3+ days, (4) 15+ ArgoCD apps OutOfSync across mgmt+build clusters with one duplicate-env-var blocker, (5) zero ACTIVE releases / pipeline drift (70 CANCELLED + 8 GIT_MIRROR_FAILED + 3 SYNC_FAILED), and (6) full ADR-compliant 1h+24h soaks for cell-2 production readiness signal. The expected outcome is a LoopZero platform where every observable layer (operator reconcile, GitOps sync, release pipeline, multi-cell routing, orphan hygiene) reports GREEN persistently — closing the maturity gap from 3.8 to 4.0+.

## Context

**State today (2026-05-02 post cell-2-activation iter 4):** The cell-2 activation plan delivered 12/12 ADR gates GREEN and a critical bug fix (`CellAwareRuntimeClientFactory`, image `tenant-controller:v0.4.2-cell-aware`). However, LIVE inspection during iter 4 surfaced six independent classes of pre-existing platform debt that are NOT covered by the cell-2 plan's ADR D6/D1 scope but are blocking the user-stated goal of "100% functional":

1. **Operator split-binary parity gap.** The fix landed only in `operators/internal/controller/` (monolithic build at `cmd/main.go`). The split-binary path (`operators/internal/tenant/` + `cmd/tenant-operator/main.go`) carries the same `r.RuntimeClient` direct-access bug — 17 call sites untouched. When the platform migrates to split-binary deploy (ADR D1, future), the cell-aware behavior regresses unless this is fixed in lockstep.
2. **ClusterCredential controller token-propagation gap.** Heal script H15 is a workaround for the controller's failure to propagate SA token rotation from `cell-2-kubeconfig` (loopzero-system) → `cell-2-argocd-cluster` (argocd). Operator must run `task k3d:heal` periodically to keep ArgoCD bearer token fresh. The architectural fix is a watcher on the ClusterCredential reconcile path that mirrors token changes to dependent ArgoCD secrets.
3. **Orphaned LoopZeroApplication CRs.** `kubectl get loopzeroapplications` shows 11 entries stuck in `phase=Progressing` since 2026-04-29 (3+ days). The GitOps repo `tn-e2etest-gitops` has commits like "Remove solo-server from production" — apps were DELETED from manifests but CR records remain. ArgoCD reports `Synced/Healthy` (sync of empty source = "nothing to do"), so the application controller never marks them `Active`. Need either (a) a reaper that detects manifest-absence and finalizes-out the CR, or (b) explicit cleanup.
4. **ArgoCD-managed apps OutOfSync at scale.** 15+ ArgoCD apps in OutOfSync across mgmt + build clusters. Confirmed examples: `mgmt-argocd` blocks with "duplicate entries for key ARGOCD_REPO_SERVER_DISABLE_TLS" (Helm values drift), `mgmt-argocd` reports 80 orphaned resources, `build-loki + build-promtail + mgmt-minio` show health=Missing (resources never created). Some are config drift, some are missing CRs, some are likely chart upgrades pending. Each needs root-cause classification.
5. **Release pipeline broken.** PG `releases` table shows: 70 CANCELLED + 8 GIT_MIRROR_FAILED + 3 SYNC_FAILED + 3 SUPERSEDED + **0 ACTIVE**. The most recent successful release was 4 days ago. Latest GIT_MIRROR_FAILED examples: `rel-c8baf8f27108309d` (repotest, 2026-04-29 06:02). The build pipeline can produce signed images (cosign LIVE-validated 2026-04-30) but the gitops mirror step fails downstream. Without ACTIVE releases, no app actually serves HTTP — explaining why the existing `tn-e2etest` namespaces were empty even before cell-2 was activated.
6. **Cell-2 ADR-compliant soaks pending.** Stage 1 + Stage 2 SMOKE were validated end-to-end in iter 3 with compressed durations (30s + 15s). The ADR mandates 1h + 24h windows for full readiness signal. Without those, production stages 4-5 (Sprint 9) cannot open per ADR D6.

**Evidence concrete:**

```
ArgoCD apps OutOfSync (sample):
  build-argo-workflows=OutOfSync|Healthy
  build-loki=OutOfSync|Missing
  build-promtail=OutOfSync|Missing
  mgmt-argocd=Unknown|Healthy (duplicate env var, 80 orphans)
  mgmt-gitea=OutOfSync|Healthy
  mgmt-harbor=OutOfSync|Progressing
  mgmt-minio=OutOfSync|Missing
  mgmt-ory-hydra=OutOfSync|Healthy
  ... 15+ total
```

```
LoopZeroApplications stuck (sample):
  tn-e2etest-secret-app-api-production-bb429d02     2026-04-29  Progressing
  tn-e2etest-solo-server-api-production-7fb31fba    2026-04-29  Progressing
  tn-e2etest-versioned-app-api-production-f48ec494  2026-04-29  Progressing
  ... 11 total, oldest 3.5 days
```

```
Operator tenant/ package r.RuntimeClient occurrences (untouched):
  environment_controller.go: 3
  environment_quota.go: 3
  environment_rbac.go: 8
  environment_network.go: 3
  Total: 17 call sites
```

Without addressing all six classes, the system is "cell-2 routes correctly but no tenant ever deploys" — technically multi-cell-functional, practically idle.

## Objective

**Done = every LoopZero platform layer reports GREEN persistently:** operator reconcile loops emit no error logs, ArgoCD shows 0 OutOfSync apps in mgmt+build+runtime cells, release pipeline successfully ships an ACTIVE release end-to-end on cell-2, all 11 stale LoopZeroApps are either Active or cleaned, ClusterCredential token rotation is automatic (H15 retired), full ADR-compliant 1h+24h soaks documented in runbook, and `cell2-activation-gates.sh --json` returns `all_pass:true` continuously for 24h+.

Specific, measurable goals:

1. `operators/internal/tenant/` package mirrors the cell-aware factory fix from `controller/`; vitest+envtest GREEN.
2. ClusterCredential reconcile loop propagates SA token rotation to `<cell>-argocd-cluster` Secret automatically; H15 becomes a dormant safety net, not a recurring requirement.
3. `kubectl get loopzeroapplications` shows ZERO orphaned `Progressing` entries older than 1h.
4. `kubectl get applications -A` in argocd shows ZERO OutOfSync apps across mgmt + build + runtime clusters.
5. PG `releases` table has at least 1 ACTIVE release for tn-e2etest pinned to cell-2; corresponding workload has Pods Running on loopzero-runtime-2.
6. Cell-2 ADR-compliant Stage 1 (1h) + Stage 2 (24h) executed end-to-end with closure sections in `multi-cell-canary.md`.
7. Maturity assessment 3.8 → 4.0 with all path-to-4.0 row items closed.
8. Regression matrix grows by ≥6 entries (one per phase) covering each class with permanent protection.

## ADRs

### D1 — Mirror cell-aware factory fix to `tenant/` package WITHOUT divergence

**Decision.** Apply the same `RuntimeClientFactory` interface + `runtimeClientForEnv()` pattern from `operators/internal/controller/` to `operators/internal/tenant/`. The factory IMPLEMENTATION lives in `controller/` (already there); the `tenant/` package imports it. NO duplicate factory.

**Rationale.** Code duplication between the two packages is already a known systemic pattern (cell-2-activation-plan iter 1 found `controller/environment_quota.go` and `tenant/environment_quota.go` differ). Forcing both to use the same factory implementation prevents drift. Since the factory has zero coupling to the rest of `controller/`, it can be referenced cross-package.

**Alternatives rejected.**
- (A) Duplicate the factory. Rejected: ensures eventual divergence — every fix needs to land twice.
- (B) Rewrite `tenant/` to import everything from `controller/`. Rejected: too large a refactor; out of scope.

**Consequences.** Enables: split-binary build (`cmd/tenant-operator/main.go`) gets the fix for free. Constrains: `controller/` becomes a soft de-facto base package; future `tenant/`-specific helpers must be careful not to break `controller/` imports.

### D2 — Replace H15 workaround with controller-managed token sync (NOT delete H15)

**Decision.** Add a SecretEvent watcher to the `ClusterCredentialReconciler` that triggers a re-sync of the dependent `<cell>-argocd-cluster` Secret whenever the `<cell>-kubeconfig` Secret in `loopzero-system` changes. Keep H15 in heal script as a safety net (operator-runnable manual fallback) but expect it to be a no-op in steady state.

**Rationale.** The architectural fix is a controller capability (event-driven), not a polling script (heal). H15 retired = brittle behavior creeps back. H15 as safety net = operator can recover from controller bugs without code changes. Both approaches coexist.

**Alternatives rejected.**
- (A) Delete H15 entirely after controller fix. Rejected: leaves operators without recovery if controller breaks.
- (B) Periodic resync from API instead of watcher. Rejected: 60s reconcile + ArgoCD cache invalidation can race; event-driven is cleaner.

**Consequences.** Enables: zero manual intervention for token rotation. Constrains: ClusterCredential controller now watches a Secret it doesn't own — must be careful with RBAC scope (read-only on loopzero-system Secrets).

### D3 — Auto-reap orphaned LoopZeroApplication CRs after grace period

**Decision.** Add a reconcile path in the application controller: if a LoopZeroApplication has `status.phase=Progressing` for > 1h AND the GitOps source path's most recent commit is "Remove" for this app's manifest, transition phase → `Cleaned` and remove finalizer. Operator-driven, no human intervention.

**Rationale.** Stale CRs pollute every list/audit query. The current "Progressing" indicates a real lifecycle state — but only valid for ~1h. After that it's drift. A reaper closes the loop without requiring operator GC scripts.

**Alternatives rejected.**
- (A) Manual cleanup script. Rejected: tribal knowledge; new orphans appear with each unrelated rollout.
- (B) Hard-delete CRs. Rejected: no audit trail of what was running. `phase=Cleaned` keeps the record.

**Consequences.** Enables: list/audit queries return only meaningful CRs. Constrains: introduces new state machine state; need to ensure no consumer (CLI, audit log, dashboard) treats `Cleaned` as Active.

### D4 — Categorize ArgoCD OutOfSync apps before fixing — three classes only

**Decision.** Triage every OutOfSync app into exactly one of three classes:
- **Class A (config drift)**: Helm values differ from live state — fix is `helmfile sync` or `helm upgrade`.
- **Class B (missing resources)**: ArgoCD shows Health=Missing — chart never installed properly; fix is targeted `helmfile apply` for that release.
- **Class C (orphaned tracking)**: ArgoCD has CR but resources don't exist OR resources changed labels. Fix is delete + re-create application.

Triage produces 1 markdown table; phase 3 fixes apps batch by class.

**Rationale.** "Fix all OutOfSync" without classification produces ad-hoc fixes that mask systemic issues. The three classes have different fix patterns. Triage before fixing keeps the work auditable.

**Alternatives rejected.**
- (A) Fix one-by-one without classification. Rejected: doesn't reveal patterns; allows recurrence.

**Consequences.** Enables: targeted fixes per class; future drift is easier to diagnose. Constrains: requires upfront triage (1-2 hours).

### D5 — Release pipeline diagnostics first; do NOT mass-retry

**Decision.** Diagnose ONE GIT_MIRROR_FAILED + ONE SYNC_FAILED case end-to-end with verbose logs BEFORE attempting any fix. Document root cause. Then implement targeted fix that addresses the underlying issue, NOT a "retry stuck releases" hack.

**Rationale.** 70 CANCELLED + 8 GIT_MIRROR_FAILED + 3 SYNC_FAILED is a SYSTEMIC failure, not transient. Mass-retry without root cause produces 8 more GIT_MIRROR_FAILED. Need to understand the gitops-push step failing.

**Alternatives rejected.**
- (A) Mass-retry all stuck releases. Rejected: would amplify the underlying failure.
- (B) Hard-delete release records. Rejected: loses audit trail; doesn't fix forward.

**Consequences.** Enables: durable fix that prevents future failures. Constrains: requires investigation time before any code change.

### D6 — Cell-2 ADR-compliant soaks run via CI/cron, NOT operator wall-clock

**Decision.** Wrap `cell2-canary-stage1.sh` + `cell2-canary-stage2.sh` as GitHub Actions cron jobs (`.github/workflows/cell2-soaks.yml`) that run once per week with full 1h + 24h durations. Operator just merges the PR enabling the workflow. No human waits 25 hours.

**Rationale.** ADR mandates the soak DURATIONS, not "operator must sit at terminal for 25h". Automating via CI cron satisfies ADR while respecting human time. CI failures page on-call.

**Alternatives rejected.**
- (A) Operator runs full soak manually. Rejected: blocks human for 25h; doesn't scale to multi-cell.
- (B) Skip full soaks, accept smoke. Rejected: ADR doesn't permit; production canary readiness gate.

**Consequences.** Enables: ADR satisfaction without operator burnout. Constrains: introduces CI cost (1h+24h compute weekly); requires CI infrastructure (k3d-in-CI or DOKS canary cluster).

### D7 — Maturity bump 3.8 → 4.0 only when ALL phases GREEN for 7 days

**Decision.** Maturity 4.0 is gated on this plan's Global DoD AND a 7-day continuous GREEN window (no regression). The bump is documented after the 7d soak, not when phases complete.

**Rationale.** Maturity scores reflect SUSTAINED state, not a snapshot. A "GREEN at PR merge but FAIL 2 days later" deserves 3.8 not 4.0. The 7-day window is empirical confirmation.

**Alternatives rejected.**
- (A) Bump immediately on phase complete. Rejected: history shows drift creeps in.

**Consequences.** Enables: trustworthy maturity score. Constrains: 7d delay between done-coding and done-asserting.

## Dependency Graph

```
Phase 0 (triage)
   │
   ├──▶ Phase 1 (operator fixes)         ──┐
   │                                       │
   ├──▶ Phase 2 (CR cleanup)               │
   │       │                               │
   │       ▼                               │
   │   Phase 3 (ArgoCD drift) ──┐          │
   │                            │          │
   │   Phase 4 (release pipe) ──┤          │
   │                            ▼          ▼
   │                          Phase 5 (full soaks)
   │                                       │
   │                                       ▼
   └────────────────────────────────▶ Phase 6 (closure)
```

**Sequential blockers:** Phase 0 → all others. Phase 5 requires Phases 1-4 GREEN. Phase 6 requires 7-day soak from Phase 5.

**Parallelizable:** Phases 1, 2, 3, 4 can run in parallel after Phase 0 — they touch independent code paths. Phase 1 and Phase 2 have a soft ordering (Phase 1 fix prevents new orphans during Phase 2 cleanup) but not strict.

---

## Phase 0: Triage & Confirm Scope

**Objective:** Categorize every OutOfSync app, every stale LoopZeroApp, and every failed release into actionable buckets so subsequent phases have no ambiguity.

### T0.1 — Triage all ArgoCD OutOfSync apps into 3 classes (D4)

#### Objective
Produce a categorized table of every non-Synced ArgoCD application across mgmt + build + runtime clusters.

#### Evidence
`kubectl get applications -A` shows 15+ OutOfSync apps; some Health=Missing, some Healthy. Without classification, fixes will be ad-hoc and mask root causes.

#### Files to edit
```
docs/audits/argocd-drift-2026-05-02.md — (NEW) triage table
infra/tests/parity/audit-argocd-drift.sh — (NEW) script to regenerate the triage on demand
```

#### Deep file dependency analysis
- **`docs/audits/argocd-drift-2026-05-02.md`** — captures snapshot at this date. Future iterations will produce dated companion audits.
- **`audit-argocd-drift.sh`** — wraps `kubectl get applications` + JSON jq parsing + classification logic. Outputs Markdown table. Re-runnable to detect regressions in Phase 6.

#### Deep Dives

**Classification logic.** For each app:
1. If `status.health.status == "Missing"` → Class B (missing resources).
2. Elif `status.conditions[*].message` contains "duplicate" OR "drift" → Class A (config drift).
3. Elif `status.conditions[*].type == "OrphanedResourceWarning"` → Class C (orphaned tracking).
4. Else → Class A default.

**Output schema:**
```markdown
| App | Cluster | Sync | Health | Class | Root Cause Hypothesis | Fix Phase |
|-----|---------|------|--------|-------|----------------------|-----------|
| build-loki | build | OutOfSync | Missing | B | Helm release never installed | T3.2 |
| mgmt-argocd | mgmt | Unknown | Healthy | A | Duplicate env var ARGOCD_REPO_SERVER_DISABLE_TLS | T3.1 |
```

**Bash quirks.** `kubectl get applications -A -o json` is verbose; use `jq` to extract relevant fields per app.

#### Tasks
1. Create `infra/tests/parity/audit-argocd-drift.sh` (~100 LOC).
2. Run it; capture output to `docs/audits/argocd-drift-2026-05-02.md`.
3. Manually annotate root-cause hypothesis per row.
4. Cross-reference each row to a Phase 3 task ID (T3.1 / T3.2 / T3.3).

#### TDD

```
RED:    test_audit_script_executable() — assert -x
RED:    test_audit_script_emits_table_header() — grep "| App | Cluster |" in stdout
RED:    test_audit_script_classifies_missing_as_B() — mock kubectl returning Health=Missing; assert row "Class | B"
RED:    test_audit_script_handles_zero_outofsync() — empty input → "0 OutOfSync apps detected" footer
GREEN:  Implement script with classification logic
REFACTOR: None expected
VERIFY: bash infra/tests/parity/audit-argocd-drift.sh > /tmp/triage.md && grep -c "Class" /tmp/triage.md
```

#### Acceptance Criteria
- [ ] Script `audit-argocd-drift.sh` produces a Markdown table with ≥1 row per OutOfSync app.
- [ ] Each row classified A/B/C.
- [ ] Each row has a Fix Phase pointer (T3.1 / T3.2 / T3.3).
- [ ] Script is idempotent + re-runnable.
- [ ] Pass: shellcheck (zero warnings).
- [ ] Pass: code-audit complexity check (≤ 10 cyclomatic per function).

#### DoD
- [ ] Audit script committed
- [ ] `docs/audits/argocd-drift-2026-05-02.md` generated and committed
- [ ] Every Phase 3 task references a row from the audit

### T0.2 — Audit 11 stuck LoopZeroApplications (orphan classification)

#### Objective
For each LoopZeroApp stuck in Progressing > 1h, determine whether GitOps source still has the manifest.

#### Evidence
`kubectl get loopzeroapplications` lists 11 entries; oldest is 2026-04-29 (3.5 days ago). Per `tn-e2etest-gitops` git log, recent commits include "Remove solo-server from production" — confirming some apps were intentionally deleted.

#### Files to edit
```
docs/audits/stale-loopzeroapps-2026-05-02.md — (NEW) classification table
infra/tests/parity/audit-stale-loopzeroapps.sh — (NEW) script: clones gitops repo + grep manifests + cross-reference CRs
```

#### Deep file dependency analysis
- **`audit-stale-loopzeroapps.sh`** — for each LoopZeroApp CR, derives the GitOps repo + path, clones temporarily, greps for the app's manifest, classifies result.
- Output: `Class A — manifest still in source` (real bug, app should be Active), `Class B — manifest removed from source` (orphan, can be cleaned).

#### Deep Dives

**Repo derivation.** LoopZeroApp.Spec.Tenant + Project produces gitops repo path: `loopzero-platform/{tenant}-gitops.git`. Manifest path: `production/{appName}.yaml` (or staging/, preview-N/).

**Reaper trigger condition.** A CR is orphan if BOTH:
1. `phase=Progressing` for > 1h
2. Manifest absent in latest commit of gitops repo

**Edge cases.** Tenant with no gitops repo (CR predates gitops binding) → cannot classify; mark Class C "ambiguous".

#### Tasks
1. Write `audit-stale-loopzeroapps.sh` (clones temp repo, greps manifests).
2. Run + classify all 11 entries.
3. Append rows to `docs/audits/stale-loopzeroapps-2026-05-02.md`.
4. Cross-reference to Phase 2 task ID (T2.1 cleanup OR T2.2 reaper trigger).

#### TDD

```
RED:    test_audit_handles_zero_loopzeroapps() — empty input → exit 0
RED:    test_audit_classifies_present_manifest_as_A()
RED:    test_audit_classifies_missing_manifest_as_B()
RED:    test_audit_classifies_no_gitops_as_C()
GREEN:  Implement
REFACTOR: None
VERIFY: bash infra/tests/parity/audit-stale-loopzeroapps.sh
```

#### Acceptance Criteria
- [ ] Audit table classifies all 11 stuck LoopZeroApps.
- [ ] Each row has a Phase 2 task pointer.
- [ ] Pass: shellcheck.

#### DoD
- [ ] Audit committed
- [ ] Phase 2 tasks reference rows

### T0.3 — Audit failed releases for root-cause patterns (D5)

#### Objective
Examine the 8 GIT_MIRROR_FAILED + 3 SYNC_FAILED releases for shared error signature.

#### Evidence
PG: 70 CANCELLED + 8 GIT_MIRROR_FAILED + 3 SYNC_FAILED + 0 ACTIVE for tn-e2etest. Most recent failures around 2026-04-29.

#### Files to edit
```
docs/audits/failed-releases-2026-05-02.md — (NEW) error signature table
infra/tests/parity/audit-failed-releases.sh — (NEW) script: query releases + correlate logs
```

#### Deep file dependency analysis
- Script: `psql -tAc "SELECT id, project_id, status, error_message FROM releases WHERE status IN ('GIT_MIRROR_FAILED', 'SYNC_FAILED') ORDER BY created_at DESC"`. Then for each release ID, fetch operator + API logs around that timestamp.
- Output: error signature → count + sample release IDs.

#### Deep Dives

**Error signature extraction.** Use awk to dedupe lines, hash error patterns, output top-5 recurring messages.

**Correlation window.** ±5 min around release.created_at — capture all logs from loopzero-api + tenant-controller + argocd-application-controller.

#### Tasks
1. Implement audit-failed-releases.sh.
2. Run + capture top error signatures.
3. Document in `failed-releases-2026-05-02.md`.
4. Cross-reference to Phase 4 tasks.

#### TDD

```
RED:    test_audit_filters_status_correctly()
RED:    test_audit_extracts_error_message_field()
RED:    test_audit_groups_by_signature()
GREEN:  Implement
REFACTOR: None
```

#### Acceptance Criteria
- [ ] Audit lists every failed release with timestamp + error signature.
- [ ] Top-3 signatures identified with counts.

#### DoD
- [ ] Audit committed
- [ ] Phase 4 tasks reference signatures

---

## Phase 1: Operator-Side Architectural Fixes (parallel with 2/3/4)

**Objective:** Close the operator code paths that allow drift to recur.

### T1.1 — Mirror cell-aware factory to `tenant/` package (D1)

#### Objective
The split-binary `tenant-operator` (`cmd/tenant-operator/main.go`) wires `tenant.SetupEnvironmentController` which uses `r.RuntimeClient` directly. Fix it to use the same factory pattern as `controller/`.

#### Evidence
17 `r.RuntimeClient` call sites in `operators/internal/tenant/environment_*.go`. The fix landed only in `operators/internal/controller/`.

#### Files to edit
```
operators/internal/tenant/environment_controller.go — add runtimeClientForEnv method, RuntimeFactory field, swap call sites
operators/internal/tenant/environment_quota.go — swap r.RuntimeClient calls (3 sites)
operators/internal/tenant/environment_rbac.go — swap r.RuntimeClient calls (8 sites)
operators/internal/tenant/environment_network.go — swap r.RuntimeClient calls (3 sites)
operators/cmd/tenant-operator/main.go — wire factory at startup
operators/internal/tenant/runtime_client_factory_test.go — (NEW) parity tests with controller/
```

#### Deep file dependency analysis
- **`tenant/environment_controller.go`** — this is the parallel of `controller/environment_controller.go`. Currently has the SAME 3 RuntimeClient call sites. Add same `runtimeClientForEnv` method (can import the factory interface from `controller/` package — Go cross-package import is fine since interfaces have no implementation coupling).
- **`tenant/environment_quota.go`** — 3 call sites in reconcileNamespace + reconcileQuota.
- **`tenant/environment_rbac.go`** — 8 call sites.
- **`tenant/environment_network.go`** — 3 call sites in reconcileEnvDenyAll/AllowIngress/AllowEgress.
- **`cmd/tenant-operator/main.go`** — wire `controller.NewCellAwareRuntimeClientFactory(...)` at startup.

#### Deep Dives

**Cross-package factory.** `tenant/` imports `controller.RuntimeClientFactory` interface. `controller.CellAwareRuntimeClientFactory` is the implementation. No code duplication.

**Backward compat.** Same pattern as iter 4: nil factory = legacy single-cell behavior. Existing envtest specs continue to pass.

**Subtle bug from iter 4.** `runtimeClientForEnv()` returns the legacy client if tenant CR fetch fails. This is the intended fallback (preserves liveness during transient mgmt drift).

#### Tasks
1. Read current `tenant/environment_controller.go` for delta vs `controller/environment_controller.go`.
2. Apply identical `runtimeClientForEnv` method.
3. Replace `r.RuntimeClient.X(...)` with resolved `runtimeClient.X(...)` in all 17 sites.
4. Add factory field + setter.
5. Wire in `cmd/tenant-operator/main.go`.
6. Write 7 parity tests mirroring `controller/runtime_client_factory_test.go`.

#### TDD

```
RED:    test_tenant_environment_controller_field_RuntimeFactory_exists()
RED:    test_tenant_environment_controller_runtimeClientForEnv_method_exists()
RED:    test_tenant_setup_with_factory_wires_correctly()
RED:    test_tenant_environment_quota_uses_resolved_client()
RED:    test_tenant_environment_rbac_uses_resolved_client()
RED:    test_tenant_environment_network_uses_resolved_client()
RED:    test_tenant_main_imports_NewCellAwareRuntimeClientFactory()
GREEN:  Apply edits
REFACTOR: Consider extracting runtimeClientForEnv to a shared file imported by both packages. Defer — could break envtest scoping.
VERIFY: cd operators && go test -count=1 ./internal/tenant/... && go vet ./...
```

#### Acceptance Criteria
- [ ] Zero `r.RuntimeClient.` direct calls in tenant/ environment_*.go (grep returns 0).
- [ ] go test ./internal/tenant/... GREEN with -race.
- [ ] go vet zero warnings.
- [ ] tenant-operator binary builds (CGO_ENABLED=0).
- [ ] Pass: code-audit complexity (≤ 10).

#### DoD
- [ ] All 17 call sites migrated
- [ ] 7 parity tests GREEN
- [ ] Build OK
- [ ] CHANGELOG entry

### T1.2 — ClusterCredential controller propagates token to ArgoCD secret (D2)

#### Objective
ClusterCredential reconcile loop watches `<cell>-kubeconfig` Secret in loopzero-system; on change, mirrors token to `<cell>-argocd-cluster` Secret in argocd. H15 becomes safety net.

#### Evidence
H15 in heal script is the workaround. Without it, ArgoCD bearer tokens expire daily and cell-2 sync stalls. Architectural fix: event-driven controller behavior.

#### Files to edit
```
operators/internal/controller/clustercredential_controller.go — add Secret watcher + token sync logic
operators/internal/controller/clustercredential_controller_test.go — add envtest spec for token propagation
operators/config/rbac/role.yaml — add Secret get/list/watch + patch on argocd ns
infra/scripts/k3d-cross-cluster-heal.sh — H15 becomes optional, log "controller handles token sync — H15 inactive in steady state"
```

#### Deep file dependency analysis
- **`clustercredential_controller.go`** — currently only watches ClusterCredential CR. Add `Watches(&source.Kind{Type: &corev1.Secret{}}, ...)` for Secrets in loopzero-system namespace, filtered to those matching `<cell>-kubeconfig` pattern.
- On Secret change → trigger reconcile of corresponding CC → existing reconcile logic builds new ArgoCD config + patches Secret in argocd ns.
- **RBAC**: controller currently has read on loopzero-system Secrets. Need WRITE on argocd ns Secrets.
- **`heal-script.sh` H15** — keep code, but document that it's a no-op when controller is healthy.

#### Deep Dives

**Watcher scope.** Watch ALL Secrets in loopzero-system would be wasteful. Use field selector: `metadata.name endswith "-kubeconfig"`. controller-runtime supports `client.MatchingFields`.

**Token diff detection.** On reconcile, compare new bearer token (from Secret) vs current ArgoCD secret bearer token. If different → patch.

**RBAC scope.** Add `secrets.get/list/watch` on loopzero-system + `secrets.patch` on argocd-system to ClusterCredential ClusterRole.

**Fallback path (H15 retired).** H15 keeps its code but its README comment changes: "fallback when controller down".

#### Tasks
1. Add watcher to Setup function with field selector.
2. Implement reconcile logic to read source Secret + diff against destination + patch.
3. Update RBAC role.yaml.
4. Write envtest spec: create CC + create kubeconfig Secret with token T1 + assert ArgoCD secret has T1; rotate Secret to T2 + assert ArgoCD secret has T2.
5. Update H15 docstring in heal-script.

#### TDD

```
RED:    test_clustercredential_controller_watches_kubeconfig_secrets()
RED:    test_token_change_triggers_argocd_secret_update()
RED:    test_no_change_no_op()
RED:    test_RBAC_has_secrets_patch_on_argocd_ns()
GREEN:  Implement watcher + reconcile diff
REFACTOR: Extract diff comparison into helper if grows
VERIFY: cd operators && go test -count=1 ./internal/controller/clustercredential...
```

#### Acceptance Criteria
- [ ] envtest spec passes for create + rotate + no-op cases.
- [ ] LIVE: `kubectl rollout restart deploy/tenant-controller && task k3d:heal && wait 60s && check H15 reports SKIP` (controller already synced).
- [ ] Pass: RBAC manifest validates with kubeconform.
- [ ] Pass: code-audit complexity.

#### DoD
- [ ] envtest GREEN
- [ ] H15 documented as fallback
- [ ] CHANGELOG entry
- [ ] Regression matrix entry 160

---

## Phase 2: Stale CR Cleanup (parallel with 1/3/4)

**Objective:** Remove orphan LoopZeroApps that no longer have GitOps manifests.

### T2.1 — Manual cleanup of 11 stale LoopZeroApps based on T0.2 audit

#### Objective
For each Class B (manifest absent) LoopZeroApp from T0.2, finalize-out the CR.

#### Evidence
T0.2 audit lists exact CRs.

#### Files to edit
```
docs/runbooks/stale-loopzeroapp-cleanup.md — (NEW) procedure
(no production code edits — operator action documented)
```

#### Deep Dives

**Cleanup command.** `kubectl patch loopzeroapplication <name> -p '{"metadata":{"finalizers":[]}}' --type=merge` then `kubectl delete loopzeroapplication <name>`.

**Audit log.** Each cleanup logs an entry in PG audit_log with reason "stale_orphan_post_gitops_remove".

#### Tasks
1. For each Class B row in T0.2: kubectl patch + kubectl delete.
2. Verify `kubectl get loopzeroapplications` returns 0 stale.
3. Append cleanup log to runbook.

#### TDD
N/A (operational task; T2.2 reaper has TDD).

#### Acceptance Criteria
- [ ] 11 → 0 stale LoopZeroApps in `Progressing > 1h`.
- [ ] Audit log has entries.

#### DoD
- [ ] Cleanup performed
- [ ] Runbook documents procedure

### T2.2 — Reaper controller logic for orphan LoopZeroApps (D3)

#### Objective
Add automatic cleanup so future orphans are reaped without operator intervention.

#### Evidence
Manual cleanup (T2.1) is one-time. Reaper prevents recurrence.

#### Files to edit
```
operators/internal/controller/application_reaper.go — (NEW) reaper logic
operators/internal/controller/application_controller.go — wire reaper + state transition Progressing → Cleaned
operators/api/v1/application_types.go — add ApplicationPhaseCleaned constant
operators/internal/controller/application_reaper_test.go — (NEW) envtest specs
```

#### Deep file dependency analysis
- **`application_reaper.go`** — for each Application CR, if `phase=Progressing` AND `creationTimestamp + 1h < now()` AND gitops source path's HEAD commit shows the manifest absent → transition to `Cleaned` + remove finalizer.
- **gitops source check** — use existing gitops client (operators/internal/gitops/client.go).
- **Phase semantics** — `Cleaned` means "GitOps source removed manifest; CR record retained for audit but no longer reconciled".

#### Deep Dives

**Grace period.** 1h is conservative. Buys time for normal Progressing → Active transitions. Configurable via env var LOOPZERO_REAPER_GRACE_PERIOD_SECONDS.

**Race condition.** If GitOps push lands AFTER reaper checks: transition is reverted next reconcile. Reaper checks again 1h later; if manifest still absent, reaps again. Idempotent.

**State machine update.** Add `Cleaned` to Application phase enum. Transitions: Progressing → Cleaned via ApplicationOrphaned event.

#### Tasks
1. Add `ApplicationPhaseCleaned = "Cleaned"` constant.
2. Update statemachine/applicationmachine.go to allow Progressing → Cleaned transition.
3. Implement application_reaper.go with gitops source check.
4. Write envtest specs: 1h+ Progressing + manifest absent → Cleaned; 1h+ Progressing + manifest present → no transition.

#### TDD

```
RED:    test_reaper_skips_recently_created_apps()
RED:    test_reaper_skips_apps_with_manifest_present()
RED:    test_reaper_transitions_orphan_to_cleaned()
RED:    test_reaper_idempotent_on_already_cleaned()
GREEN:  Implement reaper + state machine update
REFACTOR: Extract gitops source check into shared helper
VERIFY: cd operators && go test ./internal/controller/...
```

#### Acceptance Criteria
- [ ] 4 envtest specs PASS.
- [ ] state machine allows Progressing → Cleaned.
- [ ] Reaper runs every 5 min (configurable).
- [ ] Pass: code-audit complexity.

#### DoD
- [ ] Reaper deployed
- [ ] envtest GREEN
- [ ] Regression matrix entry 161

### T2.3 — Cancel stale releases (PG hygiene)

#### Objective
For each release older than 7 days with status=CANCELLED OR GIT_MIRROR_FAILED OR SYNC_FAILED with no recent reconcile, mark as `ARCHIVED` (new terminal state).

#### Evidence
70 CANCELLED records polluting queries.

#### Files to edit
```
api/internal/db/migrations/000008_archive_stale_releases.up.sql — (NEW) UPDATE with WHERE clause + status enum extension
api/internal/db/store_release.go — handle ARCHIVED in queries (skip from active lists)
api/tests/unit/migration_008_archive_stale_releases_test.go — (NEW) text-level tests
```

#### Deep Dives

**Idempotent UPDATE.** `UPDATE releases SET status='ARCHIVED' WHERE status IN ('CANCELLED','GIT_MIRROR_FAILED','SYNC_FAILED') AND created_at < now() - interval '7 days'`.

**Audit trail.** Append to release.notes the archival timestamp + reason.

**Forward-only.** No DOWN migration (CLAUDE.md Rule).

#### Tasks
1. Add `ARCHIVED` to release status state machine.
2. Write migration 000008.
3. Write 5 text-level tests (similar to migration_007 pattern).
4. Apply on dev PG.

#### TDD

```
RED:    test_migration_008_targets_three_statuses()
RED:    test_migration_008_filters_by_age()
RED:    test_migration_008_idempotent()
RED:    test_migration_008_no_destructive_ops()
RED:    test_archived_status_excluded_from_active_queries()
GREEN:  Implement
VERIFY: go test ./api/tests/unit/...
```

#### Acceptance Criteria
- [ ] After migration: 0 records in CANCELLED/GIT_MIRROR_FAILED/SYNC_FAILED status > 7 days old.
- [ ] Active list queries exclude ARCHIVED.
- [ ] Tests PASS.

#### DoD
- [ ] Migration applied
- [ ] Tests GREEN

---

## Phase 3: ArgoCD Drift Fixes (parallel with 1/2/4)

**Objective:** Fix every OutOfSync app per T0.1 classification.

### T3.1 — Class A fixes (config drift)

#### Objective
For each Class A app from T0.1, restore Helm values consistency.

#### Evidence
T0.1 lists Class A apps. Sample: mgmt-argocd has duplicate ARGOCD_REPO_SERVER_DISABLE_TLS env var.

#### Files to edit
```
infra/helm/<chart>/values-*.yaml — varies per app per T0.1
infra/helmfile/mgmt.helmfile.yaml.gotmpl — re-sync directives
```

#### Deep Dives

**Per-app deep dive.** Each Class A row in T0.1 needs a 1-paragraph fix description. For mgmt-argocd: dedupe env var entries in argocd values.yaml.

#### Tasks
1. For each Class A app: identify drifted Helm value, fix in chart values.yaml.
2. `helmfile sync` to apply.
3. Verify ArgoCD app → Synced.

#### TDD

```
RED:    test_argocd_values_no_duplicate_env_vars()
RED:    test_argocd_repo_server_envs_unique()
GREEN:  Fix values
VERIFY: helm template + grep for duplicate keys
```

#### Acceptance Criteria
- [ ] All Class A apps Synced.
- [ ] No duplicate env vars in any rendered chart.

#### DoD
- [ ] Helm values fixed
- [ ] kubeconform validates
- [ ] T0.1 audit re-run shows 0 Class A

### T3.2 — Class B fixes (missing resources)

#### Objective
For each Class B app (Health=Missing), bootstrap the missing release.

#### Evidence
build-loki, build-promtail, mgmt-minio show Health=Missing — release never properly installed.

#### Files to edit
```
infra/helmfile/build.helmfile.yaml.gotmpl — ensure loki + promtail releases enabled
infra/helmfile/mgmt.helmfile.yaml.gotmpl — ensure minio release enabled
```

#### Deep Dives

**Why Missing?** Likely chart was disabled in helmfile after dogfood/development; ArgoCD app still exists but no chart sync. Re-enable + sync.

#### Tasks
1. For each Class B app: check helmfile env config, enable if disabled.
2. Run `helmfile -e <env> apply --selector name=<chart>`.
3. Re-check ArgoCD → Synced + Healthy.

#### Acceptance Criteria
- [ ] All Class B apps Healthy + Synced.

#### DoD
- [ ] Helmfile entries enabled
- [ ] Charts deployed

### T3.3 — Class C fixes (orphaned tracking)

#### Objective
For each Class C app, delete the ArgoCD application + recreate.

#### Evidence
T0.1 audit Class C rows.

#### Tasks
1. `kubectl delete application <name>` for each Class C app.
2. Recreate via `helmfile sync` (which re-emits the ArgoCD app from chart).

#### Acceptance Criteria
- [ ] All Class C apps recreated + Synced.

#### DoD
- [ ] Recreated cleanly

---

## Phase 4: Release Pipeline Restoration (parallel with 1/2/3)

**Objective:** Diagnose + fix the GIT_MIRROR_FAILED + SYNC_FAILED root cause.

### T4.1 — Root-cause GIT_MIRROR_FAILED (D5)

#### Objective
Capture exact error message + reproduce locally.

#### Evidence
8 GIT_MIRROR_FAILED releases. Most recent: `rel-c8baf8f27108309d` (2026-04-29 06:02). Need logs.

#### Files to edit
```
docs/audits/git-mirror-failed-rca-2026-05-02.md — (NEW) RCA
api/internal/gitops/push.go — (likely) the failing path
```

#### Deep Dives

**Reproduce locally.** Run `loopzero deploy` against a dummy project; observe the gitops push step. Capture stderr.

**Likely causes.**
- Gitea credentials expired (similar to ArgoCD token issue)
- Git push rejected due to non-fast-forward (concurrent push)
- TLS verification failure if Gitea moved hostnames

**Fix pattern.** Add retry with backoff + fail-loud on auth errors (per CLAUDE.md Rule 8).

#### Tasks
1. Pull operator logs + API logs around `rel-c8baf8f27108309d` timestamp.
2. Identify error signature.
3. Reproduce locally.
4. Implement targeted fix (NOT mass retry).
5. Document RCA.

#### TDD

```
RED:    test_gitops_push_handles_auth_failure_gracefully()
RED:    test_gitops_push_retries_on_transient_network()
RED:    test_gitops_push_fails_loud_on_credential_error()
GREEN:  Implement fix
VERIFY: go test ./api/internal/gitops/...
```

#### Acceptance Criteria
- [ ] RCA document committed.
- [ ] Fix tested with mock repro.
- [ ] After fix: new test deploy → ACTIVE release achieved.

#### DoD
- [ ] RCA committed
- [ ] Fix landed
- [ ] LIVE: deploy → ACTIVE

### T4.2 — Root-cause SYNC_FAILED (D5)

#### Objective
Same procedure as T4.1, for the 3 SYNC_FAILED releases.

#### Files to edit
```
docs/audits/sync-failed-rca-2026-05-02.md — (NEW)
api/internal/argocd/sync_client.go — (likely) the failing path
```

#### Tasks
1. Inspect logs.
2. RCA + fix.

#### Acceptance Criteria
- [ ] After fix: deploy → sync succeeds.

#### DoD
- [ ] RCA + fix committed

### T4.3 — Validate end-to-end deploy on cell-2

#### Objective
Run a full `loopzero deploy` of a fresh tenant to cell-2 with HTTP 200 verification.

#### Evidence
None — this is the smoking gun. If T4.1+T4.2 fixes are correct, this works.

#### Files to edit
```
docs/runbooks/cell2-e2e-deploy-evidence.md — (NEW) walkthrough + screenshots/logs
```

#### Tasks
1. `loopzero init` new tenant tn-fix-all-test.
2. `loopzero admin tenants pin --tenant tn-fix-all-test --cell cell-2`.
3. `loopzero deploy` from a sample repo.
4. Wait for ACTIVE release.
5. `curl https://tn-fix-all-test-production.loopzero.local` → 200.

#### Acceptance Criteria
- [ ] PG release status=ACTIVE.
- [ ] Pods running on `loopzero-runtime-2`.
- [ ] HTTP 200 from public URL.

#### DoD
- [ ] Walkthrough committed with timestamps + log excerpts

---

## Phase 5: Full ADR-Compliant Soaks

### T5.1 — Bootstrap ≥10 dev tenants for Stage 2 sample

#### Objective
Stage 2 ADR requires 10% of ≥10 cell-1 tenants. Bootstrap 11+ tenants via dogfood loop.

#### Files to edit
```
infra/tests/parity/bootstrap-stage2-tenants.sh — (NEW) bulk dogfood loop wrapper
```

#### Tasks
1. Loop dogfood-loop.sh `--only journey-01-solo-deploy` 11 times with unique IDs.
2. Verify each tenant has ACTIVE release.

#### Acceptance Criteria
- [ ] PG: COUNT(*) FROM tenants WHERE cell_id='cell-1' >= 11.

#### DoD
- [ ] Tenants bootstrapped

### T5.2 — Cron workflow for full Stage 1 + Stage 2 (D6)

#### Objective
Wrap canary scripts as `.github/workflows/cell2-soaks.yml` with weekly cron.

#### Files to edit
```
.github/workflows/cell2-soaks.yml — (NEW) cron + matrix (stage 1 / stage 2)
infra/tests/parity/cell2-canary-stage1.sh — accept GITHUB_ACTIONS env var
```

#### Tasks
1. Write workflow with `schedule: cron: '0 2 * * 0'` (Sunday 02:00 UTC).
2. Matrix: stage1 (1h timeout) + stage2 (24h timeout).
3. Failure → page on-call (issue auto-create).

#### TDD

```
RED:    test_workflow_yaml_valid()
RED:    test_workflow_runs_canary_scripts()
RED:    test_workflow_paged_on_failure()
GREEN:  Write workflow
VERIFY: actionlint .github/workflows/cell2-soaks.yml
```

#### Acceptance Criteria
- [ ] Workflow exists + actionlint clean.
- [ ] First scheduled run completes (1h + 24h).

#### DoD
- [ ] First run GREEN
- [ ] Closure section in multi-cell-canary.md

### T5.3 — Manual full Stage 1 LIVE (1h) + Stage 2 LIVE (24h)

#### Objective
Run the full ADR durations once locally (or on staging) to capture baseline.

#### Tasks
1. Bootstrap synthetic tenant.
2. `SOAK_SECONDS=3600` cell2-canary-stage1.sh.
3. Append closure section.
4. `SOAK_SECONDS=86400 PERCENT=10` cell2-canary-stage2.sh.
5. Append closure section.

#### Acceptance Criteria
- [ ] Stage 1 GREEN, full 1h soak.
- [ ] Stage 2 GREEN, full 24h soak.

#### DoD
- [ ] Closure sections committed

---

## Phase 6: Closure & Maturity

### T6.1 — 7-day continuous GREEN window (D7)

#### Objective
Verify all 12 ADR gates + zero OutOfSync apps + zero stale LoopZeroApps for 7 consecutive days.

#### Files to edit
```
.github/workflows/platform-health-7day-watch.yml — (NEW) hourly check + dashboard
```

#### Tasks
1. Hourly cron runs cell2-activation-gates.sh + audit-argocd-drift.sh + audit-stale-loopzeroapps.sh.
2. Each run posts to a dashboard or issue tracker.
3. After 168 consecutive GREEN runs (7 days × 24h): trigger maturity bump PR.

#### Acceptance Criteria
- [ ] Workflow runs hourly.
- [ ] 168/168 GREEN over 7 days.

#### DoD
- [ ] Dashboard committed

### T6.2 — Maturity assessment 3.8 → 4.0

#### Objective
After T6.1 7-day GREEN, update maturity assessment.

#### Files to edit
```
docs/maturity-assessment.md — bump 3.8 → 4.0; close every Path-to-4.0 row
```

#### Tasks
1. Bump score.
2. Update Scale/Multi-tenancy 3.5 → 4.0.
3. Update Tests/Confidence 3.6 → 4.0.

#### Acceptance Criteria
- [ ] All Path-to-4.0 rows closed or moved to Path-to-5.0.

#### DoD
- [ ] Updated + committed

### T6.3 — Final regression matrix audit

#### Tasks
1. Run audit-regression-matrix.sh.
2. Confirm 158 → 164+ entries (one per phase).
3. 0 critical miss.

#### DoD
- [ ] PASS

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | Operator split-binary tenant/ has same RuntimeClient bug | T1.1 | Mirror cell-aware factory; 7 tests |
| 2 | ClusterCredential controller doesn't sync token to ArgoCD | T1.2 | Add Secret watcher + envtest |
| 3 | 11 stuck LoopZeroApps (manual cleanup) | T2.1 | kubectl finalize-out per row |
| 4 | Future stuck LoopZeroApps (auto-reaper) | T2.2 | Reaper controller + state machine |
| 5 | 70+ CANCELLED releases polluting queries | T2.3 | Migration 000008 ARCHIVED status |
| 6 | 15+ ArgoCD apps OutOfSync (multiple causes) | T0.1, T3.1, T3.2, T3.3 | Triage + per-class fix |
| 7 | mgmt-argocd duplicate env var | T3.1 | Class A fix |
| 8 | build-loki + build-promtail + mgmt-minio missing | T3.2 | Class B fix |
| 9 | 8 GIT_MIRROR_FAILED releases | T0.3, T4.1 | RCA + targeted fix |
| 10 | 3 SYNC_FAILED releases | T0.3, T4.2 | RCA + fix |
| 11 | Zero ACTIVE releases — pipeline broken | T4.3 | E2E deploy validation |
| 12 | Stage 1 ADR-compliant 1h soak not run | T5.2, T5.3 | CI cron + manual baseline |
| 13 | Stage 2 ADR-compliant 24h soak not run | T5.2, T5.3 | Same |
| 14 | Stage 2 needs ≥10 dev tenants | T5.1 | Bulk bootstrap |
| 15 | Maturity 3.8 not bumped post-fix | T6.1, T6.2 | 7-day GREEN window + bump |
| 16 | Regression matrix needs new entries per phase | T6.3 | Append entries 160-165+ |

**Coverage: 16/16 gaps covered (100%).**

## Global Definition of Done

- [ ] All 6 phases (and 17 tasks) completed
- [ ] All tests passing: Go (operators, api), TypeScript (heal-script vitests), envtest specs
- [ ] Zero `go vet` / `golangci-lint` / `shellcheck` / `tsc` / `actionlint` warnings introduced
- [ ] Backward compatibility preserved (single-cell deploys still work via factory=nil fallback)
- [ ] code-audit checks PASS
- [ ] `cell2-activation-gates.sh --json` returns `all_pass: true` continuously for 7 days
- [ ] `kubectl get applications -A` shows 0 OutOfSync apps
- [ ] `kubectl get loopzeroapplications` shows 0 stale Progressing > 1h
- [ ] PG `releases` table: ≥1 ACTIVE release on cell-2
- [ ] Stage 1 + Stage 2 ADR-compliant soaks executed (1h + 24h)
- [ ] Maturity assessment 3.8 → 4.0
- [ ] Regression matrix grows by ≥6 entries (160-165+)
- [ ] CHANGELOG `[Unreleased]` `Added` + `Fixed` entries for each phase
- [ ] Memory `reference_fix_all_<date>.md` written
- [ ] Tenant deploys end-to-end on cell-2 with HTTP 200 (T4.3 evidence)
