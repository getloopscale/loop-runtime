# Operators Review Remediation Plan

> Plano executável baseado no Deep Review do módulo `operators/` (2026-04-21).
> 18 findings: 0 critical, 2 high, 9 medium, 7 low. Score: 7.75/10.
>
> Evidências completas em `review-output/final_report.md`.

---

## Visão Geral

| Sprint | Foco | Tasks | Meta de Score |
|--------|------|-------|---------------|
| **S1** | Pre-release blockers (P1) | 2 | 8.5/10 |
| **S2** | Reliability + Architecture (P2) | 4 | 9.0/10 |
| **S3** | Error handling + Testing (P3) | 5 | 9.5/10 |
| **S4** | Cleanup + Polish (P4) | 5 | 9.75/10 |

**Classificação de mudanças (per governance):**
- S1: T1 (local implementation, single-component)
- S2: T1-T2 (circuit breakers touch shared infra, type tests are local)
- S3: T1 (error handling patterns, tests)
- S4: T0-T1 (trivial cleanups)

---

## Sprint S1 — Pre-release Blockers (P1)

> **Goal:** Eliminar os 2 findings HIGH que representam risco real em produção.
> **Estimativa:** 1 sessão de trabalho.

### Task S1.1: Fix tenant deletion stuck — add periodic requeue

**Finding:** H-02
**File:** `operators/internal/controller/tenant_controller.go:373-384`
**Tier:** T1

**O que mudar:**
Substituir `return ctrl.Result{}, nil` por `return ctrl.Result{RequeueAfter: 1 * time.Hour}, nil` quando `retryCount >= constants.DeletionMaxAttempts`.

**Critérios de aceite:**
- [ ] Após `DeletionMaxAttempts` (10), controller requeue a cada 1 hora (não para)
- [ ] Log message mantido (CRITICAL + manual intervention)
- [ ] Métrica `TenantDeletionStuckTotal` continua sendo emitida
- [ ] Se dependência se recuperar, cleanup retoma automaticamente

**DoD:**
- [ ] Teste unitário `TestDeletionStuck_ContinuesRequeuing` escrito e passando
  - Arrange: tenant com retryCount >= DeletionMaxAttempts
  - Act: reconcileDelete()
  - Assert: result.RequeueAfter == 1 * time.Hour (não zero)
- [ ] Teste de regressão: `TestDeletionBackoff_NormalPath` continua passando
- [ ] `go test ./operators/internal/controller/...` passa
- [ ] `task lint` passa

**Implementação:**
```go
// tenant_controller.go:384 — change:
// BEFORE: return ctrl.Result{}, nil
// AFTER:
return ctrl.Result{RequeueAfter: 1 * time.Hour}, nil
```

Adicionar constante em `constants/timeouts.go`:
```go
RequeueDeletionStuck = 1 * time.Hour
```

---

### Task S1.2: Verify TLS config in production

**Finding:** H-01
**File:** `operators/internal/httpclient/client.go:28-34`
**Tier:** T1

**O que verificar:**
1. Helm chart do operator seta `OPERATOR_INSECURE_SKIP_TLS=false` no env do deployment
2. Pulumi config para produção inclui a env var
3. Se nenhum dos dois seta, adicionar no Helm values

**Critérios de aceite:**
- [ ] Variável `OPERATOR_INSECURE_SKIP_TLS` configurada em produção (Helm ou Pulumi)
- [ ] Operator em dev pode usar `true` (self-signed certs)
- [ ] Operator em prod usa `false` (cert-manager certs)

**DoD:**
- [ ] `grep -r OPERATOR_INSECURE_SKIP_TLS infra/` retorna configuração para prod
- [ ] Se adicionado no Helm: `task validate:manifests` passa
- [ ] Documentar decisão no CHANGELOG.md sob `[Unreleased] > Security`

**Implementação (se não existir):**
Adicionar no Helm values do operator deployment:
```yaml
env:
  - name: OPERATOR_INSECURE_SKIP_TLS
    value: "false"  # prod: enforce TLS verification
```

---

## Sprint S2 — Reliability + Architecture (P2)

> **Goal:** Adicionar circuit breakers faltantes, corrigir error matching, adicionar condition para pull secret, type tests.
> **Estimativa:** 2-3 sessões de trabalho.

### Task S2.1: Add CircuitBreaker wrapper for MinIO provisioner

**Finding:** M-01
**File:** `operators/internal/minio/provisioner.go`
**Tier:** T1

**O que fazer:**
Criar `CircuitBreakerProvisioner` para MinIO seguindo o padrão de `gitea/circuitbreaker_provisioner.go`.

**Critérios de aceite:**
- [ ] `minio.NewCircuitBreakerProvisioner(real, config, onStateChange)` existe
- [ ] Implementa `CredentialProvisioner` interface
- [ ] `cmd/main.go` wraps MinIO provisioner com CB (quando não-nil)
- [ ] CB config usa mesmos defaults (OpenThreshold=5, CloseThreshold=1, RetryInterval=120s)
- [ ] Métrica `TenantCircuitBreakerState` reporta estado para "minio"

**DoD:**
- [ ] Teste: `TestMinIOCircuitBreaker_OpensOnRepeatedFailures`
- [ ] Teste: `TestMinIOCircuitBreaker_ClosesOnSuccess`
- [ ] `go test ./operators/internal/minio/...` passa
- [ ] `go test ./operators/...` passa (nenhum teste existente quebra)
- [ ] `task lint` passa

---

### Task S2.2: Add CircuitBreaker wrapper for Harbor client

**Finding:** M-01
**File:** `operators/internal/harbor/client.go`
**Tier:** T1

**O que fazer:**
Criar `CircuitBreakerClient` wrapper para Harbor, mesmo padrão de S2.1.

**Critérios de aceite:**
- [ ] `harbor.NewCircuitBreakerClient(real, config, onStateChange)` existe
- [ ] Wraps todas as operações públicas de `*Client` (EnsureProject, EnsureRobotAccount, etc.)
- [ ] `cmd/main.go` wraps Harbor client com CB (quando não-nil)
- [ ] Nil-guard preservado: se HarborClient é nil, CB wrapper também é nil

**DoD:**
- [ ] Teste: `TestHarborCircuitBreaker_OpensOnRepeatedFailures`
- [ ] `go test ./operators/internal/harbor/...` passa
- [ ] `task lint` passa

---

### Task S2.3: Replace custom contains() with strings.Contains in MinIO

**Finding:** M-02
**File:** `operators/internal/minio/provisioner.go:234-256`
**Tier:** T0

**O que fazer:**
1. Remover funções `contains()` e `searchString()` (linhas 245-256)
2. Usar `strings.Contains()` em `isNotFoundError()`
3. Adicionar `"strings"` ao import

**Critérios de aceite:**
- [ ] Funções `contains()` e `searchString()` removidas
- [ ] `isNotFoundError()` usa `strings.Contains()`
- [ ] Comportamento idêntico (case-sensitive matching mantido)

**DoD:**
- [ ] Teste: `TestIsNotFoundError_MatchesKnownMessages` — tabela com todas as mensagens conhecidas
- [ ] Teste: `TestIsNotFoundError_RejectsUnknownErrors` — erros genéricos retornam false
- [ ] `go test ./operators/internal/minio/...` passa
- [ ] `task lint` passa

**Implementação:**
```go
import "strings"

func isNotFoundError(err error) bool {
    if err == nil {
        return false
    }
    msg := err.Error()
    return strings.Contains(msg, "does not exist") ||
        strings.Contains(msg, "not found") ||
        strings.Contains(msg, "The specified user does not exist") ||
        strings.Contains(msg, "The canned policy does not exist")
}
// Remove contains() and searchString() entirely
```

---

### Task S2.4: Add PullSecretReady condition to EnvironmentController

**Finding:** M-05
**File:** `operators/internal/controller/environment_controller.go:330-347`
**Tier:** T1

**O que fazer:**
1. Adicionar `TypePullSecretReady` em `conditions/types.go`
2. Setar condition `PullSecretReady=True` quando `ensurePullSecret` sucede
3. Setar condition `PullSecretReady=False` com reason+message quando falha
4. Condition NÃO bloqueia transição para Active (non-blocking, informational)

**Critérios de aceite:**
- [ ] `conditions.TypePullSecretReady` definido
- [ ] Sucesso de `ensurePullSecret()` → `PullSecretReady=True`
- [ ] Falha de `ensurePullSecret()` → `PullSecretReady=False` com mensagem de erro
- [ ] `PullSecretReady` NÃO está na lista de `requiredConditionsForActive()`
- [ ] Condition é informational — visível no `kubectl describe environment`

**DoD:**
- [ ] Teste envtest: `TestEnvironment_PullSecretConditionSetOnSuccess`
- [ ] Teste envtest: `TestEnvironment_PullSecretConditionFalseOnError` (usando fake que falha)
- [ ] `go test ./operators/internal/controller/...` passa
- [ ] `task lint` passa

---

## Sprint S3 — Error Handling + Testing (P3)

> **Goal:** Padronizar error handling, corrigir Gitea EnsureOrg, remover AppSecret CRD, adicionar testes de concorrência e deletion stuck.
> **Estimativa:** 2-3 sessões de trabalho.

### Task S3.1: Standardize status update error handling pattern

**Finding:** M-03
**Files:** Todos os controllers em `operators/internal/controller/`
**Tier:** T1

**O que fazer:**
Adotar padrão único: **log statusErr, return original error**. Este é o padrão que TenantController.runSteps já usa.

**Critérios de aceite:**
- [ ] Todos os controllers seguem: `if statusErr := r.Status().Update(...); statusErr != nil { logger.Error(statusErr, ...) }` — NÃO retornam statusErr
- [ ] O erro original de reconcile é sempre retornado (nunca mascarado por statusErr)
- [ ] Padrão documentado como comentário no helper mais usado

**DoD:**
- [ ] ClusterCredentialController: corrigir paths que retornam statusErr
- [ ] ApplicationController: adicionar log em paths que ignoram statusErr silenciosamente
- [ ] `go test ./operators/internal/controller/...` passa
- [ ] `task lint` passa

---

### Task S3.2: Fix Gitea EnsureOrg — return error on verification failure

**Finding:** M-04
**File:** `operators/internal/gitea/api.go` (EnsureOrg)
**Tier:** T1

**O que fazer:**
Quando 422 e verificação do org falha (GET retorna erro), retornar o erro em vez de nil.

**Critérios de aceite:**
- [ ] 422 + GET sucesso (org existe) → return nil (idempotent, correto)
- [ ] 422 + GET falha (network error) → return error (não swallow)
- [ ] 422 + GET 404 (org não existe) → return original 422 error

**DoD:**
- [ ] Teste: `TestEnsureOrg_422_VerificationSucceeds_ReturnsNil`
- [ ] Teste: `TestEnsureOrg_422_VerificationFails_ReturnsError`
- [ ] `go test ./operators/internal/gitea/...` passa
- [ ] `task lint` passa

---

### Task S3.3: Remove AppSecret from CRD scheme registration

**Finding:** M-06
**File:** `operators/api/v1/groupversion_info.go`
**Tier:** T1

**O que fazer:**
Remover `&AppSecret{}` e `&AppSecretList{}` de `SchemeBuilder.Register()`. Manter o tipo Go para referência futura mas não registrar como CRD ativo.

**Critérios de aceite:**
- [ ] `AppSecret` e `AppSecretList` removidos de `SchemeBuilder.Register()`
- [ ] CRD YAML removido de `config/crd/bases/` (se existir)
- [ ] Tipos Go mantidos (para uso futuro em Sprint 5+)
- [ ] Nenhum controller ou teste referencia AppSecret via K8s API

**DoD:**
- [ ] `go test ./operators/...` passa
- [ ] `task validate:manifests` passa
- [ ] `task lint` passa
- [ ] CHANGELOG: `[Unreleased] > Removed: AppSecret CRD unregistered (no controller, secrets via ESO)`

---

### Task S3.4: Add concurrent reconciliation test for TenantController

**Finding:** M-08
**Tier:** T1

**O que fazer:**
Criar teste envtest que cria 3 Tenants simultaneamente e verifica que todos chegam a Active sem interferência.

**Critérios de aceite:**
- [ ] 3 Tenants criados em paralelo (diferentes nomes)
- [ ] Todos transicionam para Active
- [ ] Cada Tenant tem seus próprios Environment CRs (sem mistura)
- [ ] Nenhum erro de conflito persistente (transient conflicts são OK via retry)
- [ ] FakeProvisioner call counts refletem 3 tenants

**DoD:**
- [ ] Teste: `TestConcurrentTenantProvisioning_AllReachActive`
- [ ] Teste roda em < 30s
- [ ] `go test ./operators/internal/controller/... -count=1` passa (deterministic)

---

### Task S3.5: Add deletion stuck path test

**Finding:** M-09
**Tier:** T1

**O que fazer:**
Teste unitário que exercita o path completo: cleanup falha → retryCount incrementa → max atingido → métrica emitida → requeue lento (após fix S1.1).

**Critérios de aceite:**
- [ ] Teste injeta falha no GitProvisioner.ArchiveRepo
- [ ] Verifica retryCount incrementado via annotation
- [ ] Verifica que após DeletionMaxAttempts, result tem RequeueAfter > 0
- [ ] Verifica que `TenantDeletionStuckTotal` counter incrementou

**DoD:**
- [ ] Teste: `TestTenantDeletion_Stuck_EmitsMetricAndRequeues`
- [ ] Depende de S1.1 estar completo (requeue em vez de stop)
- [ ] `go test ./operators/internal/controller/...` passa

---

## Sprint S4 — Cleanup + Polish (P4)

> **Goal:** Limpeza de code smells low-severity. Melhoria incremental.
> **Estimativa:** 1-2 sessões de trabalho.

### Task S4.1: Use constants.NamespacePlatformSystem

**Finding:** L-01
**File:** `operators/internal/controller/environment_controller.go:393`
**Tier:** T0

**O que fazer:**
Substituir `"platform-system"` por `constants.NamespacePlatformSystem`.

**Critérios de aceite:**
- [ ] Nenhuma ocorrência de string literal `"platform-system"` em `environment_controller.go`
- [ ] Usa `constants.NamespacePlatformSystem` no lugar

**DoD:**
- [ ] `go test ./operators/internal/controller/...` passa
- [ ] `task lint` passa

---

### Task S4.2: Extract main.go helper functions

**Finding:** L-02
**File:** `operators/cmd/main.go`
**Tier:** T0

**O que fazer:**
Extrair funções: `setupGiteaProvisioner()`, `setupArgoCDProvisioner()`, `setupMultiClusterClients()`, `setupHarborClient()`, `setupMinIOProvisioner()`.

**Critérios de aceite:**
- [ ] `main()` function ≤ 80 LOC
- [ ] Cada helper function retorna (resultado, error)
- [ ] Comportamento idêntico (nenhuma mudança funcional)

**DoD:**
- [ ] `go build ./operators/cmd/...` compila
- [ ] `go test ./operators/...` passa
- [ ] `task lint` passa

---

### Task S4.3: Add ownership check on platform-tokens delete

**Finding:** L-03
**File:** `operators/internal/controller/platformbootstrap_controller.go:83-88`
**Tier:** T1

**O que fazer:**
Antes de deletar `platform-tokens`, verificar que o Secret tem label `managed-by: platformbootstrap-controller` ou ownerReference apontando para o CR sendo deletado.

**Critérios de aceite:**
- [ ] Delete só ocorre se Secret tem label ou ownerRef correto
- [ ] Se Secret não tem label/ownerRef, log warning e skip delete (não falha)

**DoD:**
- [ ] Teste: `TestBootstrapDeletion_SkipsUnownedSecret`
- [ ] `go test ./operators/internal/controller/...` passa

---

### Task S4.4: Remove ReconciliationReport dead code

**Finding:** L-05
**Files:** `operators/api/v1/reconciliationreport_types.go`, `reconciliationreport_types_test.go`
**Tier:** T0

**O que fazer:**
1. Remover `ReconciliationReport` e `ReconciliationReportList` de `SchemeBuilder.Register()`
2. Mover tipos para `api/v1/experimental/` ou adicionar comment `// EXPERIMENTAL: not registered, no controller`
3. Remover CRD YAML de `config/crd/bases/` (se existir)

**Critérios de aceite:**
- [ ] `ReconciliationReport` não registrado como CRD ativo
- [ ] Tipos Go preservados com comment de experimental
- [ ] Testes de tipo preservados (validam schema para futura implementação)

**DoD:**
- [ ] `go test ./operators/...` passa
- [ ] `task validate:manifests` passa
- [ ] CHANGELOG: `[Unreleased] > Removed: ReconciliationReport CRD unregistered (dead code, no controller)`

---

### Task S4.5: Add Reset() calls in Ginkgo test setup

**Finding:** L-06
**File:** `operators/internal/controller/suite_test.go`
**Tier:** T0

**O que fazer:**
Adicionar `BeforeEach` ou `JustBeforeEach` no `suite_test.go` que chama `Reset()` em todos os fake provisioners.

**Critérios de aceite:**
- [ ] `FakeGitProvisioner.Reset()` chamado entre Contexts
- [ ] `FakeArgoCDProvisioner.Reset()` chamado entre Contexts
- [ ] `FakeMinIOProvisioner.Reset()` chamado entre Contexts
- [ ] Nenhum teste existente quebra com o reset

**DoD:**
- [ ] `go test ./operators/internal/controller/... -count=3` passa (3 runs para detectar state leak)
- [ ] `task lint` passa

---

## Sprint S2.5: Add type tests for 5 CRDs

**Finding:** M-07
**Tier:** T1

> Esta task é large o suficiente para ser standalone. Pode ser executada em paralelo com qualquer outro sprint.

**O que fazer:**
Criar 5 arquivos de teste seguindo o padrão de `tenant_types_test.go`:
1. `application_types_test.go`
2. `environment_types_test.go`
3. `exposedroute_types_test.go`
4. `gitrepositorybinding_types_test.go`
5. `platformbootstrap_types_test.go`

**Para CADA tipo, testar:**
- [ ] Todos os valores de phase/enum são distintos
- [ ] Todos os campos de Spec populam corretamente
- [ ] Todos os campos de Status populam corretamente
- [ ] Conditions management (Set, Get, IsTrue)
- [ ] List type contém items

**Critérios de aceite:**
- [ ] 5 novos arquivos de teste criados
- [ ] ≥ 8 testes por tipo (mínimo: enums + spec + status + conditions + list)
- [ ] ≥ 40 novos testes no total

**DoD:**
- [ ] `go test ./operators/api/v1/...` passa
- [ ] Nenhum teste existente quebra
- [ ] `task lint` passa

---

## Ordem de Execução

```
S1.1 (deletion stuck)  ─────────┐
S1.2 (TLS config)      ─────────┤
                                 ├──→ S2.1 (MinIO CB) ──→ S2.2 (Harbor CB)
                                 │
S2.3 (strings.Contains) ────────┤
S2.4 (PullSecret condition) ────┤
                                 │
S2.5 (type tests) ──────────────┤  (parallelizable)
                                 │
                                 ├──→ S3.1 (status pattern) 
                                 ├──→ S3.2 (Gitea EnsureOrg)
                                 ├──→ S3.3 (AppSecret CRD)
                                 ├──→ S3.4 (concurrent test) 
                                 ├──→ S3.5 (deletion test) ──→ depends on S1.1
                                 │
                                 ├──→ S4.1..S4.5 (cleanup, all independent)
```

**Dependências:**
- S3.5 depende de S1.1 (testa o novo comportamento de requeue)
- S2.1 e S2.2 são independentes mas seguem mesmo padrão (fazer sequencial)
- Tudo em S4 é independente e pode ser feito em qualquer ordem

---

## Gate de Conclusão

Após todos os sprints:

```bash
# Verificação final
cd operators
go test ./...                    # Todos os testes passam
cd .. && task lint               # Lint clean
task validate:manifests          # Manifests válidos

# Score esperado após remediação: 9.5+/10
```

| Dimensão | Antes | Depois | Delta |
|----------|-------|--------|-------|
| Correto | 8/10 | 9/10 | +1 (error handling padronizado) |
| Completo | 7/10 | 9/10 | +2 (type tests, dead code removido) |
| Confiável | 7/10 | 9.5/10 | +2.5 (CB completo, deletion recovery, pull secret visível) |
| Controlável | 9/10 | 9.5/10 | +0.5 (PullSecretReady condition) |
| **Total** | **7.75** | **9.25** | **+1.5** |
