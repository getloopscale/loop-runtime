# Cell-2 Canary Customer Communication Template

> **ADR-0021 D9 reference.** Template for notifying customers participating in
> the cell-2 production canary (Stage 4, single customer, 7-day soak).

## Pre-canary email (T-7 days)

**Subject:** LoopZero platform: scheduled multi-cell migration — your tenant participates as canary

```
Hi {{customer_name}},

Your LoopZero tenant {{tenant_id}} has been selected for our cell-2 production
canary, scheduled to begin {{start_date}}. This is a foundational reliability
upgrade — your workloads will be migrated to a second isolated K8s cluster
(cell-2) for 7 days as part of our staged rollout.

What changes for you:
- Zero. Your endpoints remain at the same domain. No DNS change required.
- Build pipeline, deploy CLI, and dashboard are unaffected.

What we're testing:
- End-to-end deploy on cell-2.
- Rollback to cell-1 (P50 ≤ 300s, P95 ≤ 480s).
- Observability parity (metrics, logs, traces).

Risk mitigations:
- Operator-driven rollback per `loopzero admin tenants pin --to cell-default`.
- 24/7 monitoring with auto-page on EB > 0.5% or 5xx > 0.1%.
- HALT criteria: any production incident traced to cell-2 = rollback all.

Questions: reply to this email or post in #loopzero-customers.
We'll send a daily status update during the 7-day window.

— LoopZero platform team
```

## Daily status email (during canary)

**Subject:** [LoopZero canary day {{n}}/7] {{tenant_id}} on cell-2 — STATUS

```
Day {{n}}/7. Tenant: {{tenant_id}}. Cell: cell-2.

Health:
- Error budget remaining: {{eb_remaining}}%
- 5xx rate: {{5xx_rate}}%
- P95 latency: {{p95_ms}}ms (target ≤ {{p95_target}}ms)
- Rollbacks: {{rollback_count}}

Incidents in last 24h: {{incidents_summary}}

Continue? {{continue_decision}}
```

## Rollback notification (if HALT)

**Subject:** [URGENT] LoopZero cell-2 canary HALTED — {{tenant_id}} rolled back to cell-default

```
Hi {{customer_name}},

We've HALTED the cell-2 canary and rolled your tenant {{tenant_id}} back to
cell-default. Reason: {{halt_reason}}.

Status: your workload is now serving from cell-default. No customer-facing
impact expected. We'll send the RCA in {{rca_eta}}.

— LoopZero platform team
```

## References

- ADR-0021 D9: Phase 7 production rollout runbook + customer comms shipped
- `docs/runbooks/multi-cell-canary-prod.md` — operator-side production canary runbook
- `docs/runbooks/multi-cell-canary.md` — dev-side runbook (Stages 1-3)
