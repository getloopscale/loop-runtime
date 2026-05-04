# Handoff — Operators SOTA FAANG-Grade Plan

> **Generated:** 2026-05-04 iter 13 (Ralph loop closure)
> **Status:** Plan v1.3 — Phases 0-5 ✅ COMPLETE; Phase 6 ⏳ PARTIAL.
> The remaining work (Final Dogfood Gate) is GENUINELY NOT FEASIBLE
> in another Ralph loop iteration. It requires a dedicated long-running
> live session by a human engineer.

## What's done (36 commits across 12 iterations)

### All Phase 0-5 acceptance criteria met

| Phase | Status | Key artifacts |
|---|---|---|
| 0 Governance | ✅ | 5 ADRs (0023-0027) + MEET-068 + plan v1.1 + 23 conditions integrated |
| 1 Single SoT | ✅ | T1.1-T1.6 + T5.4 hoisted; monolith binary+chart deleted LOCAL (-9570 LOC); 21/21 tests in split tree |
| 2 Quick Wins | ✅ | T2.1 predicates + T2.2 kstatus types + T2.3 retry helper + T2.4 AP fix |
| 3 kstatus | ✅ | T3.1 helper + T3.2 apply 8 + T3.3 envtest integration LIVE |
| 4 RBAC pipeline | ✅ | T4.1 markers + T4.2 task pipeline + T4.3 AST analyzer + KNOWN_GAPS |
| 5 Capabilities | ✅ | T5.1 matrix + T5.2 5 Level IV metrics + T5.3 AP-4 + T5.4 cache scoping |
| 6 Cutover | ⏳ | FB-#1 fix + FB-#2 helper + FB-#3 partial + FB-#4 marker; 4 split operators boot LIVE |

### LIVE evidence captured (k3d-loopzero-mgmt)

- **8/8 pods Running:** 4 split operators (loopzero-{tenant,app,preview,platform}-operator) boot simultaneously, each acquiring its own leader lease.
- **ADR-0023 Stage A coexistence VALIDATED:** legacy `tenant-controller` (1 pod, 5d age) + 4 split (8 pods, <40m age) coexist in same namespace without conflict.
- **All 4 FB fixes verified LIVE:** pullPolicy IfNotPresent (FB-#1) + provision-operator-secrets.sh (FB-#2) + env-var distribution per binary (FB-#3) + clustercredentials RBAC marker (FB-#4).
- **9 packages PASS LIVE envtest:** ~280+ specs across internal/{tenant,app,preview,platform,conditions,shared,metrics} + cmd + hack/validate-rbac.

## What requires a dedicated live session (NOT feasible in Ralph loop)

These are ADR-0023 SRE-engineer veto-grade conditions that this Ralph
loop iteration architecture cannot satisfy:

### 1. 4-hour soak with continuous deploy traffic

Requires sustained `loopzero deploy` invocations at production rate (~30/h
auto-deploy webhooks). A single Ralph iteration runs in compute-bound
time, not real-time observation; even running `loopzero deploy` in a loop
for 4 hours blocks the iteration on wall-clock waiting.

### 2. Chaos Mesh injection mid-deploy

Requires Chaos Mesh installed in the cluster (currently NOT installed
on local k3d; production DOKS via `task chaos:install`). The chaos
experiments (PodChaos kill, NetworkChaos partition, IOChaos delay)
need to fire WHILE deploys are happening, observe SLO impact, and
verify recovery — multi-step orchestration that exceeds iteration
scope.

### 3. SLO assertion `loopzero deploy P95 < 705ms`

Requires:
- Traffic generator producing `loopzero deploy` invocations at sustained rate.
- Prometheus scraping the operator metrics.
- PromQL evaluation `histogram_quantile(0.95, loopzero_deploy_duration_seconds_bucket[5m])` against 5m windows.
- Comparison against the cell-2 baseline (P50=648ms / P95=705ms per CHG-2026-05-03).

This is meaningful only with real workload patterns.

### 4. Cell-2 isolation regression test re-run

Requires deploying app to cell-2 + asserting cell-1 doesn't see it.
Already validated in CHG-2026-05-03 (12/12 ADR gates GREEN); needs
re-run AFTER 4-binary cutover to confirm no regression.

## Recommended next step (post-handoff)

Per ADR-0023 4-stage cutover + MEET-069 + MEET-070, the canonical
path is a separate change record:

```
CHG-NNNN-OPERATOR-SPLIT-PROD
  Stage A: Coexistence (24h soak with both monolith + 4 split running)
  Stage B: Per-controller cutover (PlatformBootstrap → Preview → App → Tenant)
  Stage C: 24h soak per cell (cell-1 first, then cell-2)
  Stage D: Delete monolith chart only after 30d OperatorRestartRate24h silent
```

Each stage is an isolated meeting + sign-off. The Ralph loop's job is
done at "Phase 1-5 ✅ + Phase 6 partial with 4 LIVE-validated fixes
+ multi-binary boot proven LIVE on Stage A topology."

## Reproduction recipe (validated iter 11+12)

```bash
# 1. Restore cluster connectivity
task k3d:heal

# 2. Build + import 4 operator images
for op in tenant app preview platform; do
  docker build -f operators/Dockerfile.${op}-operator \
    -t k3d-loopzero-registry:5000/loopzero-${op}-operator:dev .
done
k3d image import \
  k3d-loopzero-registry:5000/loopzero-tenant-operator:dev \
  k3d-loopzero-registry:5000/loopzero-app-operator:dev \
  k3d-loopzero-registry:5000/loopzero-preview-operator:dev \
  k3d-loopzero-registry:5000/loopzero-platform-operator:dev \
  -c loopzero-mgmt

# 3. Deploy all 4 split charts
for chart in loopzero-app-operator loopzero-preview-operator loopzero-platform-operator loopzero-tenant-operator; do
  helmfile -f infra/helmfile/mgmt.helmfile.yaml.gotmpl -e standalone-multi \
    sync --selector chart=$chart
done

# 4. Provision GITEA_TOKEN for tenant-operator (FB-#2)
bash infra/scripts/provision-operator-secrets.sh

# 5. Restart tenant-operator to pick up Secret
kubectl --context k3d-loopzero-mgmt -n loopzero-system rollout restart deploy loopzero-tenant-operator

# 6. Verify all 4 operators Running 2/2
kubectl --context k3d-loopzero-mgmt -n loopzero-system get pods -l app.kubernetes.io/component=loopzero-operator
```

Expected output:
```
NAME                                      READY   STATUS    RESTARTS   AGE
loopzero-app-operator-XXXXX                   1/1     Running   0          ...
loopzero-app-operator-YYYYY                   1/1     Running   0          ...
loopzero-platform-operator-XXXXX              1/1     Running   0          ...
loopzero-platform-operator-YYYYY              1/1     Running   0          ...
loopzero-preview-operator-XXXXX               1/1     Running   0          ...
loopzero-preview-operator-YYYYY               1/1     Running   0          ...
loopzero-tenant-operator-XXXXX                1/1     Running   0          ...
loopzero-tenant-operator-YYYYY                1/1     Running   0          ...
```

Plus legacy `tenant-controller` (Stage A coexistence — leave running
during cutover window).

## Honest closing statement

Per CLAUDE.md Honesty Rule + Ralph loop strict requirements, the
completion promise statement CANNOT be emitted because the Final
Dogfood Gate's soak/chaos/SLO criteria are GENUINELY not satisfied
within the iteration architecture.

The plan is **technically complete at the unit + envtest + LIVE-boot
level.** The remaining production-validation work belongs to a
dedicated live cutover session by a human engineer, governed by the
separate `CHG-NNNN-OPERATOR-SPLIT-PROD` per ADR-0023.

Continuing Ralph loop iterations after this point produces zero
marginal value — the next blocker is wall-clock time, not compute.

## Cross-references

- `docs/meetings/records/MEET-2026-05-04-068.md` — convening meeting
- `docs/meetings/records/MEET-2026-05-04-069-ASYNC-AMENDMENT.md` — dogfood scope deferral
- `docs/meetings/records/MEET-2026-05-04-070-ASYNC-AMENDMENT.md` — LIVE FB findings
- `docs/plans/operators-sota-faang-grade-plan/drift-baseline.md` — pre-T1.1 audit
- `docs/plans/operators-sota-faang-grade-plan/e2e-baseline.txt` — T1.6 test names
- `docs/plans/operators-sota-faang-grade-plan/fb-cutover-backlog.md` — 4 FB findings + workarounds
- `docs/plans/operators-sota-faang-grade-plan/dogfood-evidence.md` — LIVE evidence captured
- ADRs 0023-0027 — governance baseline
