---
type: operational-runbook
title: Production Canary Stage 4 Runbook (Traffic Split via Argo Rollouts)
---

# Production Canary Stage 4 Runbook (Traffic Split via Argo Rollouts)

> **Plan reference:** T0.3 of [`docs/plans/completed/2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md`](../plans/completed/2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md)
>
> **Status:** Stage 4 = traffic split. Stage 5 (customer trial) em [`production-canary-stage5-runbook.md`](./production-canary-stage5-runbook.md).
>
> **Infra existente:** Argo Rollouts shipped 2026-05-07 (CLAUDE.md) — TraefikService weighted routing + AnalysisTemplate hooks + auto-abort em `progressDeadlineSeconds=300`.

## Purpose

Promover novo workload de cell-1 (ou novo deploy production) através de 4 fases de canary com aprovação manual entre fases. Stage 4 do plano "Path to 4.0" — last gate antes do customer trial (Stage 5).

## Pre-conditions

- [ ] Argo Rollouts CRDs instaladas em runtime cluster (cell-1): `kubectl get crd rollouts.argoproj.io`
- [ ] Argo Rollouts controller Running em cell-1
- [ ] AnalysisTemplate `loopzero-success-rate` existe em namespace tenant
- [ ] TraefikService routing weights configurados (stable=100, canary=0 initially)
- [ ] Operator tem `LOOPZERO_SERVICE_AUTH` para approval API calls
- [ ] SLO baseline measured (deploy_latency p95, rollback_success_rate, http_5xx_rate)

Verificar AnalysisTemplate existe para target environment:
```bash
kubectl --context cell-1 get analysistemplate -n <tenant>-prod
# Expected: loopzero-success-rate, loopzero-latency-p95 (at minimum)
```

## Stage 4 Phases (Approval-Gated)

### Phase A — 1% canary (watch 1h)

**Promote canary to 1% traffic:**

```bash
kubectl --context cell-1 argo rollouts set image \
  <project>-<app> <project>-<app>=<new-image-digest> \
  -n <tenant>-prod

# Verify rollout entered Progressing state
kubectl --context cell-1 argo rollouts get rollout <project>-<app> -n <tenant>-prod
```

**Watch window: 1 hour.** Monitor:
- AnalysisTemplate metrics (success_rate ≥ 0.95)
- LoopZero SLO burn rate alert: `LoopZeroDeployLatencyBudgetBurn` (fast-burn page) — emitted by Sloth from
  `infra/sloth/deploy-latency.yaml`. CORRECTED 2026-09-07: this line named
  `loopzero:slo:deploy_burn_rate_5m`, a recording rule that exists nowhere in the repository, so an
  operator following it waited on a signal that could not fire. Confirm the rules are loaded with
  `kubectl -n monitoring get prometheusrule loopzero-slo-burn-rate` before trusting silence.
- Loki errors: `{namespace="<tenant>-prod"} | json | level="ERROR" | rate(5m) > 0.01`
- TraefikService routing weights: stable=99, canary=1

**Approval gate:**
```bash
# Manual operator review of metrics dashboard
# If GREEN → proceed to Phase B
# If RED  → auto-abort triggers OR manual abort:
kubectl --context cell-1 argo rollouts abort <project>-<app> -n <tenant>-prod
```

### Phase B — 10% canary (watch 4h)

```bash
kubectl --context cell-1 argo rollouts promote <project>-<app> -n <tenant>-prod
# Promotes to next step: weight=10
```

**Watch window: 4 hours.** Verify:
- TraefikService routing weights: stable=90, canary=10
- Customer-impacting errors stable (no spike vs baseline)
- AnalysisTemplate per-step succeeds within 5min hold

**Approval gate:** operator decision via dashboard.

### Phase C — 50% canary (watch 8h)

```bash
kubectl --context cell-1 argo rollouts promote <project>-<app> -n <tenant>-prod
# Promotes to next step: weight=50
```

**Watch window: 8 hours.** Heavier traffic split — most signal here.

**Approval gate:** required for Phase D promotion.

### Phase D — 100% (promote stable)

```bash
kubectl --context cell-1 argo rollouts promote <project>-<app> -n <tenant>-prod --full
# Promote to 100% AND mark canary as new stable
```

Effects:
- TraefikService: stable=100, canary=0
- ReplicaSet do canary becomes new stable
- Old stable ReplicaSet scaled down (kept N revisions for rollback)

## Rollback Procedure

### Auto-rollback (AnalysisTemplate failure)

Argo Rollouts auto-aborts when:
- AnalysisTemplate metric < threshold for 5min window
- `progressDeadlineSeconds=300` exceeded sem promotion
- Manual `kubectl argo rollouts abort`

Auto-abort actions:
1. TraefikService weights revert: stable=100, canary=0
2. Canary ReplicaSet scaled to 0
3. Rollout status → `Degraded` + Aborted
4. SSE event `rollout.aborted` emitted to CLI

### Manual rollback (operator-triggered)

```bash
# Abort current rollout
kubectl --context cell-1 argo rollouts abort <project>-<app> -n <tenant>-prod

# Rollback to previous revision
kubectl --context cell-1 argo rollouts undo <project>-<app> -n <tenant>-prod

# Or via CLI (preferred — audit_log entry)
loopzero rollback --project <project> --env production
```

## Rollback Criteria (HARD STOP triggers)

Operator MUST abort canary se qualquer signal triggers:

| Signal | Threshold | Source |
|---|---|---|
| AnalysisTemplate success_rate | < 0.95 over 5min | Prometheus query em template |
| LoopZero SLO burn rate | > 14.4 (1h budget) | `LoopZeroDeployLatencyBudgetBurn` (slow-burn ticket) — see `docs/runbooks/multi-cell-canary.md` for the full alert map |
| HTTP 5xx rate | > 1% sustained 5min | Loki `{namespace="<tenant>-prod"} | json | status >= 500` |
| Latency p95 | > 2× baseline | Prometheus `histogram_quantile(0.95, loopzero_api_request_duration_seconds)` |
| Customer complaint | ≥ 1 verified | Operator escalation |

## Monitoring Queries

Argo Rollouts dashboard:
```bash
kubectl --context cell-1 argo rollouts dashboard
# Browser: http://localhost:3100
```

Loki — rollout-scoped errors:
```
{namespace="<tenant>-prod",app="<project>-<app>"}
  | json
  | revision="canary"
  | level="ERROR"
  | rate(5m)
```

Prometheus — traffic split realtime:
```promql
sum by (rollout_state) (rate(traefik_service_requests_total{service=~"<project>-<app>-canary"}[1m]))
```

## Audit Trail

Every promote/abort writes audit_log:
- `action: canary_promote | canary_abort | canary_rollback`
- `actor: <operator>` (X-Service-Auth subject)
- `target: <tenant>/<project>/<env>`
- `metadata: {phase: A|B|C|D, from_weight, to_weight, rollout_uid}`

## Stage 5 Gate

After Phase D (100% promote) AND zero-incident window of 24h:
- Proceed to [`production-canary-stage5-runbook.md`](./production-canary-stage5-runbook.md) (customer trial)
- Update `docs/architecture/multi-cell/` if procedure refined

## Related

- ADR Argo Rollouts Progressive Delivery (CLAUDE.md 2026-05-07)
- `[[canary-progressive-rollout]]`
- `infra/helm/argo-rollouts/values.yaml`
- `pkg/statemachine/release.go` (CANARY state transitions)
