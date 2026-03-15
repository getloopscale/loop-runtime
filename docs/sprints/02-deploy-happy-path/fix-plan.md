# Sprint 2 — Plano de Correção Pós-Revisão

> **Data:** 2026-03-14
> **Origem:** Revisão rigorosa de implementação (11 problemas encontrados)
> **Objetivo:** Sprint 2 100% funcional, zero intervenção manual, todos os testes passando
> **Critério de aceite:** `go test ./...` e `npm test` passam em todos os componentes, zero binários no git

---

## Sequência de Execução

As tarefas estão ordenadas por dependência. Cada grupo pode ser executado em paralelo internamente, mas os grupos devem ser executados em sequência.

---

## Grupo 1 — Testes Quebrados (Bloqueia tudo)

> Sem testes passando, nenhuma outra correção pode ser validada.

### T1.1 — Fix: Integration test não compila (CancelBuild missing)

**Arquivo:** `api/tests/integration/concurrent_builds_test.go:242`
**Problema:** `buildOrchestratorAdapter` não implementa `routes.BuildOrchestratorService` — falta método `CancelBuild`.
**Causa raiz:** O método `CancelBuild` foi adicionado à interface `BuildOrchestratorService` mas o adapter de teste não foi atualizado.

**Correção:**
1. Ler a interface `BuildOrchestratorService` em `api/internal/routes/builds.go`
2. Adicionar método `CancelBuild` ao `buildOrchestratorAdapter` no test file
3. Implementar com a mesma assinatura — pode delegar para o orchestrator real ou retornar erro se não for usado nos testes de concorrência

**Validação:** `cd api && go test ./tests/integration/ -run .` compila sem erros

---

### T1.2 — Fix: TestSourceUpload assertion key errada

**Arquivo:** `api/tests/unit/source_upload_test.go:131`
**Problema:** Assertion busca `store.Bundles["acme:"]` mas o handler guarda como `"acme:my-saas"` (tenant + project name do YAML).
**Causa raiz:** O handler extrai `cfg.Name` ("my-saas") do loopzero.yaml e usa como projectName. O teste não considera isso.

**Correção:**
1. Alterar linha 131: `store.Bundles["acme:"]` → `store.Bundles["acme:my-saas"]`
2. Verificar se os outros subtests do `TestSourceUpload` que passam não têm o mesmo problema latente

**Validação:** `cd api && go test ./tests/unit/ -run TestSourceUpload -v` — todos os subtests passam

---

## Grupo 2 — Concorrência e Locks (Segurança de dados)

> Race conditions reais que podem corromper estado em produção.

### T2.1 — Fix: CancelBuild sem environment lock

**Arquivo:** `api/internal/services/build_orchestrator.go:228-272`
**Problema:** `CancelBuild` faz transições de estado (RUNNING→CANCELLING, QUEUED→CANCELLED) passando `nil` como tx, sem advisory lock.
**Spec:** §4.2 — "Any operation that mutates (tenant, project, environment) state MUST use `withEnvironmentLock`"

**Correção:**
1. Buscar `tenantID`, `projectID`, `environment` do build (já disponíveis via `buildStore.GetByID`)
2. Envolver toda a lógica de cancel em `o.locker.WithLock(ctx, tenantID, projectID, environment, fn)`
3. Dentro do lock: re-fetch build, validar estado, transicionar, emit SSE event
4. Passar `tx` do lock para `buildStore.UpdateStatus` em vez de `nil`

**Validação:**
- Teste existente `TestBuildOrchestrator_CancelBuild_RunningBuild` continua passando
- Novo teste: dois `CancelBuild` concorrentes no mesmo build → um sucede, outro recebe erro (ou idempotente)

---

### T2.2 — Fix: OnBuildCompleted transições iniciais fora do lock

**Arquivo:** `api/internal/services/build_orchestrator.go:304-330`
**Problema:** Transições QUEUED→RUNNING (EC-N guard), →FAILED, →SUCCEEDED acontecem FORA do advisory lock. O lock só é adquirido na linha 343 para `activateRelease`.
**Risco:** Dois callbacks simultâneos do Argo Workflows podem processar o mesmo build.

**Correção:**
1. Mover TODA a lógica de `OnBuildCompleted` (linhas 282-346) para dentro de um `WithLock`
2. O lock key deve ser derivado do build: buscar `tenantID`, `projectID`, `environment` via `buildStore.GetByID`
3. Dentro do lock: fetch build → validate state → transition → create release → activate → emit SSE
4. O `activateRelease` (que já estava no lock) fica naturalmente dentro do mesmo escopo

**Validação:**
- Todos os testes `TestBuildOrchestrator_OnBuildCompleted_*` continuam passando
- Novo teste: dois callbacks simultâneos para o mesmo build → apenas um processa

---

## Grupo 3 — Infraestrutura de Banco (Resiliência)

### T3.1 — Fix: PostgreSQL pool sem configuração

**Arquivo:** `api/internal/db/pool.go`
**Problema:** Pool criado com `pgxpool.New(ctx, databaseURL)` sem configurar limites. Spec §7.8 exige configuração explícita.

**Correção:**
1. Usar `pgxpool.ParseConfig(databaseURL)` em vez de `pgxpool.New`
2. Configurar:
   ```go
   cfg.MaxConns = 20
   cfg.MinConns = 5
   cfg.MaxConnIdleTime = 30 * time.Second
   cfg.MaxConnLifetime = 1 * time.Hour
   cfg.HealthCheckPeriod = 30 * time.Second
   ```
3. Adicionar `statement_timeout` via connection string parameter ou `AfterConnect` hook:
   ```go
   cfg.ConnConfig.RuntimeParams["statement_timeout"] = "30000"
   ```
4. Adicionar `connect_timeout` via connection string ou config:
   ```go
   cfg.ConnConfig.ConnectTimeout = 5 * time.Second
   ```

**Validação:**
- API inicia normalmente com pool configurado
- Teste: query que excede 30s é cancelada automaticamente

---

## Grupo 4 — Higiene do Repositório

### T4.1 — Fix: Remover binários compilados do git

**Arquivos:** `api/main` (25MB), `operators/main` (43MB), `cmd` (16MB), `scripts` (8.3MB)
**Problema:** Binários ELF x86-64 commitados no repositório. Inflam clone, são platform-specific, não são source code.

**Correção:**
1. Adicionar ao `.gitignore`:
   ```
   # Compiled binaries
   api/main
   operators/main
   /cmd
   /scripts
   ```
2. Remover do tracking: `git rm --cached api/main operators/main cmd scripts`
3. Commit com mensagem explicativa

**Validação:** `git status` não mostra binários como tracked. `git ls-files | grep -E '^(api/main|operators/main|cmd$|scripts$)'` retorna vazio.

---

### T4.2 — Fix: Eliminar state machines duplicadas

**Arquivos:** `operators/internal/statemachine/` (cópia) vs `pkg/statemachine/` (canônico)
**Problema:** 7 arquivos duplicados. `pkg/` tem `IsBuildTerminal()` que `operators/` não tem. Divergência vai crescer.

**Correção:**
1. Verificar que `operators/` já importa `pkg/statemachine` para o tenant (via go.work)
2. Em `operators/internal/controller/`, alterar todos os imports de `operators/internal/statemachine` para `github.com/loopzero/loopzero/pkg/statemachine`
3. Atualizar `operators/internal/statemachine/machine.go` references em testes
4. Deletar `operators/internal/statemachine/` inteiro
5. Ajustar qualquer tipo ou constante que difira (ex: `statemachine.State` vs `statemachine.State` — mesmo tipo se o package path muda)

**Validação:** `cd operators && go build ./... && go test ./...` — compila e testes passam com import único

---

### T4.3 — Fix: Remover fake.go dos operators

**Arquivos:** `operators/internal/gitea/fake.go`, `operators/internal/argocd/fake.go`
**Problema:** Test doubles em packages de produção. Viola "NUNCA usar mocks/fakes/stubs".

**Correção:**
1. Os fakes são usados em `operators/internal/controller/suite_test.go` para testes envtest
2. Mover `FakeProvisioner` para dentro do próprio `suite_test.go` ou para um arquivo `_test.go` no mesmo package (ex: `gitea_fake_test.go` em `controller/`)
3. Deletar `operators/internal/gitea/fake.go` e `operators/internal/argocd/fake.go`
4. Atualizar imports em `suite_test.go`

**Nota:** Para os testes envtest do operator, fakes de Gitea/ArgoCD são aceitáveis porque estamos testando o reconcile loop do controller, não o provisionamento externo. O que é inaceitável é que existam em packages de produção. Movê-los para `_test.go` resolve isso.

**Validação:** `cd operators && go build ./...` compila sem fake.go. `go test ./...` passa com fakes em `_test.go`.

---

## Grupo 5 — Integração ArgoCD (Robustez)

### T5.1 — Fix: Health gate não integrado no deploy flow

**Arquivo:** `api/internal/services/build_orchestrator.go` (OnBuildCompleted)
**Problema:** Circuit breaker existe (`CircuitBreakerSyncClient`) mas não faz probe proativo. Deploys tentam sync por 2min antes de falhar.

**Correção:**
1. Antes de chamar `TriggerSync()` em `activateRelease`, verificar `circuitBreaker.State()`
2. Se `CircuitOpen`: retornar erro `PLATFORM_DEGRADED` imediatamente, manter release em `READY` (não `SYNC_FAILED`)
3. O `HealthProber` existente já faz probes periódicos — conectar seu resultado ao circuit breaker state

**Validação:**
- Teste existente `TestBuildOrchestrator_OnBuildCompleted_ProbeFails` continua passando
- Novo teste: circuit breaker OPEN → `activateRelease` retorna 503 em < 100ms (fast fail)

---

## Grupo 6 — Cobertura de Testes (Qualidade)

### T6.1 — Fix: Testes faltando para adapters CLI

**Arquivos sem teste:**
- `cli/src/adapters/git.ts` → criar `cli/tests/unit/adapters/git.test.ts`
- `cli/src/adapters/source-bundle.ts` → criar `cli/tests/unit/adapters/source-bundle.test.ts`
- `cli/src/adapters/loopzero-yaml.ts` → criar `cli/tests/unit/adapters/loopzero-yaml.test.ts`

**Escopo mínimo por adapter:**
- `git.ts`: `hasGitRepo` retorna true/false, `getHeadCommit` retorna hash, `hasUncommittedChanges` detecta mudanças
- `source-bundle.ts`: `createBundle` gera arquivo, `cleanupFile` remove arquivo
- `loopzero-yaml.ts`: `findLoopZeroYaml` encontra arquivo, `parseLoopZeroYaml` valida schema, rejeita YAML inválido

---

### T6.2 — Fix: Testes faltando para middlewares/handlers API

**Arquivos sem teste:**
- `api/internal/middleware/correlation.go` → adicionar teste em `api/tests/unit/`
- `api/internal/middleware/logging.go` → adicionar teste em `api/tests/unit/`
- `api/internal/sse/stream_handler.go` → adicionar teste em `api/tests/unit/`
- `api/internal/lock/environment_lock.go` → adicionar teste em `api/tests/unit/`

**Nota:** `lock/environment_lock.go` já tem teste em `api/internal/lock/environment_lock_test.go` (passa). Os outros 3 precisam de testes novos.

---

### T6.3 — Melhoria: Typed errors nas camadas DB/service

**Escopo:** 60+ `fmt.Errorf` em `api/internal/db/` e `api/internal/services/`
**Prioridade:** Baixa — as routes já usam `LoopZeroError`. Os `fmt.Errorf` são wrapping operacional (erros de query, connection), não erros de domínio.

**Correção gradual:**
1. Erros de "not found" → usar `LoopZeroError` com código `NOT_FOUND`
2. Erros de constraint violation → usar `LoopZeroError` com código `CONFLICT`
3. Erros de connection/query → manter `fmt.Errorf` wrapping (são erros de infraestrutura, não de domínio)

---

## Checklist de Validação Final

Após todas as correções, executar na sequência:

```bash
# 1. Compilação
cd api && go build ./...
cd operators && go build ./...
cd cli && npx tsc --noEmit

# 2. Testes unitários
cd pkg && go test ./...
cd api && go test ./tests/unit/ -v
cd operators && go test ./... -v
cd cli && npm test

# 3. Testes de integração
cd api && go test ./tests/integration/ -v

# 4. Lint
cd cli && npx eslint src/

# 5. Verificações de regras
# Zero binários tracked
git ls-files | grep -E '^(api/main|operators/main|cmd$|scripts$)' | wc -l  # deve ser 0

# Zero fakes em packages de produção
find operators/internal -name 'fake.go' -not -name '*_test.go' | wc -l  # deve ser 0

# Zero state machines duplicadas
ls operators/internal/statemachine/ 2>/dev/null | wc -l  # deve ser 0

# Zero termos internos no CLI
grep -rn 'ArgoCD\|argocd\|GitOps\|gitops-push\|CRD\|tenant_id\|advisory lock\|pg_advisory\|Gitea\|MinIO\|kubebuilder\|envtest\|Kaniko' cli/src/ --include='*.ts' | grep -v test | wc -l  # deve ser 0
```

**Exit gate Sprint 2:** Todos os comandos acima retornam sucesso e contagens zero.

---

## Resumo de Esforço

| Grupo | Tarefas | Estimativa | Risco |
|-------|---------|-----------|-------|
| G1 — Testes quebrados | T1.1, T1.2 | Baixo | Correções pontuais |
| G2 — Locks | T2.1, T2.2 | Médio | Refactor do orchestrator, precisa de cuidado com testes |
| G3 — Pool config | T3.1 | Baixo | Configuração direta |
| G4 — Higiene repo | T4.1, T4.2, T4.3 | Médio | T4.2 (state machines) pode quebrar imports |
| G5 — Health gate | T5.1 | Médio | Integração circuit breaker ↔ deploy flow |
| G6 — Cobertura | T6.1, T6.2, T6.3 | Médio | Volume de testes novos |

**Ordem recomendada de sessão:** G1 → G4.1 → G2 → G3 → G4.2 → G4.3 → G5 → G6
