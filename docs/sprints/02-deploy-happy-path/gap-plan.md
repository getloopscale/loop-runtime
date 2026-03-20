# Sprint 2 — Plano de Correção dos Gaps da Revisão de Implementação

> **Data:** 2026-03-14
> **Baseline:** Sprint 2 deploy happy-path implementado. 19 gaps identificados na revisão.
> **Objetivo:** Corrigir todos os problemas antes do merge em main.

---

## Resumo dos Gaps

| # | Gap | Componente | Severidade | Estimativa |
|---|-----|-----------|------------|------------|
| G1 | `PriorityClassReady` ausente de `allConditionsTrue()` | operators | CRÍTICO | Pequena |
| G2 | `isBuildTerminal()` duplicada com implementação divergente | api | CRÍTICO | Pequena |
| G3 | Erro silencioso no callback goroutine de build | api | CRÍTICO | Pequena |
| G4 | `db:migrate` no Taskfile está stale (faltam migrations 003, 004) | infra | CRÍTICO | Pequena |
| G5 | Dead-end states: `PromotionSyncFailed` e `RollbackSyncFailed` | pkg | MODERADO | Pequena |
| G6 | SSE client duplicado no CLI (`index.ts` vs `sse-client.ts`) | cli | MODERADO | Média |
| G7 | `index.ts` crescendo para God File (460 linhas) | cli | MODERADO | Média |
| G8 | kubeconform valida apenas `operators/config/` | infra | MODERADO | Pequena |
| G9 | Seed E2E duplicado entre script e package | infra | MODERADO | Pequena |
| G10 | Dead code: `apiClient` não utilizado | cli | MENOR | Pequena |
| G11 | SSE client usa bare `new Error()` | cli | MENOR | Pequena |
| G12 | Steps misnumbered no deploy.ts | cli | MENOR | Pequena |
| G13 | Type assertion loose em `handleError` | cli | MENOR | Pequena |
| G14 | API error em init silenciosamente ignorado | cli | MENOR | Pequena |
| G15 | `activateRelease()` com 180 linhas | api | MENOR | Média |
| G16 | `source/validator.go` sem teste unitário dedicado | api | MENOR | Pequena |
| G17 | `pgxTxDBTX` adapter duplicado em integration tests | api | MENOR | Pequena |
| G18 | `tenant_controller.go` com 786 linhas | operators | MENOR | Média |
| G19 | `LoopZeroError` sem `Unwrap()` para error chain | pkg | MENOR | Pequena |

---

## Ordem de Execução

```
FASE 1 — Bugs Críticos (paralelo, sem dependências)
─────────────────────────────────────────────────────
G1 (PriorityClassReady) ──────────┐
G2 (isBuildTerminal) ─────────────┤── Fixes isolados, zero dependências
G3 (callback logging) ────────────┤
G4 (Taskfile migrations) ─────────┘

FASE 2 — State Machine + Infra (paralelo)
─────────────────────────────────────────────────────
G5 (dead-end states) ─────────────┐
G8 (kubeconform scope) ───────────┤── Cada um em componente diferente
G9 (seed dedup) ──────────────────┘

FASE 3 — CLI Refactoring (sequencial — G6 antes de G7)
─────────────────────────────────────────────────────
G6 (SSE client dedup) ────────────┐
G7 (extract index.ts) ────────────┘── G7 depende de G6 (ambos tocam index.ts)
G10 (dead code) ───────────────────── Junto com G7

FASE 4 — Menores (paralelo)
─────────────────────────────────────────────────────
G11 (typed SSE errors) ───────────┐
G12 (step numbers) ───────────────┤
G13 (type assertion) ─────────────┤── Todos independentes
G14 (init warning) ───────────────┤
G15 (decompose activateRelease) ──┤
G16 (validator test) ─────────────┤
G17 (adapter dedup) ──────────────┤
G18 (split controller) ───────────┤
G19 (LoopZeroError Unwrap) ───────────┘
```

---

## FASE 1 — Bugs Críticos

### G1: `PriorityClassReady` ausente de `allConditionsTrue()`

**Tipo:** Bug funcional
**Spec:** §14.3
**Arquivo:** `operators/internal/controller/tenant_controller.go:195-203`
**Teste:** `operators/internal/controller/tenant_controller_test.go`

#### O que está errado

A função `allConditionsTrue()` lista 7 condições obrigatórias mas omite `ConditionPriorityClassReady`. O controller reconcilia PriorityClass e seta a condition, mas a transição para Active não a exige. Um tenant transita para Active mesmo se PriorityClass falhar silenciosamente.

#### Implementação

**1. Adicionar condição ao slice `required`**

```go
// operators/internal/controller/tenant_controller.go
func allConditionsTrue(conditions []metav1.Condition) bool {
    required := []string{
        loopzerov1.ConditionGitRepoReady,
        loopzerov1.ConditionNamespacesReady,
        loopzerov1.ConditionRBACReady,
        loopzerov1.ConditionNetworkPolicyReady,
        loopzerov1.ConditionArgoCDAppReady,
        loopzerov1.ConditionBuildNamespaceReady,
        loopzerov1.ConditionQuotaEnforced,
        loopzerov1.ConditionPriorityClassReady,  // ← ADICIONAR
    }
    // ...
}
```

**2. Teste TDD — escrever primeiro**

```
- Tenant com todas conditions True EXCETO PriorityClassReady → NÃO transita para Active
- Tenant com TODAS conditions True (incluindo PriorityClassReady) → transita para Active
```

#### Critérios de aceite

- [ ] Teste escrito e falhando antes da correção
- [ ] `PriorityClassReady` presente no slice `required`
- [ ] Tenant com PriorityClass falha → permanece em Provisioning
- [ ] Tenant com todas ok → transita para Active
- [ ] Testes existentes continuam passando

---

### G2: `isBuildTerminal()` duplicada com implementação divergente

**Tipo:** DRY violation / Bug risk
**Spec:** §5.1
**Arquivos:**
- `api/internal/services/build_orchestrator.go:514-521` — usa constantes `statemachine.BuildSucceeded` etc.
- `api/internal/sse/stream_handler.go:226-233` — usa strings hardcoded `"SUCCEEDED"`, `"FAILED"`, `"CANCELLED"`

#### O que está errado

Se as constantes do state machine mudarem (ex: renomear `SUCCEEDED` → `COMPLETED`), a versão do orchestrator atualiza automaticamente mas a versão do SSE handler continua com strings hardcoded. SSE streams nunca terminariam — o client ficaria polling indefinidamente.

#### Implementação

**1. Exportar a função do state machine package**

```go
// pkg/statemachine/build.go — adicionar:
func IsBuildTerminal(state State) bool {
    switch state {
    case BuildSucceeded, BuildFailed, BuildCancelled:
        return true
    }
    return false
}
```

**2. Substituir ambas as implementações locais**

```go
// api/internal/services/build_orchestrator.go — remover isBuildTerminal local, usar:
statemachine.IsBuildTerminal(statemachine.State(status))

// api/internal/sse/stream_handler.go — remover isBuildTerminal local, usar:
statemachine.IsBuildTerminal(statemachine.State(status))
```

**3. Teste**

```
- IsBuildTerminal("SUCCEEDED") → true
- IsBuildTerminal("FAILED") → true
- IsBuildTerminal("CANCELLED") → true
- IsBuildTerminal("PENDING") → false
- IsBuildTerminal("BUILDING") → false
```

#### Critérios de aceite

- [ ] Teste unitário em `pkg/statemachine/build_test.go` para `IsBuildTerminal`
- [ ] Zero implementações locais de `isBuildTerminal` em `api/`
- [ ] SSE handler usa a mesma fonte de verdade que o orchestrator
- [ ] Testes existentes continuam passando

---

### G3: Erro silencioso no callback goroutine de build

**Tipo:** Error handling violation (Princípios §8)
**Arquivo:** `api/internal/routes/builds.go:260-264`

#### O que está errado

```go
go func() {
    ctx := context.Background()
    _ = h.orch.OnBuildCompleted(ctx, buildID, callbackResult)
}()
```

O `_` descarta o erro completamente. Se `OnBuildCompleted` falhar (ex: lock timeout, DB down, state machine error), o build fica preso em estado intermediário sem nenhum log ou trace. Debugging impossível.

#### Implementação

```go
go func() {
    ctx := context.Background()
    if err := h.orch.OnBuildCompleted(ctx, buildID, callbackResult); err != nil {
        h.logger.Error("build completion failed",
            "build_id", buildID,
            "error", err,
        )
    }
}()
```

**Requisito:** O `BuildHandler` precisa ter acesso a um `*slog.Logger`. Verificar se já tem — se não, injetar via construtor.

#### Critérios de aceite

- [ ] Logger injetado no `BuildHandler`
- [ ] Erro logado com build_id e mensagem
- [ ] Teste: callback com orchestrator que retorna erro → log emitido (não panic, não crash)

---

### G4: `db:migrate` no Taskfile está stale

**Tipo:** Infra gap
**Arquivo:** `Taskfile.yml:148-157`

#### O que está errado

O task `db:migrate` lista manualmente apenas `000001_initial_schema.up.sql` e `000002_auth_tenants.up.sql`. As migrations `000003_deploy_support.up.sql` e `000004_add_commit_sha.up.sql` existem mas não são aplicadas. O Tiltfile faz glob correto — o Taskfile diverge.

#### Implementação

Substituir a lista manual por um loop que aplica todas as migrations em ordem:

```yaml
db:migrate:
  desc: Run database migrations against PostgreSQL
  cmds:
    - |
      kubectl --context {{.MGMT_CONTEXT}} port-forward -n loopzero-system svc/postgresql 5432:5432 &
      PF_PID=$!
      trap "kill $PF_PID 2>/dev/null; wait $PF_PID 2>/dev/null" EXIT
      sleep 2
      for f in $(ls api/internal/db/migrations/*.up.sql | sort); do
        echo "Applying $f..."
        PGPASSWORD=loopzero-dev-password psql -h localhost -p 5432 -U loopzero -d loopzero -f "$f"
      done
```

**Nota:** Todas as migrations DEVEM usar `IF NOT EXISTS` / `ON CONFLICT DO NOTHING` para idempotência. Verificar antes de aplicar.

#### Critérios de aceite

- [ ] `task db:migrate` aplica TODAS as migrations `*.up.sql` em ordem
- [ ] Idempotente: rodar 2x não falha
- [ ] Novas migrations futuras são automaticamente incluídas

---

## FASE 2 — State Machine + Infra

### G5: Dead-end states em Promotion e Rollback

**Tipo:** State machine design gap
**Spec:** §5.4 (Promotion), §5.5 (Rollback)
**Arquivos:**
- `pkg/statemachine/promotion.go:37` — `PromotionSyncFailed` não é terminal nem tem transição de saída
- `pkg/statemachine/rollback.go:40` — `RollbackSyncFailed` idem

#### Análise do spec

O spec §5.2 (Release) define explicitamente `SYNC_FAILED → PENDING` (retry automático). Mas para Promotion e Rollback, o spec mostra `SYNC_FAILED` no diagrama mas NÃO lista transições de saída.

O spec §5.4 lista terminal states: `COMPLETED, REJECTED, COMMIT_FAILED`. `SYNC_FAILED` não está na lista terminal, sugerindo que deveria ser recuperável. Porém sem transição definida, o estado é um beco sem saída.

#### Decisão de design

**Opção A:** Marcar como terminal (requer intervenção manual — novo promote/rollback).
**Opção B:** Adicionar retry: `SYNC_FAILED → SYNCING` (via evento `SyncRetry`).

**Recomendação:** Opção A — marcar como terminal. Rationale:
- Promotions e rollbacks são operações explícitas do usuário, não automáticas
- Se sync falha, o usuário deve avaliar e emitir nova promoção/rollback
- Consistente com `COMMIT_FAILED` (terminal) e `REVERT_FAILED` (terminal)
- Retry automático sem intervenção do usuário em operação destrutiva (rollback) é arriscado

#### Implementação

```go
// pkg/statemachine/promotion.go
Terminal: []State{PromotionCompleted, PromotionRejected, PromotionCommitFailed, PromotionSyncFailed},

// pkg/statemachine/rollback.go
Terminal: []State{RollbackCompleted, RollbackRejected, RollbackBlocked, RollbackRevertFailed, RollbackSyncFailed},
```

**⚠️ Confirmar com spec owner antes de implementar.** Se a intenção for retry, implementar Opção B com transição explícita.

#### Critérios de aceite

- [ ] `PromotionSyncFailed` é terminal OU tem transição de retry
- [ ] `RollbackSyncFailed` é terminal OU tem transição de retry
- [ ] Testes atualizados para refletir a decisão
- [ ] Nenhum dead-end state (state sem transição de saída E não-terminal)

---

### G8: kubeconform valida apenas `operators/config/`

**Tipo:** Infra gap
**Arquivo:** `Taskfile.yml:266-269`

#### O que está errado

```yaml
validate:manifests:
  cmds:
    - kubeconform --strict --skip CustomResourceDefinition operators/config/
```

Não valida: `infra/tilt/*.yaml`, `infra/helm/*/templates/*.yaml`, workflow templates.

#### Implementação

```yaml
validate:manifests:
  desc: Validate all K8s manifests with kubeconform
  cmds:
    - kubeconform --strict --skip CustomResourceDefinition operators/config/
    - kubeconform --strict infra/tilt/api-deployment.yaml infra/tilt/operator-deployment.yaml
    - |
      for tpl in infra/helm/argocd/templates/*.yaml infra/helm/argo-workflows/templates/*.yaml; do
        [ -f "$tpl" ] && kubeconform --strict --skip CustomResourceDefinition "$tpl"
      done
```

**Nota:** Helm templates com `{{ }}` não passam por kubeconform diretamente. Apenas templates renderizados ou manifests estáticos devem ser validados. Verificar quais são templates Go vs YAML estático.

#### Critérios de aceite

- [ ] Manifests em `infra/tilt/` validados
- [ ] Templates estáticos em `infra/helm/*/templates/` validados
- [ ] `task validate:manifests` não falha em templates Helm com placeholders

---

### G9: Seed E2E duplicado

**Tipo:** DRY violation
**Arquivos:**
- `infra/scripts/seed_e2e.go` — script standalone
- `infra/tests/seed/seed.go` — package reutilizável

#### O que está errado

SQL idêntico, JSON de Tenant CR idêntico, lógica de polling idêntica nos dois arquivos.

#### Implementação

Reescrever `infra/scripts/seed_e2e.go` para importar o package:

```go
package main

import (
    "context"
    "log"
    "loopzero/infra/tests/seed"
)

func main() {
    ctx := context.Background()
    if err := seed.Run(ctx); err != nil {
        log.Fatal(err)
    }
}
```

Verificar se `seed.Run()` aceita configuração externa (DB URL, cluster context) ou se precisa de refactor para receber parâmetros.

#### Critérios de aceite

- [ ] `infra/scripts/seed_e2e.go` importa e usa `seed.Run()`
- [ ] Zero duplicação de SQL ou JSON entre os dois arquivos
- [ ] `task dev:seed:e2e` continua funcionando

---

## FASE 3 — CLI Refactoring

### G6: SSE client duplicado no CLI

**Tipo:** DRY violation
**Arquivos:**
- `cli/src/index.ts:296-374` — implementação inline de SSE
- `cli/src/api/sse-client.ts` — classe `SSEClient` com reconnect, dedup, polling fallback

#### O que está errado

O deploy action em `index.ts` contém um SSE client inline completo quando já existe `SSEClient` com features superiores (Last-Event-ID, deduplicação, JOURNAL_EXPIRED fallback). Bugs fixados em um não se propagam.

#### Implementação

**1. Adaptar `SSEClient` para retornar um async iterator ou callback pattern compatível com `DeployCommand`**

O `DeployCommand` precisa de:
- Receber eventos como `{type, data}` objects
- Detectar conclusão (evento terminal)
- Abort em timeout

Verificar se a interface de `SSEClient` já suporta isso ou precisa de adaptação.

**2. Substituir implementação inline em `index.ts`**

```typescript
// Em vez de inline SSE parsing, usar:
const sseClient = new SSEClient({ baseUrl, token });
const events = sseClient.connect(streamUrl);
// Passar events para DeployCommand
```

**3. Atualizar testes**

Os testes de deploy que mockam a conexão SSE devem usar o mesmo contrato que `SSEClient`.

#### Critérios de aceite

- [ ] Zero implementação inline de SSE em `index.ts`
- [ ] `SSEClient` usado como única implementação
- [ ] Reconnect e dedup funcionam no deploy flow
- [ ] Testes existentes passam (com adaptação de fakes se necessário)

---

### G7: Extrair deploy wiring de `index.ts`

**Tipo:** SRP violation / Code smell
**Arquivo:** `cli/src/index.ts` (460 linhas)
**Depende de:** G6 (remove SSE inline), G10 (remove dead code)

#### O que está errado

O composition root acumula implementações inline: git operations (child_process), file upload (fetch), SSE parsing, YAML parsing, token validation. O deploy action sozinha ocupa 205 linhas de closures com imports dinâmicos de `child_process`, `fs`, `os`, `path`, `crypto`.

#### Implementação

Extrair para módulos adapter com interfaces claras:

```
cli/src/adapters/
  git.ts          ← getGitCommitSha(): string | null
  source-bundle.ts ← createBundle(dir, apps): { path, cleanup }
  source-upload.ts ← uploadBundle(apiUrl, token, path): string
  loopzero-yaml.ts    ← parseLoopZeroYaml(content): LoopZeroConfig
```

Cada adapter:
- Função pura ou classe simples
- Injetável via `Deps` interface (para testes)
- Sem dependência de `index.ts`

`index.ts` deve ficar com ~150-200 linhas: parse args, wire deps, register commands.

#### Critérios de aceite

- [ ] `index.ts` reduzido para < 250 linhas
- [ ] Adapters em `cli/src/adapters/` com testes dedicados
- [ ] Zero import dinâmico de `child_process`/`fs`/`os` em `index.ts`
- [ ] Testes existentes passam

---

### G10: Dead code — `apiClient` não utilizado

**Tipo:** Dead code
**Arquivo:** `cli/src/index.ts:52`

#### Implementação

Remover a linha `const apiClient = new ApiClient({ token: cmdOpts.token })` que é criada mas nunca usada. A login action cria seu próprio `ApiClient` internamente.

#### Critérios de aceite

- [ ] Variável removida
- [ ] Zero warnings de unused variable

---

## FASE 4 — Menores

### G11: SSE client usa bare `new Error()`

**Arquivo:** `cli/src/api/sse-client.ts:99,125,130,278,290`

Substituir por `ApiError` com `exitCode` apropriado (3 para deploy failure, 1 para connection errors) para que `DeployCommand.handleError()` mapeie corretamente.

#### Critérios de aceite

- [ ] Zero `new Error()` em `sse-client.ts`
- [ ] Erros usam `ApiError` com exit code correto
- [ ] `handleError` produz mensagens actionable

---

### G12: Steps misnumbered no `deploy.ts`

**Arquivo:** `cli/src/commands/deploy.ts`

Corrigir numeração de steps nos comentários: dois step 9 e dois step 10.

#### Critérios de aceite

- [ ] Steps numerados sequencialmente sem duplicatas

---

### G13: Type assertion loose em `handleError`

**Arquivo:** `cli/src/commands/deploy.ts:383`

Substituir `err as Error & { exitCode?: number; statusCode?: number }` por `instanceof ApiError` check.

```typescript
if (err instanceof ApiError) {
    // use err.exitCode, err.statusCode
} else if (err instanceof Error) {
    // generic error handling
}
```

#### Critérios de aceite

- [ ] Sem type assertion com `as`
- [ ] Usa `instanceof` para type narrowing

---

### G14: API error em init silenciosamente ignorado

**Arquivo:** `cli/src/commands/init.ts:173`

O `catch { /* API may not be available */ }` engole completamente o erro de `createProject`. O `loopzero.yaml` é gerado mas o projeto não é criado no servidor.

Emitir warning:

```typescript
catch (err) {
    output.warn('Could not register project on server. Run "loopzero deploy" to retry.');
}
```

#### Critérios de aceite

- [ ] Warning emitido quando API falha (não silencioso)
- [ ] `loopzero.yaml` ainda é gerado (comportamento não muda)
- [ ] Teste cobre cenário de API indisponível → warning

---

### G15: Decompor `activateRelease()` (180 linhas)

**Arquivo:** `api/internal/services/build_orchestrator.go`

Extrair para métodos privados:

```go
func (o *BuildOrchestrator) activateRelease(ctx, ...) error {
    release, err := o.createRelease(ctx, ...)
    if err != nil { return err }

    if err := o.pushToGitOps(ctx, release, ...); err != nil { return err }

    if err := o.waitForSync(ctx, release, ...); err != nil { return err }

    return o.finalizeActivation(ctx, release, ...)
}
```

#### Critérios de aceite

- [ ] `activateRelease` reduzida para < 40 linhas (orquestração)
- [ ] Cada sub-método com responsabilidade única
- [ ] Zero mudança de comportamento (refactor puro)
- [ ] Testes existentes passam sem alteração

---

### G16: `source/validator.go` sem teste unitário dedicado

**Arquivo:** `api/internal/source/validator.go`

Criar `api/tests/unit/source_validator_test.go` com testes para:

```
- Arquivo válido (tar.gz < limite) → pass
- Arquivo excedendo tamanho máximo → erro tipado
- Arquivo com tipo MIME inválido → erro tipado
- Arquivo vazio → erro tipado
```

#### Critérios de aceite

- [ ] Teste dedicado em `api/tests/unit/source_validator_test.go`
- [ ] Cobre happy path + 3 edge cases

---

### G17: `pgxTxDBTX` adapter duplicado em integration tests

**Arquivo:** `api/tests/integration/concurrent_builds_test.go:46-94`

Importar `api/internal/adapters` ao invés de reimplementar. A mesma struct `pgxTxDBTX` e `poolEnvironmentLocker` existem em `api/internal/adapters/adapters.go`.

#### Critérios de aceite

- [ ] Integration test importa `adapters.NewPoolEnvironmentLocker()`
- [ ] Zero duplicação do adapter

---

### G18: Considerar split de `tenant_controller.go` (786 linhas)

**Arquivo:** `operators/internal/controller/tenant_controller.go`

Não urgente. Se o controller crescer mais, separar por concern:

```
tenant_controller.go       ← Reconcile + allConditionsTrue + reconcileDelete (orquestração)
tenant_rbac.go             ← reconcileRBAC, reconcileServiceAccount
tenant_network.go          ← reconcileNetworkPolicies
tenant_build.go            ← reconcileBuildNamespace, reconcilePriorityClass
tenant_gitops.go           ← reconcileGiteaRepo, reconcileArgoCDApp
```

#### Critérios de aceite

- [ ] Se split: cada arquivo < 200 linhas
- [ ] Zero mudança de comportamento
- [ ] Testes passam sem alteração

---

### G19: `LoopZeroError` sem `Unwrap()` para error chain

**Arquivo:** `pkg/errors/errors.go`

Adicionar campo `Cause error` e método `Unwrap()`:

```go
type LoopZeroError struct {
    Code    string
    Message string
    Cause   error  // ← ADICIONAR
}

func (e *LoopZeroError) Unwrap() error {
    return e.Cause
}
```

Atualizar `Wrap()` para preservar a causa:

```go
func (e *LoopZeroError) Wrap(msg string) *LoopZeroError {
    return &LoopZeroError{
        Code:    e.Code,
        Message: fmt.Sprintf("%s: %s", msg, e.Message),
        Cause:   e,
    }
}
```

#### Critérios de aceite

- [ ] `errors.Unwrap(loopzeroErr)` retorna a causa
- [ ] `errors.Is(wrappedErr, originalErr)` funciona via chain
- [ ] Testes unitários para `Unwrap` e chaining

---

## Exit Gate — Validação Final

Após todos os gaps corrigidos:

```bash
# 1. Testes unitários passam
task test:unit

# 2. Lint passa
task lint

# 3. Manifests válidos
task validate:manifests

# 4. Verificar zero termos internos no CLI
grep -rn "ArgoCD\|argocd\|GitOps\|gitops-push\|CRD\|tenant_id\|advisory lock\|pg_advisory\|Gitea\|MinIO\|kubebuilder\|envtest\|Kaniko" cli/src/ --include="*.ts" | grep -v test | grep -v node_modules

# 5. Verificar zero isBuildTerminal local na API
grep -rn "func isBuildTerminal" api/

# 6. Verificar PriorityClassReady no required slice
grep -A 10 "allConditionsTrue" operators/internal/controller/tenant_controller.go | grep PriorityClass

# 7. Verificar callback com logging
grep -A 5 "OnBuildCompleted" api/internal/routes/builds.go | grep -v "_ ="

# 8. Verificar db:migrate aplica todas migrations
grep -c "\.up\.sql" Taskfile.yml  # deve ser 0 (usa glob) ou >= 4

# 9. Verificar dead-end states resolvidos
cd pkg && go test ./statemachine/... -v -run "Terminal\|SyncFailed"

# 10. Testes E2E (se infra disponível)
LOOPZERO_E2E=true task test:e2e
```

**A revisão está aprovada quando todos os 10 passos acima passam.**
