# LoopZeroFallbackStuck

**Severity:** Critical
**Alert:** `loopzero_fallback_active == 1 and on(tenant, env) loopzero_git_mirror_status{status="FAILED"} == 1`
**Duration:** 3h

## Detection
- Alert fires in Prometheus/Grafana
- Slack notification via Alertmanager
- Emergency fallback deployment is active (direct apply, not via GitOps)
- Git mirror has been FAILED for 3+ hours, so auto-sync cannot re-enable
- Affected: `{{ $labels.tenant }}/{{ $labels.env }}`

## Triage (copy-paste commands)
```bash
# 1. Identify which tenant/env is in fallback
curl -s 'http://localhost:9090/api/v1/query?query=loopzero_fallback_active==1' | jq .

# 2. Check the stuck release
kubectl --context mgmt -n loopzero-system logs deploy/loopzero-api --tail=50 \
  | grep -i "fallback\|emergency\|mirror"

# 3. Check ArgoCD app status (should be OutOfSync if fallback bypassed gitops)
kubectl --context mgmt -n argocd get applications -o wide | grep -v "Synced"

# 4. Check Gitea (root cause of mirror failure)
kubectl --context mgmt -n loopzero-system exec deploy/loopzero-api -- \
  curl -s http://gitea-http.loopzero-system:3000/api/v1/version

# 5. Check current release state in DB
kubectl --context mgmt -n loopzero-system exec deploy/loopzero-api -- \
  curl -s http://localhost:8080/api/v1/internal/releases?tenant=<tenant>\&env=<env> | jq .
```

## Resolution
1. **Fix Gitea first (root cause):**
   See [LoopZeroGiteaDown.md](LoopZeroGiteaDown.md) runbook.

2. **Once Gitea is healthy, retry mirror:**
   ```bash
   # Mirror sync will retry automatically on next reconcile cycle
   # Force a reconcile by restarting the API
   kubectl --context mgmt -n loopzero-system rollout restart deployment/loopzero-api
   ```

3. **If mirror succeeds, fallback clears automatically:**
   Once the gitops push succeeds, ArgoCD will sync the latest desired state and the fallback
   flag clears. No manual intervention needed beyond fixing Gitea.

4. **Manual rollback if fallback state is incorrect:**
   ```bash
   # If the fallback-applied state is wrong, use loopzero rollback
   loopzero rollback --project <project> --environment production
   ```

5. **Manual activate if release is stuck:**
   ```bash
   # Check build/release state
   kubectl --context mgmt -n loopzero-system logs deploy/loopzero-api --tail=20 \
     | grep -i "release\|activate"
   # If a release is stuck in PENDING, it may need manual transition
   # (requires direct DB intervention as last resort)
   ```

## Escalation
- If not resolved in 15 min: escalate to platform team
- If affecting production tenant: escalate immediately

## Verification
```bash
# Verify fallback cleared
curl -s 'http://localhost:9090/api/v1/query?query=loopzero_fallback_active' \
  | jq '.data.result[] | select(.value[1] == "1")'
# Should be empty

# Verify mirror recovered
curl -s 'http://localhost:9090/api/v1/query?query=loopzero_git_mirror_status{status="FAILED"}' \
  | jq '.data.result'
# Should be empty

# Verify ArgoCD app is synced
kubectl --context mgmt -n argocd get applications -o wide
```

## Post-Incident
- [ ] Update incident log
- [ ] Root cause analysis: why was fallback needed and why did mirror fail for 3h+?
- [ ] Review Gitea reliability and backup procedures
