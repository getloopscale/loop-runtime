# Runbook: LoopZeroPlatformBootstrapStuck

**Severity:** Warning
**Component:** loopzero-tenant-controller (PlatformBootstrap reconciler)
**SLO impact:** Self-hosted onboarding, install-to-deploy time

## Alert expression

```
loopzero_platformbootstrap_phase{phase!="Complete"} == 1
  and time() - loopzero_platformbootstrap_phase_started_timestamp > 1200
```

Triggered when a PlatformBootstrap CR is in any non-terminal phase for more than 20 min.

## What this means

The platform bootstrapping (Gitea config + ArgoCD setup + API seed) is failing or hung. New self-hosted installations cannot complete; existing platforms are not progressing through phase transitions.

## Triage (first 5 min)

1. Read CR status:
   ```
   kubectl get platformbootstrap -A
   kubectl describe platformbootstrap <name>
   ```
2. Identify current phase from `Status.Phase` (Pending, ConfiguringGitea, ConfiguringArgoCD, WaitingForAPI, Complete, Failed).
3. Read controller logs filtered by CR name:
   ```
   kubectl -n loopzero-system logs deploy/loopzero-tenant-controller --tail=500 | grep <bootstrap-name>
   ```

## Diagnose

| Phase | Symptom | Likely cause |
|-------|---------|--------------|
| Pending | Reconcile not starting | Controller pod crashed or paused |
| ConfiguringGitea | Stuck > 5min | Gitea unreachable, admin token missing/expired |
| ConfiguringArgoCD | Stuck > 5min | ArgoCD repo-server crashed; argocd-cm patching failed |
| WaitingForAPI | Stuck indefinitely | loopzero-api not yet healthy or RBAC mismatch |

## Mitigate

| Cause | Action |
|-------|--------|
| Gitea unreachable | Verify `kubectl -n gitea get pods`; check Gitea logs |
| Token missing | Re-run `loopzero platform install` with `--force-tokens`; verify Secret `gitea-admin-token` |
| ArgoCD repo-server | `kubectl -n argocd rollout restart deploy argocd-repo-server` |
| API not healthy | Check `kubectl -n loopzero-system get pods -l app=loopzero-api`; investigate via T-DB-Pool runbook if applicable |

## Verify recovery

- `Status.Phase = Complete`
- All conditions True: GiteaConfigured, ArgoCDConfigured, APISeeded
- `loopzero platform status` returns Healthy

## Escalation

> 30 min stuck or repeated regression to earlier phase: page on-call platform engineer.

## Related

- Metric: `loopzero_platformbootstrap_phase`
- Source: `operators/internal/controller/platformbootstrap_*.go`
- State machine: `pkg/statemachine/platformbootstrap.go`
- Phase 7 audit: `flow_platformbootstrap` is the best-implemented flow (VAL-007 positive finding)
