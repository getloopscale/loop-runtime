# Cell Architecture — Overview (Phase 0 → 50k)

**Criado:** 2026-04-17
**Última atualização:** 2026-04-17 (v1.0)
**Autoridade:** ADR-0008 (accepted 2026-03-31)
**Status:** roadmap consolidado pós-MEET-051

Este doc é o **overview master** do cell-based scaling. Cada fase tem um plano executável dedicado. Aqui você vê o mapa completo + onde estamos + o que destrava o quê.

---

## 📌 TL;DR

```
Hoje (L3):      100 apps, 1 cell, shared services SPOF
Phase 2:        2 cells chaos-validated          → ~10k apps teórico
Phase 3:       10 cells, migration wired, PG HA  → ~5k apps validados
Phase 4:      100 cells, multi-region             → 50k apps validados
Tempo total:   ~6-9 meses focados
Custo steady-state a 50k: ~$50k/mês
```

**Investor answer:** "PaaS é escalável. Arquitetura aprovada em FAANG-level meeting, Phase 2 demonstra failure isolation via chaos test real, Phases 3-4 automatizam scaling para 50k. Roadmap executável completo commitado."

---

## Status atual das fases

| Fase | Trigger | Status | Plan | Evidence |
|---|---|---|---|---|
| **Phase 0** Foundation | Now | ✅ Deployed | [50k-immediate-focus.md](50k-immediate-focus.md) | cell_id field, PlacementEngine, DynamicRouter wired, `LOOPZERO_DYNAMIC_ROUTER=true` live |
| **Phase 1** Cell-Ready | 50+ tenants (met) | 🟡 75% code, 0% deployed | [cell-architecture-phase2.md](cell-architecture-phase2.md) §Phase 2b T-2b-01..02 | CRD `clustercredentials.loopzero.io` installed, controller written, Pulumi `cell.ts` scaffolded. Zero CRs registered. |
| **Phase 2a** Hardening | Phase 1 code + quota arrived | ❌ Not started | [cell-architecture-phase2.md](cell-architecture-phase2.md) §Phase 2a | Gate #1 open (L3 62-75/100); shared services single-instance |
| **Phase 2b** Cell-2 Activation | Phase 2a Gate Exit | ❌ Blocked | [cell-architecture-phase2.md](cell-architecture-phase2.md) §Phase 2b | — |
| **Phase 3** Automation | Phase 2b complete + 200+ tenants | ❌ Future | [cell-architecture-phase3.md](cell-architecture-phase3.md) | — |
| **Phase 4** 50k scale | Phase 3 complete + investment | ❌ Future | [cell-architecture-phase4.md](cell-architecture-phase4.md) | — |

---

## Dependências topológicas

```
[50k-immediate-focus T-08]
  DO droplet quota (Paulo)
        │
        ▼
[Phase 2a — Prerequisites]
  L3 ≥95/100  Gitea HA  Harbor HA  MinIO dist  PG bump  mgmt head
        │
        ▼
[Phase 2b — Cell-2 Activation]
  2 cells · chaos video · failure isolation proof
        │
        ├──► ADR-0010 (rescope decision)
        └──► Investor demo milestone
        │
        ▼
[Phase 3 — Automation]
  auto-provisioning · migration · per-cell ArgoCD · PG HA · Gitea scale
        │
        ├──► L5 validation (scale-50k-plan.md)
        └──► ADR-0011 Gitea scaling decision
        │
        ▼
[Phase 4 — 50k optimization]
  multi-region · Harbor geo-rep · Prometheus federation · PG partition
        │
        ├──► L6 validation (50k apps live)
        ├──► ADR-0012 PG sharding decision
        └──► Capacity model final (evidence-based)
        │
        ▼
50,000 apps SUSTAINED
```

---

## Plans catalog

### Orchestrating plans

| Arquivo | Escopo | Quando ler |
|---|---|---|
| `docs/plans/50k-immediate-focus.md` | Phase 0 stabilization + Phase 1 partial (what we're doing TODAY) | Imediato — T-08 quota destrava Phase 2a |
| `docs/plans/cell-architecture-overview.md` (este) | Master roadmap Phase 0→50k | Para entender onde estamos no caminho |
| `docs/plans/scale-50k-plan.md` | Load-test progression L0→L6 + chaos | Execution playbook para validation |

### Phase-specific architecture plans

| Arquivo | Fase | Linhas |
|---|---|---|
| `docs/plans/cell-architecture-phase2.md` | Phase 2a + 2b | 833 |
| `docs/plans/cell-architecture-phase3.md` | Phase 3 | ~500 |
| `docs/plans/cell-architecture-phase4.md` | Phase 4 | ~500 |

### Supporting plans

| Arquivo | Escopo |
|---|---|
| `docs/plans/minio-distributed-migration.md` | T-2a-05 detalhado |
| `docs/plans/scale-50k-l3-readiness.md` | L3 iterations v1-v14 lessons |
| `docs/plans/scale-50k-technical-debt.md` | Cross-cutting debt + cell-ready/deployed analysis |
| `docs/plans/capacity-baselines.md` | 4 bench suites + real measurements |

---

## ADR catalog

| ADR | Topic | Status |
|---|---|---|
| ADR-0008 | Cell-Based Architecture (4 phases) | ACCEPTED 2026-03-31 |
| ADR-0010 | Phase 2 rescope (2a + 2b, PG-as-authority, CF KV rejected) | TO WRITE (pós-MEET-051) |
| ADR-0011 | Gitea scaling decision (per-cell vs cluster vs bare-git+S3) | TO WRITE Phase 3 |
| ADR-0012 | PG sharding strategy (partition vs per-region vs Citus) | TO WRITE Phase 4 |

---

## Meeting records

| Record | Topic | Verdict |
|---|---|---|
| MEET-2026-03-31-024 | ArgoCD scalability (remove? keep? shard?) | APPROVED_WITH_CONDITIONS (keep + sharding) |
| MEET-2026-04-17-051 | Phase 2 Multi-Cell Activation | DEFERRED → RESCOPED (2a + 2b) |
| MEET-future (Phase 3) | Gitea scaling (ADR-0011) | TBD |
| MEET-future (Phase 4) | PG sharding (ADR-0012) | TBD |

---

## Critical invariants (across all phases)

Do meeting MEET-051, essas invariants precisam sobreviver a todas as fases:

1. **PG é authority para `tenant → cell`.** Nunca CF KV (rejeitado), nunca in-memory estado como único storage.
2. **Placement tem advisory lock.** TOCTOU em capacity-check = dado corrompido.
3. **Kubeconfig nunca em CRD spec.** Secret com rotation automatizado.
4. **ArgoCD manual-sync stays.** ADR-0007. Auto-sync rejeitado.
5. **Per-cell Gitea replicas rejected.** Central Gitea (Phase 2-3) → decision Phase 3 se vai clustering ou bare-git.
6. **Cell failure isolation é chaos-validated, não diagrama.** DoD de cada fase tem chaos test explícito.
7. **Scale decisions backed por measurement.** L3→L6 load tests gating progress, não matemática projetada.

---

## Fora de escopo (permanente)

Esses items foram rejeitados em meetings e/ou são incompatíveis com nossa stack. Listados aqui para evitar re-discussões:

| Item | Razão |
|---|---|
| BGP Anycast | Infeasible em DO (requer ASN + peering) |
| Multi-cloud (AWS/GCP/Azure) | Estratégia separada, não cell-scaling |
| Tenant migration baseada em latência geolocation | Phase 5 post-50k (data-driven optimization) |
| Kubernetes Federation v1/v2 | Descontinuado; usamos cell pattern |
| GKE Multi-Cluster Gateway / Karmada | Cloud-locked; cell pattern é agnostic |
| Per-tenant cluster | Economicamente inviável < 10k tenants |

---

## Timeline consolidado (estimativa honesta)

**Premissas:** 2-3 engineers focus, zero blockers humanos fora do controle (DO quota, CF, third-party bugs).

```
Mês 1:      Phase 2a (hardening)                  [3 sprints]
Mês 1-2:    Phase 2b (cell-2 activation + chaos)  [5 sprints]
              ━━━ INVESTOR DEMO MILESTONE ━━━
Mês 3:      Phase 3 (automation)                   [3-4 sprints]
              ━━━ L5 PASS (5k tenants) ━━━
Mês 4-5:    Phase 4 tasks T-4-01..T-4-07          [4 sprints]
Mês 5-6:    Phase 4 T-4-08 L6 progressive         [2-3 sprints]
              ━━━ L6.c PASS (50k apps live) ━━━
Mês 6:      Phase 4 T-4-09 runbooks + T-4-10 cap model
              ━━━ 50k VALIDATED, investor proof ━━━
```

**Total:** ~6 meses de heads-down engineering para ir de hoje → 50k validated. Com 3 engineers, possivelmente 4-5 meses. Com 1 engineer solo, 9-12 meses.

---

## How to use this doc

- **Daily standups:** Qual task T-X-YY você está?
- **Sprint planning:** Pegar tasks da fase atual seguindo topological order
- **Investor pitch:** Mostre este doc + video chaos + ADR-0008
- **Technical interview para novo hire:** "Leia este doc. Questões?"
- **Incident post-mortem:** Qual fase era target, que invariant quebrou?

---

## Referências canônicas

- ADR-0008 Cell-Based Architecture — `docs/meetings/adr/0008-cell-based-architecture.md`
- MEET-051 Phase 2 rescope — `docs/meetings/records/MEET-2026-04-17-051.md`
- Scale 50k plan (load-test) — `docs/plans/scale-50k-plan.md`
- Immediate focus (Phase 0 stabilization) — `docs/plans/50k-immediate-focus.md`

---

## Changelog

- **v1.0 (2026-04-17):** Overview criado amarrando Phase 2/3/4 plans + scale-50k-plan + MEET-051 decisions.
