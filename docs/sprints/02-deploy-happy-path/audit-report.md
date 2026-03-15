# Sprint 2 — Audit Report: Deploy Happy Path

> **Data:** 2026-03-15
> **Auditor:** Especialista PaaS (3 iterações, 5 agentes paralelos + verificação manual line-by-line)
> **Branch:** `feat/sprint-1`
> **Método:** Código auditado contra `docs/technical.md` §0.1, §0.3, §4.2-4.4, §5.1-5.2, §7.2, §7.4, §7.8, §10.1, §12.1, §17.2

---

## Veredito Executivo

A Sprint 2 está **substancialmente implementada nas camadas API e CLI**, com concurrency controls sólidos, state machines corretas, e zero fakes em produção. A arquitetura DIP funcionou perfeitamente — todas as interfaces têm implementações reais.

Porém existem **4 bloqueantes** que impedem o exit gate e **4 bugs críticos** que corrompem estado ou perdem observabilidade em produção.

| Categoria | Contagem |
|-----------|----------|
| Bloqueantes (exit gate) | 4 |
| Críticos (bugs em produção) | 4 |
| Altos (risco significativo) | 5 |
| Médios (code smells / debt) | 9 |
| Gaps documentados (sprints futuros) | 4 |

**Estimativa para desbloqueio:** ~3h de trabalho focado para bloqueantes + críticos.

---

## Verificações Positivas (Spec Compliance Confirmado)

Antes dos problemas, o que está **correto e verificado**:

| Requisito | Status | Evidência |
|-----------|--------|-----------|
| Zero `Fake*` em `main.go` e `adapters/` | **PASS** | grep retorna 0 matches |
| Zero `Fake*` em `operators/internal/` (produção) | **PASS** | `fake.go` não existe |
| Zero `operators/internal/statemachine/` (DRY) | **PASS** | Importa de `pkg/statemachine/` |
| Zero termos internos no CLI (Rule 3) | **PASS** | grep em `cli/src/*.ts` retorna 0 |
| `pg_advisory_xact_lock` (não session-scoped) | **PASS** | Ambas implementações usam `xact` |
| `SET LOCAL lock_timeout = '5s'` | **PASS** | Nas duas implementações |
| Concurrent build check DENTRO do lock (Rule 6) | **PASS** | `build_orchestrator.go:155-163` |
| Build state machine completa (§5.1) | **PASS** | Todos os estados/transições/timeouts |
| Release state machine completa (§5.2) | **PASS** | SYNC_FAILED → PENDING retry |
| `IsBuildTerminal` exportada de `pkg/statemachine/` | **PASS** | 15 usos corretos no codebase |
| `allConditionsTrue` inclui `PriorityClassReady` | **PASS** | `tenant_controller.go:203` |
| Callback retorna 202 imediato + goroutine | **PASS** | `builds.go:267` |
| Callback auth via shared secret | **PASS** | `builds.go:212-221` |
| Pool config PostgreSQL (§7.8) | **PASS** | `db/pool.go` com MaxConns=20, MinConns=5, statement_timeout=30s |
| Exit codes CLI: 0/1/2/3/4/5 | **PASS** | Verificado no código e 298 testes |
| `--json` output com schema correto | **PASS** | Testes cobrem shape |
| Build reaper (§5.1 timeouts) | **PASS** | `main.go:229-236`, wired e rodando |
| Workflow template `loopzero-build-nodejs-v1` | **PASS** | 700+ linhas, todos os steps |
| GitOps push com retry (fetch-merge-push) | **PASS** | `workflow-template:689-699` |
| Binários compilados NÃO tracked no git | **PASS** | `git ls-files` limpo |
| Idempotency middleware funcional | **PASS** | Testes de integração passam |
| DIP funciona (interfaces isolam infra de negócio) | **PASS** | 9 fakes removidos sem alterar orchestrator |
| `PromotionSyncFailed`/`RollbackSyncFailed` tratados | **PASS** | Terminal states corretos |
| CancelBuild wrapped em advisory lock | **PASS** | `build_orchestrator.go:248` |
| OnBuildCompleted inside advisory lock (ambas fases) | **PASS** | Phase 1: linha 330, Phase 2: linha 409 |
| State transitions via state machine (nunca update direto) | **PASS** | Verificado em todo o código |

---

## B — BLOQUEANTES (Exit Gate Impossível Sem Correção)

### B1. PoolEnvironmentLocker não detecta lock timeout

**Arquivo:** `api/internal/adapters/adapters.go:110-112`
**Spec:** §4.2 — lock timeout de 5s deve retornar erro tipado

```go
// adapters.go:110 — PRODUÇÃO (usado pelo main.go)
if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", lockKey); err != nil {
    return err  // ← RETORNA ERRO CRU DO POSTGRES (55P03)
}

// lock/environment_lock.go:64-68 — CORRETO (NÃO usado pelo main.go)
if isLockTimeoutError(err) {
    return zero, loopzeroerrors.ErrLockTimeout  // ← CONVERTE PARA DOMÍNIO
}
```

**Impacto:** Dois deploys concorrentes: o segundo recebe erro PostgreSQL 55P03 cru em vez de `409 DEPLOY_IN_PROGRESS`. CLI mostra erro genérico. O test do exit gate ("exatamente um 202, um 409") falha em produção.

### B2. PoolEnvironmentLocker sem panic recovery

**Arquivo:** `api/internal/adapters/adapters.go:93-124`
**Comparação:** `lock/environment_lock.go:76-91` tem recovery; adapter não.

**Impacto:** Panic no callback (ex: invalid state machine transition = panic per spec §5) deixa transação PostgreSQL aberta. Connection pool leak progressivo. Após N panics, API inteira trava.

### B3. Correlation-ID nos git trailers é falso

**Arquivo:** `infra/helm/argo-workflows/templates/workflow-template-nodejs-v1.yaml:682-684`
**Spec:** §17.5 — cada trailer deve ter valor distinto

```
Release-ID: {{workflow.parameters.build-id}}      ← TODOS IGUAIS
Correlation-ID: {{workflow.parameters.build-id}}   ← DEVERIA SER UUID DO REQUEST
Build-ID: {{workflow.parameters.build-id}}         ← OK
```

**Impacto:** Rastreabilidade end-to-end quebrada. Em incidente de produção, impossível correlacionar request CLI → API → Workflow → Git commit.

### B4. Circuit breaker implementado mas NÃO wired

**Arquivo:** `api/cmd/main.go:197` — usa `argocd.NewSyncClient()` SEM wrapper
**Código existente:** `api/internal/services/argocd_client.go` — `CircuitBreakerSyncClient` com 130 linhas e 10 testes passando
**Spec:** §10.1 — "ArgoCD health gate: 3 consecutive failures → circuit OPEN → 503 PLATFORM_DEGRADED"
**Sprint task:** `sprint.md:68` — listado como task explícita

```go
// main.go:197 — ATUAL (sem circuit breaker)
syncClient := argocd.NewSyncClient(argocd.SyncClientConfig{...})

// CORRETO:
rawSync := argocd.NewSyncClient(...)
syncClient := services.NewCircuitBreakerSyncClient(rawSync, services.CircuitBreakerConfig{
    FailureThreshold: 3, RecoverySuccesses: 2, RecoveryInterval: 30 * time.Second,
})
```

**Impacto:** ArgoCD degradado → cada deploy segura lock por 2min (sync timeout). 10 deploys simultâneos = cascading failure across all tenants. O circuit breaker **existe e funciona** mas não está conectado.

---

## C — CRÍTICOS (Bugs Que Corrompem Estado ou Perdem Observabilidade)

### C1. Erro silencioso `_ = cancelErr` no CancelBuild

**Arquivo:** `api/internal/services/build_orchestrator.go:271`
**Spec:** Princípios §8 — "NUNCA engula exceções"

```go
if cancelErr := o.workflow.Cancel(ctx, build.WorkflowID); cancelErr != nil {
    _ = cancelErr  // ← ENGOLIDO — workflow fantasma continua rodando
}
```

### C2. Erro silencioso `_ = err` na emissão de activation failure

**Arquivo:** `api/internal/services/build_orchestrator.go:434`

```go
if _, err := o.eventStore.Append(ctx, buildID, "deploy.sync_failed", failedData); err != nil {
    _ = err  // ← CLI nunca recebe deploy.sync_failed, stream aberto para sempre
}
```

### C3. `IsBuildTerminal` reimplementada localmente no adapter

**Arquivo:** `api/internal/adapters/adapters.go:216-220`
**Enquanto 15 outros usos** usam `statemachine.IsBuildTerminal()`, o adapter tem switch local. Novo terminal state não propagaria.

### C4. Idempotency store error swallowed

**Arquivo:** `api/internal/middleware/idempotency.go:111`

```go
_ = store.Store(r.Context(), rec)  // ← Se DB falha, retry reprocessa em vez de idempotent replay
```

**Impacto:** Contrato de idempotência (§4.3) quebrado silenciosamente.

---

## H — ALTOS (Risco Significativo)

| # | Finding | Arquivo | Fix |
|---|---------|---------|-----|
| H1 | Lock duplicado: `lock/` (completo) vs `adapters/` (incompleto). Produção usa o incompleto. | `api/internal/` | Eliminar `PoolEnvironmentLocker`, usar `lock.WithEnvironmentLock` |
| H2 | `test:unit` no Taskfile não roda CLI tests (298 testes TypeScript ignorados) | `Taskfile.yml:285-294` | Adicionar `cd cli && npm test` |
| H3 | `validate:manifests` valida apenas `operators/config/` e `infra/tilt/`. 17+ manifests Helm ignorados. | `Taskfile.yml:322-329` | Expandir scope |
| H4 | Cross-cluster IPs hardcoded (`10.0.1.3`, `10.0.3.6`) — quebra em qualquer outra máquina | `infra/tilt/api-deployment.yaml:46,77` | Discovery dinâmico |
| H5 | `operator-deployment.yaml` tem `readOnlyRootFilesystem: true` sem mount para `/tmp` | `infra/tilt/operator-deployment.yaml:155` | Adicionar emptyDir mount |

---

## M — MÉDIOS (Code Smells / Technical Debt)

| # | Finding | Arquivo | Detalhe |
|---|---------|---------|---------|
| M1 | `WorkflowSpec.CallbackURL` dead code | `build_orchestrator.go:185` | Setado mas nunca lido pelo `WorkflowSubmitter` (usa `infraParams`) |
| M2 | `tenant_controller.go` — 790 linhas (God File) | `operators/internal/controller/` | 9 reconciliation methods + 10 helpers. SRP violation |
| M3 | `build_orchestrator.go` — 682 linhas | `api/internal/services/` | `activateRelease()` monolítica (~180 linhas) |
| M4 | `db:migrate` usa `ls \| sort` | `Taskfile.yml:210` | Locale-dependent, sem verificação de sucesso |
| M5 | `task test` só roda `test:unit` | `Taskfile.yml:280-283` | Sem integration/e2e por default |
| M6 | 12+ `json.Marshal` ignoram erros | `build_orchestrator.go` | SSE events potencialmente null |
| M7 | SSE client sem testes de reconnect | `cli/tests/unit/` | Last-Event-ID, JOURNAL_EXPIRED não testados |
| M8 | `lint` task não inclui CLI | `Taskfile.yml:315-320` | ESLint nunca executa |
| M9 | Partial failure (exit 5) não atingível end-to-end | Pipeline | Workflow não suporta per-app builds independentes |

---

## G — Gaps Documentados (Sprints Futuros)

| # | Gap | Spec | Sprint provável | Justificativa |
|---|-----|------|-----------------|---------------|
| G1 | Ephemeral credentials per build (tokens admin compartilhados) | §17.2 | Sprint 6 (Security) | Supply chain, não blocking para happy path |
| G2 | Startup reconciler (API crash recovery) | §12.1 | Sprint 8 (Hardening) | Resiliência, não happy path |
| G3 | Per-app build independence (partial failure support) | §0.3, §7.4 | Sprint 3 ou 4 | Envolve refactor do workflow template |
| G4 | `detect-changeset` step no workflow | §7.4 | Sprint 3 ou 4 | Otimização, não blocking |

---

## Errata (Falsos Positivos Corrigidos)

| Finding reportado na iteração 1 | Veredicto | Motivo |
|---|---|---|
| "CallbackURL hardcoded `localhost` é bloqueante" | **FALSO POSITIVO** | `WorkflowSubmitter.Submit()` usa `infraParams.CallbackURL` (de `main.go:183`), não `spec.CallbackURL`. O campo é dead code, não bug funcional. |
| "Argo Workflow template missing" | **FALSO POSITIVO** | Template existe em `infra/helm/argo-workflows/templates/workflow-template-nodejs-v1.yaml` (700+ linhas). |

---

## Plano de Ação — Status

```
FASE 1 — BLOQUEANTES
─────────────────────────────────────
[DONE] B1+B2+H1: Unificar lock → PoolEnvironmentLocker delega para lock.WithEnvironmentLock
[DONE] B4: Wire CircuitBreakerSyncClient no main.go (OpenThreshold=3, CloseThreshold=2, RetryInterval=30s)
[DONE] B3: Fix git trailers — correlation-id como param separado, wired end-to-end
[DONE] C1+C2: Trocar _ = por slog.Warn/slog.Error
[DONE] C3: adapter IsBuildTerminal → statemachine.IsBuildTerminal()
[DONE] C4: idempotency store error → slog.Error com contexto

FASE 2 — ALTOS
─────────────────────────────────────
[DONE] H2+M8: CLI no test:unit e lint (Taskfile.yml)
[DONE] H3: validate:manifests inclui monitoring/manifests/
[DONE] H5: operator /tmp mount com emptyDir
[DONE] M1: Remover WorkflowSpec.CallbackURL e SubmitBuildRequest.CallbackURL dead code

FASE 3 — MÉDIOS (sprint 3 backlog)
─────────────────────────────────────
[ ] M2: tenant_controller.go 790 linhas (SRP refactor)
[ ] M3: build_orchestrator.go 682 linhas (activateRelease decomposition)
[ ] M4: db:migrate usa ls|sort
[ ] M5: task test sem integration/e2e
[ ] M6: json.Marshal erros ignorados
[ ] M7: SSE client sem testes de reconnect
[ ] M9: Partial failure (exit 5) não atingível end-to-end
[ ] H4: Cross-cluster IPs hardcoded
```

### Correções aplicadas em 2026-03-15

**Todos os bloqueantes (B1-B4) e críticos (C1-C4) corrigidos.**
**Todos os altos da Fase 2 (H2, H3, H5, M1, M8) corrigidos.**

Validação:
- `cd api && go build ./...` — PASS
- `cd api && go test ./tests/unit/... -short` — PASS
- `cd pkg && go test ./... -short` — PASS

---

## Conclusão

A implementação da Sprint 2 demonstra maturidade nas decisões de arquitetura: advisory locks transaction-scoped, two-phase activation, SSE streaming com journal, DIP com interfaces reais.

Os problemas encontrados eram de **wiring e observabilidade**, não de design — e foram todos corrigidos:
- Lock unificado: `PoolEnvironmentLocker` agora delega para `lock.WithEnvironmentLock` (timeout detection + panic recovery)
- Circuit breaker wired: `CircuitBreakerSyncClient` conectado no `main.go`
- Correlation-ID real: propagado end-to-end de CLI → API → Argo Workflow → Git trailers
- Erros logados: 3 instâncias de `_ =` substituídas por `slog.Warn`/`slog.Error`
- Dead code removido: `CallbackURL` em `WorkflowSpec` e `SubmitBuildRequest`
- Taskfile completo: CLI tests e lint, monitoring manifests validation, operator /tmp mount

**Itens remanescentes (M2-M9, H4) são technical debt gerenciável, scoped para Sprint 3 backlog.**
