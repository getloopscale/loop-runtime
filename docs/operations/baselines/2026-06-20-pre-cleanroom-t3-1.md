# Steady-state baseline — Pre-Cleanroom for T3.1 LIVE bring-up

**Date:** 2026-06-20 UTC
**Context:** dev-public-env-config-completeness Phase 3 T3.1
**Operator:** Claude (autonomous engineering-side; sponsor JUNTOS at Step 6)
**Plan:** `knowledge-base/plans/dev-public-env-config-completeness-plan.md`

## Layer 1 — Code state

- HEAD: 96ab943eda4c848728b9e06216b53eb77ab2040c
- branch: develop
- ahead of origin/develop: 0
- working tree dirty files: 1

## Layer 2 — Runtime state (pre-destroy)

### k3d clusters
NAME             SERVERS   AGENTS   LOADBALANCER
loopzero-build       1/1       1/1      true
loopzero-mgmt        1/1       2/2      true
loopzero-runtime-2   1/1       1/1      true
loopzero-vault       1/1       0/0      true
loopzero-workload    1/1       1/1      true

### Docker network k3d-loopzero
[{"Subnet":"172.18.0.0/16","Gateway":"172.18.0.1"}]

### loopzero-vault cluster (Jeito A)
NAMESPACE      NAME                                      READY   STATUS    RESTARTS   AGE
cert-manager   cert-manager-5d6789b685-b5fhk             1/1     Running   0          119m
cert-manager   cert-manager-cainjector-df54d4bf6-jnprs   1/1     Running   0          119m
cert-manager   cert-manager-webhook-779f5dfcc4-rc4vd     1/1     Running   0          119m
kube-system    coredns-9fc4f469d-9hsh2                   1/1     Running   0          119m

## Layer 3 — Config state (pre-destroy)

### Caddy
inactive
caddy state: unknown
Caddyfile absent

### dev-credentials canonical SoT
/home/paulo/.loopzero/dev-credentials.json mode=600 size=2867 mtime=2026-06-20 15:28:42.099808892 +0000
[
  "cloud",
  "cloudflare",
  "github",
  "google",
  "openbao",
  "resend",
  "stripe"
]

## Layer 4 — LIVE functional probes (pre-destroy snapshot)

### Public TLS reachability
- https://auth-dev.loopzero.example/ → HTTP 502
- https://app-dev.loopzero.example/ → HTTP 200
- https://cloud-dev.loopzero.example/ → HTTP 502

### DNS resolution
- auth-dev.loopzero.example → 165.227.121.20
- app-dev.loopzero.example → 165.227.121.20
- cloud-dev.loopzero.example → 165.227.121.20
