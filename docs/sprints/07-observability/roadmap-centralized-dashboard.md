# Roadmap: Dashboard Centralizado de Observabilidade

> **Atividade paralela** ao Sprint 2 (deploy happy path).
> **Referencia:** spec §15 (Observability), Sprint 7 (sprint.md)
> **Branch:** `feat/observability-dashboard`
> **Objetivo:** Grafana como ponto unico de visibilidade sobre todas as ferramentas do sistema — dev e producao.

---

## Estado Atual

### Implementado
- [x] Prometheus (kube-prometheus-stack) no cluster mgmt, retention 15d
- [x] Grafana com sidecar para dashboards (NodePort 30002)
- [x] OTel Collector (OTLP gRPC/HTTP → Prometheus exporter)
- [x] 23 metricas da API LoopZero (request, build, deploy, lock, idempotency, SSE, DB pool)
- [x] ServiceMonitor scraping `loopzero-api` a cada 15s
- [x] 3 dashboards JSON (Platform Overview, Build Pipeline, Per-Tenant Health)
- [x] Middleware de metricas HTTP com testes
- [x] `/metrics` endpoint na API (promhttp.Handler)

### Implementado (2026-03-14)
- [x] Scraping de ArgoCD (chart ServiceMonitor), Gitea, MinIO (ServiceMonitors manuais)
- [x] Scraping cross-cluster: Argo Workflows (build:30090), Traefik (workload:30091)
- [x] PostgreSQL metrics ServiceMonitor via Bitnami chart
- [x] Dashboard Home (visao centralizada) — 20 paineis, links para todos os dashboards
- [x] Dashboard Drift Detection — 12 paineis, variables tenant/env
- [x] Dashboard Failure Investigation — 18 paineis, variables tenant/env
- [x] Links de navegacao em todos os 6 dashboards (hub-and-spoke)
- [x] 23 PrometheusRule alerting rules (11 critical + 12 warning), todas com runbook
- [x] Alertmanager habilitado com routing por severity
- [x] Taskfile: monitoring:install, monitoring:validate-targets, monitoring:status
- [x] CHANGELOG atualizado

### Pendente (requer clusters ativos)
- [ ] Notification channels producao (Slack/email) — receivers em dev apontam para dev-null
- [ ] Teste de alertas via simulacao (scale down PostgreSQL, Gitea)
- [ ] Validacao end-to-end com `task monitoring:validate-targets`

---

## Fase 1 — Scraping de Todas as Fontes

> **Objetivo:** Prometheus coleta metricas de todas as ferramentas. Zero dashboards novos nesta fase.
> **Entregavel:** `curl prometheus:9090/api/v1/targets` mostra todos os targets UP.

### Task 1.1 — ServiceMonitor: ArgoCD

**Arquivo:** `infra/helm/monitoring/manifests/servicemonitor-argocd.yaml`

ArgoCD expoe `/metrics` nos seguintes componentes:
- `argocd-server` (porta 8083)
- `argocd-application-controller` (porta 8082)
- `argocd-repo-server` (porta 8084)

**Metricas-chave a validar:**
- `argocd_app_info` — estado dos apps
- `argocd_app_sync_total` — syncs por status
- `argocd_app_reconcile_duration_seconds` — latencia de reconciliacao
- `argocd_cluster_api_resources` — recursos no cluster

**Criterio de aceite:**
- [ ] ServiceMonitor criado com label `release: monitoring`
- [ ] `namespaceSelector` apontando para namespace do ArgoCD
- [ ] Prometheus target mostra ArgoCD como UP
- [ ] Query `argocd_app_info` retorna dados no Prometheus

---

### Task 1.2 — ServiceMonitor: Argo Workflows

**Arquivo:** `infra/helm/monitoring/manifests/servicemonitor-argo-workflows.yaml`

Argo Workflows expoe `/metrics` no controller (porta 9090).

**Metricas-chave a validar:**
- `argo_workflows_count` — workflows por status
- `argo_workflows_pods_count` — pods ativos
- `argo_workflows_queue_depth_count` — profundidade da fila
- `argo_workflows_operation_duration_seconds` — duracao de operacoes

**Criterio de aceite:**
- [ ] ServiceMonitor criado com label `release: monitoring`
- [ ] Prometheus target mostra Argo Workflows controller como UP
- [ ] Query `argo_workflows_count` retorna dados

---

### Task 1.3 — ServiceMonitor: Gitea

**Arquivo:** `infra/helm/monitoring/manifests/servicemonitor-gitea.yaml`

Gitea expoe `/metrics` na porta HTTP (3000) quando `ENABLE_METRICS=true` no `app.ini`.

**Pre-requisito:** Validar que o Helm chart do Gitea tem `metrics.enabled=true`. Se nao, adicionar ao values.

**Metricas-chave a validar:**
- `gitea_accesses` — total de acessos
- `gitea_issues` / `gitea_repositories` — contagem de recursos
- `gitea_users` — usuarios ativos
- `process_resident_memory_bytes` — uso de memoria

**Criterio de aceite:**
- [ ] Gitea configurado com `ENABLE_METRICS=true`
- [ ] ServiceMonitor criado
- [ ] Prometheus target mostra Gitea como UP
- [ ] Query `gitea_repositories` retorna dados

---

### Task 1.4 — ServiceMonitor: Traefik

**Arquivo:** `infra/helm/monitoring/manifests/servicemonitor-traefik.yaml`

Traefik expoe `/metrics` nativamente (Prometheus provider). O kube-prometheus-stack pode ja incluir scraping via annotations.

**Metricas-chave a validar:**
- `traefik_entrypoint_requests_total` — requests por entrypoint
- `traefik_entrypoint_request_duration_seconds_bucket` — latencia
- `traefik_tls_certs_not_after` — expiracao de certificados TLS

**Criterio de aceite:**
- [ ] ServiceMonitor ou PodMonitor criado
- [ ] Prometheus target mostra Traefik como UP
- [ ] Query `traefik_entrypoint_requests_total` retorna dados

---

### Task 1.5 — ServiceMonitor: MinIO

**Arquivo:** `infra/helm/monitoring/manifests/servicemonitor-minio.yaml`

MinIO expoe `/minio/v2/metrics/cluster` (porta 9000) com bearer token.

**Pre-requisito:** Gerar token de metricas via `mc admin prometheus generate`.

**Metricas-chave a validar:**
- `minio_s3_requests_total` — requests por operacao (GET, PUT, DELETE)
- `minio_bucket_usage_total_bytes` — uso de storage por bucket
- `minio_node_disk_free_bytes` — espaco livre

**Criterio de aceite:**
- [ ] Token de metricas provisionado (secret no namespace monitoring)
- [ ] ServiceMonitor com `bearerTokenSecret`
- [ ] Prometheus target mostra MinIO como UP
- [ ] Query `minio_s3_requests_total` retorna dados

---

### Task 1.6 — ServiceMonitor: PostgreSQL

**Arquivo:** `infra/helm/monitoring/manifests/servicemonitor-postgres.yaml`

Se o Helm chart do PostgreSQL usa o sidecar `postgres-exporter`, ele expoe `/metrics` na porta 9187.

**Metricas-chave a validar:**
- `pg_up` — health (usada no alerta `LoopZeroPostgresDown`)
- `pg_stat_activity_count` — conexoes ativas
- `pg_stat_database_tup_fetched` — queries
- `pg_locks_count` — locks ativos (advisory locks visiveis aqui)

**Criterio de aceite:**
- [ ] postgres-exporter configurado no Helm chart do PostgreSQL
- [ ] ServiceMonitor criado
- [ ] Prometheus target mostra PostgreSQL exporter como UP
- [ ] Query `pg_up` retorna 1

---

### Task 1.7 — Validacao End-to-End do Scraping

**Comando:** `task monitoring:validate-targets` (novo task no Taskfile)

Script que valida via Prometheus API que todos os targets estao UP:

```bash
TARGETS="loopzero-api argocd argo-workflows gitea traefik minio postgres"
for target in $TARGETS; do
  status=$(curl -s prometheus:9090/api/v1/targets | jq -r ".data.activeTargets[] | select(.labels.job | contains(\"$target\")) | .health")
  echo "$target: $status"
done
```

**Criterio de aceite:**
- [ ] Todos os 7 targets reportam `health: up`
- [ ] Task `monitoring:validate-targets` existe e passa

---

## Fase 2 — Dashboards

> **Objetivo:** Visibilidade completa via Grafana. 6 dashboards totais (3 existentes + 3 novos).
> **Entregavel:** Grafana folder "LoopZero" com todos os dashboards operacionais.
> **Dependencia:** Fase 1 completa (todos os targets UP).

### Task 2.1 — Dashboard Home (Visao Centralizada)

**Arquivo:** `infra/helm/monitoring/dashboards/home.json`

Painel unico com status de todas as ferramentas. Funciona como "porta de entrada" — operador abre o Grafana e sabe imediatamente se algo esta errado.

**Paineis:**

| Row | Painel | Fonte | Query |
|-----|--------|-------|-------|
| 1 | **System Health** (stat panels) | Todos | UP/DOWN por componente |
| 1 | API Requests/s | loopzero-api | `rate(loopzero_api_requests_total[5m])` |
| 1 | API Error Rate | loopzero-api | `rate(loopzero_api_requests_total{status=~"5.."}[5m])` |
| 2 | ArgoCD Apps Synced/OutOfSync | ArgoCD | `argocd_app_info{sync_status}` |
| 2 | ArgoCD Sync Latency p95 | ArgoCD | `histogram_quantile(0.95, argocd_app_reconcile_duration_seconds)` |
| 3 | Active Builds | Argo Workflows | `argo_workflows_count{status="Running"}` |
| 3 | Build Queue Depth | loopzero-api | `loopzero_build_queue_depth` |
| 3 | Build Success Rate (1h) | loopzero-api | `rate(loopzero_builds_total{status="succeeded"}[1h]) / rate(loopzero_builds_total[1h])` |
| 4 | Gitea Repos | Gitea | `gitea_repositories` |
| 4 | Gitea Memory | Gitea | `process_resident_memory_bytes{job=~".*gitea.*"}` |
| 5 | MinIO Storage Used | MinIO | `minio_bucket_usage_total_bytes` |
| 5 | MinIO Requests/s | MinIO | `rate(minio_s3_requests_total[5m])` |
| 6 | PostgreSQL Connections | PostgreSQL | `pg_stat_activity_count` |
| 6 | PostgreSQL Locks | PostgreSQL | `pg_locks_count` |
| 7 | Traefik Requests/s | Traefik | `rate(traefik_entrypoint_requests_total[5m])` |
| 7 | Traefik Error Rate (4xx+5xx) | Traefik | `rate(traefik_entrypoint_requests_total{code=~"4..|5.."}[5m])` |
| 7 | TLS Cert Expiry | Traefik | `traefik_tls_certs_not_after - time()` |

**Links:** Cada row linka para o dashboard detalhado do componente.

**Criterio de aceite:**
- [ ] Dashboard renderiza sem erros no Grafana
- [ ] Todos os paineis mostram dados reais (nao "No data")
- [ ] Links para dashboards detalhados funcionam
- [ ] ConfigMap com label `grafana_dashboard=1` criado
- [ ] Sidecar injeta automaticamente

---

### Task 2.2 — Dashboard Drift Detection

**Arquivo:** `infra/helm/monitoring/dashboards/drift-detection.json`
**Referencia:** spec §15.6 dashboard 4

**Paineis:**

| Painel | Query |
|--------|-------|
| Drift Events Timeline | `loopzero_drift_detected` (graph over time) |
| Active Drifts | `loopzero_drift_detected == 1` (stat) |
| Drift Duration (p95) | `histogram_quantile(0.95, loopzero_drift_duration_seconds)` |
| Mirror Status por Tenant/Env | `loopzero_git_mirror_status` (table) |
| Git Desired State Divergence | `loopzero_git_desired_state_divergence` (stat com threshold) |
| Reconciler Errors (5m) | `increase(loopzero_reconciler_errors_total[5m])` |
| Last Reconciliation Check | `loopzero_reconciliation_last_check_timestamp` |
| ArgoCD Sync Status | `argocd_app_info{sync_status!="Synced"}` (table) |

**Nota:** Algumas metricas (drift, mirror) dependem do StateReconciler (Sprint 7). Os paineis podem ficar com "No data" ate o reconciler existir — isso e esperado. O dashboard deve estar pronto para quando as metricas aparecerem.

**Criterio de aceite:**
- [ ] Dashboard renderiza sem erros
- [ ] Paineis que tem dados (ArgoCD, reconciler errors) mostram valores
- [ ] Paineis que dependem de metricas futuras mostram "No data" sem erro
- [ ] Variable `$tenant` e `$env` para filtering

---

### Task 2.3 — Dashboard Failure Investigation

**Arquivo:** `infra/helm/monitoring/dashboards/failure-investigation.json`
**Referencia:** spec §15.6 dashboard 5

**Paineis:**

| Painel | Query |
|--------|-------|
| Correlation ID Search | Variable `$correlation_id` — filtra todas as metricas |
| API Errors Timeline | `loopzero_api_requests_total{status=~"4..|5.."}` por path |
| Build Failures | `loopzero_builds_total{status="failed"}` timeline |
| Deploy Failures | `loopzero_deploys_total{status="failed"}` timeline |
| Lock Conflicts | `loopzero_lock_conflicts_total` por tenant/env |
| Lock Hold Duration (p99) | `histogram_quantile(0.99, loopzero_lock_hold_duration_seconds)` |
| Idempotency Cache Hits vs Misses | `loopzero_idempotency_cache_hits_total` / `misses` |
| ArgoCD Sync Failures | `argocd_app_sync_total{phase="Error"}` |
| PostgreSQL Active Locks | `pg_locks_count{mode="ExclusiveLock"}` |
| Argo Workflow Errors | `argo_workflows_count{status="Error"}` |

**Criterio de aceite:**
- [ ] Dashboard renderiza sem erros
- [ ] Variable `$correlation_id` funciona como filtro
- [ ] Variables `$tenant`, `$env`, `$time_range` para drill-down
- [ ] Paineis com dados reais (API errors, lock metrics)

---

### Task 2.4 — Atualizar Dashboards Existentes

Revisar os 3 dashboards existentes para garantir que estao usando metricas reais e links para o Home.

**Arquivos:**
- `infra/helm/monitoring/dashboards/platform-overview.json`
- `infra/helm/monitoring/dashboards/build-pipeline.json`
- `infra/helm/monitoring/dashboards/per-tenant-health.json`

**Criterio de aceite:**
- [ ] Cada dashboard tem link de volta para Home
- [ ] Queries validadas contra metricas reais do Prometheus
- [ ] Sem paineis com queries para metricas que nao existem (ajustar ou marcar como "pending")

---

## Fase 3 — Alertas

> **Objetivo:** 25 PrometheusRule alerting rules deployadas + Alertmanager configurado.
> **Entregavel:** Alertas disparam corretamente em cenarios simulados.
> **Dependencia:** Fase 1 completa (targets UP para as expressoes funcionarem).

### Task 3.1 — Habilitar Alertmanager

**Arquivo:** `infra/helm/monitoring/values.yaml`

Atualmente `alertmanager.enabled: false`. Habilitar com configuracao basica.

**Criterio de aceite:**
- [ ] `alertmanager.enabled: true` no values.yaml
- [ ] Alertmanager acessivel via NodePort ou port-forward
- [ ] Receiver `default` configurado (webhook ou stdout para dev)
- [ ] Route default configurada

---

### Task 3.2 — PrometheusRule: Alertas Critical

**Arquivo:** `infra/helm/monitoring/manifests/prometheusrule-critical.yaml`

Regras do spec §15.5 — severity: critical:

```yaml
rules:
  - alert: LoopZeroDriftDetected
    expr: loopzero_drift_detected == 1
    for: 5m

  - alert: LoopZeroBuildClusterUnreachable
    expr: up{job="build-cluster"} == 0
    for: 2m

  - alert: LoopZeroPostgresDown
    expr: pg_up{job="loopzero-postgres"} == 0
    for: 1m

  - alert: LoopZeroPostgresBackupStale
    expr: time() - pg_last_backup_timestamp > 90000  # 25h

  - alert: LoopZeroGiteaDown
    expr: up{job="loopzero-gitea"} == 0
    for: 2m

  - alert: LoopZeroGiteaBackupStale
    expr: time() - loopzero_gitea_last_backup_timestamp > 25200  # 7h

  - alert: LoopZeroArgoCDCircuitOpen
    expr: loopzero_argocd_circuit_state == 1  # 1 = OPEN
    for: 0m  # fires immediately

  - alert: LoopZeroGitMirrorExhausted
    expr: |
      loopzero_git_desired_state_divergence == 1
      and on(tenant, env) loopzero_git_mirror_status{status="FAILED"} == 1
    for: 3h

  - alert: LoopZeroFallbackStuck
    expr: |
      loopzero_git_mirror_status{status="FAILED"} == 1
      and on(tenant, env) loopzero_fallback_active == 1
    for: 3h

  - alert: LoopZeroBuildEgressStale
    expr: loopzero_build_egress_connectivity == 0
    for: 5m

  - alert: LoopZeroBreakGlassAccess
    expr: loopzero_break_glass_sessions_active > 0
    for: 0m  # fires immediately
```

**Criterio de aceite:**
- [ ] PrometheusRule criado com label `release: monitoring`
- [ ] `kubectl get prometheusrule` mostra o recurso
- [ ] Prometheus UI mostra as regras em Rules > Alerts
- [ ] Cada regra tem `annotations.runbook` com URL ou descricao

---

### Task 3.3 — PrometheusRule: Alertas Warning

**Arquivo:** `infra/helm/monitoring/manifests/prometheusrule-warning.yaml`

Regras do spec §15.5 — severity: warning:

```yaml
rules:
  - alert: LoopZeroAPIHighLatency
    expr: histogram_quantile(0.99, rate(loopzero_api_request_duration_seconds_bucket[5m])) > 5
    for: 5m
    annotations:
      runbook: "Check downstream dependencies (Gitea, Argo, PostgreSQL)."

  - alert: LoopZeroTenantQuotaNearLimit
    expr: loopzero_tenant_quota_usage_ratio > 0.9
    for: 10m

  - alert: LoopZeroBuildQueueBacklog
    expr: loopzero_build_queue_depth > 10
    for: 5m

  - alert: LoopZeroReconcilerUnhealthy
    expr: increase(loopzero_reconciler_errors_total[5m]) > 5

  - alert: LoopZeroSecretSyncFailed
    expr: increase(loopzero_secret_sync_hash_mismatch_total[15m]) > 3
    annotations:
      runbook: "AppSecretOperator failed to sync. Check operator logs, workload cluster connectivity, namespace RBAC."

  - alert: LoopZeroTenantPodsStarved
    expr: loopzero_tenant_pods_pending_duration_seconds{quantile="0.99"} > 300

  - alert: LoopZeroMirrorSyncStale
    expr: loopzero_git_mirror_status{status="FAILED"} == 1
    for: 1h
    annotations:
      runbook: "Check Gitea health. If up, trigger manual mirror sync. If down, resolve Gitea first."

  - alert: LoopZeroGitDesiredStateDivergence
    expr: |
      loopzero_git_desired_state_divergence == 1
      and on(tenant, env) loopzero_git_mirror_status{status="FAILED"} == 0
      and on(tenant, env) loopzero_git_mirror_status{status="PENDING"} == 0
    for: 10m
    annotations:
      runbook: "Git SHA diverges from environment_targets AND no mirror in progress. Check Git audit log."

  - alert: LoopZeroArgoCDAppCountWarning
    expr: count(argocd_app_info) > 200
    for: 5m

  - alert: LoopZeroArgoCDAppCountCritical
    expr: count(argocd_app_info) > 400
    for: 5m
    labels:
      severity: critical  # escalation

  - alert: LoopZeroArgoCDSyncSlow
    expr: histogram_quantile(0.95, rate(argocd_app_reconcile_duration_seconds_bucket[5m])) > 30
    for: 5m

  - alert: LoopZeroMinIODown
    expr: up{job="loopzero-minio"} == 0
    for: 2m

  - alert: LoopZeroPostgresBackupFailed
    expr: time() - pg_last_backup_timestamp > 90000
    for: 0m
```

**Criterio de aceite:**
- [ ] PrometheusRule criado com label `release: monitoring`
- [ ] Prometheus UI mostra todas as regras
- [ ] Cada regra com runbook annotation

---

### Task 3.4 — Configurar Notification Channel no Grafana

**Arquivo:** Atualizar `infra/helm/monitoring/values.yaml`

Para dev: webhook receiver (stdout logging).
Para producao: preparar estrutura para Slack/email (configuravel via secret).

**Criterio de aceite:**
- [ ] Alertmanager `route` configurada com `group_by: [alertname, tenant]`
- [ ] `group_wait: 30s`, `group_interval: 5m`, `repeat_interval: 4h`
- [ ] Receiver padrao funciona em dev (webhook ou log)
- [ ] Documentacao para configurar Slack em producao

---

### Task 3.5 — Teste de Alertas

Simular cenarios para validar que os alertas disparam:

| Cenario | Como simular | Alerta esperado |
|---------|-------------|-----------------|
| PostgreSQL down | `kubectl scale deploy postgres --replicas=0` | `LoopZeroPostgresDown` (1min) |
| Gitea down | `kubectl scale deploy gitea --replicas=0` | `LoopZeroGiteaDown` (2min) |
| API high latency | Injetar delay no middleware | `LoopZeroAPIHighLatency` (5min) |
| Build queue backlog | Criar 11+ builds simultaneos | `LoopZeroBuildQueueBacklog` (5min) |

**Criterio de aceite:**
- [ ] Pelo menos 4 alertas testados manualmente em dev
- [ ] Alertas aparecem no Alertmanager UI
- [ ] Alertas resolvem quando condicao normaliza

---

## Fase 4 — Taskfile e Documentacao

> **Objetivo:** Operabilidade. Qualquer dev consegue instalar e usar.

### Task 4.1 — Tasks no Taskfile

Adicionar ao `Taskfile.yml`:

```yaml
monitoring:install:
  desc: Install full monitoring stack
  # kube-prometheus-stack + ServiceMonitors + dashboards + alerts

monitoring:dashboards:
  desc: Reload Grafana dashboards from JSON files

monitoring:validate-targets:
  desc: Validate all Prometheus targets are UP

monitoring:test-alerts:
  desc: Run alert simulation tests
```

**Criterio de aceite:**
- [ ] Tasks idempotentes (rodar 2x nao quebra)
- [ ] `task monitoring:install` configura tudo do zero
- [ ] `task monitoring:validate-targets` reporta status de todos os targets

---

### Task 4.2 — CHANGELOG

Adicionar entradas ao `CHANGELOG.md` seção `[Unreleased]`:

```markdown
### Added
- Centralized Grafana dashboard with health status for all system components
- ServiceMonitors for ArgoCD, Argo Workflows, Gitea, Traefik, MinIO, PostgreSQL
- Drift Detection and Failure Investigation dashboards
- 25 PrometheusRule alerting rules (critical + warning) per spec §15.5
- Alertmanager with configurable notification channels
```

---

## Ordem de Execucao e Dependencias

```
Fase 1 (Scraping)
├── 1.1 ArgoCD ──────────┐
├── 1.2 Argo Workflows ──┤
├── 1.3 Gitea ───────────┤  (todas paralelas)
├── 1.4 Traefik ─────────┤
├── 1.5 MinIO ───────────┤
├── 1.6 PostgreSQL ──────┘
└── 1.7 Validacao ──────── depende de 1.1-1.6

Fase 2 (Dashboards) ──── depende de Fase 1.7
├── 2.1 Home ────────────┐
├── 2.2 Drift Detection ─┤  (todas paralelas)
├── 2.3 Failure Invest. ──┤
└── 2.4 Atualizar exist. ─┘

Fase 3 (Alertas) ──────── depende de Fase 1.7
├── 3.1 Alertmanager ─────┐
├── 3.2 Rules Critical ───┤  (3.2-3.3 paralelas, depois de 3.1)
├── 3.3 Rules Warning ────┘
├── 3.4 Notification ───── depende de 3.1
└── 3.5 Teste ──────────── depende de 3.2 + 3.3 + 3.4

Fase 4 (Operabilidade) ── depende de Fase 2 + Fase 3
├── 4.1 Taskfile
└── 4.2 CHANGELOG
```

**Nota:** Fases 2 e 3 podem rodar em paralelo entre si (ambas dependem apenas da Fase 1).

---

## Riscos e Mitigacoes

| Risco | Impacto | Mitigacao |
|-------|---------|-----------|
| Ferramenta nao expoe `/metrics` por default | Scraping falha | Verificar Helm values antes; habilitar metrics no chart |
| MinIO requer bearer token para metricas | Scraping 401 | Provisionar token via `mc admin` no script de setup |
| Metricas de drift nao existem ainda (Sprint 7) | Paineis vazios | Dashboard aceita "No data" — paineis prontos para quando metricas aparecerem |
| Alertmanager em dev gera ruido | Alertas spam | Receiver `null` em dev; silences para alertas esperados |
| Dashboards JSON grandes sao dificeis de manter | Manutencao | Usar grafonnet/jsonnet se complexidade crescer; por ora JSON direto e suficiente |
| kube-prometheus-stack ja inclui ServiceMonitors para alguns componentes | Duplicacao | Verificar antes de criar; reusar se existir |

---

## Definicao de Pronto (Fase Completa)

### Fase 1
- [ ] 7 targets UP no Prometheus (`loopzero-api`, `argocd`, `argo-workflows`, `gitea`, `traefik`, `minio`, `postgres`)
- [ ] `task monitoring:validate-targets` passa

### Fase 2
- [ ] 6 dashboards no Grafana folder "LoopZero"
- [ ] Home dashboard mostra health de todos os componentes
- [ ] Nenhum painel com erro de query (aceita "No data" para metricas futuras)

### Fase 3
- [ ] Alertmanager habilitado e recebendo alertas
- [ ] 25 regras no Prometheus (visible em Rules > Alerts)
- [ ] 4+ alertas testados via simulacao em dev

### Fase 4
- [ ] Tasks no Taskfile funcionam
- [ ] CHANGELOG atualizado
