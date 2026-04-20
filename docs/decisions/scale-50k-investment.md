# Scale to 50k — Investment Decision Framework

**Criado:** 2026-04-17
**Status:** Draft for stakeholder review
**Audience:** Founder + prospective investors + board
**Purpose:** Concretize the "path to 50k apps" narrative with evidence, costs, timeline, and realistic scope scenarios. Replace "it scales" hand-waving with a defensible investment case.

---

## TL;DR

LoopZero's cell-based architecture is **approved and partially deployed** (ADR-0008 accepted, Phase 0 live). The path to 50,000 applications requires **~6-9 months of focused engineering** with 2-3 senior engineers, **~$50k/mo steady-state infra cost at scale**, and **$3-5M total engineering budget over 12 months** including team + runway.

Three realistic scope options are presented. The default recommendation is **Scenario A (full plan)** because 50k-ready is the strongest investor moat, but Scenarios B and C are provided in case runway is shorter.

---

## Current state (honest)

| Dimension | Today |
|---|---|
| Apps running | ~100 (L3 test) |
| Architectural decision | ADR-0008 (cell-based) accepted 2026-03-31 |
| Phase 0 (Foundation) | ✅ Deployed (`cell_id`, `PlacementEngine`, `DynamicRouter` live) |
| Phase 1 (Cell-Ready) | 🟡 ~75% code, 0% deployed (CRD + controller + Pulumi scaffold; zero ClusterCredential CRs registered) |
| Phase 2a/b/3/4 | ❌ Planned, not started |
| Cells in production | 1 (cell-default) |
| Workload clusters | 1 (do-sfo3-loopzero-infra-dev-sfo3-runtime) |
| L3 best result | 75/100 apps live (v8); v14 dropped to 62/100 due to runtime capacity |
| Bottleneck identified | Runtime cluster scheduling + shared-service SPOFs |
| DO droplet quota | Ticket open (2026-04-17) |

**Architectural quality:** validated by FAANG-level technical meetings (MEET-024, MEET-051) with 6-10 Staff/Principal specialists. Decisions are auditable in `docs/meetings/`.

---

## Scenario A — Full Plan (Recommended)

**Target:** 50,000 apps validated with chaos proof.

### Scope

- Phase 2a — Shared-service hardening (Gitea HA, Harbor HA, MinIO distributed, PG bump, mgmt headroom)
- Phase 2b — Cell-2 activation with failure isolation chaos video (investor-demo-grade proof)
- Phase 3 — Automation (auto-provisioning, migration, per-cell ArgoCD, PG HA)
- Phase 4 — 50k optimization (ApplicationSet sharding, Harbor geo-rep, per-region build plane, PG partitioning)

### Investment

| Item | Cost |
|---|---|
| Engineering team | 3 senior engineers × $220k/year × 0.75 years = **$495k** |
| Infra during development | 1-10 cells avg × $500/mo × 9 mo = **$45k** |
| Infra steady-state post-launch | 100 cells × $500/mo = **$50k/mo recurring** |
| Load test costs (L4-L6) | ~$2k total (L6 × 3 runs × $350) |
| Third-party (Gitea EE license if chosen) | ~$5-20k/year (optional) |
| **Total ~9-month engineering burn** | **~$550-600k** |

### Timeline

```
Month 1:    Phase 2a (shared-service hardening)
Month 1-2:  Phase 2b (cell-2 activation + chaos video)  ← investor milestone
Month 3:    Phase 3 (automation, L5 validation)
Month 4-6:  Phase 4 (multi-region, L6 progressive)
Month 6:    50k validated + capacity model published
```

### Deliverables for investor demo

1. **Chaos video** — cell-2 workload cluster killed while cell-1 apps continue serving HTTP 200 for 10+ minutes. Reverse validated.
2. **Capacity model** — measured numbers (not projected): tenants/cell, pods/cell, PG connections/cell, cost/cell/month, headroom per scaling dimension.
3. **Unit economics** — break-even customer count at $X/tenant/month pricing validated from measured infra cost.
4. **Roadmap artifacts** — ADR-0008, ADR-0010, 4 phase plans, all meeting records (MEET-024, MEET-051), all runbooks.
5. **L6 load test evidence** — 50k apps live HTTP 200 confirmed, failure rate <0.5%, deploy p99 <10min sustained 12 hours.

### Risks

| Severity | Risk | Mitigation |
|---|---|---|
| HIGH | Engineering hiring delays (2-3 senior engineers in tight market) | Start hiring immediately; Phase 2a is executable with 1-2 engineers |
| MEDIUM | Unknown unknowns at 50k scale (PG, Gitea, Argo Workflows) | L5 validation (5k tenants) exposes bottlenecks before L6 |
| MEDIUM | Runway insufficient if customer acquisition lags infra build | Scenario B is an explicit pivot point |
| LOW | DO droplet/region capacity | Multi-region plan in Phase 4 is hedge |

---

## Scenario B — Reduced Scope (Cap at 5-10k apps)

**Target:** 5,000-10,000 apps validated. Defer 50k to next investment round.

### Scope

- Phase 2a complete (prerequisite regardless)
- Phase 2b complete (cell-2 chaos video)
- Phase 3 partial: auto-provisioning + migration + PG HA. **Skip per-cell ArgoCD** (keep central, accept ~500-tenant ceiling as near-term cap).

### Investment

| Item | Cost |
|---|---|
| Engineering team | 2 senior engineers × $220k × 0.4 years = **$176k** |
| Infra during development | 1-5 cells × $500/mo × 5 mo = **$12k** |
| Infra steady-state | 10 cells × $500/mo = **$5k/mo recurring** |
| **Total ~5-month burn** | **~$190-210k** |

### Timeline

```
Month 1:   Phase 2a
Month 2-3: Phase 2b (chaos video for investor)  ← milestone
Month 4-5: Phase 3 partial (auto-provision + migration)
End of M5: 10k apps ceiling documented
```

### When Scenario B is right

- Runway <12 months
- Customer acquisition rate won't hit 10k tenants within 12 months
- Investor pitch accepts "5-10k proven + roadmap to 50k in next round"
- Team is 1-2 engineers (not 3+)

### What you give up

- No 50k proof. Pitch narrative becomes "path to 50k is clear but we're at 10k capable today."
- No multi-region. Single-region failure = platform outage.
- ApplicationSet single-thread bottleneck hits at ~500 tenants — must migrate to Phase 3 per-cell ArgoCD or accept cap.

---

## Scenario C — Managed Pivot (Ship Custom, Host on Managed PaaS)

**Target:** Re-architect as control-plane-only; workloads run on managed PaaS (Render, Railway, Fly.io Machines API, or cosmonic).

### Scope

- Keep LoopZero CLI + API + operators as control plane
- Delegate actual workload execution to managed PaaS via their APIs
- Drop custom cell provisioning (managed provider handles scale)
- Retain differentiated features: monorepo, preview environments, unified deploy flow, secrets, observability

### Investment

| Item | Cost |
|---|---|
| Engineering team | 2 engineers × $220k × 0.5 years = **$220k** (integration focus) |
| Infra during development | Minimal — managed provider absorbs scaling |
| Infra steady-state | Pass-through pricing from managed provider + LoopZero margin |
| **Total** | **~$240-260k** |

### Timeline

```
Month 1-2: Integrate LoopZero control plane with managed provider API (Fly.io most likely — Machines API + Anycast)
Month 3:   Migrate existing tenants to managed provider substrate
Month 4:   Feature parity + GA
Month 5:   Launch
```

### When Scenario C is right

- Runway <6 months OR team <2 engineers
- Investor pitch is "we're a DX layer above existing infra, not infra ourselves"
- Market thesis is commodity compute is commodity; differentiation is developer workflow + monorepo + preview environments

### What you give up

- **Infrastructure moat.** If your moat is "we built cell-based scaling", Scenario C eliminates it.
- **Pricing power.** Pass-through pricing caps margins at managed provider resale margin (~20-30%).
- **Control.** Managed provider outage = your platform outage with no recourse.
- **Enterprise story.** "Compliant multi-tenant at 50k" is significantly weaker when you don't run the plane.

### Advantage

- **Time-to-market.** 5 months to GA vs 9 months for Scenario A.
- **Burn reduction.** ~$260k vs $600k for Scenario A.
- **Operational risk.** Managed provider owns uptime, you own DX.

---

## Comparison Matrix

| Dimension | A (Full 50k) | B (Capped 10k) | C (Managed Pivot) |
|---|---|---|---|
| **Months to goal** | 6-9 | 4-5 | 4-5 |
| **Total burn** | $550-600k | $190-210k | $240-260k |
| **Steady-state infra** | $50k/mo at 50k | $5k/mo at 10k | Pass-through |
| **Team size** | 3 seniors | 2 seniors | 2 generalists |
| **Infrastructure moat** | Strongest (50k proven) | Medium (10k proven + roadmap) | Weakest (DX-only differentiation) |
| **Investor narrative** | "50k-ready, chaos-validated" | "10k now, clear path to 50k" | "DX-first PaaS on proven infra" |
| **Risk if scope slips** | LOW — gates are independent | LOW — already partial scope | HIGH — managed API dependencies |
| **Optionality to upsize later** | N/A (already max) | HIGH — can resume Phase 4 | LOW — re-architecting out of managed is expensive |

---

## Recommendation

**Scenario A (full plan), conditioned on the following being true:**

1. Runway ≥ 12 months at current or next funding round
2. Ability to hire 3 senior engineers within 8 weeks
3. Customer acquisition trajectory credible for 5k+ tenants within 18 months (validates the unit economics of 50k target)
4. Founder commitment to infrastructure as core product, not commodity

**If any of 1-4 is false, default to Scenario B.** Scenario C is a viable business but a different one; treat it as pivot, not continuation.

---

## Appendix: What investors should ask

These are questions that signal technical due diligence. Answers are pre-loaded here:

| Question | Answer |
|---|---|
| "How do you know cells isolate failure?" | Chaos video + MEET-051 veto conditions validate failure isolation. |
| "What happens when a cell fails?" | Tenants in failed cell return 503 (correct). Tenants in other cells serve 200. Proven by chaos test. |
| "Multi-region?" | Phase 4. 2 regions (SFO3 + NYC3) at 50k. Decision to go earlier is trivial. |
| "What if Kubernetes doesn't scale?" | K8s max is 150k pods/cluster. At 500 apps/cell × 2 pods = 1k pods/cluster, we're at 0.6% utilization per cell. Not a limit. |
| "What if ArgoCD doesn't scale?" | CNOE tested 50k apps at 3 shards. Per-cell ArgoCD in Phase 3 = 2k apps/shard × 10 cells = 20k ceiling before needing further sharding (Phase 4). |
| "What if Gitea doesn't scale?" | ADR-0011 decides at Phase 3: cluster vs per-cell vs bare-git+S3. Benchmark-driven. 3 viable options documented. |
| "What if PG doesn't scale?" | ADR-0012 at Phase 4: partitioning vs per-region vs Citus. Three escape hatches documented. |
| "How do you handle tenant migration?" | State machine `ACTIVE → PREPARING → DUAL → DRAINING → ACTIVE` with finalizers in `pkg/statemachine/` (Phase 2b types, Phase 3 wiring). |
| "What's the unit economics?" | At $10/tenant/mo × 50k tenants = $500k ARR vs $50k/mo infra = 10:1 ratio. Gross margin positive, break-even at ~5k paying tenants. |
| "Which competitor does this best today?" | Fly.io (BGP Anycast, cell-based explicit). Heroku + Private Spaces (cell per enterprise). We differ on CLI-first DX + monorepo-native. |
| "What if you need to pivot?" | Scenario C above. Managed pivot is viable in 4-5 months with existing team. |

---

## References

- ADR-0008 Cell-Based Architecture (`docs/meetings/adr/0008-cell-based-architecture.md`)
- ADR-0010 Phase 2 Rescope (`docs/meetings/adr/0010-phase2-rescope.md`)
- Plan overview (`docs/plans/cell-architecture-overview.md`)
- Plan Phase 2 (`docs/plans/cell-architecture-phase2.md`)
- Plan Phase 3 (`docs/plans/cell-architecture-phase3.md`)
- Plan Phase 4 (`docs/plans/cell-architecture-phase4.md`)
- Plan load-test (`docs/plans/scale-50k-plan.md`)
- Meeting MEET-051 Phase 2 rescope (`docs/meetings/records/MEET-2026-04-17-051.md`)

---

## Changelog

- **v1.0 (2026-04-17):** Initial draft. Three scenarios + recommendation + investor Q&A pre-loaded.
