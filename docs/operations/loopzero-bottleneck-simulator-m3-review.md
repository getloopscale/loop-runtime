# Review — LoopZero Bottleneck Simulator M3 (multi-region + failover)

> Cycle verdict record. Slug: `loopzero-bottleneck-simulator-m3`.
> **VERDICT: READY_TO_MERGE** (after remediation). Independent review first returned **NEEDS_FIXES**
> (1 HIGH + 2 MEDIUM + 4 LOW); H-1, M-1, M-2, L-1, L-2, L-4 closed with evidence; re-confirmed.

## DoD → evidence (all satisfied)

| DoD | Evidence | Status |
|---|---|---|
| No regression | 29 M0/M1/M2 tests green inside **engine 36/36**; **MCP 6/6**; default regions=1 → killRegion capLoss=1, sla=95, review 32/CRITICAL | ✅ |
| Sharded (regions=3) | `capLossPct=1/3`; survivors' ρ == base ρ; sla blended 98.32% (∈ (95, 99.985)) — asserted | ✅ |
| Active-active (regions=3) | `capLossPct=0`; survivors' ρ ×1.5; sla 99.98% > sharded — asserted | ✅ |
| Review posture + resilience gain | single −10 / sharded −5 / active-active posture-aware; with headroom **100 > 95 > 90**; **invariant aa ≥ single** even at 2-fragile load (34→36) — asserted | ✅ |
| Grounding | region finding numbers come from the engine (asserted) | ✅ |
| MCP accepts new inputs | schema advertises `regions` + `crossRegionReplication`; passthrough **proven** via capsim_chaos capLossPct (sharded 1/3 vs aa 0) | ✅ |
| CODE-QUALITY | engine + server syntax OK; 0 dead exports; no fabrication; HTML wired + serves 200 | ✅ |

## Findings raised by independent review — disposition

| Sev | Finding | Disposition |
|---|---|---|
| **H-1** | Uncapped region-fragile penalty could make active-active (0% blast) score BELOW single-region (100% blast) — resilience inversion | **FIXED** — `score -= Math.min(8, regionFragile)` caps below single's flat −10; invariant test added (was 24<34, now 36≥34) |
| M-1 | MCP test proved `regions` but not `crossRegionReplication` passthrough | **FIXED** — test now asserts capsim_chaos `capLossPct` (sharded 1/3 vs aa 0) — impossible if the flag were stripped |
| M-2 | Blended sharded SLA mislabeled "SLA" | **FIXED** — commented as "expected per-tenant availability during the outage"; only the ordering property is claimed |
| L-1 | DoD test counts stale (29+4) | **FIXED** — plan updated (37 engine + 7 MCP target range) |
| L-2 | `regions` schema unvalidated | **FIXED** — `z.number().int().min(1)` |
| L-4 | schema advertisement not test-asserted | **FIXED** — test asserts `regions` + `crossRegionReplication` in inputSchema.properties |
| L-3 | active-active region-kill SLA marginally < base | ACCEPTED (noted) — the real benefit `capLossPct=0` is surfaced separately; not misleading in full context |

## Model (three region postures)

| Posture | region-kill | capLoss | SLA | review |
|---|---|---|---|---|
| single (regions=1) | total outage | 100% | 95% (DR floor) | −10 |
| sharded (≥2, no repl) | lose 1/N tenants, survivors fine | 1/N (33% @ 3) | blended 98.3% | −5 |
| active-active (≥2, repl) | 0 loss, survivors absorb (ρ ×N/(N−1)) | 0% | 99.98% | 0, or capped region-fragile |

Active-active honestly labeled as an OPTION that contradicts today's sticky/DNS-cravado model (a
larger architectural change), not presented as free.

## Deliverables (M3)

- `loopzero-capsim.mjs` — 3-branch `chaos()` region model + posture-aware `review()`.
- `loopzero-capsim.test.mjs` — **36 tests** (+7 M3 incl. the invariant guard).
- `loopzero-capsim-mcp/{server.mjs, server.test.mjs}` — schema + **6 integration tests** (passthrough proven).
- `loopzero-bottleneck-simulator.html` — regions slider + replication toggle.
- `loopzero-bottleneck-simulator-m3-{plan,review}.md`.
