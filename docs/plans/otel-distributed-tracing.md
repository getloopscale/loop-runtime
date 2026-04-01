# Plano: OpenTelemetry Distributed Tracing

> **Objetivo:** Habilitar distributed tracing no LoopZero para troubleshooting do fluxo de deploy.
>
> **Escopo:** Apenas tracing. Metricas (Prometheus) e logs (Loki) permanecem inalterados.
>
> **Classificacao:** T2 (toca Helm, Pulumi, codigo Go da API)
>
> **Aprovacao:** MEET-2026-03-30-020 — APPROVED_WITH_CONDITIONS
>
> **Comite:** SRE, Distributed Systems, Principal Architect, Verification Architect

---

## Contexto

O fluxo de deploy cruza 4 planos (CLI -> API -> Build -> Workload) em processos sincronos e assincronos. Hoje, troubleshooting depende de correlacionar logs manualmente pelo `Correlation-ID` em 3 clusters via Loki/LogQL. Com tracing, cada deploy vira um trace completo visivel no Grafana.

### O que ja existe
- **OTel Collector** deployado via Helm (`infra/helm/otel/values.yaml`, imagem `0.96.0`)
  - Recebe OTLP (gRPC :4317, HTTP :4318)
  - Traces vao para `debug` exporter (descartadas)
  - Metricas vao para Prometheus exporter (:8889)
- **Correlation-ID** middleware na API (`api/internal/middleware/correlation.go`)
  - Gera UUID v4 por request, propaga via header
  - Persistido no banco: `builds.correlation_id`, `releases.correlation_id`, `environment_targets.correlation_id`
- **Grafana** no mgmt cluster com datasources Prometheus + Loki
- **Zero instrumentacao** no codigo Go — nenhum import de `go.opentelemetry.io/otel`

### O que sera adicionado
1. **Grafana Tempo** como backend de traces (storage em filesystem/S3)
2. **Collector reconfigurado** para exportar traces para Tempo
3. **API instrumentada** com spans nos pontos criticos do fluxo de deploy
4. **Datasource Tempo** no Grafana (correlacao trace <-> logs via derived fields)

### Decisoes da reuniao tecnica (MEET-020)

| Decisao | Rationale |
|---------|-----------|
| Correlation-ID permanece UUID v4 | trace-id e campo separado. Unificar quebraria Loki queries, dados no PG, e CLI. |
| Phase 2 dividida em 2A (sync) e 2B (async) | Async trace linking requer design especifico para `context.Background()` em BuildReconcileLoop e SyncCallbackActivator. |
| Feature flag `LOOPZERO_TRACING_ENABLED` | Default `true` em dev, `false` em prod. Flip apos 48h de validacao em DO. |
| Alerting obrigatorio para Tempo e Collector | "Observability of the observability" — sem alertas, falha silenciosa e inevitavel. |
| PriorityClass baixa para Tempo | Telemetria e first-to-shed sob resource pressure. |
| Sampling config pronta no Collector | `probabilistic_sampler` com ratio 1.0, pronto para tunar sem redeploy. |

---

## Condicoes obrigatorias (9 total)

Todas as condicoes BLOCKING devem ser atendidas antes da Phase correspondente ir para DigitalOcean.

### BLOCKING Phase 1

**C1 — Alerting para tracing subsystem:**
- `LoopZeroTempoDown`: `up{job="tempo"} == 0` for 2m (critical)
- `LoopZeroTempoStorageHigh`: PVC >80% for 10m (warning)
- `LoopZeroOTelCollectorDown`: `up{job="otel-collector"} == 0` for 2m (critical)
- `LoopZeroCollectorExportErrors`: `otelcol_exporter_send_failed_spans_total` increasing for 5m (warning)
- ServiceMonitor para Tempo (seguir padrao existente em `infra/helm/monitoring/manifests/`)

### BLOCKING Phase 2A

**C2 — Correlation-ID permanece UUID v4.**
Trace-id e campo separado no slog e no banco. Grafana faz a ponte via derived fields. NENHUMA mudanca no formato do header `Correlation-ID`.

**C3 — Span naming convention:**
Pattern: `loopzero.api.<operation>` (ex: `loopzero.api.build.submit`, `loopzero.api.source.upload`, `loopzero.api.lock.acquire`). Documentar antes de instrumentar.

**C5 — Graceful degradation testada:**
- Export timeout = 1s (NAO o default de 10s)
- Retry disabled no boot
- Bounded queue: 2048 spans max
- Teste: API sem collector, 100 requests, p99 < 50ms, zero panics, zero goroutine leaks

**C6 — Zero regressao nos testes existentes:**
`cd api && go test ./...` passa com OTel middleware ativo.

**C7 — Feature flag `LOOPZERO_TRACING_ENABLED`:**
- Env var: `LOOPZERO_TRACING_ENABLED`
- Default: `true` em dev, `false` em prod
- Quando `false`: `tracing.Init()` retorna no-op, zero overhead
- Flip para `true` apos 48h de validacao em DO

### BLOCKING Phase 2B

**C4 — Async trace linking design:**
Armazenar `traceparent` completo (trace-id + span-id + flags) na tabela `builds` no momento da submissao. BuildReconcileLoop e SyncCallbackActivator reconstroem SpanContext e criam linked spans (NAO child spans — request original ja terminou). Design documentado ANTES de implementar.

### RECOMMENDED Phase 1

**C8 — PriorityClass para Tempo:** Menor que PostgreSQL, ArgoCD, API, operators.

**C9 — Sampling config no Collector:** `probabilistic_sampler` processor com ratio 1.0 (default). Pronto para tunar sem redeploy.

---

## Fases

### Phase 1 — Infra: Tempo + Collector + Alerting

**Objetivo:** Backend de traces funcionando, alertas ativos, recebendo dados de teste.

#### Task 1.1 — Helm chart do Grafana Tempo
**Arquivo:** `infra/helm/tempo/values.yaml`

```yaml
# Grafana Tempo — trace backend
# Chart: grafana/tempo
# Modo: standalone (suficiente para <100 deploys/dia)

tempo:
  receivers:
    otlp:
      protocols:
        grpc:
          endpoint: "0.0.0.0:4317"
        http:
          endpoint: "0.0.0.0:4318"
  retention: 168h                  # 7 dias, mesmo que Loki
  storage:
    trace:
      backend: local               # Dev: filesystem. Prod: s3 (DO Spaces)
      local:
        path: /var/tempo/traces
      wal:
        path: /var/tempo/wal

persistence:
  enabled: true
  size: 5Gi

resources:
  limits:
    cpu: "300m"
    memory: "512Mi"
  requests:
    cpu: "100m"
    memory: "256Mi"
```

**Criterio de aceite:** `helm template` renderiza sem erros.

#### Task 1.2 — Versao pinada no bootstrap-shared
**Arquivo:** `infra/pulumi/src/components/bootstrap-shared.ts`

Adicionar ao `HELM_VERSIONS`:
```typescript
tempo: "1.14.0",   // grafana/tempo chart
```

**Criterio de aceite:** Constante acessivel, sem floating version.

#### Task 1.3 — Pulumi: instalar Tempo no mgmt cluster
**Arquivo:** `infra/pulumi/src/components/mgmt-observability.ts`

Nova funcao `installTempo()` seguindo o padrao de `installPrometheus()` e `installGrafana()`:

```typescript
export function installTempo(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  config: PlatformStackConfig,
): k8s.helm.v3.Release {
  return new k8s.helm.v3.Release("tempo", {
    name: "tempo",
    chart: "tempo",
    version: HELM_VERSIONS.tempo,
    repositoryOpts: { repo: "https://grafana.github.io/helm-charts" },
    namespace: PLATFORM_NAMESPACE,
    createNamespace: false,
    values: {
      fullnameOverride: "tempo",
      // C8: PriorityClass lower than core platform services
      priorityClassName: "loopzero-observability",
      tempo: {
        receivers: {
          otlp: {
            protocols: {
              grpc: { endpoint: "0.0.0.0:4317" },
              http: { endpoint: "0.0.0.0:4318" },
            },
          },
        },
        retention: "168h",
        storage: {
          trace: {
            backend: config.environment === "prod" ? "s3" : "local",
            local: { path: "/var/tempo/traces" },
            ...(config.environment === "prod" ? {
              s3: {
                bucket: "loopzero-traces",
                endpoint: `${config.region}.digitaloceanspaces.com`,
                forcepathstyle: true,
              },
            } : {}),
          },
        },
      },
      persistence: { enabled: true, size: "5Gi" },
      resources: {
        requests: { cpu: "100m", memory: "256Mi" },
        limits: { cpu: "300m", memory: "512Mi" },
      },
    },
  }, { provider, dependsOn: [ns] });
}
```

Chamar em `mgmt-bootstrap.ts` apos Grafana:
```typescript
const tempo = installTempo(provider, ns, config);
```

**Criterio de aceite:** `pulumi preview` mostra Tempo como novo recurso. Deploy local (`tilt up`) cria pod Running.

#### Task 1.4 — Reconfigurar OTel Collector
**Arquivo:** `infra/helm/otel/values.yaml`

Trocar exporter de traces de `debug` para `otlp` apontando para Tempo. Adicionar `probabilistic_sampler` (C9):

```yaml
processors:
  batch:
    timeout: 5s
    send_batch_size: 1024
  probabilistic_sampler:            # C9: sampling pronto para tunar
    sampling_percentage: 100        # 100% em dev/staging. Reduzir em prod se necessario.

exporters:
  debug:
    verbosity: basic
  prometheus:
    endpoint: 0.0.0.0:8889
  otlp/tempo:
    endpoint: "tempo.platform-system.svc:4317"
    tls:
      insecure: true                # In-cluster, sem TLS

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [probabilistic_sampler, batch]
      exporters: [otlp/tempo]       # ERA: [debug]
    metrics:
      receivers: [otlp]
      processors: [batch]
      exporters: [prometheus]       # Sem mudanca
```

**Criterio de aceite:** Collector reinicia sem erros. Payload de teste via OTLP aparece no Tempo API.

#### Task 1.5 — Datasource Tempo no Grafana
**Arquivo:** `infra/pulumi/src/components/mgmt-observability.ts`

Adicionar datasource na funcao `installGrafana()`:

```typescript
datasources: [
  { name: "Prometheus", type: "prometheus", url: "...", isDefault: true },
  {
    name: "Tempo",
    type: "tempo",
    url: `http://tempo.${PLATFORM_NAMESPACE}.svc.cluster.local:3200`,
    access: "proxy",
    jsonData: {
      tracesToLogsV2: {
        datasourceUid: "loki",
        filterByTraceID: false,
        customQuery: true,
        // C2: Correlation-ID permanece UUID v4, trace-id e campo separado.
        // Grafana faz a ponte via derived field no slog.
        query: '{job="loopzero-api"} |= "${__trace.traceId}"',
      },
    },
  },
],
```

**Criterio de aceite:** Grafana mostra Tempo como datasource. "Explore -> Tempo -> Search" funciona.

#### Task 1.6 — PrometheusRules + ServiceMonitor para Tempo (C1)
**Arquivo:** `infra/helm/monitoring/manifests/prometheusrule-tracing.yaml`

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: loopzero-tracing
  namespace: platform-system
  labels:
    release: prometheus
spec:
  groups:
    - name: loopzero-tracing
      rules:
        - alert: LoopZeroTempoDown
          expr: 'up{job="tempo"} == 0'
          for: 2m
          labels:
            severity: critical
          annotations:
            summary: "Grafana Tempo is down"
            description: "Tempo trace backend unreachable for 2+ minutes. Traces are being lost."
            runbook: "Check pod status: kubectl -n platform-system get pods -l app.kubernetes.io/name=tempo"

        - alert: LoopZeroTempoStorageHigh
          expr: |
            kubelet_volume_stats_used_bytes{persistentvolumeclaim=~".*tempo.*"}
            / kubelet_volume_stats_capacity_bytes{persistentvolumeclaim=~".*tempo.*"}
            > 0.8
          for: 10m
          labels:
            severity: warning
          annotations:
            summary: "Tempo PVC usage above 80%"
            description: "Tempo storage at {{ $value | humanizePercentage }}. Reduce retention or increase PVC."

        - alert: LoopZeroOTelCollectorDown
          expr: 'up{job="otel-collector"} == 0'
          for: 2m
          labels:
            severity: critical
          annotations:
            summary: "OTel Collector is down"
            description: "OTel Collector unreachable for 2+ minutes. All traces and OTel metrics are being lost."
            runbook: "Check pod: kubectl -n platform-system get pods -l app.kubernetes.io/name=opentelemetry-collector"

        - alert: LoopZeroCollectorExportErrors
          expr: 'increase(otelcol_exporter_send_failed_spans_total[5m]) > 0'
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "OTel Collector failing to export spans"
            description: "Collector has failed span exports for 5+ minutes. Tempo may be down or unreachable."
```

**Arquivo:** `infra/helm/monitoring/manifests/servicemonitor-tempo.yaml`

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: tempo
  namespace: platform-system
  labels:
    release: prometheus
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: tempo
  endpoints:
    - port: http
      interval: 30s
      path: /metrics
```

**Criterio de aceite:** `kubectl get prometheusrules -n platform-system` mostra `loopzero-tracing`. Prometheus targets inclui Tempo.

#### Task 1.7 — Taskfile: tasks de verificacao
**Arquivo:** `Taskfile.yml`

```yaml
tempo:status:
  desc: Check Tempo health and connectivity
  cmds:
    - kubectl --context k3d-loopzero-mgmt -n platform-system get pods -l app.kubernetes.io/name=tempo
    - kubectl --context k3d-loopzero-mgmt -n platform-system get servicemonitor tempo
    - kubectl --context k3d-loopzero-mgmt -n platform-system get prometheusrules loopzero-tracing
```

**Criterio de aceite:** `task tempo:status` mostra pod Running + ServiceMonitor + PrometheusRule.

---

### Phase 2A — API: Sync Spans (valor imediato)

**Objetivo:** Spans nos pontos sincronos do fluxo de deploy. Correlation-ID inalterado.

#### Task 2A.1 — Dependencias Go
**Arquivo:** `api/go.mod`

```bash
cd api && go get \
  go.opentelemetry.io/otel \
  go.opentelemetry.io/otel/sdk \
  go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc \
  go.opentelemetry.io/otel/propagation \
  go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp
```

**Criterio de aceite:** `go mod tidy` sem erros, `go build ./...` compila.

#### Task 2A.2 — Pacote `tracing` (bootstrap do OTel SDK)
**Arquivo:** `api/internal/tracing/tracing.go`

```go
package tracing

// Init configura o OTel TracerProvider com OTLP gRPC exporter.
// Retorna shutdown function para graceful shutdown.
//
// Behavior:
//   - If LOOPZERO_TRACING_ENABLED=false (C7): returns no-op, zero overhead.
//   - If collector unreachable: API continues normally (graceful degradation).
//   - Export timeout: 1s (C5, NOT the default 10s).
//   - Retry: disabled on boot (C5).
//   - Queue: bounded at 2048 spans max (C5).
//
// Env vars:
//   LOOPZERO_TRACING_ENABLED            (default: "true" in dev, "false" in prod)
//   OTEL_EXPORTER_OTLP_ENDPOINT     (default: "otel-collector.platform-system.svc:4317")
//   OTEL_SERVICE_NAME               (default: "loopzero-api")

func Init(ctx context.Context, serviceName string) (shutdown func(context.Context) error, err error)
```

Responsabilidades:
- Checar `LOOPZERO_TRACING_ENABLED` — se false, retornar no-op shutdown (C7)
- Criar `TracerProvider` com `otlptracegrpc` exporter
  - `WithTimeout(1 * time.Second)` (C5)
  - `WithRetry(otlptracegrpc.RetryConfig{Enabled: false})` (C5)
  - `BatchSpanProcessor` com `MaxQueueSize: 2048` (C5)
- Registrar `TextMapPropagator` (W3C TraceContext + Baggage)
- Configurar resource attributes: `service.name`, `service.version`, `deployment.environment`
- Retornar `shutdown()` que faz `tp.Shutdown(ctx)` com timeout

**Criterio de aceite:**
- Teste: `Init()` com collector ausente retorna sem erro, API funciona
- Teste: `Init()` com `LOOPZERO_TRACING_ENABLED=false` retorna no-op
- Teste: 100 requests sem collector, p99 < 50ms, zero panics (C5)

#### Task 2A.3 — Bootstrap no main.go
**Arquivo:** `api/cmd/main.go`

Apos `config.Load()` e antes de criar stores:

```go
// Distributed tracing (OTel) — C7: controlled by LOOPZERO_TRACING_ENABLED
tracingShutdown, err := tracing.Init(shutdownCtx, "loopzero-api")
if err != nil {
    logger.Warn("tracing disabled", "error", err)
} else {
    defer tracingShutdown(context.Background())
    logger.Info("tracing enabled",
        "endpoint", os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
}
```

**Criterio de aceite:** API inicia com e sem collector. Logs indicam "tracing enabled" ou "tracing disabled".

#### Task 2A.4 — Middleware HTTP: auto-spans
**Arquivo:** `api/internal/middleware/tracing.go`

```go
// OtelHTTP wraps chi routes with otelhttp for automatic span creation.
// Every HTTP request becomes a root span with standard HTTP attributes.
//
// C2: Correlation-ID is NOT modified. trace-id is added as a SEPARATE
// slog field ("trace_id") alongside the existing "correlation_id".
// The Correlation-ID middleware runs AFTER OtelHTTP and is unaware of traces.
func OtelHTTP(handler http.Handler) http.Handler
```

**Importante (C2):** NAO modificar o middleware CorrelationID existente. O trace-id e adicionado ao logger como campo separado:

```go
// No middleware OtelHTTP, apos span criado:
span := trace.SpanFromContext(r.Context())
if span.SpanContext().IsValid() {
    logger := slog.Default()
    // Adiciona trace_id ao structured log SEM mexer no correlation_id
    ctx = logging.WithField(ctx, "trace_id", span.SpanContext().TraceID().String())
}
```

Registrar no middleware stack **antes** do correlation middleware:
```go
r.Use(middleware.OtelHTTP)       // Cria span, adiciona trace_id ao log context
r.Use(middleware.CorrelationID)  // Inalterado — continua gerando UUID v4
```

**Criterio de aceite:**
- Teste: request gera span com `http.method`, `http.route`, `http.status_code`
- Teste: response header `Correlation-ID` e UUID v4 valido (formato inalterado) (C2)
- Teste: structured log contem AMBOS `correlation_id` (UUID v4) E `trace_id` (hex)
- `cd api && go test ./...` passa sem regressoes (C6)

#### Task 2A.5 — Spans nos pontos sincronos do deploy (C3)
**Arquivos:** Services da API que participam do fluxo sincrono.

**Span naming convention (C3):** `loopzero.api.<operation>`

| Service | Span Name | Atributos | Arquivo |
|---------|-----------|-----------|---------|
| BuildOrchestrator | `loopzero.api.build.submit` | tenant, project, environment, build_id | `services/build_orchestrator.go` |
| S3 Uploader | `loopzero.api.source.upload` | bucket, key, size_bytes | `s3/uploader.go` |
| Gitea Source | `loopzero.api.source.push_git` | repo, ref | `source/gitea_store.go` |
| Argo Workflow | `loopzero.api.build.workflow_submit` | workflow_name, namespace | `argo/workflow_submitter.go` |
| Lock | `loopzero.api.lock.acquire` | tenant, project, environment, wait_ms | `lock/advisory.go` |
| GitOps Pusher | `loopzero.api.gitops.push_manifests` | repo, path, commit_sha | `gitops/gitea_pusher.go` |

**Padrao de instrumentacao:**

```go
func (s *BuildOrchestrator) SubmitBuild(ctx context.Context, ...) error {
    ctx, span := otel.Tracer("loopzero-api").Start(ctx, "loopzero.api.build.submit",
        trace.WithAttributes(
            attribute.String("loopzero.tenant", tenantID),
            attribute.String("loopzero.project", projectName),
            attribute.String("loopzero.environment", env),
        ))
    defer span.End()

    // ... logica existente (sem mudanca) ...

    if err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, err.Error())
        return err
    }
    span.SetAttributes(attribute.String("loopzero.build_id", buildID))
    return nil
}
```

**NOTA (MEET-020):** NAO instrumentar dentro do advisory lock critical section. Instrumentar ao redor de `WithLock`, nao dentro dele. O timeout de 5s e muito apertado para qualquer overhead adicional.

**Criterio de aceite:** Um `loopzero deploy` produz trace com 6+ spans visivel no Grafana Tempo.

#### Task 2A.6 — Propagacao de contexto em HTTP clients
**Arquivos:** Clients HTTP existentes.

Injetar `traceparent` header nas chamadas HTTP outgoing via `otelhttp.NewTransport`:

- `api/internal/argocd/sync_client.go` — HTTP client para ArgoCD API
- `api/internal/gitops/gitea_pusher.go` — git operations (via HTTP)
- `api/internal/s3/uploader.go` — S3 requests

**NAO instrumentar** o K8s dynamic client (Argo Workflow submitter) — a propagacao via REST config transport e complexa e o Argo Workflow nao tem OTel SDK. O span `loopzero.api.build.workflow_submit` ja captura a duracao da submissao.

**Criterio de aceite:**
- Teste: request da API para servico interno carrega header `traceparent`
- Traces mostram parent-child entre API span e client span

---

### Phase 2B — Async Trace Linking (requer design primeiro)

**Objetivo:** Conectar spans assincronos (reconcile loop, ArgoCD callback) ao trace original do deploy.

> **BLOCKING (C4):** Esta phase requer design documentado ANTES de implementar.
> O design deve ser revisado e aprovado.

#### Problema

O deploy do LoopZero tem 4 async boundaries onde `context.Background()` severa o trace context:

1. **BuildReconcileLoop** — background goroutine polling a cada 5s, usa shutdown context
2. **SyncCallbackActivator** — ArgoCD webhook handler, cria `context.Background()` com 30s timeout (linha 66)
3. **ReleaseConvergenceLoop** — background goroutine, usa shutdown context
4. **SSE event emission** — PostgreSQL NOTIFY, sem trace context

#### Solucao proposta (C4)

**Passo 1:** Armazenar `traceparent` na tabela `builds`:

```sql
-- Migration
ALTER TABLE builds ADD COLUMN trace_parent TEXT;
-- Formato: "00-{trace_id}-{span_id}-{flags}" (W3C traceparent)
```

**Passo 2:** No `BuildOrchestrator.SubmitBuild`, persistir o traceparent:

```go
span := trace.SpanFromContext(ctx)
if span.SpanContext().IsValid() {
    build.TraceParent = fmt.Sprintf("00-%s-%s-01",
        span.SpanContext().TraceID().String(),
        span.SpanContext().SpanID().String())
}
```

**Passo 3:** No `BuildReconcileLoop.reconcileBuild`, reconstruir SpanContext e criar linked span:

```go
if build.TraceParent != "" {
    remoteCtx := propagation.TraceContext{}.Extract(ctx,
        propagation.MapCarrier{"traceparent": build.TraceParent})
    ctx, span = otel.Tracer("loopzero-api").Start(remoteCtx,
        "loopzero.api.build.reconcile",
        trace.WithLinks(trace.LinkFromContext(remoteCtx)))
    defer span.End()
}
```

**Passo 4:** No `SyncCallbackActivator`, buscar traceparent do build e criar linked span:

```go
build, _ := s.buildStore.GetByArgoWorkflow(ctx, workflowName)
if build != nil && build.TraceParent != "" {
    // Reconstruct span context from stored traceparent
    remoteCtx := propagation.TraceContext{}.Extract(ctx,
        propagation.MapCarrier{"traceparent": build.TraceParent})
    ctx, span = otel.Tracer("loopzero-api").Start(remoteCtx,
        "loopzero.api.deploy.callback_activate",
        trace.WithLinks(trace.LinkFromContext(remoteCtx)))
    defer span.End()
}
```

#### Spans async (Phase 2B)

| Service | Span Name | Trigger | Arquivo |
|---------|-----------|---------|---------|
| BuildReconcileLoop | `loopzero.api.build.reconcile` | Status change detected | `services/build_reconcile_loop.go` |
| SyncCallbackActivator | `loopzero.api.deploy.callback_activate` | ArgoCD webhook received | `services/sync_callback_activator.go` |
| Delivery | `loopzero.api.release.activate` | Release transition to ACTIVE | `services/delivery.go` |
| SSE | `loopzero.api.sse.emit` | Build event emitted | `sse/event_store.go` |

**NOTA:** Spans no reconcile loop so devem ser criados quando ha state change real, NAO em cada tick de polling (evitar 120 spans/min de no-ops).

**Criterio de aceite:**
- Teste: deploy completo produz trace unico com spans sync E async linked
- Teste: Tempo query por trace-id retorna spans de ambas as fases
- Teste: reconcile sem state change NAO cria span

---

### Phase 3 — Verificacao e Correlacao

#### Task 3.1 — Dashboard "Deploy Trace" no Grafana
**Arquivo:** `infra/pulumi/src/components/mgmt-observability.ts` (ou ConfigMap JSON)

Dashboard com:
- **Panel 1:** Trace search por `loopzero.tenant` + `loopzero.build_id`
- **Panel 2:** Trace gantt chart (timeline de spans)
- **Panel 3:** Link para logs no Loki filtrados pelo trace_id (derived field)

**Criterio de aceite:** Abrir dashboard, buscar por build-id, ver trace completo.

#### Task 3.2 — Config para producao (DO Spaces)
**Arquivo:** `infra/pulumi/Pulumi.prod.yaml`

```yaml
loopzero:tempoStorageBucket: "loopzero-traces"
```

Tempo em prod usa S3 backend (DO Spaces) ao inves de filesystem local.

**Criterio de aceite:** `pulumi preview --stack prod` mostra Tempo com S3 config.

#### Task 3.3 — Testes de integracao
**Arquivo:** `api/tests/integration/tracing_test.go`

```go
// TestTracePropagation:
// 1. Request com traceparent header gera span como child
// 2. Request sem traceparent gera novo trace
// 3. Correlation-ID no response e UUID v4 valido (C2 — formato inalterado)
// 4. Span tem atributos corretos (http.route, http.status_code)
// 5. Structured log contem AMBOS correlation_id E trace_id

// TestGracefulDegradation (C5):
// 1. Init sem collector: retorna sem erro
// 2. 100 requests sem collector: todos retornam 200
// 3. p99 < 50ms, zero panics
// 4. Zero goroutine leaks (runtime.NumGoroutine delta < 5)

// TestTracingDisabled (C7):
// 1. LOOPZERO_TRACING_ENABLED=false: Init retorna no-op
// 2. Requests nao geram spans
// 3. Zero overhead mensuravel

// TestContextPropagation:
// 1. Request outgoing para servico interno carrega header traceparent
// 2. Trace-id no header traceparent = trace-id do span pai

// TestMiddlewareOrdering:
// 1. OtelHTTP antes de CorrelationID
// 2. Correlation-ID e UUID v4 independente do trace-id
```

**Nota sobre no-mock rule:** `tracetest.InMemoryExporter` do OTel SDK e uma implementacao real do interface `SpanExporter` (nao e mock). Aprovado para uso em testes pela reuniao MEET-020.

**Criterio de aceite:** `go test ./tests/integration/ -run TestTrace` passa.

#### Task 3.4 — E2E: trace completo de um deploy
**Arquivo:** `infra/tests/e2e/tracing_test.go`

```go
// TestDeployProducesTrace:
// 1. Executa loopzero deploy
// 2. Poll Tempo API com backoff (max 30s, 1s intervals) — traces sao batched
// 3. Consulta Tempo API: GET /api/traces/{traceID}
// 4. Phase 2A: verifica spans sync (build.submit, source.upload, workflow_submit, lock.acquire, gitops.push)
// 5. Phase 2B: verifica spans async linked (build.reconcile, callback_activate, release.activate)
```

**NOTA:** Teste deve usar polling com backoff para lidar com batch delay do Collector (MEET-020, Verification concern). Max 30s, 1s intervals. Deve passar 100/100 runs.

**Criterio de aceite:** Teste E2E passa no ambiente local (k3d). Pode ser executado no DO.

---

## Ordem de Execucao

```
Phase 1 (infra)              Phase 2A (sync)              Phase 2B (async)          Phase 3
──────────────               ─────────────                ───────────────           ──────────
1.1 Helm chart         ──┐
1.2 HELM_VERSIONS      ──┤
1.3 Pulumi Tempo       ──┤── 2A.1 go get deps    ──┐
1.4 Collector cfg      ──┤   2A.2 tracing pkg     ──┤
1.5 Grafana DS         ──┤   2A.3 main.go boot    ──┤
1.6 Alerting+SM        ──┤   2A.4 HTTP middleware  ──┤── [Design C4] ──┐
1.7 Taskfile           ──┘   2A.5 Sync spans      ──┤                 ├── 3.1 Dashboard
                             2A.6 Context propag   ──┘   Migration  ──┤   3.2 Prod config
                                                         Linked spans─┤   3.3 Integration
                                                         Reconcile  ──┘   3.4 E2E test
```

Phase 1 e Tasks 2A.1-2A.2 podem ser paralelas. Phase 2B requer design aprovado (C4) antes de iniciar.

---

## O que NAO esta no escopo

- Instrumentar CLI (TypeScript) — valor baixo, CLI e o "leaf" do trace
- Instrumentar operators — nao participam do hot path de deploy
- Instrumentar Argo Workflows internamente — ja tem logs no Loki, pods sao processos separados sem OTel SDK
- Migrar metricas de Prometheus para OTel — Prometheus esta maduro
- Migrar logs de Loki para OTel — Promtail esta maduro
- Sampling avancado (tail-based) — volume baixo, probabilistic sampler suficiente (C9)
- Tracing no Cloudflare Worker — fora do cluster
- Cross-cluster Collector federation — futuro, apos validar valor da Phase 2A

---

## Riscos e Mitigacoes

| Risco | Prob. | Impacto | Mitigacao | Owner |
|-------|-------|---------|-----------|-------|
| OTel SDK latencia | Baixa | Baixo | Export timeout 1s, bounded queue, retry disabled (C5) | API Engineer |
| Collector down perde traces | Media | Medio | PrometheusRules: LoopZeroOTelCollectorDown + LoopZeroCollectorExportErrors (C1) | Infra Engineer |
| Tempo storage enche | Baixa | Medio | PrometheusRule: LoopZeroTempoStorageHigh >80% (C1) | Infra Engineer |
| Resource pressure mgmt cluster | Media | Medio | PriorityClass baixa para Tempo (C8), resource audit antes de DO | Infra Engineer |
| Async spans orfaos (pre-2B) | Certa | Baixo | Aceito: Phase 2A entrega valor parcial. Phase 2B resolve. | API Engineer |
| Reconcile loop gera spans demais | Media | Baixo | Spans so em state changes, nao em cada poll tick | API Engineer |
| Tempo AGPL-3.0 | Baixa | Baixo | Interno only, nunca expor para end users | — |
| Existing tests quebram | Media | Alto | Gate: `go test ./...` deve passar antes de merge (C6) | API Engineer |

---

## Dependencias Externas

| Dependencia | Versao | Licenca | Manutencao |
|-------------|--------|---------|------------|
| `grafana/tempo` (Helm) | 1.14.0 | AGPL-3.0 | Ativo (Grafana Labs) |
| `go.opentelemetry.io/otel` | v1.35+ | Apache-2.0 | Ativo (CNCF) |
| `go.opentelemetry.io/otel/sdk` | v1.35+ | Apache-2.0 | Ativo |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc` | v1.35+ | Apache-2.0 | Ativo |
| `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` | v0.60+ | Apache-2.0 | Ativo |

Todas CNCF graduated/incubating. OTel e o padrao da industria para observabilidade.

---

## Estimativa de Impacto

- **Arquivos novos:** ~6 (tracing.go, middleware/tracing.go, tempo values.yaml, prometheusrule, servicemonitor, dashboard JSON)
- **Arquivos modificados:** ~14 (services com spans, config, main.go, collector values, Pulumi, Taskfile, migration)
- **Dependencias Go novas:** 5 (OTel SDK + contrib)
- **Infra nova:** 1 pod (Tempo, ~256Mi RAM, PriorityClass baixa)
- **Migration:** 1 (add `trace_parent` column to `builds` — Phase 2B only)
- **Breaking changes:** Zero. Correlation-ID formato inalterado (C2). Tracing e additive.

---

## Definition of Done

- [ ] Phase 1: Tempo running em k3d + DO, alertas ativos, Grafana datasource OK, ServiceMonitor scraping
- [ ] Phase 2A: Deploy produz trace com 6+ sync spans, Correlation-ID formato UUID v4 inalterado, `go test ./...` sem regressoes
- [ ] Phase 2B: Deploy produz trace completo com async linked spans (build->callback->activate), reconcile span so em state change
- [ ] Phase 3: Dashboard funcional, E2E test passing (100/100 runs), prod config validada
- [ ] Validado em DigitalOcean (Done = funciona no DO)
