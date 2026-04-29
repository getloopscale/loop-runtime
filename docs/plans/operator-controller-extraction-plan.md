# Plan: Operator Controller Extraction — Monolith to Multi-Binary

> **Version 1.0** — Refatorar o binário monolítico `loopzero-tenant-controller` (8 controllers, 1 binary, 1 Deployment) em 4 binários independentes agrupados por domínio, cada um com seu próprio Deployment, Helm chart, RBAC, e pipeline de build. O objetivo é reduzir blast radius, permitir scaling independente, e aplicar least-privilege RBAC por controller group.

## Context

Hoje o operador LoopZero é um único binário `loopzero-tenant-controller` que empacota 8 controllers em um processo Go com leader election centralizada. A análise de código revela:

- **~3.500 linhas** de código de controller (sem testes), sendo o TenantController o maior (~1.740 linhas em 6 arquivos).
- **Um ClusterRole monolítico** com CRUD em todos os 10 CRDs + ArgoCD + Argo Workflows + namespaces + secrets — qualquer controller tem acesso a tudo.
- **Um Deployment com 256Mi de memória** compartilhado entre controllers com perfis de carga radicalmente diferentes (Tenant: raro+pesado, Preview: frequente+leve, PlatformBootstrap: one-shot).
- **Blast radius total**: panic em qualquer controller derruba todos os 8. Leader election única (lease `loopzero-operators.loopzero.io`) significa gap de ~15s até re-eleição.
- **Build acoplado**: mudança em `exposure_controller.go` recompila e redeploya todos os 8 controllers.

**Evidência de risco real**: o comentário na linha 355 do `main.go` documenta que `SetupPreviewController` foi implementado mas **nunca invocado** até ser wired tardiamente — padrão "implemented but not wired" que o monolith facilita.

## Objective

**Done = 4 binários independentes**, cada um com Helm chart, RBAC, Dockerfile, CI job, e test suite próprios, deployados como Deployments separados no namespace `loopzero-system`, com leader election independente e scaling configurável.

**Metas mensuráveis:**
1. Zero controllers compartilham processo — panic em um não afeta os outros
2. Cada binary tem ClusterRole com apenas os recursos que precisa (least-privilege)
3. Cada binary pode ser scaled independentemente (replicas + MaxConcurrentReconciles)
4. CI build paralelo — mudança em um controller não rebuilda os outros
5. Todos os 200+ envtest specs passam após a separação
6. Deploy validado em DigitalOcean (4 DOKS clusters)

## ADRs

### D1 — Quatro binários por agrupamento de domínio

**Decision:** Agrupar os 8 controllers em 4 binários por afinidade de domínio e dependência compartilhada, não 8 binários individuais.

**Rationale:** 8 binários seria over-engineering (YAGNI). Os agrupamentos naturais são:
- **Tenant+Environment**: acoplados por owner reference (`Tenant` owns `Environment`), compartilham `RuntimeClient` e `BuildClient`.
- **Application+Exposure**: acoplados por owner reference (`LoopZeroApplication` owns `ExposedRoute`), zero dependências externas.
- **Preview**: self-contained, perfil de carga único (TTL checks a cada 5min), precisa de `RuntimeClient`.
- **Platform (ClusterCredential+GitRepoBinding+PlatformBootstrap)**: todos são infra platform, baixa frequência, não reconcilam workloads.

Alternativa rejeitada: 8 binários (1 por controller) — overhead operacional desproporcional (8 Deployments, 8 leases, 8 ServiceMonitors, 8 CI jobs). Alternativa rejeitada: 2 binários (core vs auxiliary) — não resolve blast radius entre Tenant e Preview.

**Consequences:** 4 Helm sub-charts, 4 CI build jobs, 4 leases, 4 ServiceMonitors. Complexidade operacional aceitável.

### D2 — Shared library `operators/internal/shared/` para código universal

**Decision:** Extrair `tracing.go` e utilitários compartilhados para `operators/internal/shared/`, importado por todos os 4 binários.

**Rationale:** `tracing.go` é usado por todos os 8 controllers (hard coupling). `conditions/`, `constants/`, `metrics/`, `api/v1/` já são packages separados — não precisam mover. Apenas `tracing.go` está em `internal/controller/` e precisa ser extraído.

Alternativa rejeitada: mover para `pkg/` — é específico dos operators, não faz sentido em shared libs cross-component.

**Consequences:** Import path muda de chamada implícita (mesmo package) para `shared.StartReconcileSpan(...)`. Todos os controllers precisam atualizar imports.

### D3 — Um Helm chart por binário, umbrella chart atualizado

**Decision:** Criar 4 Helm charts: `loopzero-tenant-operator`, `loopzero-app-operator`, `loopzero-preview-operator`, `loopzero-platform-operator`. O chart `loopzero-tenant-controller` é removido. O umbrella `loopzero-platform` referencia os 4 novos.

**Rationale:** Charts independentes permitem enable/disable por ambiente (ex: `loopzero-preview-operator.enabled: false` em ambientes sem PR previews). RBAC, resources, e replicas configuráveis por chart.

Alternativa rejeitada: chart único com múltiplos Deployments — impede toggle independente e complica values nesting.

**Consequences:** Helm release names mudam. `infra-restart.yml` precisa de 4 rollout restart targets. Breaking change no Helm — aceitável em pre-release.

### D4 — RBAC least-privilege por binário

**Decision:** Decompor o ClusterRole monolítico em 4 ClusterRoles com apenas as permissões que cada group precisa.

**Rationale:** Hoje o binary de Preview tem CRUD em `tenants`, `applications`, `argoproj.io/applications`, `secrets` cluster-wide — nenhuma dessas permissões é usada pelo PreviewController. Violação direta de least-privilege.

**Consequences:** Cada chart tem seu próprio `clusterrole.yaml`. Se um controller for adicionado a um grupo no futuro, o RBAC precisa ser estendido. As 2 Roles namespaced (`argocd`, `loopzero-system`) migram para o grupo que efetivamente as usa.

### D5 — Test suite split com envtest compartilhado opcional

**Decision:** Cada binário tem seu próprio `suite_test.go` e envtest environment. O `operatortest/` package continua fornecendo fakes reutilizáveis.

**Rationale:** Um envtest compartilhado entre 4 packages criaria dependência circular de testes. Cada suite carrega apenas os controllers do seu grupo, o que é mais rápido e isolado.

Alternativa rejeitada: manter uma suite monolítica — contradiz o objetivo de independência.

**Consequences:** Tempo total de `go test` pode aumentar (~4 envtest startups vs 1), mas cada suite individual é mais rápida. Paralelismo no CI compensa.

### D6 — Leader election independente por binário

**Decision:** Cada binário tem seu próprio lease ID: `loopzero-tenant-operator.loopzero.io`, `loopzero-app-operator.loopzero.io`, `loopzero-preview-operator.loopzero.io`, `loopzero-platform-operator.loopzero.io`.

**Rationale:** Leases compartilhadas entre binários diferentes causariam conflito. Leases independentes permitem que cada grupo tenha replicas>1 com leader election ativa.

**Consequences:** 4 leases em `loopzero-system` namespace. Monitoring de leader health precisa cobrir 4 leases.

## Dependency Graph

```
Phase 0 (Shared Lib) ──▶ Phase 1 (Entrypoints) ──▶ Phase 2 (Dockerfiles)
                              │                          │
                              ▼                          ▼
                         Phase 3 (Tests)           Phase 4 (Helm + RBAC)
                                                         │
                                                         ▼
                                                   Phase 5 (CI/CD)
                                                         │
                                                         ▼
                                                   Phase 6 (Tilt)
                                                         │
                                                         ▼
                                                   Phase 7 (DO Validation)
```

- **Phase 0 → Phase 1**: sequencial (shared lib precisa existir antes dos novos entrypoints)
- **Phase 1 → Phase 2 + Phase 3**: paralelo (Dockerfiles e tests são independentes)
- **Phase 2 + Phase 3 → Phase 4**: Phase 4 depende de builds e tests passando
- **Phase 4 → Phase 5 → Phase 6 → Phase 7**: sequencial estrito

---

## Controller Group Reference

| Group | Binary | Controllers | Key Dependencies |
|-------|--------|------------|-----------------|
| **A** | `loopzero-tenant-operator` | Tenant, Environment | Gitea, ArgoCD, Harbor, MinIO, BuildClient, RuntimeClient |
| **B** | `loopzero-app-operator` | Application, Exposure | BaseDomain, HTTP client (probe) |
| **C** | `loopzero-preview-operator` | Preview | RuntimeClient, BaseDomain |
| **D** | `loopzero-platform-operator` | ClusterCredential, GitRepoBinding, PlatformBootstrap | ArgoCDNamespace, HTTP client |

---

## Phase 0: Extract Shared Tracing Library

**Objective:** Extrair `tracing.go` de `internal/controller/` para `internal/shared/` tornando-o importável por todos os 4 futuros binários sem dependência circular.

### T0.1 — Create `operators/internal/shared/` package

#### Objective
Mover as funções de tracing (`startReconcileSpan`, `startChildSpan`, `recordError`, `setSuccess`, `reconcileOutcome`) de `internal/controller/tracing.go` para `internal/shared/tracing.go`, exportando-as com nomes públicos.

#### Evidence
`tracing.go` está em `package controller` (linha 4) e é chamado por todos os 8 controllers via acesso direto ao package. Quando os controllers forem separados em packages distintos, essas funções precisam ser acessíveis cross-package.

#### Files to edit
```
operators/internal/shared/tracing.go — (NEW) Package shared com funções de tracing exportadas
operators/internal/controller/tracing.go — Substituir implementação por thin wrappers (backward compat durante migração)
operators/internal/controller/tenant_controller.go — Atualizar chamadas para shared.*
operators/internal/controller/environment_controller.go — Atualizar chamadas para shared.*
operators/internal/controller/application_controller.go — Atualizar chamadas para shared.*
operators/internal/controller/exposure_controller.go — Atualizar chamadas para shared.*
operators/internal/controller/preview_controller.go — Atualizar chamadas para shared.*
operators/internal/controller/clustercredential_controller.go — Atualizar chamadas para shared.*
operators/internal/controller/gitrepositorybinding_controller.go — Atualizar chamadas para shared.*
operators/internal/controller/platformbootstrap_controller.go — Atualizar chamadas para shared.*
```

#### Deep file dependency analysis
- **`tracing.go`**: Define 5 funções não-exportadas + 1 var (`tracer`). Depende de `operators/internal/metrics` para `ReconcileDuration`, `ReconcileErrors`, `ReconcileTotal`. Sem dependências circulares — `metrics/` não importa `controller/`.
- **Cada controller**: Chama `startReconcileSpan()`, `reconcileOutcome()`, `recordError()`, `setSuccess()` no início/fim de `Reconcile()`. Mudança é mecânica (prefixar com `shared.`).
- **`metrics/`**: Não muda. Continua sendo importado por `shared/tracing.go`.

#### Deep Dives
- As funções são renomeadas para exportadas: `startReconcileSpan` → `StartReconcileSpan`, etc.
- O `tracer` var é inicializado com `otel.Tracer("loopzero-operator")` — o nome do tracer permanece o mesmo para manter compatibilidade com dashboards de tracing existentes.
- `reconcileOutcome` importa `metrics.ReconcileDuration` etc — essa dependência move junto.
- O `tracing.go` original em `internal/controller/` pode ser deletado inteiramente, pois na mesma Phase todos os controllers são atualizados.

#### Tasks
1. Criar diretório `operators/internal/shared/`
2. Criar `operators/internal/shared/tracing.go` com as 5 funções exportadas + `tracer` var
3. Atualizar todos os 8 controllers para importar `shared` e prefixar chamadas
4. Deletar `operators/internal/controller/tracing.go`
5. `go build ./...` verifica compilação
6. `go test ./...` verifica que nada quebrou

#### TDD
```
RED:     TestStartReconcileSpan_ReturnsSpanWithAttributes — cria span e verifica attributes "controller", "k8s.object.name"
RED:     TestReconcileOutcome_Success — verifica que metrics são incrementadas com result="success"
RED:     TestReconcileOutcome_Error — verifica que ReconcileErrors é incrementado
GREEN:   Implementar shared/tracing.go com funções exportadas
REFACTOR: Remover tracing.go original, atualizar imports
VERIFY:  cd operators && go test ./internal/shared/... -v
```

#### Acceptance Criteria
- [ ] `operators/internal/shared/tracing.go` existe com 5 funções exportadas
- [ ] `operators/internal/controller/tracing.go` não existe mais
- [ ] `cd operators && go build ./...` compila sem erros
- [ ] `cd operators && go test ./...` passa todos os 200+ specs
- [ ] Zero mudanças de comportamento — apenas reorganização de código

#### DoD
- [ ] All tasks completed and validated
- [ ] `go vet ./...` sem warnings
- [ ] `go test ./...` green
- [ ] Import cycle check: `go build ./...` sem circular deps

---

## Phase 1: Create Separate Entrypoints

**Objective:** Criar 4 `main.go` independentes em `operators/cmd/`, um por group, cada um registrando apenas seus controllers e dependências.

### T1.1 — Reorganize cmd/ directory structure

#### Objective
Criar a estrutura de diretórios para os 4 binários e mover o main.go atual para referência.

#### Evidence
Hoje existe apenas `operators/cmd/main.go` (460 linhas) que importa e wires todos os 8 controllers. A função `setupProvisioners()` cria dependências que apenas o TenantController usa (Gitea, ArgoCD, Harbor, MinIO, BuildClient).

#### Files to edit
```
operators/cmd/main.go — Renomear para operators/cmd/main.go.bak (referência durante migração, deletar ao final)
operators/cmd/tenant-operator/main.go — (NEW) Entrypoint para Group A (Tenant + Environment)
operators/cmd/app-operator/main.go — (NEW) Entrypoint para Group B (Application + Exposure)
operators/cmd/preview-operator/main.go — (NEW) Entrypoint para Group C (Preview)
operators/cmd/platform-operator/main.go — (NEW) Entrypoint para Group D (ClusterCredential + GitRepoBinding + PlatformBootstrap)
```

#### Deep file dependency analysis
- **`main.go` atual**: `setupProvisioners()` cria Gitea, ArgoCD, Harbor, MinIO clients e BuildClient/RuntimeClient. `setupControllers()` wires todos os 8. Ambas funções precisam ser decompostas.
- **Group A (tenant-operator)**: Precisa de TODAS as dependências — Gitea, ArgoCD, Harbor, MinIO, BuildClient, RuntimeClient. É o mais pesado.
- **Group B (app-operator)**: Precisa apenas de `BaseDomain` string. Zero clients externos. Mais leve possível.
- **Group C (preview-operator)**: Precisa de `RuntimeClient` e `BaseDomain`.
- **Group D (platform-operator)**: Precisa de `ArgoCDNamespace` string e `*http.Client`. Sem provisioners.
- **Todos**: Precisam de scheme registration (`clientgoscheme`, `loopzerov1`), manager setup, health/ready checks, tracing init.

#### Deep Dives
- Cada `main.go` segue o mesmo padrão: flags → logger → tracing → manager → deps → controllers → health → start.
- A lógica de `newClientWithRetry` é usada apenas por Group A (BuildClient/RuntimeClient) e Group C (RuntimeClient). Mover para `shared/`.
- `cbStateLoggerFunc` é usada apenas por Group A (circuit breakers). Fica no main do tenant-operator.
- `pprofEnabled()` é utility genérica — mover para `shared/`.
- `parseRegistryCIDRs()` é usada apenas por Group A.
- Cada binário terá seu próprio lease ID (D6): `loopzero-{group}-operator.loopzero.io`.

#### Tasks
1. Criar `operators/cmd/tenant-operator/main.go` com provisioners + Tenant + Environment setup
2. Criar `operators/cmd/app-operator/main.go` com Application + Exposure setup (mínimo de deps)
3. Criar `operators/cmd/preview-operator/main.go` com RuntimeClient + Preview setup
4. Criar `operators/cmd/platform-operator/main.go` com HTTP client + 3 platform controllers
5. Extrair `newClientWithRetry` e `pprofEnabled` para `internal/shared/`
6. Verificar que cada binário compila: `go build ./cmd/tenant-operator/`, etc.
7. Manter `cmd/main.go` original intacto até Phase 7 (rollback safety)

#### TDD
```
RED:     TestTenantOperatorRegistersOnlyTenantAndEnvironment — start manager, verify only 2 controllers registered
RED:     TestAppOperatorRegistersOnlyApplicationAndExposure — start manager, verify only 2 controllers registered
RED:     TestPreviewOperatorRegistersOnlyPreview — start manager, verify only 1 controller registered
RED:     TestPlatformOperatorRegistersOnly3Controllers — start manager, verify 3 controllers registered
GREEN:   Implementar os 4 main.go
REFACTOR: Extrair boilerplate compartilhado para shared/
VERIFY:  cd operators && go build ./cmd/tenant-operator/ && go build ./cmd/app-operator/ && go build ./cmd/preview-operator/ && go build ./cmd/platform-operator/
```

#### Acceptance Criteria
- [ ] 4 binários compilam sem erros
- [ ] Cada binário registra apenas seus controllers (verificável via log "starting manager" e setup logs)
- [ ] Nenhum binário importa dependências que não precisa (ex: app-operator não importa `gitea/`, `argocd/`, `harbor/`, `minio/`)
- [ ] Lease IDs são únicos por binário
- [ ] `cmd/main.go` original ainda existe e compila (rollback)

#### DoD
- [ ] 4 binários compiláveis
- [ ] Import graph verificado (`go list -deps ./cmd/app-operator/` não inclui gitea/argocd/harbor/minio)
- [ ] `go vet ./...` sem warnings

---

## Phase 2: Split Dockerfiles

**Objective:** Criar 4 Dockerfiles, um por binário, com multi-stage build otimizado.

### T2.1 — Create per-binary Dockerfiles

#### Objective
Cada binário tem seu próprio Dockerfile que builda apenas o código necessário, produzindo imagens distroless mínimas.

#### Evidence
O Dockerfile atual (`operators/Dockerfile`) compila `./cmd/main.go` em um único binário `loopzero-tenant-controller`. Labels, entrypoint, e exposed ports são genéricos.

#### Files to edit
```
operators/Dockerfile — Manter como referência, marcar deprecated
operators/Dockerfile.tenant-operator — (NEW) Build loopzero-tenant-operator
operators/Dockerfile.app-operator — (NEW) Build loopzero-app-operator
operators/Dockerfile.preview-operator — (NEW) Build loopzero-preview-operator
operators/Dockerfile.platform-operator — (NEW) Build loopzero-platform-operator
```

#### Deep file dependency analysis
- **Dockerfile atual**: Copia `pkg/` e `operators/` inteiros, builda `./cmd/main.go`. Cache layer em `go mod download`.
- **Cada novo Dockerfile**: Mesmo padrão (distroless:nonroot, GOWORK=off), mas target diferente: `go build ./cmd/{group}/main.go`.
- **Dependência**: `pkg/` é compartilhado por todos (circuitbreaker, statemachine, naming, etc). Todos os Dockerfiles copiam `pkg/`.
- **Imagens resultantes**: `loopzero-tenant-operator`, `loopzero-app-operator`, `loopzero-preview-operator`, `loopzero-platform-operator` no DOCR.

#### Deep Dives
- Build args: nenhum necessário — a diferenciação é apenas o target path.
- Cache: cada Dockerfile compartilha o mesmo `go mod download` cache key (mesmo `go.mod`), então build paralelo no CI é eficiente.
- Labels: `org.opencontainers.image.title` é o único que muda por Dockerfile.
- Entrypoint: nome do binário muda (ex: `/usr/local/bin/loopzero-app-operator`).
- CMD default: `["--leader-elect=true"]` para todos.

#### Tasks
1. Criar 4 Dockerfiles seguindo o padrão do original (distroless:nonroot, CGO_ENABLED=0, trimpath)
2. Testar build local de cada: `docker build -f operators/Dockerfile.app-operator .`
3. Verificar que imagens resultantes são ≤20MB
4. Marcar `operators/Dockerfile` com comment: `# DEPRECATED: see Dockerfile.{group}-operator`

#### TDD
```
RED:     (N/A — Dockerfiles não têm testes unitários)
GREEN:   Build local dos 4 Dockerfiles
REFACTOR: None expected
VERIFY:  docker build -f operators/Dockerfile.tenant-operator -t test-tenant . && docker build -f operators/Dockerfile.app-operator -t test-app . && docker build -f operators/Dockerfile.preview-operator -t test-preview . && docker build -f operators/Dockerfile.platform-operator -t test-platform .
```

#### Acceptance Criteria
- [ ] 4 Dockerfiles buildam com sucesso
- [ ] Cada imagem contém apenas 1 binário
- [ ] Base image é `gcr.io/distroless/static-debian12:nonroot`
- [ ] Nenhum Dockerfile copia código de outros componentes (api/, cli/)
- [ ] `operators/Dockerfile` marcado como deprecated

#### DoD
- [ ] Build local dos 4 pass
- [ ] Image size ≤ 20MB cada
- [ ] Container runs com UID 65532 (nonroot)

---

## Phase 3: Split Test Suites

**Objective:** Criar test suites independentes por controller group para que cada binário tenha seus próprios testes envtest isolados.

### T3.1 — Reorganize controller packages

#### Objective
Mover cada controller group para seu próprio package dentro de `operators/internal/`, permitindo test suites independentes com envtest separado.

#### Evidence
Hoje todos os 8 controllers estão em `package controller` (`operators/internal/controller/`). Um único `suite_test.go` inicia 1 envtest e registra todos os 8 controllers. Separar os packages é pré-requisito para test suites independentes.

#### Files to edit
```
operators/internal/tenant/ — (NEW) Package para TenantController + EnvironmentController
operators/internal/tenant/tenant_controller.go — (MOVE from controller/)
operators/internal/tenant/tenant_build.go — (MOVE)
operators/internal/tenant/tenant_gitops.go — (MOVE)
operators/internal/tenant/tenant_deletion.go — (MOVE)
operators/internal/tenant/tenant_harbor.go — (MOVE)
operators/internal/tenant/tenant_priority.go — (MOVE)
operators/internal/tenant/registry_auth_merge.go — (MOVE, usado apenas por tenant)
operators/internal/tenant/environment_controller.go — (MOVE)
operators/internal/tenant/environment_network.go — (MOVE)
operators/internal/tenant/environment_quota.go — (MOVE)
operators/internal/tenant/environment_rbac.go — (MOVE)

operators/internal/app/ — (NEW) Package para ApplicationController + ExposureController
operators/internal/app/application_controller.go — (MOVE)
operators/internal/app/exposure_controller.go — (MOVE)

operators/internal/preview/ — (NEW) Package para PreviewController
operators/internal/preview/preview_controller.go — (MOVE)
operators/internal/preview/preview_resources.go — (MOVE)

operators/internal/platform/ — (NEW) Package para os 3 platform controllers
operators/internal/platform/clustercredential_controller.go — (MOVE)
operators/internal/platform/gitrepositorybinding_controller.go — (MOVE)
operators/internal/platform/platformbootstrap_controller.go — (MOVE)
operators/internal/platform/platformbootstrap_gitea.go — (MOVE)
operators/internal/platform/platformbootstrap_argocd.go — (MOVE)
operators/internal/platform/platformbootstrap_api.go — (MOVE)
```

#### Deep file dependency analysis
- **Cada arquivo de controller**: Declara `package controller` e usa funções não-exportadas do mesmo package (ex: `startReconcileSpan` de `tracing.go`). Após Phase 0, essas funções estão em `shared/` — então a dependência intra-package é eliminada.
- **`registry_auth_merge.go`**: Usado apenas por `tenant_build.go` e `tenant_harbor.go`. Move com o tenant group.
- **`environment_network.go`, `environment_quota.go`, `environment_rbac.go`**: Usados apenas pelo EnvironmentController. Movem para `tenant/`.
- **`platformbootstrap_gitea.go`, `_argocd.go`, `_api.go`**: Usados apenas pelo PlatformBootstrapController. Movem para `platform/`.
- **Downstream**: `cmd/main.go` (e os 4 novos main.go) precisam atualizar imports de `controller.Setup*` para `tenant.Setup*`, `app.Setup*`, etc.

#### Deep Dives
- Package names: `tenant`, `app`, `preview`, `platform` (curtos, descritivos).
- Cada package exporta uma função `Setup*Controller(mgr ctrl.Manager, ...)` — mesma assinatura atual, apenas o package muda.
- O reconciler struct em cada package muda: `type TenantReconciler struct` (hoje sem prefixo público, pois está no mesmo package). Manter não-exportado.
- `internal/controller/` fica vazio após a migração. Deletar o diretório.
- `operatortest/setup.go` precisa atualizar imports para os novos packages.

#### Tasks
1. Criar os 4 diretórios: `tenant/`, `app/`, `preview/`, `platform/`
2. Mover arquivos para seus respectivos packages (alterar `package` declaration)
3. Atualizar imports em `cmd/tenant-operator/main.go`, etc.
4. Atualizar `operatortest/setup.go` imports
5. Atualizar `fake_provisioners_test.go` (hoje em `controller/`, redistribuir por group)
6. Deletar `operators/internal/controller/` (somente após todos os moves)
7. `go build ./...` verifica compilação
8. `go vet ./...` verifica sem warnings

#### TDD
```
RED:     (Testes existentes que importam `controller` vão falhar — isso É o red)
GREEN:   Atualizar todos os imports e package declarations
REFACTOR: Limpar imports não usados
VERIFY:  cd operators && go build ./... && go vet ./...
```

#### Acceptance Criteria
- [ ] `operators/internal/controller/` não existe mais
- [ ] 4 novos packages com `package tenant`, `package app`, `package preview`, `package platform`
- [ ] Cada package exporta apenas `Setup*Controller` functions
- [ ] `go build ./...` compila sem erros
- [ ] Zero dependências circulares

#### DoD
- [ ] All moves completed
- [ ] `go build ./...` green
- [ ] `go vet ./...` green
- [ ] Import graph verified: `go list -deps ./internal/app/` não inclui `./internal/tenant/`

### T3.2 — Create per-group test suites

#### Objective
Criar um `suite_test.go` independente para cada package, com seu próprio envtest environment registrando apenas os controllers do grupo.

#### Evidence
O `suite_test.go` atual (200 linhas) registra todos os 8 controllers e cria fakes para Gitea, ArgoCD, MinIO. Cada grupo precisa apenas dos fakes relevantes.

#### Files to edit
```
operators/internal/tenant/suite_test.go — (NEW) envtest com Tenant + Environment + fakes (Gitea, ArgoCD, MinIO, Harbor)
operators/internal/app/suite_test.go — (NEW) envtest com Application + Exposure (sem fakes de provisioners)
operators/internal/preview/suite_test.go — (NEW) envtest com Preview (RuntimeClient fake)
operators/internal/platform/suite_test.go — (NEW) envtest com 3 platform controllers (HTTP client)
operators/internal/tenant/*_test.go — (MOVE) testes de tenant/environment/isolation/quotas/network
operators/internal/app/*_test.go — (MOVE) testes de application/exposure
operators/internal/preview/*_test.go — (MOVE) testes de preview (envtest + unit)
operators/internal/platform/*_test.go — (MOVE) testes de platform bootstrap/clustercredential/gitrepobinding
operators/internal/controller/suite_test.go — DELETE (após mover tudo)
```

#### Deep file dependency analysis
- **`suite_test.go` atual**: Inicia envtest, cria manager, registra todos os 8 controllers, cria 3 fake provisioners. Variáveis globais: `k8sClient`, `ctx`, `cancel`, `testEnv`.
- **Test files por controller**: Usam as variáveis globais do suite. Após split, cada suite tem suas próprias.
- **`fake_provisioners_test.go`**: Define `fakeGitProvisioner`, `fakeArgoCDProvisioner`, `fakeMinIOProvisioner`. Redistribuir: Gitea+ArgoCD+MinIO fakes vão para `tenant/`, HTTP fakes para `platform/`.
- **`operatortest/`**: Já tem fakes exportados. Os suites podem importar diretamente.

#### Deep Dives
- Cada suite carrega todos os 10 CRDs (via `config/crd/bases/`), mesmo que o controller não use todos. Isso é intencional — envtest precisa que CRDs existam para que owner references e watches funcionem.
- O dual-client pattern (build/runtime simulados pelo mesmo envtest) se aplica apenas a `tenant/` e `preview/`.
- `app/` é o suite mais simples: zero fakes, zero multi-cluster, apenas o manager + 2 controllers.
- Timeouts: manter `timeout = 15s`, `interval = 250ms` do original.

#### Tasks
1. Criar `tenant/suite_test.go` com envtest + 2 controllers + 4 fakes
2. Criar `app/suite_test.go` com envtest + 2 controllers (sem fakes)
3. Criar `preview/suite_test.go` com envtest + 1 controller + RuntimeClient
4. Criar `platform/suite_test.go` com envtest + 3 controllers + HTTP client
5. Mover test files para seus packages (atualizar `package` declaration)
6. Redistribuir `fake_provisioners_test.go` conteúdo
7. Rodar cada suite independente: `go test ./internal/tenant/...`, etc.
8. Verificar que contagem total de specs é >= 200

#### TDD
```
RED:     Testes existentes falham porque suite_test.go foi removido do controller/
GREEN:   Novos suites registram seus controllers e testes passam
REFACTOR: Remover duplicação entre suites (uso de operatortest/)
VERIFY:  cd operators && go test ./internal/tenant/... -v -count=1 && go test ./internal/app/... -v -count=1 && go test ./internal/preview/... -v -count=1 && go test ./internal/platform/... -v -count=1
```

#### Acceptance Criteria
- [ ] 4 `suite_test.go` independentes existem
- [ ] Cada suite inicia e para sem erros
- [ ] Total de specs >= 200 (mesma contagem do monolith)
- [ ] Zero testes flaky introduzidos
- [ ] `go test ./...` no diretório operators/ passa tudo
- [ ] Testes podem rodar em paralelo (diferentes envtest instances)

#### DoD
- [ ] All test suites green
- [ ] `go test ./... -count=1` green
- [ ] Coverage não diminuiu vs baseline

---

## Phase 4: Helm Charts + RBAC Decomposition

**Objective:** Criar 4 Helm charts independentes com RBAC least-privilege, substituindo o chart monolítico `loopzero-tenant-controller`.

### T4.1 — Create 4 Helm sub-charts

#### Objective
Criar `charts/loopzero-tenant-operator/`, `charts/loopzero-app-operator/`, `charts/loopzero-preview-operator/`, `charts/loopzero-platform-operator/` com templates de Deployment, Service, ServiceMonitor, e RBAC.

#### Evidence
O chart `charts/loopzero-tenant-controller/` tem: deployment, clusterrole, clusterrolebinding, 2 roles namespaced (argocd, loopzero-system), secret, configmap, servicemonitor, networkpolicy. Resources: 100m/64Mi → 500m/256Mi. Todos esses recursos precisam ser decompostos por grupo.

#### Files to edit
```
charts/loopzero-tenant-operator/ — (NEW) Chart completo para Group A
charts/loopzero-app-operator/ — (NEW) Chart completo para Group B
charts/loopzero-preview-operator/ — (NEW) Chart completo para Group C
charts/loopzero-platform-operator/ — (NEW) Chart completo para Group D
charts/loopzero-tenant-controller/ — DELETE ao final (após umbrella atualizado)
charts/loopzero-platform/Chart.yaml — Atualizar deps: remover loopzero-tenant-controller, adicionar 4 novos
charts/loopzero-platform/values.yaml — Adicionar enable toggles e values defaults para 4 novos
```

#### Deep file dependency analysis
- **Chart atual**: ClusterRole com ~25 rules, 2 namespace Roles, 1 Deployment, 1 ConfigMap, 1 Secret.
- **Group A (tenant-operator)**: Precisa de RBAC mais amplo (tenants, environments, namespaces, secrets cluster-wide, argoproj.io, scheduling.k8s.io). Herda as 2 Roles namespaced. ConfigMap com GITEA_URL, ARGOCD_NAMESPACE, WORKLOAD_CLUSTER_URL, INGRESS_NAMESPACE. Secret com GITEA_TOKEN + MinIO/Harbor/DO creds. Kubeconfig volumes (build + runtime).
- **Group B (app-operator)**: RBAC mínimo (loopzeroapplications, exposedroutes, ingresses, secrets para TLS, services). ConfigMap com BASE_DOMAIN apenas. Sem Secret. Sem volumes.
- **Group C (preview-operator)**: RBAC para previewenvironments, namespaces, resourcequotas, limitranges, networkpolicies (runtime cluster). ConfigMap com BASE_DOMAIN. Kubeconfig volume (runtime apenas).
- **Group D (platform-operator)**: RBAC para clustercredentials, gitrepositorybindings, platformbootstraps, secrets (argocd namespace). Role namespaced em argocd e loopzero-system migra para este group. ConfigMap com ARGOCD_NAMESPACE.

#### Deep Dives

**RBAC Decomposition (D4):**

| Group | CRDs | Core Resources | Namespaced Roles |
|-------|------|---------------|-----------------|
| A (tenant) | tenants, environments | namespaces, secrets, resourcequotas, limitranges, roles, rolebindings, serviceaccounts, networkpolicies, priorityclasses, configmaps, deployments, services + argoproj.io/* + argoproj.io/workflows | (nenhum — acessa build/runtime via kubeconfig) |
| B (app) | loopzeroapplications, exposedroutes | secrets (TLS read), services, ingresses | — |
| C (preview) | previewenvironments | namespaces, resourcequotas, limitranges, networkpolicies | — |
| D (platform) | clustercredentials, gitrepositorybindings, platformbootstraps | secrets, configmaps | argocd (secret delete), loopzero-system (secret delete) |

Todos precisam de: `coordination.k8s.io/leases` (leader election), `events` (create).

**Resource sizing:**

| Group | CPU req/lim | Memory req/lim | Replicas | Rationale |
|-------|-----------|---------------|----------|-----------|
| A (tenant) | 100m/500m | 64Mi/256Mi | 1 | Mais pesado, circuit breakers, multi-cluster |
| B (app) | 50m/200m | 32Mi/128Mi | 1 | Leve, sem I/O externo |
| C (preview) | 50m/200m | 32Mi/128Mi | 1 | TTL checks frequentes mas leves |
| D (platform) | 50m/200m | 32Mi/128Mi | 1 | Low frequency, one-shot bootstrap |

#### Tasks
1. Criar `charts/loopzero-tenant-operator/` baseado no chart atual (ajustar RBAC, nome, labels)
2. Criar `charts/loopzero-app-operator/` com RBAC mínimo (apenas loopzeroapplications + exposedroutes)
3. Criar `charts/loopzero-preview-operator/` com RBAC para preview + runtime namespaces
4. Criar `charts/loopzero-platform-operator/` com RBAC para platform CRDs + 2 Roles namespaced
5. Atualizar `charts/loopzero-platform/Chart.yaml` — remover dep `loopzero-tenant-controller`, adicionar 4 novos
6. Atualizar `charts/loopzero-platform/values.yaml` — enable toggles por operator
7. Validar com `helm template` cada chart
8. Validar com `kubeconform --strict` cada chart

#### TDD
```
RED:     helm template loopzero-tenant-operator charts/loopzero-tenant-operator/ falha (chart não existe)
RED:     kubeconform --strict rejeita ClusterRole com recursos desnecessários
GREEN:   Criar charts com RBAC correto
REFACTOR: Extrair helpers comuns para loopzero-common
VERIFY:  for chart in loopzero-tenant-operator loopzero-app-operator loopzero-preview-operator loopzero-platform-operator; do helm template $chart charts/$chart/ && echo "OK: $chart"; done
```

#### Acceptance Criteria
- [ ] 4 charts geram manifests válidos via `helm template`
- [ ] `kubeconform --strict` passa para todos os 4
- [ ] ClusterRole de cada chart contém APENAS as permissões necessárias
- [ ] NetworkPolicy de cada chart permite egress apenas para services que o grupo usa
- [ ] Umbrella chart `loopzero-platform` inclui os 4 novos como deps
- [ ] `loopzero-tenant-controller` chart marcado como deprecated/removido

#### DoD
- [ ] `helm template` OK para os 4 charts
- [ ] `kubeconform --strict` OK
- [ ] `task validate:manifests` passa
- [ ] `helm dependency update charts/loopzero-platform/` sucesso

---

## Phase 5: CI/CD Pipeline Updates

**Objective:** Atualizar GitHub Actions para buildar, assinar, e deployar 4 imagens independentes.

### T5.1 — Update build-images.yml

#### Objective
Adicionar 4 build jobs (um por binário) no workflow `build-images.yml`, substituindo o job `build-operator` monolítico.

#### Evidence
O job `build-operator` (linhas 193-236 de `build-images.yml`) builda uma única imagem `loopzero-tenant-controller` com change detection em `operators/`. A matrix `sign-and-attest` referencia `component: loopzero-tenant-controller`.

#### Files to edit
```
.github/workflows/build-images.yml — Substituir build-operator por 4 jobs, atualizar sign matrix
```

#### Deep file dependency analysis
- **`build-images.yml`**: Job `build-operator` usa `docker buildx build` com cache registry. Output: `image` e `digest`. Downstream: `sign-and-attest` matrix consume via `needs`.
- **Change detection**: Hoje qualquer mudança em `operators/` triggera rebuild. Com 4 binários, mudanças em `internal/tenant/` devem rebuildar apenas `loopzero-tenant-operator`.
- **Sign matrix** (linha 435-451): Lista cada componente com build_job ref. Precisa de 4 entries.

#### Deep Dives
- Change detection granular por path: `operators/cmd/tenant-operator/**`, `operators/internal/tenant/**`, `operators/internal/shared/**`, `operators/api/v1/**`, `pkg/**`. Shared paths (`shared/`, `api/v1/`, `pkg/`) triggerram rebuild de todos os 4.
- Cache keys: cada Dockerfile pode usar o mesmo `go.mod` cache key, mas cache-to tag precisa ser separada (ex: `loopzero-tenant-operator:cache`).
- Sign-and-attest: cosign signs each image digest individually. 4 entries na matrix.
- SBOM: syft gera SBOM por imagem. 4 SBOMs.

#### Tasks
1. Criar 4 jobs: `build-tenant-operator`, `build-app-operator`, `build-preview-operator`, `build-platform-operator`
2. Cada job com change detection paths granulares
3. Atualizar `sign-and-attest` matrix com 4 componentes
4. Remover job `build-operator` antigo
5. Testar workflow com `act` ou dry-run

#### TDD
```
RED:     CI falha porque build-operator não encontra Dockerfile antigo
GREEN:   4 novos jobs referenciam Dockerfiles corretos
REFACTOR: None expected
VERIFY:  gh workflow run build-images.yml (dry run ou PR trigger)
```

#### Acceptance Criteria
- [ ] 4 build jobs no workflow
- [ ] Change detection granular por controller group
- [ ] Sign-and-attest cobre as 4 imagens
- [ ] SBOM gerado para cada imagem
- [ ] Build paralelo — 4 jobs rodam concorrentemente

#### DoD
- [ ] Workflow syntax válida (`gh workflow view build-images.yml`)
- [ ] PR de teste triggera builds corretos
- [ ] Sign-and-attest sucesso para todos

### T5.2 — Update infra-restart.yml and infra-deploy.yml

#### Objective
Atualizar workflows de deploy e restart para targets 4 Deployments.

#### Evidence
`infra-restart.yml` hardcodes `kubectl rollout restart deploy tenant-controller -n loopzero-system`. Precisa de 4 targets.

#### Files to edit
```
.github/workflows/infra-restart.yml — 4 rollout restart targets
.github/workflows/infra-deploy.yml — Atualizar refs se necessário
.github/workflows/helm-charts.yml — Adicionar push de 4 novos charts OCI
```

#### Deep file dependency analysis
- **`infra-restart.yml`**: Simples — `kubectl rollout restart` + `rollout status`. Precisa repetir para 4 deployments.
- **`helm-charts.yml`**: Itera sobre sub-charts para push OCI. Precisa incluir os 4 novos e excluir o antigo.

#### Tasks
1. Atualizar `infra-restart.yml` com loop ou matrix para 4 deployments
2. Atualizar `helm-charts.yml` para incluir novos chart names
3. Verificar `infra-deploy.yml` por refs hardcoded ao chart antigo

#### TDD
```
RED:     (N/A — workflows testados via dry run)
GREEN:   Workflows referenciam os 4 novos nomes
REFACTOR: None expected
VERIFY:  grep -r "loopzero-tenant-controller" .github/workflows/ retorna 0 matches (após cleanup)
```

#### Acceptance Criteria
- [ ] `infra-restart.yml` reinicia 4 deployments
- [ ] `helm-charts.yml` pusha 4 charts OCI
- [ ] Zero referências ao chart antigo `loopzero-tenant-controller` nos workflows

#### DoD
- [ ] Workflow files valid YAML
- [ ] `grep -r loopzero-tenant-controller .github/workflows/` retorna 0

---

## Phase 6: Tilt Dev Loop

**Objective:** Atualizar Tiltfile e manifests de dev para 4 deployments locais.

### T6.1 — Split Tilt operator deployment

#### Objective
Criar 4 `docker_build` + 4 `k8s_yaml` + 4 `k8s_resource` no Tiltfile, com dependency graph correto.

#### Evidence
Tiltfile atual tem 1 `docker_build("loopzero-tenant-controller", ...)` e 1 `k8s_resource("tenant-controller", ...)` com deps `["crds", "loopzero-api", "gitea", "minio"]`.

#### Files to edit
```
Tiltfile — 4 docker_build + k8s_yaml + k8s_resource blocks
infra/tilt/operator-deployment.yaml — DELETE ou renomear
infra/tilt/tenant-operator-deployment.yaml — (NEW)
infra/tilt/app-operator-deployment.yaml — (NEW)
infra/tilt/preview-operator-deployment.yaml — (NEW)
infra/tilt/platform-operator-deployment.yaml — (NEW)
```

#### Deep file dependency analysis
- **Tiltfile**: `docker_build` scope define quais files triggerram rebuild. Com 4 builds, o `only=` param precisa ser granular por group.
- **`operator-deployment.yaml`**: Static K8s manifest (não Helm-rendered). Hardcodes env vars, ports, volumes. Precisa ser replicado 4x com ajustes.
- **Dependency graph no Tilt**: tenant-operator depende de `crds`, `gitea`, `minio`, `loopzero-api`. app-operator depende apenas de `crds`. preview-operator de `crds`. platform-operator de `crds`, `gitea`.

#### Deep Dives
- Leader election OFF em dev (`--leader-elect=false`) para todos os 4 — evita lease delay.
- Metrics port: usar `:8080` para todos em dev (reconciliar com Helm que usa `:8443` — isso é divergência pré-existente, não introduzida por este plan).
- `only=` no docker_build: tenant precisa de `["operators/cmd/tenant-operator/", "operators/internal/tenant/", "operators/internal/shared/", "operators/api/v1/", "pkg/"]`. App precisa menos.

#### Tasks
1. Criar 4 static manifests em `infra/tilt/`
2. Atualizar Tiltfile com 4 docker_build blocks
3. Atualizar Tiltfile com 4 k8s_resource blocks e deps corretas
4. Testar `tilt up` — todos os 4 operators iniciam
5. Testar hot reload — mudança em `internal/tenant/` rebuilda apenas tenant-operator

#### TDD
```
RED:     tilt up falha porque operator-deployment.yaml não existe mais
GREEN:   4 novos manifests + Tiltfile blocks
REFACTOR: None expected
VERIFY:  tilt up (visual: 4 pods running em k3d)
```

#### Acceptance Criteria
- [ ] `tilt up` inicia 4 operator pods
- [ ] Hot reload granular funciona (mudança em app/ rebuilda apenas app-operator)
- [ ] Todos os 4 passam health check (readyz/healthz)
- [ ] Dev loop time não aumentou significativamente (< 30s per rebuild)

#### DoD
- [ ] `tilt up` green
- [ ] 4 pods `Running` em k3d
- [ ] Rebuild seletivo verificado

---

## Phase 7: Production Validation on DigitalOcean

**Objective:** Deployar os 4 operators em DigitalOcean, validar que todos os CRDs reconcilam corretamente, e remover o binário monolítico.

### T7.1 — Deploy and validate on DO

#### Objective
Executar `pulumi up` com os novos charts, verificar que todos os pods estão healthy, e que operações existentes (tenant create, deploy, preview, etc) funcionam.

#### Evidence
Produção atual tem 195 pods saudáveis em 4 clusters. O tenant-controller é um dos pods no mgmt cluster. A mudança substitui 1 pod por 4 pods menores.

#### Files to edit
```
infra/pulumi/ — Atualizar refs ao chart loopzero-tenant-controller para os 4 novos
charts/loopzero-platform/charts/ — Gerar .tgz para os 4 novos sub-charts
```

#### Deep file dependency analysis
- **Pulumi stack**: Referencia `loopzero-platform` umbrella chart. A mudança nos deps do umbrella propaga automaticamente.
- **ArgoCD**: Se managed pelo ArgoCD no mgmt cluster, o sync precisa ser re-triggered após a mudança de charts.

#### Tasks
1. `pulumi preview --stack dev` — verificar diff esperado (1 deployment removido, 4 adicionados)
2. `pulumi up --stack dev` — aplicar
3. Verificar 4 pods `Running` no mgmt cluster
4. Criar tenant de teste — verificar TenantController reconcilia
5. Criar LoopZeroApplication — verificar ApplicationController reconcilia
6. Criar PreviewEnvironment — verificar PreviewController reconcilia
7. Verificar leader election leases: `kubectl get leases -n loopzero-system`
8. Verificar métricas: `curl :8080/metrics` de cada pod
9. Remover `operators/cmd/main.go` e `operators/Dockerfile` originais
10. Remover `charts/loopzero-tenant-controller/`

#### TDD
```
RED:     (N/A — validação em ambiente real)
GREEN:   Todos os controllers reconcilam corretamente
REFACTOR: Remover artifacts do monolith
VERIFY:  kubectl get pods -n loopzero-system | grep operator (4 pods Running)
```

#### Acceptance Criteria
- [ ] 4 pods `Running` no mgmt cluster DO
- [ ] `kubectl get leases -n loopzero-system` mostra 4 leases ativas
- [ ] Tenant creation reconcilia sem erros
- [ ] Application creation reconcilia sem erros
- [ ] Preview creation reconcilia sem erros
- [ ] PlatformBootstrap em estado Complete
- [ ] Métricas `loopzero_operator_reconcile_total` presentes em cada pod
- [ ] Memory usage < 256Mi por pod
- [ ] `operators/cmd/main.go` removido
- [ ] `charts/loopzero-tenant-controller/` removido
- [ ] CI pipeline passa com os novos jobs

#### DoD
- [ ] Validação em DO completa
- [ ] Zero artifacts do monolith restam
- [ ] `task test` passa
- [ ] `task lint` passa
- [ ] `task validate:manifests` passa
- [ ] CHANGELOG.md atualizado

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | Blast radius — panic em 1 controller derruba todos | T1.1, T2.1 | 4 binários independentes, crash isolado |
| 2 | RBAC overprivileged — ClusterRole monolítico | T4.1 | 4 ClusterRoles least-privilege por grupo |
| 3 | Scaling acoplado — não pode escalar Preview sem Tenant | T4.1 | Replicas independentes por chart |
| 4 | Build acoplado — mudança local rebuilda tudo | T2.1, T5.1 | Dockerfiles e CI jobs separados com change detection |
| 5 | Leader election única — gap de 15s afeta todos | T1.1 (D6) | 4 leases independentes |
| 6 | Test suite monolítica — 1 envtest para 200+ specs | T3.2 | 4 suites envtest independentes |
| 7 | Deploy atômico — redeploy de todos por mudança local | T4.1, T5.2 | Charts independentes, rollout restart seletivo |
| 8 | Shared tracing code em package controller | T0.1 | Extraído para `internal/shared/` |
| 9 | Dev loop rebuilda tudo | T6.1 | Tilt granular por group |
| 10 | Helm chart name incorreto (tenant-controller ≠ platform operator) | T4.1 | 4 charts com nomes que refletem o conteúdo |

**Coverage: 10/10 gaps covered (100%)**

## Global Definition of Done

- [ ] All 7 phases completed
- [ ] All tests passing (`cd operators && go test ./... -count=1`)
- [ ] Zero `go vet` warnings
- [ ] `task lint` green
- [ ] `task validate:manifests` green
- [ ] 4 pods Running em DigitalOcean
- [ ] 4 leader election leases ativas
- [ ] `operators/cmd/main.go` monolítico removido
- [ ] `charts/loopzero-tenant-controller/` removido
- [ ] Zero referências a `loopzero-tenant-controller` em workflows (exceto CHANGELOG)
- [ ] CHANGELOG.md atualizado com entry na seção `[Unreleased]`
- [ ] Maturity assessment atualizado
