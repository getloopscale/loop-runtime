# Runbook: LoopZeroAppDeployStuck

**Severity:** Warning
**Component:** ArgoCD + loopzero-api orchestrator
**SLO impact:** Deploy SLO (95% < 5min)

## Alert expression

```
loopzero_deploy_in_progress_seconds > 600
```

Triggered when a deploy has been in progress for more than 10 min (vs SLO of 5 min).

## What this means

Build succeeded but ArgoCD has not yet marked the Application Healthy, OR build is hanging in BuildKit/cosign step.

## Triage (first 5 min)

1. Check build status: `loopzero build list --project <name> --env <env> | head`
2. If build is RELEASING: ArgoCD sync issue. Check Application:
   ```
   kubectl -n argocd get application -l loopzero.io/tenant=<tenant>
   kubectl -n argocd describe application <name>
   ```
3. If build is RUNNING > 30min: BuildKit hang. Check workflow:
   ```
   kubectl -n argo get workflow -l loopzero.io/build-id=<build-id>
   ```

## Diagnose

| Stage | Symptom | Likely cause |
|-------|---------|--------------|
| RUNNING > 30m | BuildKit hung | Cache miss + slow base image; OOM in builder |
| RELEASING > 10m | cosign sign timing out | sigstore/Fulcio backend slow |
| ArgoCD Out-of-sync | Sync queued | ArgoCD throttled (controller-runtime overloaded) |
| ArgoCD Degraded | Resources unhealthy | App crash, ImagePullBackOff, NP misconfiguration |

## Mitigate

**Cancel hung build:**
```
loopzero cancel <build-id>
```

**Force ArgoCD sync:**
```
kubectl -n argocd argocd app sync <app-name> --force
```

**Workflow node stuck:**
```
kubectl -n argo delete workflow <wf-name>  # operator will recreate via reconcile
```

## Verify recovery

- Deploy completes (release ACTIVE) OR rolled back cleanly
- `loopzero_deploy_in_progress_seconds` returns to 0

## Escalation

After 2 cancel/retry cycles failed: page on-call build/pipeline engineer.

## Related

- Metric: `loopzero_deploy_in_progress_seconds`, `loopzero_deploy_lead_time_seconds`
- CLI: `loopzero deploy`, `loopzero cancel`, `loopzero build inspect`
- ArgoCD UI: `argocd.<base>/applications`
