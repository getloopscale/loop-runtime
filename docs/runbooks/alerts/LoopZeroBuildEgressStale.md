# LoopZeroBuildEgressStale

**Severity:** Critical
**Alert:** `loopzero_build_egress_connectivity == 0`
**Duration:** 5m

## Detection
- Alert fires in Prometheus/Grafana
- Slack notification via Alertmanager
- Build pods cannot reach Gitea, MinIO, or container registry
- Builds fail at clone, cache, or image push steps

## Triage (copy-paste commands)
```bash
# 1. Check DNS resolution from build namespace
kubectl --context build -n argo run dns-test --rm -i --restart=Never \
  --image=busybox:1.36 -- nslookup gitea-http.platform-system.svc.cluster.local

# 2. Check NetworkPolicy in build namespace
kubectl --context build -n argo get networkpolicy
kubectl --context build -n argo describe networkpolicy

# 3. Check Service endpoints for dependencies
kubectl --context build get endpoints -A | grep -E "gitea|minio|registry"

# 4. Check recent build failures (which step fails?)
kubectl --context build -n argo get wf --sort-by=.metadata.creationTimestamp \
  | grep -E "Failed|Error" | tail -5
FAILED_WF=$(kubectl --context build -n argo get wf --sort-by=.metadata.creationTimestamp -o name \
  | grep -v Running | tail -1)
kubectl --context build -n argo logs $FAILED_WF --tail=30

# 5. Check connectivity probe metric
curl -s 'http://localhost:9090/api/v1/query?query=loopzero_build_egress_connectivity' | jq .
```

## Resolution
1. **NetworkPolicy blocking egress:**
   ```bash
   # List policies and check egress rules
   kubectl --context build -n argo get networkpolicy -o yaml | grep -A20 "egress"
   # Verify required egress targets are allowed:
   # - Gitea (platform-system, port 3000)
   # - MinIO (platform-system, port 9000)
   # - Container registry (port 443)
   # - DNS (kube-system, port 53)
   ```

2. **DNS broken in build cluster:**
   ```bash
   kubectl --context build -n kube-system get pods -l k8s-app=kube-dns
   kubectl --context build -n kube-system rollout restart deployment/coredns
   ```

3. **Service endpoints empty (dependency pod down):**
   ```bash
   # If cross-cluster: verify ExternalName or headless service pointing to mgmt
   kubectl --context build get svc -A | grep -E "gitea|minio"
   ```

4. **Registry unreachable:**
   ```bash
   # For DO Container Registry
   kubectl --context build -n argo run registry-check --rm -i --restart=Never \
     --image=curlimages/curl:latest -- \
     curl -sk https://registry.digitalocean.com/v2/
   # Check registry credentials
   kubectl --context build get secrets -A | grep -i registry
   ```

## Escalation
- If not resolved in 10 min: escalate to SRE (builds blocked = deploys blocked)
- If NetworkPolicy change needed: verify with platform team before modifying

## Verification
```bash
# Verify DNS works
kubectl --context build -n argo run dns-verify --rm -i --restart=Never \
  --image=busybox:1.36 -- nslookup gitea-http.platform-system.svc.cluster.local

# Verify connectivity metric recovered
curl -s 'http://localhost:9090/api/v1/query?query=loopzero_build_egress_connectivity' \
  | jq '.data.result[0].value[1]'
# Should be "1"

# Submit a test build and verify it passes clone step
kubectl --context build -n argo get wf --sort-by=.metadata.creationTimestamp | tail -3
```

## Post-Incident
- [ ] Update incident log
- [ ] Root cause analysis if recurring
- [ ] Review NetworkPolicy rules for build namespace
