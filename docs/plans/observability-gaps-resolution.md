# Plano: Resolução de Todos os Gaps de Observability

> Aprovado em 2026-04-22. Baseado em análise comparativa entre stack SOTA e estado atual do LoopZero.

## Contexto

Uma análise comparou a stack de observability SOTA para PaaS contra o estado atual do LoopZero. O LoopZero já tem uma base sólida (50+ métricas, 19 dashboards, 24+ alertas, OTel+Tempo+Loki), mas 9 gaps foram identificados. Este plano resolve todos, organizados em 7 fases com dependências claras.

### Estado Atual (já implementado)
- **Métricas:** 50+ Prometheus custom (`loopzero_*`), cross-cluster scrape, ServiceMonitors
- **Tracing:** OTel SDK no API (11 spans) + Operators (22+ spans em 7 controllers), Collector → Tempo
- **Logs:** Loki + Promtail, S3 backend, 7d retention
- **Dashboards:** 19 Grafana dashboards
- **Alertas:** 24+ PrometheusRules com runbooks, Slack routing
- **SLOs:** 7 targets como threshold alerts (não formais)

---

## Resumo de Fases

| Fase | Gaps | Tier | Escopo | Dependência |
|------|------|------|--------|-------------|
| 1 | Blackbox Probes + Telemetry Pipeline | T1/T0 | M | Nenhuma |
| 2 | Deploy E2E Trace (12 spans no API) | T1 | L | Nenhuma |
| 3 | SLOs formais com Sloth | T2 | M | Fase 1 |
| 4 | Business Metrics + Exec Dashboard | T1 | M | Fase 3 |
| 5 | CLI Tracing | T1 | L | Fase 2 |
| 6 | pprof Endpoints | T1 | S | Nenhuma |
| 7 | Cilium + Hubble | T3 | XL | **ADIADO** pós-launch |

Fases 1, 2, 5 e 6 podem ser desenvolvidas **em paralelo**. Fase 3 depende de Fase 1. Fase 4 depende de Fase 3.

```
Fase 1 (Blackbox + Pipeline) ───┐
                                 ├──► Fase 3 (Sloth SLOs) ──► Fase 4 (Metrics + Dashboard)
Fase 2 (API Spans) ─────────────┘                    
         │
         └──────────────────────────► Fase 5 (CLI Tracing)
Fase 6 (pprof) ──── independente
Fase 7 (Cilium) ──── ADIADO
```

---

## FASE 1: Blackbox Probes + Telemetry Pipeline Health

**Tier:** T1 (infra-only, sem mudança em código de aplicação)  
**Escopo:** Medium

### 1A — Blackbox Exporter

**Arquivos a criar:**

| Arquivo | Descrição |
|---------|-----------|
| `infra/helm/blackbox-exporter/values.yaml` | Helm values: modules `http_2xx`, `dns_resolve`, `tcp_connect` |
| `infra/helm/monitoring/manifests/probe-targets.yaml` | Prometheus `Probe` CRD: API health, tenant URLs, ArgoCD, Gitea, Traefik |
| `infra/helm/monitoring/manifests/prometheusrule-probes.yaml` | Alertas: `LoopZeroAPIProbeFailure`, `LoopZeroTenantURLProbeFailure`, `LoopZeroTLSCertExpiring`, `LoopZeroDNSResolutionFailure` |
| `infra/helm/monitoring/dashboards/blackbox-probes.json` | Dashboard: probe success rate, latência, TLS expiry, DNS resolution |
| `infra/pulumi/src/components/mgmt-blackbox.ts` | Pulumi component (padrão: `mgmt-tempo-otel.ts`) |

**Arquivos a modificar:**

| Arquivo | Mudança |
|---------|---------|
| `infra/pulumi/src/components/bootstrap-shared.ts` | Adicionar `blackboxExporter: "9.2.0"` a `HELM_VERSIONS` (L25-42) |
| `infra/pulumi/src/index.ts` | Chamar `installBlackboxExporter()` |
| `infra/helm/monitoring/values.yaml` | Adicionar scrape config para blackbox em `additionalScrapeConfigs` |
| `Taskfile.yml` | Tasks `blackbox:install` e `blackbox:test` |

**Probe targets:**
- `https://{API_URL}/api/v1/health` — API availability
- `https://*.dev.loopzero.example` — tenant URL health (sample representativo)
- `https://argocd-server.argocd.svc/healthz` — ArgoCD interno
- `http://gitea-http.gitea.svc:3000/api/v1/version` — Gitea
- `http://traefik.traefik.svc:9000/ping` — Traefik ingress

**Alertas:**
- `LoopZeroAPIProbeFailure`: `probe_success{job="blackbox-api"} == 0` por 2m → critical
- `LoopZeroTenantURLProbeFailure`: `probe_success{job="blackbox-tenant"} == 0` por 5m → warning
- `LoopZeroTLSCertExpiring`: `probe_ssl_earliest_cert_expiry - time() < 7*86400` → warning
- `LoopZeroDNSResolutionFailure`: `probe_dns_lookup_time_seconds > 5` → warning

### 1B — Telemetry Pipeline Observability

**Arquivos a criar:**

| Arquivo | Descrição |
|---------|-----------|
| `infra/helm/monitoring/manifests/servicemonitor-otel-collector.yaml` | ServiceMonitor para porta 8890 do Collector (padrão: `servicemonitor-tempo.yaml`) |
| `infra/helm/monitoring/manifests/prometheusrule-otel-pipeline.yaml` | Alertas: `LoopZeroOTelSpanDropRate`, `LoopZeroOTelExportFailureRate`, `LoopZeroOTelQueueSaturation` |

**Arquivos a modificar:**

| Arquivo | Mudança |
|---------|---------|
| `infra/helm/monitoring/dashboards/platform-overview.json` | Nova row "Telemetry Pipeline Health": span throughput, drop rate, export success, queue utilization |

**Testes:**
- `kubeconform --strict` em todos os YAML criados
- Deploy no k3d, verificar probes no Prometheus targets
- Verificar métricas `otelcol_*` e `probe_*` aparecem no Grafana

---

## FASE 2: Deploy E2E Trace — 12 Spans Faltantes no API

**Tier:** T1 (instrumentação aditiva, sem mudança de comportamento)  
**Escopo:** Large

**Padrão a seguir** (de `api/internal/services/build_orchestrator_submit.go:41-47`):
```go
ctx, span := otel.Tracer("loopzero-api").Start(ctx, "loopzero.api.<subsystem>.<operation>",
    trace.WithAttributes(
        attribute.String("loopzero.tenant", tenantID),
    ))
defer span.End()
```

### 12 Spans a adicionar:

| # | Arquivo | Função | Span Name | Atributos |
|---|---------|--------|-----------|-----------|
| 1 | `api/internal/middleware/auth.go` | `AuthWithTTL()` handler | `loopzero.api.auth.validate_token` | tenant_id, user_id, result |
| 2 | `api/internal/services/build_orchestrator_submit.go` | `SubmitBuild()` → chamada `EnsureProject` | `loopzero.api.project.ensure` | tenant, project (child do span existente) |
| 3 | `api/internal/middleware/idempotency.go` | `Idempotency()` handler | `loopzero.api.idempotency.check` | key, hit (bool) |
| 4 | `api/internal/argocd/sync_client.go` | `TriggerSync()` | `loopzero.api.argocd.sync_trigger` | app_name |
| 5 | `api/internal/routes/argocd_callback.go` | `Handle()` | `loopzero.api.argocd.callback_received` | app_name, sync_status, health_status, revision |
| 6 | `api/internal/routes/health.go` | `DeepHealth()` | `loopzero.api.health.deep_check` | status |
| 7 | `api/internal/services/release_activator.go` | `ActivateReleaseKernel()` | `loopzero.api.release.activate` | release_id, tenant, environment |
| 8 | `api/internal/services/rollback_orchestrator.go` | `Rollback()` | `loopzero.api.rollback.execute` | current_release_id, target_release_id, environment |
| 9 | `api/internal/services/domain_provisioner.go` | `provisionDomain()` / `removeDomain()` | `loopzero.api.domain.provision` / `.remove` | domain, tenant |
| 10 | `api/internal/services/build_reconcile_single.go` | `reconcileBuild()` | `loopzero.api.build.reconcile` | build_id, workflow_phase |
| 11 | `api/internal/github/webhook/handler.go` | `ServeHTTP()` / `dispatch()` | `loopzero.api.webhook.github.receive` | event_type, delivery_id |
| 12 | `api/internal/github/autodeploy.go` | `HandlePush()` | `loopzero.api.github.autodeploy` | repo, commit_sha, project |

**Resultado:** O trace de deploy cobrirá a jornada completa:
```
CLI submit → auth → idempotency → lock → project.ensure → build.submit 
  → workflow_submit → source.upload → gitops.push → argocd.sync_trigger 
  → argocd.callback → release.activate → health.deep_check
```

**Testes (por span):**
- Unit test com `tracetest.NewSpanRecorder()`: verificar nome, atributos e error recording
- Integration test final: deploy completo no k3d → query Tempo para trace com 23 spans (11 existentes + 12 novos)

---

## FASE 3: SLOs Formais com Sloth

**Tier:** T2 (substitui regras de alerta existentes — REQUER meeting review)  
**Escopo:** Medium  
**Depende de:** Fase 1 (métrica `probe_success` para SLO de tenant availability)

### 7 SLO Specs

**Arquivos a criar em `infra/sloth/`:**

| Arquivo | SLO | SLI | Objetivo |
|---------|-----|-----|----------|
| `deploy-success.yaml` | Deploy Success | `loopzero_deploys_total{status="success"}` / total | 99.5% |
| `deploy-latency.yaml` | Deploy Latency | `loopzero_deploy_duration_seconds_bucket{le="300"}` / count | 95% < 5min |
| `rollback-success.yaml` | Rollback Success | `loopzero_rollbacks_total{status="success"}` / total | 99.9% |
| `tenant-availability.yaml` | Tenant Availability | `probe_success{job="blackbox-tenant"}` | 99.9% |
| `api-availability.yaml` | API Availability | `loopzero_api_requests_total{status!~"5.."}` / total | 99.9% |
| `build-pipeline.yaml` | Build Pipeline | `loopzero_builds_total{status="SUCCEEDED"}` / total | 99% |
| `domain-provisioning.yaml` | Domain Provisioning | `loopzero_domain_provisioning_duration_seconds_bucket{result="success"}` | 99% |

**Geração:**
```bash
sloth generate -i infra/sloth/*.yaml -o infra/helm/monitoring/manifests/prometheusrule-slo-generated.yaml
```

**Arquivos adicionais:**

| Arquivo | Descrição |
|---------|-----------|
| `infra/sloth/Makefile` | Target `generate` e `validate` |
| `infra/helm/monitoring/manifests/prometheusrule-slo-generated.yaml` | Output do Sloth (committed) |

**Arquivos a modificar:**

| Arquivo | Mudança |
|---------|---------|
| `infra/helm/monitoring/manifests/prometheusrule-slos.yaml` | Remover alertas manuais agora cobertos pelo Sloth: `LoopZeroQueueSlow`, `LoopZeroDeployFailureRate`, `LoopZeroLockContention`, `LoopZeroSyncSlow` |
| `Taskfile.yml` | Tasks `sloth:generate` e `sloth:validate` |

**Testes:**
- `sloth validate` em cada spec
- `kubeconform --strict` no YAML gerado
- Verificar métricas `slo:sli_error:ratio_rate5m` e `slo:error_budget:ratio` no Prometheus

---

## FASE 4: Business Metrics + Exec Summary Dashboard

**Tier:** T1  
**Escopo:** Medium  
**Depende de:** Fase 3 (dashboard usa métricas de error budget do Sloth)

### 4A — Novas Métricas de Negócio

**Arquivo a modificar:** `api/internal/metrics/metrics.go`

| Métrica | Tipo | Labels | Buckets |
|---------|------|--------|---------|
| `loopzero_deploy_lead_time_seconds` | Histogram | tenant, environment | 30, 60, 120, 180, 300, 600, 900, 1800 |
| `loopzero_false_success_total` | Counter | tenant, environment | — |
| `loopzero_retry_amplification_total` | Counter | operation | — |

**Pontos de emissão:**

| Métrica | Arquivo | Ponto de emissão |
|---------|---------|------------------|
| `deploy_lead_time` | `api/internal/services/release_activator.go` | Após ativação: `time.Since(build.CreatedAt)` |
| `false_success` | `api/internal/services/release_convergence_loop.go` | Quando divergência detectada em release ACTIVE |
| `retry_amplification` | `api/internal/services/build_orchestrator_retry.go` | Incrementar por correlation_id |

### 4B — Exec Summary Dashboard

**Arquivo a criar:** `infra/helm/monitoring/dashboards/exec-summary.json`

**Panels:**
1. SLO Error Budgets — stat panels por SLO (`slo:error_budget:ratio`)
2. SLO Burn Rates — time series (`slo:sli_error:ratio_rate5m`)
3. Deploy Success Trend — 7d/30d rolling
4. Tenant Availability — aggregate blackbox probes
5. Deploy Lead Time — p50/p95/p99 (`loopzero_deploy_lead_time_seconds`)
6. False Success Rate — trend (`loopzero_false_success_total`)

**Testes:**
- Unit tests para registro das métricas (nomes, labels, buckets)
- Deploy no k3d → verificar métricas no Prometheus
- Dashboard renderiza sem erros no Grafana

---

## FASE 5: CLI Tracing (OpenTelemetry no TypeScript)

**Tier:** T1  
**Escopo:** Large  
**Depende de:** Fase 2 (API já aceita W3C TraceContext via `propagation.TraceContext{}` em `api/internal/tracing/tracing.go:101`)

### Dependências npm a adicionar em `cli/package.json`:
- `@opentelemetry/api`
- `@opentelemetry/sdk-trace-node`
- `@opentelemetry/exporter-trace-otlp-http`
- `@opentelemetry/resources`
- `@opentelemetry/semantic-conventions`

### Arquivos a criar:

| Arquivo | Descrição |
|---------|-----------|
| `cli/src/tracing/index.ts` | Init/shutdown OTel: `OTLPTraceExporter` → Collector, service name `loopzero-cli`, graceful degradation |
| `cli/src/tracing/propagation.ts` | Inject `traceparent` header em requests via `propagation.inject()` |

### Arquivos a modificar:

| Arquivo | Mudança |
|---------|---------|
| `cli/src/index.ts` | `initTracing()` no startup, `shutdownTracing()` no cleanup, root span `loopzero.cli.command` |
| `cli/src/api/client.ts` | Na construção de `headers` (L188-200): injetar trace context via `propagation.inject()` |
| `cli/src/commands/deploy.ts` | Spans: `loopzero.cli.deploy.bundle`, `loopzero.cli.deploy.upload`, `loopzero.cli.deploy.stream` |

### Resultado:
O span `loopzero.cli.command` do CLI será **parent** de todos os spans do API, criando um trace unificado de ponta a ponta:
```
loopzero.cli.command (CLI)
  └── loopzero.cli.deploy.upload
       └── loopzero.api.auth.validate_token (API)
            └── loopzero.api.idempotency.check
                 └── loopzero.api.lock.acquire
                      └── loopzero.api.build.submit
                           └── ...
```

### Configuração:
- `LOOPZERO_OTEL_ENDPOINT` — endpoint do Collector (default: não enviar, opt-in)
- `LOOPZERO_TRACING_ENABLED` — feature flag (default: `false` no CLI, não impactar DX)
- **Degradação graciosa:** se collector inacessível, traces descartados silenciosamente

### Testes:
- `cli/tests/unit/tracing/tracing.test.ts` — init/shutdown, sem crash quando collector off
- `cli/tests/unit/tracing/propagation.test.ts` — formato `traceparent` correto
- `cli/tests/unit/api/client-tracing.test.ts` — trace context injetado nos headers
- Integration: `loopzero deploy` → query Tempo para trace CLI→API→Operators

---

## FASE 6: pprof Endpoints

**Tier:** T1  
**Escopo:** Small

### Arquivos a modificar:

| Arquivo | Mudança |
|---------|---------|
| `api/cmd/main.go` | Blank import `_ "net/http/pprof"`, goroutine com `http.ListenAndServe(":6060", nil)`, controlado por `LOOPZERO_PPROF_ENABLED` |
| `operators/cmd/main.go` | Mesmo padrão, porta `:6061` |

### Segurança:
- pprof roda em porta SEPARADA (6060/6061) da API (cfg.Port) e métricas (cfg.MetricsPort)
- **NÃO** exposto via Traefik/Ingress — acesso apenas via `kubectl port-forward`
- Feature flag `LOOPZERO_PPROF_ENABLED`: default `true` em dev, `false` em prod

### Arquivos a criar:

| Arquivo | Descrição |
|---------|-----------|
| `docs/runbooks/pprof-profiling.md` | Como usar: port-forward, `go tool pprof`, exemplos CPU/memory/goroutine |

### Taskfile:
- `task pprof:api` — `kubectl port-forward deploy/loopzero-api 6060:6060`
- `task pprof:operators` — `kubectl port-forward deploy/loopzero-operator 6061:6061`

### Testes:
- Deploy no k3d, port-forward, verificar `/debug/pprof/` responde
- Verificar porta NÃO acessível externamente

---

## FASE 7: Cilium + Hubble — ADIADO

**Tier:** T3 (migração de CNI em 3 clusters)  
**Status:** Adiado para pós-launch  
**Motivo:** Migração de CNI é alto risco, requer downtime potencial, e não é proporcional ao benefício em pré-release com poucos tenants.

**Ação:** Criar `docs/plans/cilium-hubble-migration.md` documentando:
- Fases de migração (workload → build → mgmt)
- Dashboards Hubble planejados (L3/L4/L7 flow visibility)
- Pré-requisitos e riscos

---

## Verificação End-to-End

Após todas as fases implementadas, a validação final:

1. **Deploy completo via CLI** → trace no Tempo com spans CLI→API→Operators (35+ spans)
2. **SLO burn rates** visíveis no Exec Summary dashboard
3. **Blackbox probes** mostrando status de todos os endpoints
4. **Alertas SLO** disparam corretamente em teste de carga
5. **pprof** acessível via port-forward em API e operators
6. **Pipeline telemetry** mostrando span throughput e drop rate
7. `task test` — todos os testes passam
8. `task lint` — sem violações
9. `task validate:manifests` — `kubeconform --strict` passa em todos os YAML

---

## Notas de Governança

- **Fases 1, 2, 4, 5, 6:** T0/T1 — implementação direta com TDD
- **Fase 3 (Sloth):** T2 — substitui regras de alerta existentes → **requer meeting review** antes da implementação
- **Fase 7 (Cilium):** T3 — **requer meeting + ADR** → adiado
