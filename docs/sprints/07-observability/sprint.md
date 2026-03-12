# Sprint 7 — Observability Complete

> **Weeks:** 22-23
> **System Design Reference:** §23 (Sprint 7)

---

## Objective

Full observability stack operational. Every failure mode has a dashboard and alert. On-call can diagnose incidents without reading code.

## Product Exit Gate

Simulated incident (Gitea down) resolved using only dashboards + runbooks in < 15 minutes.

## CLI Commands Delivered

None — this sprint is an observability layer.

## Tasks

### Metrics

```
[ ] All metrics from section 15.2 emitting correctly
[ ] loopzero_argocd_app_count with app_type label (production|staging|preview)
[ ] loopzero_argocd_circuit_state metric
[ ] loopzero_git_desired_state_divergence gauge (60s verification loop)
[ ] loopzero_gitea_concurrent_operations gauge
[ ] Correlation ID end-to-end (CLI → API → Argo Workflow → ArgoCD)
```

### Dashboards

```
[ ] Platform Overview dashboard
[ ] Per-Tenant dashboard
[ ] Build Pipeline dashboard
[ ] Drift Investigation dashboard
[ ] Failure Investigation dashboard
[ ] SLO tracking dashboard (5-min deploy target)
```

### Alerting

```
[ ] All alerting rules from section 15.5
[ ] LoopZeroPostgresDown, LoopZeroGiteaDown, LoopZeroGiteaBackupStale
[ ] LoopZeroMirrorSyncStale, LoopZeroGitMirrorExhausted (3h threshold)
[ ] LoopZeroFallbackStuck, LoopZeroSecretSyncFailed
[ ] LoopZeroBuildEgressStale
[ ] ArgoCD sync duration > 30s
[ ] ArgoCD app count thresholds: Warning at 200, Critical at 400
[ ] Alert filters: loopzero_git_desired_state_divergence excludes PENDING and FAILED mirrors
```

### Startup reconciler

```
[ ] API startup reconciler: 5-step ordered startup (section 7.2)
[ ] PITR detection as step 1 (Release-ID from Git trailers vs environment_targets)
[ ] Fallback reconciliation step
```

### Tests

```
[ ] Test: every alert fires correctly in simulated failure
[ ] Test: correlation ID traceable from CLI request to pod log
[ ] Incident simulation: Gitea down → alert fires → runbook followed → resolved
```

## Key References in System Design

| Topic | Section |
|-------|---------|
| Observability (three pillars) | §15.1 |
| Key metrics (full list) | §15.2 |
| Correlation ID flow | §15.3 |
| SLOs | §15.4 |
| Alerting rules | §15.5 |
| Dashboards | §15.6 |
| API crash recovery (startup reconciler) | §7.2 |
| PITR detection (Git trailers) | §7.2, §17.5 |
| Failure modes & recovery | §12 |
| Recovery invariants | §12.2 |
