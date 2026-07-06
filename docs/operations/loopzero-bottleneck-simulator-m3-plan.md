# Plan — LoopZero Bottleneck Simulator M3 (multi-region + failover)

> Slug: `loopzero-bottleneck-simulator-m3`. Builds on M0/M1/M2 engine. FAANG-level, no fabrication.
> Model single vs multi-region so the simulator + `capsim_review` quantify the #1 standing finding
> ("single-region SLA 95%") — what does going multi-region buy, and at what cost/blast-radius?

## Goal

Answer the AWS re-platform's next big question with numbers: given LoopZero's **sticky, sharded** cell
model, model (a) single-region (today), (b) multi-region **sharded** (cells spread across regions,
tenants sticky to one region — region loss drops 1/N of tenants), and (c) **active-active**
(cross-region replication — full failover, but ~N× control-plane footprint). `capsim_review` then
stops flatly penalizing "single-region" and instead scores the chosen posture.

## Baseline Context (grounded — DISCOVER)

| Fact | Value | Source |
|---|---|---|
| Cell placement | sticky, once, never auto-rebalanced | `loopzero-design.md:53`, `engine.go:179` |
| No cross-region replication today | tenant lives in one cell/region; DNS cravado no cluster origem | `CLAUDE.md:321,547`, multi-cell rules |
| AWS state prefix | `env/region/unit` — region is a dimension | ADR `2026-06-17:27` |
| Current posture | single-region (multi-region is future) | ADR + memory "AWS target" |

## Model — three region postures

`regions` (int ≥1, default 1) + `crossRegionReplication` (bool, default false). A region kill
(`chaos killRegion`) behaves per posture:

| Posture | condition | region-kill effect | capLossPct | SLA |
|---|---|---|---|---|
| **single** | regions=1 | total outage until DR | 1.0 | `regionalOutageAvail` (DR floor) |
| **sharded** | regions≥2, no replication | lose the killed region's tenants; survivors run at normal ρ | `1/regions` | blended: `(1−1/regions)·full + (1/regions)·DR floor` |
| **active-active** | regions≥2, replication | 0 tenants lost; survivors absorb failover load (ρ ×= regions/(regions−1)) | 0 | region-redundant (no DR floor); may surface **region-fragile** components |

- Sharded ⇒ `regionSurvivorFactor=1` on component capacity (survivors independent), but blast radius
  = `1/regions` of tenants.
- Active-active ⇒ `regionSurvivorFactor=(regions−1)/regions` (survivors take the dead region's load
  → ρ rises), blast radius 0. Cost note: control-plane footprint ≈ ×regions.

## `review()` region posture (replaces the flat −10)

| Posture | finding | deduction |
|---|---|---|
| single | `single_region` (SLA falls to DR floor) | −10 |
| sharded | `region_sharded` (a region loss drops 1/N tenants — no failover) | −5 |
| active-active | none for the posture; but a **region-fragile** finding (−10) if a component crosses ρ≥1 when it absorbs failover load | 0 (or −10 per fragile tier) |

Consequence: for the same load, active-active scores strictly higher than single-region (the tool
shows the resilience gain), while surfacing the new active-active risk (survivors must have headroom).

## Tasks

- **T1 — engine multi-region (TDD).** `regions` + `crossRegionReplication` inputs; `chaos(killRegion)`
  3-branch model (single/sharded/active-active) with correct `capLossPct` + `sla` + optional
  region-fragile detection. RED test first.
- **T2 — `review()` region posture.** Replace flat single_region with the posture-aware findings.
- **T3 — MCP schema.** Add `regions` + `crossRegionReplication` to the tool input schemas.
- **T4 — UI.** `regions` slider + replication toggle; region-chaos reflects posture.
- **T5 — REVIEW.** Independent adversarial review.

## Acceptance Criteria / DoD (all evidenced 100%)

1. **No regression** — all prior engine + MCP tests still green (exit 0; M3 → **36 engine + 6 MCP**). Default `regions=1`
   reproduces M2 behavior exactly (killRegion capLoss=1, sla=DR floor, review single_region −10).
2. Sharded (regions=3, no replication): `chaos(killRegion).capLossPct === 1/3`; survivors' ρ ==
   base ρ (unaffected); sla strictly between DR floor and full — asserted.
3. Active-active (regions=3, replication): `chaos(killRegion).capLossPct === 0`; survivors' ρ rises
   by factor 3/2 vs base; sla > sharded sla — asserted.
4. `review()` posture: single → −10 single_region; sharded → −5 region_sharded; active-active →
   no posture penalty. **For a scenario WITH failover headroom**, active-active score > sharded
   score > single-region score (asserted resilience gain). Honest caveat: at high load with thin
   replicas active-active surfaces **region-fragile** findings (survivors can't absorb the failover)
   — also asserted, so the tool never oversells active-active.
5. Grounding preserved: every region finding's number comes from the engine (asserted).
6. MCP tools accept `regions`/`crossRegionReplication`; integration test still green; new inputs
   in the schema.
7. CODE-QUALITY: engine + server syntax OK; 0 dead exports; no fabrication. HTML wired + serves.

## Test Plan
- Unit (node:test): 3 postures × (capLossPct, sla ordering, ρ effect) + review posture monotonicity
  + region-fragile + no-regression (default regions=1) + grounding.
- Boundary: regions=1 with replication=true (replication irrelevant → single behavior); regions=2
  active-active survivor load factor = 2/1 = 2×.
- Integration: MCP schema exposes new inputs; a `capsim_review({regions:3,crossRegionReplication:true})`
  call scores higher than default.

## Risks / Drawbacks
- Active-active contradicts today's sticky/DNS-cravado model — it is modeled as an OPTION with a
  cost/complexity note, not a recommendation. Labeled.
- Blended sharded SLA is a simplification (per-tenant availability averaged) — labeled.
- Cost of extra regions is reported as a note (×regions footprint); full $ stays in the cost calculator.

## Unresolved Questions
- (none blocking) — RTO/RPO timing (how long the failover takes) is deferred; M3 models steady-state
  posture + blast radius, not recovery duration.
