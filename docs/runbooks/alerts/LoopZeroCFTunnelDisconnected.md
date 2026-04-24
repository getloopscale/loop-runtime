# LoopZeroCFTunnelDisconnected

**Severity:** Critical
**Alert:** Cloudflare Tunnel health check failure / cloudflared pods not ready
**Duration:** 2m

## Detection
- CF dashboard shows tunnel disconnected (https://dash.cloudflare.com → Zero Trust → Tunnels)
- User apps unreachable via `*.dev.loopzero.example` (but mgmt services may still work if direct)
- cloudflared pod readiness probe failing

## Triage (copy-paste commands)
```bash
# 1. Check cloudflared pods (runs in runtime/workload cluster)
kubectl --context workload -n loopzero-system get pods -l app=cloudflared
kubectl --context workload -n loopzero-system describe pods -l app=cloudflared

# 2. Check cloudflared logs
kubectl --context workload -n loopzero-system logs -l app=cloudflared --tail=50

# 3. Check readiness probe
kubectl --context workload -n loopzero-system exec -l app=cloudflared -- \
  curl -s http://localhost:2000/ready

# 4. Check tunnel token secret exists
kubectl --context workload -n loopzero-system get secret cloudflared-tunnel-token

# 5. Check if Traefik (upstream) is healthy
kubectl --context workload -n loopzero-system get pods -l app.kubernetes.io/name=traefik

# 6. Check from outside
dig api.dev.loopzero.example
curl -sk https://api.dev.loopzero.example/healthz
```

## Resolution
1. **cloudflared pods crashed/not ready:**
   ```bash
   kubectl --context workload -n loopzero-system rollout restart deployment/cloudflared
   kubectl --context workload -n loopzero-system rollout status deployment/cloudflared --timeout=120s
   ```

2. **Tunnel token expired or invalid:**
   ```bash
   # Check if the tunnel still exists in CF
   # Visit: https://dash.cloudflare.com → Zero Trust → Access → Tunnels
   # If deleted, re-create via Pulumi:
   cd /home/paulo/Projetos/loopzero/loopzero/infra/pulumi-platform
   npx pulumi up --stack dev
   ```

3. **CF service outage:**
   ```bash
   # Check CF status: https://www.cloudflarestatus.com/
   # If CF is down, wait for recovery. No action on our side.
   ```

4. **Upstream (Traefik) unreachable:**
   ```bash
   # cloudflared routes to Traefik. If Traefik is down, tunnel connects but returns 502.
   # See LoopZeroTraefikDown.md
   kubectl --context workload -n loopzero-system get pods -l app.kubernetes.io/name=traefik
   ```

5. **Node/cluster issue:**
   ```bash
   kubectl --context workload get nodes
   doctl kubernetes cluster get <workload-cluster-id>
   ```

## Escalation
- If not resolved in 10 min: escalate to SRE (all external traffic blocked)
- If CF account issue: escalate to platform team lead (CF credentials)

## Verification
```bash
# Verify cloudflared pods are ready
kubectl --context workload -n loopzero-system get pods -l app=cloudflared

# Verify readiness
kubectl --context workload -n loopzero-system exec deploy/cloudflared -- \
  curl -s http://localhost:2000/ready

# Verify external access
curl -sk https://api.dev.loopzero.example/healthz

# Verify CF dashboard shows tunnel connected
echo "Check: https://dash.cloudflare.com → Zero Trust → Tunnels"
```

## Post-Incident
- [ ] Update incident log
- [ ] Root cause analysis if recurring
- [ ] Verify cloudflared replicas (should be 2 for HA)
- [ ] Check tunnel metrics endpoint (:2000/metrics) for error patterns
