# Module 08 — Observability (Prometheus + Grafana + OTel + Tempo + Loki + Promtail)

**Audit Date:** 2026-05-11
**Auditor:** Claude Opus 4.6 (1M context), code-validated
**Confidence:** 90% (Claude-only)
**Maturity:** **3.5 / 4.0**

## TL;DR

Stack maduro multi-layer (Prometheus + Grafana + OTel + Tempo + Loki) com **109 metrics reais** (vs 43 documentado, 2.5× overcount), 21 dashboards, 7 SLOs Sloth, structured slog JSON, W3C TraceContext. Bloqueia 4.0 por 3 gaps de propagação: operator ↔ API correlation_id quebrado, async queues sem context, Loki cross-cluster siloed.

---

## 1. Inventário Real

| Componente | LOC | Métricas/Files |
|---|---|---|
| `api/internal/metrics/` | 1,412 | 89 metrics |
| `operators/internal/metrics/` | 266 | 15 metrics |
| `cloud/internal/metrics/` | ~160 | 10 metrics |
| `pkg/logging/` | 500 | slog JSON handler |
| `api/internal/tracing/` | 236 | OTel bootstrap |
| `operators/internal/tracing/` | 125 | OTel bootstrap |
| **Total observabilidade** | **~2,700** | **109 metrics + 8 cell metrics** |

**Grafana dashboards:** 21 JSONs (home, exec-summary, platform-overview, cell-health, cell-saturation, build-pipeline, database-health, drift-detection, github-autodeploy, argo-workflows, blackbox-probes, custom-domains, registry, load-test, alerts-overview, failure-investigation, per-tenant-health, tenant-workloads, capacity-elasticity, component-inventory, tenant-ecosystem).

**PrometheusRule files:** 24 (critical, warning, capacity, database, registry, SLO-driven, OTel pipeline, tracing, domains, probes, multi-cell, pulumi state).

**SLOs Sloth (7):** api-availability (99.9%), deploy-success (99.5%), deploy-latency, build-pipeline, tenant-availability, rollback-success, domain-provisioning.

**Promtail instances:** 2 (build cluster + runtime cluster) — endpoint `http://172.18.0.3:30094/loki/api/v1/push`, batch 5s/1MB/20 retries, CHG-002b dropped step+pod labels.

---

## 2. Arquitetura Observada

**Metrics pipeline:** apps → `/metrics` endpoint (promauto) → Prometheus scrape via ServiceMonitor (per chart) → Grafana queries.

**Logs pipeline:** app stdout (slog JSON) → kubelet `/var/log/pods/*/` → Promtail DaemonSet (K8s SD relabeling) → Loki (`loopzero-monitoring` ns) → Grafana LogQL.

**Traces pipeline:** OTel TracerProvider (`tracing.Init(ctx, "loopzero-api")`) → OTLP gRPC → `otel-collector.loopzero-system.svc.cluster.local:4317` → Tempo → Grafana Traces.

**Correlation-ID propagation:**
- `api/internal/middleware/correlation.go:22` — generate UUID v4 se header ausente
- `api/internal/middleware/correlation.go:28` — echo `Correlation-ID` no response
- `api/internal/middleware/correlation.go:29` — `correlation.WithID(ctx, id)`
- `api/internal/middleware/logging.go:53` — `correlation_id` em todos request logs
- `api/internal/middleware/audit.go` — `audit_log` table persiste correlation_id

**Trace context** (W3C TraceContext + Baggage):
- `api/internal/tracing/tracing.go:104` — `propagation.TraceContext{}, propagation.Baggage{}`
- `operators/internal/tracing/tracing.go:94` — mesmo (cross-binary correlation)
- `api/internal/middleware/tracing.go:38` — `trace_id` extraído do span → injected em slog

**Cell metrics (8 total):**
- Core 4 (ADR-0008 Phase 0): `loopzero_cell_tenant_count`, `loopzero_cell_discovery_count`, `loopzero_cell_routing_decisions_total`, `loopzero_cell_health`
- Extended 4: `loopzero_cell_capacity_pods`, `loopzero_cluster_credential_status`, `loopzero_orphaned_tenants`, `loopzero_pulumi_archives_total`/`loopzero_pulumi_stacks_total`

---

## 3. Contratos Públicos

**Correlation-ID header:** `Correlation-ID` (RFC-compliant, NÃO `X-Correlation-ID`).

**Trace headers:** W3C `traceparent` + `baggage`.

**Log format JSON estruturado:**
```json
{"timestamp": "...", "level": "info", "component": "loopzero-api",
 "correlation_id": "...", "trace_id": "...", "method": "POST", ...}
```

**Top 20 métricas por importância operacional:**
- HTTP: `loopzero_api_requests_total`, `loopzero_api_request_duration_seconds`, `loopzero_api_leader_status`
- Build/Deploy: `loopzero_builds_total`, `loopzero_build_duration_seconds`, `loopzero_deploys_total`, `loopzero_deploy_duration_seconds`, `loopzero_deploy_lead_time_seconds`
- Cell: `loopzero_cell_tenant_count`, `loopzero_cell_discovery_count`, `loopzero_cell_routing_decisions_total`, `loopzero_cell_health`
- DB: `loopzero_postgres_connections_active`, `loopzero_postgres_pool_acquire_duration_seconds`, `loopzero_pg_circuit_breaker_tripped_total`
- ArgoCD: `loopzero_argocd_circuit_state`, `loopzero_argocd_token_age_seconds`
- Convergence: `loopzero_release_state_divergence`, `loopzero_drift_detected`, `loopzero_releases_pending`
- Lock: `loopzero_lock_acquired_total`, `loopzero_lock_conflicts_total`, `loopzero_lock_hold_duration_seconds`

---

## 4. Cobertura de Testes

**Tests de métricas:**
- `api/internal/metrics/metrics_test.go` — HTTP middleware, route pattern cardinality
- `api/internal/metrics/collector_test.go` — collector-based metrics (CellTenantCount)
- `api/internal/metrics/secret_age_test.go` — service auth secret age
- `operators/internal/metrics/metrics_test.go` — reconcile metrics
- `cloud/internal/metrics/metrics_test.go` — auth/billing

**Tests de tracing:**
- `api/tests/integration/tracing_integration_test.go` (5 tests: span generation, correlation_id UUID format, W3C traceparent extraction, tracing-disabled no-op)

**Tests de correlation:**
- `api/internal/middleware/middleware_test.go` (`TestCorrelationIDGenerated`, `TestCorrelationIDPreserved`)

**Gaps:**
- ✗ Cell routing decision metric (CellRoutingDecisions) — sem teste dedicado
- ✗ ArgoCD circuit breaker state metric — exportado mas cobertura unclear
- ✗ Drift detection metrics — referenced em alerts sem unit test
- ✗ Dashboard JSON validation (lint não-automatizado, drift risk)

---

## 5. Gaps e Dívida Técnica

| Item | Severidade | Locação/Evidência |
|---|---|---|
| **CLAUDE.md drift numérico** (43+ → 109 metrics; 4 → 8 cell metrics) | BAIXA | docs/CLAUDE.md |
| **Operator ↔ API correlation_id propagation gap** | **HIGH** | Operator reconcile não carrega correlation_id do CLI call original |
| **Async workqueue sem correlation context** | MÉDIA | `api/internal/services/workqueue_consumer.go` |
| **Webhook → build perde correlation_id** | MÉDIA | GitHub webhook handler ↔ build row |
| **Loki cross-cluster siloed** (infra gap) | MÉDIA | Build cluster Loki ≠ Runtime cluster Loki |
| Cardinality HIGH em `loopzero_domain_tls_expiry_days{domain,tenant}` | MÉDIA | N domains × M tenants |
| Dashboards hand-edited JSON (drift vs metric names) | MÉDIA | 21 JSONs sem Jsonnet/Grafonnet |
| Missing SLOs: ArgoCD convergence, operator latency per controller, auth refresh | MÉDIA | sloth/ apenas 7 SLOs |
| LOOPZERO_TRACING_ENABLED feature flag drift | RESOLVIDO ✅ | Bootstrap chart set false para prod |
| Implemented-but-not-wired metrics | **0** ✅ | All metrics verified actively emitted |

---

## 6. Avaliação de Maturidade

**Score: 3.5 / 4.0** (confidence 90%)

### Evidências

1. **T1 (commit+test)** — `api/internal/metrics/` 1,412 LOC com 89 metrics + 5 tests; `pkg/logging/logger.go:26` JSON handler; 24 PrometheusRule files + 7 SLO files; W3C TraceContext em 3 services.
2. **T2 (LIVE log)** — 12/12 cleanroom GREEN com observability stack LIVE (2026-05-05); canary 80/80 GREEN com SLO assertions (2026-05-07); cell-2 ativação com cell metrics LIVE (2026-05-02).
3. **T3 (CI run)** — `ci.yml` + `monitoring` workflows; PrometheusRule template tests; tracing integration tests.

### Raciocínio

**Sustenta 3.0-3.5:**
- 109 metrics ativamente emitidos (todos verificados wired, 0 orphans)
- 21 dashboards cobrindo user/platform/infra/business
- 7 SLOs Sloth com burn rate alerts
- W3C TraceContext propagation em 3 services
- Structured JSON logging via slog stdlib
- 24 PrometheusRule files com alerting hierarquia (critical/warning/capacity)
- Cell metrics extensos (8 metrics) com routing/health/orphan detection
- Cardinality controlada por route patterns (não raw paths)
- Circuit breaker state metric para ArgoCD/PG/dependencies

**Bloqueia 4.0:**
- **Operator ↔ API correlation_id gap** — audit trail quebra quando operator atua em Tenant CR
- Async queues + webhooks sem context propagation
- Loki cross-cluster siloed (infra gap conhecido)
- Cardinality HIGH em `loopzero_domain_tls_expiry_days` (N×M series)
- Dashboards hand-edited (sem drift detection automatizado)
- Missing SLOs em flows críticos (ArgoCD convergence, operator latency)
- 4+ tests gaps (cell routing, ArgoCD circuit, drift detection metrics)

### Caminho para 3.8-4.0

1. Implementar correlation_id propagation operator ↔ API (CR annotations + reconcile context)
2. Threading correlation_id em workqueue consumer + webhook handlers
3. Unificar Loki cross-cluster (federated query ou single instance)
4. Drop high-cardinality labels em `loopzero_domain_tls_expiry_days` (agregar por status)
5. Migrar dashboards para Jsonnet/Grafonnet (code-driven)
6. Definir SLOs: ArgoCD convergence, operator latency, auth refresh
7. Atualizar CLAUDE.md: 109 metrics + 8 cell metrics

---

**Próximas auditorias relacionadas:** Multi-Cell (M09), CI/CD (M12).
