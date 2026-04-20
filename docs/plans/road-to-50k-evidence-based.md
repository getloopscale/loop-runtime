# Road to 50k — Evidence-Based Plan

**Criado:** 2026-04-19
**Ultima verificacao:** 2026-04-20 (infra recriada do zero, multi-cell ativo em producao)
**Baseado em:** Deep research (4 agentes, ~150 arquivos inspecionados, 32 itens verificados)
**Status:** Sprint 0 em execucao (next-sprint-execution.md)

> Este documento e um snapshot. O codigo e mais autoritativo que qualquer roadmap.
> Para verificar status atual: `grep -r "DONE\|TODO\|STUB" docs/plans/road-to-50k-evidence-based.md`

---

## Scorecard Real (verificado 2026-04-19)

```
DONE:             20/32 items (62%)
PARTIAL:           6/32 items (19%)
STUB/NOT STARTED:  6/32 items (19%)
```

> Nota: Items 2 (work queue) e 23 (VAP signing) sao "implemented but not wired"
> — codigo existe mas nao esta integrado no runtime. Pattern documentado no CLAUDE.md.

### Inventario Completo

| # | Item | Status | Evidencia |
|---|------|:------:|-----------|
| 1 | Leader election (generation-based) | :white_check_mark: DONE | `api/internal/leader/generation.go` 251 LOC, production |
| 2 | Work queue (PG SKIP LOCKED) | :yellow_circle: PARTIAL | `api/internal/workqueue/queue.go` 169 LOC, 6 queues. **Not wired:** main.go nao importa o package. Reapers rodam como goroutines diretas. |
| 3 | Build reaper | :white_check_mark: DONE | `api/internal/services/build_reaper.go` 313 LOC |
| 4 | Release reaper | :white_check_mark: DONE | `api/internal/services/release_reaper.go` 292 LOC |
| 5 | Admission control (cell+tenant) | :white_check_mark: DONE | `api/internal/admission/build.go` 248 LOC, 3 guards |
| 6 | PG circuit breaker | :white_check_mark: DONE | `api/internal/admission/pg_breaker.go` 215 LOC |
| 7 | Static frontend (CF Worker+KV) | :white_check_mark: DONE | `infra/cloudflare/worker.mjs` 110 LOC + `api/internal/cfkv/` |
| 8 | Image signing (cosign pipeline) | :white_check_mark: DONE | `api/internal/argo/cosign.go` 87 LOC + CI signing |
| 9 | Network policies | :white_check_mark: DONE | `infra/helm/network-policies/` 298 LOC, 6 policies enforced |
| 10 | Build cache optimization | :white_check_mark: DONE | CWT cache-restore/cache-save templates, `mc` image, configurable timeout. Verify: `infra/helm/argo-workflows/templates/cwt-cache-*` |
| 11 | ArgoCD sharding (3 replicas) | :white_check_mark: DONE | `infra/helm/argocd/values.yaml` L31-38 |
| 12 | Chaos suite (9 scenarios) | :white_check_mark: DONE | `infra/tests/chaos/chaos_test.go` 388 LOC |
| 13 | Cell provisioning (cell.ts) | :white_check_mark: DONE | `infra/pulumi/src/components/cell.ts` 279 LOC |
| 14 | Mgmt node upgrade (prod) | :white_check_mark: DONE | `Pulumi.prod.yaml` medium nodes |
| 15 | Multi-cell code (MEET-053 Phase A) | :white_check_mark: DONE | 14/21 tasks, DynamicRouter, placement, admission |
| 16 | Capacity validation suite | :white_check_mark: DONE | `infra/tests/capacity/` 11 files (10 Go + run-capacity.sh) |
| 17 | Placement constraints (region-aware) | :white_check_mark: DONE | `api/internal/placement/constraints.go` L113-118 |
| 18 | Gitea HA (replicas=2, Redis single-pod) | :white_check_mark: DONE | Gitea replicas=2, Redis session/cache/queue. **Nota:** Redis e single-pod (replicas=1, sem persistence). Risco documentado na tabela de riscos. |
| 19 | Harbor (S3 backend, PG cap) | :yellow_circle: PARTIAL | PG cap 10 conns done, core replicas=1 (not 2) |
| 20 | MinIO distributed (values.production.yaml) | :white_check_mark: DONE | `infra/helm/minio/values.production.yaml` mode:distributed, 4 replicas |
| 21 | PG bump | :white_check_mark: DONE | Managed DO PG, dbNodeCount:3, prod medium tier |
| 22 | Per-cell ArgoCD (cell.ts creates per-cell) | :white_check_mark: DONE | `cell.ts` L193-240: per-cell ArgoCD Helm + repo-creds |
| 23 | Image signature verification (VAP) | :yellow_circle: PARTIAL | VAP policy exists (audit mode, failurePolicy:Ignore). Cosign signing in pipeline DONE. Enforcement (flip audit→deny) NOT DONE. |
| 24 | Tenant health dashboards | :white_check_mark: DONE | `infra/helm/monitoring/dashboards/` per-tenant-health.json, cell-health.json, cell-saturation.json |
| 25 | HPAs | :white_check_mark: DONE | 12 HPAs: loopzero-api (2-10), loopzero-cloud (1-5), gitea (4-12), dashboard (1-3), harbor-core (1-5), harbor-registry (1-8), harbor-jobservice (1-3), argocd-repo-server (2-10), argocd-server (1-3), hydra (1-3), kratos (1-3), mgmt-traefik (2-6) |
| 26 | Inter-cell migration | :red_circle: NOT STARTED | Tenant lifecycle SM + reconciler exist since Sprint 1 (Provisioning→Active→Suspended→Deleting). Inter-cell migration logic (ACTIVE@cell-A→ACTIVE@cell-B) NOT STARTED. |
| 27 | Auto-cell-provisioning trigger | :yellow_circle: PARTIAL | Metrics + admission + cell.ts existem. Falta trigger (80% threshold → provision) |
| 28 | Multi-region deployment | :red_circle: NOT STARTED | Single region (sfo3/nyc3) |
| 29 | PG partitioning | :yellow_circle: PARTIAL | cell_id denormalized (migration 29), PARTITION BY pendente |
| 30 | Prometheus federation | :red_circle: NOT STARTED | Thanos/Mimir decision pending |
| 31 | Harbor geo-replication | :red_circle: NOT STARTED | Single instance |
| 32 | Harbor HA (2 replicas) | :yellow_circle: PARTIAL | S3 backend done, core replicas=1 |

---

## O que REALMENTE falta para 50k

### Tier 0 — Prerequisito Absoluto (BLOCKER)

Enquanto nao resolvido, NENHUMA gate de capacity e confiavel.

| Task | DoD |
|------|-----|
| **Fix Alertmanager /dev/null** | Receivers configurados com Slack webhook real. Teste: `amtool alert add alertname=test severity=critical` → mensagem no #loopzero-critical em <60s. Canal: #loopzero-alerts (warning), #loopzero-critical (critical). |

### Tier 1 — Multi-cell em producao (Sprint atual, 5-7 dias)

Plano detalhado: `next-sprint-execution.md` (18 microtasks com DoD).
MEET-053 blockers B1-B9 ja resolvidos em Phase A.

| Task | Gate |
|------|------|
| ~~S1: Fix Alertmanager /dev/null~~ | Movido para Tier 0 (BLOCKER) |
| S2: ClusterCredential CR via Pulumi | Connected=True |
| S3: Flip DynamicRouter default | 1 deploy E2E + rollback testado |
| S4: Capacity baselines + L3 100/100 | 5 baselines medidos + L3 >=95/100 |

**Capacity budget assumptions (gates explicitos):**
- [ ] Go memory peak medido vs projecao 200-300 Mi
- [ ] BuildKit concurrent ceiling medido vs ~4/node
- [ ] ArgoCD mem/app medido vs ~500 kB
- [ ] Gitea push p95 medido vs <2s @100 repos
- [ ] Pod memory avg medido vs ~50 Mi

**Rollback:** `LOOPZERO_SINGLE_CELL=true` (testado), `LOOPZERO_DYNAMIC_ROUTER=false` (fallback).

### Tier 2 — Cell-2 + chaos validated (1-2 semanas)

**Prerequisito:** Tier 1 completo + L3 >=95/100.

| # | Task | DoD |
|---|------|-----|
| T2-01 | Harbor core replicas=2 | 2 pods running, push/pull ok |
| T2-02 | Habilitar admission control (`LOOPZERO_ADMISSION_CONTROL=true`) | 429 quando cell cheia |
| T2-03 | Wiring cell.ts → infra.ts (loop de cells no Pulumi config) | `pulumi preview` mostra cell-2 |
| T2-04 | Provisionar cell-2 (2o runtime cluster) | ClusterCredential Connected=True, DynamicRouter descobre 2 cells |
| T2-05 | CF Worker cell routing para cell-2 + parametrizar BASE_DOMAIN (hardcoded `dev.loopzero.example` em worker.mjs:2) | Requests roteados para tenants em cell-2, dominio configuravel |
| T2-06 | Chaos suite em 2 cells | Cell-1 pod kill → cell-2 tenants nao afetados |
| T2-07 | L3 100/100 com 2 cells (50/cell) | 100/100 PASSED com distribuicao ~50/50 |

**Custo adicional:** ~$80/mo (2 nodes s-2vcpu-4gb para runtime-2).

**Rollback:** Deletar cell-2 ClusterCredential → tenants orphaned mas cell-1 funciona. Migrar tenants manualmente (UPDATE tenants SET cell_id = 'cell-default').

### Tier 3 — L4 500 tenants, 10 cells (2-3 semanas)

**Prerequisito:** Tier 2 completo + chaos validated.

| # | Task | DoD |
|---|------|-----|
| T3-01 | Admission tuning (MaxConcurrentPerCell calibrado) | 429 rate <5% sob L4 |
| T3-02 | PG connection pool tuning per cell count | Pool saturation <60% sob 10 cells |
| T3-03 | Image signature enforce (flip VAP audit→enforce) | Pods sem assinatura rejeitados |
| T3-04 | Provisionar cells 3-10 via cell.ts loop | 10 cells Connected=True |
| T3-05 | L4 500 tenants (10 cells) | 500/500 PASSED, deploys <5min |

**Custo:** ~$800/mo (10 runtime clusters × $80).

**Rollback:** Reduzir cells para 2. PlacementEngine para de atribuir para cells removidas. Tenants existentes ficam orphaned — precisa de UPDATE manual ou tenant migration (Tier 4).

### Tier 4 — L5 5k tenants, 100 cells (4-6 semanas)

**Prerequisito:** Tier 3 completo + L4 500/500.

| # | Task | DoD |
|---|------|-----|
| T4-01 | Auto-cell provisioning trigger (80% capacity → cria cell) | Cell adicionada automaticamente em <15min |
| T4-02 | Inter-cell migration MVP (extend existing tenant SM with migration states: ACTIVE@A→PREPARING@B→DUAL→DRAINING@A→ACTIVE@B) | 1 tenant migrado entre cells sem downtime |
| T4-03 | Prometheus federation (Thanos sidecar ou Mimir) | Metricas centrais de 100 cells |
| T4-04 | PG read replicas | Write latency <10ms, read <5ms com 100 cells |
| T4-05 | L5 5k tenants (100 cells) | 5000/5000 PASSED |

**Custo:** ~$8,000/mo (100 cells × $80).

**Rollback:** Desabilitar auto-provisioning. Freeze em cell count. Migracoes em flight completam ou falham (state machine handles).

### Tier 5 — L6 50k apps sustained (6-8 semanas)

**Prerequisito:** Tier 4 completo + L5 5000/5000.

| # | Task | DoD |
|---|------|-----|
| T5-01 | Multi-region (DO SFO3 + NYC3) | Cells em 2 regioes |
| T5-02 | Harbor geo-replication (<1min sync) | Image pull de regiao local |
| T5-03 | PG partitioning por cell_id (PARTITION BY LIST) | Query time linear com cell count |
| T5-04 | L6 50k apps sustained 30min | 50000 apps LIVE |
| T5-05 | Capacity model final (evidence-based) | Documento MEASURED |

**Custo:** ~$45,000/mo (500 cells, 2 regioes).

**Rollback:** Reduzir para single-region. PG partitioning e aditivo (nao muda queries existentes).

---

## Riscos por Tier

| Risco | Tier | Severidade | Mitigacao |
|-------|:----:|:----------:|-----------|
| Alertmanager /dev/null | 0 | :red_circle: BLOCKER | Tier 0 fix — prerequisito absoluto |
| CF Worker hardcoded `dev.loopzero.example` | 2 | :orange_circle: HIGH | Parametrizar dominio em `worker.mjs` L1 |
| Gitea NFS (ReadWriteMany) SPOF | 3+ | :orange_circle: HIGH | 2 replicas compartilham NFS. NFS down = ambas down. Migrar para object storage (Gitea 1.21+). |
| Redis single-pod para Gitea sessions | 3+ | :yellow_circle: MEDIUM | Redis crash = session loss → builds falham no clone. Adicionar Redis replicas. |
| ~~Harbor/ArgoCD sem HPA~~ | — | ~~MEDIUM~~ | RESOLVIDO: 12 HPAs existem |
| Work queue not wired | 2+ | :yellow_circle: MEDIUM | `api/internal/workqueue/` existe mas main.go nao importa. Debt invisivel — ou wire ou remover. |
| cell.ts not imported | 2 | :orange_circle: HIGH | `createCell()` existe mas `index.ts` nao importa. T2-03 e o wiring task. |
| Mgmt node 74% memory idle | 2 | :orange_circle: HIGH | Prod ja bumped to medium |
| DO kubeconfig expira | 2 | :orange_circle: HIGH | Secret Watch no controller (D-02 done) |
| Advisory lock 5s timeout cross-cloud | 5 | :orange_circle: HIGH | Central PG aceita latencia; PG per-region em Tier 5 |
| Single PG para 10+ cells | 3 | :orange_circle: HIGH | Connection pool tuning (T3-02) |
| ArgoCD AppSet controller single-threaded | 4 | :yellow_circle: MEDIUM | Per-cell ArgoCD (cell.ts done) |

---

## Timeline Realista (revisada pos-review)

```
Tier 1:  ████░░░░░░░░░░░░░░░░  5-7 dias     → L3 + multi-cell
Tier 2:  ██████░░░░░░░░░░░░░░  1-2 semanas  → 2 cells + chaos
Tier 3:  ██████████░░░░░░░░░░  2-3 semanas  → 10 cells + L4
Tier 4:  ██████████████░░░░░░  4-6 semanas  → 100 cells + L5
Tier 5:  ████████████████████  6-8 semanas  → multi-region + L6
                               ─────────────────────────────
                               Total: 3-5 meses (1 engineer)
                                      2-3 meses (3 engineers)
```

## Custo Projetado por Tier (DO)

| Tier | Cells | Custo Infra/mo | Custo Acumulado |
|:----:|:-----:|:--------------:|:---------------:|
| 1 | 1 | ~$180 | $180 |
| 2 | 2 | ~$260 | $260 |
| 3 | 10 | ~$980 | $980 |
| 4 | 100 | ~$8,180 | $8,180 |
| 5 | 500 | ~$40,180 | $40,180 |

Formula: $180 (base mgmt+build+PG) + $80/cell (2 nodes s-2vcpu-4gb).

---

## O que NAO precisa ser feito

| Item | Razao |
|------|-------|
| Crossplane | MEET-054: rejeitado. Cliente ja tem infra. |
| CAPI | Pulumi cria clusters (raro). CAPI overkill. |
| AWS provider (agora) | Escalar DO primeiro. AWS e pos-50k. |
| PG Citus/CockroachDB | Partitioning nativo resolve ate 50k (foundations feitas: cell_id denormalized). |
| Per-tenant cluster | Inviavel <10k tenants. |
| K8s Federation | Descontinuado. Cell pattern substitui. |
| `postgresql-ha/` Helm chart | Diretorio vazio, placeholder. LoopZero usa DO Managed PG (HA by default). Remover ou documentar. |

---

## Mapa de Dependencias

```
Tier 1 (5-7d) ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  S1 Alertmanager → S2 ClusterCredential → S3 DynamicRouter
  (MEET-053 B1-B9 ja resolvidos)            → S4 L3 + baselines
                                                       │
Tier 2 (1-2w) ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  T2-01 Harbor HA ──┐
  T2-02 Admission ──┼──► T2-03 cell.ts → T2-04 cell-2 → T2-06 chaos
                    └──► T2-05 CF routing               → T2-07 L3×2
                                                              │
Tier 3 (2-3w) ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  T3-01 Admission tuning ─┐
  T3-02 PG pool tuning ───┼──► T3-04 cells 3-10 → T3-05 L4 500
  T3-03 Image enforce ────┘
                                        │
Tier 4 (4-6w) ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  T4-01 Auto-provision ──┐
  T4-02 Migration MVP ───┼──► T4-05 L5 5k
  T4-03 Prometheus fed ──┘    T4-04 PG replicas
                                    │
Tier 5 (6-8w) ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  T5-01 Multi-region ──┐
  T5-02 Harbor geo-rep ┼──► T5-04 L6 50k SUSTAINED
  T5-03 PG partitioning┘    T5-05 Capacity model
```

---

## Referencias

- **Sprint atual:** `docs/plans/next-sprint-execution.md` (18 microtasks)
- **Multi-cell roadmap:** `docs/plans/multi-cell-default-roadmap.md` (14/21 done)
- **Multi-cloud plan:** `docs/plans/multi-cloud-aws-do-plan.md` (5 fases)
- **MEET-053:** Multi-cell as default
- **MEET-054:** Multi-cloud strategy (Crossplane rejected)
- **ADR-0008:** Cell-Based Architecture
- **Cell budget:** `docs/architecture/cell-budget.md`
- **Capacity suite:** `infra/tests/capacity/run-capacity.sh`
