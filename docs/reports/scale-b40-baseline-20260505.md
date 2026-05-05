# B40 Baseline RED Proof — `loopzero scale` is orphan from deploy path

**Date:** 2026-05-05 23:23 UTC
**HEAD:** `ccaa093e` (docs(plan): loopzero-scale-end-to-end plan)
**Tag:** `scale-b40-baseline-20260505`

## Sequence executed

```bash
# 1. Clean state
loopzero project delete solo-server --yes  # already not found, OK
DELETE FROM app_scaling_config WHERE tenant_id='tn-e2etest';  # → 1 row removed
seed-local-user.sh  # token regen

# 2. First deploy (baseline from loopzero.yaml)
loopzero deploy --env production --no-wait
# build-fcee37dd37e5583f → SUCCEEDED in ~2min

# 3. Cluster state pre-scale
kubectl --context k3d-loopzero-workload -n loopzero-tn-e2etest-production \
  get deploy solo-server-api -o jsonpath='{.spec.replicas}'
# Output: 1  ← from loopzero.yaml fixture

# 4. loopzero scale --replicas 3
loopzero scale --app api --env production --replicas 3 --json
# Response: {"app":"api","environment":"production","replicas":3,"status":"applied"}

# 5. DB inspection
SELECT tenant_id, project_id, environment, app_name, min_replicas, max_replicas
FROM app_scaling_config WHERE tenant_id='tn-e2etest';
# Output: tn-e2etest|proj-2c94fdc3|production|api|3|3
# ✅ Row WRITTEN with min=max=3

# 6. Redeploy (forces manifest re-render)
loopzero deploy --env production --no-wait
# build-dfcb8408ad7be4ab → SUCCEEDED in ~2min

# 7. Cluster state post-scale + post-redeploy
kubectl --context k3d-loopzero-workload -n loopzero-tn-e2etest-production \
  get deploy solo-server-api -o jsonpath='{.spec.replicas}'
# Output: 1  ← BUG: should be 3
```

## Gap evidence

| Step | Expected | Actual | Status |
|---|---|---|---|
| Baseline replicas | 1 (from loopzero.yaml) | 1 | ✅ |
| Scale handler response | `applied`, replicas=3 | `applied`, replicas=3 | ✅ |
| DB row write | `min=max=3` | `min=max=3` | ✅ |
| Post-redeploy K8s replicas | **3** (from DB) | **1** (from loopzero.yaml) | ❌ **BUG** |

## Root cause confirmed

`api/internal/services/build_orchestrator_submit.go:107-124` populates
`BuildAppSpec.Replicas = a.Replicas` direct from `req.Apps[].Replicas`
which comes from `loopzero.yaml` parsed CLI-side. Renderer reads from this
spec, not from `app_scaling_config` table.

The `app_scaling_config` row is written by `loopzero scale` but **never
read** by the deploy path.

## Important detail discovered

`project_id` in `app_scaling_config` is `proj-2c94fdc3` (UUID-like
internal ID), NOT the project name `solo-server`. This means the
ScalingResolver implementation must use the same identifier scheme as
the scale handler — `req.ProjectID` from the build_orchestrator must
match the value persisted by the scale handler.

Cross-reference: `api/internal/routes/scale.go` resolves the project
name → ID via `projectStore.GetByName` (line ~140) before persisting.
The build_orchestrator receives `req.ProjectID` already as the ID
(from the API contract). So both paths use ID; resolver call must use
`req.ProjectID` directly.

## Conclusion

RED proof: `loopzero scale --replicas N` + `loopzero deploy` does NOT propagate
N to the K8s `Deployment.spec.replicas`. The fix per
`docs/plans/loopzero-scale-end-to-end-plan.md` Phase 1+2+3 will close this
gap. Phase 6 dogfood gate validates LIVE with assertion.
