# Cell Architecture — Phase 3 (Automation)

**Criado:** 2026-04-17
**Última atualização:** 2026-04-17 (v1.0)
**Horizonte:** 3-4 sprints (~3-4 semanas com 2 engineers)
**Trigger:** 500+ tenants OU Phase 2b completo com 2 cells validadas
**Objetivo:** Automatizar o que Phase 2 fez manualmente. Cell N+1 é `task add-cell` não semana de engenharia. Tenants podem migrar entre cells sem downtime. Shared services escalam para ~10 cells sem redesenho.

**Origem:** ADR-0008 §Phases, MEET-2026-04-17-051 (Phase 3 gap analysis), scale-50k-plan.md Phase 4.

---

## 📌 Snapshot

**Blocker para começar:** Phase 2b Gate Exit (ver `cell-architecture-phase2.md`). Adicionar automação a algo não estável é multiplicar risco.

```
Phase 2b complete (2 cells, chaos validated)
   │
   ▼
Phase 3 (~3-4 sprints)
   │  • T-3-01 Auto-provisioning cells (Pulumi automation)
   │  • T-3-02 Migration state machine WIRED (from types-only T-2b-15)
   │  • T-3-03 Per-cell ArgoCD (substitui central beyond 500 tenants)
   │  • T-3-04 PG read replicas + PgBouncer
   │  • T-3-05 Gitea decision: clustering vs per-cell vs bare-git+S3
   │  • T-3-06 mgmt cluster HA
   │  • T-3-07 CrossCellAggregator proven at N cells
   │  • T-3-08 ReconciliationReport CRD + controller (drift detection)
   │  • T-3-09 Cell capacity auto-scaling triggers
   ▼
Capacity: ~5-10 cells operacionais, ~5,000 tenants
Trigger para Phase 4: 1000+ tenants ou 10 cells
```

---

## Princípios Phase 3

1. **Adicionar cell deve ser 1 comando.** Se adicionar cell-11 requer engenharia, não automatizamos — só multiplicamos esforço manual.
2. **Migration é produção, não demo.** Zero-downtime cross-cell movement precisa ser testado com tenants reais.
3. **Shared services param de ser Phase 2a hardening e viram elastic.** PG read replicas, Gitea cluster (ou shard), Harbor geo-prep.
4. **Controle plane não pode ser SPOF com 10 cells.** 1 mgmt cluster down = todo routing para.

---

## T-3-01 · Auto-provisioning cells (Pulumi automation)

**Tipo:** T3 (IaC automation)
**Esforço:** 1 sprint
**Owner:** Infra
**Depends on:** Phase 2b T-2b-01 (cell.ts real implementation)

### Motivação

Phase 2 fez cell #2 manualmente via `pulumi up --target`. Não escala para 10 cells, muito menos 100. Queremos `task add-cell --region=sfo3` invocar Pulumi programaticamente.

### Tarefas

- [ ] 01.1 Pulumi Automation API CLI wrapper: `tools/add-cell.sh --region --size --name`
- [ ] 01.2 Config array `cells: [...]` em `Pulumi.prod.yaml` — cada entry declara uma cell
- [ ] 01.3 `createCell()` loop iterativo sobre config array; adições incrementais
- [ ] 01.4 Pre-flight validations: DO droplet quota disponível, VPC capacity, DNS zones prontos
- [ ] 01.5 Post-create validations: cell passa health contract (T-2b-05) antes de ser marcada `Active`
- [ ] 01.6 `task remove-cell --name` simétrico (drain tenants, decommission)
- [ ] 01.7 Runbook `docs/runbooks/add-cell.md` cobrindo golden-path + failure modes

### DoD

- `task add-cell --region=nyc3 --size=medium --name=cell-nyc3-01` completa em < 15 min, resultado: cell-nyc3-01 ativa, health Active, PlacementEngine a enxerga
- Rollback testado: se health falha durante provisioning, rollback automático sem resíduos
- 3 adições consecutivas sem intervenção manual

---

## T-3-02 · Migration state machine WIRED

**Tipo:** T3 (distsys + critical flow)
**Esforço:** 1 sprint
**Owner:** API + DistSys review
**Depends on:** T-2b-15 (state machine types) + T-2b-02 (kubeconfig rotation) + T-2b-14 (gitops path cell encoding)

### Motivação

Phase 2 deliberadamente deferred migration. Com 2 cells, migration é nice-to-have. Com 10 cells, migration é **mandatory** — tenants precisam ser rebalanced conforme cells fill up, ou cells new recebem tráfego que deveria ir para cells underused.

### State machine (Já types em T-2b-15)

```
ACTIVE(cell-A) → PREPARING(cell-B) → DUAL(A+B) → DRAINING(cell-A) → ACTIVE(cell-B)
```

### Tarefas

- [ ] 02.1 Implementar reconciler wiring a state machine a recursos reais:
  - [ ] `PREPARING`: provisionar namespace + ArgoCD app + gitops path em cell-B
  - [ ] `DUAL`: ambos servem; TTL 5min; verify cell-B healthy via synthetic probe
  - [ ] `DRAINING`: flip PG `tenants.cell_id` atômica; drain cell-A pods; aguardar
  - [ ] `ACTIVE(cell-B)`: delete cell-A resources com finalizers
- [ ] 02.2 API endpoint `POST /admin/tenants/{id}/migrate --to-cell=X` (admin only, feature flag)
- [ ] 02.3 Advisory lock `pg_advisory_xact_lock('migration', tenant_id)` — migration é exclusiva por tenant
- [ ] 02.4 Chaos tests:
  - [ ] Migration pausada em DUAL → sistema idempotente, retomável
  - [ ] Cell-B cai em DRAINING → rollback para ACTIVE(cell-A)
  - [ ] Concurrent migrate + deploy → serializado via locks
- [ ] 02.5 CLI: `loopzero admin tenants migrate --id=X --to-cell=Y --yes`
- [ ] 02.6 Runbook `docs/runbooks/tenant-migration.md`

### DoD

- 10 tenants migrados com zero requests 5xx (verified via log de production traffic)
- Chaos: 3 cenários recuperam sem intervenção manual
- Migration é reversível (rollback automático se falhar dentro de 10min)

---

## T-3-03 · Per-cell ArgoCD

**Tipo:** T3 (gitops — major topology change)
**Esforço:** 1-1.5 sprints
**Owner:** GitOps + Infra
**Depends on:** T-2b-14 (gitops path cell encoding)

### Motivação

GitOps Engineer meeting: "Central ArgoCD survives Phase 2 (≤500 tenants), NOT Phase 3 (500+)." Evidence CNOE: 1 shard satura a ~2k apps/m5.2xlarge. Sharding (T-05) aumenta shards dentro de 1 instância mas não resolve o controller bottleneck completo.

### Tarefas

- [ ] 03.1 Instalar ArgoCD por cell via `cell.ts` (parte do provisioning) — não mais central
- [ ] 03.2 ArgoCD por cell lê de **central Gitea** (por ora; T-3-05 pode mudar)
- [ ] 03.3 Path-level scoping: cell-N ArgoCD vê só `gitops/cells/cell-N/**`
- [ ] 03.4 Decommission central ArgoCD gradualmente:
  - [ ] Novo tenant em cell-X → ArgoCD(cell-X) cria Application
  - [ ] Tenants existentes migrados via T-3-02 → Application recriada em cell-N ArgoCD
  - [ ] Após todos tenants migrated: desligar central ArgoCD
- [ ] 03.5 CrossCellAggregator (T-3-07) consome API de cada ArgoCD para views cross-cell
- [ ] 03.6 Grafana federation dashboards — cada ArgoCD exporta métricas, central visualiza
- [ ] 03.7 Benchmark: 2000 apps/cell reconcile p95 < 60s

### DoD

- 10 cells cada uma com seu ArgoCD
- Zero dependência de central ArgoCD
- Sync latency p95 < 60s a 2000 apps/cell
- CrossCellAggregator retorna lista merged em < 2s

---

## T-3-04 · PostgreSQL read replicas + PgBouncer

**Tipo:** T3 (database — critical infra)
**Esforço:** 1 sprint
**Owner:** Infra + DistSys review
**Depends on:** T-2a-02 (PG bump done)

### Motivação

A 500+ tenants:
- N cells × 2-3 controllers/cell × DB_MAX_CONNS=40 = 800+ connections. Single PG limita a 100-200 mesmo com bump.
- Queries de listing (CrossCellAggregator, `loopzero project list`, audit) competem com writes críticos (builds, releases).

### Tarefas

- [ ] 04.1 Pulumi: bump DB para node-count ≥ 3 (1 primary + 2 replicas) via `loopzero-infra:dbHa: true`
- [ ] 04.2 Deploy PgBouncer em mgmt cluster (transaction pooling mode)
- [ ] 04.3 API config:
  - [ ] `DB_PRIMARY_URL` para writes (advisory locks, mutations)
  - [ ] `DB_REPLICA_URL` para reads (`project list`, `status`, audit queries)
  - [ ] `sqlx` wrapper decidindo primary vs replica baseado em query context
- [ ] 04.4 Replica lag monitoring: `pg_stat_replication` Prometheus scrape
- [ ] 04.5 Alert `ReplicaLagHigh` se lag > 5s (indica que reads podem estar stale)
- [ ] 04.6 Read-your-own-writes invariant: após POST, GET subsequente via primary (não replica) — evita read-your-own-writes stale

### DoD

- PgBouncer multiplexing active, 300+ client connections, 50 server connections
- Read queries (list, status) → replica; write queries → primary
- Replica lag p99 < 1s
- Alert testado com injected lag

---

## T-3-05 · Gitea scaling decision + implementation

**Tipo:** T3 (architecture decision)
**Esforço:** 1-2 sprints (depende da decisão)
**Owner:** GitOps + Architect review
**Depends on:** T-2a-03 (Gitea HA Phase 2a)

### Motivação

ADR-0008 explicit: "Evaluate Gitea clustering or per-Cell Gitea in Phase 3." A 10,000 repos (10 cells × 1000 tenants × 1 repo) Gitea HA 2-replica não aguenta concurrent clone load de 10 ArgoCDs.

### Opções

| Opção | Custo | Pros | Cons |
|---|---|---|---|
| **A.** Per-cell Gitea (replication via pull) | Alto — state sync | Isolamento real | Dual-write split-brain (VETOED by GitOps Phase 2) |
| **B.** Gitea Enterprise clustering (shared PG + distributed storage) | Médio — licensing + infra | Single source of truth, transparent | Gitea EE pricing, lock-in |
| **C.** Bare git + DO Spaces (git-on-S3 lib como `gitopia/git-remote-s3`) | Alto — custom engineering | S3 é infinitamente escalável, zero ops | Perdemos Gitea UI + admin features |
| **D.** Gitea HA já feito + aggressive caching per cell ArgoCD | Baixo — já temos | Minimal change | Ceiling em ~10k repos, não 50k |

### Tarefas (branching pela decisão)

**MEETING obrigatório antes de implementar.** Triggers: load test a 10k repos + bench concurrent clone → qual opção sobrevive?

- [ ] 05.1 Benchmark: 200 concurrent clones a 10k repos Gitea HA (Phase 2a state) → coletar p95/p99
- [ ] 05.2 Technical meeting com GitOps + Architect + Infra: escolher opção
- [ ] 05.3 ADR-0011: decisão formal
- [ ] 05.4 Implementação da opção escolhida (efforts variam drasticamente)
- [ ] 05.5 Migration script repos existing → novo backend se necessário

### DoD

- Decisão em ADR-0011
- Implementação passa bench de 500 concurrent clones a 20k repos sem saturar
- Runbook operacional atualizado

---

## T-3-06 · mgmt cluster HA

**Tipo:** T3 (infra + distsys)
**Esforço:** 1 sprint
**Owner:** Infra + DistSys
**Depends on:** T-3-04 (PG HA — API replicas precisam escrever sem split-brain)

### Motivação

mgmt cluster = controle plane de tudo. Com 10 cells, mgmt down = 10 cells sem routing, sem new deploys, sem migration. Single point failure amplificado.

### Tarefas

- [ ] 06.1 DOKS mgmt bump: min/max nodes para aguentar perda de 1 node sem OOM
- [ ] 06.2 API: 3 replicas com leader election (via generation fencing do work do sprint anterior)
- [ ] 06.3 Controller: 3 replicas com leader election
- [ ] 06.4 Kratos/Hydra: 3 replicas (já HPA-ready per T-04)
- [ ] 06.5 Traefik mgmt: 3 replicas (já HPA-ready)
- [ ] 06.6 Chaos: kill 1 mgmt node durante operações → recovery < 30s sem ações falhas
- [ ] 06.7 Chaos: kill 2 mgmt nodes (sobrando 1) → degraded mas ainda serve

### DoD

- 3-node mgmt cluster com todos serviços HA
- Chaos test 1-node kill: zero ops falhas
- Chaos test 2-node kill: degraded mas functional

---

## T-3-07 · CrossCellAggregator proven at N cells

**Tipo:** T2 (api scaling)
**Esforço:** 0.5 sprint
**Owner:** API
**Depends on:** T-3-03 (per-cell ArgoCD — aggregator chama cada)

### Motivação

`loopzero project list` com 5000 tenants em 10 cells = 10 ArgoCD API calls + 10 K8s LIST calls. Sem otimização, latency > 5s.

### Tarefas

- [ ] 07.1 Parallelize cross-cell queries (goroutines, semaphore=10)
- [ ] 07.2 Timeout per-cell: 2s, degrade gracefully (retorna parcial + flag `partial=true`)
- [ ] 07.3 Cache 30s in-memory para idempotent reads
- [ ] 07.4 Métrica `crosscell_query_duration_seconds{cells_queried,cells_responded}`
- [ ] 07.5 Load test: 10 cells × 5000 tenants → `project list` p95 < 3s

### DoD

- p95 < 3s a 10 cells/5000 tenants
- Partial degradation test: 1 cell unreachable → response em < 3s com 9 cells data

---

## T-3-08 · ReconciliationReport CRD + controller (drift detection)

**Tipo:** T3 (controllers)
**Esforço:** 1 sprint
**Owner:** Operator
**Depends on:** T-3-03 (per-cell ArgoCD)

### Motivação

CRD definido mas controller não existe ("Implemented but not documented" anti-pattern em CLAUDE.md). Com N cells, drift detection vira crítico — ArgoCD OutOfSync é per-cell mas precisamos view centralizada.

### Tarefas

- [ ] 08.1 `operators/internal/controller/reconciliation_report_controller.go`
- [ ] 08.2 Scheduled reconcile (a cada 10min) que:
  - [ ] Consulta cada per-cell ArgoCD para lista de Applications em OutOfSync
  - [ ] Consulta cada cell para CR drift (Tenant.Status vs real state)
  - [ ] Escreve ReconciliationReport CR com summary
- [ ] 08.3 Metric `loopzero_drift_detected{cell,resource_type}` gauge
- [ ] 08.4 Alert `DriftSustained` se drift > 10min
- [ ] 08.5 Runbook `docs/runbooks/drift-recovery.md`

### DoD

- Controller rodando + ReconciliationReport CR updated a cada 10min
- Injected drift (manual `kubectl edit` em workload) detectado < 10min
- Alert disparando

---

## T-3-09 · Cell capacity auto-scaling triggers

**Tipo:** T2 (automation)
**Esforço:** 0.5 sprint
**Owner:** SRE + Infra
**Depends on:** T-3-01 (auto-provisioning)

### Motivação

A 10 cells manualmente tracked = operador decide quando criar cell-11. Com trigger automático, cell-11 é provisionada quando cell-8/9/10 passam 80% de capacity, evitando refuse de placement.

### Tarefas

- [ ] 09.1 Capacity metric per cell: `cell_capacity_used_pct = tenants_in_cell / cell_max_tenants`
- [ ] 09.2 Threshold: todas active cells > 80% por 1h sustained → emit evento `CellsNearCapacity`
- [ ] 09.3 Auto-trigger: chama `task add-cell --region=$(next-region)` via CronJob
- [ ] 09.4 Cooldown: no auto-scale dentro de 24h do último (evita runaway)
- [ ] 09.5 Alert Grafana + Slack com opt-in/opt-out do auto-scale

### DoD

- Load test 5 cells saturando → 6ª cell provisionada automaticamente em 20min
- Cooldown respected
- Operador pode opt-out via config

---

## Gate Phase 3 Exit

| Gate | Status |
|---|---|
| Phase 2b Gate Exit feito | ❌ (prereq) |
| T-3-01: 3 cells consecutivas adicionadas via automation | ❌ |
| T-3-02: 10 tenants migrated zero-downtime | ❌ |
| T-3-03: central ArgoCD desligado, per-cell ArgoCDs operacionais | ❌ |
| T-3-04: PG HA + replicas + PgBouncer, lag p99 < 1s | ❌ |
| T-3-05: Decisão Gitea escolhida + implementada + bench passing | ❌ |
| T-3-06: mgmt HA validado via 2-node chaos kill | ❌ |
| T-3-07: CrossCellAggregator p95 < 3s a 10 cells | ❌ |
| T-3-08: Drift detection ativo, runbook testado | ❌ |
| T-3-09: Auto-scale de cell validado end-to-end | ❌ |
| **L5 load test pass**: 10 cells, 5000 tenants, < 1% failure, 4h stable | ❌ |

Scale-50k-plan.md §Phase 4 (L5) valida Phase 3 Exit. Ver lá para detalhes de load scenarios.

---

## Fora de escopo (explícito) Phase 3

| Item | Onde |
|---|---|
| Multi-region (SFO3 + NYC3) | Phase 4 |
| ApplicationSets sharding avançado | Phase 4 |
| Harbor geo-replication | Phase 4 |
| Per-region build plane | Phase 4 |

---

## Referências

- **Fase anterior:** `docs/plans/cell-architecture-phase2.md` (T-2a + T-2b)
- **Fase seguinte:** `docs/plans/cell-architecture-phase4.md`
- **Overview:** `docs/plans/cell-architecture-overview.md`
- **Load test plan:** `docs/plans/scale-50k-plan.md` §Phase 4 (L5 scenarios)
- **ADR base:** `docs/meetings/adr/0008-cell-based-architecture.md`
- **Meeting record:** `docs/meetings/records/MEET-2026-04-17-051.md`

---

## Changelog

- **v1.0 (2026-04-17):** Plano criado. 9 tasks. Gate Exit requer L5 load test pass (10 cells / 5000 tenants).
