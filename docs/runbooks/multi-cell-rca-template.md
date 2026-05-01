# Multi-cell RCA Template (C17 Mandatory)

> CHG-2026-04-30-INFRA-NORMALIZE / MEET-INFRA-NORMALIZE-T3-multi-cell C17.
> **Required for any halt event during cell-2 canary stages 4 / 5a / 5b / 5c.**
> Populate within 24h of incident, 5 business days for full RCA.
>
> Owner: SRE on-call who triggered the halt. Reviewer: Staff SRE.
> Distribution: incident channel + customer-success + this RCA index.

---

## Header (fill on T+15min)

| Field | Value |
|---|---|
| RCA ID | `RCA-CELL2-{YYYY-MM-DD}-{nn}` |
| Incident severity | P1 / P2 |
| Trigger event | _eg "ArgoCD app Degraded > 5min", "burn-rate alert fired"_ |
| Stage at trigger | T7.1 stage 4 / T7.2 stage 5a / 5b / 5c |
| Tenants impacted | count + IDs |
| Cell-2 active duration before halt | _hh:mm_ |
| First detected at | _ISO timestamp UTC_ |
| Mitigation started at | _ISO timestamp UTC_ |
| Tenant(s) re-pinned to cell-1 at | _ISO timestamp UTC_ |
| Time-to-mitigate | _mm:ss_ |
| Customer comms sent (§6, §7) | yes / no |
| RCA owner | name |
| Status | DRAFT / FINAL |

---

## Timeline (fill chronologically)

| Time (UTC) | Event | Source |
|---|---|---|
| | _eg "deploy of tenant T succeeded on cell-2"_ | argocd app sync log |
| | _eg "burn-rate alert fired"_ | prometheus |
| | _eg "operator paged via #incident-..."_ | slack |
| | _eg "rollback initiated via loopzero admin tenants pin"_ | audit log |
| | _eg "all canary tenants Healthy on cell-1"_ | kubectl |
| | _eg "incident resolved, customers notified §7"_ | slack |

---

## Root cause categorization (C17 — pick exactly ONE)

Per MEET-INFRA-NORMALIZE-T3-multi-cell.md §4 categorization:

- [ ] **(a) Capacity** — cell-2 under-provisioned vs prod load profile.
  Examples: insufficient node count, undersized PVCs, RAM exhaustion,
  Argo controller workflowWorkers limit hit.
- [ ] **(b) Config drift** — cell-2 cluster missing a component present on
  cell-1 OR a config value diverged. Examples: cert-manager missing,
  ESO missing, ClusterIssuer stale, NetworkPolicy too strict.
- [ ] **(c) Traffic pattern** — prod has request shape dev didn't.
  Examples: long-tail latency, 95th-percentile-only behavior, batch
  workload not anticipated by capacity model.
- [ ] **(d) Customer-specific** — tenant T has unusual workload.
  Examples: WebSocket usage, large gRPC streams, high deployment cadence,
  custom domain / external dependency that breaks on different egress.

**Selected:** _(a) / (b) / (c) / (d)_

**One-paragraph cause statement:**

> _Be specific. "Cell-2 ran out of memory" is not a cause. "Argo
> workflow-controller pod evicted at {time} after p99 memory hit
> 1.8GiB requesting 2GiB; cause: cell-2 nodeClass=small does not match
> cell-1 nodeClass=medium" is a cause._

---

## Why was this not caught in dev canary (T5.6/T5.7)?

C17 mandatory section. Force ourselves to extract a learning.

- [ ] T5.6 (1h synthetic) didn't simulate this load shape — fix:
  _what synthetic test should be added_
- [ ] T5.7 (24h, 10% dev) didn't see this because dev tenants don't
  exhibit this pattern — fix:
  _what staging traffic should be added_
- [ ] Detection gap: alert fired late OR missing — fix:
  _what alert should be added/tightened_
- [ ] Capacity model used wrong inputs — fix:
  _what input was wrong, where it lives_

---

## Action items (each must have owner + horizon)

| # | Action | Category | Owner | Horizon | Status |
|---|---|---|---|---|---|
| 1 | _eg "Bump cell-2 nodeClass small → medium"_ | (a) | infra | within 7d | open |
| 2 | _eg "Add capacity gate: cell-{N} max_pods_per_node"_ | (a) | sre | next sprint | open |
| 3 | _eg "Sync cell-2 helmfile with cell-1 baseline diff"_ | (b) | infra | next deploy | open |
| 4 | _eg "Add CanaryCellMemoryHigh alert at 70% threshold"_ | detection | sre | within 14d | open |

**Halt criterion to resume canary:** all action items in column "open"
flipped to "shipped" + 24h soak in dev. Without this, do NOT re-enter
the stage that triggered the halt.

---

## Rollback verification (C17)

The point of this section is to prove the rollback worked AND was
durable. Not to declare success because pods came up.

- [ ] All canary tenants returned to cell-1 within 5min of mitigation start.
- [ ] HTTP 200 confirmed for each tenant's primary domain (one curl
  per tenant logged here).
- [ ] No data loss (no DB row inserted on cell-2 lost in transit).
- [ ] No state divergence: ArgoCD apps for canary tenants now Synced/Healthy
  pointing at cell-1.
- [ ] Secret rotation NOT triggered as a side effect (would amplify blast
  radius).
- [ ] Customer comms §7 (rollback notification) sent within 10min.
- [ ] Status page updated.

---

## Customer impact

| Metric | Value |
|---|---|
| Customer-visible downtime | _seconds; 0 if rollback was seamless_ |
| HTTP 5xx during incident | _request count_ |
| Failed deploys during incident | _build IDs_ |
| Customer reach-out (inbound) | _count_ |
| Customer satisfaction post-incident | _per §7 follow-up_ |

If downtime > 30s OR failed deploys > 0: incident is "customer-impacting"
and triggers credit policy review (link to billing-runbook).

---

## Related artifacts

- Incident channel: `#incident-{YYYY-MM-DD}-{slug}`
- Status page link: _url_
- Grafana snapshot: _url, time range_
- Audit log query: `SELECT * FROM audit_log WHERE action='tenant.pinned' AND occurred_at BETWEEN ...`
- Argo Workflow logs (if build affected): _url_
- ArgoCD app diff (cell-1 vs cell-2 manifests): _link to gitops repo commits_

---

## Sign-off

| Role | Name | Sign-off date |
|---|---|---|
| SRE on-call | | |
| Staff SRE | | |
| Customer-success | | |
| Engineering manager | | |

---

## Append to RCA index

When FINAL: add row to `docs/runbooks/multi-cell-rca-index.md`:

```
| YYYY-MM-DD | RCA-CELL2-XXXX-NN | (a)/(b)/(c)/(d) | severity | TTM |
```

The index is the source of truth for "should we proceed to next stage?"
— if any open action items reference the next stage's risk profile, halt.

---

## Versioning

Template v1.0 — 2026-04-30. Owner: SRE. Updates require:
1. Reflect C17 review board feedback.
2. Bump version + add row in CHANGELOG.md (Changed section).
