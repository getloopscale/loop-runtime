# Sub-Sprint 6A — `loopzero logs`

**Ação:** Instalar Loki + Promtail e expor build logs via API e CLI.
**DoD:** `loopzero logs --build <id>` retorna output completo do build com steps identificados após deploy real.
**Confiança:** MÉDIA
**Esforço:** ~12h (5 tasks sequenciais)
**Pré-requisito:** GATE-6.1 passou (`loopzero status` funcional + wired).
**Regra:** Uma camada por vez. Teste antes de implementar. Zero termos internos.

---

## Contexto Técnico

**O que existe:**
- Build cluster client (`api/internal/argo/workflow_submitter.go`) — conexão funcional com dynamic client
- SSE event store (`api/internal/sse/event_store.go`) — NOTIFY/LISTEN funcional
- Build store com `error_message` (resumo do erro, não log completo)
- CLI SSE streaming pattern (`cli/src/commands/deploy.ts`) — referência para `--follow`
- Build reconcile loop (`api/internal/services/build_reconcile_loop.go`) — detecta build terminal
- **Grafana 8.5.2** — 6 dashboards existentes, namespace `monitoring`
- **MinIO** — standalone, buckets `loopzero-artifacts` e `loopzero-builds`
- **Prometheus + OTEL Collector** — já no namespace `monitoring`

**O que NÃO existe:**
- Grafana Loki (log aggregation)
- Promtail ou fluent-bit (log shipping)
- Pod retention policy nos workflows (Argo deleta pods após conclusão)
- Endpoint `/api/v1/builds/{id}/logs`
- CLI command `loopzero logs`

**Decisões de design (pré-definidas para eliminar incerteza):**

| Decisão | Escolha | Rejeitado | Justificativa |
|---------|---------|-----------|---------------|
| Onde armazenar logs? | Grafana Loki + MinIO (bucket: `loopzero-logs`) | PostgreSQL `build_logs` table | PG é OLTP, não log store. Loki é purpose-built, integra com Grafana existente, escala horizontalmente. |
| Como coletar? | Promtail (DaemonSet no build cluster) | Captura no reconcile loop | Automático, captura todos os pods em real-time. Zero código custom de log shipping. |
| `--follow` real-time? | Sim (v1) — via Loki tail API | Não (descartado no roadmap anterior) | Loki oferece tail endpoint nativo. Custo marginal zero vs captura manual. |
| Retenção | 7 dias (Loki `retention_period: 168h`) | 72h no PG | Storage em MinIO é barato. 7 dias dá mais margem para debug. |
| Formato API | JSON array (batch) + SSE (follow) | SQL query custom | API faz proxy de LogQL para Loki. Zero schema custom. |
| Dashboard | Grafana + Loki data source (automático) | Sem dashboard | Vem grátis com Loki. Correlação com métricas existentes. |

---

## Task 6A.1 — Instalar Loki + Promtail via Helm

**Confiança:** ALTA
**Esforço:** ~3h

### O que fazer

1. **Criar** `infra/helm/loki/values.yaml` — Loki em modo monolítico (single binary, suficiente para escala do LoopZero)
2. **Criar** `infra/helm/promtail/values.yaml` — DaemonSet no build cluster para coletar logs dos pods de build
3. **Adicionar** bucket `loopzero-logs` ao MinIO (em `infra/helm/minio/values.yaml`)
4. **Adicionar** tasks no `Taskfile.yml` para instalação

### Loki config

```yaml
# infra/helm/loki/values.yaml
loki:
  auth_enabled: false
  commonConfig:
    replication_factor: 1
  storage:
    type: s3
    s3:
      endpoint: minio.platform-system:9000
      bucketnames: loopzero-logs
      access_key_id: loopzero-admin
      secret_access_key: loopzero-dev-password   # dev only — prod usa secretKeyRef
      s3ForcePathStyle: true
      insecure: true
  limits_config:
    retention_period: 168h                    # 7 dias
    max_query_series: 500
    max_query_length: 721h
  schema_config:
    configs:
      - from: "2026-01-01"
        store: tsdb
        object_store: s3
        schema: v13
        index:
          prefix: loki_index_
          period: 24h

deploymentMode: SingleBinary
singleBinary:
  replicas: 1
  resources:
    requests:
      cpu: 100m
      memory: 256Mi
    limits:
      cpu: 500m
      memory: 512Mi

gateway:
  enabled: false

monitoring:
  selfMonitoring:
    enabled: false
  lokiCanary:
    enabled: false
```

### Cross-cluster networking (Promtail → Loki)

Promtail roda no **build cluster**. Loki roda no **mgmt cluster**. Logs cruzam a rede entre clusters.

**Local (k3d):** Clusters compartilham rede Docker — `loki.monitoring:3100` resolve via DNS interno.

**Produção (DigitalOcean):** Clusters DOKS estão na mesma VPC (SFO3). Comunicação via IP interno do Service `loki.monitoring`. Requer:
1. Loki exposto como `ClusterIP` com `externalTrafficPolicy: Local` ou via VPC peering (DO clusters na mesma VPC já se comunicam via private IP)
2. Promtail `client.url` apontando para o **IP interno do LoadBalancer** do Loki no mgmt, não para DNS interno (que não resolve cross-cluster)
3. Alternativa: usar Loki gateway com Ingress interno (sem TLS, VPC-only) — ex: `http://loki-internal.mgmt.svc:3100`

**Resiliência:**
- Promtail faz buffer local em disco (default: `positions.yaml` + WAL) se Loki estiver indisponível
- Configurar `client.batchwait: 5s` e `client.batchsize: 1048576` para otimizar batch cross-cluster
- Configurar `client.backoff_config.max_retries: 20` para sobreviver a Loki restart

**Alerta obrigatório:**
- `promtail_targets_active_total` — se cair para 0, Promtail não está coletando
- `promtail_sent_bytes_total` rate — se cair para 0 por >5min, link cross-cluster pode estar down

**Decisão em produção:** Se latência p99 de push ultrapassar 500ms, considerar instalar Loki no build cluster (eliminando tráfego cross-cluster). Storage continua no MinIO do mgmt via S3 endpoint.

### Promtail config

```yaml
# infra/helm/promtail/values.yaml
# Deploy no BUILD cluster para coletar logs dos pods de Argo Workflows
config:
  clients:
    - url: http://loki.monitoring:3100/loki/api/v1/push
      batchwait: 5s
      batchsize: 1048576
      backoff_config:
        max_retries: 20
        min_period: 500ms
        max_period: 5m
  snippets:
    scrapeConfigs: |
      - job_name: argo-workflow-pods
        kubernetes_sd_configs:
          - role: pod
        relabel_configs:
          - source_labels: [__meta_kubernetes_pod_label_workflows_argoproj_io_workflow]
            action: keep
            regex: .+
          - source_labels: [__meta_kubernetes_pod_label_workflows_argoproj_io_workflow]
            target_label: workflow
          - source_labels: [__meta_kubernetes_pod_label_workflows_argoproj_io_node_name]
            target_label: step
          - source_labels: [__meta_kubernetes_namespace]
            target_label: namespace
          - source_labels: [__meta_kubernetes_pod_name]
            target_label: pod

resources:
  requests:
    cpu: 50m
    memory: 64Mi
  limits:
    cpu: 200m
    memory: 128Mi
```

### Taskfile

```yaml
loki:install:
  desc: Install Grafana Loki in monitoring namespace
  cmds:
    - helm upgrade --install loki grafana/loki
      --kube-context {{.MGMT_CONTEXT}}
      --namespace monitoring --create-namespace
      -f infra/helm/loki/values.yaml

promtail:install:
  desc: Install Promtail in build cluster
  cmds:
    - helm upgrade --install promtail grafana/promtail
      --kube-context {{.BUILD_CONTEXT}}
      --namespace monitoring --create-namespace
      -f infra/helm/promtail/values.yaml
```

### Grafana data source

Adicionar ao values de monitoring:

```yaml
grafana:
  additionalDataSources:
    - name: Loki
      type: loki
      url: http://loki.monitoring:3100
      access: proxy
      isDefault: false
```

### TDD

1. Teste: Loki health endpoint responde (`GET /ready` → 200)
2. Teste: Promtail está coletando logs (verificar via Loki query `{job="argo-workflow-pods"}`)
3. Teste: Bucket `loopzero-logs` existe no MinIO

### Gate 6A.1
- [ ] Loki rodando no namespace `monitoring` do mgmt cluster
- [ ] Promtail rodando como DaemonSet no build cluster
- [ ] Grafana data source Loki configurado
- [ ] Query via Grafana Explore retorna logs de build

---

## Task 6A.2 — Pod retention policy

**Confiança:** ALTA
**Esforço:** ~1h

### O que fazer

1. **Configurar pod retention** no Argo Workflows para que pods de build não sejam deletados imediatamente
2. **Retention de 4h** — suficiente para Promtail capturar logs antes do cleanup

### Arquivos

- `infra/helm/argo-workflows/values.yaml` — adicionar `workflowDefaults.podGC`
- Ou CWT templates — adicionar `podGC` direto no workflow spec

### Config

```yaml
# Em workflowDefaults ou no workflow builder
podGC:
  strategy: OnWorkflowCompletion
  deleteDelayDuration: 4h
```

### TDD

1. Teste: workflow spec gerado inclui `podGC.deleteDelayDuration = 4h`
2. Teste: pod de build completo ainda existe após 1 minuto (antes era deletado imediatamente)

### Gate 6A.2
- [ ] Pods de build retidos por 4h após conclusão
- [ ] Promtail captura logs antes do cleanup
- [ ] Testes passam

---

## Task 6A.3 — API endpoint

**Confiança:** ALTA
**Esforço:** ~3h

### O que fazer

1. **Criar handler** `GET /api/v1/builds/{id}/logs`
2. **Internamente:** consultar Loki via HTTP API com LogQL
3. **Query params:** `?tail=100` (últimas N linhas), `?step=install-deps` (filtro por step), `?follow=true` (streaming)
4. **Wiring:** Registrar no router em `server.go` (NÃO repetir padrão "implemented but not wired")

### Interface (DIP)

```go
type LogQuerier interface {
    QueryBuildLogs(ctx context.Context, buildID string, opts LogQueryOpts) ([]LogEntry, error)
    TailBuildLogs(ctx context.Context, buildID string) (<-chan LogEntry, error)
}

type LogQueryOpts struct {
    Limit    int
    Step     string
    Since    time.Duration
}
```

### LogQL queries

```
# Todos os logs de um build
{workflow="build-abc123"}

# Filtrado por step
{workflow="build-abc123", step="install-deps"}

# Últimas N linhas
{workflow="build-abc123"} | limit N
```

### Response shape

```json
{
  "build_id": "build-abc123",
  "status": "SUCCEEDED",
  "entries": [
    { "step": "clone", "stream": "stdout", "content": "Cloning into...", "timestamp": "2026-03-25T14:30:01Z" },
    { "step": "install-deps", "stream": "stdout", "content": "npm install...", "timestamp": "2026-03-25T14:30:05Z" }
  ],
  "total_lines": 142
}
```

### TDD

1. Teste: `GET /builds/{id}/logs` → 200 com entries (Loki retorna dados)
2. Teste: build inexistente → 404
3. Teste: build sem logs (Loki vazio) → 200 com entries vazio + mensagem "No logs available yet"
4. Teste: `?tail=10` → últimas 10 linhas
5. Teste: `?step=clone` → apenas logs do step clone
6. Teste: 401 sem auth
7. Teste: tenant isolation (build de outro tenant → 404)
8. Teste: endpoint está wired no router (integração — previne P-03)

### Gate 6A.3
- [ ] Endpoint retorna logs do Loki
- [ ] Filtragem por tail e step funciona
- [ ] Tenant isolation verificado
- [ ] **Wired no router** (verificado por teste de integração)
- [ ] 8 testes passam

---

## Task 6A.4 — CLI command

**Confiança:** ALTA
**Esforço:** ~3h

### O que fazer

1. **Criar** `cli/src/commands/logs.ts` — classe `LogsCommand`
2. **Wiring** em `cli/src/index.ts`
3. **Flags:** `--build <id>` (obrigatório), `--tail <n>`, `--step <name>`, `--follow`, `--json`, `--env`

### Output human

```
Build: build-abc123 (SUCCEEDED)

[clone] Cloning into '/workspace'...
[clone] HEAD is now at abc123
[install-deps] npm install
[install-deps] added 342 packages in 12s
[build] Building application...
[build] Successfully built image
```

### Output JSON (`--json`)

```json
{
  "build_id": "build-abc123",
  "status": "SUCCEEDED",
  "entries": [...],
  "total_lines": 142
}
```

### `--follow` (real-time streaming)

```
Build: build-abc123 (RUNNING)

[clone] Cloning into '/workspace'...
[clone] HEAD is now at abc123
[install-deps] npm install
  ← streaming, novas linhas aparecem em tempo real
```

Implementação: SSE connection para o endpoint API que faz proxy do Loki tail.

### Exit codes

| Code | Significado |
|------|-------------|
| 0 | Sucesso |
| 1 | Uso inválido (build ID faltando) |
| 4 | Auth error |

### TDD

1. Teste: logs de build com sucesso → exit 0, output contém step names
2. Teste: build inexistente → exit 1, mensagem sugere `loopzero status`
3. Teste: sem auth → exit 4, mensagem sugere `loopzero login`
4. Teste: `--json` → JSON válido com schema correto
5. Teste: `--tail 5` → passa param para API
6. Teste: `--follow` → usa SSE/streaming connection
7. Teste: zero termos internos em qualquer cenário (forbidden terms check)
8. Teste: sem logs disponíveis → mensagem clara "No logs available. Build may still be starting."

### Gate 6A.4
- [ ] `loopzero logs --build <id>` retorna output formatado
- [ ] `--follow` funciona com build ativo
- [ ] `--json` retorna schema estável
- [ ] Mensagem clara para builds sem logs
- [ ] Zero termos internos
- [ ] 8 testes passam

---

## Task 6A.5 — Validação E2E + Dashboard

**Confiança:** MÉDIA
**Esforço:** ~2h

### O que fazer

1. **Teste E2E:**
   - `loopzero deploy` → build executa
   - `loopzero logs --build <id>` → retorna output do build
   - `loopzero logs --build <id> --follow` → streaming funciona durante build ativo
   - Build antigo (após 7 dias) → mensagem clara
2. **Dashboard Grafana** (opcional — Loki + Grafana Explore já funcionam):
   - Painel "Build Logs" no dashboard existente de Build Pipeline
   - Link direto: `grafana.dev.loopzero.example/explore?left={"queries":[{"expr":"{workflow=\"$build_id\"}"}]}`

### Gate 6A.5
- [ ] `loopzero logs` funcional após deploy real
- [ ] `--follow` funciona durante build ativo
- [ ] Logs visíveis no Grafana Explore
- [ ] 3 deploys estáveis com logs verificáveis

---

## Limitações Aceitas (v1)

| Limitação | Justificativa | Futuro |
|-----------|---------------|--------|
| Loki single binary (não cluster mode) | Escala suficiente para v1. MinIO absorve storage. | Escalar para microservices mode se >100 builds/dia |
| Sem log de runtime (app logs) | Escopo é build logs, não app observability | Sprint 7 (Observability) — mesmo Loki, adicionar Promtail no workload cluster |
| Promtail apenas no build cluster | Build logs são a prioridade. App logs futuramente. | Adicionar DaemonSet no workload cluster |
| Logs retidos por 7 dias | Storage em MinIO é barato. Suficiente para debug. | Aumentar para 30 dias se storage permitir |

---

## GATE-6A

- [ ] Loki + Promtail rodando e coletando logs
- [ ] `loopzero logs --build <id>` retorna output de build
- [ ] `--follow` funciona durante build ativo (via Loki tail)
- [ ] Logs visíveis no Grafana Explore
- [ ] `--json` shape estável
- [ ] Zero termos internos
- [ ] Testes passam (unit API + unit CLI)
- [ ] Endpoints wired no router (teste de integração)
- [ ] 3 deploys estáveis com logs verificáveis
