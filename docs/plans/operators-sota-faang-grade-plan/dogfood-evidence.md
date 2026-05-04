# Dogfood Evidence — Operators SOTA FAANG-Grade Plan

> **Per MEET-2026-05-04-069 + MEET-2026-05-04-070:** the Final Dogfood
> Gate requires live infrastructure validation. This document captures
> evidence collected during iter 11 of the Ralph loop.

**Generated:** 2026-05-04 iter 11
**Cluster:** k3d-loopzero-mgmt (existing local k3d, not fresh)

## ✅ LIVE evidence collected

### 1. loopzero-tenant-operator split chart deploys + boots correctly

After applying FB-#1 (pullPolicy=IfNotPresent), FB-#2
(provision-operator-secrets.sh), and FB-#3 (env-var distribution),
the loopzero-tenant-operator chart deploys cleanly on k3d-loopzero-mgmt:

```
$ kubectl --context k3d-loopzero-mgmt -n loopzero-system get pods -l app.kubernetes.io/name=loopzero-tenant-operator
NAME                                    READY   STATUS    RESTARTS   AGE
loopzero-tenant-operator-6565cf7649-fbzv7   1/1     Running   0          57s
loopzero-tenant-operator-6565cf7649-fjd2r   1/1     Running   0          66s
```

Logs confirm the binary boots correctly:

```
INFO tracing enabled endpoint=otel-collector.loopzero-system.svc.cluster.local:4317
INFO build cluster client created kubeconfig=/etc/loopzero/build-kubeconfig
INFO runtime cluster client created kubeconfig=/etc/loopzero/runtime-kubeconfig
INFO starting tenant-operator leaderElection=true
INFO Starting metrics server
INFO starting server name="health probe" addr="[::]:8081"
INFO Serving metrics server bindAddress=":8443" secure=false
leaderelection: Attempting to acquire leader lease lock="loopzero-system/loopzero-tenant-operator.loopzero.io"
```

2/2 replicas Running; leader election in progress; metrics + health
probes serving. This is the FIRST LIVE evidence that the Phase 1
split-chart cutover path works end-to-end on k3d.

### 2. envtest validation across all 9 packages

```
ok  internal/tenant     (42s)  — 156 specs incl. kstatus_compliance
ok  internal/app        (15s)
ok  internal/preview    (162s)
ok  internal/platform   (40s)  — 22 specs
ok  internal/conditions (2s)   — 29 unit
ok  internal/shared     (0.3s) — 14 unit
ok  internal/metrics    (0.08s)— 7 unit
ok  cmd                 (0.15s)— 12 cmd
ok  hack/validate-rbac  (0.005s) — 6 unit

9 packages, ~280+ specs, 0 failures
```

## ⏳ Items still requiring full live session

The following acceptance criteria from ADR-0023 SRE conditions cannot
be validated in a single Ralph loop iteration:

1. **4-hour soak with continuous deploy traffic.**
   Requires sustained `loopzero deploy` invocations at production rate
   (~30/h). Iter 11 only deployed the chart; no soak.

2. **Chaos Mesh injection mid-deploy.**
   Requires Chaos Mesh installed + scripted PodChaos / NetworkChaos
   experiments. Out of scope for chart-validation iteration.

3. **Operator cutover rollback drill.**
   `infra/scripts/operator-cutover-rollback.sh` was DELETED in T1.5;
   the LOCAL plan does not include rollback (production is separate
   CHG). Iter 11 demonstrated FORWARD migration; reverse migration is
   `kubectl delete deploy loopzero-tenant-operator + restart legacy chart`
   (worked during iter 11 cleanup).

4. **SLO assertion `loopzero deploy P95 < 705ms`.**
   Requires production-rate deploy traffic + Prometheus query
   evaluation post-cutover.

5. **Cell-2 isolation regression test.**
   Requires deploying app to cell-2 + asserting cell-1 doesn't see it.
   Out of scope; cell-2 was already validated in CHG-2026-05-03.

6. **Multi-controller (4 binaries) simultaneous boot.**
   Iter 11 validated tenant-operator only. app-operator,
   preview-operator, platform-operator should follow the same path
   but their charts may need additional FB-#3 audit (HARBOR_*,
   MINIO_*, etc., for tenant-operator's full env surface).

## Conclusion

The operators SOTA plan v1.3 is **LIVE-validated at the boot level**:
the split chart deploys, binaries connect to all required dependencies
(build/runtime kubeconfigs, otel-collector, leader election lease),
and serve metrics + health probes.

The plan is **NOT validated at the soak/SLO/multi-controller level**
within this iteration; that work belongs to a dedicated live cutover
session per MEET-069 + MEET-070.

The Ralph loop completion promise CANNOT be emitted yet because:
- 4-hour soak NOT done
- Chaos injection NOT done
- SLO assertion NOT measured
- 3 of 4 split charts NOT live-deployed (only tenant-operator was)

When a future session captures this evidence, the promise statement
becomes truthful per ADR-0022 §"Async exception" ratification path.

## Reproduction recipe

```bash
# 1. Restore cluster connectivity (heal scripts)
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

# 3. Deploy a single split chart (tenant-operator validated this iter)
helmfile -f infra/helmfile/mgmt.helmfile.yaml.gotmpl -e standalone-multi \
  sync --selector chart=loopzero-tenant-operator

# 4. Provision the chart-managed Secret with GITEA_TOKEN
bash infra/scripts/provision-operator-secrets.sh

# 5. Restart deployment to pick up Secret
kubectl --context k3d-loopzero-mgmt -n loopzero-system rollout restart deploy loopzero-tenant-operator

# 6. Verify Running 2/2
kubectl --context k3d-loopzero-mgmt -n loopzero-system get pods -l app.kubernetes.io/name=loopzero-tenant-operator
```
