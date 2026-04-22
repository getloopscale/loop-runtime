# LoopZeroArgoCDCircuitOpen

**Severity:** Critical
**Alert:** `loopzero_argocd_circuit_state == 1`
**Duration:** 0m (fires immediately when circuit opens)

## Detection
- Alert fires in Prometheus/Grafana
- Slack notification via Alertmanager
- New deployments rejected with 503 (circuit breaker open)
- Circuit opens after 3 consecutive ArgoCD health check failures

## Triage (copy-paste commands)
```bash
# 1. Check ArgoCD server and controller
kubectl --context mgmt -n argocd get pods
kubectl --context mgmt -n argocd logs deploy/argocd-server --tail=30
kubectl --context mgmt -n argocd logs deploy/argocd-application-controller --tail=30

# 2. Check ArgoCD health endpoint
kubectl --context mgmt -n argocd exec deploy/argocd-server -- \
  curl -s http://localhost:8080/healthz

# 3. Check circuit breaker state in API logs
kubectl --context mgmt -n platform-system logs deploy/loopzero-api --tail=30 \
  | grep -i "circuit\|argocd"

# 4. Check ArgoCD Application sync status
kubectl --context mgmt -n argocd get applications -o wide
```

## Resolution
1. **ArgoCD server/controller crashed:**
   ```bash
   kubectl --context mgmt -n argocd rollout restart deployment/argocd-server
   kubectl --context mgmt -n argocd rollout restart deployment/argocd-application-controller
   # Wait for ready
   kubectl --context mgmt -n argocd rollout status deployment/argocd-server --timeout=120s
   ```

2. **ArgoCD Redis down (session store):**
   ```bash
   kubectl --context mgmt -n argocd get pods -l app.kubernetes.io/name=argocd-redis
   kubectl --context mgmt -n argocd rollout restart deployment/argocd-redis
   ```

3. **Circuit breaker auto-recovery:**
   The circuit closes automatically after 2 consecutive successful health checks.
   Once ArgoCD is healthy, the API will detect it within ~30s and resume accepting deploys.

4. **If circuit stays open after ArgoCD is healthy:**
   ```bash
   # Restart API to reset circuit state
   kubectl --context mgmt -n platform-system rollout restart deployment/loopzero-api
   ```

## Escalation
- If not resolved in 15 min: escalate to SRE
- If ArgoCD data corruption: escalate to platform team lead

## Verification
```bash
# Verify ArgoCD is healthy
kubectl --context mgmt -n argocd exec deploy/argocd-server -- curl -s http://localhost:8080/healthz

# Verify circuit is closed (metric = 0)
curl -s 'http://localhost:9090/api/v1/query?query=loopzero_argocd_circuit_state' \
  | jq '.data.result[0].value[1]'

# Verify deploys work (try a build)
kubectl --context mgmt -n platform-system logs deploy/loopzero-api --tail=10 | grep -i "circuit"
```

## Post-Incident
- [ ] Update incident log
- [ ] Root cause analysis if recurring
- [ ] Check ArgoCD resource limits (OOM?) and adjust if needed
