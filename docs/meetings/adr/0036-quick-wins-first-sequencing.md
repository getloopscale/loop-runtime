# ADR-0036 — Quick-wins-first sequencing strategy

## Status

Accepted (async, pending human ratification) — 2026-05-05 (MEET-2026-05-05-CHARTS-SOTA-ASYNC-AMENDMENT)

## Context

Charts SOTA implementation plan has 22 identified items distributed in 8 phases (~34 tasks). Without explicit sequencing, work could be parallel-everything (high context load, no checkpoint) or strict-linear (slow, no early evidence).

5 quick-wins (T1.1-T1.5) share characteristics: 1-2 sessões each, reversible, no cross-deps, fecham 3 NEW gates (CH-G15, CH-G16, CH-G17 partial).

## Decision

Phase 1 (5 quick-wins) executes BEFORE any other work in the plan. Each quick-win is paralelizável dentro de Phase 1 (no internal blockers).

Specifically:
- T1.1: Delete ghost charts (per ADR-0042 is T1, no governance gate)
- T1.2: Port 5 admission-policies templates (T2)
- T1.3: Build drift detector script + CI step (T2)
- T1.4: Refactor crd-sync for semantic diff (T1)
- T1.5: Add helm-unittest to 4 charts (T1)

Phase 2+ blocked until Phase 1 GREEN with merged PR(s).

## Alternatives Considered

1. **All-fronts parallel work.** REJECTED — lacks measurable progress checkpoint; high cognitive load; ghost charts blocking nothing but confusing audit; some quick-wins (T1.5) feed into Phase 3 (T3.5 auto-discovery) — order matters.
2. **Defer all to Sprint 9.** REJECTED — quick-wins têm ROI altíssimo (1-2 sessões); postponing accumulates technical debt sem benefício; CH-G15 ghost charts é defeito imediato visível em audit.
3. **Strict linear within Phase 1.** REJECTED — T1.1-T1.5 não têm cross-deps, paralelismo é safe.

## Consequences

- **+** End of Phase 1: score saiu de 7.2 para ~7.8 em 1 sprint (3 NEW gates fechados).
- **+** Habilita medição de progresso semanal por gate count.
- **+** No technical debt acumulado se Phase 2+ atrasa.
- **−** Phase 1 may use disproportionate planning meeting time relative to its size.

## Related

- Plan: `docs/plans/charts-sota-implementation-plan.md` v1.0
- Source research: `docs/technical/charts/improvement-roadmap.md`
