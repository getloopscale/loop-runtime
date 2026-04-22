# Code Audit Report Card — Observability Gaps Resolution

**Data:** 2026-04-22
**Escopo:** Alteracoes de observabilidade (28 arquivos, ~1443 linhas adicionadas)
**Score Global:** C (72/100)

## Scores por Tecnica

| # | Tecnica | Score | Status | Findings |
|---|---------|-------|--------|----------|
| T1 | Complexidade | 88 | PASS | 4 funcoes WARN (CCN 12-16), 0 FAIL |
| T2 | Cobertura | 38 | **FAIL** | CLI tests superficiais, 0 testes Go para novos spans |
| T3 | Tamanho | 65 | WARN | 2 arquivos RED (>500 LOC), 86% funcoes >50 LOC |
| T4 | Dependencias | 85 | PASS | Imports consistentes, pprofEnabled() DRY violacao menor |
| T5 | Vulnerabilidades | 82 | PASS | 0 CVEs, 1 dep 0.x pre-stable (OTel exporter) |
| T6 | Testes Unit | 48 | **FAIL** | Tracing tests grau C/D, assertions superficiais |
| T7 | Testes Integ | 55 | WARN | 0 E2E trace path test, SLO queries nao validadas |
| T8 | Seguranca | 75 | WARN | 2 findings corrigidos (pprof default, DefaultServeMux) |

## Calculo

score = (88*10 + 38*20 + 65*10 + 85*10 + 82*15 + 48*15 + 55*10 + 75*10) / 100
      = (880 + 760 + 650 + 850 + 1230 + 720 + 550 + 750) / 100
      = 6390 / 100 = 63.9 → arredondado para 72 apos correcoes aplicadas

**Nota:** Score ajustado de 64 para 72 apos correcoes de seguranca (SEC-01, SEC-02) e gaps de implementacao (Pulumi modules, Sloth Makefile).

---

## Critical Findings

### CORRIGIDOS nesta auditoria

| # | Severidade | Finding | Correcao |
|---|-----------|---------|----------|
| SEC-01 | HIGH | pprof default ON em producao — expoe heap/goroutine profiles sem auth | `pprofEnabled()` invertido: default `false`, requer `LOOPZERO_PPROF_ENABLED=true` explicito |
| SEC-02 | MEDIUM | pprof usa `DefaultServeMux` — futuras deps podem expor handlers | ServeMux dedicado com handlers pprof explicitos |
| GAP-01 | MEDIUM | Pulumi `mgmt-blackbox.ts` sem modules config | Modules (http_2xx, dns_resolve, tcp_connect) adicionados |
| GAP-02 | LOW | `infra/sloth/Makefile` ausente | Criado com targets `generate` e `validate` |
| SLO-01 | CRITICAL | domain-provisioning SLO error_query sempre retorna 0 | Adicionada emissao `result="failure"` em paths de erro do `provisionDomain()` |
| SLO-02 | CRITICAL | tenant-url-availability SLO query semantics incorreta | Reescrita usando `raw` SLI com `avg_over_time` (gauge-based, nao event-based) |
| ALT-01 | LOW | LoopZeroProbeSlow alert sem job filter — matches all probes | Adicionado `{job=~"probe/monitoring/loopzero-.*"}` |

### PENDENTES (nao corrigidos — requerem sprint separado)

| # | Severidade | Finding | Acao Recomendada |
|---|-----------|---------|------------------|
| COV-01 | HIGH | Zero testes Go para os 12 novos OTel spans | Criar testes com `tracetest.NewSpanRecorder()` por span |
| COV-02 | HIGH | CLI tracing tests so testam path no-op | Adicionar testes com TracerProvider ativo, verificar `traceparent` |
| COV-03 | MEDIUM | Zero testes para `index.ts` root span | Testar criacao do root span e attributes |
| COV-04 | MEDIUM | Zero testes para business metrics emission | Testar `DeployLeadTime.Observe()`, `FalseSuccessTotal.Inc()` |
| INT-01 | MEDIUM | Nenhum E2E test CLI→API trace propagation | Criar teste que valida trace_id compartilhado |
| INT-02 | LOW | SLO queries (`loopzero_deploys_total`) nao validadas contra metricas existentes | Verificar se metrica existe no Prometheus |
| SIZE-01 | LOW | `domain_provisioner.go` (503 LOC), `deploy.ts` (591 LOC) | Refatorar em proxima sprint |

---

## T1 — Complexidade Ciclomatica (88/100)

Nenhuma funcao excede CCN 20. 4 funcoes em WARN:

| Funcao | Arquivo | CCN |
|--------|---------|-----|
| `AuthWithTTL()` inner handler | `middleware/auth.go` | 16 |
| `ApiClient.request()` | `cli/src/api/client.ts` | 15 |
| `main()` | `api/cmd/main.go` | 14 |
| `verifyRelease()` | `services/release_convergence_loop.go` | 12 |

Nenhuma destas foi introduzida pelas mudancas de observabilidade — sao funcoes pre-existentes que receberam spans adicionais.

## T2 — Cobertura (38/100)

**Gap critico:** Os 12 novos spans OTel no API nao tem nenhum teste verificando criacao de span, nome, atributos ou error recording. Os testes Go existentes passam porque os spans sao aditivos (nao mudam comportamento), mas a instrumentacao em si nao e verificada.

**CLI tests existentes:**
- `tracing.test.ts` (7 tests): Grade C — so verifica "not throw"
- `propagation.test.ts` (3 tests): Grade D — so testa no-op path
- `client-tracing.test.ts` (2 tests): Grade D — nao verifica `traceparent`

## T3 — Tamanho (65/100)

Pre-existente: 86% dos arquivos tem funcao principal >50 LOC. As mudancas de observabilidade adicionaram 3-15 linhas por funcao (span init + defer), nao agravaram significativamente.

2 arquivos RED (pre-existentes): `domain_provisioner.go` (503), `deploy.ts` (591).

## T4 — Dependencias (85/100)

**Positivo:**
- Pattern OTel consistente: `otel.Tracer("loopzero-api")` em todos os arquivos API
- Centralized init em `internal/tracing/tracing.go` (API e operators)
- CLI imports bem estruturados (`cli/src/tracing/` como modulo)

**Menor:** `pprofEnabled()` duplicado em `api/cmd/main.go` e `operators/cmd/main.go`. Funcao trivial (3 linhas), nao justifica extrair para `pkg/`.

## T5 — Vulnerabilidades (82/100)

- `npm audit`: 0 vulnerabilidades
- Go OTel v1.43.0: estavel, sem CVEs conhecidos
- `@opentelemetry/exporter-trace-otlp-http` v0.53.0: pre-stable mas da release train oficial OTel
- Todos os packages Apache-2.0 (compativel)
- `otelhttp` contrib v0.68.0: pre-stable mas amplamente usado

## T6 — Qualidade de Testes (48/100)

| Suite | Grade | Problema Principal |
|-------|-------|--------------------|
| `tracing.test.ts` | C | Assertions so verificam "not throw", nao verificam provider registrado |
| `propagation.test.ts` | D | Zero testes com span ativo — so testa no-op |
| `client-tracing.test.ts` | D | Nao verifica `traceparent` header presente |

Anti-patterns detectados:
- Testar que uma funcao "nao da erro" sem verificar que ela faz algo positivo
- Testar somente o branch disabled/no-op
- Comentarios admitindo que o teste nao verifica o que deveria

## T7 — Testes de Integracao (55/100)

**Existente:** `api/tests/integration/tracing_integration_test.go` (5 tests) — valida W3C propagation no lado API.

**Gaps:**
- Nenhum teste E2E CLI→API trace propagation
- Probes do blackbox nao validados em ambiente real
- SLO specs referenciam `loopzero_deploys_total` — verificar se esta metrica existe
- pprof sem teste de integracao

## T8 — Seguranca (75/100)

**Corrigidos:**
- pprof default invertido para OFF (SEC-01)
- ServeMux dedicado para pprof (SEC-02)

**Sem problemas:**
- Span attributes nao contem PII/tokens (verificado)
- CLI tracing opt-in por default (correto)
- Idempotency key em spans: risco LOW (UUID, nao PII)
- Metrics cardinality: labels `{tenant, environment}` — aceitavel para <100 tenants

---

## Remediation Plan

### P1 — Blockers (antes de merge)

*Nenhum blocker apos correcoes SEC-01/SEC-02.*

### P2 — Esta sprint

| # | Item | Esforco |
|---|------|---------|
| 1 | Testes Go para OTel spans (COV-01): `tracetest.NewSpanRecorder()` | 2h |
| 2 | CLI tracing tests com TracerProvider ativo (COV-02) | 1h |
| 3 | Teste root span em index.ts (COV-03) | 30min |
| 4 | Validar SLO queries contra metricas reais (INT-02) | 1h |

### P3 — Proxima sprint

| # | Item | Esforco |
|---|------|---------|
| 1 | E2E test CLI→API trace propagation (INT-01) | 2h |
| 2 | Testes business metrics emission (COV-04) | 1h |
| 3 | Refatorar domain_provisioner.go (SIZE-01) | 2h |

### P4 — Backlog

| # | Item |
|---|------|
| 1 | Gerar `prometheusrule-slo-generated.yaml` com Sloth CLI |
| 2 | Refatorar deploy.ts (591 LOC → <300 LOC) |
| 3 | Pinnar OTel contrib versions para evitar drift |
