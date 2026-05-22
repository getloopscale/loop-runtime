# Cost Attribution per Tenant (OpenCost)

Operator-facing guide for the OpenCost integration adopted via plan
`docs/plans/2026-05-22-shell-script-strategic-plan.md` v1.3 Phase 7 T7.2.

---

## Why this exists

The LoopZero pricing model differentiates tiers (Starter Free / Pro $29 / Team $99 /
Enterprise custom) based on per-tenant project caps. **Without cost attribution
per tenant, we cannot validate** whether the cap-de-plano covers actual cluster
cost — a Sprint 9 GA gate.

OpenCost was adopted (not reimplemented as `pkg/quality/costattribution/`) per
Anti-Pattern #8 of the plan ("adopt > reinvent" for mature external tools).

---

## Topology

- **One central install** in mgmt cluster, namespace `loopzero-system`
- Integrated with **existing Prometheus** at `monitoring/prometheus` (NOT internal
  Prometheus from OpenCost chart — would duplicate scrape)
- **Federation-friendly**: Prometheus already federates cell metrics → single
  OpenCost install covers all cells (no per-cell deployment cost)
- Tenant attribution via **namespace label** `loopzero.io/tenant` (LoopZero platform
  emits this label on all workload namespaces)

---

## Configuration

### Pricing source (DigitalOcean)

OpenCost defaults assume AWS pricing. LoopZero runs on DigitalOcean. The wrapper
chart at `infra/helm/opencost/values.yaml` configures:

```yaml
opencost:
  opencost:
    customPricing:
      enabled: true
      provider: custom
      configmapName: opencost-pricing-do
      createConfigmap: true
```

Operator MUST populate the ConfigMap with current DO on-demand pricing.
Sample values (as of 2026-05; update from https://www.digitalocean.com/pricing):

| Node SKU | CPU ($/hr) | RAM ($/GB-hr) | Notes |
|----------|-----------|---------------|-------|
| s-2vcpu-4gb | 0.030 | 0.0075 | Worker default |
| s-4vcpu-8gb | 0.060 | 0.0075 | mgmt baseline |
| s-8vcpu-16gb | 0.119 | 0.0075 | runtime scale |

ConfigMap example:

```bash
kubectl create configmap opencost-pricing-do \
  --namespace=loopzero-system \
  --from-literal=CPU=0.030 \
  --from-literal=RAM=0.0075 \
  --from-literal=GPU=0.0 \
  --from-literal=storage=0.0001
```

### Attribution queries

Query Prometheus directly (Grafana already federates):

```promql
# Total cost per namespace (per-tenant), rolling hour
sum by (namespace) (
  kubecost_namespace_cost_per_hour
)

# Cost per LoopZero tier (requires label_loopzero_io_plan to be exported)
sum by (label_loopzero_io_plan) (
  kubecost_namespace_cost_per_hour
)

# Top 10 expensive tenants
topk(10, sum by (namespace) (kubecost_namespace_cost_per_hour))
```

---

## Validation post-install (operator HARD STOP)

After Helmfile sync:

```bash
# 1. Pod Running
kubectl --context k3d-loopzero-mgmt -n loopzero-system get pods -l app.kubernetes.io/name=opencost
# Expected: 1/1 Running

# 2. Metrics endpoint responsive
kubectl --context k3d-loopzero-mgmt -n loopzero-system port-forward svc/opencost 9003:9003 &
curl -sf http://localhost:9003/metrics | grep -c "kubecost_"
# Expected: positive integer (cost metrics present)

# 3. Prometheus scraping
kubectl --context k3d-loopzero-mgmt -n monitoring port-forward svc/prometheus-server 9090:80 &
curl -sf 'http://localhost:9090/api/v1/query?query=up{job="opencost"}' | jq '.data.result[0].value[1]'
# Expected: "1" (scrape working)

# 4. Per-tenant query (requires at least 1 tenant deployed)
curl -sG 'http://localhost:9090/api/v1/query' \
  --data-urlencode 'query=sum by (namespace) (kubecost_namespace_cost_per_hour)' \
  | jq '.data.result | length'
# Expected: count of active tenant namespaces (>0 after first deploy)
```

---

## Grafana dashboard

OpenCost ships a community dashboard. Import via Grafana UI → Dashboards →
Import → use dashboard ID `15714` (OpenCost community dashboard) OR JSON
from `infra/helm/monitoring/dashboards/opencost.json` (operator may custom).

---

## Limitations + decisions documented

- **Cost data lags ~15min** — OpenCost rolling window. Real-time decisions should
  use Prometheus metrics directly, not OpenCost UI.
- **DO Spaces / DOCR costs NOT attributed per tenant** — those are mgmt-cluster
  costs, attributed to platform overhead. Operator may add manual allocation
  via OpenCost customPricing if needed.
- **Reserved instances**: DO has no equivalent of AWS RIs in 2026; pricing model
  is on-demand. If DO introduces reserved tiers, customPricing ConfigMap must
  be updated accordingly.

---

## References

- OpenCost upstream: https://opencost.io/
- Helm chart: https://github.com/opencost/opencost-helm-chart
- LoopZero wrapper chart: `infra/helm/opencost/`
- Helmfile entry: `infra/helmfile/mgmt.helmfile.yaml.gotmpl` (look for `name: opencost`)
- ServiceMonitor: `infra/helm/monitoring/manifests/servicemonitor-opencost.yaml`
- Hermetic tests: `pkg/quality/opencostgate/check_test.go`
- Plan section: `docs/plans/2026-05-22-shell-script-strategic-plan.md` Phase 7 T7.2
