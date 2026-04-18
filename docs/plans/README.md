# Plans

Planos de engenharia do LoopZero Platform. Cada documento aqui representa uma decisao aprovada, um plano executavel, ou licoes aprendidas em producao real (DigitalOcean).

**Regra:** decisoes sao gated por load tests reais (HTTP 200 verificado), nao estimativas teoricas.

---

## Estado Atual (2026-04-17)

| Nivel | Tenants | Apps | Status |
|-------|---------|------|--------|
| L1 | 10 | 20 | PASSED 10/10 |
| L2 | 50 | 100 | PASSED 50/50 |
| L3 | 100 | 200 | Best 75/100 (v8). Bloqueado: runtime capacity (DO droplet quota) |
| L4+ | 500+ | 1000+ | Requer multi-cell (codigo pronto, zero cells ativas) |

**Blocker principal:** DO droplet quota + Pulumi state drift. Contingencia: `LOOPZERO_LEAN_RESOURCES=true`.

---

## Indice por Categoria

### Roadmap Cell-Based (Phase 0 a 50k)

| Arquivo | Escopo | Quando ler |
|---------|--------|------------|
| [cell-architecture-overview.md](cell-architecture-overview.md) | Master roadmap Phase 0 a 50k, dependencias topologicas, invariants | Primeiro — visao geral de onde estamos |
| [cell-architecture-phase2.md](cell-architecture-phase2.md) | Phase 2a (hardening shared services) + 2b (cell-2 real + chaos video) | Quando Phase 2a iniciar |
| [cell-architecture-phase3.md](cell-architecture-phase3.md) | Automacao: add-cell em 15min, migration, per-cell ArgoCD, PG HA | Apos Phase 2b completa |
| [cell-architecture-phase4.md](cell-architecture-phase4.md) | Otimizacao 5k a 50k: multi-region, Harbor geo-rep, PG partitioning | Apos Phase 3 completa |

### Plano Master de Scale

| Arquivo | Escopo | Quando ler |
|---------|--------|------------|
| [scale-50k-plan.md](scale-50k-plan.md) | Plano completo L0 a L6, 6 fases, 16 semanas, deploys reais, chaos suite | Referencia canonica para load tests |
| [load-test-plan.md](load-test-plan.md) | Detalhamento L0 a L7 (ate 500k), cenarios por nivel, cost projection | Complemento ao scale-50k-plan |

### Foco Imediato

| Arquivo | Escopo | Quando ler |
|---------|--------|------------|
| [50k-immediate-focus.md](50k-immediate-focus.md) | 9 tasks para destravar L3, gates, status de cada task, next-iteration checklist | Diariamente — ponto de retomada do trabalho |

### Learnings (diario de execucao)

Lidos em ordem cronologica para entender a evolucao:

| Arquivo | Escopo |
|---------|--------|
| [scale-50k-day1-learnings.md](scale-50k-day1-learnings.md) | Dia 1: Phase 0 wiring, 9 bugs pipeline (crane distroless, cosign SHA, xargs), 6/10 apps live |
| [scale-50k-day2-learnings.md](scale-50k-day2-learnings.md) | Dia 2: L1 10/10 PASSED (marco historico), 15 bugs corrigidos, callback race, BuildReaper zero-value |
| [scale-50k-l2-learnings.md](scale-50k-l2-learnings.md) | L2 50/50, Phase 0 ativada, Pulumi drift catastrofico, imagePullSecret race |
| [scale-50k-l3-readiness.md](scale-50k-l3-readiness.md) | L3 v1 a v14 iterations, evolucao de bugs cascading para infra sizing |
| [scale-50k-technical-debt.md](scale-50k-technical-debt.md) | 26 bugs em matriz de sobrevivencia a destroy+recreate, cell-ready vs cell-deployed |
| [scale-50k-slo-bottlenecks.md](scale-50k-slo-bottlenecks.md) | 11 gargalos identificados, L2 real 1.15 deploys/min vs SLO 5/min |

### Infraestrutura

| Arquivo | Escopo | Quando ler |
|---------|--------|------------|
| [autoscaling-topology.md](autoscaling-topology.md) | Mapa visual de todos HPAs (12 no mgmt), ArgoCD sharding, behavior config | Ao ajustar HPAs ou debugar scaling |
| [capacity-baselines.md](capacity-baselines.md) | Baselines medidas por subsistema (loopzero-api, Gitea, MinIO, ArgoCD) | Antes de rodar E2E — bench primeiro |
| [minio-distributed-migration.md](minio-distributed-migration.md) | 3 fases: tune standalone, backup, blue/green distributed 4 pods | Prerequisito Phase 2a (T-2a-05) |

### TLS e Edge Routing

| Arquivo | Escopo | Quando ler |
|---------|--------|------------|
| [tls-strategy.md](tls-strategy.md) | 3 issuers (LE Prod, LE Staging, CF Origin CA), rotation policy, fallback | Ao tocar em certs ou issuers |
| [edge-routing-tls-lessons.md](edge-routing-tls-lessons.md) | 9 licoes operacionais: CF routing hibrido, Origin CA exige proxy real, checklist | Antes de mexer em DNS ou TLS |

### Tenant Lifecycle

| Arquivo | Escopo | Quando ler |
|---------|--------|------------|
| [tenant-lifecycle-deletion.md](tenant-lifecycle-deletion.md) | Arquitetura aprovada (MEET-052): controller-native + API intent gateway, state machine, bulk delete | Ao implementar delete/evict de tenant |

---

## Como usar estes planos

- **Daily standup:** abra `50k-immediate-focus.md`, veja qual task T-XX voce esta
- **Sprint planning:** pegue tasks da fase atual seguindo topological order em `cell-architecture-overview.md`
- **Debugging scale:** leia learnings na ordem cronologica, depois `slo-bottlenecks.md`
- **Investor pitch:** `cell-architecture-overview.md` + chaos video + ADR-0008
- **Novo engineer:** leia `cell-architecture-overview.md` primeiro, depois `scale-50k-plan.md`

## Convencoes

- **Gate:** condicao que DEVE ser satisfeita antes de avancar. Nao e sugestao.
- **DoD:** Definition of Done por task. Inclui testes, commits, e artefatos.
- **Veto condition:** requisito de security/SRE que tem poder de veto absoluto.
- **L{N}:** nivel de load test (L1=10 tenants, L2=50, L3=100, L4=500, L5=5k, L6=50k).

## Referencias canonicas

- **Spec tecnico:** `docs/technical.md`
- **ADR-0008:** `docs/meetings/adr/0008-cell-based-architecture.md`
- **MEET-051:** `docs/meetings/records/MEET-2026-04-17-051.md` (Phase 2 rescope)
- **MEET-052:** `docs/meetings/records/MEET-2026-04-17-052.md` (tenant lifecycle)
