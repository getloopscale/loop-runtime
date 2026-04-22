# Test Quality Improvement Plan

> Plano executável baseado em MEET-2026-04-22-056.
> Objetivo: eliminar ~5,900 linhas de código crítico com ZERO testes, adicionar infraestrutura de cobertura, e preparar mutation testing.
> 4 fases, 13 mandatory actions, ~80 novos testes.

---

## Visão Geral

| Fase | Foco | Semana | Ações | Testes Novos |
|------|------|--------|-------|-------------|
| **P0** | Stop the bleeding — testes para deploy pipeline core | 1-2 | M-01..M-08 | ~50 |
| **P1** | Coverage infrastructure — visibilidade | 3 | M-09..M-10 | 0 |
| **P2** | Fill remaining gaps + E2E em CI | 4-6 | M-11 + extras | ~30 |
| **P3** | Mutation testing (condicional) | 7+ | M-12..M-13 | 0 |

**SRE VETO ativo:** P3 bloqueada até P0 completa.

---

## Fase P0 — Stop the Bleeding (Semana 1-2)

> Preencher os gaps de teste mais críticos. Zero tooling novo — apenas testes.

### Task P0.1: Testes para autodeploy.go + appclient.go

**Finding:** ZERO testes para o PRIMARY deploy path (git push → auto-deploy).
**Files:** `api/internal/github/autodeploy.go` (336 LOC), `api/internal/github/appclient.go` (236 LOC)
**Tier:** T1

**Criar:** `api/tests/unit/autodeploy_test.go`

| # | Nome do Teste | O que testa | Arrange | Act | Assert |
|---|--------------|------------|---------|-----|--------|
| 1 | `TestExtractLoopZeroYaml_ValidTarball` | Parsing de loopzero.yaml de dentro de um tarball gzip | Criar tarball com loopzero.yaml válido | `extractLoopZeroYaml(reader)` | Retorna project name e apps corretos |
| 2 | `TestExtractLoopZeroYaml_MissingFile` | Tarball sem loopzero.yaml | Criar tarball sem loopzero.yaml | `extractLoopZeroYaml(reader)` | Erro "loopzero.yaml not found" |
| 3 | `TestExtractLoopZeroYaml_MalformedYaml` | YAML inválido dentro do tarball | Tarball com loopzero.yaml contendo `{{{` | `extractLoopZeroYaml(reader)` | Erro de YAML unmarshal |
| 4 | `TestExtractLoopZeroYaml_NestedPath` | loopzero.yaml em subdir (GitHub prefix dir) | Tarball com `reponame-sha/loopzero.yaml` | `extractLoopZeroYaml(reader)` | Encontra e retorna corretamente |
| 5 | `TestHandlePush_HappyPath` | Fluxo completo: download → parse → bundle → upload → submit | Fakes para AppClient, SourceStore, ObjectStore, BuildSubmitter | `HandlePush(ctx, ...)` | BuildSubmitter.SubmitBuildFromWebhook chamado com campos corretos |
| 6 | `TestHandlePush_DownloadFailure` | GitHub API retorna erro | AppClient.DownloadSource retorna erro | `HandlePush(ctx, ...)` | Erro propagado, build não submetido |
| 7 | `TestHandlePush_NoLoopZeroYaml` | Source sem loopzero.yaml | AppClient retorna tarball válido sem loopzero.yaml | `HandlePush(ctx, ...)` | Erro "loopzero.yaml not found", build não submetido |
| 8 | `TestHandlePush_S3UploadFailure` | Upload para S3 falha | ObjectStore.Upload retorna erro | `HandlePush(ctx, ...)` | Erro propagado |
| 9 | `TestHandlePush_BuildSubmitFailure` | Submissão falha | BuildSubmitter retorna erro | `HandlePush(ctx, ...)` | Erro propagado |
| 10 | `TestHandlePush_CancelQueuedNonFatal` | Cancel de builds antigos falha (non-fatal) | BuildCanceller retorna erro | `HandlePush(ctx, ...)` | Erro logado mas HandlePush retorna nil (build continua) |

**Criar:** `api/tests/unit/appclient_test.go`

| # | Nome do Teste | O que testa |
|---|--------------|------------|
| 11 | `TestNewAppClient_ValidCredentials` | Constructor com PEM válido retorna client |
| 12 | `TestNewAppClient_InvalidPEM` | PEM inválido retorna erro |
| 13 | `TestGetInstallationToken_CachesToken` | Primeira chamada faz HTTP, segunda retorna cache |
| 14 | `TestGetInstallationToken_RefreshesExpired` | Token expirado faz nova chamada HTTP |
| 15 | `TestDownloadSource_Returns200` | HTTP 200 retorna reader com body |
| 16 | `TestDownloadSource_Returns404` | HTTP 404 retorna erro |
| 17 | `TestSetCommitStatus_Success` | HTTP 201 retorna nil |

**Dependências para testes:**
- Interfaces já existem (`SourcePusher`, `ObjectUploader`, `BuildSubmitter`, `BuildCanceller`)
- AppClient usa HTTP — testar com `httptest.NewServer`
- `extractLoopZeroYaml` aceita `io.Reader` — fácil de testar com bytes in-memory
- `tarballToGitBundle` usa `gitclient.GitRunner` — precisa de fake ou git real

**Critérios de aceite:**
- [ ] 17 testes escritos e passando
- [ ] Nenhum `time.Sleep` nos novos testes
- [ ] Testes usam `httptest.NewServer` para HTTP mocking (não mocks de interface)
- [ ] `go test ./api/tests/unit/... -run TestExtract -v` passa
- [ ] `go test ./api/tests/unit/... -run TestHandlePush -v` passa

**DoD:**
- [ ] `go test ./api/...` passa
- [ ] `go build ./api/...` compila
- [ ] Zero `time.Sleep` nos novos testes

---

### Task P0.2: Testes para build_reconcile_*.go (5 arquivos)

**Finding:** ZERO testes para o background build lifecycle processor (830 LOC).
**Files:** `api/internal/services/build_reconcile_{loop,dispatch,single,complete,retry}.go`
**Tier:** T1

**Criar:** `api/tests/unit/build_reconcile_loop_test.go`

| # | Nome do Teste | O que testa |
|---|--------------|------------|
| 1 | `TestReconcileLoop_ProcessesActiveBuilds` | reconcile() encontra builds ativos e chama reconcileBuild para cada |
| 2 | `TestReconcileLoop_SkipsBuildAtTenantLimit` | Per-tenant backpressure: tenant com MaxBuildsPerTenant builds é skipped |
| 3 | `TestReconcileLoop_SemaphoreBackpressure` | Semaphore cheio: builds extras são deferridos para o próximo tick |
| 4 | `TestReconcileBuild_ReleasingState_RetriesActivation` | Build em RELEASING → chama retryReleaseActivation |
| 5 | `TestReconcileBuild_NoWorkflowID_GracePeriod` | Build sem WorkflowID e idade < 60s → skip (grace period) |
| 6 | `TestReconcileBuild_NoWorkflowID_Expired` | Build sem WorkflowID e idade > 60s → FAILED |
| 7 | `TestReconcileBuild_WorkflowSucceeded` | Workflow status Succeeded → chama onWorkflowSucceeded |
| 8 | `TestReconcileBuild_WorkflowFailed` | Workflow status Failed com FinishedAt → chama failBuild |
| 9 | `TestOnWorkflowSucceeded_BuildsManifestsAndActivates` | Happy path: GetBuildApps → Render → OnBuildCompleted |
| 10 | `TestOnWorkflowSucceeded_GetBuildAppsFails` | GetBuildApps retorna erro → build FAILED |
| 11 | `TestOnWorkflowSucceeded_ManifestRenderFails` | Render retorna erro → build FAILED (F-010 fatal) |
| 12 | `TestOnWorkflowSucceeded_CompletionRetry` | OnBuildCompleted falha na primeira tentativa → retry (max 3) |
| 13 | `TestRetryReleaseActivation_MaxAttempts` | Após 3 tentativas (BRC-003) → build FAILED |
| 14 | `TestRetryReleaseActivation_ReleaseAlreadyActive` | Release já ACTIVE → completeBuild (não recria) |
| 15 | `TestFailBuild_TransitionsToFailed` | failBuild transiciona estado e emite SSE event |
| 16 | `TestEnsureApplicationCRs_NonFatal` | AppCR creation falha → logado, não bloqueia deploy |

**Dependências:**
- Interfaces: `BuildCompletionHandler`, `CellLister`, `CustomDomainQuerier`, `ApplicationCREnsurer`
- Criar fakes simples que implementam essas interfaces
- `buildStore` já tem interface — usar fake
- `workflow` (WorkflowGetter) — usar fake que retorna status configurável

**Critérios de aceite:**
- [ ] 16 testes escritos e passando
- [ ] Cada teste verifica UM comportamento (sem "e" no nome)
- [ ] Fakes thread-safe (sync.Mutex) para testes de concorrência

**DoD:**
- [ ] `go test ./api/tests/unit/... -run TestReconcile -v` passa
- [ ] `go test ./api/...` passa

---

### Task P0.3: Testes para release_activator.go + sync_callback_activator.go

**Finding:** Thin coverage para o código que faz deploy "go live" (462 LOC). Bug histórico documentado.
**Files:** `api/internal/services/release_activator.go` (297 LOC), `api/internal/services/sync_callback_activator.go` (167 LOC)
**Tier:** T1

**Criar:** `api/tests/unit/release_activator_test.go`

| # | Nome do Teste | O que testa |
|---|--------------|------------|
| 1 | `TestActivateReleaseKernel_HappyPath` | 7-step sequence: re-fetch → get previous → transition → metric → SSE → SetActive → supersede → event → callback |
| 2 | `TestActivateReleaseKernel_SupersedesPreviousRelease` | Previous ACTIVE release → superseded. Bug regression: `project_rollback_supersede_bug` |
| 3 | `TestActivateReleaseKernel_NoPreviousRelease` | First deploy (no previous ACTIVE) → skip supersede |
| 4 | `TestActivateReleaseKernel_AlreadyActive` | Release already ACTIVE → idempotent (no error) |
| 5 | `TestActivateReleaseKernel_StateTransitionFails` | PENDING→ACTIVE fails (invalid state) → error |
| 6 | `TestRetryRelease_RePushesManifests` | Release with GIT_MIRROR_FAILED → re-pushes gitops |
| 7 | `TestRetryRelease_NotGitMirrorFailed` | Release NOT GIT_MIRROR_FAILED → error (wrong state) |
| 8 | `TestDeriveDeployID_Format` | rel-abc12345 → d-abc12345 (8 hex chars) |

**Criar ou estender:** `api/tests/unit/sync_callback_activator_test.go` (já existe parcialmente)

| # | Nome do Teste | O que testa |
|---|--------------|------------|
| 9 | `TestOnSyncCompleted_ActivatesPendingRelease` | ArgoCD callback → encontra release PENDING → ativa |
| 10 | `TestOnSyncCompleted_RevisionMismatch` | GitCommit do release não match revision → skip |
| 11 | `TestOnSyncCompleted_EmptyGitCommit` | Release sem GitCommit (BUG-03 defense) → skip |
| 12 | `TestParseArgoAppName_ValidFormat` | "tenant-production" → (tenant, production, true) |
| 13 | `TestParseArgoAppName_TenantWithHyphens` | "my-org-staging" → (my-org, staging, true) — ARCH-002 |
| 14 | `TestParseArgoAppName_Invalid` | "notenant" → ("", "", false) |

**Critérios de aceite:**
- [ ] 14 testes escritos e passando
- [ ] Teste #2 é regression test para o bug documentado

**DoD:**
- [ ] `go test ./api/tests/unit/... -run TestActivateRelease -v` passa
- [ ] `go test ./api/...` passa

---

### Task P0.4: Router wiring exhaustive test

**Finding:** 44 routes registradas, apenas ~5 testadas. "Implemented but not wired" é o padrão sistêmico #1.
**File:** `api/internal/server/router.go` (325 LOC, 44 routes)
**Tier:** T1

**Criar:** `api/tests/unit/router_wiring_test.go`

| # | Nome do Teste | O que testa |
|---|--------------|------------|
| 1 | `TestRouter_AllRoutesRegistered` | Instancia NewRouter com handlers no-op, faz GET/POST/DELETE para cada uma das 44 rotas, asserta que nenhuma retorna 404 ou 405 |

**Implementação:**
```go
// Para cada rota documentada, enviar request com método correto.
// Esperar: qualquer status EXCETO 404 e 405.
// Isso garante que a rota está registrada (wired), não que funciona.
routes := []struct{method, path string}{
    {"GET", "/api/v1/health"},
    {"POST", "/api/v1/builds"},
    // ... todas as 44 rotas
}
for _, r := range routes {
    resp := httptest.NewRecorder()
    req := httptest.NewRequest(r.method, r.path, nil)
    router.ServeHTTP(resp, req)
    assert(resp.Code != 404 && resp.Code != 405, "route %s %s not wired", r.method, r.path)
}
```

**Critérios de aceite:**
- [ ] Todas as 44 rotas verificadas (GET, POST, DELETE, PATCH, PUT conforme documentado)
- [ ] Teste falha se uma rota for removida sem atualizar o teste (guard contra "unwired")
- [ ] Handlers podem retornar qualquer status (401, 400, 500) — o teste só verifica que a rota EXISTE

**DoD:**
- [ ] `go test ./api/tests/unit/... -run TestRouter_AllRoutesRegistered -v` passa

---

### Task P0.5: Testes para adapters.go

**Finding:** ZERO testes para 412 LOC de field mapping + default values.
**File:** `api/internal/adapters/adapters.go`
**Tier:** T1

**Criar:** `api/internal/adapters/adapters_test.go`

| # | Nome do Teste | O que testa |
|---|--------------|------------|
| 1 | `TestBuildOrchestratorAdapter_SubmitBuild_DefaultType` | Input sem type → default "server" |
| 2 | `TestBuildOrchestratorAdapter_SubmitBuild_DefaultPort` | Server type com port 0 → default 8080 |
| 3 | `TestBuildOrchestratorAdapter_SubmitBuild_ExplicitPort` | Worker type com port 3000 → mantém 3000 |
| 4 | `TestBuildOrchestratorAdapter_GetBuildStatus_TimestampFormat` | Timestamps em RFC3339 UTC |
| 5 | `TestBuildOrchestratorAdapter_GetBuildStatus_NilTimestamps` | Campos opcionais nil → empty string no response |
| 6 | `TestPgxRowAdapter_Scan_MapsErrNoRows` | pgx.ErrNoRows → db.ErrNoRows |
| 7 | `TestPoolEnvironmentLocker_WithLock_AcquiresAdvisoryLock` | Chama lock.WithEnvironmentLock corretamente |

**Critérios de aceite:**
- [ ] 7 testes escritos e passando
- [ ] Default values testados explicitamente (não assumidos)

**DoD:**
- [ ] `go test ./api/internal/adapters/... -v` passa

---

### Task P0.6: Fix 32 time.Sleep calls em test files

**Finding:** 32 chamadas `time.Sleep` em testes da API — bomba de flakiness.
**Tier:** T0

**O que fazer:**
1. `grep -rn "time.Sleep" api/tests/ api/internal/ --include="*_test.go"` — listar todas
2. Para cada `time.Sleep`:
   - Se espera goroutine: substituir por `sync.WaitGroup` ou channel
   - Se espera eventual consistency: substituir por poll loop com timeout
   - Se espera tempo real (rate limiter): manter mas documentar motivo

**Critérios de aceite:**
- [ ] `grep -rn "time.Sleep" api/ --include="*_test.go" | wc -l` retorna 0 (ou apenas os documentados)
- [ ] Todos os testes continuam passando

**DoD:**
- [ ] `go test ./api/...` passa
- [ ] Zero `time.Sleep` não-documentado em testes

---

### Task P0.7: Adicionar gauge + alert para reconcile loop

**Finding:** Reconcile loop pode morrer silenciosamente — nenhum alert existe.
**Tier:** T1

**O que fazer:**
1. Em `api/internal/metrics/metrics.go`: adicionar `ReconcileLoopLastRunTimestamp` (Gauge)
2. Em `build_reconcile_dispatch.go`: no final de cada `reconcile()`, setar `ReconcileLoopLastRunTimestamp.SetToCurrentTime()`
3. Em `infra/helm/monitoring/manifests/`: adicionar PrometheusRule:
   ```yaml
   - alert: LoopZeroReconcileLoopStalled
     expr: time() - loopzero_reconcile_loop_last_run_timestamp > 300
     for: 2m
     labels:
       severity: critical
     annotations:
       summary: "Build reconcile loop has not run in 5+ minutes"
   ```

**Critérios de aceite:**
- [ ] Gauge `loopzero_reconcile_loop_last_run_timestamp` registrado
- [ ] Gauge atualizado a cada ciclo de reconcile
- [ ] Alert fires após 5 min sem atualização
- [ ] `task validate:manifests` passa

**DoD:**
- [ ] `go build ./api/...` compila
- [ ] Manifest YAML válido

---

### Task P0.8: Fix liveness probe para checar background loops

**Finding:** `/api/v1/health` retorna 200 incondicional — não detecta loops mortos.
**File:** `api/internal/routes/health.go`
**Tier:** T1

**O que fazer:**
1. Adicionar check no health endpoint que verifica `ReconcileLoopLastRunTimestamp`
2. Se timestamp > 5 min atrás → retornar `{"status": "degraded", "reconcile_loop": "stalled"}`
3. Manter HTTP 200 (K8s liveness) mas incluir campo `degraded: true` no body
4. Readiness probe (`/api/v1/health/deep`) já é adequada — não alterar

**Alternativa mais segura:** Não mudar liveness (pode causar restart loop). Em vez disso, adicionar endpoint `/api/v1/health/loops` que reporta status de cada background loop. Grafana/alerting pode monitorar.

**Critérios de aceite:**
- [ ] Endpoint reporta saúde dos background loops
- [ ] Reconcile loop stalled é detectável via API

**DoD:**
- [ ] `go build ./api/...` compila
- [ ] `go test ./api/...` passa

---

## Fase P1 — Coverage Infrastructure (Semana 3)

### Task P1.1: Adicionar `task test:coverage` ao Taskfile

**Tier:** T0

**O que fazer:**
```yaml
test:coverage:
  desc: Run all tests with coverage, generate HTML reports
  cmds:
    - cd pkg && go test ./... -coverprofile=coverage.out && go tool cover -html=coverage.out -o coverage.html
    - cd api && go test ./... -coverprofile=coverage.out && go tool cover -html=coverage.out -o coverage.html
    - cd operators && go test ./... -coverprofile=coverage.out -short && go tool cover -html=coverage.out -o coverage.html
    - cd cli && npm test -- --coverage
    - echo "Reports: pkg/coverage.html, api/coverage.html, operators/coverage.html, cli/coverage/"
```

**Critérios de aceite:**
- [ ] `task test:coverage` gera reports HTML para todos os componentes
- [ ] Reports abrem no browser e mostram cobertura por linha

**DoD:**
- [ ] Taskfile atualizado
- [ ] `task test:coverage` roda sem erros

---

### Task P1.2: Coverage floor enforcement no CI

**Tier:** T1

**O que fazer:**
1. No `.github/workflows/ci.yml`, adicionar step após testes:
   ```yaml
   - name: Check coverage floor
     run: |
       go test ./api/... -coverprofile=api-coverage.out
       COVERAGE=$(go tool cover -func=api-coverage.out | grep total | awk '{print $3}' | tr -d '%')
       echo "API coverage: ${COVERAGE}%"
       # Floor: fail if coverage DROPS below baseline (ratchet)
       if (( $(echo "$COVERAGE < 25" | bc -l) )); then
         echo "FAIL: Coverage ${COVERAGE}% below floor 25%"
         exit 1
       fi
   ```
2. Floors iniciais (ratchet — só sobe, nunca desce):

| Package | Floor |
|---------|-------|
| `pkg/` | 65% |
| `api/` (global) | 25% |
| `operators/` | 55% |
| `cli/` | 70% |

**Critérios de aceite:**
- [ ] CI falha se cobertura cair abaixo do floor
- [ ] Floors documentados no CI como comentários

**DoD:**
- [ ] `.github/workflows/ci.yml` atualizado
- [ ] CI passa com floors atuais

---

## Fase P2 — Fill Remaining Gaps + E2E em CI (Semana 4-6)

### Task P2.1: Testes para reapers + GC

**Files:** `build_reaper.go` (313 LOC), `release_reaper.go` (292 LOC), `build_gc.go` (101 LOC)

| # | Teste | O que verifica |
|---|-------|---------------|
| 1 | `TestBuildReaper_CleansOldBuilds` | Builds >30 dias são limpos |
| 2 | `TestBuildReaper_KeepsRecentBuilds` | Builds <30 dias mantidos |
| 3 | `TestBuildReaper_KeepsLastN` | Últimos 10 builds mantidos independente de idade |
| 4 | `TestReleaseReaper_SupersedesOldReleases` | Releases superseded >30 dias são limpos |
| 5 | `TestBuildGC_DeletesOrphanedWorkflows` | Workflows sem build correspondente são deletados |

**DoD:** `go test ./api/tests/unit/... -run TestReaper -v` passa

---

### Task P2.2: Testes para circuit breakers

**Files:** `pg_circuit_breaker.go` (223 LOC), `workflow_circuit_breaker.go` (144 LOC)

| # | Teste | O que verifica |
|---|-------|---------------|
| 1 | `TestPGCircuitBreaker_OpensAfterThreshold` | N falhas consecutivas → estado OPEN |
| 2 | `TestPGCircuitBreaker_ClosesOnSuccess` | Após retry interval → HALF_OPEN → sucesso → CLOSED |
| 3 | `TestPGCircuitBreaker_RejectsInOpenState` | Requests rejeitados quando OPEN |
| 4 | `TestWorkflowCB_SamePattern` | Mesmo padrão para workflow CB |

**DoD:** `go test ./api/tests/unit/... -run TestCircuitBreaker -v` passa

---

### Task P2.3: Testes para domain provisioner

**Files:** `domain_provisioner.go` (485 LOC), `domain_health_checker.go` (340 LOC)

| # | Teste | O que verifica |
|---|-------|---------------|
| 1 | `TestDomainProvisioner_ProvisionsCFZone` | CF Zone criada com DNS scan |
| 2 | `TestDomainProvisioner_HandlesDNSConflict` | Conflicting A record → deletado antes de criar CNAME |
| 3 | `TestDomainHealthChecker_DetectsUnhealthy` | CNAME check falha → domain marcado unhealthy |
| 4 | `TestDomainHealthChecker_SkipsRecentlyChecked` | Domain checado <1h atrás → skip |

**DoD:** `go test ./api/tests/unit/... -run TestDomain -v` passa

---

### Task P2.4: E2E tests no CI (merge to develop only)

**Tier:** T2

**O que fazer:**
1. Adicionar job `e2e` no `.github/workflows/ci.yml`
2. Trigger: push to `develop` (não PRs)
3. Usar k3d clusters no GitHub Actions runner
4. Timeout: 20 min
5. `continue-on-error: false` — bloqueia merge para main

```yaml
e2e:
  name: E2E Tests
  runs-on: ubuntu-latest
  if: github.ref == 'refs/heads/develop'
  timeout-minutes: 20
  steps:
    - uses: actions/checkout@v4
    - uses: AbsaOSS/k3d-action@v2
      with:
        cluster-name: "loopzero-e2e"
    - run: task test:e2e
```

**Critérios de aceite:**
- [ ] E2E roda em merge para develop
- [ ] NÃO roda em PRs (economia de CI)
- [ ] Falha bloqueia merge develop→main

**DoD:**
- [ ] `.github/workflows/ci.yml` atualizado
- [ ] E2E passa no GitHub Actions

---

## Fase P3 — Mutation Testing (Semana 7+, CONDICIONAL)

> **BLOQUEADA até P0 completa.** SRE VETO ativo.

### Task P3.1: Instalar gremlins + StrykerJS

**Tier:** T1

**O que fazer:**
1. `go install github.com/go-gremlins/gremlins/cmd/gremlins@latest`
2. `cd cli && npm install --save-dev @stryker-mutator/core @stryker-mutator/jest-runner`
3. Criar `cli/stryker.conf.json` com scope limitado a `src/output/`

**Taskfile:**
```yaml
test:mutate:
  desc: "Run mutation testing on specific package (e.g., task test:mutate -- pkg/statemachine)"
  cmds:
    - gremlins unleash {{.CLI_ARGS}}

test:mutate:cli:
  desc: "Run mutation testing on CLI output module"
  dir: cli
  cmds:
    - npx stryker run --incremental
```

**Critérios de aceite:**
- [ ] `task test:mutate -- pkg/statemachine` roda e produz relatório
- [ ] `task test:mutate:cli` roda em cli/src/output/
- [ ] Nenhum dos dois é CI gate (nunca bloqueia PR)

---

### Task P3.2: Nightly CI job para mutation report

**O que fazer:**
```yaml
mutation-report:
  name: Mutation Testing Report
  runs-on: ubuntu-latest
  schedule:
    - cron: '0 3 * * *'  # 3 AM UTC daily
  continue-on-error: true  # NEVER blocks
  steps:
    - run: gremlins unleash ./pkg/statemachine/ --output=json > mutation-report.json
    - uses: actions/upload-artifact@v4
      with:
        name: mutation-report
        path: mutation-report.json
```

**Critérios de aceite:**
- [ ] Job roda diariamente às 3 AM
- [ ] `continue-on-error: true` — nunca bloqueia
- [ ] Report como artifact no GitHub Actions

---

### Task P3.3: Documentação mutation testing

**Criar:** `docs/testing/mutation-testing.md`

Conteúdo:
- O que é mutation testing (2 parágrafos)
- Quando usar (critical paths only)
- Como rodar (`task test:mutate -- <package>`)
- Como interpretar resultados
- Thresholds: statemachine 90%, lock 80%, services 70%

**DoD:**
- [ ] Documento existe e é útil em 5 min de leitura

---

## Ordem de Execução

```
P0.6 (fix time.Sleep) ──┐
P0.7 (reconcile gauge)──┤
P0.8 (liveness probe) ──┤── Semana 1 (infra + quick fixes)
P0.4 (router wiring)  ──┘

P0.1 (autodeploy)     ──┐
P0.2 (reconcile loop) ──┤── Semana 2 (big test writing)
P0.3 (release activator)┤
P0.5 (adapters)        ──┘

P1.1 (task coverage)   ──┐
P1.2 (CI floor)         ──┘── Semana 3

P2.1..P2.3 (gaps)      ──┐
P2.4 (E2E in CI)        ──┘── Semana 4-6

P3.1..P3.3 (mutation)  ──── Semana 7+ (APÓS P0 COMPLETA)
```

**Dependências:**
- P0.6 não bloqueia nada mas deve ser feito cedo (evita flakiness nos novos testes)
- P0.7 e P0.8 são infra — fazer primeiro
- P0.1-P0.5 são independentes entre si (paralelizáveis)
- P1 depende de P0 (cobertura só é útil após gaps preenchidos)
- P2 depende de P0 (build sobre a base)
- P3 tem SRE VETO até P0 completa

---

## Gate de Conclusão

### P0 Done (SRE veto lift):
```bash
# ~50 novos testes passando
go test ./api/... -count=1 | grep -c "ok"
# Zero time.Sleep não-documentado
grep -rn "time.Sleep" api/ --include="*_test.go" | grep -v "// documented"
# Reconcile gauge existe
grep "loopzero_reconcile_loop_last_run_timestamp" api/internal/metrics/metrics.go
# Alert existe
grep "LoopZeroReconcileLoopStalled" infra/helm/monitoring/manifests/
```

### P1 Done:
```bash
task test:coverage  # Gera reports
# CI tem floors enforced
grep "coverage.*floor" .github/workflows/ci.yml
```

### P3 Done:
```bash
task test:mutate -- pkg/statemachine  # Score >90%
task test:mutate:cli                  # Score >70%
cat docs/testing/mutation-testing.md  # Exists
```
