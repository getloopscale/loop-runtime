---
type: baseline
title: T6.2 Post-Rebuild Baseline
description: "Captured: 2026-06-11T17:36:52Z"
---

# T6.2 Post-Rebuild Baseline
Captured: 2026-06-11T17:36:52Z
Status: Phase 5 of Cleanroom TOTAL chain (post task setup + tilt up + task doctor PASS gate)

## Layer 1 — Code state
```
HEAD:    b76b9e9f2325f525de1c9667c6ea235b222cb543
Branch:  worktree-glowing-growing-babbage
Dirty:   0 files
Ahead:   2248 commits ahead of main
```

### Commits da Cleanroom (4 LIVE catches resolved):
b76b9e9f fix(infra): update-cross-cluster-ips.sh — patch live ConfigMap post-T1.1.b
f4ccb6c2 fix(build): canonical go mod tidy across 8 submodules (T6.2 follow-up)
21c0fae1 fix(build): remove redundant toolchain directive in dockerfile-lint go.mod
07326c68 fix(build): bump Dockerfile FROM golang:1.26.0 → 1.26.1 (T0.8 follow-up)

## Layer 2 — Runtime state (clusters)
```
NAME             SERVERS   AGENTS   LOADBALANCER
loopzero-build       1/1       1/1      true
loopzero-mgmt        1/1       2/2      true
loopzero-runtime-2   1/1       1/1      true
loopzero-workload    1/1       1/1      true
```

### loopzero-system pods (mgmt cluster):
```
app-operator-595c598c4-zrndq         1/1     Running             0          13m
gitea-6fc9696d4c-7h9l7               1/1     Running             0          52m
gitea-redis-master-0                 2/2     Running             0          54m
harbor-core-5c97f5b68f-sz7zq         1/1     Running             0          36m
harbor-database-0                    1/1     Running             0          52m
harbor-jobservice-7bc9b5c4f7-z2pc2   1/1     Running             0          36m
harbor-portal-7b49df4d49-fhhzd       1/1     Running             0          52m
harbor-redis-0                       1/1     Running             0          52m
harbor-redis-master-0                2/2     Running             0          54m
harbor-registry-659b59f696-g958p     2/2     Running             0          36m
harbor-trivy-0                       1/1     Running             0          52m
minio-5759b486cc-dd2j8               1/1     Running             0          54m
platform-operator-86ccdd9d4f-fmzzf   1/1     Running             0          11m
postgresql-0                         2/2     Running             0          54m
preview-operator-854fc9b49d-t6cbh    1/1     Running             0          13m
loopzero-api-868758956c-zbcwc            1/1     Running             0          80s
loopzero-api-868758956c-zlh7w            1/1     Running             0          80s
loopzero-cloud-5bd7655865-q5tgw          0/1     ContainerCreating   0          2s
```

### vault-system pods (mgmt cluster, post-OpenBao bootstrap):
```
NAME                 READY   STATUS      RESTARTS   AGE
openbao-0            1/1     Running     0          6m17s
openbao-init-74mqx   0/1     Completed   0          4m33s
```

### CRs:
```
NAMESPACE     NAME                        PHASE   PLAN      AGE
loopzero-system   tenant.loopzero.io/tn-e2etest           starter   25m

NAMESPACE     NAME                                     CLUSTER        CONNECTED   AGE
loopzero-system   clustercredential.loopzero.io/cell-1         cell-1         true        29m
loopzero-system   clustercredential.loopzero.io/cell-2         cell-2         true        29m
loopzero-system   clustercredential.loopzero.io/cell-default   cell-default   true        26m
```

## Layer 3 — Config state
```
Schema migrations version:
Defaulted container "postgresql" out of: postgresql, metrics
Password for user loopzero: 
psql: error: connection to server on socket "/tmp/.s.PGSQL.5432" failed: fe_sendauth: no password supplied
command terminated with exit code 2

Secret resourceVersions (NOT values):
loopzero-openbao-approle: 8342
loopzero-service-auth:    898
openbao-init-token:   8086
```

## Layer 4 — LIVE functional probes
```
[200] GET http://localhost:13001/api/v1/health (API health)
[{] bao status (OpenBao seal state)

task doctor: PASS=41 / WARN=6 / FAIL=0 (gate PASS≥38, FAIL=0 ✅)
Cells connected: 3/3 (cell-default + cell-1 + cell-2)
```

## Diff vs pre-rebuild (Phase 0)
- HEAD: 540bc70f → b76b9e9f (+4 commits)
- Clusters: same 4 (mgmt + build + workload + runtime-2) but fully bootstrapped (vs sealed pre-state)
- OpenBao: NEW (was absent in pre-rebuild)
- loopzero-api: NEW (was absent in pre-rebuild)
- ClusterCredentials: 3 connected (vs 0 connected pre-rebuild)

## Outstanding (Phase 6 HARD STOPs)
- WARN x6: ory-kratos/hydra not installed (4) + tenant-operator deployment not found (1) + 1 other
- Phase 6 sponsor LIVE walkthrough J-22 + J-10 awaiting human-driven test
