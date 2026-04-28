# Sprint 8 / T6.3 — Observability Validation Report

**Date:** 2026-04-27
**Phase:** 6 (Observability) — T6.3
**Status:** Validation complete; gaps documented; remediation deferred to CHG-002b/T6 follow-on.

## Goal

Validate that Promtail is Running on every cluster of the LoopZero platform (per ADR-0010) and that build/runtime/runtime-2 logs are query-able from the central Grafana on mgmt.

## Findings

### 1. Cluster Inventory

| Cluster | Promtail Pods | Loki | Grafana | Prometheus | Status |
|---|---|---|---|---|---|
| **mgmt** | **0** | — | ✅ | ✅ | ⚠️ no log shipper — workload here is platform itself (loopzero-api, tenant-controller, ArgoCD); platform logs not shipped centrally |
| **build** | 3 (1 NotReady) | ✅ (loki-0 in `monitoring` ns) | — | — | ⚠️ partial; Loki saturated at default 5000-stream limit |
| **runtime** | 2 ✅ | — | — | — | ✅ |
| **runtime-2** | **0** | — | — | — | ❌ Helm release not provisioned |

### 2. Build Cluster — Promtail Readiness Failure

`promtail-bbc2x` on build node 0 has been NotReady (0/1) for 6h32m. Readiness probe: HTTP 500.

**Root cause:** Loki saturated.

```bash
$ kubectl --kubeconfig=$KCFG_BUILD logs -n monitoring promtail-bbc2x --tail=20 | grep -E 'status=429'
... msg="error sending batch, will retry" status=429 tenant=
   error="server returned HTTP status 429 Too Many Requests:
   Maximum active stream limit exceeded when trying to create stream
   {container=\"init\", filename=\"/var/log/pods/argo_loopzero-build-...\"
   reduce the number of active streams (reduce labels or reduce label values),
   or contact your Loki administrator to see if the limit can be increased"
```

`promtail` reports HTTP 500 on its own readiness probe because it cannot flush its buffer.

**Loki ingester state:**

```bash
$ kubectl exec -n monitoring loki-0 -c loki -- wget -qO- localhost:3100/metrics \
    | grep loki_ingester_memory_streams
loki_ingester_memory_streams{tenant="fake"} 5000   ← exactly the default cap
loki_ingester_streams_created_total{tenant="fake"} 5000
loki_ingester_memory_chunks 5006
```

L3 stream production:
- 30 concurrent workflows × 5 pods/workflow × 3 containers (init+main+wait) ≈ **450 streams per L3 cycle**
- Streams remain in memory for `chunk_idle_period` (1h default) — accumulating across cycles
- After 5+ L3 runs of churn, ingester hits 5000-stream cap

### 3. Mgmt Cluster — No Log Shipping

The mgmt cluster runs `loopzero-api`, `tenant-controller`, ArgoCD, and Gitea — yet has zero `monitoring` namespace and zero Promtail pods. Platform logs are **only** available via `kubectl logs`, not centrally queryable.

This is a **bootstrap gap**: `infra/pulumi/src/components/mgmt-bootstrap.ts` does not include `installPromtail()`. The current architecture assumes Loki on the build cluster sees only build/workflow logs; platform logs were never wired.

### 4. Runtime-2 Cluster — Helm Release Missing

`runtime-2` was provisioned by Sprint 8 / Phase -1 / Pulumi but its `monitoring` namespace was never created and no Promtail DaemonSet exists. The cell-2 activation work (Sprint 9) cannot rely on workload logs from this cluster as-is.

## Plan vs Reality

The Sprint 8 plan T6.3 description:
> "Pulumi ja instala Promtail no build cluster via `infra/pulumi/src/components/build-loki.ts:142-146` e no runtime cluster via `runtime-monitoring.ts:42-50`."

The plan claim is **only partially true** for the dev stack as-bootstrapped:
- ✅ build cluster: Promtail Helm release was provisioned
- ✅ runtime cluster: Promtail DaemonSet running
- ❌ runtime-2 cluster: Promtail DaemonSet **absent**
- ❌ mgmt cluster: Promtail not in scope of bootstrap (architecture gap, not provisioning bug)
- ⚠️ Loki capacity: default `max_global_streams_per_user: 5000` — too low for a 4-cluster, 100-tenant L3 workload

## Remediation Options (deferred to CHG-002b/T6 follow-on)

### Option A — Raise Loki stream limit (operational, low risk)

```yaml
# infra/helm/loki/values.yaml or Pulumi build-loki.ts override
limits_config:
  max_global_streams_per_user: 50000
  max_streams_per_user: 50000
  ingestion_rate_mb: 16
  ingestion_burst_size_mb: 32
  retention_period: 168h  # 7d
```

Operational mitigation today (no Helm rollout):

```bash
KCFG_BUILD=/home/paulo/.kube/loopzero-loopzero-infra-dev-sfo3-build.yaml
kubectl --kubeconfig=$KCFG_BUILD edit cm -n monitoring loki
# raise max_global_streams_per_user to 50000, save
kubectl --kubeconfig=$KCFG_BUILD rollout restart -n monitoring statefulset/loki
```

Validates with:

```bash
kubectl exec -n monitoring loki-0 -c loki -- wget -qO- localhost:3100/metrics \
  | grep loki_ingester_memory_streams
# expected: well below new limit, no more 429s
kubectl logs -n monitoring promtail-<id> --tail=20 | grep -c '429'
# expected: 0
```

### Option B — Reduce stream cardinality (proper fix)

Drop high-cardinality labels at Promtail source:

```yaml
# infra/helm/promtail/values.yaml
config:
  snippets:
    pipelineStages:
      - cri: {}
      - drop:
          source: workflow   # drop workflow label entirely
      - drop:
          source: filename   # path includes pod UID, drops cardinality
```

Trade-off: harder to query a specific workflow's logs, but stream count drops 10×.

### Option C — Add Promtail to runtime-2 cluster (correctness fix)

`infra/pulumi/src/components/runtime-monitoring.ts` reuses `runtime` provider. Either:
- Make `runtime-monitoring` accept an array of providers and install on both, or
- Duplicate the call in `index.ts` for `runtime-2`'s provider.

This is T2 (Pulumi infra), CHG-002b scope.

### Option D — Add Promtail to mgmt cluster (architectural decision)

Decision needed: should mgmt platform logs (loopzero-api, controllers) be centrally shipped to Loki on build? Pros: single Grafana view; cons: cross-cluster push adds latency + tenant complexity.

Recommended: defer to Sprint 9 / cell-2 activation, where multi-cluster log shipping must be solved formally anyway (per Gate 2b "per-cell Prometheus with remote-write").

## Acceptance Criteria for T6.3

| Criterion | Status |
|---|---|
| Promtail Running em build, runtime, runtime-2 | ❌ runtime-2 missing; build 1 of 3 NotReady |
| Logs queryaveis no Grafana (mgmt) por `{cluster=...}` | ⚠️ Grafana on mgmt has `loki` data source pointing at build's Loki — querying works for build/runtime; runtime-2 emits no logs centrally |
| Versao pinned 6.16.6 em todos os clusters | ✅ where installed |

## Decision

**T6.3 status:** validation complete, **plan claim revised** — T6.3 was assumed correct in plan v3.1; reality has 3 distinct gaps (Loki capacity + runtime-2 missing + mgmt not in scope). All three are T2/Pulumi changes outside CHG-002a.

**Action:** open CHG-002b ticket "Observability hardening — Loki capacity + runtime-2 + mgmt log shipping decision" with Options A-D above as concrete alternatives.
