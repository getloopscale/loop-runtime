# Runbook — multi-cell canary

**Owner:** SRE · **Status:** active · **Created:** 2026-09-07

## Why this file exists

`MEET-INFRA-NORMALIZE-T3-multi-cell.md:138` lifted a partial veto **conditional on the SLI/SLO
table below being committed verbatim into this runbook**, and `:357` (D17) assigned it to SRE. The
file was never written. A 2026-09-07 infrastructure system-design audit found the condition
outstanding: the halt criteria existed only inside a meeting record, and an operator during a burn
goes to a runbook, not to an ata.

The table below is copied verbatim from that meeting. Do not paraphrase it here — if it needs to
change, change it in a new meeting record and copy the new one.

## Halt criteria per stage (verbatim, MEET-INFRA-NORMALIZE-T3-multi-cell)

| Stage | SLI | SLO target | Window | Halt criterion |
|-------|-----|-----------|--------|----------------|
| Stage 1 (dev, 1h) | `loopzero_tenant_availability{cell="cell-2"}` (1 - 5xx_rate over 5m windows, synthetic tenant probe) | ≥ 99.0% | 1h rolling | Any 5m window < 95% **OR** `loopzero_cell_routing_decisions_total{cell="cell-2",decision="error"}` > 5 over the hour |
| Stage 2 (dev, 24h) | cell-1 EB consumption | < 0.1% of 28-day budget | 24h soak | (a) Cell-1 5xx_rate increases > 0.05 pp vs pre-canary 24h baseline, OR (b) cell-2 availability < 99.0% sustained 15m, OR (c) `MultiCellRouterErrorRate` fires (rate(error)>0.01 over 5m), OR (d) cell-2 P99 latency > 2× cell-1 P99 sustained 30m |
| Stage 4 (prod, 7d) | tenant-T availability | ≥ 99.5% (matches plan) | 7d | Any 1h window < 99.0%, OR EB consumption > 0.5% of 28-day budget for tenant T, OR customer reports incident, OR cell-2 cluster-wide availability < 99.5% |
| Stage 5a (prod 10%, 7d) | cell-2 fleet availability | ≥ 99.5% | 7d | Cell-2 EB consumption > 1% (per ADR D3 line 124), OR cell-1 EB consumption > 0.1% (cross-contamination), OR P99 latency divergence cell-2 vs cell-1 > 50% sustained 1h |
| Stage 5b (prod 50%) | same as 5a | 99.5% | 7d | same; multi-burn-rate alert (1h burn × 14.4 + 5m burn × 14.4 — Google SRE Workbook recipe) |
| Stage 5c (prod 100%) | overall fleet availability | ≥ 99.5% | 7d soak post-cutover | same |

The governing decision is ADR D3 in `docs/wiki/decisions/2026-04-30-infra-normalize-decisions.md:87`:
**halt on cell-1 error-budget consumption above 1%.**

## When a halt criterion is met

Per **C15** of the same meeting, a halt is machine-actionable and not a judgement call:

1. **The alert pages.** Acknowledge in PagerDuty within 15 minutes.
2. **Stop the rollout.** Block promotion with the canary `--paused` flag before diagnosing —
   diagnosis happens with traffic held, not while it grows.
3. **Roll back** per the stage's rollback path.
4. **Write the RCA** (C17). Any halt produces one.

## Which alerts to watch

**Read this section before trusting an alert name.** The 2026-09-07 audit found
`docs/operations/production-canary-stage4-runbook.md:50` telling operators to watch
`loopzero:slo:deploy_burn_rate_5m` — a recording rule that **exists nowhere in the repository**. An
operator waiting on it reads the silence as budget intact.

The real alerts come from `infra/sloth/` (7 SLO specs) through `task sloth:generate`, and Sloth
names them after the spec, not after the runbook prose:

| SLO spec | Alert emitted |
|---|---|
| `api-availability.yaml` | `LoopZeroAPIAvailabilityBudgetBurn` |
| `build-pipeline.yaml` | `LoopZeroBuildPipelineBudgetBurn` |
| `deploy-latency.yaml` | `LoopZeroDeployLatencyBudgetBurn` |
| `deploy-success.yaml` | `LoopZeroDeploySuccessBudgetBurn` |
| `domain-provisioning.yaml` | `LoopZeroDomainProvisioningBudgetBurn` |
| `rollback-success.yaml` | `LoopZeroRollbackSuccessBudgetBurn` |
| `tenant-availability.yaml` | `LoopZeroTenantAvailabilityBudgetBurn` |

Each is emitted **twice** — a fast page and a slow ticket, which is the multi-burn-rate pairing the
Stage 5b row above calls for.

**Before relying on any of them, confirm they are actually loaded:**

```bash
kubectl --context k3d-loopzero-core -n monitoring get prometheusrule loopzero-slo-burn-rate
```

If that returns NotFound, the rules were never generated or never applied, and every burn-rate
criterion in the table above is unmonitored. Generate and re-run the monitoring install:

```bash
task sloth:generate            # needs the sloth CLI; the task states how to install it
task infra:install:monitoring  # applies every prometheusrule-*.yaml
```

## Related

- Governing ADR: `docs/wiki/decisions/2026-04-30-infra-normalize-decisions.md` (D3)
- Source meeting: `docs/wiki/meetings/MEET-INFRA-NORMALIZE-T3-multi-cell.md` (C15, C17, D17)
- Per-stage production procedures: `docs/operations/production-canary-stage4-runbook.md`,
  `docs/operations/production-canary-stage5-runbook.md`
- SLO specs: `infra/sloth/`
