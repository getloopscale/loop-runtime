# LoopZeroGitMirrorExhausted

**Severity:** Critical
**Alert:** `loopzero_git_desired_state_divergence == 1 and on(tenant, env) loopzero_git_mirror_status{status="FAILED"} == 1`
**Duration:** 3h

## Detection
- Alert fires in Prometheus/Grafana
- Slack notification via Alertmanager
- Git desired state diverges AND mirror has been FAILED for 3+ hours
- All retries exhausted; tenant/env stuck with stale state
- Affected: `{{ $labels.tenant }}/{{ $labels.env }}`

## Triage (copy-paste commands)
```bash
# 1. Check Gitea connectivity from API
kubectl --context mgmt -n loopzero-system exec deploy/loopzero-api -- \
  curl -s http://gitea-http.loopzero-system:3000/api/v1/version

# 2. Check API logs for mirror push errors
kubectl --context mgmt -n loopzero-system logs deploy/loopzero-api --tail=50 \
  | grep -i "mirror\|gitops\|push"

# 3. Check build status for affected tenant
kubectl --context mgmt -n loopzero-system logs deploy/loopzero-api --tail=50 \
  | grep -i "RELEASING\|gitops"

# 4. Check Gitea repo status
kubectl --context mgmt -n loopzero-system exec deploy/loopzero-api -- \
  curl -s "http://gitea-http.loopzero-system:3000/api/v1/repos/search?limit=10" \
  -H "Authorization: token $GITEA_TOKEN" | jq '.[].full_name'

# 5. Check Prometheus metrics
curl -s 'http://localhost:9090/api/v1/query?query=loopzero_git_mirror_status{status="FAILED"}' | jq .
```

## Resolution
1. **Gitea is down — fix Gitea first:**
   See [LoopZeroGiteaDown.md](LoopZeroGiteaDown.md) runbook.

2. **Gitea is up but push fails (auth/permission):**
   ```bash
   # Verify Gitea admin token
   kubectl --context mgmt -n loopzero-system get secret gitea-admin-secret -o jsonpath='{.data.token}' | base64 -d

   # Test push manually
   kubectl --context mgmt -n loopzero-system exec deploy/loopzero-api -- \
     git ls-remote http://gitea-http.loopzero-system:3000/<org>/<repo>.git
   ```

3. **Retry gitops push via API (trigger a new build):**
   ```bash
   # The next successful build for this tenant/env will attempt gitops push
   # Alternatively, trigger a manual retry if the build is stuck in RELEASING
   kubectl --context mgmt -n loopzero-system exec deploy/loopzero-api -- \
     curl -s -X POST http://localhost:8080/api/v1/internal/retry-mirror \
     -H "Content-Type: application/json" \
     -d '{"tenant":"<tenant>","environment":"<env>"}'
   ```

4. **Verify manifests are valid:**
   ```bash
   # Clone and inspect the gitops repo
   kubectl --context mgmt -n loopzero-system run git-check --rm -i --restart=Never \
     --image=bitnami/git:latest -- \
     git clone http://gitea-http.loopzero-system:3000/<org>/<repo>.git /tmp/check
   ```

## Escalation
- If not resolved in 15 min: escalate to platform team
- If Gitea data corruption: invoke Gitea restore procedure

## Verification
```bash
# Verify mirror status recovered
curl -s 'http://localhost:9090/api/v1/query?query=loopzero_git_mirror_status{status="FAILED"}' \
  | jq '.data.result'
# Should be empty

# Verify divergence cleared
curl -s 'http://localhost:9090/api/v1/query?query=loopzero_git_desired_state_divergence' \
  | jq '.data.result[] | select(.value[1] == "1")'
# Should be empty
```

## Post-Incident
- [ ] Update incident log
- [ ] Root cause analysis: why did mirror push fail for 3+ hours?
- [ ] Check Gitea health monitoring and alerting
