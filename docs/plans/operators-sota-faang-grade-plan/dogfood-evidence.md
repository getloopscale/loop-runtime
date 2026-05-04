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

### 3. Multi-controller (4 binaries) simultaneous boot — ✅ VALIDATED iter 12

After applying FB-#1/#2/#3 fixes + FB-#4 (RBAC marker for
clustercredentials in tenant-operator), all 4 split operators boot
together on k3d-loopzero-mgmt:

```
$ kubectl --context k3d-loopzero-mgmt -n loopzero-system get pods \
    -l app.kubernetes.io/component=loopzero-operator
NAME                                      READY   STATUS    RESTARTS   AGE
loopzero-app-operator-756775475b-77z9l        1/1     Running   0          40m
loopzero-app-operator-756775475b-t5t54        1/1     Running   0          40m
loopzero-platform-operator-68f57f9cd5-9xkxd   1/1     Running   0          39m
loopzero-platform-operator-68f57f9cd5-n7cnk   1/1     Running   0          39m
loopzero-preview-operator-7777465495-26dlm    1/1     Running   0          39m
loopzero-preview-operator-7777465495-cwjqh    1/1     Running   0          39m
loopzero-tenant-operator-6854644f64-7llmn     1/1     Running   0           2m
loopzero-tenant-operator-6854644f64-dpdcq     1/1     Running   0           2m
```

**8/8 pods Running across 4 deployments.** Each binary acquired its
own leader-election lease (loopzero.io/{tenant,app,preview,platform}-operator).

Validates the dx-engineer condition #1 (shared label
`app.kubernetes.io/component: loopzero-operator`): a single kubectl
selector returns all 4 operator pods — fan-out queries work.

### 4. ADR-0023 Stage A coexistence — ✅ VALIDATED iter 12

Legacy `tenant-controller` deployment (monolith image, 1 pod, age 5d)
coexists with the 4 split operators (8 pods, age <40m) in the same
namespace WITHOUT lease conflict, RBAC collision, or pod eviction.

This is the exact pattern ADR-0023 4-stage cutover specifies for
Stage A. Production cutover (CHG-NNNN-OPERATOR-SPLIT-PROD) can use
the same Stage A pattern with confidence.

### 5. FB-#4 (NEW): tenant-operator RBAC missing clustercredentials list

**Discovered LIVE iter 12.** tenant-operator's
`shared.RuntimeClientFactory` LISTs ClusterCredentials to resolve
cell→runtime-cluster mapping, but the T4.1 marker only covered
tenants/environments. Crash log:

```
"User \"system:serviceaccount:loopzero-system:loopzero-tenant-operator\"
cannot list resource \"clustercredentials\" in API group \"loopzero.io\"
at the cluster scope"
```

**Fix applied (commit FB-#4):** added marker
`+kubebuilder:rbac:groups=loopzero.io,resources=clustercredentials,verbs=get;list;watch`
to TenantReconciler struct. Verified LIVE via patch
`kubectl patch clusterrole loopzero-tenant-operator` — no more forbidden
errors after patch.

### 6. T5.4 cache scoping congruence — ✅ STILL PASSES post-iter-12

```
=== RUN   TestCacheScopingCongruence_Static
--- PASS: TestCacheScopingCongruence_Static (0.00s)
=== RUN   TestCacheScopingCongruence_Coverage
--- PASS: TestCacheScopingCongruence_Coverage (0.00s)
```

Adding the FB-#4 marker did NOT introduce cache scoping overlap
between binaries — the matrix gates still hold.

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
