# LoopZeroTraefikDown

**Severity:** Critical
**Alert:** `absent(up{job=~".*traefik.*"} == 1)`
**Duration:** 1m

## Detection
- Alert fires in Prometheus/Grafana
- Slack notification via Alertmanager
- ALL platform traffic blocked: dashboard, API, auth, CLI, user apps

## Triage (copy-paste commands)
```bash
# 1. Check Traefik pods
kubectl --context mgmt -n loopzero-system get pods -l app.kubernetes.io/name=traefik
kubectl --context mgmt -n loopzero-system describe pods -l app.kubernetes.io/name=traefik

# 2. Check Traefik logs
kubectl --context mgmt -n loopzero-system logs -l app.kubernetes.io/name=traefik --tail=50

# 3. Check PDB (minAvailable: 1)
kubectl --context mgmt -n loopzero-system get pdb -l app.kubernetes.io/name=traefik

# 4. Check node health (Traefik needs healthy nodes)
kubectl --context mgmt get nodes

# 5. Check LoadBalancer service
kubectl --context mgmt -n loopzero-system get svc -l app.kubernetes.io/name=traefik

# 6. Check events
kubectl --context mgmt -n loopzero-system get events --sort-by=.lastTimestamp \
  | grep -i traefik | tail -10
```

## Resolution
1. **Pods in CrashLoopBackOff or Pending:**
   ```bash
   # Check resource constraints or config errors
   kubectl --context mgmt -n loopzero-system describe pods -l app.kubernetes.io/name=traefik \
     | grep -A5 "Last State\|Reason\|OOMKilled"
   # Restart
   kubectl --context mgmt -n loopzero-system rollout restart deployment/traefik
   kubectl --context mgmt -n loopzero-system rollout status deployment/traefik --timeout=120s
   ```

2. **Node issues (all nodes NotReady):**
   ```bash
   kubectl --context mgmt get nodes -o wide
   # For DO: check DOKS
   doctl kubernetes cluster get <mgmt-cluster-id>
   ```

3. **LoadBalancer external IP lost:**
   ```bash
   kubectl --context mgmt -n loopzero-system get svc traefik -o jsonpath='{.status.loadBalancer.ingress[0]}'
   # If empty, check cloud controller manager
   kubectl --context mgmt -n kube-system logs -l app=digitalocean-cloud-controller-manager --tail=20
   ```

4. **DNS not resolving (CF Tunnel route):**
   ```bash
   # If using CF Tunnel, Traefik is behind cloudflared. Check both.
   dig api.dev.loopzero.example
   ```

## Escalation
- If not resolved in 5 min: escalate immediately (total platform outage)
- If node/cluster issue: escalate to infra + open DO support ticket

## Verification
```bash
# Verify Traefik pods running
kubectl --context mgmt -n loopzero-system get pods -l app.kubernetes.io/name=traefik

# Verify external access
curl -sk https://api.dev.loopzero.example/healthz

# Verify Prometheus metric
curl -s 'http://localhost:9090/api/v1/query?query=up{job=~".*traefik.*"}' \
  | jq '.data.result[0].value[1]'
```

## Post-Incident
- [ ] Update incident log
- [ ] Root cause analysis if recurring
- [ ] Verify PDB and replica count (should be 2+ for HA)
