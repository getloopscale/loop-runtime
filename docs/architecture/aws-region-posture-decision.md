---
title: "AWS Region Posture — Decision Memo (single vs sharded vs active-active)"
generated: { by: process:m6-loopzero-bottleneck-simulator-operationalization, at: 2026-07-02 }
status: draft
name: aws-region-posture-decision
description: MADR decision memo — single vs sharded vs active-active region posture for the AWS re-platform, quantified by the LoopZero bottleneck simulator (capsim). Every number is a LIVE capsim_* MCP output.
type: decision
created_at: 2026-07-02
updated_at: 2026-07-02
updated_by: M6 (loopzero-bottleneck-simulator operationalization)
workflow_status: proposed
---

# AWS Region Posture — Decision Memo (single vs sharded vs active-active)

> **Status:** proposed · **Date:** 2026-07-02 · **Deciders:** platform + re-platform owner
> **Method:** every quantitative claim is a LIVE `capsim_*` MCP output (LoopZero bottleneck simulator,
> M0-M5). See § Reproduction to re-run each. No number is from memory.


> [!IMPORTANT]
> ## Correção — 2026-08-07: a premissa de capacidade está errada
>
> **Toda a simulação abaixo foi rodada com `maxPerCell = 500`. O valor real é `5000`.**
>
> | Origem | Valor | Papel |
> |---|---|---|
> | `api/internal/config/config.go:497` | `envInt("MAX_TENANTS_PER_CELL", 5000)` | **default real** |
> | `charts/loopzero-api/values.yaml:121` | `maxTenantsPerCell: "5000"` | valor entregue |
> | `api/internal/placement/engine.go:90-91` | `if maxPerCell <= 0 { maxPerCell = 500 }` | fallback do construtor |
>
> O `500` só é alcançado quando o construtor recebe `<= 0`, e a config sempre passa `5000`.
> É um fallback defensivo — mas é o número que aparece no comentário do código, e
> provavelmente foi dali que a simulação o tirou.
>
> **O que muda, com o valor real:**
>
> - **50.000 tenants são ~10 cells, não 100.** A contagem de clusters cai por um fator de 10.
> - O gargalo `cell_count` (ρ=1.56, CRITICAL) **provavelmente não existe** — ele é função
>   direta da contagem de cells.
> - A **Fase 1 ("do now") seria um downgrade**: elevar `maxPerCell` "toward ~2000" é *reduzir*
>   de 5000 para 2000, multiplicando o número de clusters em vez de diminuí-lo. **Não execute
>   a Fase 1 como escrita.**
> - A economia atribuída à Fase 1 (−$31.560/mês) é, em boa parte, economia que **já está
>   realizada** hoje.
>
> **O que NÃO muda:** a Fase 4 (tier de request — BFF e engine_api) e a alavanca de *prefix
> delegation* não dependem de `maxPerCell`. A escolha de posture *sharded* continua plausível,
> mas o memo alerta ele próprio que decidir multi-região antes de corrigir `maxPerCell` é a
> ordem errada — e é exatamente essa a situação.
>
> **Os números originais foram preservados**, não reescritos: são a saída real de uma
> simulação datada, e apagá-los esconderia a premissa que os produziu. A correção honesta é
> **re-rodar o `capsim` com `maxPerCell=5000`** e registrar o novo resultado abaixo. Até lá,
> trate cada número desta página como computado sobre premissa incorreta.
>
> Medido e registrado em `docs/decisions/2026-08-07-capi-byoc-cluster-lifecycle-adr.md` § D16.

## Context and problem statement

The AWS re-platform (ADR 2026-06-17) targets a single AWS region today; region is a state-prefix
dimension (`env/region/unit`) but no multi-region topology exists. At the 50k-tenant target, we must
decide the **region posture** — single-region, multi-region **sharded** (sticky cells split across
regions, no cross-region replication), or **active-active** (cross-region replication with failover)
— and, prior to that, whether the current capacity choices are even sound.

The simulator's review of the current posture is unambiguous:

> `capsim_review {tenants:50000}` → **score 32 / CRITICAL**. Bottlenecks: `cell_count` ρ=1.56
> (100 EKS clusters via `maxPerCell=500` — **premissa incorreta, ver correção acima; com o valor
> real de 5000 são ~10 cells**), `bff` ρ=1.04 (saturated); single-region SLA falls to 95%
> during a full-region outage.

So the decision is two-layered: (A) fix capacity, then (B) choose the region posture.

## Decision drivers

- **Cost/month** at the 50k target (and how it scales to 100k).
- **Region blast radius** — fraction of tenants lost if one region fails.
- **SLA** during a region outage.
- **Architecture review score** (capsim rubric, higher = sounder).
- **Cost of resilience** — Δ$/mo vs single-region for each posture.
- **Reversibility / scale-sensitivity** — does the cost hold as tenants grow?

## Considered options (LIVE `capsim_compare {tenants:50000, compareRegions:3}`)

| Posture | Cost/mo | Region blast radius | Region-outage SLA | Review score | Cost of resilience |
|---|---|---|---|---|---|
| **single** (1 region) | $76,656 | 100% (total outage) | 95% | 32 | — |
| **sharded** (3 regions, sticky) | $82,434 | 33% | 98.32% | 37 | **+$5,778** |
| **active-active** (3 regions, replicated) | $198,581 | 0% | 99.98% | 37 | **+$121,925** |

† *SLA column:* single (95%) and active-active (99.98%) are **nominal** region-outage availability;
sharded's **98.32%** is **expected per-tenant availability** during the outage (33% of tenants at the
95% DR floor, 67% at ~99.99%), not a nominal SLA. For an SLA-*contract* decision (Phase 3) compare
against the nominal figures. *Review score* single/sharded/aa = 32/37/37 — the rubric caps the
active-active penalty (resilience-ranking invariant), so it does not discriminate sharded vs aa; the
deferral of active-active rests on **cost + SLA-contract need**, not on the score.

Scale check (`capsim_compare {tenants:100000, compareRegions:3}`): sharded's cost of resilience stays
**flat at +$5,778** (per-region control-plane is constant); active-active's rises to **+$234,611**
(replicating compute scales with tenants). Sharded is scale-insensitive; active-active is not.

## The capacity levers come FIRST (before any multi-region spend)

Two capacity levers dominate the cost and the review score — and both are far cheaper than any
region posture:

| Lever | Call | Effect |
|---|---|---|
| ~~**Raise `maxPerCell` 500→2000**~~ **NÃO EXECUTAR** | `capsim_cost {tenants:50000, maxPerCell:2000}` | ~~$76,656 → **$45,096**; cells 100→25~~ — **o valor real já é 5000; isto seria um downgrade** (ver correção no topo) |
| **Enable prefix delegation** | `capsim_cost {tenants:50000, prefixDelegation:true}` | $76,656 → $74,396 (−$2,261); unblocks the ENI pod-density ceiling |
| **Both** | `capsim_review {tenants:50000, maxPerCell:2000, prefixDelegation:true}` | **$42,836/mo, score 32→52**; the `cell_count` bottleneck is **resolved** (BFF becomes the new top) |

Pulling the levers is a **−44% cost cut** ($76,656→$42,836) that also lifts the review from CRITICAL
to AT_RISK — with zero multi-region complexity. Critically, the levers **compound with the posture
choice**: with levers applied, active-active's cost of resilience drops from +$121,925 to **+$54,577**
(`capsim_compare {tenants:50000, maxPerCell:2000, prefixDelegation:true, compareRegions:3}`), because
there is far less compute to replicate. **Deciding multi-region before fixing `maxPerCell` would
price failover at 2.23× its real cost.**

## Decision outcome

**Chosen: levers-first, then sharded multi-region, defer active-active.** In order:

1. **Phase 1 — capacity (do now).** Raise `maxPerCell` toward ~2000 and enable prefix delegation.
   Evidence: $76,656 → $42,836/mo (−44%), review 32→52, `cell_count` bottleneck resolved. Lowest
   risk, highest ROI; no region work required.
2. **Phase 2 — sharded multi-region (recommended).** Spread sticky cells across ≥2 regions (no
   replication). Evidence: **+$5,778/mo** cuts region blast radius **100%→33%** and lifts region-outage
   SLA **95%→98.32%**. Scale-insensitive cheap insurance; preserves the current sticky/DNS-cravado model.
3. **Phase 3 — active-active (deferred).** Only when an enterprise SLA contract (≥99.9%) justifies it.
   Post-levers cost of resilience is **+$54,577/mo** for 0% blast radius / 99.98% SLA. Revisit trigger
   below.
4. **Ongoing — request tier (quantified, M7).** The review's next bottleneck after cells is `bff`
   (ρ=1.04) then `engine_api` (ρ=0.89, AZ-fragile at ρ=1.34). Now MCP-parametrized (`bffReplicas`/
   `engineReplicas`): **BFF 6→7 replicas** resolves saturation (ρ 1.04→0.89, warm), **6→10** fully
   clears it incl. AZ-fragility (an AZ loss saturates BFF at 8-9 replicas); **Engine 8→11 replicas**
   clears both the base and the AZ-fragility (AZ ρ 1.34→0.97). Spread across ≥2 AZs. These are review
   fixes, not cost levers (small platform pods).

## Consequences

- **Good:** −44% cost before any resilience spend; region resilience for +$5,778/mo (≈15% of the
  post-lever single-region bill of $42,836 — small in absolute terms); a clear, numeric revisit path
  to active-active; the sticky cell model is preserved (Phase 2 needs no cross-region replication).
- **Bad / cost:** sharded still loses 1/N of tenants on a full-region outage (33% at 3 regions) — not
  zero. Raising `maxPerCell` concentrates more tenants per cell (larger per-cell blast radius within a
  region); the review's per-cell isolation trade-off should be tracked. Active-active remains on the
  table for enterprise SLA deals but is not funded now.

## Risks & estimate-sensitivity (honesty)

- The **absolute** cost figures include estimate-labeled inputs: the ~$14k `global` bucket — of which
  CloudFront/NAT **traffic** ≈ $6k (the estimate-exposed part), the rest ECR/WAF/S3/RDS-storage/KMS
  fixed items — plus muRps/holdSec/regionalOutageAvail. **The decision does not hinge on them** — it
  hinges on the **deltas** (cell-overhead from `maxPerCell`, control-plane ×regions, compute ×regions),
  which are structural. Re-pricing traffic shifts every option equally and does not change the ranking
  (the `global` bucket is added identically to all three postures, so it cancels in the cost-of-resilience Δ).
- BFF/Engine-API replica counts are now MCP-parametrized (M7: `bffReplicas`/`engineReplicas`) — the
  Phase-4 fix is quantified in replica counts (above). It changes the review (ρ), not the cost model
  (small platform pods, not separately priced), so it is a soundness fix rather than a cost lever.

## Revisit triggers

- **Scale:** re-run `capsim_compare` when the target crosses ~100k tenants — single-region cost hits
  $126,403 and the request tier (BFF/Engine) needs re-sizing.
- **SLA contract:** if an enterprise deal requires ≥99.9% region-outage SLA, active-active becomes
  justified — at post-lever +$54,577/mo it is 2.23× cheaper than pricing it before the levers.
- **Model calibration:** after a real load test calibrates muRps/holdSec, re-confirm the request-tier
  bottleneck ordering.

## Reproduction (every number above is one of these LIVE calls)

Registered MCP server `loopzero-capsim` (`claude mcp add loopzero-capsim -- node docs/operations/loopzero-capsim-mcp/server.mjs`).

```
capsim_review  {tenants:50000}                                                   → 32 / CRITICAL
capsim_compare {tenants:50000, compareRegions:3}                                 → single/sharded/active-active table
capsim_compare {tenants:100000, compareRegions:3}                                → scale sensitivity
capsim_cost    {tenants:50000, prefixDelegation:true}                            → $74,396
capsim_cost    {tenants:50000, maxPerCell:2000}                                  → $45,096
capsim_review  {tenants:50000, maxPerCell:2000, prefixDelegation:true}           → 52 / AT_RISK
capsim_compare {tenants:50000, maxPerCell:2000, prefixDelegation:true, compareRegions:3} → post-lever table
```

Equivalent offline: `cd docs/operations && node -e "import('./loopzero-capsim.mjs').then(m=>console.log(m.comparePostures(m.scn({tenants:50000}),{regions:3})))"`.

## Confirmation

- Numbers verified against the LIVE tool by an independent reviewer (re-runs ≥3 calls, diffs).
- Simulator suite green (M7 snapshot): engine 48/48 + MCP 9/9 (`node --test`). (The region-posture
  decision numbers were locked at the M6 45/45 + 8/8 snapshot; M7 added the request-tier tests.)

## Cross-references
- Simulator + engine: `../operations/loopzero-bottleneck-simulator.html`, `../operations/loopzero-capsim.mjs` (M0-M5).
- Cost model: `../operations/aws-cost-calculator.html` (engine-backed, M5) — same $ as this memo.
- AWS re-platform: `decisions/2026-06-17-infra-state-decomposition-aws.md`.
- Diagrams index: [`eraser-diagrams-index.md`](./eraser-diagrams-index.md) (Views 13/14/15).
