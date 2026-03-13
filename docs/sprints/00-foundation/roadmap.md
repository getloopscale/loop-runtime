# Sprint 0 — Roadmap Executavel

> Gerado a partir de `docs/technical.md` §5, §9, §15, §19, §22, §23.
> Cada task e implementavel em uma unica sessao de trabalho com TDD obrigatorio.

---

## Indice

- [Grafo de dependencias](#grafo-de-dependencias)
- [Tasks Infra](#tasks-infra)
- [Tasks Operator (CRDs)](#tasks-operator-crds)
- [Tasks API (Schema + State Machines TS)](#tasks-api-schema--state-machines-ts)
- [Tasks Operator (State Machines Go)](#tasks-operator-state-machines-go)
- [Tasks Cross (CI + Observability)](#tasks-cross-ci--observability)
- [Exit Gate](#exit-gate-sprint-0)
- [Resumo de Tasks](#resumo-de-tasks)

---

## Grafo de Dependencias

```
S0.1 (k3d configs) ──> S0.2 (Taskfile clusters:create) ──> S0.3 (Helm infra:install)
                                                                    │
                              ┌─────────────────────────────────────┤
                              │                                     │
                              v                                     v
                        S0.4 (CRDs)                     S0.5 (PostgreSQL schema)
                              │                                     │
                              v                                     v
                        S0.7 (State machines Go)        S0.6 (State machines TS)
                              │                                     │
                              v                                     v
                        S0.8 (Unit tests Go)            S0.9 (Unit tests TS)
                              │                                     │
                              └──────────┬──────────────────────────┘
                                         │
                                         v
                        S0.3 ──> S0.10 (Prometheus + Grafana)
                        S0.3 ──> S0.11 (OTel Collector)
                        S0.3 ──> S0.12 (Structured logging)
                                         │
                              S0.8 + S0.9 + S0.12
                                         │
                                         v
                                  S0.13 (CI pipeline)
                                         │
                                         v
                                  S0.14 (Taskfile: setup + test:unit)
```

**Trilhas paralelas:**
- **Trilha Infra:** S0.1 → S0.2 → S0.3 → S0.10/S0.11/S0.12
- **Trilha Operator:** S0.4 → S0.7 → S0.8
- **Trilha API:** S0.5 → S0.6 → S0.9
- **Integracao:** S0.13 → S0.14

---

## Tasks Infra

---

### Task S0.1: k3d cluster configs

**Componente:** infra
**Agent:** infra-engineer
**Spec:** §19.3 — k3d configuration
**Depende de:** nenhuma
**Estimativa:** P (uma sessao)

#### O que implementar
- Arquivo `infra/k3d/mgmt-cluster.yaml` conforme §19.3:
  - 1 server + 2 agents
  - kubeAPI hostPort: 6443
  - Ports: 3000:30000 (Gitea), 3001:30001 (API), 3002:30002 (Grafana), 9001:30003 (MinIO console), 9090:30004 (Prometheus), 30082:30082 (ArgoCD UI)
- Arquivo `infra/k3d/build-cluster.yaml`:
  - 1 server + 2 agents
  - kubeAPI hostPort: 6445
  - Ports: 30081:30081 (Argo Workflows UI)
- Arquivo `infra/k3d/workload-cluster.yaml`:
  - 1 server + 2 agents
  - kubeAPI hostPort: 6444
  - Sem port mappings extras

#### Criterios de aceite
- [ ] 3 arquivos YAML validos em `infra/k3d/`
- [ ] Cada cluster tem nome unico: `loopzero-mgmt`, `loopzero-build`, `loopzero-workload`
- [ ] Ports mapeadas conforme §19.3
- [ ] kubeAPI ports nao conflitam (6443, 6444, 6445)
- [ ] Arquivos passam validacao YAML

#### Teste primeiro (TDD)
- Arquivo: `infra/tests/unit/k3d-configs_test.go` (ou script de validacao)
- Testar: YAML e parseavel
- Testar: nomes dos clusters sao unicos
- Testar: ports nao conflitam entre clusters

#### Riscos conhecidos
- Ports ocupadas na maquina do dev podem causar falha — documentar requisitos

---

### Task S0.2: Taskfile — clusters:create

**Componente:** infra
**Agent:** infra-engineer
**Spec:** §19.2 — Taskfile
**Depende de:** S0.1
**Estimativa:** M (meio dia)

#### O que implementar
- `Taskfile.yml` na raiz do repositorio
- Task `clusters:create`:
  - `k3d cluster create loopzero-mgmt --config infra/k3d/mgmt-cluster.yaml`
  - `k3d cluster create loopzero-build --config infra/k3d/build-cluster.yaml`
  - `k3d cluster create loopzero-workload --config infra/k3d/workload-cluster.yaml`
- Task deve ser idempotente: se cluster ja existe, nao falha
- Verificacao pos-criacao: `kubectl cluster-info` em cada contexto

#### Criterios de aceite
- [ ] `task clusters:create` cria os 3 clusters sem erro
- [ ] Idempotente: rodar 2x nao falha
- [ ] 3 contextos kubeconfig disponiveis apos execucao
- [ ] Cada cluster responde a `kubectl cluster-info`

#### Teste primeiro (TDD)
- Nao ha teste unitario — task e validada pela execucao
- Validacao: `task clusters:create && kubectl --context k3d-loopzero-mgmt cluster-info`

#### Riscos conhecidos
- Docker precisa estar rodando
- Memoria: 3 clusters k3d com 3 nodes cada = ~6GB RAM minimo

---

### Task S0.3: Taskfile + Helm — infra:install

**Componente:** infra
**Agent:** infra-engineer
**Spec:** §19.2 — Taskfile, §7 — System components, §22 — Toolchain
**Depende de:** S0.2
**Estimativa:** G (dia inteiro)

#### O que implementar
- Task `infra:install` que instala todos os componentes de plataforma via Helm:
  - **mgmt cluster:** PostgreSQL, Gitea, MinIO, ArgoCD, Traefik, cert-manager
  - **build cluster:** Argo Workflows
  - **workload cluster:** Traefik (ingress para workloads)
- Helm values files em `infra/helm/` para cada componente
- Cada instalacao deve:
  - Usar versao fixa (nunca `latest`)
  - Ter resource limits
  - Ter readiness/liveness probes
  - Rodar como non-root
  - Esperar readiness antes de prosseguir
- Task idempotente: `helm upgrade --install`

#### Criterios de aceite
- [ ] `task infra:install` instala todos os componentes
- [ ] PostgreSQL acessivel no mgmt cluster
- [ ] Gitea acessivel em localhost:3000
- [ ] MinIO acessivel em localhost:9001
- [ ] ArgoCD acessivel em localhost:30082
- [ ] Argo Workflows acessivel no build cluster
- [ ] Traefik rodando no workload cluster
- [ ] Todos os pods healthy (Running/Ready)
- [ ] Nenhuma imagem usa tag `latest`
- [ ] Todos os containers tem resource limits
- [ ] Idempotente: rodar 2x nao falha
- [ ] Helm values em `infra/helm/` versionados

#### Teste primeiro (TDD)
- Arquivo: `infra/tests/unit/helm-values_test.go`
- Testar: nenhum values file referencia `latest`
- Testar: todos os values tem `resources.limits` definidos
- Testar: todos os values tem `securityContext.runAsNonRoot: true`
- Validacao pos-install: `kubectl get pods -A --field-selector=status.phase!=Running` retorna vazio

#### Riscos conhecidos
- Ordem de instalacao importa: PostgreSQL antes de Gitea (se Gitea usa PG), cert-manager antes de Traefik TLS
- Timeouts de pull de imagem no primeiro setup podem ser longos
- Helm chart versions podem ter breaking changes — fixar versoes

---

## Tasks Operator (CRDs)

---

### Task S0.4: CRD definitions — Tenant, PreviewEnvironment, AppSecret, ClusterCredential, ReconciliationReport

**Componente:** operators
**Agent:** operator-engineer
**Spec:** §7.3.1 (Tenant), §5.3 (PreviewEnvironment), §9 (Data model)
**Depende de:** S0.3 (clusters precisam existir para instalar CRDs)
**Estimativa:** G (dia inteiro)

#### O que implementar
- Projeto Go em `operators/` com kubebuilder scaffold
- 5 CRD type definitions em `operators/api/v1/`:
  - **Tenant:** spec (displayName, plan, environments, gitops, preview, limits) + status (phase, gitopsRepo, conditions)
  - **PreviewEnvironment:** spec (tenantId, projectId, prNumber, branch, expiresAt) + status (phase, url, releaseId, conditions)
  - **AppSecret:** spec (tenantId, projectId, secretName, encryptedValue) + status (phase, syncedAt, hash)
  - **ClusterCredential:** spec (clusterName, kubeconfig, role) + status (phase, lastVerified)
  - **ReconciliationReport:** spec (tenantId, projectId, environment) + status (lastReconciled, drift, conditions)
- Gerar manifests com `make manifests`
- Instalar CRDs nos clusters: `kubectl apply -f config/crd/bases/`
- Task `crds:install` no Taskfile

#### Criterios de aceite
- [ ] Tipos Go compilam sem erro (`go build ./...`)
- [ ] `make generate` e `make manifests` rodam sem erro
- [ ] 5 CRD manifests gerados em `operators/config/crd/bases/`
- [ ] Todos os manifests passam `kubeconform --strict`
- [ ] CRDs instalados no mgmt cluster: `kubectl get crd | grep loopzero.io`
- [ ] Tenant CRD tem todos os campos de §7.3.1
- [ ] PreviewEnvironment CRD tem campos de §5.3
- [ ] Task `crds:install` funciona e e idempotente

#### Teste primeiro (TDD)
- Arquivo: `operators/api/v1/tenant_types_test.go`
- Testar: Tenant CR pode ser instanciado com todos os campos
- Testar: defaults aplicados (plan default = "starter")
- Arquivo: `operators/api/v1/preview_types_test.go`
- Testar: PreviewEnvironment CR instancia corretamente
- Arquivo: `operators/api/v1/zz_validation_test.go`
- Testar: CRD manifests gerados sao YAML valido

#### Riscos conhecidos
- kubebuilder version precisa estar alinhada com Go version
- CRDs do ArgoCD (Application) precisam existir para que Tenant reference — instalar ArgoCD CRDs como dependencia

---

## Tasks API (Schema + State Machines TS)

---

### Task S0.5: PostgreSQL schema — migrations iniciais

**Componente:** api
**Agent:** api-engineer
**Spec:** §9 — Data model (todas as tabelas)
**Depende de:** S0.3 (PostgreSQL precisa estar rodando)
**Estimativa:** G (dia inteiro)

#### O que implementar
- Projeto TypeScript em `api/` com Drizzle ORM
- Schema Drizzle em `api/src/db/schema/`:
  - **builds:** id (build-{8hex}), tenant_id, project_id, environment, status, correlation_id, created_at, created_by, started_at, finished_at, error_message, duration_seconds
  - **releases:** id (rel-{8hex}), build_id, tenant_id, project_id, environment, status, correlation_id, created_at, created_by, git_commit, git_mirror_status, artifacts (JSONB), side_effects (JSONB)
  - **environment_targets:** tenant_id, project_id, environment, desired_release_id, active_release_id, updated_at, updated_by, correlation_id — PK (tenant_id, project_id, environment)
  - **audit_log:** id, tenant_id, action, actor, resource_type, resource_id, correlation_id, metadata (JSONB), created_at
  - **idempotency_keys:** key, tenant_id, endpoint, request_hash, response_status, response_body (JSONB), created_at, expires_at
  - **sse_events:** id, tenant_id, channel, event_type, payload (JSONB), correlation_id, created_at, sequence
- Forward-only migrations em `api/src/db/migrations/`
- Migration script no Taskfile: `task db:migrate`

#### Criterios de aceite
- [ ] Schema Drizzle compila sem erro
- [ ] `task db:migrate` executa migrations no PostgreSQL
- [ ] Todas as 6 tabelas criadas com colunas conforme §9
- [ ] `environment_targets` tem PK composta (tenant_id, project_id, environment)
- [ ] `idempotency_keys` tem index em (key, tenant_id)
- [ ] `builds.id` segue formato `build-{8hex}`
- [ ] `releases.id` segue formato `rel-{8hex}`
- [ ] Migrations sao forward-only (sem DROP)
- [ ] JSONB usado para artifacts, side_effects, metadata, payload, response_body
- [ ] Foreign key: releases.build_id → builds.id
- [ ] Foreign key: environment_targets.desired_release_id → releases.id

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/db/schema.test.ts`
- Testar: schema Drizzle exporta todas as 6 tabelas
- Testar: formatos de ID (build-{8hex}, rel-{8hex}) com regex
- Testar: environment_targets PK e composta
- Arquivo: `api/tests/unit/db/id-generation.test.ts`
- Testar: `generateBuildId()` retorna formato `build-{8hex}`
- Testar: `generateReleaseId()` retorna formato `rel-{8hex}`
- Testar: IDs sao unicos (gerar 1000, nenhuma colisao)

#### Riscos conhecidos
- Conexao com PostgreSQL do cluster k3d precisa de port-forward ou nodeport
- Drizzle migrations precisam de `drizzle-kit` configurado corretamente

---

### Task S0.6: State machines — TypeScript (API side)

**Componente:** api
**Agent:** api-engineer
**Spec:** §5 — State machines (all 5)
**Depende de:** S0.5
**Estimativa:** G (dia inteiro)

#### O que implementar
- Modulo `api/src/state-machines/` com implementacoes TypeScript dos 5 state machines:
  - **Build (§5.1):** QUEUED → RUNNING → SUCCEEDED/FAILED/CANCELLED
  - **Release (§5.2):** READY → PENDING → ACTIVE/SUPERSEDED/ROLLED_BACK + error states
  - **PreviewEnvironment (§5.3):** CREATING → ACTIVE → EXPIRING → DELETED + error states
  - **Promotion (§5.4):** VALIDATING → COMMITTING → SYNCING → COMPLETED + error states
  - **Rollback (§5.5):** VALIDATING → REVERTING → SYNCING → COMPLETED + error states
- Cada state machine deve:
  - Definir todos os estados como enum
  - Definir todas as transicoes validas
  - Funcao `transition(currentState, event) → newState | Error`
  - **Transicao invalida = throw (programming error, nao runtime error)** — conforme Rule 8
  - Timeouts definidos como constantes (QUEUE_TIMEOUT=5min, BUILD_TIMEOUT=30min, etc.)
- Tipos compartilhados exportados para uso em routes e services

#### Criterios de aceite
- [ ] 5 state machines implementados com todos os estados do §5
- [ ] Transicoes validas retornam novo estado
- [ ] Transicoes invalidas lancam erro (throw, nao return null)
- [ ] Timeouts definidos como constantes com valores do spec
- [ ] Build: QUEUED → RUNNING → SUCCEEDED funciona
- [ ] Build: SUCCEEDED → qualquer coisa = throw
- [ ] Release: READY → PENDING → ACTIVE funciona
- [ ] Release: ACTIVE → SUPERSEDED funciona (novo deploy)
- [ ] PreviewEnvironment: cleanup order respeitada
- [ ] Tipos exportados e reutilizaveis

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/state-machines/build.test.ts`
- Testar: todas as transicoes validas do build (happy path + error paths)
- Testar: transicoes invalidas throw (SUCCEEDED → RUNNING, FAILED → QUEUED, etc.)
- Testar: estados terminais nao aceitam nenhuma transicao
- Arquivo: `api/tests/unit/state-machines/release.test.ts`
- Testar: happy path READY → PENDING → ACTIVE
- Testar: error path PENDING → SYNC_FAILED → DRIFT_ALERT
- Testar: ACTIVE → SUPERSEDED (novo release)
- Testar: rollback path ACTIVE → ROLLING_BACK → ROLLED_BACK
- Arquivo: `api/tests/unit/state-machines/preview.test.ts`
- Testar: lifecycle completo CREATING → ACTIVE → EXPIRING → DELETED
- Testar: error path CREATING → FAILED → DELETING → DELETED
- Arquivo: `api/tests/unit/state-machines/promotion.test.ts`
- Testar: happy path e error paths
- Arquivo: `api/tests/unit/state-machines/rollback.test.ts`
- Testar: happy path e error paths (REJECTED, BLOCKED)

#### Riscos conhecidos
- State machines Go e TS precisam ser consistentes — definir spec como fonte unica
- Timeouts no TS sao constantes de configuracao, nao timers reais (timers sao responsabilidade do caller)

---

## Tasks Operator (State Machines Go)

---

### Task S0.7: State machines — Go (Operator side)

**Componente:** operators
**Agent:** operator-engineer
**Spec:** §5 — State machines (all 5)
**Depende de:** S0.4
**Estimativa:** G (dia inteiro)

#### O que implementar
- Pacote `operators/internal/statemachine/` com implementacoes Go dos 5 state machines:
  - **Build (§5.1):** mesmas transicoes que TS
  - **Release (§5.2):** mesmas transicoes
  - **PreviewEnvironment (§5.3):** mesmas transicoes
  - **Promotion (§5.4):** mesmas transicoes
  - **Rollback (§5.5):** mesmas transicoes
- Cada state machine deve:
  - Estados como `type State string` com constantes
  - Eventos como `type Event string` com constantes
  - Funcao `Transition(current State, event Event) (State, error)`
  - **Transicao invalida = panic** — conforme Rule 8 (programming error)
  - Timeouts como constantes
- Interface comum `StateMachine` para todos

#### Criterios de aceite
- [ ] 5 state machines implementados em Go
- [ ] Transicoes identicas ao TypeScript (mesma spec)
- [ ] Transicoes invalidas fazem panic (Rule 8)
- [ ] Timeouts como constantes com mesmos valores
- [ ] `go build ./...` compila sem erro
- [ ] Interface `StateMachine` implementada por todos

#### Teste primeiro (TDD)
- Arquivo: `operators/internal/statemachine/build_test.go`
- Testar (Ginkgo): todas as transicoes validas do build
- Testar: transicoes invalidas fazem panic (usar `Expect(func() { ... }).To(Panic())`)
- Testar: estados terminais nao aceitam eventos
- Arquivo: `operators/internal/statemachine/release_test.go`
- Testar: happy path e error paths
- Arquivo: `operators/internal/statemachine/preview_test.go`
- Testar: lifecycle completo e cleanup order
- Arquivo: `operators/internal/statemachine/promotion_test.go`
- Testar: happy path e error paths
- Arquivo: `operators/internal/statemachine/rollback_test.go`
- Testar: happy path e error paths

#### Riscos conhecidos
- Go panics sao diferentes de TS throws — documentar que panic e intencional para programming errors
- Manter paridade exata com TS: qualquer mudanca na spec deve atualizar ambos

---

### Task S0.8: Unit tests Go — namespace naming, release ID, error types

**Componente:** operators
**Agent:** operator-engineer
**Spec:** §9 — Data model (naming conventions), §14 — Multi-tenancy
**Depende de:** S0.7
**Estimativa:** M (meio dia)

#### O que implementar
- Pacote `operators/internal/naming/`:
  - `NamespaceName(tenant, project, env) string` → `{tenant}-{project}-{env}`
  - `BuildNamespaceName(tenant) string` → `build-{tenant}`
  - `ReleaseID() string` → `rel-{8hex}`
  - `BuildID() string` → `build-{8hex}`
- Pacote `operators/internal/errors/`:
  - `LoopZeroError` struct com Code, Message, HTTPStatus
  - Erros tipados: `ErrLockTimeout`, `ErrInvalidTransition`, `ErrTenantNotFound`, etc.
- Validacao de nomes: Kubernetes naming rules (lowercase, alphanumeric, hyphens, max 63 chars)

#### Criterios de aceite
- [ ] `NamespaceName("acme", "my-saas", "production")` → `"acme-my-saas-production"`
- [ ] `BuildNamespaceName("acme")` → `"build-acme"`
- [ ] Nomes gerados sao validos para Kubernetes (RFC 1123)
- [ ] IDs sao unicos e seguem formato correto
- [ ] Error types tem Code e Message
- [ ] `go test ./operators/internal/...` passa

#### Teste primeiro (TDD)
- Arquivo: `operators/internal/naming/naming_test.go`
- Testar: namespace naming com diferentes inputs
- Testar: nomes invalidos para Kubernetes sao rejeitados
- Testar: max 63 chars respeitado
- Testar: build namespace naming
- Arquivo: `operators/internal/naming/id_test.go`
- Testar: release ID formato `rel-{8hex}`
- Testar: build ID formato `build-{8hex}`
- Testar: unicidade (gerar 1000, nenhuma colisao)
- Arquivo: `operators/internal/errors/errors_test.go`
- Testar: LoopZeroError implementa interface `error`
- Testar: erros tipados tem codes corretos

#### Riscos conhecidos
- Nenhum risco significativo

---

## Tasks Cross (CI + Observability)

---

### Task S0.9: Unit tests TypeScript — ID generation, error types

**Componente:** api
**Agent:** api-engineer
**Spec:** §9 — Data model
**Depende de:** S0.6
**Estimativa:** P (uma sessao)

#### O que implementar
- Validar que testes de S0.5 e S0.6 cobrem:
  - ID generation (build-{8hex}, rel-{8hex})
  - Error types (LoopZeroError com codes)
  - State machine transitions (todos os 5)
- Adicionar testes extras se necessario:
  - Namespace naming helpers TS (se existirem no API)
  - Correlation ID generation (UUID v4)

#### Criterios de aceite
- [ ] `npm test` no `api/` passa todos os testes
- [ ] Cobertura: ID generation, error types, state machines
- [ ] Correlation ID e UUID v4 valido

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/correlation-id.test.ts`
- Testar: formato UUID v4
- Testar: unicidade
- Ja coberto em S0.5 e S0.6 — esta task e validacao e gap-fill

#### Riscos conhecidos
- Nenhum

---

### Task S0.10: Prometheus + Grafana no mgmt cluster

**Componente:** infra
**Agent:** infra-engineer
**Spec:** §15 — Observability
**Depende de:** S0.3
**Estimativa:** M (meio dia)

#### O que implementar
- Instalar kube-prometheus-stack via Helm no mgmt cluster
- Configurar Grafana acessivel em localhost:3002 (port mapping do k3d)
- Dashboard basico de cluster health:
  - CPU/memory por node
  - Pods por namespace
  - Pod restarts
- Prometheus acessivel em localhost:9090
- Retention: 15d raw (conforme §15)

#### Criterios de aceite
- [ ] Prometheus rodando e acessivel em localhost:9090
- [ ] Grafana rodando e acessivel em localhost:3002
- [ ] Dashboard "Cluster Health" pre-configurado
- [ ] Metricas de nodes e pods sendo coletadas
- [ ] Retention configurada para 15d
- [ ] Helm values versionados em `infra/helm/monitoring/`

#### Teste primeiro (TDD)
- Nao ha teste unitario — validacao funcional
- Validacao: `curl -s http://localhost:9090/-/healthy` retorna 200
- Validacao: `curl -s http://localhost:3002/api/health` retorna 200

#### Riscos conhecidos
- kube-prometheus-stack e pesado — pode precisar de ajustes de resources para k3d
- Port 3002 pode conflitar — verificar disponibilidade

---

### Task S0.11: OpenTelemetry Collector

**Componente:** infra
**Agent:** infra-engineer
**Spec:** §15 — Observability (tracing), §22 — Toolchain (OpenTelemetry)
**Depende de:** S0.3
**Estimativa:** M (meio dia)

#### O que implementar
- Instalar OpenTelemetry Collector via Helm no mgmt cluster
- Configurar receiver OTLP (gRPC + HTTP)
- Configurar exporter para Prometheus (metricas) e logs (stdout por enquanto)
- Traces receiver ativo mesmo que nao haja dashboards de traces ainda (§15: "receives traces, even if no dashboards yet")
- Retention de traces: 7d (conforme §15)

#### Criterios de aceite
- [ ] OTel Collector rodando no mgmt cluster
- [ ] Receiver OTLP (gRPC 4317, HTTP 4318) ativo
- [ ] Collector aceita spans de teste sem erro
- [ ] Helm values em `infra/helm/otel/`
- [ ] Pod healthy com resource limits

#### Teste primeiro (TDD)
- Nao ha teste unitario — validacao funcional
- Validacao: enviar span de teste via `curl` para OTLP HTTP e verificar 200

#### Riscos conhecidos
- Sem backend de traces persistente neste sprint — traces vao para logs
- Adicionar Jaeger/Tempo em sprint futuro

---

### Task S0.12: Structured logging (JSON)

**Componente:** cross
**Agent:** orchestrator
**Spec:** §15 — Observability ("Structured logging JSON configured for all components")
**Depende de:** S0.3
**Estimativa:** M (meio dia)

#### O que implementar
- **API (TypeScript):** configurar logger estruturado (pino ou similar) com output JSON
  - Campos obrigatorios: `timestamp`, `level`, `message`, `correlation_id`, `component`
  - Nao usar `console.log` em producao
- **Operators (Go):** configurar `slog` com output JSON
  - Campos obrigatorios: `timestamp`, `level`, `msg`, `correlation_id`, `component`
  - Nao usar `fmt.Println` em producao
- Garantir que correlation_id flui do request ate os logs (§15 correlation ID flow)

#### Criterios de aceite
- [ ] API: todos os logs em formato JSON
- [ ] Operators: todos os logs em formato JSON
- [ ] Campo `correlation_id` presente em logs relacionados a requests
- [ ] Campo `component` identifica a origem (api, tenant-controller, etc.)
- [ ] Nenhum `console.log` ou `fmt.Println` no codigo de producao
- [ ] Log level configuravel via env var (LOG_LEVEL)

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/logger.test.ts`
- Testar: log output e JSON valido
- Testar: campos obrigatorios presentes
- Testar: correlation_id propagado
- Arquivo: `operators/internal/logging/logger_test.go`
- Testar: log output e JSON valido
- Testar: campos obrigatorios presentes

#### Riscos conhecidos
- API e Operators ainda nao tem codigo de producao — configurar logger base para uso futuro
- Pino (TS) e slog (Go) sao escolhas maduras e alinhadas com §22

---

### Task S0.13: CI pipeline — unit tests + lint

**Componente:** cross
**Agent:** orchestrator
**Spec:** Sprint 0 task list ("CI: unit tests + lint")
**Depende de:** S0.8, S0.9, S0.12
**Estimativa:** M (meio dia)

#### O que implementar
- GitHub Actions workflow `.github/workflows/ci.yml`:
  - Trigger: push to any branch, PR to main
  - Jobs paralelos:
    - `test-go`: `go test ./operators/... -short`
    - `test-ts`: `pnpm --filter @loopzero/api test:unit && pnpm --filter @loopzero/cli test:unit`
    - `lint-go`: `golangci-lint run ./operators/...`
    - `lint-ts`: `eslint` em api/ e cli/
    - `validate-manifests`: `kubeconform --strict` nos CRD manifests
  - Cache: Go modules, pnpm store
  - Nao precisa de clusters k3d (unit tests apenas)

#### Criterios de aceite
- [ ] Workflow CI roda em push e PR
- [ ] Testes Go passam
- [ ] Testes TS passam
- [ ] Lint Go passa
- [ ] Lint TS passa
- [ ] Manifests validados com kubeconform
- [ ] Cache de dependencias configurado
- [ ] Pipeline completa em < 5 minutos

#### Teste primeiro (TDD)
- Nao ha teste do CI em si — validacao pelo primeiro PR
- Validacao: `act` (ferramenta local para GitHub Actions) ou push de teste

#### Riscos conhecidos
- `kubeconform` precisa dos schemas dos CRDs custom — pode precisar de flag `--skip` para CRDs proprios
- `golangci-lint` pode ser lento na primeira execucao

---

### Task S0.14: Taskfile — setup + test:unit (integracao final)

**Componente:** infra
**Agent:** infra-engineer
**Spec:** §19.2 — Taskfile, Sprint 0 exit gate
**Depende de:** S0.2, S0.3, S0.4, S0.8, S0.9, S0.10, S0.11
**Estimativa:** M (meio dia)

#### O que implementar
- Task `setup` no Taskfile que orquestra tudo:
  ```yaml
  setup:
    cmds:
      - task: clusters:create
      - task: infra:install
      - task: crds:install
      - task: db:migrate
  ```
- Task `test:unit`:
  ```yaml
  test:unit:
    cmds:
      - go test ./operators/... -short
      - pnpm --filter @loopzero/api test:unit
      - pnpm --filter @loopzero/cli test:unit
  ```
- Task `lint`:
  ```yaml
  lint:
    cmds:
      - golangci-lint run ./operators/...
      - pnpm --filter @loopzero/api lint
      - pnpm --filter @loopzero/cli lint
  ```
- Task `validate:manifests`:
  ```yaml
  validate:manifests:
    cmds:
      - kubeconform --strict operators/config/crd/bases/
  ```
- Task `dev:up` (alias para Tilt):
  ```yaml
  dev:up:
    cmds:
      - task: setup
      - tilt up
  ```

#### Criterios de aceite
- [ ] `task setup` executa do zero ate clusters + infra + CRDs + schema
- [ ] `task test:unit` roda todos os testes unitarios
- [ ] `task lint` roda todos os linters
- [ ] `task validate:manifests` valida CRDs
- [ ] `task dev:up` sobe ambiente completo
- [ ] Todas as tasks sao idempotentes

#### Teste primeiro (TDD)
- Nao ha teste unitario — validacao pela execucao end-to-end
- Validacao: `task setup && task test:unit && task lint && task validate:manifests`

#### Riscos conhecidos
- `task setup` pode levar 10+ minutos no primeiro run (pull de imagens)
- Tilt precisa de Tiltfile configurado — pode ser placeholder neste sprint

---

## Exit Gate Sprint 0

```bash
# 1. Setup funciona do zero
task setup                                    # exit 0

# 2. Clusters rodando
kubectl --context k3d-loopzero-mgmt cluster-info  # OK
kubectl --context k3d-loopzero-build cluster-info # OK
kubectl --context k3d-loopzero-workload cluster-info # OK

# 3. CRDs instalados
kubectl --context k3d-loopzero-mgmt get crd | grep loopzero.io
# tenants.loopzero.io
# previewenvironments.loopzero.io
# appsecrets.loopzero.io
# clustercredentials.loopzero.io
# reconciliationreports.loopzero.io

# 4. PostgreSQL com schema
kubectl --context k3d-loopzero-mgmt exec -it deploy/postgresql -- \
  psql -U loopzero -d loopzero -c "\dt"
# builds, releases, environment_targets, audit_log, idempotency_keys, sse_events

# 5. Testes passam
task test:unit                                # all green
task lint                                     # no errors
task validate:manifests                       # kubeconform OK

# 6. Observability
curl -s http://localhost:9090/-/healthy       # Prometheus OK
curl -s http://localhost:3002/api/health      # Grafana OK
# Grafana dashboard "Cluster Health" mostra metricas

# 7. State machines
# Todos os 5 state machines testados (Go + TS)
# Transicoes invalidas fazem panic/throw
```

---

## Resumo de Tasks

| Task | Titulo | Componente | Agent | Estimativa | Depende de |
|------|--------|------------|-------|------------|------------|
| S0.1 | k3d cluster configs | infra | infra-engineer | P | nenhuma |
| S0.2 | Taskfile clusters:create | infra | infra-engineer | M | S0.1 |
| S0.3 | Helm infra:install | infra | infra-engineer | G | S0.2 |
| S0.4 | CRD definitions (5 CRDs) | operators | operator-engineer | G | S0.3 |
| S0.5 | PostgreSQL schema (6 tabelas) | api | api-engineer | G | S0.3 |
| S0.6 | State machines TypeScript (5) | api | api-engineer | G | S0.5 |
| S0.7 | State machines Go (5) | operators | operator-engineer | G | S0.4 |
| S0.8 | Unit tests Go (naming, IDs, errors) | operators | operator-engineer | M | S0.7 |
| S0.9 | Unit tests TS (IDs, correlation, validation) | api | api-engineer | P | S0.6 |
| S0.10 | Prometheus + Grafana | infra | infra-engineer | M | S0.3 |
| S0.11 | OpenTelemetry Collector | infra | infra-engineer | M | S0.3 |
| S0.12 | Structured logging (JSON) | cross | orchestrator | M | S0.3 |
| S0.13 | CI pipeline (tests + lint) | cross | orchestrator | M | S0.8, S0.9, S0.12 |
| S0.14 | Taskfile setup + test:unit | infra | infra-engineer | M | todos |

**Totais:** 14 tasks | 5 G + 7 M + 2 P | ~2 semanas de trabalho paralelo nas 3 trilhas
