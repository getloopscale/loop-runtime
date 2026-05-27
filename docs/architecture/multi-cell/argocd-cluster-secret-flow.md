<!-- AS-IS STATE -->
---
name: argocd-cluster-secret-flow
description: Multi-cell domain — how ArgoCD app-controller talks to tenant cells using cluster Secrets materialized by ClusterCredential operator
type: concept
created_at: 2026-05-26
updated_at: 2026-05-26
updated_by: ralph-loop
evidence_class: MEASURED
---

# ArgoCD Cluster Secret Flow — AS-IS (post Op B)

> Plan: `docs/plans/completed/2026-05-25-live-ca-fetch-clustercredential-plan.md` v1.2 — T0.1 Pre-Plan
> Related: [[clustercredential]] (platform domain)

## Multi-cell trust model

LoopZero runs **multi-cell**: one mgmt cluster + N tenant cells. ArgoCD on mgmt deploys tenant workloads to cells via cluster Secrets that encode credentials + trust material.

```
                  ┌───────────────────────────────────────┐
                  │            mgmt cluster                │
                  │                                        │
                  │  ┌────────────────┐                    │
                  │  │ ClusterCred CR │ (per cell)         │
                  │  │   cell-default │                    │
                  │  │   cell-1       │                    │
                  │  │   cell-2       │                    │
                  │  │   runtime-2    │                    │
                  │  └───────┬────────┘                    │
                  │          │ reconcile materializes      │
                  │          v                             │
                  │  ┌────────────────┐                    │
                  │  │ ArgoCD cluster │ (per cell)         │
                  │  │ Secret         │   bearer + caData  │
                  │  └───────┬────────┘                    │
                  │          │ ArgoCD app-controller       │
                  │          │ reads + connects            │
                  └──────────┼────────────────────────────┘
                             │ HTTPS via apiServer URL
            ┌────────────────┼────────────────┐
            v                v                v
       ┌────────┐       ┌────────┐       ┌────────┐
       │ cell-  │       │ cell-1 │       │ cell-2 │  (k3d-loopzero-workload + others)
       │ default│       │        │       │        │
       └────────┘       └────────┘       └────────┘
```

## Sequence — Tenant deploy to cell

```
1. loopzero deploy → API
2. API submits Argo Workflow on build cluster
3. Build → cosign sign → push image
4. API generates manifests → push to Gitea
5. ArgoCD app-controller polls Gitea ApplicationSet
6. ArgoCD reads cluster Secret (per cell) to authenticate to cell K8s API
   ├─ bearerToken = SA token (rotated by ClusterCredential operator)
   └─ caData = base64(PEM) CA verifying cell apiServer cert
7. ArgoCD applies manifests on cell K8s
8. Cell K8s schedules pods, LoopZero app starts serving
```

## Op B's role in this flow (step 6)

Before Op B (CHG-002b 2026-04-27): `caData` was a **snapshot** copied from the kubeconfig Secret. If the target cell's API-server CA rotated (k3s recreate, k3d server-ca rebuild, prod cert rollover), the snapshot went stale and ArgoCD app-controller failed with `x509: certificate signed by unknown authority`. Manual heal needed (H19/H19a in `k3d-cross-cluster-heal.sh`).

After Op B (this plan): on every reconcile, the operator fetches the **live** CA from the cell's `default/kube-root-ca.crt` ConfigMap. The flow self-heals across CA rotations.

```
Step 6.preamble (per reconcile, 60s interval):
  ClusterCredentialReconciler.reconcileArgoCDClusterSecret(cr)
    └─ resolveCAData()
         ├─ D4: if cr.Spec.TLS.Insecure → skip + counter "skipped-insecure"
         ├─ D1: kubeRootCAFetcher.FetchClusterCA(restConfig)
         │       └─ in-cluster GET default/kube-root-ca.crt
         │       └─ EC-13: clone restConfig with TLSClientConfig{Insecure:true}
         │       └─ counter "success" on hit
         └─ D2: fallback to snapshot caData on error + counter "fallback"
    └─ Update ArgoCD cluster Secret data.config.tlsClientConfig.caData
```

## Failure modes — multi-cell perspective

| Scenario | Op A (insecure=true) | Op B (insecure=false, live fetch) |
|---|---|---|
| Cell CA rotated | ArgoCD ignores caData, connects insecure | Live fetch picks up new CA on next reconcile (60s recovery) |
| Cell's kube-root-ca.crt RBAC denied | ArgoCD ignores caData, connects insecure | Fallback to snapshot caData (degraded but no panic), counter alerts SRE |
| ClusterCredential CR deleted | ArgoCD loses Secret, can't connect | ArgoCD loses Secret, can't connect |
| Cell unreachable | `status.Connected=false`, sync blocked | Same |
| `tenants.cell_id` re-pinned via `loopzero admin tenants pin` | Atomic DB UPDATE + Tenant CR MergePatch | ArgoCD re-deploys with new cell's cluster Secret |

## Cell-pinning relationship to this flow

- Tenant placement: `RoundRobinEngine.AssignAndExecute` writes `tenants.cell_id` (sticky, ADR-0008/0021).
- Routing: `DynamicRouter.Resolve(cellID)` hash lookup, no decision.
- For a tenant on cell X, ArgoCD app `<tenant>-<env>` has `destination.name=X` → ArgoCD finds cluster Secret labeled `loopzero.io/cluster-credential=X`.
- Op B keeps THAT Secret's `caData` fresh for cell X.

## Source-of-truth files

| File | What |
|---|---|
| `api/internal/placement/dynamic_router.go` | Resolves cellID → CellConfig (kubeconfig, ArgoCD URL/token, base domain) |
| `api/internal/placement/engine.go` | RoundRobinEngine sticky-assigns tenant → cell |
| `operators/internal/platform/clustercredential_controller.go` | Materializes ArgoCD cluster Secret per CC |
| `infra/scripts/local/k3d-cross-cluster-heal.sh` | H19/H19a heal blocks (deprecate after Op B 7-day soak per T5.1) |
| `pkg/naming/argocd.go` | Cluster Secret naming convention |

## Cross-references

- [[clustercredential]] (platform domain entity)
- [[live-vs-snapshot-credentials]] (concept)
- ADR: `docs/decisions/2026-05-25-live-ca-fetch-clustercredential-adr.md`
- LIVE evidence (Op B fetch + fingerprint match): `.claude/knowledge-base/reviews/2026-05-26-op-b-live-evidence.md`
- LIVE evidence (dogfood 112/0): `.claude/knowledge-base/reviews/2026-05-26-dogfood-112-0-pass.md`
- CLAUDE.md § Multi-Cell Routing
- CLAUDE.md "Live vs snapshot credentials" Known Systemic Pattern
