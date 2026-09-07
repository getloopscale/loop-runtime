# Runbook — platform alerts

**Owner:** SRE · **Created:** 2026-09-07 · **Covers:** the 27 alerts that had no `runbook`
annotation after the 2026-09-07 system-design audit

## Read this header before using this runbook

**This document is assembled from artifacts in the repository — each alert's own expression,
threshold and window, plus the component it queries. It is not written from experience of these
alerts firing.** Nobody has yet responded to most of them in production.

That makes it useful for the first thirty seconds of an incident — *what fired, what it measures,
where to look first* — and unreliable for the decision that follows. Every section ends with what it
cannot tell you, and those gaps are the honest part. **Correct each section from the first real
incident it is used in.** That version will be worth more than this one.

An earlier draft of the audit that produced this file proposed deleting these alerts instead,
because they lacked documentation. That was rejected for a reason worth keeping in mind here: an
alert without a runbook still tells you something broke; a deleted alert tells you nothing.

---

## Delivery path — user traffic

### `LoopZeroCFTunnelDisconnected` — 2m

`absent(cloudflared_tunnel_connections{status="connected"})` or the count at zero.
**All user traffic is blocked while this fires.** It is the highest-impact alert in this document.

```bash
kubectl --context k3d-loopzero-cell-0 -n loopzero-system get pods -l app=cloudflared
kubectl --context k3d-loopzero-cell-0 -n loopzero-system logs -l app=cloudflared --tail=100
```

`absent()` also fires when the metric stops being scraped, so confirm the exporter is up before
concluding the tunnel is down — a monitoring outage and a traffic outage look identical here.

**Does not tell you:** whether to fail traffic over, and where to. No alternative ingress path is
declared in this repository.

---

## Tenant correctness — the ones that are quiet and expensive

### `LoopZeroMisplacedTenant` — 5m

`rate(loopzero_misplaced_tenant_total[10m]) > 0`. Tenant resources observed on the wrong cell — an
ADR-0018 violation. Placement is sticky and one-time (`api/internal/placement/engine.go`); the only
sanctioned move is `loopzero admin tenants pin`, which updates the DB row and the Tenant CR atomically.

```bash
kubectl --context k3d-loopzero-core get tenants -o custom-columns=NAME:.metadata.name,CELL:.spec.cellID
# compare against the DB, which is the source of truth for cell_id
```

**Does not tell you:** whether the CR or the DB is right. They diverged somehow, and which one to
believe depends on how — a failed pin leaves a different trail from a manual CR edit.

### `LoopZeroCellPinnerCRPatchDiverged` — 2m

`rate(loopzero_cell_pinner_cr_patch_fail_total[5m]) > 0`. The DB moved and the CR did not. Same
divergence as above, caught at the write instead of by observation.

### `LoopZeroTenantDeletionStuck` — 5m

Deletion is finalizer-driven and `gitops.Remove` failure is contractually FATAL, so a stuck deletion
usually means the GitOps removal did not complete.

```bash
kubectl --context k3d-loopzero-core get tenant <name> -o jsonpath='{.metadata.finalizers}'
```

**Does not tell you:** whether removing a finalizer is safe here. It is the standard unstick and it
can orphan resources the finalizer existed to clean.

### `LoopZeroSBOMGenerationStalled` — 5m

Releases are activating while SBOM generation has produced nothing for 10 minutes. **Supply-chain
provenance is missing for whatever ships during this window**, which is not visible in the deploy
itself — the deploy succeeds.

### `LoopZeroWorkqueueTerminalWriteFailed` — 2m

A terminal-state write to the DB failed. The alert's own summary names the consequence: duplicate
side-effects risk. The work completed and the record that it completed did not land, so a retry may
redo it.

---

## Capacity and saturation

| Alert | Fires on | First look |
|---|---|---|
| `TenantReconcileBacklog` (5m) | `workqueue_depth{name=~"tenant\|environment\|application\|exposure"} > 50` | operator logs; a backlog is usually one slow reconcile blocking a queue, not global load |
| `BuildFailureRateHigh` (5m) | `>10%` of builds FAILED over 5m | recent Argo Workflow failures; check whether one step (clone, sign, push) dominates |
| `LoopZeroBuildQueueSlow` (5m) | `loopzero_build_queue_depth > 5` | `maxConcurrentBuildsPerCell` is 20 — a depth of 5 is early, not saturated |
| `ArgoWorkflowQuotaExceeded` (10m) | 403s `> 0.1/s` | quota **or RBAC** — the status code does not distinguish them, and the fix differs completely |
| `HPAAtMaxReplicas` (5m) | current == max | tenant workloads only; the platform has no HPA by decision (ADR 2026-09-07) |
| `PVCPendingSustained` (2m) | any PVC Pending 2m+ | storage class and provisioner; `nfs-server-provisioner` serves RWX here |
| `GiteaAuthLatencyHigh` (5m) | p95 `> 2s` | Gitea is on the deploy critical path — ArgoCD syncs from it |

**Does not tell you:** which of these are load and which are a single stuck object. Every threshold
above is a static number nobody has calibrated against measured traffic — the audit found no QPS
figure declared for any tier.

---

## Latency and SLO-adjacent

| Alert | Fires on | Note |
|---|---|---|
| `LoopZeroQueueSlow` (10m) | build p95 `> 300s` | this is the `deploy_latency` SLO's build half |
| `LoopZeroSyncSlow` (10m) | end-to-end deploy p95 `> 600s` | |
| `LoopZeroDeployFailureRate` (30m) | `>10%` over 24h rolling | slower cousin of `BuildFailureRateHigh`; both firing means it is not a blip |
| `LoopZeroLockContention` (5m) | lock hold p95 `> 2s` | the environment lock serialises every mutation for a `(tenant, project, environment)` — contention here slows everything downstream |

These four are the closest thing to SLO burn signals outside the Sloth-generated rules. When one
fires, check `docs/runbooks/multi-cell-canary.md` for whether a canary is in progress — a canary
stage has its own halt criteria and they take precedence.

---

## Security and supply chain

### `LoopZeroServiceAuthReplayNonceRate` — 10m

Signature rejections with `reason="replay_nonce"` above `0.01/s`. A replayed nonce is either an
attack or a client retrying with a stale signature. **Both are worth knowing; only one is urgent**,
and the metric does not distinguish them.

### `LoopZeroServiceAuthSecretExpiring` — 1h

`loopzero_service_auth_secret_age_seconds > 6912000` — 80 days of a 90-day rotation deadline. This is a
deadline alert, not an incident: it fires with ten days of runway.
Rotation procedure: `docs/operations/session-token-rotation-runbook.md`.

---

## Chaos-window invariants

These three exist because chaos experiments set `loopzero_chaos_active`, and the invariant is that
certain conditions must **not** occur outside a chaos window.

| Alert | Meaning |
|---|---|
| `AdmissionDeniedRateHigh_OutsideChaos` (5m) | admission denials elevated with no experiment running |
| `PGCircuitBreakerOpen_OutsideChaos` (3m) | the Postgres breaker (threshold 90%) opened for real |
| `ChaosWindowStuckOpen` (1h) | `loopzero_chaos_active == 1` for over an hour — an experiment was never cleared, which **disables the other two** |

`ChaosWindowStuckOpen` is the one to fix first when several fire: while it is stuck, the two
`_OutsideChaos` alerts are suppressed by their own `unless` clause and cannot warn about anything.

---

## Observability pipeline

| Alert | Fires on | Note |
|---|---|---|
| `LoopZeroTempoStorageHigh` (10m) | Tempo PVC above 80% | retention is 168h (`infra/helm/tempo/values.yaml:14`); if usage grows at fixed retention, trace volume grew |
| `LoopZeroCollectorExportErrors` (5m) | any failed span export in 5m | spans are being dropped; traces for the affected window are incomplete and will stay that way |

---

## Infrastructure state

| Alert | Fires on | Note |
|---|---|---|
| `PulumiArchiveStaleness` (1h) | no archive in 90 days | Pulumi is legacy AS-IS until the AM8 cutover, so this stays live until then |
| `PulumiOrphanStackDetected` (6h) | stack count `> 4` | a count above baseline suggests a stack nobody reaped |
| `ZeroReadyOperatorWindow` (30s) | any of the four operators at zero ready replicas | **kept deliberately** when the two expired cutover alerts beside it were deleted: zero ready operators means nothing is reconciling, which is permanent, not cutover-era |

---

## What this runbook does not cover, for any alert

- **Escalation.** Who to wake, and when. Alertmanager routes on severity, not on component.
- **Whether the threshold is right.** Every number here was chosen before the load suites produced
  measured traffic figures. A noisy alert may be a bad threshold rather than a bad system.
- **Rollback authority.** Several of these are conditions under which a deploy should be reverted,
  and nothing here says who decides that.

## Related

- Canary halt criteria, which override these during a canary: `docs/runbooks/multi-cell-canary.md`
- Backup alerts: `docs/runbooks/velero-backup-alerts.md`
- Security-delivery alerts, written from real incidents: `docs/operations/runbook-security-alerts.md`
- Audit that produced this file: `system-design-output/final_report.md` — `SRE-05.1`, `SRE-04.4`
