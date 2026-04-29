# LoopZero Observability Runbook

> **Audiência**: operators / SREs configurando Prometheus, Grafana, Alertmanager
> **Status**: F-08 (CHG-2026-04-29-DOGFOOD T5.3) — endpoint inventory + scrape config
> **Sister docs**: `secrets-rotation.md`, `bootstrap-regression-matrix.md`

## Endpoint inventory

A aplicação `loopzero-api` expõe **dois listeners distintos** (intencional):

| Port | Path                         | Auth     | Quem usa                                    |
|------|------------------------------|----------|---------------------------------------------|
| 3001 | `/api/v1/health`             | None     | livenessProbe, readinessProbe, smoke tests  |
| 3001 | `/api/v1/health/deep`        | None     | deep-readiness com check de DB+ArgoCD+Gitea |
| 3001 | `/api/v1/version`            | None     | dogfood/CI smoke (F-07)                     |
| 3001 | `/api/v1/*` (resto)          | Bearer   | CLI + dashboard                             |
| 9090 | `/metrics`                   | None     | Prometheus scrape (interno-only)            |
| 9090 | `/debug/pprof/*`             | None     | profiling local (não exposto em produção)   |

### Por que `/metrics` em port 9090 e não em 3001?

Three-fold reason:

1. **Blast-radius:** o serviço de métricas é separado do plano de dados.
   Metrics endpoint compromised → atacante NÃO ganha acesso às rotas auth.
2. **Network policy granularidade:** Prometheus scrape namespace pode ter
   ingress permitido para `:9090` apenas, sem abrir `:3001`. Em standalone-multi
   ainda não temos NP por port, mas a estrutura está pronta.
3. **Conformidade Prometheus:** `/metrics` em port dedicado é o padrão da
   comunidade (kube-state-metrics, node-exporter, todas seguem o pattern).

`curl http://localhost:13001/metrics` retorna **404 esperado** — esse port
é o `service.port=3001`, não o metrics listener. **Não é bug.**

## Scrape configuration

### Standalone-multi (Prometheus standalone, sem Operator)

ServiceMonitor é opt-in via `serviceMonitor.enabled=true` (default `false`)
porque prometheus-operator não está instalado. Para scraping:

**Option A — usar ServiceMonitor:** install prometheus-operator no overlay
mgmt (`infra/helmfile/mgmt.helmfile.yaml.gotmpl`) e setar `serviceMonitor.
enabled=true` no values overlay. ServiceMonitor renderizará automaticamente.

**Option B — config direto no Prometheus standalone:**

```yaml
scrape_configs:
  - job_name: loopzero-api
    static_configs:
      - targets:
          - loopzero-api.loopzero-system.svc.cluster.local:9090
    metric_relabel_configs:
      # LoopZero emite labels canônicos (loopzero_*); Prometheus não deve overwrite.
      - regex: __meta_kubernetes_pod_label_(.+)
        action: labeldrop
```

### Dev / Prod (com prometheus-operator)

Overlay `values-dev.yaml` / `values-prod.yaml` deve ter:

```yaml
serviceMonitor:
  enabled: true
  interval: 30s
  scrapeTimeout: 10s
  labels:
    # Match prometheus-operator selector. Default em kube-prometheus-stack:
    #   prometheus.spec.serviceMonitorSelector.matchLabels = {release: prometheus}
    release: prometheus
```

Verifique selector com:

```bash
kubectl get prometheus -n monitoring -o jsonpath='{.items[0].spec.serviceMonitorSelector}'
```

## Inventário de métricas

`loopzero-api` emite **200+ métricas custom (`loopzero_*`)** + métricas Go padrão (`go_*`,
`process_*`). Categorias principais:

| Prefix                              | Tipo / cardinalidade | Significado |
|-------------------------------------|----------------------|-------------|
| `loopzero_argocd_auth_failures_total`   | counter / low        | Auth failures contra ArgoCD (excluí circuit breaker) |
| `loopzero_argocd_sync_duration_seconds` | histogram / med      | Latência do trigger sync por status |
| `loopzero_build_*`                      | counter+gauge / med  | Build orchestration: queued, running, succeeded, failed |
| `loopzero_cell_tenant_count`            | gauge / low          | Tenants por cell (ADR-0008) |
| `loopzero_cell_discovery_count`         | gauge / 1            | Cells descobertos pelo router |
| `loopzero_db_query_duration_seconds`    | histogram / med      | Latência de queries por route |
| `loopzero_deploy_*`                     | counter+gauge / med  | Deploys por status, per-tenant |
| `loopzero_env_lock_*`                   | counter / low        | pg_advisory_xact_lock acquisitions / contentions |
| `loopzero_idempotency_*`                | counter / low        | Idempotency-Key replays detectados |
| `loopzero_release_*`                    | counter+gauge / med  | Release state machine transitions |

Para ver lista completa contra o cluster ativo:

```bash
kubectl exec -n loopzero-system deploy/loopzero-api -- \
  wget -qO- http://localhost:9090/metrics | grep "^# HELP loopzero_"
```

## SLO alerts (Grafana)

Os arquivos de alert PrometheusRule estão em `infra/helm/monitoring/`. Em
standalone-multi os alerts ficam disabled (`alerts.enabled: false`, B64 fix)
porque não há Alertmanager. Em dev/prod são ativados via overlay.

Critical SLOs:
- `loopzero_api_request_duration_seconds_bucket` p99 > 1s por 5 min → page
- `loopzero_argocd_sync_duration_seconds_bucket` p95 > 60s por 10 min → page
- `loopzero_build_duration_seconds_bucket` p95 > 300s por 10 min → ticket
- `up{job="loopzero-api"} == 0` por 2 min → page

## Troubleshooting

### `/api/v1/health/deep` retorna `{"status":"degraded"}`

Possíveis causas (parsed do JSON body):
- `argocd: auth_failed` → `ARGOCD_TOKEN` ausente ou expirado. Re-run
  `bash infra/helmfile/scripts/generate-secrets.sh` para regenerar (F-12).
- `gitea: <error>` → Gitea pod down ou `GITEA_TOKEN` inválido. Check
  `kubectl logs -n loopzero-system -l app.kubernetes.io/name=gitea`.
- `database: <error>` → PG down. Check `kubectl get pod -l app.kubernetes.io
  /name=postgresql -n loopzero-system`.

### Prometheus scrape vazio (`up{job="loopzero-api"}==0`)

```bash
# 1. Verificar Service tem 9090 port
kubectl get svc loopzero-api -n loopzero-system -o jsonpath='{.spec.ports[?(@.name=="metrics")]}'

# 2. Verificar pod expõe 9090
kubectl get pod -n loopzero-system -l app=loopzero-api \
  -o jsonpath='{.items[0].spec.containers[0].ports}'

# 3. Test scrape manual
kubectl run prom-probe -i --rm --restart=Never --image=curlimages/curl \
  -- curl -s http://loopzero-api.loopzero-system.svc.cluster.local:9090/metrics | head

# 4. ServiceMonitor labels matching prometheus selector
kubectl get servicemonitor loopzero-api -n loopzero-system -o yaml | grep -A3 labels
```

### NetworkPolicy bloqueando scrape

Em standalone-multi não há NP por port. Se em produção a scrape começar a
falhar após mudança de chart, audite `infra/helm/network-policies/templates
/loopzero-system.yaml` por uma regra que restrinja ingress no Service `loopzero-api`
e adicione exceção do namespace `monitoring`.
