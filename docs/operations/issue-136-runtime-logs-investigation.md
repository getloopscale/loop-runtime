# #136 — Runtime logs vazios — Investigation 2026-06-22 (Run-7)

**Status:** ROOT CAUSE IDENTIFIED; canonical fix in helmfile overlay (not LIVE patch).
**Severity:** medium (deploy/serving healthy; observability gap on runtime stdout/stderr).

## TL;DR

`loopzero logs --runtime` retorna vazio porque o **Promtail no workload cluster
não descobre pods em namespaces `loopzero-tn-*`**. O canonical Loki cross-cluster
chain (build cluster Loki + loopzero-api wired to it) está sano, mas o produtor
(Promtail workload) só varre 2 jobs de scrape: `argo-workflow-pods` +
`vault-audit`. Tenant pods nunca chegam ao Loki.

## Evidence

### Loki has data only from non-tenant namespaces

```
$ curl -G ... http://172.18.0.7:30120/loki/api/v1/label/namespace/values
data: ["argo", "argo-ci", "loopzero-tn-2b39686d51d10c02-production"]
```

Note: the only `loopzero-tn-*` namespace in Loki is a **stale** one
(`tn-2b39686d51d10c02`, not the active `tn-6864661cd10bd134`) — left over
from a prior deploy when the scrape was lucky. After Cleanroom-fresh, the
workload Promtail config no longer covers tenant pods.

### Promtail workload boot logs

```
level=info config=argo-workflow-pods msg="Using pod service account via in-cluster config"
level=info config=vault-audit msg="Using pod service account via in-cluster config"
```

Only 2 jobs discovered. No `loopzero-tenant-pods` job for `loopzero-tn-*` ns
prefix.

### App stdout is being emitted

```
$ kubectl logs deploy/loopzerokit-example-app -n loopzero-tn-...-production
  LoopZero production server
  → http://localhost:3000
```

So the app emits stdout — the gap is purely the **scrape side**.

### Promtail clients.url repointed to canonical (LIVE patch applied 2026-06-22)

Before: `url: http://172.18.0.3:30094/loki/api/v1/push` (mgmt
cluster `loki-cross-cluster` NodePort — split brain)

After: `url: http://172.18.0.7:30120/loki/api/v1/push` (build
cluster Loki canonical, same as loopzero-api LOKI_URL per
seed-loki-cross-cluster.sh).

But this alone does NOT fix #136 — even with the correct destination,
Promtail does not have a scrape_config that targets tenant namespaces.

## Canonical fix (deferred — helmfile overlay change)

Add a `loopzero-tenant-pods` scrape job to `infra/helm/promtail/values.yaml`
(workload cluster overlay):

```yaml
scrape_configs:
  - job_name: loopzero-tenant-pods
    kubernetes_sd_configs:
      - role: pod
    relabel_configs:
      - source_labels: [__meta_kubernetes_namespace]
        action: keep
        regex: 'loopzero-tn-.*'
      - source_labels: [__meta_kubernetes_pod_label_app_kubernetes_io_managed_by]
        action: keep
        regex: 'loopzero'
      - source_labels: [__meta_kubernetes_namespace]
        target_label: namespace
      - source_labels: [__meta_kubernetes_pod_name]
        target_label: pod
      - source_labels: [__meta_kubernetes_pod_label_app]
        target_label: app
    pipeline_stages:
      - cri: {}
```

Then `helmfile sync --selector name=promtail` to apply.

Per `j17-aprendizado-promtail-loki-pipeline-canonical-via-helmfile`
memory: codified workaround via runtime patch is NOT canonical — the
fix must land in the chart values overlay.

## Out of scope for this LIVE session

The LIVE patch (clients.url repoint) is sufficient to confirm the gap is
on the scrape side, not the egress side. The canonical fix requires
chart-values edit + helmfile sync + Cleanroom-fresh validation — that's
a separate slice.

## Cross-references

- Memory: `j17-aprendizado-promtail-loki-pipeline-canonical-via-helmfile`
- Canonical Loki chain seed: `infra/scripts/local/seed-loki-cross-cluster.sh`
- loopzero-api Loki URL config: loopzero-api Deployment env `LOKI_URL=http://172.18.0.7:30120`
- Build cluster Loki: NodePort 30120 (per seed-loki-cross-cluster)
- Cleanroom mandate: `cleanroom-up-live-test-mandate.md`
