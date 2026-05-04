# ADR-0023 — Operators Split Tree Is Canonical; Monolith Deprecated

## Status

Accepted — 2026-05-04 (MEET-2026-05-04-068, async ratification pending human review)

## Context

LoopZero's operators module today carries two parallel implementations of all 8 controllers:

- **Monolith:** `operators/internal/controller/` — 14,567 LOC across 45 files (24 production + 21 tests). Deployed in production via `infra/helm/tenant-controller` Helm chart. Single binary `operators/cmd/main.go` (574 LOC) wires all 8 controllers into one process.
- **Split:** `operators/internal/{tenant,app,preview,platform}/` — 5,747 LOC across 22 files (22 production + 0 tests). 4 separate binaries (`cmd/{tenant,app,preview,platform}-operator/main.go`) and 4 prepared Helm charts in `charts/loopzero-{tenant,app,preview,platform}-operator/`.

Audit (`docs/technical/operators/improvement-roadmap.md` §S9-T1, §AP-1) confirms the split tree was added as "forward-looking preparation" per ADR-0018 §C9 (per-Kind cache scoping) but never made canonical. The trees differ only in package declaration and a tracing helper indirection — except for ClusterCredential, which has already drifted 78 lines. Drift is in progress; both trees are silently picked up depending on which file a developer edits.

`operatortest/setup.go:24` imports the monolith only. All 21 envtest specs exercise the monolith package. The split tree has zero test coverage. `cmd/registration_test.go` validates wiring for the monolith only.

## Decision

The 4-binary split tree (`operators/internal/{tenant,app,preview,platform}/` + `cmd/{tenant,app,preview,platform}-operator/main.go` + `charts/loopzero-{tenant,app,preview,platform}-operator/`) becomes the **canonical** implementation. The monolith (`operators/internal/controller/`, `operators/cmd/main.go`, `operators/Dockerfile`, `infra/helm/tenant-controller/`) is deprecated and removed via staged deletion per the conditions below.

This ADR ratifies plan `docs/plans/operators-sota-faang-grade-plan.md` Phase 1 with the following conditions integrated.

### Conditions

1. **Phase 1 ordering revision (per platform-architect C1, sre veto):** T5.4 (cache scoping congruence test) HOISTED to Phase 1 — runs between T1.3 and T1.4. Deleting monolith before proving split scopes are congruent risks regression discovered only in dogfood.
2. **Diff baseline mandatory before T1.1 (per verification-architect veto #1):** `docs/plans/operators-sota-faang-grade-plan/drift-baseline.md` committed before any test move. Per-file diff between monolith and split with explicit "mechanical / semantic-port-N-lines" classification per pair. Already-known: ClusterCredential 78 LOC.
3. **Multi-stage cutover (per sre-engineer veto):** T1.5 atomic helmfile swap REJECTED. Replaced with Stage A (coexistence with shared lease names) → Stage B (per-controller cutover, ascending blast radius) → Stage C (24h soak per cell, cell-1 first) → Stage D (delete monolith chart only after 30d `OperatorRestartRate24h` silent).
4. **Production rollout SCOPED OUT (per platform-architect C3):** This plan ends at local dogfood GREEN. Production cutover handled via separate `CHG-NNNN-OPERATOR-SPLIT-PROD` change record. Phase 1 in this plan = local k3d migration + envtest + E2E smoke.
5. **SA rename safety (per security-engineer):** Helm `extraSubjects` knob enables temporary dual-binding (`tenant-controller` + `loopzero-tenant-operator`) on cell ClusterRoleBindings during rollout window. NetworkPolicy `podSelector` widened to `app in (tenant-controller, loopzero-tenant-operator)`. Pre-flight grep audit of `tenant-controller|loopzero-controller` references mandatory.

## Rationale

### Why split tree wins

- **Per-Kind cache scoping** (ADR-0018 §C9) is more granular per-binary; monolith caches every Kind in one process.
- **Blast radius isolation:** OOM in app-operator does not impact tenant-operator's reconcile loop.
- **Smaller binaries:** ~16 MB each vs 63 MB monolith → faster pulls, smaller surface area.
- **Independent deploy cadence:** preview-operator can be released independently of tenant-operator.
- **Test pyramid clarity:** each binary has its own test suite, independently runnable.

### Why "delete the monolith" wins over "shared core package"

Alternative B (refactor both trees to consume a shared `internal/{domain}/core/` package) was rejected. It adds a third indirection layer without solving the original problem ("two binaries to test"). Source-of-truth duplication is the disease; abstraction is not the cure.

### Why "delete the split, keep monolith" was rejected

- Discards 4 prepared Helm charts (already production-quality)
- Discards cache-scoping wins (would need to re-architect monolith for per-Kind scoping)
- Discards blast-radius isolation
- Pre-release window is the legitimate moment to take this debt off the books; post-launch this becomes a multi-quarter migration

### Why staged cutover

SRE veto: an atomic `helmfile sync` switching from 1 monolith pod to 4 split pods has no rollback path mid-flight. ServiceMonitor selector mismatches would silently zero-out metrics. SA rename would silently 403 cross-cluster reconciles. Multi-stage cutover with leader-elect arbitration (Stage A) and per-controller cutover (Stage B) are the only safe paths.

## Alternatives considered

- **A: Keep monolith, delete split.** Rejected — discards prepared infrastructure + cache scoping wins. Aligns with current production but accumulates debt.
- **B: Keep both, refactor to shared core.** Rejected — adds indirection without solving duplication. Three layers of code to maintain for every controller change.
- **C (chosen): Migrate to split, delete monolith via staged cutover.** Single canonical path forward.

## Consequences

### Enables

- Single source of truth per controller — every PR edits one file, not two.
- Per-domain leader election + cache scoping → granular ops control.
- 4 independent binary lifecycles → smaller blast radius for OOMs, panics, image pull failures.
- Honest capability matrix (D5 / ADR-0027) — per-binary capability declaration becomes possible.

### Constrains

- Mid-migration period (Stage A through Stage D) requires both monolith and 4 split pods running concurrently. Resource overhead ~2× during window.
- Production cutover requires Helm chart swap coordinated via ArgoCD (separate CHG record).
- All 21 envtest specs must be migrated to split tree; any silent semantic divergence between trees becomes a Phase 1 bug.
- Tilt + Taskfile + dashboards + alerts all updated in Phase 1 PR (per dx-engineer 6 fan-out points).

### Reversibility

- During Stage A through Stage C: rollback = scale split pods to 0 + re-enable monolith reconcile. <10min MTTR target.
- After Stage D (monolith chart deleted): reversibility requires re-implementation. 30-day silence gate provides enough soak window.
- ADR itself reversible only via new MEET record + new ADR (per ADR-0022).

## Risks

- **HIGH:** ClusterCredential 78-LOC drift may indicate other silent divergences. Mitigated by drift baseline (condition #2).
- **HIGH:** Cell-2 was just activated 2026-05-03 with 12/12 ADR gates GREEN. Stage C order (cell-1 24h GREEN before cell-2) protects cell-2 stability.
- **HIGH:** ServiceMonitor selector mismatch silently zeroes operator metrics. Mitigated by enumerated 6 fan-out points (per dx-engineer condition #5).
- **MEDIUM:** SA rename breaks cell-1/cell-2 ClusterCredential consumption mid-rollout if pre-flight inventory missed any reference. Mitigated by `extraSubjects` dual-binding window.

## References

- `docs/plans/operators-sota-faang-grade-plan.md` v1.1 D1
- `docs/technical/operators/improvement-roadmap.md` §S9-T1, §AP-1
- `docs/technical/operators/sota-gates.md` OP-G21
- ADR-0018 — Tenant Namespace SoT (origin of per-Kind cache scoping)
- ADR-0022 — Plan Amendment Policy (governs this MEET record + plan version bump)
- MEET-2026-05-04-068 — convening meeting record
- CLAUDE.md `.claude/rules/pre-release-governance.md` (T3 protocol)
