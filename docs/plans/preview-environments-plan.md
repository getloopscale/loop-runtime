# Plan: Preview Environments (Sprint 5)

> **Version 1.0** — Implementar preview environments completos: cada Pull Request no GitHub gera automaticamente um ambiente efêmero com URL única, build isolado, e cleanup automático por TTL ou merge. Isso é table stakes para competir com Vercel, Railway e Render — sem preview envs, LoopZero é deploy+rollback, não um PaaS que justifica $29/mês. O resultado final: developer abre PR → em ~5min tem URL funcional → PR fecha → ambiente destruído.

## Context

### O que existe hoje

Preview Environments é o caso mais completo do padrão "implemented but not wired" do LoopZero:

| Camada | Estado | Detalhe |
|--------|--------|---------|
| **CLI** (`loopzero preview list/inspect/delete`) | Completo | 3 commands, testes, retorna `PREVIEW_NOT_AVAILABLE` (AUDIT-28) |
| **API Routes** (`GET/DELETE /api/v1/previews`) | Definido | `PreviewHandler` + `PreviewStore` interface existem, não registrados no router |
| **API Store** | Interface apenas | `PreviewStore` sem implementação PostgreSQL |
| **Database** | Inexistente | Nenhuma tabela `preview_environments` |
| **CRD** (`PreviewEnvironment`) | Completo | Spec/Status/Phases definidos, schema YAML gerado |
| **State Machine** (`pkg/statemachine/preview.go`) | Completo | 7 estados, 10 transições, 8 testes — **importado mas não usado** no controller |
| **Controller** (`preview_controller.go`) | Parcial | Namespace + PSA + TTL + cleanup OK. **Sem workload deployment**. **Não registrado no main.go** |
| **GitHub Webhook** | Parcial | `PullRequestEvent` type definido, `OnPullRequest` handler slot existe, nenhum handler implementado |

### Evidência que justifica o trabalho

1. **Todos os concorrentes têm**: Vercel (automático), Railway (PR→env isolado), Render (PR→URL), Northflank (PR→secrets herdados)
2. **Maturity Assessment 2.5/5 em Product Completeness**: preview envs explicitamente listado como razão
3. **Impact em pricing**: feature ausente no tier Pro ($29/mês) que compete com Render/Railway que oferecem
4. **CLI já promete**: 3 commands implementados + testados que retornam "not available"
5. **25/30 E2E pass** — o core deploy flow funciona, permitindo construir preview em cima

### O que NÃO está no escopo

- Preview com banco de dados isolado (PG por namespace) — futuro, Enterprise
- Preview de múltiplos serviços (monorepo multi-app preview) — v2
- Dashboard UI para previews — CLI first (Rule 9)
- Cluster dedicado para preview — namespace isolation no workload cluster (decisão D1)

## Objective

**Done = developer abre PR em repo conectado → preview environment criado automaticamente com URL acessível → PR merge/close → ambiente destruído.**

Metas mensuráveis:
1. PR aberto → preview URL funcional em ≤6 minutos (alinhado com deploy ~4min + overhead)
2. PR closed/merged → namespace deletado em ≤5 minutos
3. TTL default 72h com cleanup automático para PRs abandonados
4. `loopzero preview list/inspect/delete` funcionais contra API real
5. Zero internal terms expostos (Rule 3)
6. State machine enforced em todas as transições (Rule 8)
7. Controller registrado e reconciliando no mgmt cluster
8. Testes: Ginkgo+envtest para controller, Go tests para store, integração para webhook→preview flow

## ADRs

### D1 — Namespace no workload cluster, não cluster dedicado

**Decision:** Preview environments rodam como namespaces isolados no workload cluster existente, não em cluster dedicado.

**Rationale:**
- Custo: cluster DO ~$100-200/mês fixo vs. zero custo adicional com namespaces
- Previews são efêmeros (horas/dias) — infra permanente é desperdício
- Concorrentes (Railway, Render, Northflank) usam namespace isolation
- Kubernetes já oferece isolamento suficiente via PSA + NetworkPolicy + ResourceQuota + LimitRange
- Cell architecture é o upgrade path natural para Enterprise (muitos previews simultâneos)

**Consequences:**
- Preview ruidoso pode impactar workloads (mitigado por ResourceQuota/LimitRange)
- Compartilha rede do cluster (mitigado por NetworkPolicy deny-all + allow-ingress)
- Não requer nova infra — deploy imediato no workload cluster existente

### D2 — Reusar deploy flow existente, não pipeline separada

**Decision:** Preview builds usam o mesmo fluxo: Argo Workflow → BuildKit → cosign → manifests → Gitea → ArgoCD. Não criar pipeline alternativa.

**Rationale:**
- DRY: o deploy flow já funciona E2E e é validado (25/30 E2E pass)
- Consistência: preview usa exatamente o mesmo build que produção — sem divergência
- Manutenção: uma pipeline para manter, não duas
- O que muda é apenas o target namespace e a URL gerada

**Consequences:**
- Preview consome capacidade do build cluster (mitigado: um build por vez por PR, concurrent check)
- ArgoCD Application CR extra por preview (cleanup obrigatório)
- GitOps repo precisa de path separado por preview

### D3 — API cria PreviewEnvironment CR, controller faz o resto

**Decision:** A API (webhook handler) cria o `PreviewEnvironment` CR no mgmt cluster. O controller observa o CR e orquestra namespace + build + deploy no runtime/build clusters.

**Rationale:**
- Padrão Kubernetes nativo: declarativo (CR) + reconciliação (controller)
- Consistente com TenantController, EnvironmentController, ApplicationController
- Resiliência: controller retenta automaticamente em falhas
- Separação de concerns: API valida + cria intent, controller executa

**Consequences:**
- Controller precisa ser capaz de submeter builds (nova dependência ou API call-back)
- Estado do preview vive em dois lugares: CR (status) + DB (metadata para API queries)
- Sync entre CR status e DB row precisa ser eventual-consistent

### D4 — Controller delega build ao API, não submete diretamente

**Decision:** O controller não submete Argo Workflows diretamente. Em vez disso, ao transicionar para CREATING, ele chama a API interna (`POST /api/v1/internal/previews/{id}/build`) para submeter o build, reusando toda a lógica de `BuildOrchestrator.SubmitBuild`.

**Rationale:**
- Build submission é complexa (lock, concurrent check, plan enforcement, app spec normalization, workflow templating)
- Duplicar essa lógica no operator viola DRY gravemente
- API já tem acesso a: PG, S3, Argo client, Gitea, ArgoCD — operator não tem
- Padrão similar ao auto-deploy: webhook handler chama SubmitBuild via serviço interno

**Consequences:**
- Controller depende da API estar disponível (mitigado: requeue on failure)
- Precisa endpoint interno (não exposto externamente) para trigger build
- Build callback (completion) precisa saber que é preview para atualizar o CR

### D5 — GitHub comment com URL do preview

**Decision:** Após preview ficar ACTIVE, API posta comment no PR do GitHub com a URL do preview.

**Rationale:**
- É a experiência que Vercel e Netlify oferecem — developer vê URL sem sair do GitHub
- GitHub App já tem permissões de escrita em PRs (pull_requests: write)
- Reduz fricção: não precisa rodar `loopzero preview list` para encontrar URL

**Consequences:**
- Precisa de GitHub API client para criar/atualizar comments
- Comment deve ser atualizado (não duplicado) em cada push subsequente
- Precisa identificar o comment anterior (marker text ou metadata)

### D6 — Preview namespace naming via pkg/naming

**Decision:** Usar `naming.PreviewNamespace(tenantSlug, projectSlug, prNumber)` que já existe e gera formato `{tenantSlug}-{projectSlug}-pr-{prNumber}`.

**Rationale:**
- Função já existe, testada, RFC 1123 compliant
- Consistente com `naming.EnvironmentNamespace`, `naming.BuildNamespace`
- O controller atual usa formato diferente (`preview-{tenantID}-pr{prNumber}`) — deve migrar para `naming.PreviewNamespace`

**Consequences:**
- Controller precisa ser corrigido para usar `naming.PreviewNamespace` em vez de `fmt.Sprintf`
- Namespace name inclui projectSlug, permitindo múltiplos previews do mesmo tenant/PR em projetos diferentes

### D7 — Dual-write: CR status + DB row

**Decision:** Preview metadata existe em dois lugares: `PreviewEnvironment` CR (status operacional, reconciliado pelo controller) e `preview_environments` tabela PG (queries da API, audit, histórico).

**Rationale:**
- API precisa de queries rápidas (list by tenant, get by ID) — PG é a ferramenta certa
- Controller precisa de status operacional persistente — CR é a ferramenta certa
- Padrão consistente com builds/releases (DB) + Applications/Environments (CR)

**Consequences:**
- Sync entre CR e DB precisa ser eventual-consistent
- API cria row na DB ao criar o CR, controller atualiza status via API callback
- Em caso de divergência, CR é source of truth para estado operacional

## Dependency Graph

```
Phase 0 (Foundation) ──▶ Phase 1 (Controller) ──▶ Phase 2 (API + DB) ──▶ Phase 3 (GitHub Integration)
                                                                                    │
                                                                                    ▼
                                                                            Phase 4 (Wiring + E2E)
```

- **Phase 0 → Phase 1**: Controller precisa do state machine fix e naming fix antes de implementar workload
- **Phase 1 → Phase 2**: API precisa do controller funcional para queries serem significativas
- **Phase 2 → Phase 3**: GitHub integration precisa de API + DB para persistir previews
- **Phase 3 → Phase 4**: E2E só faz sentido com todas as camadas conectadas

Phases 0 e 1 são sequenciais. Phases 2 e 3 têm dependência sequencial mas tasks internas podem paralelizar.

---

## Phase 0: Foundation — Fix Existing Code

**Objective:** Corrigir o código existente para usar state machine e naming corretamente, e registrar o controller no manager.

### T0.1 — Migrar controller para usar state machine

#### Objective
O controller atual usa `preview.Status.Phase = loopzerov1.PreviewPhaseActive` (atribuição direta). Isso viola Rule 8 ("State machines are strict — use state machine functions, never direct status updates"). Migrar para `statemachine.NewPreviewMachine().Transition()`.

#### Evidence
- `preview_controller.go:79` — `preview.Status.Phase = loopzerov1.PreviewPhaseCreating` (direto)
- `preview_controller.go:133` — `preview.Status.Phase = loopzerov1.PreviewPhaseActive` (direto)
- `preview_controller.go:155` — `preview.Status.Phase = loopzerov1.PreviewPhaseExpiring` (direto)
- `preview_controller.go:169` — `preview.Status.Phase = loopzerov1.PreviewPhaseDeleting` (direto)
- `preview_controller.go:29` — `var _ = statemachine.NewPreviewMachine` (imported, unused)
- `environment_controller.go:30` — `var environmentSM = statemachine.NewEnvironmentMachine()` (padrão correto)

#### Files to edit
```
operators/internal/controller/preview_controller.go — Substituir atribuições diretas por machine.Transition()
pkg/statemachine/preview.go — Verificar alinhamento entre states SM (CREATING) e CRD phases (Creating)
operators/api/v1/preview_types.go — Pode precisar de helper para converter SM state ↔ CRD phase
```

#### Deep file dependency analysis

**`preview_controller.go`:**
- Hoje: atribui phases diretamente via `preview.Status.Phase = loopzerov1.PreviewPhaseXxx`
- Mudança: usar `previewSM.Transition(currentState, event)` e mapear resultado para CRD phase
- Downstream: nenhum — controller é leaf node, ninguém depende dele (ainda nem está registrado)

**`pkg/statemachine/preview.go`:**
- Hoje: states são `CREATING`, `ACTIVE` etc. (uppercase)
- CRD phases são `Creating`, `Active` etc. (capitalized)
- Precisa de mapeamento bidirecional ou normalização

**`preview_types.go`:**
- Hoje: define `PreviewPhase` type com constantes
- Mudança: adicionar funções `PhaseToState()` e `StateToPhase()` para bridge SM ↔ CRD

#### Deep Dives

**Mapeamento State Machine ↔ CRD Phase:**
```go
// SM State (pkg/statemachine)    CRD Phase (operators/api/v1)
// CREATING                    →  Creating
// ACTIVE                      →  Active
// EXPIRING                    →  Expiring
// DELETING                    →  Deleting
// DELETED                     →  Deleted
// FAILED                      →  Failed
// CLEANUP_FAILED              →  CleanupFailed
```

Padrão: `strings.ToUpper(phase)` converte CRD→SM, `cases.Title(language.English).String(strings.ToLower(state))` converte SM→CRD. Ou usar mapa explícito (mais seguro, sem dependência de locale).

**Invariantes:**
- Toda transição de phase DEVE passar pelo state machine
- Transição inválida = panic (programming error, per Rule 8)
- Estado inicial (phase vazia) = CREATING (não precisa de transição, é o init)

#### Tasks
1. Criar helper functions `PreviewPhaseToState(phase)` e `PreviewStateToPhase(state)` em `preview_types.go`
2. Substituir `var _ = statemachine.NewPreviewMachine` por `var previewSM = statemachine.NewPreviewMachine()` no controller
3. Substituir cada atribuição direta `preview.Status.Phase = X` por chamada a `previewSM.Transition()` + conversão
4. Manter init de phase vazia → Creating como caso especial (não é transição, é estado inicial)
5. Adicionar testes para as funções helper de conversão
6. Rodar testes existentes do state machine para confirmar compatibilidade

#### TDD
```
RED:     TestPreviewPhaseToState() — verifica conversão de cada CRD phase para SM state
RED:     TestPreviewStateToPhase() — verifica conversão de cada SM state para CRD phase
RED:     TestPreviewPhaseToState_InvalidPhase() — phase desconhecida retorna erro
RED:     TestPreviewStateToPhase_InvalidState() — state desconhecido retorna erro
GREEN:   Implementar PhaseToState() e StateToPhase() em preview_types.go
REFACTOR: Verificar que mapa cobre todos os 7 estados
VERIFY:  cd operators && go test ./api/v1/... && go test ./internal/controller/...
```

#### Acceptance Criteria
- [ ] Zero atribuições diretas de `preview.Status.Phase` no controller (exceto init)
- [ ] Todas as transições passam por `previewSM.Transition()`
- [ ] Helper functions com 100% de cobertura nos 7 estados
- [ ] Testes existentes do state machine continuam passando
- [ ] Pass: /code-audit complexity check (cyclomatic complexity <= 10)
- [ ] Pass: /code-audit lint check (zero warnings)

#### DoD
- [ ] Todas as tasks completadas
- [ ] `cd operators && go test ./...` green
- [ ] `cd pkg && go test ./...` green
- [ ] Zero atribuições diretas de phase no controller

---

### T0.2 — Corrigir naming do namespace no controller

#### Objective
O controller usa `fmt.Sprintf("preview-%s-pr%d", preview.Spec.TenantID, preview.Spec.PRNumber)` (line 101, 191). Isso ignora o `ProjectID` e não usa `pkg/naming.PreviewNamespace()` que já existe e gera o formato correto `{tenantSlug}-{projectSlug}-pr-{prNumber}`.

#### Evidence
- `preview_controller.go:101` — `nsName := fmt.Sprintf("preview-%s-pr%d", preview.Spec.TenantID, preview.Spec.PRNumber)`
- `preview_controller.go:191` — Mesmo formato na deletion
- `pkg/naming/naming.go:109-124` — `PreviewNamespace()` já existe com formato correto e validação RFC 1123

#### Files to edit
```
operators/internal/controller/preview_controller.go — Substituir fmt.Sprintf por naming.PreviewNamespace()
```

#### Deep file dependency analysis

**`preview_controller.go`:**
- Hoje: gera namespace name inline via Sprintf, sem ProjectID
- Mudança: importar `pkg/naming` e usar `naming.PreviewNamespace(tenantSlug, projectSlug, prNumber)`
- Impacto: namespace names mudam de formato — mas como controller nunca foi registrado, não há dados legados

#### Deep Dives

**Formato atual vs. correto:**
```
Atual:   preview-{tenantID}-pr{prNumber}        → preview-acme-pr42
Correto: {tenantSlug}-{projectSlug}-pr-{prNumber} → acme-api-pr-42
```

O formato correto inclui `projectSlug` o que permite múltiplos previews do mesmo PR em projetos diferentes (monorepo). Também é consistente com o padrão de naming do LoopZero.

**Invariantes:**
- `naming.PreviewNamespace()` valida RFC 1123 compliance
- Retorna error se inputs forem vazios ou prNumber <= 0
- Controller deve tratar o error (não panic — é input validation)

#### Tasks
1. Importar `github.com/loopzero/loopzero/pkg/naming` no controller
2. Substituir ambas as ocorrências de `fmt.Sprintf` por `naming.PreviewNamespace()`
3. Tratar error retornado por `naming.PreviewNamespace()` (set condition + requeue)
4. Extrair namespace name para método helper para evitar duplicação entre reconcileCreating e reconcileDelete

#### TDD
```
RED:     TestPreviewController_UsesNamingPackage() — verifica que namespace criado segue formato naming.PreviewNamespace
RED:     TestPreviewController_InvalidNaming() — verifica que inputs inválidos geram condition de erro
GREEN:   Substituir Sprintf por naming.PreviewNamespace()
REFACTOR: Extrair helper method previewNamespace()
VERIFY:  cd operators && go test ./internal/controller/...
```

#### Acceptance Criteria
- [ ] Zero ocorrências de `fmt.Sprintf("preview-` no controller
- [ ] Namespace name inclui projectSlug
- [ ] Error de naming tratado com condition + requeue
- [ ] Pass: /code-audit lint check

#### DoD
- [ ] Todas as tasks completadas
- [ ] `cd operators && go test ./...` green
- [ ] Namespace format validado

---

### T0.3 — Registrar PreviewController no manager

#### Objective
`SetupPreviewController()` existe mas nunca é chamado em `operators/cmd/main.go:setupControllers()`. Registrar o controller para que reconciliações aconteçam.

#### Evidence
- `operators/cmd/main.go:280-342` — `setupControllers()` registra 7 controllers, PreviewController ausente
- `operators/internal/controller/preview_controller.go:214-224` — `SetupPreviewController()` existe e espera `(mgr, runtimeClient, baseDomain)`
- `operators/cmd/main.go:317-321` — `baseDomain` já é extraído de env var e usado por ApplicationController

#### Files to edit
```
operators/cmd/main.go — Adicionar chamada a SetupPreviewController() na função setupControllers()
```

#### Deep file dependency analysis

**`operators/cmd/main.go`:**
- Hoje: registra 7 controllers (Tenant, ClusterCredential, Environment, Application, Exposure, GitRepositoryBinding, PlatformBootstrap)
- Mudança: adicionar `SetupPreviewController(mgr, deps.runtimeClient, baseDomain)` após EnvironmentController (line 315)
- Dependências: `baseDomain` (já extraído line 317), `deps.runtimeClient` (já disponível)
- Downstream: controller começa a reconciliar PreviewEnvironment CRs — sem CRs existentes, não faz nada

#### Deep Dives

**Posição no setup:**
Registrar após `SetupEnvironmentController` e antes de `SetupApplicationController` — mesma sequência lógica (environment → preview → application → exposure).

**Variáveis necessárias:**
- `mgr` — já disponível (parâmetro da função)
- `deps.runtimeClient` — já disponível (construído no init)
- `baseDomain` — já extraído de `BASE_DOMAIN` env var (line 317)

**Risco:**
- Nenhum — sem PreviewEnvironment CRs, controller não faz nada
- Se `BASE_DOMAIN` estiver vazio, o setup já falha (line 318-320)

#### Tasks
1. Adicionar chamada `SetupPreviewController(mgr, deps.runtimeClient, baseDomain)` após line 315
2. Seguir padrão de error handling dos controllers existentes (log + os.Exit(1))

#### TDD
```
RED:     TestSetupControllers_RegistersPreview() — verificar que PreviewController é registrado (mock manager)
GREEN:   Adicionar chamada no setupControllers()
REFACTOR: Nenhum esperado
VERIFY:  cd operators && go build ./cmd/... && go test ./...
```

#### Acceptance Criteria
- [ ] `SetupPreviewController` chamado em `setupControllers()`
- [ ] Segue padrão de error handling dos controllers vizinhos
- [ ] Operator compila sem erros
- [ ] Testes existentes não quebram

#### DoD
- [ ] Task completada
- [ ] `cd operators && go build ./cmd/...` green
- [ ] `cd operators && go test ./...` green

---

## Phase 1: Controller — Workload Deployment

**Objective:** Completar o controller para que, além de criar namespace, ele também inicie build e monitore deployment do workload no preview environment.

### T1.1 — Implementar endpoint interno de build para preview

#### Objective
Criar endpoint `POST /api/v1/internal/previews/build` que a API usa internamente (chamado pelo webhook handler de PR) para submeter um build targeting um preview environment. Reutiliza `BuildOrchestrator.SubmitBuild` com parâmetros adaptados para preview.

#### Evidence
- `api/internal/services/build_orchestrator_submit.go:41` — `SubmitBuild()` é o ponto de entrada de builds
- `api/internal/github/autodeploy.go:86` — `HandlePush()` submete builds para auto-deploy (padrão a seguir)
- ADR D4 — controller delega build ao API, não submete diretamente

#### Files to edit
```
api/internal/services/preview_service.go (NEW) — PreviewService que orquestra criação/deleção de previews
api/internal/routes/preview.go — Adicionar endpoints internos (Create preview, trigger build)
api/internal/server/config.go — Adicionar PreviewService ao Config struct (verificar nome correto do arquivo)
```

#### Deep file dependency analysis

**`preview_service.go` (NEW):**
- Novo serviço que encapsula lógica de preview: criar CR, submeter build, atualizar DB
- Depende de: BuildOrchestrator (build submission), k8s client (CR creation), PreviewStore (DB)
- Downstream: chamado por PreviewHandler (API) e webhook handler (GitHub PR events)

**`routes/preview.go`:**
- Hoje: List, Get, Delete apenas
- Mudança: adicionar `Create` handler que recebe PR data e dispara criação
- Downstream: chamado pelo GitHub PR webhook handler

#### Deep Dives

**PreviewService interface:**
```go
type PreviewService interface {
    CreatePreview(ctx context.Context, req CreatePreviewRequest) (*domain.Preview, error)
    TriggerBuild(ctx context.Context, previewID string) error
    DeletePreview(ctx context.Context, tenantID, previewID string) error
    OnBuildCompleted(ctx context.Context, previewID, buildID string) error
}

type CreatePreviewRequest struct {
    TenantID       string
    ProjectName    string
    PRNumber       int
    Branch         string
    CommitSHA      string
    InstallationID int64
    RepoFullName   string
    TTL            time.Duration // default 72h
}
```

**Fluxo CreatePreview:**
1. Validar inputs
2. Verificar se preview para este PR já existe (upsert logic)
3. Criar/atualizar row na tabela `preview_environments` (status=CREATING)
4. Criar/atualizar `PreviewEnvironment` CR no mgmt cluster
5. Submeter build via `BuildOrchestrator.SubmitBuild` com environment="preview-{pr}"
6. Retornar preview com URL provisória

**Invariante:** Um PR pode ter no máximo um preview ativo por projeto. Push subsequente atualiza o preview existente (novo build, mesma URL).

#### Tasks
1. Definir `PreviewService` interface e `CreatePreviewRequest` struct
2. Implementar `CreatePreview`: validação → DB row → CR creation → build submission
3. Implementar `TriggerBuild`: reusar SubmitBuild com namespace targeting
4. Implementar `DeletePreview`: set CR deletionTimestamp → controller handles cleanup
5. Implementar `OnBuildCompleted`: atualizar DB status + CR status
6. Adicionar endpoint `POST /api/v1/previews` no handler

#### TDD
```
RED:     TestPreviewService_CreatePreview_HappyPath() — cria row + CR + submete build
RED:     TestPreviewService_CreatePreview_AlreadyExists() — atualiza existing preview (novo commit)
RED:     TestPreviewService_CreatePreview_InvalidInputs() — rejeita tenant vazio, PR <= 0
RED:     TestPreviewService_DeletePreview_HappyPath() — marca para deleção
RED:     TestPreviewService_DeletePreview_NotFound() — retorna typed error
RED:     TestPreviewService_OnBuildCompleted() — atualiza status no DB
GREEN:   Implementar PreviewService
REFACTOR: Extrair validação para método separado
VERIFY:  cd api && go test ./internal/services/...
```

#### Acceptance Criteria
- [ ] PreviewService implementa Create, TriggerBuild, Delete, OnBuildCompleted
- [ ] Reutiliza BuildOrchestrator.SubmitBuild (DRY)
- [ ] Upsert: push subsequente atualiza preview existente
- [ ] Typed errors para casos de falha
- [ ] Pass: /code-audit complexity check (cyclomatic complexity <= 10)
- [ ] Pass: /code-audit coverage check (coverage >= 90%)

#### DoD
- [ ] Todas as tasks completadas
- [ ] `cd api && go test ./...` green
- [ ] Todos os testes passando

---

### T1.2 — Completar reconcileCreating no controller com workload deployment

#### Objective
Após criar o namespace, o controller deve gerar manifests, fazer push ao Gitea, e criar ArgoCD Application CR para que o workload seja deployado.

#### Evidence
- `preview_controller.go:99-146` — reconcileCreating cria namespace e transiciona para Active, mas não deploya workload
- `api/internal/manifests/renderer.go:100` — `Render()` gera manifests YAML
- `api/internal/gitops/gitea_pusher.go:70` — `Push()` escreve manifests no Gitea
- ADR D4 — Controller delega build ao API

#### Files to edit
```
operators/internal/controller/preview_controller.go — Expandir reconcileCreating para aguardar build completion
operators/internal/controller/preview_conditions.go (NEW) — Condition types específicos de preview
```

#### Deep file dependency analysis

**`preview_controller.go`:**
- Hoje: reconcileCreating cria namespace → Active (sem workload)
- Mudança: reconcileCreating cria namespace → conditions. Transição para Active só após build completed + ArgoCD synced
- Nova sequência: CREATING (namespace ready) → espera build → espera sync → ACTIVE

**Impacto no state machine:**
O SM atual tem `CREATING → CreationCompleted → ACTIVE`. O controller precisa requeue durante CREATING enquanto build está em progresso, e só emitir `CreationCompleted` quando workload estiver healthy.

#### Deep Dives

**Novo fluxo de reconcileCreating:**
```
1. Criar namespace (já existe)
2. Aplicar PSA, NetworkPolicy, ResourceQuota, LimitRange
3. Verificar condition "BuildSubmitted"
   - Se não: build ainda não foi submetido — requeue (API submete via webhook handler)
   - Se sim: verificar condition "BuildCompleted"
4. Verificar condition "BuildCompleted"
   - Se não: build em progresso — requeue 30s
   - Se sim: verificar condition "WorkloadHealthy"
5. Verificar condition "WorkloadHealthy"
   - Se não: ArgoCD sync em progresso — requeue 10s
   - Se sim: Transition(CREATING, CreationCompleted) → ACTIVE
```

**Conditions definidos:**
- `NamespaceReady` — namespace exists on runtime cluster
- `NetworkPolicyApplied` — deny-all + allow-ingress + allow-egress applied
- `ResourceQuotaApplied` — quota + limit range applied
- `BuildSubmitted` — build was triggered (set by API callback)
- `BuildCompleted` — build succeeded (set by API callback)
- `WorkloadHealthy` — ArgoCD reports healthy (set by controller polling or callback)

O controller observa conditions setadas externamente (pela API via status update) e reage. Isso mantém a separação: API orquestra builds, controller gerencia lifecycle.

#### Tasks
1. Definir condition types em preview_conditions.go
2. Expandir reconcileCreating para aplicar NetworkPolicy + ResourceQuota + LimitRange (reusar de EnvironmentReconciler)
3. Adicionar lógica de espera por conditions (BuildSubmitted → BuildCompleted → WorkloadHealthy)
4. Transicionar para ACTIVE somente após WorkloadHealthy=True
5. Transicionar para FAILED se BuildCompleted=False (build falhou)
6. Implementar reconcileActive atualizado com requeue para TTL

#### TDD
```
RED:     TestReconcileCreating_CreatesNamespaceWithNetworkPolicy() — verifica NP criada
RED:     TestReconcileCreating_CreatesResourceQuota() — verifica quota criada
RED:     TestReconcileCreating_WaitsBuildSubmitted() — requeue se condition ausente
RED:     TestReconcileCreating_WaitsBuildCompleted() — requeue se build em progresso
RED:     TestReconcileCreating_TransitionsToActive() — Active quando WorkloadHealthy=True
RED:     TestReconcileCreating_TransitionsToFailed() — Failed quando BuildCompleted=False
GREEN:   Implementar lógica de conditions no controller
REFACTOR: Extrair condition checks para helpers
VERIFY:  cd operators && go test ./internal/controller/... -run Preview
```

#### Acceptance Criteria
- [ ] Namespace criado com NetworkPolicy, ResourceQuota, LimitRange
- [ ] Controller espera conditions antes de transicionar
- [ ] Transição para Active usa state machine (não atribuição direta)
- [ ] Transição para Failed em caso de build failure
- [ ] Envtest specs cobrem happy path e failure path
- [ ] Pass: /code-audit complexity check (cyclomatic complexity <= 10)

#### DoD
- [ ] Todas as tasks completadas
- [ ] `cd operators && go test ./...` green
- [ ] Envtest specs passando

---

### T1.3 — Implementar reconcileDelete completo (Application CR + GitOps cleanup)

#### Objective
Expandir a deleção do preview para limpar não só o namespace mas também Application CR no ArgoCD e path no Gitea.

#### Evidence
- `preview_controller.go:183-211` — reconcileDelete só deleta namespace
- Deploy flow: cada release cria Application CR + path no Gitea que precisam ser limpos
- "gitops.Remove failure is FATAL" — CLAUDE.md Known Systemic Patterns

#### Files to edit
```
operators/internal/controller/preview_controller.go — Expandir reconcileDelete
```

#### Deep file dependency analysis

**`preview_controller.go`:**
- Hoje: reconcileDelete deleta namespace, remove finalizer
- Mudança: adicionar cleanup de Application CR e Gitea path
- O controller precisa de ArgoCD client e Gitea client, ou delegar cleanup para a API

**Decisão: delegar cleanup para API** — por consistência com D4, controller não deve ter acesso direto a Gitea/ArgoCD. Controller seta condition `CleanupRequested=True`, API observa e faz cleanup.

Alternativa mais simples: controller deleta o namespace (que contém todos os workload resources) e a Application CR no mgmt cluster (que ele já tem acesso). Gitea path é garbage collected quando o preview row é deletado (API responsibility).

#### Deep Dives

**Sequência de cleanup:**
```
1. Deletar Application CR no mgmt cluster (ArgoCD para de syncar)
2. Deletar namespace no runtime cluster (pods, services, etc. morrem)
3. API limpa path no Gitea (eventual, não bloqueante)
4. API atualiza row no DB (status=DELETED)
5. Controller remove finalizer
```

Steps 1-2 são bloqueantes (controller). Steps 3-4 são eventual (API).

**Invariantes:**
- Application CR DEVE ser deletada antes do namespace (ArgoCD pode recriar resources)
- Se cleanup falha: transicionar para CLEANUP_FAILED, não ficar em loop infinito
- Após max retries: emitir GarbageCollected event e forçar DELETED

#### Tasks
1. Extrair nome da Application CR do preview (convention: `preview-{tenant}-{project}-pr-{number}`)
2. Na reconcileDelete: deletar Application CR primeiro, depois namespace
3. Adicionar retry com backoff para cleanup (max 3 tentativas)
4. Se cleanup falha após retries: transicionar para CLEANUP_FAILED
5. Adicionar condition `ApplicationDeleted` e `NamespaceDeleted`

#### TDD
```
RED:     TestReconcileDelete_DeletesApplicationCR() — verifica Application CR deletada
RED:     TestReconcileDelete_DeletesNamespace() — verifica namespace deletada (já existe parcialmente)
RED:     TestReconcileDelete_OrderMatters() — Application CR deletada ANTES do namespace
RED:     TestReconcileDelete_CleanupFailed() — transiciona para CLEANUP_FAILED após max retries
RED:     TestReconcileDelete_RemovesFinalizer() — finalizer removido após cleanup completo
GREEN:   Implementar cleanup expandido
REFACTOR: Nenhum esperado
VERIFY:  cd operators && go test ./internal/controller/... -run Preview
```

#### Acceptance Criteria
- [ ] Application CR deletada antes do namespace
- [ ] Retry com backoff (max 3 tentativas)
- [ ] CLEANUP_FAILED state usado quando cleanup falha
- [ ] Finalizer removido somente após cleanup completo
- [ ] Envtest specs cobrem order-of-operations e failure path

#### DoD
- [ ] Todas as tasks completadas
- [ ] `cd operators && go test ./...` green
- [ ] Envtest specs passando

---

## Phase 2: API + Database

**Objective:** Criar tabela de previews no PostgreSQL, implementar store, e wirar routes no router.

### T2.1 — Criar migration para tabela preview_environments

#### Objective
Criar tabela PostgreSQL para persistir metadata de preview environments. Segue padrão das tabelas existentes (RLS, triggers, tipos).

#### Evidence
- `api/internal/db/migrations/000001_initial_schema.up.sql` — 26 tabelas, nenhuma de preview
- `api/internal/routes/preview.go:18-22` — `PreviewStore` interface define ListByTenant, GetByID, RequestDeletion
- ADR D7 — dual-write CR + DB

#### Files to edit
```
api/internal/db/migrations/000003_preview_environments.up.sql (NEW) — Migration forward-only
```

#### Deep file dependency analysis

**Nova migration:**
- Próximo número sequencial: 000003
- Deve seguir padrões: RLS, trigger de audit, índices para queries frequentes
- Downstream: `PostgresPreviewStore` depende desta tabela

#### Deep Dives

**Schema:**
```sql
CREATE TABLE preview_environments (
    id          TEXT PRIMARY KEY DEFAULT 'prev-' || substr(gen_random_uuid()::text, 1, 8),
    tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    project_id  TEXT NOT NULL,  -- project name (not UUID, per Known Patterns)
    pr_number   INTEGER NOT NULL CHECK (pr_number > 0),
    branch      TEXT NOT NULL,
    commit_sha  TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'creating'
                CHECK (status IN ('creating','active','expiring','deleting','deleted','failed','cleanup_failed')),
    url         TEXT,
    build_id    TEXT REFERENCES builds(id),
    release_id  TEXT REFERENCES releases(id),
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ,  -- soft delete for history

    UNIQUE (tenant_id, project_id, pr_number)  -- one preview per PR per project
);

-- RLS
ALTER TABLE preview_environments ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON preview_environments
    USING (tenant_id = current_setting('app.tenant_id', true));

-- Indexes
CREATE INDEX idx_preview_tenant ON preview_environments(tenant_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_preview_status ON preview_environments(status) WHERE status NOT IN ('deleted');

-- Audit trigger (reuse existing)
CREATE TRIGGER preview_audit
    AFTER INSERT OR UPDATE OR DELETE ON preview_environments
    FOR EACH ROW EXECUTE FUNCTION audit_sensitive_mutation();
```

**Invariantes:**
- `UNIQUE (tenant_id, project_id, pr_number)` garante um preview por PR por projeto
- RLS garante tenant isolation
- `status` CHECK constraint alinhado com state machine states (lowercase)
- Forward-only migration (no DOWN file, per LoopZero conventions)

#### Tasks
1. Criar arquivo `000003_preview_environments.up.sql`
2. Definir tabela com campos, constraints, RLS
3. Adicionar índices para queries frequentes
4. Adicionar trigger de audit
5. Validar migration executando localmente

#### TDD
```
RED:     TestMigration_PreviewEnvironments_CreatesTable() — tabela existe após migration
RED:     TestMigration_PreviewEnvironments_RLS() — queries sem set_config retornam vazio
RED:     TestMigration_PreviewEnvironments_UniqueConstraint() — duplicate (tenant, project, pr) rejeita
GREEN:   Escrever migration SQL
REFACTOR: Nenhum esperado
VERIFY:  task db:migrate && task test:unit
```

#### Acceptance Criteria
- [ ] Tabela criada com todos os campos
- [ ] RLS habilitado com policy tenant_isolation
- [ ] UNIQUE constraint em (tenant_id, project_id, pr_number)
- [ ] Audit trigger wired
- [ ] Migration forward-only (sem DOWN)
- [ ] `task db:migrate` executa sem erros

#### DoD
- [ ] Migration aplicada com sucesso
- [ ] Constraints validadas
- [ ] `task db:migrate` green

---

### T2.2 — Implementar PostgresPreviewStore

#### Objective
Implementar a interface `PreviewStore` com PostgreSQL via pgx, seguindo o padrão de stores existentes.

#### Evidence
- `api/internal/routes/preview.go:18-22` — interface: ListByTenant, GetByID, RequestDeletion
- `api/internal/db/environment_target_store.go` — padrão a seguir (ExecQueryExecutor + QueryExecutor)
- ADR D7 — DB para queries da API

#### Files to edit
```
api/internal/db/preview_store.go (NEW) — Implementação PostgresPreviewStore
api/internal/domain/types.go — Expandir Preview struct se necessário
```

#### Deep file dependency analysis

**`preview_store.go` (NEW):**
- Implementa `routes.PreviewStore` interface
- Usa `ExecQueryExecutor` (writes) + `QueryExecutor` (reads) — padrão pgx
- Downstream: usado pelo `PreviewHandler` (routes) e `PreviewService` (services)

**`domain/types.go`:**
- Hoje: `Preview` struct com campos básicos (lines 361-372)
- Pode precisar de campos adicionais: `CommitSHA`, `BuildID`, `ReleaseID`, `DeletedAt`

#### Deep Dives

**Interface expandida para PreviewService:**
```go
type PreviewStore interface {
    // API routes (read-only)
    ListByTenant(tenantID string) ([]domain.Preview, error)
    GetByID(tenantID, previewID string) (*domain.Preview, error)
    
    // PreviewService (writes)
    Create(ctx context.Context, tx DBTX, preview *domain.Preview) error
    UpdateStatus(ctx context.Context, tx DBTX, id, status string) error
    UpdateBuild(ctx context.Context, tx DBTX, id, buildID, commitSHA string) error
    GetByPR(ctx context.Context, tenantID, projectID string, prNumber int) (*domain.Preview, error)
    RequestDeletion(tenantID, previewID string) error
}
```

**Padrão UPSERT para CreateOrUpdate (push subsequente):**
```sql
INSERT INTO preview_environments (tenant_id, project_id, pr_number, branch, commit_sha, status, url, expires_at)
VALUES ($1, $2, $3, $4, $5, 'creating', $6, $7)
ON CONFLICT (tenant_id, project_id, pr_number) DO UPDATE SET
    commit_sha = EXCLUDED.commit_sha,
    status = 'creating',
    build_id = NULL,
    updated_at = now()
RETURNING id;
```

#### Tasks
1. Expandir `domain.Preview` com campos: CommitSHA, BuildID, ReleaseID, DeletedAt
2. Definir interface expandida `PreviewStore` em arquivo separado ou expandir existente
3. Implementar `PostgresPreviewStore` com todos os métodos
4. Usar UPSERT para Create (handle push subsequente ao mesmo PR)
5. ListByTenant filtra `deleted_at IS NULL`
6. RequestDeletion faz soft-delete (set status='deleting')

#### TDD
```
RED:     TestPostgresPreviewStore_Create() — cria row, retorna ID gerado
RED:     TestPostgresPreviewStore_Create_Upsert() — segundo Create para mesmo PR atualiza
RED:     TestPostgresPreviewStore_ListByTenant() — retorna apenas previews do tenant, exclui deletados
RED:     TestPostgresPreviewStore_GetByID() — retorna preview ou erro
RED:     TestPostgresPreviewStore_GetByID_WrongTenant() — não retorna preview de outro tenant
RED:     TestPostgresPreviewStore_GetByPR() — busca por (tenant, project, pr_number)
RED:     TestPostgresPreviewStore_UpdateStatus() — atualiza status
RED:     TestPostgresPreviewStore_RequestDeletion() — marca como deleting
GREEN:   Implementar todos os métodos
REFACTOR: Extrair scan helper para reusar entre List e Get
VERIFY:  cd api && go test ./internal/db/... -run Preview
```

#### Acceptance Criteria
- [ ] Todos os métodos da interface implementados
- [ ] UPSERT para handle push subsequente
- [ ] RLS enforced (queries dependem de app.tenant_id)
- [ ] Soft-delete (deleted_at, não DELETE FROM)
- [ ] Segue padrão ExecQueryExecutor/QueryExecutor
- [ ] Pass: /code-audit coverage check (coverage >= 90%)

#### DoD
- [ ] Todas as tasks completadas
- [ ] `cd api && go test ./...` green
- [ ] Testes contra DB real (não mocks)

---

### T2.3 — Wirar PreviewHandler no router e Config

#### Objective
Registrar as routes de preview no router da API e adicionar dependências ao Config struct.

#### Evidence
- `api/internal/server/router.go:158-310` — `registerProtectedRoutes` sem preview routes
- `api/internal/routes/preview.go` — handler pronto mas não registrado
- Padrão: Config struct → router registration → handler instantiation

#### Files to edit
```
api/internal/server/router.go — Registrar preview routes no registerProtectedRoutes
api/internal/server/server.go — Adicionar PreviewStore e PreviewHandler ao Config (ou onde Config é definido)
```

#### Deep file dependency analysis

**`router.go`:**
- Hoje: 15 route groups registrados, preview ausente
- Mudança: adicionar bloco `if cfg.PreviewHandler != nil { ... }` após Environment routes (line 284)
- Padrão: idêntico aos outros blocos (nil check → register GET/POST/DELETE)

**`server.go` (Config):**
- Hoje: Config struct com ~30 fields para handlers e stores
- Mudança: adicionar `PreviewHandler *routes.PreviewHandler`
- Downstream: server initialization precisa criar PreviewHandler com PreviewStore

#### Deep Dives

**Route registration:**
```go
// Preview environment routes
if cfg.PreviewHandler != nil {
    r.Get("/api/v1/previews", cfg.PreviewHandler.List)
    r.Get("/api/v1/previews/{id}", cfg.PreviewHandler.Get)
    r.With(rateLimitMiddlewares...).Delete("/api/v1/previews/{id}", cfg.PreviewHandler.Delete)
    r.With(rateLimitMiddlewares...).Post("/api/v1/previews", cfg.PreviewHandler.Create)
}
```

**Invariantes:**
- POST usa rate limiting (cria resources)
- DELETE usa rate limiting (mutation)
- GET não precisa de rate limiting
- Todas as routes estão sob auth middleware (parent group)

#### Tasks
1. Adicionar `PreviewHandler *routes.PreviewHandler` ao Config struct
2. Registrar routes em `registerProtectedRoutes` após Environment routes
3. Instanciar PreviewHandler na inicialização do server com PreviewStore
4. Verificar que nil-check segue padrão existente

#### TDD
```
RED:     TestRouter_PreviewRoutes_Registered() — GET/DELETE /api/v1/previews routes respondem
RED:     TestRouter_PreviewRoutes_RequireAuth() — 401 sem token
RED:     TestRouter_PreviewRoutes_NilHandler_NoRoutes() — sem handler, routes não existem
GREEN:   Registrar routes no router
REFACTOR: Nenhum esperado
VERIFY:  cd api && go test ./internal/server/... -run Preview
```

#### Acceptance Criteria
- [ ] Routes registradas com padrão idêntico aos existentes
- [ ] Nil-check para backwards compatibility
- [ ] Rate limiting em POST e DELETE
- [ ] Auth middleware aplicado (parent group)
- [ ] Pass: /code-audit lint check

#### DoD
- [ ] Todas as tasks completadas
- [ ] `cd api && go test ./...` green
- [ ] Routes acessíveis com auth token válido

---

## Phase 3: GitHub Integration

**Objective:** Conectar webhook de PR do GitHub ao fluxo de criação/atualização/deleção de preview environments.

### T3.1 — Implementar PR webhook handler

#### Objective
Implementar handler para `pull_request` events do GitHub App que cria/atualiza/deleta preview environments baseado na ação do PR.

#### Evidence
- `api/internal/github/webhook/handler.go:72-78` — `OnPullRequest(fn PullRequestHandler)` slot existe
- `api/internal/github/webhook/events.go:41-50` — `PullRequestEvent` struct completo
- `api/internal/github/autodeploy.go` — HandlePush como padrão a seguir

#### Files to edit
```
api/internal/github/preview_deploy.go (NEW) — PreviewDeployer handler para PR events
```

#### Deep file dependency analysis

**`preview_deploy.go` (NEW):**
- Handler para `PullRequestEvent` 
- Mapeia ações: `opened`/`synchronize` → create/update preview, `closed` → delete preview
- Depende de: PreviewService, repository store (para lookup tenant/project)
- Registrado via `webhookHandler.OnPullRequest(previewDeployer.HandlePR)`

#### Deep Dives

**Mapeamento de ações:**
```
PR opened      → CreatePreview (novo preview)
PR synchronize → CreatePreview (upsert — novo commit, mesmo PR)
PR reopened    → CreatePreview (recriar preview)
PR closed      → DeletePreview (cleanup)
```

**HandlePR flow:**
```go
func (d *PreviewDeployer) HandlePR(event webhook.PullRequestEvent) error {
    // 1. Lookup repository → tenant + project
    repo, err := d.repoStore.GetByFullName(event.Repository.FullName)
    
    // 2. Check auto-deploy enabled
    if !repo.AutoDeploy { return nil }
    
    // 3. Dispatch by action
    switch event.Action {
    case "opened", "synchronize", "reopened":
        return d.previewService.CreatePreview(ctx, CreatePreviewRequest{
            TenantID:       repo.TenantID,
            ProjectName:    repo.ProjectName,
            PRNumber:       event.Number,
            Branch:         event.PullRequest.Head.Ref,
            CommitSHA:      event.PullRequest.Head.SHA,
            InstallationID: event.Installation.ID,
            RepoFullName:   event.Repository.FullName,
        })
    case "closed":
        return d.previewService.DeletePreviewByPR(ctx, repo.TenantID, repo.ProjectName, event.Number)
    }
    return nil
}
```

**Invariantes:**
- Só processa PRs para repos com `auto_deploy=true`
- `synchronize` = push ao branch do PR — atualiza preview existente
- `closed` (merged ou não) = cleanup
- Handler roda async (goroutine do webhook dispatcher) — errors são logados, não retornados ao GitHub

#### Tasks
1. Criar `PreviewDeployer` struct com dependências (PreviewService, RepositoryStore, logger)
2. Implementar `HandlePR` com dispatch por action
3. Lookup repo → tenant/project mapping
4. Guard: só processar se auto_deploy=true
5. Registrar handler no webhook handler: `webhookHandler.OnPullRequest(previewDeployer.HandlePR)`

#### TDD
```
RED:     TestPreviewDeployer_HandlePR_Opened() — cria preview para PR opened
RED:     TestPreviewDeployer_HandlePR_Synchronize() — atualiza preview com novo commit
RED:     TestPreviewDeployer_HandlePR_Closed() — deleta preview
RED:     TestPreviewDeployer_HandlePR_ClosedMerged() — deleta preview (merged=true)
RED:     TestPreviewDeployer_HandlePR_AutoDeployDisabled() — ignora se auto_deploy=false
RED:     TestPreviewDeployer_HandlePR_UnknownRepo() — ignora se repo não conectado
RED:     TestPreviewDeployer_HandlePR_UnknownAction() — ignora ações irrelevantes (labeled, etc)
GREEN:   Implementar PreviewDeployer
REFACTOR: Nenhum esperado
VERIFY:  cd api && go test ./internal/github/... -run Preview
```

#### Acceptance Criteria
- [ ] PR opened/synchronize/reopened → CreatePreview
- [ ] PR closed → DeletePreview
- [ ] Auto-deploy guard (só repos com auto_deploy=true)
- [ ] Handler async, errors logados
- [ ] Zero internal terms em logs user-facing
- [ ] Pass: /code-audit coverage check (coverage >= 90%)

#### DoD
- [ ] Todas as tasks completadas
- [ ] `cd api && go test ./...` green
- [ ] Handler registrado no webhook dispatcher

---

### T3.2 — Implementar GitHub comment com URL do preview

#### Objective
Após preview ficar ACTIVE, postar comment no PR do GitHub com a URL do preview. Atualizar (não duplicar) o comment em pushes subsequentes.

#### Evidence
- ADR D5 — GitHub comment com URL
- GitHub App `LoopZero Platform` (ID 3319955) tem permissões de PR write
- Vercel e Netlify fazem isso — é a experiência esperada

#### Files to edit
```
api/internal/github/comment.go (NEW) — GitHub PR comment client
api/internal/services/preview_service.go — Chamar comment após preview ACTIVE
```

#### Deep file dependency analysis

**`comment.go` (NEW):**
- Cliente para GitHub API: create/update issue comments
- Usa installation token (já existe no autodeploy flow)
- Identifica comment anterior por marker text (e.g., `<!-- loopzero-preview -->`)

**`preview_service.go`:**
- `OnBuildCompleted` já atualiza status para ACTIVE
- Adicionar chamada ao comment client após status=ACTIVE

#### Deep Dives

**Comment format:**
```markdown
<!-- loopzero-preview:project-name:42 -->
### Preview Environment

| | |
|---|---|
| **Status** | ✅ Ready |
| **URL** | https://pr42-myapp.preview.loopzero.app |
| **Commit** | abc1234 |
| **Updated** | 2 minutes ago |

---
*Deployed by [LoopZero](https://loopzero.com)*
```

**Upsert logic:**
1. List comments on PR
2. Find comment with marker `<!-- loopzero-preview:project:pr -->`
3. Se existe: update comment body
4. Se não: create new comment

**Invariantes:**
- Um comment por projeto por PR (marker garante)
- Comment atualizado (não duplicado) em cada push
- Se GitHub API falha: log error, não falhar o preview (comment é nice-to-have)

#### Tasks
1. Implementar `GitHubCommentClient` com Create/Update usando installation tokens
2. Implementar marker-based upsert logic
3. Definir template do comment (markdown)
4. Chamar comment client no `OnBuildCompleted` após status=ACTIVE
5. Handle errors gracefully (log, don't fail)

#### TDD
```
RED:     TestGitHubComment_CreateNew() — cria comment quando nenhum existe
RED:     TestGitHubComment_UpdateExisting() — atualiza comment com marker
RED:     TestGitHubComment_MarkerIdentification() — encontra comment correto entre vários
RED:     TestGitHubComment_APIFailure_DoesNotFail() — error logado, preview não afetado
GREEN:   Implementar GitHubCommentClient
REFACTOR: Extrair template para constante
VERIFY:  cd api && go test ./internal/github/... -run Comment
```

#### Acceptance Criteria
- [ ] Comment criado no PR com URL do preview
- [ ] Comment atualizado (não duplicado) em push subsequente
- [ ] Marker-based identification
- [ ] Failure não afeta preview lifecycle
- [ ] Nenhum internal term no comment

#### DoD
- [ ] Todas as tasks completadas
- [ ] `cd api && go test ./...` green
- [ ] Comment template validado

---

## Phase 4: Wiring + E2E

**Objective:** Conectar todas as camadas, atualizar CLI para remover `PREVIEW_NOT_AVAILABLE`, e validar E2E.

### T4.1 — Remover PREVIEW_NOT_AVAILABLE do CLI e wirar contra API real

#### Objective
O CLI hoje retorna `PREVIEW_NOT_AVAILABLE` para todos os commands de preview. Com a API funcional, remover essa guard e apontar para endpoints reais.

#### Evidence
- `cli/src/commands/preview.ts` — handlers verificam se API retorna 404 com code=PREVIEW_NOT_AVAILABLE
- `cli/tests/unit/commands/preview.test.ts` — testes cobrem o caso PREVIEW_NOT_AVAILABLE
- `docs/cli-reference.md` — marca preview como "Sprint 5 controller não implementado"

#### Files to edit
```
cli/src/commands/preview.ts — Remover guard PREVIEW_NOT_AVAILABLE (feature agora existe)
cli/src/register/preview.ts — Verificar URLs de API match com routes reais
cli/tests/unit/commands/preview.test.ts — Atualizar testes (remover PREVIEW_NOT_AVAILABLE, adicionar happy paths)
docs/cli-reference.md — Atualizar status de ❌ para ✅
```

#### Deep file dependency analysis

**`preview.ts`:**
- Hoje: trata 404 como "feature not available"
- Mudança: 404 passa a significar "preview not found" (comportamento normal)
- AUDIT-28 error handling pode ser simplificado

**`preview.test.ts`:**
- Hoje: testes para PREVIEW_NOT_AVAILABLE
- Mudança: substituir por testes de happy path contra API funcional
- Manter testes de auth error, not found, etc.

**`cli-reference.md`:**
- Atualizar status dos 3 commands de ❌ para ✅

#### Tasks
1. Remover guard PREVIEW_NOT_AVAILABLE do CLI
2. Verificar que URLs de API match: CLI espera `/projects/{project}/previews/...` — alinhar com routes reais
3. Atualizar testes: remover cenário PREVIEW_NOT_AVAILABLE, confirmar happy paths
4. Atualizar docs/cli-reference.md
5. Rodar full test suite: `cd cli && npm test`

#### TDD
```
RED:     TestPreviewList_HappyPath_ReturnsTable() — formato correto de output
RED:     TestPreviewList_HappyPath_JSON() — shape JSON correta
RED:     TestPreviewInspect_HappyPath() — detalhes do preview exibidos
RED:     TestPreviewDelete_HappyPath() — confirmation + success message
GREEN:   Remover guards, ajustar URLs
REFACTOR: Simplificar error handling sem PREVIEW_NOT_AVAILABLE
VERIFY:  cd cli && npm test
```

#### Acceptance Criteria
- [ ] Zero referências a PREVIEW_NOT_AVAILABLE no código
- [ ] URLs de API alinhadas entre CLI e router
- [ ] Todos os 1079+ testes CLI passando
- [ ] docs/cli-reference.md atualizado
- [ ] Exit codes corretos (0/1/3/4)
- [ ] Zero internal terms (Rule 3)

#### DoD
- [ ] Todas as tasks completadas
- [ ] `cd cli && npm test` green (todos os suites)
- [ ] Docs atualizados

---

### T4.2 — Testes de integração E2E do fluxo completo

#### Objective
Validar o fluxo end-to-end: PR webhook → preview created → build → deploy → URL accessible → PR close → cleanup.

#### Evidence
- 25/30 E2E tests pass hoje — preview adiciona coverage
- `infra/tests/e2e/` — Ginkgo test suite existente

#### Files to edit
```
infra/tests/e2e/preview_test.go (NEW) — E2E tests para preview environments
```

#### Deep file dependency analysis

**`preview_test.go` (NEW):**
- Ginkgo specs que testam fluxo completo
- Precisa de: GitHub webhook simulator, API client, kubectl para verificar namespace
- Downstream: nenhum — testes são leaf nodes

#### Deep Dives

**Test scenarios:**
1. **Happy path**: simular PR opened webhook → verificar preview criado no DB → verificar namespace existe → verificar URL acessível → simular PR closed → verificar cleanup
2. **Push update**: simular PR synchronize → verificar preview atualizado com novo commit
3. **TTL expiry**: criar preview com TTL curto → verificar cleanup automático
4. **Concurrent PRs**: abrir 2 PRs → verificar 2 previews independentes
5. **Manual delete**: criar preview → `loopzero preview delete` → verificar cleanup

**Nota:** E2E tests podem ser marcados como `[Serial]` para evitar interferência.

#### Tasks
1. Criar test file com Ginkgo structure
2. Implementar webhook simulator (POST para /api/v1/webhooks/github com HMAC)
3. Implementar verificações: DB row, K8s namespace, URL accessibility
4. Implementar cleanup verification
5. Tag com `[Preview]` label para execução seletiva

#### TDD
```
RED:     Describe("Preview Environments") — todos os specs falham (feature não existia)
GREEN:   Executar após todas as phases anteriores completas
REFACTOR: Extrair helpers reutilizáveis
VERIFY:  cd infra/tests/e2e && go test -v -tags e2e -run Preview
```

#### Acceptance Criteria
- [ ] Happy path E2E passando
- [ ] Push update E2E passando
- [ ] Cleanup (PR close) E2E passando
- [ ] Manual delete E2E passando
- [ ] Testes idempotentes (rodar 2x não quebra)

#### DoD
- [ ] Todas as tasks completadas
- [ ] E2E specs passando
- [ ] E2E count: 25 → 30+ (5 novos specs)

---

### T4.3 — Atualizar maturity assessment e CHANGELOG

#### Objective
Atualizar docs de maturidade e changelog para refletir preview environments completo.

#### Files to edit
```
docs/maturity-assessment.md — Atualizar score de Product Completeness e overall
CHANGELOG.md — Adicionar entry em [Unreleased] > Added
```

#### Tasks
1. Atualizar Product Completeness de 2.5 para 3.0+ (preview envs era +0.3 no path to 4.0)
2. Recalcular overall score (~3.5)
3. Atualizar Feature Completion Matrix: Preview environments → Done
4. Adicionar entry no CHANGELOG: "Preview environments automáticos via GitHub App"

#### TDD
```
N/A — documentação apenas
VERIFY: Revisão manual dos scores e consistência
```

#### Acceptance Criteria
- [ ] Maturity score atualizado com justificativa
- [ ] CHANGELOG entry em [Unreleased] com referência ao PR
- [ ] Feature matrix atualizada

#### DoD
- [ ] Docs atualizados
- [ ] Scores recalculados com justificativa

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | Controller não usa state machine (Rule 8) | T0.1 | Migrar para statemachine.Transition() |
| 2 | Controller usa naming inline (ignora ProjectID) | T0.2 | Migrar para naming.PreviewNamespace() |
| 3 | Controller não registrado no manager | T0.3 | Adicionar SetupPreviewController em main.go |
| 4 | Controller não deploya workload | T1.2 | Conditions-based workload lifecycle |
| 5 | Controller não limpa Application CR | T1.3 | Cleanup expandido com Application CR + Gitea |
| 6 | Sem endpoint de criação de preview | T1.1 | PreviewService + POST endpoint |
| 7 | Sem tabela no database | T2.1 | Migration 000003_preview_environments |
| 8 | Sem store PostgreSQL | T2.2 | PostgresPreviewStore implementation |
| 9 | Routes não registradas no router | T2.3 | Wire PreviewHandler in registerProtectedRoutes |
| 10 | Sem webhook handler para PR events | T3.1 | PreviewDeployer.HandlePR() |
| 11 | Sem GitHub comment com URL | T3.2 | GitHubCommentClient upsert |
| 12 | CLI retorna PREVIEW_NOT_AVAILABLE | T4.1 | Remover guard, apontar para API real |
| 13 | Sem testes E2E | T4.2 | 5 Ginkgo E2E specs |
| 14 | Maturity assessment desatualizado | T4.3 | Recalcular score |

**Coverage: 14/14 gaps covered (100%)**

## Global Definition of Done

- [ ] Todas as phases completadas (0-4)
- [ ] `task test` green (Go + CLI + E2E)
- [ ] `task lint` green
- [ ] `task validate:manifests` green
- [ ] Zero internal terms em CLI output (Rule 3)
- [ ] Todas as mutations usam environment lock (Rule 6)
- [ ] State machine enforced (Rule 8)
- [ ] Exit codes 0/1/2/3/4/5 only (Rule 4)
- [ ] `--json` field names stable (Rule 5)
- [ ] E2E: PR open → preview URL → PR close → cleanup (≤6min total)
- [ ] Maturity assessment atualizado (~3.5/5.0)
- [ ] CHANGELOG atualizado em [Unreleased]
