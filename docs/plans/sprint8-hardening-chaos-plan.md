# Plan: Sprint 8 — Phase 2a: Destravar Escala 50k Apps

> **Version 2.0** — O objetivo do LoopZero e suportar 50k apps em producao via cell architecture (5k apps/cell x 10 cells). O bloqueador hoje e Phase 2a (ADR-0010): 7 prerequisitos obrigatorios antes de ativar cell-2. Sprint 8 ataca TODOS os 7 deliverables de Phase 2a, executa os chaos tests como Gate 2a exige, e estabiliza L3 (100 tenants >= 95/100) que e o bloqueador #1. Ao final, Phase 2a deve estar completa ou com gap documentado e path claro, desbloqueando Phase 2b (cell-2 activation) na Sprint 9.

## Context

### O objetivo: 50k apps

O LoopZero e desenhado para escalar a 50k apps via cell architecture (ADR-0008):
- **1 cell = 1 workload cluster** com ~5k apps (dentro do limite K8s de 150k pods)
- **10 cells** = 50k apps, escalamento linear
- Control plane centralizado: API + PG + Gitea + Argo Workflows + ArgoCD (central ate Phase 3)
- Routing via PostgreSQL (`tenants.cell_id`), nao CF KV (ADR-0010 veto)

### O que bloqueia: Phase 2a (ADR-0010)

ADR-0010 define 7 prerequisitos obrigatorios antes de provisionar cell-2:

| # | Deliverable | Status Atual | Gap |
|---|---|---|---|
| 1 | **L3 10x consecutivo >= 95/100** | Melhor: 75/100 | **BLOQUEADOR PRINCIPAL** — 20 pontos abaixo |
| 2 | **PG max_connections >= 100** | `dbNodeClass: small` (db-s-1vcpu-2gb) | Config change em Pulumi |
| 3 | **Gitea HA** (2 replicas + shared storage + 200 concurrent clones) | Parcial: Redis done, replicas=1 | Requer NFS/S3 git storage + multi-replica |
| 4 | **Harbor HA** (2+ core + Redis HA + S3 shared) | core=1, registry=1, Redis interno | Config changes + Redis HA |
| 5 | **MinIO distributed** | Plano referenciado mas NAO encontrado | Requer plano + implementacao |
| 6 | **mgmt cluster memory < 50%** | 74% idle | Scale nodes: small→medium ou +nodes |
| 7 | **tenant-health Grafana dashboard** | Nao existe | Novo dashboard |

**Gate 2a Exit:** Todos os 7 + chaos test (kill each shared service, recovery <2min) + 5 consecutive L3 >= 98/100.

### O que existe e nunca foi executado

**Load tests (L1-L5):** Harness real completo em `infra/tests/load/`. L1-L3 rodados, L4/L5 nunca.
**Chaos tests (9 cenarios):** 388 LOC + 9 manifests Chaos Mesh em `infra/tests/chaos/`. Nunca executados contra DO.
**Capacity tests (10 suites):** 2616 LOC em `infra/tests/capacity/`. Validados.

### Infraestrutura atual (Pulumi.dev.yaml)

```
mgmtNodeClass: small     ← precisa medium (memoria)
mgmtMinNodes: 3          ← fixo (sem auto-scale)
mgmtMaxNodes: 3          ← precisa 5 (headroom)
dbNodeClass: small        ← precisa medium (connections)
runtimeNodeClass: small   ← OK per-cell
buildNodeClass: medium    ← OK
Harbor core: 1 replica    ← precisa >= 2
Gitea: 1 replica          ← precisa >= 2
```

## Objective

**Done = Phase 2a Gate Exit atingido, desbloqueando cell-2 (Phase 2b) na Sprint 9.**

Metas mensuraveis:
1. L3 >= 95/100 em 5 execucoes consecutivas (Gate #1 de ADR-0010)
2. PG bumped para `dbNodeClass: medium` (max_connections >= 100)
3. Gitea HA: 2 replicas + shared storage + 200 concurrent clones validado
4. Harbor HA: 2+ core replicas + Redis externo (DO managed)
5. MinIO distributed: plano criado + implementado
6. mgmt cluster: `mgmtNodeClass: medium`, memory idle < 50%
7. tenant-health Grafana dashboard criado e funcional
8. 9/9 chaos tests passam contra DO (shared service kill + recovery <2min)
9. L4 (500 tenants) executado com resultados documentados
10. Observability gaps fechados (alerts orfas, runbooks, SLO formal)

## ADRs

### D1 — L3 stability e o bloqueador critico (nao chaos, nao observability)
- **Decision:** L3 >= 95/100 e a prioridade #1 da sprint. Todo o resto e secundario.
- **Rationale:** ADR-0010 Gate #1. Sem L3 estavel, os outros 6 deliverables sao irrelevantes — nao passamos no gate. O gap e 20+ pontos (75→95), indicando problemas sistemicos que precisam de diagnostico + fix iterativo.
- **Consequences:** Se L3 nao estabilizar, Sprint 8 falha. Outros deliverables continuam em paralelo mas nao compensam L3.

### D2 — Infrastructure scale-up via Pulumi config (nao refactoring)
- **Decision:** PG bump, mgmt memory, Harbor HA, e Gitea replicas sao config changes em Pulumi, nao redesign.
- **Rationale:** Pulumi.dev.yaml ja suporta todos os parametros necessarios. `dbNodeClass: medium` e um valor em YAML. `mgmtNodeClass: medium` idem. Harbor `core.replicas: 2` e Helm value. Nao ha codigo novo — apenas escalar o que ja existe.
- **Consequences:** Custo mensal aumenta (~$100-200/mo adicional para nodes medium + PG medium). Tradeoff aceito: investimento em infraestrutura para desbloquear 50k.

### D3 — Gitea HA via NFS (nao S3 git storage)
- **Decision:** Usar DigitalOcean Volumes (NFS) como shared storage para Gitea replicas, nao S3 para git data.
- **Rationale:** Gitea Community Edition nao suporta S3 como backend para repositorios git (apenas para LFS, attachments, avatars — ja configurado). Git operations requerem filesystem POSIX. DO Volumes com ReadWriteMany (via NFS) e a opcao viavel. Alternativa: Gitea Cluster (Enterprise only, rejeitado por custo).
- **Consequences:** Requer DO Volume com NFS access mode. Performance de git clone depende de latencia NFS. Validar com load test de 200 concurrent clones.

### D4 — MinIO distributed via erasure coding (4 nodes)
- **Decision:** Migrar MinIO de single-instance para distributed mode com 4 drives (2 nodes x 2 drives) usando erasure coding.
- **Rationale:** MinIO single-instance e SPOF para build artifacts. Com 100+ tenants, perda de MinIO = perda de todos os bundles. Erasure coding (EC:2) sobrevive a perda de 2 drives. 4 drives e o minimo para distributed mode.
- **Consequences:** Requer 4 PVCs (DO Volumes). Migration path: criar novo cluster MinIO distributed, copiar dados, trocar endpoint. Downtime aceitavel em dev (~30min).

### D5 — Chaos tests servem ao Gate 2a (nao sao objetivo isolado)
- **Decision:** Chaos tests sao executados como parte do Gate 2a exit, nao como exercicio academico.
- **Rationale:** Gate 2a exige: "chaos test proving each shared service can be killed independently with recovery <2min". Os 9 cenarios existentes cobrem exatamente isso (API, controller, PG, ArgoCD, Gitea, build node, simultaneous crash). Executar e validar — nao escrever mais.
- **Consequences:** Se algum cenario falha, o fix e prioridade (bloqueia Gate 2a).

### D6 — L4 (500 tenants) como validation, nao exploration
- **Decision:** L4 e executado como validacao de capacidade da celula unica, informando dimensionamento de cell-2.
- **Rationale:** 5k apps/cell e o target por ADR-0008. L4 (500 tenants) valida ~10% da capacidade target. Se L4 falha, o design de 5k/cell precisa ser revisado antes de provisionar cell-2. L4 nao e gate formal, mas resultado informa dimensionamento.
- **Consequences:** Se L4 < 90% success: revisar capacity assumptions antes de Phase 2b.

### D7 — SLO formal como contrato de escala
- **Decision:** Documentar SLOs em `docs/slo.md` focados em metricas de escala, nao apenas uptime.
- **Rationale:** 50k apps requer SLOs de escala: deploy throughput, build queue latency, sync convergence time. SLOs de uptime sao necessarios mas insuficientes para validar que o sistema escala.
- **Consequences:** SLOs de escala informam decisoes de cell sizing e Gate 2b.

### D8 — Observability hardening suporta operacao multi-cell
- **Decision:** Resolver alerts orfas, criar runbooks, e deploy Promtail no build cluster como pre-requisito para operar 2+ cells.
- **Rationale:** Multi-cell sem observability cross-cluster e operacionalmente inviavel. Gate 2b exige: "per-cell Prometheus with remote-write" e "cell:degraded Alertmanager aggregation". Sprint 8 prepara a base.
- **Consequences:** Observability work e investimento para multi-cell, nao overhead de compliance.

## Dependency Graph

```
Phase 0 (Infra Scale-Up) ──▶ Phase 1 (L3 Stabilization) ──▶ Phase 3 (L4 Validation)
        │                            │                              │
        │                            │                              ▼
        ▼                            ▼                      Phase 4 (Gate 2a Exit)
Phase 2 (Shared Service HA)   Phase 2 (Shared Service HA)
  ├─ Gitea HA                        │
  ├─ Harbor HA                       │
  ├─ MinIO Distributed               │
  └─ mgmt memory                     │
                                     ▼
                              Phase 5 (Chaos Gate)
                                     │
                                     ▼
                              Phase 4 (Gate 2a Exit)

Phase 6 (Observability) ── paralelo com tudo ──
```

- **Phase 0** (infra config) desbloqueia **tudo** (L3 precisa de PG medium para nao saturar)
- **Phase 1** (L3) e o **bloqueador critico** — sem L3 >= 95/100, Gate 2a falha
- **Phase 2** (HA) roda em paralelo com Phase 1 — Gitea/Harbor/MinIO nao dependem de L3
- **Phase 3** (L4) depende de Phase 1 (L3 estavel primeiro)
- **Phase 5** (chaos) depende de Phases 0+2 (shared services HA antes de testar kill/recovery)
- **Phase 4** (Gate 2a Exit) e o checkpoint final — requer Phases 1+2+5 completas
- **Phase 6** (observability) e paralela e suporta todos os outros phases

---

## Phase 0: Infrastructure Scale-Up

**Objective:** Escalar infraestrutura DO para suportar Phase 2a workload (100-500 tenants + HA services).

### T0.1 — PG bump: dbNodeClass small → medium

#### Objective
Escalar PostgreSQL para `db-s-2vcpu-4gb` (max_connections >= 100) para eliminar bottleneck de connection pool.

#### Evidence
ADR-0010 deliverable #2: "PG bump — max_connections >= 100 (db-s-2vcpu-4gb minimum)". Pulumi.dev.yaml atual: `dbNodeClass: small` = db-s-1vcpu-2gb (~25 connections). MEET-051 analise: "PG connection pool at 72% idle saturation". Com 100 tenants (L3), cada deploy usa 1 advisory lock connection + 1 query connection = 200 connections nos picos.

#### Files to edit
```
infra/pulumi/Pulumi.dev.yaml — dbNodeClass: small → medium
```

#### Deep file dependency analysis
- `Pulumi.dev.yaml` (49 LOC): stack config. Unica mudanca: linha 24 `dbNodeClass: small` → `medium`.
- `infra/pulumi/src/config/stack.ts`: le `dbNodeClass` como `NodeClass` type. `medium` ja e valor valido.
- `infra/pulumi/src/components/infra.ts`: usa `config.dbNodeClass` para provisionar DO managed PG. Size mapping: `small` = db-s-1vcpu-2gb, `medium` = db-s-2vcpu-4gb.
- Downstream: zero code changes. PG managed faz resize com failover automatico (~5min downtime).

#### Tasks
1. Alterar `Pulumi.dev.yaml` linha 24: `loopzero-infra:dbNodeClass: medium`
2. `cd infra/pulumi && npx pulumi preview --stack dev` — confirmar que unica mudanca e PG resize
3. `cd infra/pulumi && npx pulumi up --stack dev` — aplicar (esperar failover ~5min)
4. Validar: `SELECT setting FROM pg_settings WHERE name = 'max_connections'` retorna >= 100

#### TDD
```
RED:     max_connections < 100 (estado atual)
GREEN:   pulumi up aplica resize
REFACTOR: None
VERIFY:  psql -c "SHOW max_connections" retorna >= 100
```

#### Acceptance Criteria
- [ ] `dbNodeClass: medium` no Pulumi.dev.yaml
- [ ] PG resize concluido sem data loss
- [ ] max_connections >= 100 confirmado
- [ ] Aplicacao reconecta automaticamente apos failover
- [ ] `pulumi preview` mostra zero drift apos apply

#### DoD
- [ ] PG medium ativo
- [ ] max_connections >= 100
- [ ] API healthy apos resize

---

### T0.2 — mgmt cluster: small → medium + auto-scale

#### Objective
Escalar mgmt cluster para reduzir memory idle de 74% para < 50% e habilitar auto-scale para headroom.

#### Evidence
ADR-0010 deliverable #6: "mgmt cluster memory headroom — below 50% idle". Atual: `mgmtNodeClass: small` (2vCPU/4GB per node x 3 nodes = 12GB total). Com HA replicas de Gitea, Harbor, ArgoCD, Prometheus, Loki: ~8.9GB usado = 74% saturacao. Com `medium` (4vCPU/8GB per node x 3 = 24GB total), mesmo workload = ~37% saturacao.

#### Files to edit
```
infra/pulumi/Pulumi.dev.yaml — mgmtNodeClass: medium, mgmtMaxNodes: 5
```

#### Deep file dependency analysis
- `Pulumi.dev.yaml`: linhas 10-12. `mgmtNodeClass: small` → `medium`, `mgmtMaxNodes: 3` → `5`.
- `infra/pulumi/src/components/infra.ts`: usa `config.mgmtNodeClass` e `config.mgmtMaxNodes` para DOKS cluster spec. Auto-scale habilitado quando `maxNodes > minNodes`.
- Downstream: DOKS faz rolling upgrade dos nodes (um por vez, ~15min total). Pods re-schedulam automaticamente.

#### Tasks
1. Alterar Pulumi.dev.yaml: `mgmtNodeClass: medium`, `mgmtMaxNodes: "5"`
2. `pulumi preview --stack dev` — confirmar node pool resize
3. `pulumi up --stack dev` — aplicar rolling upgrade
4. Monitorar: `kubectl top nodes` — confirmar memory < 50%
5. Validar: todos os pods healthy apos rolling upgrade

#### TDD
```
RED:     kubectl top nodes mostra > 50% memory usage
GREEN:   pulumi up com medium nodes
REFACTOR: None
VERIFY:  kubectl top nodes mostra < 50% memory em todos os nodes
```

#### Acceptance Criteria
- [ ] `mgmtNodeClass: medium` no Pulumi.dev.yaml
- [ ] `mgmtMaxNodes: 5` (auto-scale habilitado)
- [ ] Memory idle < 50% apos upgrade
- [ ] Zero downtime durante rolling upgrade (pods re-scheduled)
- [ ] Todos os pods running apos upgrade

#### DoD
- [ ] mgmt cluster com nodes medium
- [ ] Memory < 50% confirmado
- [ ] Auto-scale funcional (min=3, max=5)

---

### T0.3 — Instalar Chaos Mesh nos clusters DO

#### Objective
Instalar Chaos Mesh 2.7.0 nos 3 clusters DOKS para habilitar chaos tests do Gate 2a.

#### Evidence
Gate 2a exige: "chaos test proving each shared service can be killed independently with recovery <2min". Chaos tests em `infra/tests/chaos/` requerem Chaos Mesh CRDs. 9 manifests ja existem.

#### Files to edit
```
infra/helm/chaos-mesh/values.yaml — (NEW) Helm values
infra/helm/chaos-mesh/Chart.yaml — (NEW) Chart dependency
Taskfile.yml — task chaos:install
```

#### Deep file dependency analysis
- `Taskfile.yml` (1695 LOC): adicionar tasks `chaos:install` e `chaos:uninstall`.
- `infra/tests/chaos/manifests/`: 9 YAML files que referenciam PodChaos, StressChaos, NetworkChaos CRDs.
- Chaos Mesh namespace: `chaos-mesh` (isolado de workloads).

#### Tasks
1. Criar `infra/helm/chaos-mesh/Chart.yaml` com Chaos Mesh 2.7.0 dependency
2. Criar `infra/helm/chaos-mesh/values.yaml`: containerd runtime, dashboard disabled, non-root
3. Adicionar `task chaos:install` no Taskfile.yml
4. Executar `task chaos:install` nos 3 clusters
5. Validar: `kubectl get crd podchaos.chaos-mesh.org` retorna Found

#### TDD
```
RED:     kubectl get crd podchaos.chaos-mesh.org — NotFound
GREEN:   task chaos:install
VERIFY:  3 CRDs registrados em cada cluster
```

#### Acceptance Criteria
- [ ] Chaos Mesh 2.7.0 instalado nos 3 clusters
- [ ] CRDs PodChaos, StressChaos, NetworkChaos registrados
- [ ] `task chaos:install` idempotente
- [ ] Helm chart versao pinned

#### DoD
- [ ] 3 clusters com Chaos Mesh
- [ ] `task chaos:install` / `task chaos:uninstall` funcionais

---

## Phase 1: L3 Stabilization (BLOQUEADOR CRITICO)

**Objective:** Atingir L3 >= 95/100 em 5 execucoes consecutivas. Sem isso, Phase 2a falha.

### T1.1 — Executar L3 diagnostico + categorizar falhas

#### Objective
Executar L3 contra DO (apos PG bump de T0.1) e categorizar cada falha por root cause.

#### Evidence
Melhor L3: 75/100. Categorias provaveis (ADR-0010 analise): shared service saturation (Gitea concurrent clones), PG pool exhaustion (antes do bump), BuildKit concurrency ceiling (4/node x 2 = 8 concurrent, L3 usa 10), ArgoCD sync timeout, app verification timeout.

#### Files to edit
```
infra/tests/load/real_deploy_test.go — ajustes de timeout/concurrency se necessario
docs/plans/sprint8-hardening-chaos-plan.md — registrar diagnostico inline
```

#### Deep file dependency analysis
- `real_deploy_test.go`: `TestRealDeploy_L3_100Tenants` com 100 tenants, concurrency 10, timeout 4h. Cada deploy: provision → upload → submit → poll → verify HTTP 200.
- `real_deploy_harness.go`: `RealDeploy()` retorna `FullDeployResult` com stages timing + error categorization.
- Dependencia: T0.1 (PG medium) deve estar completo — PG small satura com 100 concurrent locks.

#### Tasks
1. Confirmar T0.1 completo (PG medium ativo)
2. Executar L3: `LOOPZERO_REAL_DEPLOY=1 LOOPZERO_LOAD_LEVEL=l3 LOOPZERO_API_URL=https://api.dev.loopzero.example LOOPZERO_APP_DOMAIN=dev.loopzero.example LOOPZERO_SERVICE_AUTH=<token> go test ./infra/tests/load/ -run L3 -v -timeout 4h 2>&1 | tee l3-diag-run1.log`
3. Para cada falha: categorizar como `build_timeout | verify_timeout | admission_reject | upload_fail | pg_timeout | argocd_sync_fail | unknown`
4. Coletar metricas Prometheus durante L3: `loopzero_postgres_connections_active`, `loopzero_builds_active`, `loopzero_build_queue_depth`, `loopzero_api_request_duration_seconds`
5. Identificar top 3 root causes
6. Documentar com dados

#### TDD
```
RED:     L3 < 95/100
GREEN:   Fixes nos tasks T1.2-T1.4
VERIFY:  L3 >= 95/100 apos fixes
```

#### Acceptance Criteria
- [ ] L3 executado com logs completos
- [ ] Cada falha categorizada
- [ ] Top 3 root causes com dados quantitativos
- [ ] Metricas Prometheus coletadas durante execucao

#### DoD
- [ ] Diagnostico completo com dados
- [ ] Action items para cada root cause

---

### T1.2 — Fix L3 root cause #1

#### Objective
Corrigir o root cause mais frequente de L3 failures.

#### Evidence
Dependente de T1.1. Area provavel: BuildKit concurrency ceiling (10 concurrent vs 8 max → queuing → timeout). Fix provavel: aumentar `buildMaxNodes` de 2 para 3 (12 concurrent slots) ou ajustar workflow concurrency.

#### Files to edit
```
TBD — dependente de T1.1
Provavel: infra/pulumi/Pulumi.dev.yaml (buildMinNodes/buildMaxNodes)
      ou: api/internal/argo/ (workflow concurrency/timeout config)
```

#### Tasks
1. Escrever teste que reproduz root cause
2. Implementar fix (config ou code)
3. Re-executar L3
4. Documentar melhoria

#### TDD
```
RED:     Test que reproduz root cause #1
GREEN:   Fix minimo
VERIFY:  L3 re-run mostra melhoria
```

#### Acceptance Criteria
- [ ] Root cause #1 tem fix implementado
- [ ] L3 success rate melhora (medido)

#### DoD
- [ ] Fix aplicado
- [ ] L3 re-executado com melhoria documentada

---

### T1.3 — Fix L3 root cause #2

#### Objective
Corrigir o segundo root cause mais frequente.

#### Evidence
Dependente de T1.1. Area provavel: ArgoCD sync timeout ou app verification timeout.

#### Files to edit
```
TBD — dependente de T1.1
```

#### Tasks
1. Escrever teste que reproduz root cause
2. Implementar fix
3. Re-executar L3
4. Documentar melhoria cumulativa

#### TDD
```
RED:     Test que reproduz root cause #2
GREEN:   Fix minimo
VERIFY:  L3 re-run com melhoria cumulativa
```

#### Acceptance Criteria
- [ ] Root cause #2 corrigido
- [ ] L3 success rate cumulativamente melhor

#### DoD
- [ ] Fix aplicado e testado

---

### T1.4 — Fix L3 root cause #3 + 5x validation runs

#### Objective
Corrigir terceiro root cause e executar 5 L3 consecutivos >= 95/100 (Gate #1 + Gate 2a exit criteria).

#### Evidence
Gate 2a Exit: "5 consecutive L3 >= 98/100". Gate #1: "10x >= 95/100". Sprint 8 target: 5x >= 95/100 (progresso para ambos os gates).

#### Files to edit
```
TBD + docs/runbooks/l3-validation-results.md — (NEW)
```

#### Tasks
1. Implementar fix root cause #3 (se necessario)
2. Executar L3 run #1 — documentar resultado
3. Se >= 95/100: executar run #2, senao: voltar ao diagnostico
4. Repetir ate 5 runs consecutivos >= 95/100
5. Documentar todos os resultados com metricas

#### TDD
```
RED:     L3 < 95/100
GREEN:   Fixes acumulados
VERIFY:  5 L3 consecutivos >= 95/100
```

#### Acceptance Criteria
- [ ] 5 L3 consecutivos >= 95/100 (ou melhor resultado com gap documentado)
- [ ] Nenhum data loss durante L3
- [ ] Recovery times dentro dos SLOs

#### DoD
- [ ] 5 runs documentados com resultados e metricas
- [ ] Progresso para Gate #1 de ADR-0010 registrado

---

## Phase 2: Shared Service HA (Paralelo com Phase 1)

**Objective:** Entregar os 4 deliverables de HA de ADR-0010: Gitea, Harbor, MinIO, tenant-health dashboard.

### T2.1 — Gitea HA: 2 replicas + NFS shared storage

#### Objective
Escalar Gitea para 2 replicas com shared storage via DO Volumes (NFS).

#### Evidence
ADR-0010 deliverable #3: "Gitea HA — shared storage + 2 replicas + load test 200 concurrent clones". Gitea atual: 1 replica, PVC local. Redis para sessions/cache ja configurado (`values-ha.yaml`). Git repos estao no PVC — replicar exige shared filesystem.

#### Files to edit
```
infra/helm/gitea/values.yaml — replicas: 2, persistence via NFS
infra/helm/gitea/values-ha.yaml — ajustes para multi-replica
infra/pulumi/src/components/mgmt-gitea.ts — provisionar DO Volume com NFS
infra/tests/capacity/gitea_push_test.go — adicionar test 200 concurrent clones
```

#### Deep file dependency analysis
- `values.yaml`: Gitea Helm chart config. `replicas: 1` → `2`. `persistence.storageClass` precisa mudar para NFS-capable.
- `values-ha.yaml`: overlay com Redis config. Ja configurado para sessions/cache/queues.
- `mgmt-gitea.ts`: Pulumi component. Precisa criar DO Volume e configurar como PVC compartilhado.
- `gitea_push_test.go` (413 LOC): testa push latency. Adicionar cenario de 200 concurrent `git clone`.
- Downstream: ArgoCD le repos do Gitea. Com 2 replicas: load balancing via ClusterIP Service.

#### Deep Dives
- **NFS no DigitalOcean:** DO Volumes suportam ReadWriteOnce (RWO), nao ReadWriteMany (RWX). Para RWX: usar NFS provisioner (nfs-subdir-external-provisioner) montando um DO Volume como backend. Alternativa: Longhorn. Complexidade: media.
- **Gitea multi-replica caveats:** Gitea 1.22+ suporta multi-replica com shared filesystem. `indexer` precisa de Bleve on shared disk ou Elasticsearch externo. Preferir Bleve on NFS (simples).
- **Load test 200 concurrent clones:** Fork do `gitea_push_test.go` com 200 goroutines fazendo `git clone` paralelo. Medir: p50, p95, p99 latency + failure rate.

#### Tasks
1. Instalar NFS provisioner no mgmt cluster: `helm install nfs-provisioner nfs-subdir-external-provisioner/nfs-subdir-external-provisioner --set nfs.path=/data --set nfs.server=<volume-ip>`
2. Criar DO Volume para Gitea data (50GB, SFO3)
3. Configurar PVC com StorageClass NFS
4. Alterar `values.yaml`: `replicas: 2`, persistence apontando para NFS PVC
5. Deploy: `helm upgrade gitea ...`
6. Validar: ambos pods running, git clone funciona em ambos
7. Adicionar test de 200 concurrent clones em `gitea_push_test.go`
8. Executar test: confirmar p95 < 5s para clone e 0% failure rate

#### TDD
```
RED:     TestGiteaConcurrentClones_200() — 200 goroutines clone simultaneo, falha se > 5% error
RED:     TestGiteaReplicas() — kubectl get pods -l app=gitea retorna 2 Running
GREEN:   Deploy Gitea HA
REFACTOR: Tune NFS mount options se latency alta
VERIFY:  LOOPZERO_CAPACITY=1 go test ./infra/tests/capacity/ -run Gitea -v
```

#### Acceptance Criteria
- [ ] 2 replicas Gitea running
- [ ] Shared storage funcional (clone em replica A, push em replica B)
- [ ] 200 concurrent clones: p95 < 5s, failure rate < 1%
- [ ] ArgoCD continua sincronizando sem erro
- [ ] Failover: kill 1 pod, service continua respondendo

#### DoD
- [ ] Gitea HA operacional
- [ ] Load test 200 concurrent clones passando
- [ ] Failover validado

---

### T2.2 — Harbor HA: 2+ core replicas + Redis externo

#### Objective
Escalar Harbor para 2+ core/registry replicas com Redis externo (DO managed).

#### Evidence
ADR-0010 deliverable #4: "Harbor HA — 2+ core replicas + Redis HA + S3 shared". Atual: core=1, registry=1, Redis interno single-instance. S3 backend ja configurado (DO Spaces). PG externo ja configurado.

#### Files to edit
```
infra/helm/harbor/values.yaml — core.replicas: 2, registry.replicas: 2, Redis externo
infra/pulumi/src/components/harbor-bootstrap.ts — configurar Redis managed
```

#### Deep file dependency analysis
- `values.yaml` (Harbor): `core.replicas: 1` → `2`, `registry.replicas: 1` → `2`. Redis config: mudar de `internal` para `external` apontando para DO managed Redis.
- `harbor-bootstrap.ts`: Pulumi component que cria DB, robot accounts. Adicionar Redis connection string.
- Downstream: TenantController cria Harbor robot accounts. Multi-replica: load balanced via ClusterIP.

#### Tasks
1. Alterar `values.yaml`: `core.replicas: 2`, `registry.replicas: 2`
2. Configurar Redis externo (DO managed Redis, mesmo usado por Gitea)
3. `helm upgrade harbor ...`
4. Validar: core e registry com 2 pods cada
5. Testar: push + pull image funciona
6. Testar: kill 1 core pod, operations continuam

#### TDD
```
RED:     TestHarborCoreReplicas() — kubectl get pods retorna 2 core + 2 registry Running
RED:     TestHarborFailover() — kill 1 core pod, push/pull continua funcionando
GREEN:   Deploy Harbor HA
VERIFY:  docker push + docker pull funciona apos deploy
```

#### Acceptance Criteria
- [ ] core: 2 replicas running
- [ ] registry: 2 replicas running
- [ ] Redis externo (DO managed) configurado
- [ ] Push/pull image funciona
- [ ] Failover: kill 1 pod, operacoes continuam

#### DoD
- [ ] Harbor HA operacional
- [ ] Failover validado

---

### T2.3 — MinIO distributed: 4 drives com erasure coding

#### Objective
Migrar MinIO de single-instance para distributed mode com erasure coding.

#### Evidence
ADR-0010 deliverable #5: "MinIO distributed migration — per existing docs/plans/minio-distributed-migration.md". Plano referenciado mas NAO encontrado no codebase. MinIO single-instance e SPOF: perda de pod = perda de build artifacts.

#### Files to edit
```
infra/helm/minio/values.yaml — mode: distributed, replicas: 4
infra/helm/minio/values-distributed.yaml — (NEW) overlay para distributed
docs/plans/minio-distributed-migration.md — (NEW) plano de migracao (referenciado por ADR-0010)
```

#### Deep file dependency analysis
- `infra/helm/minio/values.yaml`: MinIO Helm chart. Mudar de `mode: standalone` para `mode: distributed` com `replicas: 4` e `drivesPerNode: 1`.
- Downstream: build pipeline (`infra/helm/loopzero-build-cwts/`) referencia MinIO endpoint. Se endpoint muda: atualizar.
- `loopzero-api`: le MinIO credentials de Secret. Endpoint configurado via env var.

#### Deep Dives
- **Erasure coding:** MinIO distributed com 4 drives usa EC:2 (2 parity, 2 data). Sobrevive a perda de 2 drives.
- **Migration path:** 1) Deploy novo MinIO distributed ao lado do existente, 2) `mc mirror` dados do old para new, 3) Trocar endpoint no API + build workflows, 4) Deletar old MinIO.
- **DO Volumes:** 4 PVCs de 50GB cada (200GB total). Custo: ~$40/mo.

#### Tasks
1. Criar `docs/plans/minio-distributed-migration.md` (plano referenciado por ADR-0010)
2. Criar `infra/helm/minio/values-distributed.yaml` com mode distributed, 4 replicas
3. Deploy novo MinIO distributed no namespace `minio-distributed`
4. `mc mirror` dados do MinIO antigo para novo
5. Atualizar endpoint no loopzero-api e build CWTs
6. Validar: upload + download de build artifacts funciona
7. Deletar MinIO antigo apos validacao
8. Validar: kill 1 pod MinIO, operacoes continuam (erasure coding)

#### TDD
```
RED:     TestMinIODistributedFailover() — kill 1/4 pods, mc cp funciona
RED:     TestMinIODistributedUploadDownload() — build artifact upload + download OK
GREEN:   Deploy MinIO distributed + migrate data
VERIFY:  Build pipeline funciona end-to-end com novo MinIO
```

#### Acceptance Criteria
- [ ] MinIO distributed com 4 replicas
- [ ] Erasure coding ativo (EC:2)
- [ ] Dados migrados do antigo sem perda
- [ ] Build pipeline funciona com novo endpoint
- [ ] Failover: kill 1 pod, operacoes continuam
- [ ] MinIO antigo removido

#### DoD
- [ ] MinIO distributed operacional
- [ ] `docs/plans/minio-distributed-migration.md` criado
- [ ] Failover validado
- [ ] Zero data loss na migracao

---

### T2.4 — tenant-health Grafana dashboard

#### Objective
Criar dashboard unificado: "tenant X em qual cell, com qual status em cada stage".

#### Evidence
ADR-0010 deliverable #7: "Unified tenant-health Grafana dashboard — answer 'tenant X at which cell, with what status at each stage' from one panel".

#### Files to edit
```
infra/helm/monitoring/dashboards/tenant-health.json — (NEW) dashboard
```

#### Deep file dependency analysis
- `infra/helm/monitoring/dashboards/`: 21 dashboards existentes em JSON. ConfigMap montado no Grafana.
- Metricas necessarias: `loopzero_cell_tenant_count{cell_id}`, `loopzero_builds_total{tenant, status}`, `loopzero_deploys_total{tenant, status}`, `loopzero_custom_domains_total{tenant, status}`.
- Downstream: Grafana auto-import via sidecar.

#### Tasks
1. Criar `tenant-health.json` com paineis:
   - Variable: tenant selector (dropdown)
   - Panel 1: Cell assignment (`loopzero_cell_tenant_count`)
   - Panel 2: Deploy status timeline (builds queued/running/succeeded/failed)
   - Panel 3: Release state (pending/active/superseded/rolled_back)
   - Panel 4: App health (pods running/pending/crashloop)
   - Panel 5: Custom domain status
2. Deploy via ConfigMap
3. Validar: selecionar tenant, ver status em todos os stages

#### TDD
```
RED:     Dashboard nao existe — Grafana 404
GREEN:   Criar JSON + deploy
VERIFY:  Grafana /d/tenant-health acessivel com dados
```

#### Acceptance Criteria
- [ ] Dashboard acessivel no Grafana
- [ ] Tenant selector funcional (dropdown com todos os tenants)
- [ ] Todas as stages visiveis: cell → build → release → app → domain
- [ ] Funciona com dados reais (nao placeholder)

#### DoD
- [ ] `tenant-health.json` commitado
- [ ] Dashboard funcional no Grafana DO

---

## Phase 3: L4 Validation (500 Tenants)

**Objective:** Validar capacidade da celula unica com 500 tenants. Informar dimensionamento de cell-2.

### T3.1 — Executar L4 e documentar capacidade

#### Objective
Rodar L4 (500 tenants) apos L3 estavel para validar capacity assumptions de ADR-0008 (5k apps/cell).

#### Evidence
L4 nunca executado. ADR-0008 assume 5k apps/cell. L4 (500 tenants) = 10% da capacidade target. Se L4 falha em <90%: capacity model precisa revisao antes de Phase 2b.

#### Files to edit
```
docs/runbooks/l4-capacity-results.md — (NEW) resultados + capacity analysis
```

#### Tasks
1. Confirmar L3 estavel (T1.4 completo)
2. Executar L4: `LOOPZERO_REAL_DEPLOY=1 LOOPZERO_LOAD_LEVEL=l4 go test ./infra/tests/load/ -run L4 -v -timeout 8h 2>&1 | tee l4-run1.log`
3. Monitorar: PG connections, memory, CPU, BuildKit queue, ArgoCD app count
4. Documentar: success rate, bottleneck(s), resource peaks
5. Extrapolar: com estes dados, 5k apps/cell e viavel?
6. Se < 90%: documentar revisao de capacity model

#### TDD
```
RED:     L4 nunca executado
GREEN:   Executar L4
VERIFY:  Resultados documentados com extrapolacao para 5k/cell
```

#### Acceptance Criteria
- [ ] L4 executado ate conclusao
- [ ] Success rate documentado (target: >= 90%)
- [ ] Resource peaks por componente documentados
- [ ] Capacity extrapolacao para 5k apps/cell documentada
- [ ] Se < 90%: capacity model revisado

#### DoD
- [ ] `docs/runbooks/l4-capacity-results.md` criado
- [ ] Dados informam dimensionamento de cell-2

---

## Phase 4: Gate 2a Exit Validation

**Objective:** Checkpoint formal: todos os 7 deliverables de Phase 2a completos.

### T4.1 — Gate 2a exit checklist

#### Objective
Validar formalmente que todos os 7 deliverables de ADR-0010 Phase 2a estao completos.

#### Evidence
ADR-0010 Gate 2a Exit: "all seven done + chaos test proving each shared service can be killed independently with recovery <2min + 5 consecutive L3 >= 98/100".

#### Files to edit
```
docs/meetings/adr/0010-phase2-rescope.md — adicionar secao "Gate 2a Status" com evidencia
docs/maturity-assessment.md — atualizar score
CLAUDE.md — atualizar sprint status
```

#### Tasks
1. Checklist Gate 2a:
   - [ ] L3 5x >= 95/100 (evidencia: logs de T1.4)
   - [ ] PG max_connections >= 100 (evidencia: `SHOW max_connections`)
   - [ ] Gitea HA: 2 replicas + 200 concurrent clones test (evidencia: T2.1 results)
   - [ ] Harbor HA: 2+ core + Redis externo (evidencia: T2.2 results)
   - [ ] MinIO distributed: 4 drives + failover (evidencia: T2.3 results)
   - [ ] mgmt memory < 50% (evidencia: `kubectl top nodes`)
   - [ ] tenant-health dashboard funcional (evidencia: Grafana screenshot)
   - [ ] Chaos: each shared service kill + recovery <2min (evidencia: Phase 5)
2. Documentar cada item com evidencia concreta
3. Se todos passam: Gate 2a PASSED — Sprint 9 pode iniciar Phase 2b
4. Se algum falha: documentar gap e path para Sprint 9

#### Acceptance Criteria
- [ ] Todos os 7 deliverables validados com evidencia
- [ ] Gate 2a status documentado no ADR-0010
- [ ] Maturity assessment atualizado
- [ ] CLAUDE.md sprint status atualizado

#### DoD
- [ ] Gate 2a PASSED (ou gap documentado com path)
- [ ] Phase 2b desbloqueado para Sprint 9

---

## Phase 5: Chaos Gate (Shared Service Kill + Recovery)

**Objective:** Executar chaos tests contra DO validando recovery <2min para cada shared service. Gate 2a requirement.

### T5.1 — Executar 9 chaos cenarios contra DO

#### Objective
Rodar todos os 9 cenarios de chaos contra clusters DO com shared services HA (apos Phase 2).

#### Evidence
Gate 2a: "chaos test proving each shared service can be killed independently with recovery <2min". 9 cenarios em `infra/tests/chaos/chaos_test.go`. Targets: API (120s), controller (60s), PG storm (recovery), PG latency, ArgoCD (120s), Gitea (com HA agora), build node, simultaneous crash (180s), cell partition.

#### Files to edit
```
infra/tests/chaos/chaos_test.go — ajustes de contexto DO se necessario
docs/runbooks/chaos-gate-results.md — (NEW) resultados + evidencia
```

#### Tasks
1. Configurar kubeconfig com contextos DO
2. Executar cenarios 1-9 sequencialmente com logging
3. Para cada cenario: registrar recovery time exato
4. Gate validation: cada shared service (PG, Gitea, ArgoCD, MinIO, controller) recovery < 2min
5. Documentar resultados com timestamps

#### TDD
```
RED:     Chaos cenarios nunca executados em DO
GREEN:   Executar — recovery times dentro do SLO
VERIFY:  LOOPZERO_CHAOS_TEST=1 go test ./infra/tests/chaos/ -v -timeout 30m — 9/9 pass
```

#### Acceptance Criteria
- [ ] 9/9 cenarios executados contra DO
- [ ] Cada shared service: recovery < 2min (Gate 2a)
- [ ] API recovery < 120s
- [ ] Controller recovery < 60s
- [ ] Simultaneous crash recovery < 180s
- [ ] Resultados documentados com evidence

#### DoD
- [ ] 9/9 chaos tests passam
- [ ] `docs/runbooks/chaos-gate-results.md` criado
- [ ] Gate 2a chaos requirement satisfeito

---

### T5.2 — Fix chaos test failures

#### Objective
Corrigir quaisquer falhas dos chaos tests. Bloqueia Gate 2a.

#### Evidence
Historicamente, codigo nao testado em producao tem alta taxa de defeito (padrao "implemented but not wired"). Provavelmente 1-3 cenarios precisam ajuste.

#### Files to edit
```
TBD — dependente de T5.1
```

#### Tasks
1. Analisar logs de cenarios que falharam
2. Identificar root cause (infra config vs code bug vs timeout)
3. Escrever teste de regressao
4. Implementar fix
5. Re-executar cenario

#### Acceptance Criteria
- [ ] Todos os cenarios que falharam agora passam
- [ ] Cada fix tem teste de regressao

#### DoD
- [ ] 9/9 chaos tests passam (re-run completo)

---

## Phase 6: Observability (Paralelo)

**Objective:** Fechar gaps de observability que suportam operacao multi-cell.

### T6.1 — Resolver 17 alerts orfas

#### Objective
Eliminar alerts que referenciam metricas inexistentes (falsa confianca).

#### Evidence
17 PrometheusRules referenciam metricas nunca emitidas. 5 criticos, 12 warning/registry.

#### Files to edit
```
api/internal/metrics/metrics.go — adicionar 5 metricas criticas
infra/helm/monitoring/manifests/prometheusrule-*.yaml — deletar 12 orfas
```

#### Tasks
1. Implementar 5 metricas criticas: `loopzero_gitea_last_backup_timestamp`, `loopzero_build_egress_connectivity`, `loopzero_git_mirror_status`, `loopzero_git_desired_state_divergence`, `loopzero_break_glass_sessions_active`
2. Deletar 12 alerts orfas (warning/registry/compound cujas features nao existem)
3. Validar: `task validate:manifests` passa

#### TDD
```
RED:     TestOrphanAlertsRemoved() — zero alerts referencing non-emitted metrics
GREEN:   Implementar metricas + deletar orfas
VERIFY:  go test ./api/internal/metrics/... + kubeconform
```

#### Acceptance Criteria
- [ ] 5 novas metricas emitidas
- [ ] 12 alerts orfas deletadas
- [ ] Zero alerts referenciando metricas inexistentes

#### DoD
- [ ] 100% dos alerts ativos tem metricas reais

---

### T6.2 — Criar SLO formal + 13 runbooks faltantes

#### Objective
Documentar SLOs de escala em `docs/slo.md` e criar 13 runbooks faltantes.

#### Evidence
SLOs fragmentados em 9 YAML files. 13 alerts ativos sem runbook. Multi-cell precisa de SLOs formais para dimensionamento.

#### Files to edit
```
docs/slo.md — (NEW) SLOs formais com metricas de escala
docs/runbooks/alerts/ — 13 novos runbooks
```

#### Tasks
1. Criar `docs/slo.md` com SLOs de escala: deploy throughput >= 10/min, build p95 < 5min, cell recovery < 2min
2. Criar 13 runbooks para alerts sem documentacao
3. Validar: todos os `runbook_url` apontam para arquivos existentes

#### Acceptance Criteria
- [ ] `docs/slo.md` com SLOs de escala (nao apenas uptime)
- [ ] 13 runbooks criados
- [ ] Zero dead links em runbook_url

#### DoD
- [ ] SLO formal commitado
- [ ] 47/47 alerts ativos tem runbooks

---

### T6.3 — Deploy Promtail no build cluster

#### Objective
Centralizar logs do build cluster para suportar debugging em escala.

#### Evidence
Build cluster sem log centralizado. MTTR > 2h para build failures. Multi-cell amplifica: N clusters sem logs = N x overhead de debug.

#### Files to edit
```
infra/helm/promtail-build/ — (NEW) Promtail para build cluster
Taskfile.yml — task promtail:build:install
```

#### Tasks
1. Criar Helm chart wrapper para Promtail no build cluster
2. Configurar remote-write para Loki no mgmt cluster
3. Deploy no build cluster
4. Validar: build logs visiveis no Grafana

#### Acceptance Criteria
- [ ] Promtail running no build cluster
- [ ] Build logs queryaveis via Grafana `{cluster="build"}`
- [ ] Helm chart versao pinned

#### DoD
- [ ] Build logs centralizados
- [ ] Queryaveis no Grafana

---

## Coverage Matrix

| # | Gap / ADR-0010 Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | **L3 >= 95/100** (Gate #1, BLOQUEADOR) | T1.1-T1.4 | Diagnostico + fix iterativo + 5x validation |
| 2 | **PG max_connections >= 100** | T0.1 | Pulumi config: dbNodeClass medium |
| 3 | **Gitea HA** (2 replicas + shared storage + 200 clones) | T2.1 | NFS + 2 replicas + load test |
| 4 | **Harbor HA** (2+ core + Redis HA + S3) | T2.2 | 2 replicas + Redis externo DO |
| 5 | **MinIO distributed** | T2.3 | 4 drives erasure coding + migration |
| 6 | **mgmt memory < 50%** | T0.2 | Pulumi config: mgmtNodeClass medium |
| 7 | **tenant-health dashboard** | T2.4 | Novo Grafana dashboard |
| 8 | **Chaos: shared service kill + recovery <2min** (Gate 2a) | T0.3, T5.1, T5.2 | Chaos Mesh + executar 9 cenarios |
| 9 | **L4 capacity validation** (informa cell-2 sizing) | T3.1 | 500 tenants com capacity extrapolacao |
| 10 | **Gate 2a formal exit** | T4.1 | Checklist com evidencia |
| 11 | 17 alerts orfas (falsa confianca) | T6.1 | Implementar 5 + deletar 12 |
| 12 | SLO formal + 13 runbooks | T6.2 | docs/slo.md + runbooks |
| 13 | Build cluster sem logs | T6.3 | Promtail remote-write |

**Coverage: 13/13 gaps cobertos (100%)** — 7/7 ADR-0010 deliverables + 6 supporting items

## Global Definition of Done

- [ ] **Gate 2a: 7/7 deliverables de ADR-0010 completos com evidencia**
  - [ ] L3 5x >= 95/100
  - [ ] PG max_connections >= 100
  - [ ] Gitea HA (2 replicas + 200 concurrent clones)
  - [ ] Harbor HA (2+ core + Redis externo)
  - [ ] MinIO distributed (4 drives + erasure coding)
  - [ ] mgmt memory < 50%
  - [ ] tenant-health Grafana dashboard
- [ ] **Gate 2a: chaos tests passam** (9/9 cenarios, recovery <2min)
- [ ] **L4 executado** com capacity extrapolacao documentada
- [ ] **Observability:** 0 alerts orfas, SLO formal, build logs centralizados
- [ ] `task test:unit` passa
- [ ] `task lint` passa
- [ ] `task validate:manifests` passa
- [ ] CHANGELOG.md atualizado
- [ ] Maturity assessment atualizado (target >= 4.0)
- [ ] **Phase 2b desbloqueado** para Sprint 9 (cell-2 activation)
