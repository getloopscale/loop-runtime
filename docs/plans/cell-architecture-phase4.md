# Cell Architecture — Phase 4 (Optimization to 50k)

**Criado:** 2026-04-17
**Última atualização:** 2026-04-17 (v1.0)
**Horizonte:** 6-8 sprints (~6-8 semanas com 2-3 engineers)
**Trigger:** Phase 3 Exit (10 cells / 5k tenants validated) OU roadmap explícito para 50k
**Objetivo:** Escalar de 5k tenants → 50k. Multi-region. Shared services que escalam geometricamente (Harbor geo-rep, PG partitioning, per-region build plane). Control plane HA real.

**Origem:** ADR-0008 §Phases, scale-50k-plan.md Phase 5.

---

## 📌 Snapshot

**Blocker para começar:** Phase 3 Exit (10 cells operacionais + auto-provisioning + migration + per-cell ArgoCD + PG HA). Não faz sentido otimizar para 50k se a base para 5k ainda não está provada.

```
Phase 3 complete (10 cells, 5k tenants, migration wired)
   │
   ▼
Phase 4 (~6-8 sprints, 2-3 engineers)
   │  • T-4-01 ApplicationSet sharding (per cell + per tenant hash)
   │  • T-4-02 Harbor geo-replication (multi-region)
   │  • T-4-03 Per-region build plane (Argo Workflows per region)
   │  • T-4-04 Prometheus federation (Thanos ou Mimir)
   │  • T-4-05 CF Tunnel multi-region failover
   │  • T-4-06 PG partitioning by cell_id OU PG per region
   │  • T-4-07 Cold-path optimization (sync batching, Gitea cache)
   │  • T-4-08 L6 load test execution (50k apps)
   │  • T-4-09 Operational runbooks per region
   │  • T-4-10 Capacity model final (evidence-based)
   ▼
50,000 apps validated, 2 regions, chaos-proof
```

---

## Princípios Phase 4

1. **Shared services centrais viram federated/regional.** Single Gitea/Harbor/PG **NÃO** aguenta 50k. Cada um vira multi-region ou sharded.
2. **Control plane NÃO aguenta 100 cells via central single-mgmt.** Phase 4 introduz mgmt-per-region (ou equivalent federation).
3. **Evidence > projection.** Toda decisão em Phase 4 backed por load-test measured numbers, não matemática no papel.
4. **Custo > 50 cells precisa ser validado.** L6 load test é ~$350 por rodada; 6 rodadas de refinamento = $2k — previsto no budget.
5. **Post-50k é Phase 5.** Coisas tipo "tenant re-homing por geolocation" ou "ArgoCD inteligente" são depois.

---

## T-4-01 · ApplicationSet sharding

**Tipo:** T3 (gitops — scale-critical)
**Esforço:** 1 sprint
**Owner:** GitOps
**Depends on:** T-3-03 (per-cell ArgoCD)

### Motivação

GH argo-cd #19113: ApplicationSet controller é single-threaded. A ~500 apps/AppSet reconcile loop leva 45-90s; a ~2000 apps leva 6+ min. Novo tenant espera o ciclo. Inaceitável em 50k (onboarding p99 seria > 10min só na AppSet).

### Tarefas

- [ ] 01.1 Sharding strategy: multiple AppSets per cell, split por hash(tenant_id) % K
- [ ] 01.2 K começa em 4 (cap 500 apps/AppSet), cresce conforme cell capacity
- [ ] 01.3 AppSet list generator alimentado por API endpoint `/internal/appset/{cell}/{shard}` — retorna subset de tenants
- [ ] 01.4 Controller wire: rebalancing quando K muda (cautious)
- [ ] 01.5 Metric `appset_reconcile_duration_seconds{cell,shard}` + alert > 60s sustained
- [ ] 01.6 Benchmark: 2000 apps × 4 AppSets (total 8000) reconcile p95 < 60s

### DoD

- 4+ AppSets por cell
- Reconcile p95 < 60s a 2000 apps/AppSet (validated)
- Novo tenant reachable em < 5min (AppSet + Application create)

---

## T-4-02 · Harbor geo-replication (multi-region)

**Tipo:** T3 (infra + supply chain security)
**Esforço:** 1 sprint
**Owner:** Infra + Security review
**Depends on:** nenhum (isolado)

### Motivação

Central Harbor SFO3 + cells em NYC3 = cada deploy em NYC3 pulls imagem de SFO3 → latency cross-region + egress cost. A 50k deploys/dia = $$$.

### Tarefas

- [ ] 02.1 Deploy Harbor replica NYC3 + registry S3 backend regional (DO Spaces NYC3)
- [ ] 02.2 Harbor replication policy: tenant images push SFO3 → auto-replicate NYC3 < 1min
- [ ] 02.3 DNS GeoIP: `harbor.dev.loopzero.example` resolve para região mais próxima (CF GeoDNS)
- [ ] 02.4 Cells em NYC3 pullam `harbor-nyc3.dev.loopzero.example`; SFO3 pullam SFO3
- [ ] 02.5 Cosign signature portable entre regions (T-2b-10 requires)
- [ ] 02.6 Bench: 100 concurrent pulls NYC3 → p95 < 5s (vs cross-region > 15s)

### DoD

- 2 Harbor regions operacionais + replication
- Region-local pulls verified via `trace route` + p95
- Cosign enforcement unchanged (passa verify em ambas regions)

---

## T-4-03 · Per-region build plane

**Tipo:** T3 (build/release — critical flow)
**Esforço:** 1-1.5 sprint
**Owner:** Infra + Pipeline
**Depends on:** nenhum

### Motivação

Single build cluster (SFO3) para 50k apps = 35 builds/min sustained min. Cross-region builds têm latency cross-cell para image push, Gitea reads etc. Per-region build plane localiza o fluxo.

### Tarefas

- [ ] 03.1 Provision build cluster NYC3 via Pulumi
- [ ] 03.2 Deploy Argo Workflows + BuildKit stack (reuse existing helm chart parametrizado)
- [ ] 03.3 API `BuildSubmitter` chooses build cluster baseado em cell location do tenant
- [ ] 03.4 Source bundle — decisão: upload to region-local MinIO/Spaces vs central. Recomenda region-local com background sync.
- [ ] 03.5 Image push: build NYC3 → Harbor NYC3 (região-local, sem cross-region)
- [ ] 03.6 Gitops push: ainda central Gitea OU per-region (depende T-3-05 decision)
- [ ] 03.7 Chaos: SFO3 build plane down → NYC3 builds continuam independente

### DoD

- 2 build planes operacionais
- Region assignment via PlacementEngine (tenant em NYC3 cell → NYC3 build plane)
- Chaos: kill 1 region build plane → other unaffected

---

## T-4-04 · Prometheus federation (Thanos/Mimir)

**Tipo:** T3 (observability — scale critical)
**Esforço:** 1 sprint
**Owner:** SRE + Observability
**Depends on:** T-2b-06 (synthetic probe + per-cell Prometheus)

### Motivação

100 cells × Prometheus agent → central Prometheus não aguenta. Queries cross-region pagam latência. Precisa tier long-retention + query-federation camada.

### Tarefas

- [ ] 04.1 Decisão: Thanos vs Mimir (meeting com SRE + Observability)
- [ ] 04.2 Deploy escolhido com object store backend (DO Spaces)
- [ ] 04.3 Prometheus Agent per cell → remote_write para central tier
- [ ] 04.4 Grafana datasource: tier escolhido (não direct Prometheus)
- [ ] 04.5 Retention policy: raw 15d em SSD, downsampled 12m em S3
- [ ] 04.6 Query performance: `rate(loopzero_builds_total[5m])` cross 100 cells p95 < 3s

### DoD

- Federation tier operacional
- 100 cells remote_write sem drop
- Query latency p95 < 3s cross-cell

---

## T-4-05 · CF Tunnel multi-region failover

**Tipo:** T2 (networking)
**Esforço:** 0.5 sprint
**Owner:** Infra

### Motivação

Single CF Tunnel SFO3 down → todo traffic to cells do. Phase 4 adiciona redundância por region.

### Tarefas

- [ ] 05.1 Provision CF Tunnel NYC3 (2nd tunnel ID)
- [ ] 05.2 Both tunnels route to respective region cells
- [ ] 05.3 CF-side load balancer with health checks → automatic failover
- [ ] 05.4 Chaos: kill tunnel SFO3 → traffic continues via NYC3 em < 30s
- [ ] 05.5 Documented runbook `docs/runbooks/tunnel-failover.md`

### DoD

- 2 tunnels active
- Chaos failover < 30s
- Runbook tested

---

## T-4-06 · PostgreSQL partitioning OR per-region PG

**Tipo:** T3 (database — critical)
**Esforço:** 1-2 sprints (depende da decisão)
**Owner:** DistSys + DB expertise
**Depends on:** T-3-04 (PG HA Phase 3)

### Motivação

A 50k tenants:
- `tenants` table: 50k rows — OK
- `builds` table: 50k × 100 builds = 5M rows. Indexes bloat. Partition desired.
- `audit_log`: 500M+ rows possível. Partition mandatory.
- Connections: 10 regions × 5 services/region = 50+ sustained — PgBouncer necessário mas insuficiente em single PG central.

### Opções

| Opção | Custo | Evidence |
|---|---|---|
| **A.** PG partitioning by `cell_id` (declarative partitioning) | Médio | Postgres 16 native support |
| **B.** PG per region (SFO3 + NYC3) with cross-region queries via FDW | Alto | Complexidade migration |
| **C.** Citus distributed (Hyperscale) | Alto — licensing | Proven >1B rows |

### Tarefas

- [ ] 06.1 Meeting: decisão entre A/B/C com measurement data de Phase 3
- [ ] 06.2 ADR-0012 formalizando decisão
- [ ] 06.3 Implementação (efforts variam)
- [ ] 06.4 Migration script: preservar tenants existentes, zero-downtime switchover
- [ ] 06.5 Test: 50k tenants simulados, queries p95 < 200ms

### DoD

- Decisão em ADR-0012
- 50k tenants simulados persistem + query performance validada
- Runbook migration

---

## T-4-07 · Cold-path optimization

**Tipo:** T2 (performance tuning)
**Esforço:** 1 sprint
**Owner:** API + GitOps

### Motivação

Deploy p99 target é < 10min a 50k. Cold paths (raro: tenant init, first deploy) adicionam tempo se não otimizados.

### Tarefas

- [ ] 07.1 Gitea clone cache per cell ArgoCD (local mirror do repo, refresh 5s)
- [ ] 07.2 ArgoCD sync batching: N Applications em same cell coordenadas vs cada individual
- [ ] 07.3 API `/builds` endpoint: preheat cache de tenant metadata após POST
- [ ] 07.4 Cosign signature cache per cell (evita re-verify se image SHA é known good)
- [ ] 07.5 Bench: deploy p99 < 10min sustained
- [ ] 07.6 Bench: cold deploy (primeiro tenant deploy) p95 < 6min

### DoD

- p99 deploy < 10min em L6 load test
- Cold deploy < 6min

---

## T-4-08 · L6 load test execution (50k apps)

**Tipo:** T3 (validation — goal critical)
**Esforço:** 2-3 sprints (progressive execution + fixes)
**Owner:** SRE + Full team on standby
**Depends on:** T-4-01 a T-4-07 todas completadas

### Motivação

**Este é o momento do plano.** Todo trabalho de Phase 2-4 culmina aqui.

### Execução (progressive — scale-50k-plan.md §Phase 5.2)

- [ ] 08.1 **L6.a** — 10,000 tenants em 20 cells, 4h. Fix identified bottlenecks.
- [ ] 08.2 **L6.b** — 25,000 tenants em 50 cells, 8h. Fix.
- [ ] 08.3 **L6.c** — 50,000 tenants em 100 cells, 12h. Final validation.

### Pass criteria (de scale-50k-plan.md)

| Métrica | Target |
|---|---|
| Apps verified live | 50,000 / 50,000 (100%) HTTP 200 |
| Failure rate | < 0.5% |
| Deploy p99 e2e | < 10 min |
| Cell failure propagation | Zero (blast radius = 1 cell) |
| Chaos scenarios (28) | Todos pass |

### Chaos suite at 50k (scale-50k-plan.md §5.3)

10 scenarios — C1..C10, cobrindo kill-10-cells, PG failover, region failure, build saturation, Gitea unavailable, CF tunnel failure, leader election storm, PG pool exhaustion, webhook flood 10k/10s, ArgoCD corruption.

### DoD

- 3 progressive runs passadas
- Chaos suite C1-C10 passada
- Capacity model documented `docs/plans/cell-capacity-final.md` com measured numbers
- Video demonstration (investor-ready): 50k apps live, kill 10 cells, zero 5xx globally

---

## T-4-09 · Operational runbooks per region

**Tipo:** T2 (docs)
**Esforço:** 1 sprint
**Owner:** SRE

### Tarefas

Runbooks novos em `docs/runbooks/` para 50k/multi-region:

- [ ] 09.1 `region-failover.md` — SFO3 down → NYC3 acomoda
- [ ] 09.2 `harbor-replication-delay.md` — geo-rep stuck
- [ ] 09.3 `cross-region-pg-partition.md` — PG split decision
- [ ] 09.4 `appset-rebalance.md` — K sharding change
- [ ] 09.5 `thanos-rebalance.md` — tier query performance
- [ ] 09.6 `cell-decommission.md` — remove cell safely (drain + tenant migration + teardown)
- [ ] 09.7 `50k-incident-response.md` — playbook para incident paging a 50k scale

### DoD

- 7 runbooks merged, cada um com recovery steps tested em staging-equiv

---

## T-4-10 · Capacity model final (evidence-based)

**Tipo:** T2 (docs + artifact)
**Esforço:** 0.5 sprint
**Owner:** Architect + SRE

### Tarefas

- [ ] 10.1 `docs/plans/cell-capacity-final.md` com tabelas:
  - Per-cell: tenants, apps, pods, PG conns, Gitea repos, Harbor images, sustained RPS
  - Per-region: N cells, N nodes, cost/month, capacity headroom
  - Global: 2 regions, 100 cells, 50k tenants, 100k apps, cost/month
- [ ] 10.2 Each number from measurement (L6 load test), not projection
- [ ] 10.3 Scaling curves — add 1 cell = +N tenant capacity measured
- [ ] 10.4 Break-even analysis: $/tenant/month at 50k
- [ ] 10.5 Link from main README + pitch deck

### DoD

- Doc committed with real numbers
- Referenced from investor pitch

---

## Gate Phase 4 Exit (= 50k Plan DONE)

| Gate | Status |
|---|---|
| Phase 3 Gate Exit feito | ❌ (prereq) |
| T-4-01..T-4-07 done | ❌ |
| T-4-08 L6.c pass: 50k apps live verified HTTP 200 | ❌ |
| T-4-08 Chaos C1-C10 pass | ❌ |
| Blast radius = 1 cell (não propagation) | ❌ |
| Deploy p99 e2e < 10min sustained 12h | ❌ |
| Failure rate < 0.5% | ❌ |
| T-4-10 capacity model final published | ❌ |

**Se todos ✅:** LoopZero é PaaS 50k-capable com evidence. Pitch deck pode mostrar video + capacity table.

---

## Cost Model Phase 4

| Item | Custo |
|---|---|
| 100 cells × $400-800/mês = | **$40k-80k/mês** (steady state at 50k tenants real) |
| L6 load test (4h, on-demand) | ~$350/run × 3 runs = $1050 |
| Thanos/Mimir storage (100 cells × 15d raw) | ~$2k/mês |
| Harbor geo-rep egress | ~$500/mês steady |
| **Total steady-state to host 50k tenants** | **~$50k/mês** |

At $10/tenant/mês pricing = $500k ARR. Gross margin positive.

---

## Fora de escopo Phase 4

| Item | Quando |
|---|---|
| > 50k apps (tipo 100k, 500k) | Phase 5 post-launch |
| Multi-cloud (AWS, GCP) | Separate strategic plan |
| ML-based cell placement | Phase 5 (after data) |
| Auto-region expansion | Phase 5 |
| BGP Anycast | **REJEITADO** (infeasible DO) |

---

## Referências

- **Fase anterior:** `docs/plans/cell-architecture-phase3.md`
- **Overview:** `docs/plans/cell-architecture-overview.md`
- **Load test detail:** `docs/plans/scale-50k-plan.md` §Phase 5
- **ADR base:** `docs/meetings/adr/0008-cell-based-architecture.md`
- **ADR Gitea:** `docs/meetings/adr/0011-gitea-scaling.md` (to write Phase 3)
- **ADR PG:** `docs/meetings/adr/0012-pg-sharding.md` (to write Phase 4)

---

## Changelog

- **v1.0 (2026-04-17):** Plano Phase 4 criado — caminho de 5k para 50k. 10 tasks principais + L6 load test como Gate Exit absoluto.
