# Plans

Planos de engenharia do LoopZero Platform. Cada documento representa uma decisao aprovada ou um plano executavel. Decisoes sao gated por load tests reais (HTTP 200 verificado), nao estimativas.

---

## Estado Atual (2026-04-19)

### Scale Validation

| Nivel | Tenants | Apps | Status |
|:-----:|:-------:|:----:|--------|
| L1 | 10 | 20 | :white_check_mark: PASSED 10/10 |
| L2 | 50 | 100 | :white_check_mark: PASSED 50/50 |
| L3 | 100 | 200 | :arrow_forward: Quota desbloqueada — pronto para re-run |
| L4+ | 500+ | 1000+ | Requer multi-cell ativado |

### Blockers Ativos

| Blocker | Severidade | Plano | Status |
|---------|:----------:|-------|--------|
| Alertmanager → /dev/null | :red_circle: SRE VETO | `multi-cloud-aws-do-plan.md` P0 | Bloqueia tudo |
| Multi-cell Phase B (ClusterCredential CR) | :yellow_circle: HIGH | `multi-cell-default-roadmap.md` B-01 | Codigo pronto, falta Pulumi |
| Multi-cell Phase C (flip DynamicRouter) | :yellow_circle: HIGH | `multi-cell-default-roadmap.md` C-01 | Requer Phase B |
| AWS Pulumi provider (stub) | :yellow_circle: HIGH | `multi-cloud-aws-do-plan.md` P2 | Nao iniciado |

### Maturidade

```
Codigo:         ████████████████████░░ 90%
Testes:         ████████████████░░░░░░ 75%
Infra DO:       ████████████████████░░ 90%
Multi-cell:     ████████████████░░░░░░ 75%  (codigo done, infra/validation pendente)
Multi-cloud:    ██░░░░░░░░░░░░░░░░░░░░ 10%
Self-hosted:    ░░░░░░░░░░░░░░░░░░░░░░  0%
```

---

## Planos Ativos

### 1. Multi-Cell Default (MEET-053)

Ativar multi-cell como default para fail-fast: descobrir bugs no path multi-cell em producao desde Phase 0.

| Arquivo | Status | Tasks |
|---------|--------|:-----:|
| [multi-cell-default-roadmap.md](multi-cell-default-roadmap.md) | Phase A+D done. **Phase B NEXT.** | 14/21 |

Fases: A (fix safety net) :white_check_mark: → B (Pulumi CR) :arrow_forward: → C (flip switch) → D (harden) :white_check_mark:

### 2. Multi-Cloud AWS + DO (MEET-054)

Instalacao full multi-cluster em AWS e DigitalOcean. Crossplane rejeitado — modelo e Helm BYOI + Pulumi provider.

| Arquivo | Status | Fases |
|---------|--------|:-----:|
| [multi-cloud-aws-do-plan.md](multi-cloud-aws-do-plan.md) | **Phase 0 NEXT** (fix Alertmanager) | 0/5 |

Fases: P0 (Alertmanager) :arrow_forward: → P1 (abstrair lock-ins) → P2 (AWS provider) → P3 (Helm + CLI) → P4 (E2E)

### 3. Cell Architecture (ADR-0008)

Roadmap de escala de 100 a 50k apps via celulas autonomas.

| Arquivo | Escopo | Quando ler |
|---------|--------|------------|
| [cell-architecture-overview.md](cell-architecture-overview.md) | Master roadmap Phase 0→50k, dependencias, invariants | Visao geral |
| [cell-architecture-phase2.md](cell-architecture-phase2.md) | Phase 2a (hardening) + 2b (cell-2 + chaos) | Quando Phase 2a iniciar |
| [cell-architecture-phase3.md](cell-architecture-phase3.md) | Automacao: add-cell em 15min, per-cell ArgoCD, PG HA | Apos Phase 2b |
| [cell-architecture-phase4.md](cell-architecture-phase4.md) | Multi-region, Harbor geo-rep, PG partitioning | Apos Phase 3 |

### 4. Scale Validation

| Arquivo | Escopo | Quando ler |
|---------|--------|------------|
| [scale-50k-plan.md](scale-50k-plan.md) | Plano L0→L6, 6 fases, deploys reais, chaos suite | Playbook para load tests |
| [load-test-plan.md](load-test-plan.md) | Detalhamento L0→L7, cenarios por nivel, cost projection | Complemento |

**Capacity suite:** `infra/tests/capacity/` — 5 testes validam cell-budget.md (Go memory, BuildKit, ArgoCD, Gitea, pod memory). Run: `./infra/tests/capacity/run-capacity.sh all`

### 5. Operacional

| Arquivo | Escopo | Quando ler |
|---------|--------|------------|
| [edge-routing-tls-lessons.md](edge-routing-tls-lessons.md) | 9 licoes: CF routing, Origin CA, checklist | Antes de mexer em DNS/TLS |

---

## Arquivo Morto

Planos concluidos, superados, ou absorvidos por planos ativos. Movidos para `olds/`.

| Arquivo | Razao |
|---------|-------|
| `50k-immediate-focus.md` | Absorvido por `multi-cell-default-roadmap.md` e `multi-cloud-aws-do-plan.md` |
| `scale-50k-day1-learnings.md` | Historico — learnings incorporados nos planos ativos |
| `scale-50k-day2-learnings.md` | Historico |
| `scale-50k-l2-learnings.md` | Historico |
| `scale-50k-l3-readiness.md` | Historico |
| `scale-50k-technical-debt.md` | Debt absorvido pelos roadmaps ativos |
| `scale-50k-slo-bottlenecks.md` | Gargalos enderecados nos planos ativos |
| `capacity-baselines.md` | Substituido pela capacity suite em `infra/tests/capacity/` |
| `autoscaling-topology.md` | Referencia operacional, nao plano ativo |
| `minio-distributed-migration.md` | Prerequisito Phase 2a (consultar quando iniciar) |
| `tenant-lifecycle-deletion.md` | MEET-052 (consultar quando implementar delete) |
| `tls-strategy.md` | Referencia (consultar quando tocar em certs) |

---

## Proximos Passos (prioridade)

1. **Fix Alertmanager /dev/null** — SRE VETO. `multi-cloud-aws-do-plan.md` P0-01.
2. **Multi-cell Phase B** — Pulumi ClusterCredential CR. `multi-cell-default-roadmap.md` B-01.
3. **Multi-cell Phase C** — Flip DynamicRouter. `multi-cell-default-roadmap.md` C-01.
4. **Re-run L3 100/100** — Quota desbloqueada. `scale-50k-plan.md`.
5. **Abstrair lock-ins DO** — `multi-cloud-aws-do-plan.md` P1.
6. **AWS Pulumi provider** — `multi-cloud-aws-do-plan.md` P2.

---

## Convencoes

- **Gate:** condicao que DEVE ser satisfeita antes de avancar
- **DoD:** Definition of Done (testes, commits, artefatos)
- **Veto:** requisito de security/SRE com poder absoluto
- **L{N}:** nivel de load test (L1=10, L2=50, L3=100, L4=500, L5=5k, L6=50k)
- **BYOI:** Bring Your Own Infrastructure (cliente ja tem K8s/PG/S3)

## Meetings

| Record | Topico | Verdict |
|--------|--------|---------|
| [MEET-051](../meetings/records/MEET-2026-04-17-051.md) | Phase 2 rescope | DEFERRED → RESCOPED |
| [MEET-052](../meetings/records/MEET-2026-04-17-052.md) | Tenant lifecycle deletion | APPROVED |
| [MEET-053](../meetings/records/MEET-2026-04-18-053.md) | Multi-cell as default | APPROVED_WITH_CONDITIONS |
| [MEET-054](../meetings/records/MEET-2026-04-18-054.md) | Multi-cloud strategy | APPROVED_WITH_CONDITIONS |

## Referencias

- **Spec:** `docs/technical.md`
- **ADR-0008:** Cell-Based Architecture
- **Cell Budget:** `docs/architecture/cell-budget.md`
- **Runbooks:** `docs/runbooks/`
