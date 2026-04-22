# LoopZeroBuildClusterUnreachable

**Severity:** Critical
**Alert:** `up{job="argo-workflows"} == 0`
**Duration:** 2m

## Detection
- Alert fires in Prometheus/Grafana
- Slack notification via Alertmanager
- No new builds can start; running builds may hang or timeout

## Triage (copy-paste commands)
```bash
# 1. Check build cluster is reachable
kubectl --context build cluster-info

# 2. Check Argo Workflows controller
kubectl --context build -n argo get pods -l app.kubernetes.io/name=argo-workflows-workflow-controller
kubectl --context build -n argo logs -l app.kubernetes.io/name=argo-workflows-workflow-controller --tail=30

# 3. Check DOKS cluster status (production)
doctl kubernetes cluster list
doctl kubernetes cluster get <build-cluster-id>

# 4. Check kubeconfig validity
kubectl --context build auth whoami

# 5. Check node status
kubectl --context build get nodes
```

## Resolution
1. **DOKS cluster unreachable (production):**
   ```bash
   # Check DO status page: https://status.digitalocean.com
   # Refresh kubeconfig
   doctl kubernetes cluster kubeconfig save <build-cluster-id>
   # Retry
   kubectl --context build cluster-info
   ```

2. **Argo Workflows controller down:**
   ```bash
   kubectl --context build -n argo describe pod -l app.kubernetes.io/name=argo-workflows-workflow-controller
   kubectl --context build -n argo rollout restart deployment/argo-workflows-workflow-controller
   ```

3. **Network connectivity (cross-cluster):**
   ```bash
   # Verify API pod can reach build cluster
   kubectl --context mgmt -n platform-system exec deploy/loopzero-api -- \
     curl -sk https://<build-cluster-api-endpoint>/healthz
   ```

4. **k3d cluster down (local dev):**
   ```bash
   k3d cluster list
   # If stopped:
   k3d cluster start loopzero-build
   ```

## Escalation
- If not resolved in 10 min: escalate to SRE (no builds = no deploys)
- If DOKS issue: open DO support ticket

## Verification
```bash
# Verify Argo controller is healthy
kubectl --context build -n argo get pods -l app.kubernetes.io/name=argo-workflows-workflow-controller

# Verify a workflow can be submitted
kubectl --context build -n argo get wf --sort-by=.metadata.creationTimestamp | tail -3

# Verify Prometheus metric
curl -s 'http://localhost:9090/api/v1/query?query=up{job="argo-workflows"}' \
  | jq '.data.result[0].value[1]'
```

## Post-Incident
- [ ] Update incident log
- [ ] Root cause analysis if recurring
- [ ] Verify ClusterCredential CR reflects connectivity status
