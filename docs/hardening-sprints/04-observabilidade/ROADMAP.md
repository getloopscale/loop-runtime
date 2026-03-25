# Sprint 4 — OBSERVABILIDADE BASE

**Objetivo:** Cada etapa do deploy é mensurável. "Por que esse deploy demorou?" respondível em 30s.
**Confiança:** ALTA (métricas), MÉDIA (SLO targets — estimativas iniciais)
**Esforço:** ~8h
**Pré-requisito:** GATE-SPRINT-3 passou
**Rollback:** Remover métricas/dashboard — zero impacto no runtime (read-only).

---

## Tasks

### Task 4.1 — Métricas do deploy pipeline

**Estado atual:** `api/internal/metrics/` existe. Prometheus rodando. Verificar quais métricas LoopZero já emitem.

**Ação:** Verificar e adicionar se necessário:

| Métrica | Tipo | Labels | Onde instrumentar |
|---|---|---|---|
| `loopzero_build_duration_seconds` | histogram | tenant, status | BuildReconcileLoop (ao completar) |
| `loopzero_build_queue_seconds` | histogram | tenant | QUEUED → RUNNING delta |
| `loopzero_release_activation_seconds` | histogram | tenant | READY → ACTIVE delta |
| `loopzero_deploy_total` | counter | tenant, status | BuildOrchestrator.SubmitBuild |
| `loopzero_lock_wait_seconds` | histogram | — | withEnvironmentLock (se não criada em Sprint 3) |
| `loopzero_argocd_sync_seconds` | histogram | tenant, status | SyncClient |
| `loopzero_circuit_breaker_state` | gauge | — | CircuitBreakerSyncClient |
| `loopzero_build_queue_depth` | gauge | — | Count de builds QUEUED + RUNNING |

**Arquivos a modificar:**
- `api/internal/metrics/metrics.go` — registrar histograms/counters/gauges
- `api/internal/services/build_reconcile_loop.go` — instrumentar build_duration, queue_seconds
- `api/internal/services/build_orchestrator.go` — instrumentar deploy_total, queue_depth
- `api/internal/lock/environment_lock.go` — instrumentar lock_wait (se não feito em Sprint 3)
- `api/internal/argocd/sync_client.go` — instrumentar sync_seconds
- `api/internal/services/pg_circuit_breaker.go` — instrumentar circuit_breaker_state

**Gate 4.1:**
- [ ] `curl <api-metrics> | grep loopzero_` → todas as 8 métricas presentes
- [ ] Após 1 deploy: `loopzero_build_duration_seconds` tem observação
- [ ] `loopzero_circuit_breaker_state` reflete estado real

---

### Task 4.2 — SLOs

**SLOs iniciais (ajustar após 1 semana de dados):**

| SLO | Target | Métrica | Nome do Alerta |
|---|---|---|---|
| Build queue time p95 | < 60s | `loopzero_build_queue_seconds` | `LoopZeroQueueSlow` |
| Build duration p95 | < 300s | `loopzero_build_duration_seconds` | `LoopZeroBuildSlow` |
| Deploy success rate | > 90% (24h rolling) | `loopzero_deploy_total` | `LoopZeroDeployFailureRate` |
| Lock wait time p95 | < 2s | `loopzero_lock_wait_seconds` | `LoopZeroLockContention` |
| ArgoCD sync p95 | < 120s | `loopzero_argocd_sync_seconds` | `LoopZeroSyncSlow` |

**Ação:**
1. Criar `infra/helm/monitoring/manifests/loopzero-alerts.yaml` com PrometheusRule
2. Recording rules para cálculos de percentil
3. Alerting rules para cada SLO

**Gate 4.2:**
- [ ] `kubectl get prometheusrules -n platform-system` → `loopzero-slos` existe
- [ ] Grafana: alertas visíveis no painel

---

### Task 4.3 — Dashboard Grafana "LoopZero Platform"

**Ação:**
1. Criar `infra/helm/monitoring/dashboards/loopzero-platform.json`
2. Configurar auto-provisioning via ConfigMap no Grafana
3. Panels obrigatórios:

| Panel | Tipo | Dados |
|---|---|---|
| Deploys/hora | timeseries | `loopzero_deploy_total` por status |
| Build Duration | timeseries | p50, p95 vs SLO (300s) |
| Queue Time | timeseries | p50, p95 vs SLO (60s) |
| ArgoCD Sync | timeseries | p50, p95 vs SLO (120s) |
| Lock Wait Time | timeseries | p50, p95 vs SLO (2s) |
| Circuit Breaker | stat | `loopzero_circuit_breaker_state` |
| Build Queue Depth | gauge | `loopzero_build_queue_depth` |
| Deploy Success Rate | gauge | rate vs SLO (90%) |
| API Error Rate | timeseries | por endpoint |
| PG Pool Utilization | timeseries | active/max connections |

**Gate 4.3:**
- [ ] Dashboard acessível em Grafana
- [ ] Após 3 deploys: todos os panels mostram dados
- [ ] Teste: "por que esse deploy demorou?" → respondível em <30s olhando o dashboard

---

## GATE-SPRINT-4

- [ ] 8 métricas emitindo
- [ ] 5 SLOs definidos com alertas
- [ ] Dashboard funcional com dados reais
- [ ] "Por que esse deploy demorou?" respondível em <30s
- [ ] 3 deploys consecutivos estáveis

**SE QUALQUER ITEM FALHAR → NÃO AVANÇAR PARA SPRINT 5.**
