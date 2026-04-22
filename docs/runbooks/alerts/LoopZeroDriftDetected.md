# LoopZeroDriftDetected

**Severity:** Critical
**Alert:** `loopzero_drift_detected == 1`
**Duration:** 5m

## Detection
- Alert fires in Prometheus/Grafana
- Slack notification via Alertmanager
- Cluster state diverges from desired state in Git for >5 minutes
- Affected tenant/env visible in alert labels: `{{ $labels.tenant }}/{{ $labels.env }}`

## Triage (copy-paste commands)
```bash
# 1. Identify drifted ArgoCD application
kubectl --context mgmt -n argocd get applications -o wide | grep -v "Synced.*Healthy"

# 2. Get detailed diff for the drifted app
DRIFTED_APP=$(kubectl --context mgmt -n argocd get applications -o json \
  | jq -r '.items[] | select(.status.sync.status != "Synced") | .metadata.name' | head -1)
kubectl --context mgmt -n argocd exec deploy/argocd-server -- \
  argocd app diff $DRIFTED_APP --local-repo-root /tmp 2>&1 | head -50

# 3. Check ArgoCD sync status details
kubectl --context mgmt -n argocd get application $DRIFTED_APP -o yaml \
  | grep -A20 "status:"

# 4. Check if auto-sync failed
kubectl --context mgmt -n argocd get application $DRIFTED_APP -o json \
  | jq '.status.operationState'

# 5. Check API drift metric details
kubectl --context mgmt -n platform-system logs deploy/loopzero-api --tail=30 \
  | grep -i "drift"
```

## Resolution
1. **Manual sync needed (LoopZero uses manual sync only):**
   ```bash
   kubectl --context mgmt -n argocd exec deploy/argocd-server -- \
     argocd app sync $DRIFTED_APP --force
   ```

2. **Sync failed due to invalid manifests:**
   ```bash
   # Check ArgoCD sync error
   kubectl --context mgmt -n argocd get application $DRIFTED_APP -o json \
     | jq '.status.operationState.message'
   # Check Gitea for the offending manifest
   kubectl --context mgmt -n platform-system exec deploy/loopzero-api -- \
     curl -s "http://gitea-http.platform-system:3000/api/v1/repos/<org>/<repo>/contents/" \
     -H "Authorization: token $GITEA_TOKEN"
   ```

3. **Orphaned resources causing Degraded state:**
   ```bash
   # List resources managed by the app
   kubectl --context mgmt -n argocd get application $DRIFTED_APP -o json \
     | jq '.status.resources[] | select(.health.status != "Healthy")'
   ```

4. **Certificate CR stuck (known pattern):**
   ```bash
   # Manifest validation gate should reject Certificate CRs in gitops
   # If one leaked through, remove it
   kubectl --context workload get certificates --all-namespaces | grep -v "True"
   ```

## Escalation
- If not resolved in 15 min: escalate to platform team
- If drift is in a production tenant: escalate immediately

## Verification
```bash
# Verify app is synced
kubectl --context mgmt -n argocd get application $DRIFTED_APP -o jsonpath='{.status.sync.status}'
# Should be "Synced"

# Verify drift metric cleared
curl -s 'http://localhost:9090/api/v1/query?query=loopzero_drift_detected' \
  | jq '.data.result[] | select(.value[1] == "1")'
# Should return empty
```

## Post-Incident
- [ ] Update incident log
- [ ] Root cause analysis if recurring
- [ ] Investigate why manual sync was needed (gitops push failure? invalid manifests?)
