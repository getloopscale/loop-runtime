---
type: architecture
title: AS-IS architecture (post Op B)
---

<!-- AS-IS STATE -->
---
name: clustercredential-architecture
description: Platform domain — ClusterCredential CRD reconcile flow + ArgoCD cluster Secret materialization
type: entity
created_at: 2026-05-26
updated_at: 2026-05-26
updated_by: ralph-loop
evidence_class: MEASURED
---

# ClusterCredential — AS-IS architecture (post Op B)

> Plan: [`docs/plans/completed/2026-05-25-live-ca-fetch-clustercredential-plan.md`](../../plans/completed/2026-05-25-live-ca-fetch-clustercredential-plan.md) v1.2 — T0.1 Pre-Plan
> Code references: `operators/internal/platform/clustercredential_controller.go` + `operators/internal/platform/ca_fetcher.go`
> LIVE evidence: [[2026-05-26-op-b-live-evidence]]

## Domain scope

The `platform` domain owns the operator-level bridge between tenant clusters and ArgoCD. The `ClusterCredential` CRD is the API contract for "how LoopZero talks to a tenant cluster".

## C4 Container view

```
┌─────────────────────────── mgmt cluster ───────────────────────────┐
│                                                                    │
│   ┌─────────────────────────┐                                      │
│   │ ClusterCredential CR    │   (loopzero.io/v1)                       │
│   │   spec.clusterName      │                                      │
│   │   spec.apiServer        │                                      │
│   │   spec.secretRef        │── kubeconfig Secret (loopzero-system)    │
│   │   spec.tls.insecure     │── Op A precedence (D4 short-circuit) │
│   │   spec.cellID           │                                      │
│   └────────┬────────────────┘                                      │
│            │                                                       │
│            v                                                       │
│   ┌─────────────────────────────────────────────────────────────┐  │
│   │ ClusterCredentialReconciler (platform-operator)             │  │
│   │   1. resolveKubeconfig() → reads Secret                     │  │
│   │   2. checkConnectivity() → HTTPS to apiServer               │  │
│   │   3. reconcileArgoCDClusterSecret()                         │  │
│   │      └─ resolveCAData()  [Op B per ADR D1-D5]               │  │
│   │           ├─ D4 short-circuit insecure=true → "" + counter  │  │
│   │           ├─ D1 live fetch kube-root-ca.crt (5s budget)     │  │
│   │           ├─ D2 fallback to kubeconfig snapshot on error    │  │
│   │           └─ counter loopzero_clustercredential_ca_fetch_total  │  │
│   └────────┬────────────────────────────────────────────────────┘  │
│            │                                                       │
│            v writes                                                │
│   ┌──────────────────────────────────┐                             │
│   │ ArgoCD cluster Secret             │   argocd namespace         │
│   │   data.config (JSON):             │                            │
│   │     bearerToken                   │                            │
│   │     tlsClientConfig.insecure      │                            │
│   │     tlsClientConfig.caData (b64)  │                            │
│   │   label argocd.argoproj.io/       │                            │
│   │         secret-type=cluster       │                            │
│   └──────────────────────────────────┘                             │
└────────────────────────────────────────────────────────────────────┘
```

## Reconcile sequence (Op B path post 2026-05-25)

```
ClusterCredentialReconciler.Reconcile(ctx, req)
  └─ resolveKubeconfig(cr)                    [MEASURED] controller.go:161-203
       └─ Get Secret cr.Spec.SecretRef
       └─ extract data["kubeconfig"]
  └─ reconcileArgoCDClusterSecret(cr, kubeconfig)
       └─ RESTConfigFromKubeConfig(kubeconfig) [MEASURED] controller.go:300
       └─ resolveCAData(ctx, cr, restConfig)
            ├─ if cr.Spec.TLS.Insecure:
            │    counter += {result="skipped-insecure"}
            │    return "", "skipped-insecure"
            ├─ caFetcher.FetchClusterCA(restConfig.clone(Insecure=true))
            │    └─ GET default/kube-root-ca.crt via in-cluster client
            │    └─ EC-13 bootstrap: clone with TLSClientConfig{Insecure:true}
            ├─ on success: counter += {result="success"}
            │    return base64(livePEM), "live"
            └─ on error: counter += {result="fallback"}
                 return base64(snapshot.CAData), "fallback"
  └─ checkConnectivity(cr, kubeconfig)         [MEASURED] controller.go:208-266
  └─ status.Connected = true/false
```

## Source-of-truth files

| File | Lines | What |
|---|---|---|
| `operators/internal/platform/clustercredential_controller.go` | 90-160, 280-453 | Reconcile + resolveCAData logic |
| `operators/internal/platform/ca_fetcher.go` | full | `caFetcher` interface + `kubeRootCAFetcher` default impl |
| `operators/internal/platform/ca_fetcher_test.go` | full | 10 unit tests for fetcher |
| `operators/internal/platform/op_b_envtest_test.go` | full | 4 envtest specs (live/fallback/short-circuit/idempotency) |
| `operators/internal/metrics/metrics.go` | counter decl | `ClusterCredentialCAFetchTotal{cluster,result}` |
| `operators/api/v1/clustercredential_types.go` | TLS struct | `spec.tls.insecure` field |

## Failure modes

| Mode | Symptom | Counter result | Recovery |
|---|---|---|---|
| Stale kubeconfig CA (Issue #67) | ArgoCD app-controller x509 errors | `result="success"` (Op B overrides snapshot) | Self-healing on next reconcile (60s) |
| RBAC denial for kube-root-ca.crt | Fetch returns error | `result="fallback"` (graceful degradation) | SRE alert via Grafana on sustained rate |
| `insecure=true` (Op A active) | Op B skipped | `result="skipped-insecure"` | By design — operator decision per #67 closure |
| kubeconfig Secret missing | Reconcile re-queued 30s | n/a (no fetch attempt) | Operator must create Secret |
| Cluster unreachable | `status.Connected=false` | n/a | Network/firewall issue |

## Cross-references

- [[live-vs-snapshot-credentials]] — Concept page on the Op A vs Op B pattern
- ADR: [`docs/decisions/2026-05-25-live-ca-fetch-clustercredential-adr.md`](../../decisions/2026-05-25-live-ca-fetch-clustercredential-adr.md)
- Meeting: `.claude/knowledge-base/meetings/MEET-2026-05-25-op-b-live-ca-fetch.md`
- LIVE evidence: `.claude/knowledge-base/reviews/2026-05-26-op-b-live-evidence.md`
- Dogfood evidence: `.claude/knowledge-base/reviews/2026-05-26-dogfood-112-0-pass.md`
- Pattern in CLAUDE.md: "Live vs snapshot credentials" + "Bootstrap circular dependency (X-fetches-X)"
