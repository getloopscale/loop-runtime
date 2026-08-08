---
type: baseline
title: "Steady-state baseline — 2026-06-09 pre M2 operator canonical fix"
---

# Steady-state baseline — 2026-06-09 pre M2 operator canonical fix

> 4-layer baseline per `.claude/rules/steady-state-baseline.md` captured at start
> of `/implement` halt-loop for plan
> `.claude/knowledge-base/plans/2026-06-09-m2-operator-canonical-fix-plan.md` v1.4
> (verdict SHIPPABLE_WITH_CAVEATS, final score 89.0; TDD shape gate 9/9 PASS).

## Why this baseline

This baseline is the bisect anchor for the M2 operator canonical fix delivery.
The plan ships 9 tasks (T0.1, T1.1, T2.1, T2.2, T3.1, T4.1, T5.1, T6.1, T7.1)
closing the M2 operator drift identified LIVE during T6.1 testing
(`reference_m2_runtime_envs_per_project_session_2026_06_07` +
`feedback_m2_canonical_drift_operator_renderer` memories): the manifest renderer
emits the M2 canonical 3-segment namespace pattern (`loopzero-<tenant>-<project>-<env>`)
while TenantController + EnvironmentController still emit the M1 2-segment pattern
(`loopzero-<tenant>-<env>`). Production deploys cascade-fail with `AppProject reject
destination → namespace not found → SA not found → pods Forbidden` and require
4 manual operator workarounds. The PreviewController is M2-correct; the gap is
isolated to the production env reconcile path in `operators/internal/tenant/`.

Post-implementation baseline [`2026-06-09-post-m2-operator-fix.md`](./2026-06-09-post-m2-operator-fix.md) (T7.1 deliverable)
will capture state after the 9 tasks ship + LIVE E2E `dev-public-production-deploy_test.sh`
passes. Any regression that surfaces post-merge is diff-bounded by these two snapshots.

The prior milestone baseline is
[`docs/operations/baselines/2026-06-08-post-test-rigor-cleanroom.md`](./2026-06-08-post-test-rigor-cleanroom.md) (pre-release
test rigor delivery). Intermediate work in `worktree-glowing-growing-babbage`
delivered the pre-release-test-rigor harness + T6.1 LIVE testing that surfaced
the M2 operator drift documented here.

---

## Layer 1 — Code state

```
commit:               408e59c5 (chore(implement): SEPA agent + paired skill + halt-loop driver + contract for M2 operator canonical fix)
branch:               worktree-glowing-growing-babbage
relation:             ahead of origin/develop by N commits (see git log --oneline origin/develop..HEAD)
prior milestone:      2026-06-08-post-test-rigor-cleanroom.md
working tree:         clean (this baseline lands within iteration 1 of /implement halt-loop)
plan slug:            2026-06-09-m2-operator-canonical-fix
plan version:         v1.4 (SHIPPABLE_WITH_CAVEATS 89.0)
```

Files in scope for the 9 tasks (per plan § Baseline Context + Files to edit):

```
operators/internal/tenant/tenant_gitops.go           (T2.1, T2.2 — canonicalDestNamespace + AppProject destinations)
operators/internal/tenant/environment_controller.go  (T3.1 — reconcileNamespace M2 canonical)
operators/api/v1/environment_types.go                (T1.1 — Project required + Pattern)
api/internal/services/project_service.go             (T4.1 — POST /api/v1/projects auto-create production env)
infra/scripts/local/migrate-legacy-namespaces.sh     (T5.1 — dual-mode migration)
infra/tests/e2e/dev-public-production-deploy_test.sh (T6.1 — LIVE chain test, NEW)
pkg/naming/naming.go                                 (read-only — canonical lives here already)
```

Recent prior commits for context:

```
408e59c5 chore(implement): SEPA agent + paired skill + halt-loop driver + contract for M2 operator canonical fix
144927ec chore(plan): M2 operator canonical fix plan v1.4 — /implement TDD shape gate compliance
2a50e07f feat(plan): M2 operator canonical fix plan v1.3 SHIPPABLE 96.0
c8edc0a5 fix(t6.1): reset-for-e2e.sh re-seeds plan_limits after TRUNCATE
a0dabfc4 feat(t6.1): TEMPORARY reset-for-e2e.sh — DELETE after umbrella PASS
86c69ba8 fix(t6.1): wire GITHUB_APP_ID + GITHUB_APP_PRIVATE_KEY for App integrations
e2747009 fix(t6.1): wire APP_PUBLIC_URL so install callback redirects to /projects, not /billing
9f5f0652 refactor(tilt): GITHUB_WEBHOOK_SECRET via Secret canonical (was hardcoded literal)
cd53792d fix(dashboard): login page rebuilds stale flow instead of phantom UI
```

---

## Layer 2 — Runtime state (M1 drift symptoms)

The following Helm releases + K8s resources reflect the **M2-drifted** state.
Captured manually via operator inspection during T6.1 (2026-06-08/09) since
this iteration runs without live cluster probe. Per memory
`feedback_m2_canonical_drift_operator_renderer`:

**Tenant under test:** `tn-2a997c82db0bda1b` (admin@loopzero.example seed).

Symptoms LIVE-observed during T6.1 production deploy:

```
1. AppProject rejects destination:
   "namespace loopzero-tn-2a997c82db0bda1b-teste-production is not permitted in project 'tn-2a997c82db0bda1b'"
   → TenantController.reconcileArgoCDApp emitted spec.destinations limited to
     M1 2-segment `loopzero-tn-2a997c82db0bda1b-production` (no -teste- middle segment).

2. Namespace M2 3-segment not found:
   "namespaces \"loopzero-tn-2a997c82db0bda1b-teste-production\" not found"
   → EnvironmentController.reconcileNamespace created M1 2-segment ns only.
     ArgoCD AppProject syncOptions has CreateNamespace=false by design.

3. ServiceAccount loopzero-app absent in M2 ns:
   "error looking up service account loopzero-tn-2a997c82db0bda1b-teste-production/loopzero-app:
    serviceaccount \"loopzero-app\" not found"
   → EnvironmentController only populated canonical resources (SA + NetworkPolicy +
     ResourceQuota) in the M1 2-segment ns.

4. ReplicaSet creates but pods never scheduled:
   "Failed" event — SA absent → pods Forbidden.
```

Preview path comparison (PR #3 deploy in same T6.1 session) ran 100% canonical
with ZERO workarounds: PreviewController correctly emitted M2 4-segment ns
`loopzero-tn-2a997c82db0bda1b-<project>-preview-pr-3` + AppProject destination +
SA loopzero-app + pods 2/2 Running + ArgoCD Synced Healthy. This isolates the gap
to the **production env reconcile path** in `operators/internal/tenant/` —
the canonical fix scope is `TenantController.reconcileArgoCDApp` +
`EnvironmentController.reconcileNamespace`.

T6.1 LIVE workarounds applied (to be removed post-fix):

```bash
# 1. Patch AppProject destinations to include M2 3-segment
kubectl --context k3d-loopzero-mgmt -n argocd patch appproject "$TENANT" --type=merge \
  -p '{"spec":{"destinations":[{"name":"cell-default","namespace":"loopzero-..production"},
                                {"name":"cell-default","namespace":"loopzero-..teste-production"}]}}'
# 2. Create M2 3-segment namespace manually
kubectl --context k3d-loopzero-workload create namespace "$NS_M2"
# 3. Copy SA loopzero-app from M1 2-seg ns to M2 3-seg ns
kubectl --context k3d-loopzero-workload -n "$NS_M1" get sa loopzero-app -o yaml \
  | grep -v 'namespace:\|resourceVersion:\|uid:' \
  | kubectl --context k3d-loopzero-workload -n "$NS_M2" apply -f -
# 4. Force ArgoCD sync + restart RS
kubectl --context k3d-loopzero-mgmt -n argocd patch app "$TENANT-production" --type merge \
  -p '{"operation":{"sync":{"revision":"HEAD","prune":true}}}'
kubectl --context k3d-loopzero-workload -n "$NS_M2" delete rs -l 'app=<project>-api'
```

Result LIVE: pods 2/2 Running with the 4 workarounds. The 4 workarounds are the
exact deliverables this plan eliminates.

---

## Layer 3 — Config state

```
schema_migrations latest: per /tmp inspection during T6.1 — migrations 000001..000032
                          applied via reset-for-e2e.sh + 000002_seed_data + 000032_plan_caps_m2.
operator config:          GITHUB_APP_ID=3319955 wired into loopzero-api Deployment (commit 86c69ba8)
                          GITHUB_WEBHOOK_SECRET via valueFrom (Secret loopzero-github-webhook)
                          APP_PUBLIC_URL=https://app-dev.loopzero.example (commit e2747009)
                          BILLING_RETURN_URL preserved per dashboard-public canonicalization
plan-confidence verdict:  SHIPPABLE_WITH_CAVEATS 89.0 (residual: concurrency_tests_missing soft cap)
deps audit verdict:       PASS_WITH_CAVEATS cap 89 (4 informational CVEs in deps NOT touched by plan;
                          tracked separately as dep-hygiene PR per audit recommendation)
TDD shape gate:           9/9 tasks PASS (check_tdd_shape.py all_pass=true)
```

---

## Layer 4 — LIVE functional probes (pre-fix expected behavior)

These probes describe the SLO floor at the pre-fix baseline; the post-fix baseline
should transition all of them to PASS without manual intervention.

```
- GET /engine/api/v1/projects/teste → 200 with project metadata          [PRE-FIX: PASS]
- POST github webhook (canonical fixture push)
   → workflow created within 5s in argo                                  [PRE-FIX: PASS]
- ArgoCD app loopzero-<tenant>-production sync                              [PRE-FIX: FAIL — AppProject reject]
   → state: SyncFailed; reason: "namespace ... is not permitted"        [PRE-FIX: FAIL]
- Namespace loopzero-<tenant>-<project>-production exists in cell-default   [PRE-FIX: FAIL — not auto-created]
- ServiceAccount loopzero-app in M2 namespace                                [PRE-FIX: FAIL — only in M1]
- Pods 2/2 Running in M2 namespace                                       [PRE-FIX: FAIL without 4 workarounds]
- HTTPS GET https://<tenant>.<project>.<entryApp>.loopzero.example → 200      [PRE-FIX: FAIL — no pods serving]
- Preview deploy chain (PR opened → preview env URL 200)                 [PRE-FIX: PASS — PreviewController M2-correct]

T6.1 LIVE wall-clock to production deploy success WITH 4 workarounds: ~25min (manual operator).
T6.1 LIVE wall-clock to preview deploy success WITHOUT workarounds:    ~7min (automated).
Asymmetry confirms drift is in production path only.
```

---

## Cross-links

- Plan: `.claude/knowledge-base/plans/2026-06-09-m2-operator-canonical-fix-plan.md` (v1.4)
- Edge-case review: `.claude/knowledge-base/reviews/2026-06-09-m2-operator-canonical-fix-edge-cases-2026-06-09.md`
- Deps audit: `.claude/knowledge-base/audits/2026-06-09-m2-operator-canonical-fix-deps-audit-2026-06-09-v1.2.md`
- Implementation contract: `.claude/knowledge-base/implementations/2026-06-09-m2-operator-canonical-fix-implementation.md`
- SEPA: `.claude/agents/implement-2026-06-09-m2-operator-canonical-fix-2026-06-09/sepa.md`
- Prior baseline: [`docs/operations/baselines/2026-06-08-post-test-rigor-cleanroom.md`](./2026-06-08-post-test-rigor-cleanroom.md)
- Memory: `feedback_m2_canonical_drift_operator_renderer` + `reference_m2_runtime_envs_per_project_session_2026_06_07`

This baseline is referenced from the commit body that closes T0.1 (this commit)
per `.claude/rules/steady-state-baseline.md` § Cross-linking mandatory.
