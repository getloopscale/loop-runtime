# Drift Baseline — Monolith vs Split Tree (Phase 1 prerequisite)

> **Mandatory artifact per ADR-0023 condition #2 + verification-architect veto #1.** This document classifies every monolith↔split file pair as "mechanical" (safe to migrate as-is) vs "semantic divergence" (must port specific lines from monolith to split BEFORE T1.1). Without this baseline, T1.1 carries silent regressions into the canonical tree.

**Generated:** 2026-05-04 (Phase 1 prerequisite, post-Phase 0 commit `f6cbd377`)
**Method:** `diff -u operators/internal/controller/X.go operators/internal/{tenant,app,preview,platform}/X.go` per pair, manual classification of non-mechanical hunks.
**Pre-fix state:** AP-2 (`application_controller.go:269-282` hardcoded strings) and AP-3 (`application_controller.go:179-181` silent error swallow) FIXED in BOTH trees prior to baseline calculation (per ADR-0023 condition: hoist AP-3 to Phase 1).

## Pair classification

| # | Monolith file | Split target | Diff lines | Class | Action |
|---|---|---|---|---|---|
| 1 | `controller/application_controller.go` | `app/application_controller.go` | 49 | Mechanical | Move test |
| 2 | `controller/clustercredential_controller.go` | `platform/clustercredential_controller.go` | 135 | **SEMANTIC — must port 25 lines** | Port `reconcileArgoCDClusterSecret` ALWAYS-call before T1.1 |
| 3 | `controller/environment_controller.go` | `tenant/environment_controller.go` | 134 | Mostly mechanical (comments) | Verify tests, then move |
| 4 | `controller/environment_network.go` | `tenant/environment_network.go` | 46 | Mechanical | Move test |
| 5 | `controller/environment_quota.go` | `tenant/environment_quota.go` | 34 | Mechanical | Move test |
| 6 | `controller/environment_rbac.go` | `tenant/environment_rbac.go` | 71 | **SEMANTIC — must port ~15 lines** | Port pull-secret NotFound handling (switch case) |
| 7 | `controller/exposure_controller.go` | `app/exposure_controller.go` | 54 | Mechanical | Move test |
| 8 | `controller/gitrepositorybinding_controller.go` | `platform/gitrepositorybinding_controller.go` | 49 | Mechanical | Move test |
| 9 | `controller/platformbootstrap_api.go` | `platform/platformbootstrap_api.go` | 27 | Mechanical | Move test (no test exists) |
| 10 | `controller/platformbootstrap_argocd.go` | `platform/platformbootstrap_argocd.go` | 28 | Mechanical | Move test (no test exists) |
| 11 | `controller/platformbootstrap_controller.go` | `platform/platformbootstrap_controller.go` | 76 | Mechanical (em-dash vs double-dash in comments) | Move test |
| 12 | `controller/platformbootstrap_gitea.go` | `platform/platformbootstrap_gitea.go` | 36 | Mechanical | Move test (no test exists) |
| 13 | `controller/preview_controller.go` | `preview/preview_controller.go` | 99 | **SEMANTIC — must port 11 lines** | Port `IngressNamespace` field, `ingressNS()` method, `previewBuildGracePeriod` constant |
| 14 | `controller/preview_resources.go` | `preview/preview_resources.go` | 24 | Mechanical | Move test |
| 15 | `controller/registry_auth_merge.go` | `tenant/registry_auth_merge.go` | 11 | Mechanical | Move test |
| 16 | `controller/tenant_build.go` | `tenant/tenant_build.go` | 47 | Mechanical | Move test |
| 17 | `controller/tenant_controller.go` | `tenant/tenant_controller.go` | 40 | Mechanical | Move test |
| 18 | `controller/tenant_credentials.go` | `tenant/tenant_credentials.go` | 11 | Mechanical | Move test |
| 19 | `controller/tenant_deletion.go` | `tenant/tenant_deletion.go` | 11 | Mechanical | Move test |
| 20 | `controller/tenant_gitops.go` | `tenant/tenant_gitops.go` | 56 | Mechanical | Move test |
| 21 | `controller/tenant_harbor.go` | `tenant/tenant_harbor.go` | 50 | Mechanical | Move test |
| 22 | `controller/tenant_priority.go` | `tenant/tenant_priority.go` | 11 | Mechanical | Move test |

**Summary:**
- Total pairs: 22
- Mechanical-only: 19 (86%)
- Semantic divergence requiring port: **3** (#2, #6, #13)
- Total semantic LOC to port: ~51 lines (manageable in single PR)

## Required ports BEFORE T1.1 starts

### Port #1 — ClusterCredential `reconcileArgoCDClusterSecret` ALWAYS-call

**Source:** `operators/internal/controller/clustercredential_controller.go` Step 2 (after kubeconfig validation, before connectivity check).
**Target:** `operators/internal/platform/clustercredential_controller.go` (same location).
**Provenance:** `fix-all-platform-issues-plan` T1.2 (per memory `reference_cell2_activation_2026_05_02`).

**Behavior to port:**
- ALWAYS reconcile ArgoCD cluster secret to mirror kubeconfig Secret token, BEFORE connectivity check.
- Previously the early-return on connectivity=false skipped this sync, leaving the ArgoCD secret with stale token even after heal scripts (H14/H15) refreshed the kubeconfig.
- When operator action rotates the token in `cell-X-kubeconfig`, controller propagates it to `<cell>-argocd-cluster` on next reconcile (event-driven via Secret watcher), independent of destination cluster reachability.

**Code block to copy:** ~25 lines (the `// Step 2 ...` comment + the conditional `if cr.Spec.Scope.ArgoCD { ... reconcileArgoCDClusterSecret ... }` block).

### Port #2 — Environment_RBAC pull-secret NotFound handling

**Source:** `operators/internal/controller/environment_rbac.go` (the `switch err := r.ensurePullSecret(ctx, env, nsName); {` block).
**Target:** `operators/internal/tenant/environment_rbac.go` (replace simpler `if err != nil` block).
**Provenance:** F-09 (CHG-2026-04-29-DOGFOOD T4.1).

**Behavior to port:**
- Distinguish NotFound (expected in some profiles, e.g., k3d local with public registry) from real failure.
- NotFound → `TypePullSecretReady=False/SourceMissing` WITHOUT log noise (idempotent).
- Other errors → continue logging.
- Eliminates "pull secret ensure failed (non-fatal)" log noise per reconcile.

**Code block to copy:** ~15 lines (the switch statement + its case bodies).

### Port #3 — Preview Controller IngressNamespace + grace period

**Source:** `operators/internal/controller/preview_controller.go`:
- `IngressNamespace string` field on `PreviewReconciler` struct.
- `ingressNS() string` method returning `IngressNamespace` or fallback to `constants.NamespaceIngress`.
- `previewBuildGracePeriod = 90 * time.Second` constant.

**Target:** `operators/internal/preview/preview_controller.go` (add same field + method + constant).

**Provenance:** Mirrors `EnvironmentReconciler.ingressNS()` for `allow-ingress-traefik` NetworkPolicy semantics consistency between regular and preview envs.

**Code block to copy:** ~11 lines (field declaration + method body + constant).

## What this baseline does NOT cover

These are explicitly out-of-scope:

- **Test file content:** This baseline covers production code only. Test files (21 in monolith, 0 in split) are migrated 1:1 via `git mv` in T1.1 — no semantic port required because tests didn't exist in split.
- **Tracing helper indirection:** All split files use `shared.StartReconcileSpan` / `shared.RecordError` / `shared.SetSuccess` / `shared.ReconcileOutcome` instead of monolith's local helpers. This is per ADR-0023 D7 and is the desired end state — no port needed.
- **Comment polish (em-dash vs double-dash):** Cosmetic, classified as mechanical.
- **Package declaration:** `package controller` → `package <split>` is the only intended difference per file.

## Sign-off

- Date: 2026-05-04
- Author: Ralph loop iter 1 (operators-sota-faang-grade-plan)
- Reviewer requirement (per ADR-0023 condition #2): MUST be signed off by `agent:test` BEFORE T1.1 begins.
- Async ratification status: PENDING human ratification.

## Verification commands (run before T1.1)

```bash
# Compute current per-pair diff sizes (should match this baseline)
cd /home/paulo/loopzero/operators
for sf in internal/controller/*.go; do
  base=$(basename "$sf")
  if [[ "$base" == *_test.go ]]; then continue; fi
  for sd in internal/tenant internal/app internal/preview internal/platform; do
    tf="$sd/$base"
    if [[ -f "$tf" ]]; then
      lines=$(diff -u "$sf" "$tf" | wc -l)
      printf "%-50s -> %-30s diff_lines=%s\n" "$sf" "$tf" "$lines"
    fi
  done
done

# After porting #1, #2, #3: re-compute. Expected diffs should drop:
# - clustercredential: 135 → ~30
# - environment_rbac: 71 → ~25
# - preview_controller: 99 → ~30
```

## References

- ADR-0023 — D1 split tree canonical (condition #2 mandates this baseline)
- MEET-2026-05-04-068 — convening meeting record
- `docs/technical/operators/improvement-roadmap.md` §S9-T1, §AP-1
- `fix-all-platform-issues-plan` T1.1 / T1.2 (provenance for ports #1, #2)
- F-09 / CHG-2026-04-29-DOGFOOD T4.1 (provenance for port #2)
- Ralph loop iter 1 commit `f6cbd377` (Phase 0 closure)
