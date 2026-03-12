# Sprint 4 — `loopzero rollback` + `loopzero promote` + Cancel

> **Weeks:** 13-15
> **System Design Reference:** §23 (Sprint 4)

---

## Objective

Operational safety commands work. Lucas can recover from a bad deploy in seconds.

## Product Exit Gate

- `loopzero rollback` reverts in < 30s.
- `loopzero promote` moves staging to production.
- `loopzero build cancel` stops a running build.

## CLI Commands Delivered

`rollback`, `promote`, `build`, `env`, `team`, `project`

## Tasks

### CLI — operational commands

```
[ ] loopzero rollback: revert to previous deploy (production)
[ ] loopzero rollback --env staging
[ ] loopzero rollback <deploy-id>: revert to specific deploy
[ ] loopzero rollback --list: list available deploys
[ ] loopzero rollback: inline warnings for irreversible side effects + [Y/n] confirmation
[ ] loopzero rollback --force: skip confirmation (CI use)
[ ] loopzero promote --from staging --to production
[ ] loopzero promote --from staging --to production --app api
[ ] loopzero build list, loopzero build cancel <build-id>, loopzero build watch <build-id>
[ ] loopzero env list, loopzero team list, loopzero team switch, loopzero project list/link/unlink
```

### API — rollback/promote via environment_targets

```
[ ] Rollback: atomic pointer update in environment_targets
[ ] Promote: pointer update + ArgoCD notification
[ ] Cancel with cancellation points per build stage
[ ] Compensation sweepers (orphaned assets, images, namespaces)
[ ] State machines: Promotion, Rollback (section 5.4, 5.5)
[ ] Side effect classification (reversible, irreversible-known, irreversible-unknown)
[ ] Emergency fallback path: if gitMirrorStatus=FAILED, disable auto-sync, apply directly
[ ] loopzero.io/fallback-active annotation, loopzero-fallback field manager
[ ] Auto-sync restored when gitMirrorStatus → SYNCED
```

### StateReconciler

```
[ ] StateReconciler operator (full algorithm from section 13)
[ ] ReconciliationReport CRD
[ ] Authority-based drift classification
[ ] EXPECTED_PENDING_MIRROR classification for fallback deploys
```

### Tests

```
[ ] E2E: promote staging→production, rollback, cancel mid-build
[ ] E2E: drift detection + ArgoCD auto-repair
[ ] E2E: full deploy → rollback → verify previous version running
[ ] Chaos: API crash mid-deploy recovery
[ ] Chaos: concurrent promote and rollback → one wins, one gets 409
[ ] Test: fallback path activates when gitMirrorStatus=FAILED
```

## Key References in System Design

| Topic | Section |
|-------|---------|
| Core CLI workflows — rollback | §0.1 (Workflow 3) |
| CLI Reference — Rollback | §25.6 |
| CLI Reference — Promote | §25.7 |
| CLI Reference — Build management | §25.11 |
| CLI Reference — Teams and projects | §25.10 |
| Promotion state machine | §5.4 |
| Rollback state machine | §5.5 |
| `loopzero promote` complete flow | §10.2 |
| `loopzero rollback` complete flow | §10.3 |
| Cancellation & compensation | §11 |
| Build cancellation points | §11.1 |
| Compensation mechanisms | §11.2 |
| Side effect classification | §9.1 |
| State reconciliation algorithm | §13.2 |
| Emergency fallback path (DeploymentActivator) | §7.2 |
| Conflict matrix | §4.4 |
