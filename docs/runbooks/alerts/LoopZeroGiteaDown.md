# LoopZeroGiteaDown

**Severity:** Critical
**Alert:** `up{job=~".*gitea.*"} == 0`
**Duration:** 2m

## Detection
- Alert fires in Prometheus/Grafana
- Slack notification via Alertmanager
- Builds cannot clone source code; GitOps pushes fail; new deploys blocked

## Triage (copy-paste commands)
```bash
# 1. Check Gitea pod status
kubectl --context mgmt -n loopzero-system get pods -l app.kubernetes.io/name=gitea
kubectl --context mgmt -n loopzero-system describe pod -l app.kubernetes.io/name=gitea

# 2. Check Gitea logs
kubectl --context mgmt -n loopzero-system logs -l app.kubernetes.io/name=gitea --tail=50

# 3. Check PVC (data loss indicator)
kubectl --context mgmt -n loopzero-system get pvc -l app.kubernetes.io/name=gitea

# 4. Check Gitea HTTP health
kubectl --context mgmt -n loopzero-system exec deploy/loopzero-api -- \
  curl -s http://gitea-http.loopzero-system:3000/api/v1/version

# 5. Check events for crash reasons
kubectl --context mgmt -n loopzero-system get events --sort-by=.lastTimestamp \
  | grep -i gitea | tail -10
```

## Resolution
1. **Pod in CrashLoopBackOff:**
   ```bash
   # Check OOM or resource limits
   kubectl --context mgmt -n loopzero-system describe pod -l app.kubernetes.io/name=gitea \
     | grep -A5 "Last State\|Limits\|OOMKilled"
   # Restart
   kubectl --context mgmt -n loopzero-system rollout restart deployment/gitea
   ```

2. **PVC not bound (data volume issue):**
   ```bash
   kubectl --context mgmt -n loopzero-system get pvc
   kubectl --context mgmt -n loopzero-system describe pvc <gitea-pvc>
   ```

3. **Redis session store down (if applicable):**
   ```bash
   kubectl --context mgmt -n loopzero-system get pods -l app=redis
   ```

4. **Data corruption — restore from backup (RPO: 6h):**
   ```bash
   # List available backups in MinIO/Spaces
   kubectl --context mgmt -n loopzero-system run mc-list --rm -i --restart=Never \
     --image=minio/mc:latest -- mc ls spaces/loopzero-artifacts/gitea-backups/ | tail -5
   ```

## Escalation
- If not resolved in 10 min: escalate to SRE (Gitea down = all deploys blocked)
- If data corruption: invoke Gitea backup restore procedure

## Verification
```bash
# Verify Gitea is responding
kubectl --context mgmt -n loopzero-system exec deploy/loopzero-api -- \
  curl -s http://gitea-http.loopzero-system:3000/api/v1/version | jq .

# Verify builds can clone
kubectl --context mgmt -n loopzero-system logs deploy/loopzero-api --tail=10 \
  | grep -i "gitea\|source"

# Verify Prometheus metric
curl -s 'http://localhost:9090/api/v1/query?query=up{job=~".*gitea.*"}' \
  | jq '.data.result[0].value[1]'
```

## Post-Incident
- [ ] Update incident log
- [ ] Root cause analysis if recurring
- [ ] Verify Gitea backup CronJob is healthy (see LoopZeroGiteaBackupStale)
