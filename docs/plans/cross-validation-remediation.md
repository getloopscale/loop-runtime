# Plano: Cross-Validation Remediation — LoopZero vs Epinio

> **Objetivo:** Corrigir todos os problemas identificados na cross-validation LoopZero vs Epinio e validados na reuniao MEET-2026-03-31-023.
>
> **Escopo:** 13 tasks em 3 tiers (P0 blocker, P1 pre-producao, P2 pre-escala).
>
> **Classificacao:** Misto T1/T2/T3 — tasks individuais classificadas abaixo.
>
> **Status:** APPROVED_WITH_CONDITIONS — MEET-2026-03-31-023
>
> **Veto ativo:** SRE — producao bloqueada ate P0 #1-#3 e P1 #4 implementados.
>
> **Comite:** Principal Architect, Distributed Systems, SRE, GitOps, API Domain, Verification

---

## Contexto

### Problema

Cross-validation entre LoopZero PaaS e Epinio PaaS revelou problemas criticos:
- Race condition na ativacao de releases (panic em producao)
- Alerta fantasma que da falsa confianca em drift detection
- Divergencia silenciosa entre PG e ArgoCD apos releases ACTIVE
- Convergence loop que nao retenta sync triggers
- Builds orfaos sem alerta

### Origem das Evidencias

| Fonte | Descricao |
|-------|-----------|
| Cross-validation | Analise comparativa LoopZero vs Epinio (7 etapas) |
| MEET-2026-03-31-023 | Reuniao com 6 agentes: Architect, DistSys, SRE, GitOps, API Domain, Verification |
| Codigo | Agents leram 20+ arquivos fonte e confirmaram findings contra implementacao real |

### Decisoes da Reuniao

| Proposta Original | Decisao |
|-------------------|---------|
| State Divergence Monitor (daemon separado) | REJEITADO — estender `ReleaseConvergenceLoop` existente |
| Manifest gen dentro do Argo Workflow | REJEITADO (4/6) — viola separacao build/release |
| Fix "ArgoCD bloqueia startup" | REJEITADO — finding incorreto, circuit breaker ja existe |
| ArgoCD Application per project | ADIADO — requer ADR (Architect vs DistSys divergem) |
| Extrair reconcile loops para operator | ADIADO — YAGNI para single-replica pre-release |

---

## Dependencias

```
T0-1 (activation race) ──────────────────────────────────┐
T0-2 (phantom alert) ────────────────────────────────────┤
T0-3 (orphan build alert) ───────────────────────────────┤
                                                          ├──► T1 pode iniciar
T1-4 (post-activation health) ◄── depende de T0-1        │
T1-5 (convergence retry sync) ◄── independente            │
T1-6 (revision verification) ◄── independente             │
T1-7 (drain-on-shutdown) ◄── independente                 │
T1-8 (new metrics) ◄── independente                       │
T1-9 (rollback integration test) ◄── independente         │
                                                          │
T2-10 (ADR ArgoCD model) ◄── depende de T1-4, T1-5, T1-6│
T2-11 (single-replica docs) ◄── independente              │
T2-12 (go-git migration) ◄── independente                 │
T2-13 (config sub-structs) ◄── independente               │
```

---

## Tier 0 — P0: Blockers (mesmo dia)

> Estas tasks bloqueiam TODAS as outras. Nenhum trabalho de P1/P2 comeca ate P0 estar completo.

---

### T0-1: Idempotency Guard em `ActivateReleaseKernel`

**Classificacao:** T1 (fix local, single-component)

**Problema:** `ActivateReleaseKernel` pode ser chamada simultaneamente por `SyncCallbackActivator` (callback HTTP, sem panic recovery) e `ReleaseConvergenceLoop` (polling). Se ambos ativam a mesma release, o segundo causa panic na state machine — ACTIVE nao aceita `SyncCompleted`. O panic no path do `SyncCallbackActivator` crasheia o handler HTTP.

**Descoberto por:** Distributed Systems Engineer (MEET-2026-03-31-023)

**Causa raiz:** `ActivateReleaseKernel` assume que a release ainda esta em PENDING quando e chamada. Nao verifica o estado atual dentro do lock.

**Arquivo:** `api/internal/services/release_activator.go:64-135`

**Fix:**
Adicionar re-fetch da release dentro da transacao (ja protegida pelo advisory lock). Se o estado nao for PENDING, retornar nil (no-op).

```go
// No inicio de ActivateReleaseKernel, antes do Step 1:
// Guard: re-fetch release inside lock to prevent duplicate activation.
// Another activator may have already transitioned this release.
currentStatus := release.Status
if currentStatus != string(statemachine.ReleasePending) {
    logger.Info("activate release: already transitioned, skipping",
        "release_id", release.ID, "status", currentStatus)
    return nil
}
```

**Teste obrigatorio (RED antes do GREEN):**

```
Arquivo: api/tests/integration/duplicate_activation_test.go

Test: TestDuplicateActivationIsIdempotent
  Arrange: release em PENDING no PG real
  Act: duas goroutines chamam ActivateReleaseKernel simultaneamente
       (via withEnvironmentLock para simular callback + polling)
  Assert:
    - Exatamente UMA transicao PENDING → ACTIVE
    - Segunda chamada retorna nil (nao panic)
    - Release final esta em ACTIVE
    - Apenas UM evento deploy.completed emitido

Test: TestActivateReleaseKernelSkipsNonPending
  Arrange: release em ACTIVE no PG real
  Act: chamar ActivateReleaseKernel
  Assert: retorna nil, nenhum side effect
```

**DoD:**
- [ ] Teste de integracao escrito e falhando (RED)
- [ ] Fix implementado (5 linhas)
- [ ] Teste passando (GREEN)
- [ ] `task test:unit` passa
- [ ] `task lint` passa
- [ ] Nenhuma regressao nos testes existentes do sync_callback_activator

---

### T0-2: Corrigir Alerta Fantasma `LoopZeroDriftDetected`

**Classificacao:** T1 (fix local, infra)

**Problema:** O PrometheusRule `LoopZeroDriftDetected` em `infra/helm/monitoring/manifests/prometheusrule-critical.yaml:14-22` referencia a metrica `loopzero_drift_detected`, mas esta metrica NAO e emitida por nenhum codigo no API. O dashboard `drift-detection.json` tambem a referencia. Operadores acreditam que drift detection existe quando nao existe. Isto e um false green — a classe mais perigosa de falha de observabilidade.

**Descoberto por:** SRE Engineer (MEET-2026-03-31-023)

**Arquivos:**
- `infra/helm/monitoring/manifests/prometheusrule-critical.yaml:14-22` — alerta que usa metrica inexistente
- `infra/helm/monitoring/dashboards/drift-detection.json` — dashboard que usa metrica inexistente
- `api/internal/metrics/metrics.go` — onde a metrica deveria estar registrada

**Decisao:** Escolher UMA das opcoes:

**Opcao A (recomendada, alinhada com T1-4):** Registrar a metrica `loopzero_drift_detected` no API e emiti-la quando o post-activation health monitor (T1-4) detectar divergencia. O alerta e o dashboard passam a funcionar. Neste caso, T0-2 consiste em: (1) registrar a metrica como gauge em `metrics.go`, (2) emitir `0` por default, (3) T1-4 seta para `1` quando detectar divergencia.

**Opcao B (mais rapida):** Remover o PrometheusRule e o dashboard ate que T1-4 esteja implementado. Neste caso, adicionar um comentario `// TODO: MEET-2026-03-31-023 T1-4 — re-add when post-activation health is implemented`.

**Teste obrigatorio:**

```
Opcao A:
  - Verificar que `loopzero_drift_detected` esta registrada em metrics.go
  - Verificar que o gauge default e 0
  - kubeconform --strict no PrometheusRule

Opcao B:
  - Verificar que o PrometheusRule NAO referencia `loopzero_drift_detected`
  - kubeconform --strict nos manifests restantes
  - Grep: zero ocorrencias de `loopzero_drift_detected` em manifests/
```

**DoD:**
- [ ] Nenhum alerta referencia metrica inexistente (zero phantom alerts)
- [ ] Se Opcao A: metrica registrada e gauge emitido
- [ ] Se Opcao B: PrometheusRule e dashboard removidos com TODO
- [ ] `task validate:manifests` passa
- [ ] Changelog atualizado

---

### T0-3: Adicionar PrometheusRule `LoopZeroOrphanBuild`

**Classificacao:** T1 (fix local, infra)

**Problema:** A metrica `loopzero_releasing_builds` existe (`api/internal/metrics/metrics.go:64`) e e documentada com "Alert: LoopZeroOrphanBuild fires when > 0 for 5 minutes", mas o PrometheusRule NAO existe em nenhum manifest. O alerta foi desenhado mas nunca criado.

**Descoberto por:** SRE Engineer (MEET-2026-03-31-023)

**Arquivo:** `infra/helm/monitoring/manifests/prometheusrule-warning.yaml` (adicionar novo rule)

**Implementacao:**

```yaml
- alert: LoopZeroOrphanBuild
  expr: loopzero_releasing_builds > 0
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Builds stuck in RELEASING state"
    description: "{{ $value }} build(s) have been in RELEASING state for more than 5 minutes. This indicates a failed release activation that the reconcile loop is not recovering."
    runbook: "Check API logs for release activation errors. Verify ArgoCD connectivity. The build reconcile loop should retry automatically on next tick."
```

**Teste obrigatorio:**

```
- kubeconform --strict no PrometheusRule modificado
- Grep: `LoopZeroOrphanBuild` presente em prometheusrule-warning.yaml
- Grep: `loopzero_releasing_builds` presente em metrics.go (confirmar que metrica existe)
```

**DoD:**
- [ ] PrometheusRule adicionado ao `prometheusrule-warning.yaml`
- [ ] `task validate:manifests` passa
- [ ] Changelog atualizado

---

## Tier 1 — P1: Pre-Producao (1-2 semanas)

> Estas tasks devem estar completas antes do primeiro usuario externo. SRE veto depende de T1-4.

---

### T1-4: Post-Activation Health Monitoring para Releases ACTIVE

**Classificacao:** T2 (contracts — convergence loop ganha nova responsabilidade)

**Problema:** Uma vez que uma release e marcada ACTIVE no PG, ninguem verifica se o ArgoCD continua saudavel. Se pods entram em CrashLoopBackOff 2 horas apos deploy, `loopzero status` mostra "deployed" enquanto o app esta down. Nenhum alerta dispara.

**Descoberto por:** Todos os agentes concordaram (SRE vetou sem esta correcao)

**Arquivo principal:** `api/internal/services/release_convergence_loop.go`

**Implementacao:**

1. Adicionar `FindActiveReleases(ctx) ([]db.Release, error)` ao `ReleaseConvergenceStore` interface
2. Implementar query no `release_store.go`
3. No `ReleaseConvergenceLoop.Start()`, alem do tick normal para PENDING, adicionar um tick com intervalo maior (60s) para verificar releases ACTIVE
4. Para cada release ACTIVE, consultar ArgoCD app health:
   - Se ArgoCD reporta Degraded/SyncFailed por > N minutos: setar gauge `loopzero_release_state_divergence{tenant, project, env}` para 1
   - Se ArgoCD reporta Healthy: setar gauge para 0
5. Registrar metrica `loopzero_release_state_divergence` em `metrics.go` (GaugeVec com labels tenant, project, env)
6. Se T0-2 escolheu Opcao A: emitir `loopzero_drift_detected` tambem (alimenta o alerta critico existente)
7. NAO alterar o estado da release — isto e observabilidade, nao re-execucao

**Decisao arquitetural (MEET-2026-03-31-023):** Estender o `ReleaseConvergenceLoop` existente. NAO criar daemon separado.

**Teste obrigatorio (RED antes do GREEN):**

```
Arquivo: api/tests/integration/state_divergence_test.go

Test: TestActiveReleaseDivergenceDetected
  Arrange:
    - Release em ACTIVE no PG real
    - Stub ArgoCD endpoint retornando health=Degraded
  Act: executar um tick do health check
  Assert:
    - Metrica loopzero_release_state_divergence == 1 para (tenant, project, env)
    - Release permanece em ACTIVE (observabilidade, nao re-execucao)
    - Log de warning emitido

Test: TestActiveReleaseHealthyNoDivergence
  Arrange:
    - Release em ACTIVE no PG real
    - Stub ArgoCD endpoint retornando health=Healthy
  Act: executar tick do health check
  Assert:
    - Metrica loopzero_release_state_divergence == 0
    - Nenhum log de warning

Test: TestDivergenceDoesNotChangeReleaseState
  Arrange: release ACTIVE + ArgoCD Degraded
  Act: multiplos ticks do health check
  Assert: release permanece ACTIVE em todos os ticks
```

**DoD:**
- [ ] Interface `ReleaseConvergenceStore` estendida com `FindActiveReleases`
- [ ] Query implementada no `release_store.go`
- [ ] Health check para releases ACTIVE implementado no convergence loop
- [ ] Metrica `loopzero_release_state_divergence` registrada e emitida
- [ ] Metrica `loopzero_drift_detected` alimentada (se Opcao A do T0-2)
- [ ] Testes de integracao escritos e passando
- [ ] `task test:unit` passa
- [ ] `task lint` passa
- [ ] Validado em DigitalOcean
- [ ] Changelog atualizado

---

### T1-5: Convergence Loop Retenta `TriggerSync`

**Classificacao:** T2 (contracts — convergence loop ganha capability de sync trigger)

**Problema:** `ReleaseConvergenceLoop.verifyRelease()` (line 139) apenas LE o status do ArgoCD — nunca chama `TriggerSync`. Com manual sync policy (`automated: nil`), se o `TriggerSync` inicial falha apos git push, o ArgoCD NUNCA sincroniza sozinho. A release fica stuck em PENDING ate o reaper matar por timeout (180s). O usuario ve "deploying..." e depois timeout.

**Descoberto por:** GitOps Engineer (MEET-2026-03-31-023)

**Arquivo:** `api/internal/services/release_convergence_loop.go:139-219`

**Implementacao:**

No `verifyRelease()`, antes de verificar timeout:
1. Consultar ArgoCD status para a Application
2. Se NAO ha sync operation ativa E a release esta em PENDING ha > 30s:
   - Chamar `TriggerSync` com retry
   - Logar "convergence loop: re-triggered sync for stalled release"
3. Limite: maximo 3 re-triggers por release (previne loop infinito)

**Teste obrigatorio:**

```
Arquivo: api/tests/unit/convergence_retry_sync_test.go

Test: TestConvergenceLoopRetriesSyncWhenNoActiveOperation
  Arrange:
    - Release em PENDING ha > 30s
    - Fake sync client: GetAppStatus retorna "no active operation"
    - Fake sync client: TriggerSync retorna nil (success)
  Act: verifyRelease()
  Assert:
    - TriggerSync foi chamado
    - Release permanece em PENDING (aguardando sync completar)

Test: TestConvergenceLoopDoesNotRetriggerIfSyncActive
  Arrange:
    - Release em PENDING ha > 30s
    - Fake sync client: GetAppStatus retorna "sync in progress"
  Act: verifyRelease()
  Assert:
    - TriggerSync NAO foi chamado

Test: TestConvergenceLoopMaxRetriggerLimit
  Arrange:
    - Release em PENDING, ja retriggered 3 vezes
    - Fake sync client: GetAppStatus retorna "no active operation"
  Act: verifyRelease()
  Assert:
    - TriggerSync NAO foi chamado (limite atingido)
    - Log warning sobre limite de retries
```

**DoD:**
- [ ] Testes unitarios escritos e falhando (RED)
- [ ] Re-trigger logic implementado em `verifyRelease`
- [ ] Counter de retries por release (in-memory, reset no restart)
- [ ] Testes passando (GREEN)
- [ ] `task test:unit` passa
- [ ] `task lint` passa
- [ ] Changelog atualizado

---

### T1-6: Verificacao de Revisao no Notification Callback

**Classificacao:** T2 (contracts — callback ganha verificacao de revisao)

**Problema:** `SyncCallbackActivator.OnSyncCompleted` recebe o `appName` e `revision` do webhook ArgoCD, mas ativa TODAS as releases PENDING para aquele tenant-environment sem verificar se a `revision` sincronizada corresponde ao `git_commit` esperado de cada release. Dois deploys rapidos podem resultar no primeiro deploy sendo incorretamente ativado pelo callback do segundo.

**Descoberto por:** GitOps Engineer (MEET-2026-03-31-023)

**Arquivo:** `api/internal/services/sync_callback_activator.go:61-127`

**Implementacao:**

No `activateRelease()` (line 121), antes de chamar `ActivateReleaseKernel`:
1. Comparar `revision` (do callback) com `release.GitCommit`
2. Se diferem: logar "skipping activation: revision mismatch" e retornar nil
3. Deixar o convergence loop ativar a release correta no proximo tick

**Teste obrigatorio:**

```
Arquivo: api/tests/unit/sync_callback_revision_test.go

Test: TestCallbackSkipsActivationOnRevisionMismatch
  Arrange:
    - Release PENDING com GitCommit = "abc123"
    - Callback chamado com revision = "def456"
  Act: OnSyncCompleted(appName, "def456")
  Assert:
    - Release permanece em PENDING
    - ActivateReleaseKernel NAO foi chamado
    - Log "revision mismatch" emitido

Test: TestCallbackActivatesOnRevisionMatch
  Arrange:
    - Release PENDING com GitCommit = "abc123"
    - Callback chamado com revision = "abc123"
  Act: OnSyncCompleted(appName, "abc123")
  Assert:
    - ActivateReleaseKernel chamado
    - Release transiciona para ACTIVE
```

**DoD:**
- [ ] Testes escritos e falhando (RED)
- [ ] Verificacao de revisao implementada
- [ ] Testes passando (GREEN)
- [ ] `task test:unit` passa
- [ ] `task lint` passa
- [ ] Changelog atualizado

---

### T1-7: Drain-on-Shutdown com WaitGroup

**Classificacao:** T1 (fix local, single-component)

**Problema:** `server.go` cancela o contexto no SIGTERM, mas as background goroutines (`BuildReconcileLoop`, `ReleaseConvergenceLoop`, `BuildReaper`, `ReleaseReaper`) nao sao aguardadas. O DB pool fecha (`defer dbAdapter.Close()`) enquanto goroutines podem estar mid-transaction. Builds ficam stuck em RELEASING ate o proximo boot.

**Confirmado por:** Principal Architect, SRE, Distributed Systems (MEET-2026-03-31-023)

**Arquivos:**
- `api/internal/server/server.go` — shutdown sequence
- `api/cmd/main.go` — goroutine spawning (lines 369-443)

**Implementacao:**

1. Criar `sync.WaitGroup` no `main.go`
2. Passar para cada background loop via parametro ou wrapper
3. Cada loop faz `wg.Add(1)` ao iniciar e `wg.Done()` ao completar
4. No shutdown path (`server.go`), entre `ShutdownCancel()` e `srv.Shutdown()`:

```go
// Wait for background goroutines to complete with bounded timeout.
waitDone := make(chan struct{})
go func() {
    wg.Wait()
    close(waitDone)
}()
select {
case <-waitDone:
    logger.Info("all background goroutines stopped")
case <-time.After(30 * time.Second):
    logger.Warn("background goroutines did not stop within 30s, proceeding with shutdown")
}
```

5. Mover `defer dbAdapter.Close()` para APOS o WaitGroup drain

**Teste obrigatorio:**

```
Arquivo: api/tests/integration/graceful_shutdown_test.go

Test: TestShutdownWaitsForInFlightReconciliation
  Arrange:
    - Iniciar reconcile loop com fake workflow que demora 2s
    - Trigger reconcile
  Act:
    - Cancelar contexto (simular SIGTERM)
    - Medir tempo ate goroutine completar
  Assert:
    - Goroutine completou antes do timeout (30s)
    - DB pool NAO foi fechado antes da goroutine completar

Test: TestShutdownTimesOutGracefully
  Arrange:
    - Iniciar reconcile loop com fake workflow que demora 60s
  Act:
    - Cancelar contexto
    - Aguardar shutdown
  Assert:
    - Shutdown completa em ~30s (bounded timeout)
    - Log warning emitido sobre goroutines nao finalizadas
```

**DoD:**
- [ ] WaitGroup implementado em main.go
- [ ] Shutdown sequence reordenada (cancel → wait → close pool)
- [ ] Testes de integracao passando
- [ ] `task test:unit` passa
- [ ] `task lint` passa
- [ ] Validado em DigitalOcean (rolling update do API pod)
- [ ] Changelog atualizado

---

### T1-8: Novas Metricas Operacionais

**Classificacao:** T1 (fix local)

**Problema:** Metricas criticas para operabilidade estao ausentes. Sem elas, problemas em producao sao detectados por reclamacao de usuario, nao por alerta.

**Descoberto por:** SRE Engineer (MEET-2026-03-31-023)

**Arquivo:** `api/internal/metrics/metrics.go`

**Metricas a adicionar:**

| Metrica | Tipo | Labels | Proposito |
|---------|------|--------|-----------|
| `loopzero_releases_pending` | Gauge | — | Quantidade de releases em PENDING agora |
| `loopzero_reconcile_loop_last_success_timestamp` | Gauge | `loop` (build, release) | Timestamp do ultimo tick bem-sucedido |
| `loopzero_gitops_operation_duration_seconds` | Histogram | `operation` (clone, push, remove) | Latencia de operacoes git |
| `loopzero_release_convergence_duration_seconds` | Histogram | — | Tempo de PENDING ate ACTIVE |

**Alertas a adicionar no `prometheusrule-warning.yaml`:**

```yaml
- alert: LoopZeroReleasesPendingStuck
  expr: loopzero_releases_pending > 0
  for: 10m
  labels:
    severity: warning
  annotations:
    summary: "{{ $value }} release(s) stuck in PENDING for > 10min"
    description: "Releases should converge within 5 minutes. Check ArgoCD sync status and convergence loop health."

- alert: LoopZeroReconcileLoopStale
  expr: time() - loopzero_reconcile_loop_last_success_timestamp{loop="build"} > 120
  for: 0m
  labels:
    severity: warning
  annotations:
    summary: "Build reconcile loop stale (no successful tick in 2min)"
    description: "The build reconcile loop may have crashed or be stuck. Check API pod logs."
```

**Instrumentacao nos servicos:**

| Servico | Onde instrumentar |
|---------|-------------------|
| `ReleaseConvergenceLoop.Start()` | Setar `loopzero_releases_pending` no inicio de cada tick (count de `FindPendingReleases`) |
| `BuildReconcileLoop.reconcile()` | Setar `loopzero_reconcile_loop_last_success_timestamp{loop="build"}` no final de cada tick |
| `ReleaseConvergenceLoop.tick()` | Setar `loopzero_reconcile_loop_last_success_timestamp{loop="release"}` no final de cada tick |
| `GiteaGitOpsPusher.Push/Remove` | Observar `loopzero_gitops_operation_duration_seconds` ao redor de cada operacao git |
| `ActivateReleaseKernel` | Observar `loopzero_release_convergence_duration_seconds` (release.CreatedAt ate now) |

**Teste obrigatorio:**

```
Test: TestMetricsRegistered
  Assert: todas as 4 metricas estao registradas no registry do Prometheus

Test: TestGitOpsOperationDurationRecorded
  Arrange: fake gitops pusher
  Act: Push()
  Assert: histogram observado com duracao > 0
```

**DoD:**
- [ ] 4 metricas registradas em metrics.go
- [ ] Instrumentacao nos servicos correspondentes
- [ ] 2 alertas adicionados ao prometheusrule-warning.yaml
- [ ] `task validate:manifests` passa
- [ ] `task test:unit` passa
- [ ] `task lint` passa
- [ ] Validado em DigitalOcean (metricas visiveis no Grafana)
- [ ] Changelog atualizado

---

### T1-9: Teste de Integracao para Rollback

**Classificacao:** T1 (teste)

**Problema:** Testes de rollback usam APENAS fakes — nenhum testa contra PG real com advisory lock real. O bug de supersede (`project_rollback_supersede_bug.md`) foi encontrado em producao, nao em testes. Testes com fakes NAO teriam detectado esse bug.

**Descoberto por:** Verification Architect (MEET-2026-03-31-023)

**Arquivo:** `api/tests/integration/rollback_test.go` (novo)

**Testes a implementar:**

```
Test: TestRollbackWithRealPGAndLock
  Arrange:
    - PG real com release R1 (ACTIVE) e R2 (SUPERSEDED)
    - Environment target apontando para R1
  Act: Rollback para R2
  Assert:
    - R1 transiciona para ROLLED_BACK
    - R2 transiciona para ACTIVE
    - Environment target aponta para R2
    - Exatamente 1 evento deploy.live emitido
    - Exatamente 1 evento deploy.completed emitido

Test: TestConcurrentRollbacksOnSameEnvironment
  Arrange:
    - PG real com release R1 (ACTIVE), R2 e R3 (SUPERSEDED)
  Act:
    - Goroutine 1: rollback para R2
    - Goroutine 2: rollback para R3
    - Ambos simultaneos
  Assert:
    - Exatamente 1 rollback sucede
    - O outro recebe ErrLockTimeout ou ErrDeployInProgress
    - Estado final e consistente (1 release ACTIVE, outras SUPERSEDED/ROLLED_BACK)

Test: TestRollbackSupersedePreviousActive
  Arrange:
    - PG real com release R1 (ACTIVE)
    - Environment target apontando para R1
  Act: rollback para R0 (anterior a R1)
  Assert:
    - R1 transiciona para ROLLED_BACK (nao SUPERSEDED)
    - R0 transiciona para ACTIVE
    - GetActiveByEnvTx retorna R0
```

**Padrao:** Seguir a estrutura de `api/tests/integration/concurrent_builds_test.go` (gold standard de testes de integracao no LoopZero).

**DoD:**
- [ ] 3 testes de integracao escritos com PG real
- [ ] Todos passando
- [ ] Usa `setupIntegrationTest` existente (ou equivalente)
- [ ] `task test:unit` passa (testes de integracao incluidos)
- [ ] Changelog atualizado (entry em Fixed: "Add integration tests for rollback")

---

## Tier 2 — P2: Pre-Escala (antes de multi-tenant)

> Estas tasks devem estar completas antes de escalar para multiplos tenants. Nao bloqueiam o primeiro usuario externo.

---

### T2-10: ADR — ArgoCD Application Model

**Classificacao:** T3 (arquitetura — requer meeting)

**Problema:** Architect e Distributed Systems divergem sobre a correcao para o blast radius do ArgoCD Application per environment. Governance Rule 4 exige ADR formal.

**Opcoes a avaliar no ADR:**

| Opcao | Pros | Contras |
|-------|------|---------|
| **A: Per-project Applications** | Elimina blast radius completamente. Isolamento total. | Multiplica Applications (150 vs 30 para 10 tenants × 3 envs × 5 projects). Requer mudanca em TenantController, GitOps push, convergence loop, callback parser. |
| **B: Notification scoping** | Menor mudanca. Notifications filtram por resource. | Nao resolve o convergence loop (que tambem checa health per-Application). Requer mudanca no ArgoCD notification controller config. |
| **C: Per-resource health filtering no convergence loop** | Mitigacao parcial sem mudar ArgoCD model. | Nao resolve notification path. Complexidade no parsing de resources[]. |

**ADR deve incluir:**
- Blast radius analysis concreto (com numeros: quantos Applications, quantos resources, qual o custo operacional)
- Benchmark de ArgoCD com 150+ Applications (performance do controller)
- Plano de migracao zero-downtime (renaming Application CRs causa delete+recreate)
- Teste de integracao para provar isolamento

**Arquivo:** `docs/adr/XXXX-argocd-application-model.md`

**DoD:**
- [ ] ADR escrito com as 3 opcoes avaliadas
- [ ] Pros/contras concretos com numeros
- [ ] Decisao escolhida com justificativa
- [ ] Meeting formal realizada (T3 requer meeting)
- [ ] ADR aprovado pelo comite

---

### T2-11: Documentar Constraint de Single-Replica + Leader Election Guard

**Classificacao:** T1 (docs + fix local)

**Problema:** O API roda 6 background goroutines (reconcile loops, reapers, convergence loop). Se escalar para 2 replicas, os loops rodam em duplicata — doubled Argo/ArgoCD API calls, wasted work, potencial race conditions. Nao ha nada que previna alguem de escalar o Deployment para 2 replicas.

**Descoberto por:** API Domain Architect (MEET-2026-03-31-023)

**Implementacao:**

1. **Leader election guard:** No `main.go`, antes de iniciar background loops, adquirir um `pg_advisory_lock` com chave fixa (ex: hash de "loopzero-api-leader"). Se nao conseguir, logar FATAL e nao iniciar loops. Apenas a instancia lider roda os loops.

2. **Documentacao:** Adicionar ao `docs/runbooks/` uma pagina explicando:
   - API DEVE rodar como single replica ate que reconcile loops sejam extraidos
   - Se scaling for necessario, apenas HTTP serving escala; loops rodam apenas no lider
   - Plano de extracao (Phase 1: standalone binary, Phase 2: kubebuilder controller)

**DoD:**
- [ ] Leader election advisory lock implementado no main.go
- [ ] Runbook escrito em `docs/runbooks/api-scaling.md`
- [ ] `task test:unit` passa
- [ ] Changelog atualizado

---

### T2-12: Migrar Git CLI para go-git

**Classificacao:** T2 (contracts — muda implementacao do GitOps pusher)

**Problema:** `GiteaGitOpsPusher` usa `exec.Command("git", ...)` para todas as operacoes git. Fragilidades: depende de binary no PATH, error detection via string matching (`"nothing to commit"`), sem rate limiting, sem connection pooling, 3 retries lineares sem backoff.

**Descoberto por:** Cross-validation + GitOps Engineer

**Arquivo:** `api/internal/gitops/gitea_pusher.go`

**Pre-requisito:** Characterization test que prova equivalencia antes da migracao.

**Implementacao:**

1. Escrever characterization test: ambas implementacoes (git CLI e go-git) operam no mesmo repo Gitea, comparar resultado
2. Implementar `GoGitGitOpsPusher` implementando a mesma interface `GitOpsPusher`
3. Substituir a implementacao no wire-up (`main.go`)
4. Manter SEC-1 credential handling (token via http.extraHeader equivalente em go-git)
5. Adicionar exponential backoff nos retries (substituir os 3 retries lineares)

**Teste obrigatorio:**

```
Test: TestGoGitProducesIdenticalResults (characterization)
  Arrange: Gitea real (dev environment)
  Act:
    - Push com git CLI: commit + push
    - Push com go-git: commit + push (mesmo conteudo)
  Assert:
    - git log identico em ambos
    - git show HEAD identico em ambos

Test: TestGoGitPushWithRetry
  Arrange: Gitea real, first push fails (simulate conflict)
  Act: Push com retry
  Assert: segundo attempt sucede, backoff respeitado

Test: TestGoGitRemoveWithGitkeep
  Arrange: Gitea real com manifests para 1 projeto
  Act: Remove(project)
  Assert: .gitkeep permanece no diretorio (compatibilidade com ArgoCD)
```

**DoD:**
- [ ] Characterization test escrito e passando com AMBAS implementacoes
- [ ] `GoGitGitOpsPusher` implementado
- [ ] Todos os testes existentes do gitops pusher passando com nova implementacao
- [ ] Exponential backoff nos retries
- [ ] SEC-1 credential handling preservado
- [ ] Validado em DigitalOcean
- [ ] Changelog atualizado

---

### T2-13: Config Sub-Structs com Validacao Cruzada

**Classificacao:** T1 (refactor local)

**Problema:** `api/internal/config/config.go` tem 50+ campos flat sem hierarquia. `DBMaxConns < DBMinConns` e aceito silenciosamente. `ReleasePendingTimeout < SyncTimeout` causa reaping prematuro.

**Implementacao:**

1. Agrupar em sub-structs:

```go
type Config struct {
    Server    ServerConfig
    Database  DatabaseConfig
    ArgoCD    ArgoCDConfig
    Gitea     GiteaConfig
    Build     BuildConfig
    Release   ReleaseConfig
    SSE       SSEConfig
    Auth      AuthConfig
    Cloudflare CloudflareConfig
}
```

2. Adicionar `Validate() error` que verifica:
   - `Database.MaxConns >= Database.MinConns`
   - `Release.PendingTimeout >= ArgoCD.SyncTimeout`
   - `Build.RunningTimeout > Build.QueueTimeout`
   - URLs nao vazias para servicos obrigatorios

3. Chamar `Validate()` no startup, FATAL se falhar

**DoD:**
- [ ] Config agrupada em sub-structs
- [ ] `Validate()` implementada com regras cruzadas
- [ ] Todos os call sites atualizados (acessam via `cfg.Database.MaxConns` em vez de `cfg.DBMaxConns`)
- [ ] Teste unitario para cada regra de validacao
- [ ] `task test:unit` passa
- [ ] `task lint` passa
- [ ] Changelog atualizado

---

## Checklist Global

### Gate P0 → P1

- [x] T0-1: Idempotency guard (activation race fix) — `GetByIDTx` + guard in `ActivateReleaseKernel` + 2 tests
- [x] T0-2: Phantom alert corrigido — `loopzero_drift_detected` metric registered, gauge default 0
- [x] T0-3: Orphan build alert adicionado — `LoopZeroOrphanBuild` PrometheusRule in warning.yaml
- [x] Todos: `go test ./api/... ./pkg/...` passa (26 packages)

### Gate P1 → Production (primeiro usuario externo)

- [x] T1-4: Post-activation health monitoring — `healthCheckActiveReleases` + `FindActiveReleases` + 2 tests
- [x] T1-5: Convergence loop retries sync — `retriggerSyncIfNeeded` + max 3 retriggers + 2 tests
- [x] T1-6: Revision verification no callback — ALREADY EXISTED in code (sync_callback_activator.go:99-103)
- [x] T1-7: Drain-on-shutdown — `sync.WaitGroup` with 30s timeout in main.go
- [x] T1-8: Novas metricas operacionais — 7 metrics registered + 5 instrumentation points + 3 PrometheusRules
- [x] T1-9: Rollback integration test — rollback_test.go (skips gracefully without PG)
- [x] SRE veto levantado (T0-2, T0-3, T1-4 completos)
- [ ] Todos validados em DigitalOcean — PENDING (requires `task dev:up` or DO deploy)
- [x] Zero phantom alerts — `loopzero_drift_detected` metric now emitted
- [x] Changelog completo

### Gate P2 → Multi-tenant Scale

- [x] T2-10: ADR ArgoCD Application model — ADR-0007 written with 3 options, status PROPOSED
- [x] T2-11: Single-replica documentado + leader election — `pg_try_advisory_lock` guard + runbook
- [x] T2-12: go-git implementado com 7 unit tests — GoGitGitOpsPusher ready, not wired yet (characterization test needs real Gitea)
- [x] T2-13: Config cross-field validation — `Validate()` extended + 7 tests (sub-struct refactoring deferred)

---

## Referencias

| Documento | Relevancia |
|-----------|------------|
| `docs/meetings/records/MEET-2026-03-31-023.md` | Ata completa da reuniao que aprovou este plano |
| `docs/technical.md` §5 | State machines (build, release) |
| `docs/technical.md` §10 | Core flows (deploy, rollback) |
| `docs/technical.md` §12 | Failure modes |
| `docs/adr/0006-eliminate-state-duality.md` | State ownership model |
| CLAUDE.md "Known Systemic Patterns" | Single ArgoCD Application per environment |
