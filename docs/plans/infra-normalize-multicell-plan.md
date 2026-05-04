# Plan: Infra Normalize — Multi-cell Activation

> **Plan reference for ADR-0021 verification matrix D10.** This plan tracks
> the multi-cell normalization effort (Phase 7 production rollout). Closure
> evidence: `docs/runbooks/multi-cell-canary.md` + `docs/runbooks/multi-cell-canary-prod.md`.

## Status

Iter 1+2 closure: cells SQL table, multi-cell observability, admin pin, CC bootstrap, integration test harness shipped (CHG-2026-04-30-INFRA-NORMALIZE).

## Coverage matrix

| Decision | Status | Evidence |
|---|---|---|
| D1 4 split charts | ✅ | `charts/loopzero-{tenant,app,preview,platform}-operator/` |
| D2 single Pulumi project | ✅ | `infra/scripts/pulumi-{archive,orphan-rm}.sh`, `docs/audits/pulumi-state-inventory.md` |
| D3 multi-cell stack | ✅ | migration 000006, prometheusrule-multi-cell.yaml, admin_pin_cell |
| D4 integration suites | ✅ | spawn-helper.ts + mock-api-server.ts + 11 suites |
| D5 governance MEET DRs | ✅ | `MEET-INFRA-NORMALIZE-T3-{operator-split,pulumi-consolidation,multi-cell}.md` |
| D6 observability runbook | ✅ | `multi-cell-alerts.md` + prometheusrule |
| D7 pre-release governance | ✅ | `.claude/rules/pre-release-governance.md` |
| D8 — | reserved | — |
| D9 Phase 7 prod runbook | ✅ | `multi-cell-canary-prod.md` + `cell-2-canary-template.md` |
| **DEBT (D10): owner=infra-engineer, horizon=2026-Q3** | tracked | this row |

## DEBT (D10): owner=infra-engineer, horizon=2026-Q3

Outstanding multi-cell hardening tasks tracked but deferred:

1. **Cell-3 capacity reservation** — extend Pulumi to support a third runtime cluster slot (capacity scaling for plan tier "Team" growth).
2. **Cross-cell DR replication** — replicate critical state (PG WAL) across cells for regional failover. Currently single-cell replication only.
3. **Tenant draining UX** — `loopzero admin tenants drain --cell <id>` to evacuate ahead of cell retirement.

Owner: infra-engineer. Horizon: 2026-Q3 review with platform-architect.

## References

- ADR-0021 (`docs/meetings/adr/ADR-0021-infra-normalize-decisions.md`)
- Plan iter 1+2 closure: CHANGELOG.md `[Unreleased]/Added` (CHG-2026-04-30-INFRA-NORMALIZE)
- `docs/runbooks/multi-cell-canary.md` (Stages 1-3 dev)
- `docs/runbooks/multi-cell-canary-prod.md` (Stages 4-5 prod)
