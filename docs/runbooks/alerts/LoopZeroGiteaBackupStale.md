# LoopZeroGiteaBackupStale

**Severity:** Critical
**Alert:** `time() - loopzero_gitea_last_backup_timestamp > 25200`
**Duration:** 0m (fires immediately when stale >7h)

## Detection
- Alert fires in Prometheus/Grafana
- Slack notification via Alertmanager
- RPO exceeded: if Gitea data is lost now, source and gitops repos are unrecoverable beyond 7h

## Triage (copy-paste commands)
```bash
# 1. Check Gitea backup CronJob
kubectl --context mgmt -n loopzero-system get cronjobs | grep -i gitea
kubectl --context mgmt -n loopzero-system get jobs --sort-by=.metadata.creationTimestamp \
  | grep -i gitea | tail -5

# 2. Check last backup job logs
LAST_JOB=$(kubectl --context mgmt -n loopzero-system get jobs --sort-by=.metadata.creationTimestamp -o name | grep gitea | tail -1)
kubectl --context mgmt -n loopzero-system logs $LAST_JOB --tail=50

# 3. Check S3/MinIO connectivity
kubectl --context mgmt -n loopzero-system run mc-check --rm -i --restart=Never \
  --image=minio/mc:latest -- mc ls spaces/loopzero-artifacts/gitea-backups/ | tail -5

# 4. Check metric
curl -s 'http://localhost:9090/api/v1/query?query=loopzero_gitea_last_backup_timestamp' | jq .
```

## Resolution
1. **CronJob not running:**
   ```bash
   # Check schedule and suspend status
   kubectl --context mgmt -n loopzero-system get cronjob <gitea-backup> -o yaml \
     | grep -A5 "schedule\|suspend"
   # Resume if suspended
   kubectl --context mgmt -n loopzero-system patch cronjob <gitea-backup> \
     -p '{"spec":{"suspend":false}}'
   ```

2. **S3 upload failing:**
   ```bash
   # Verify MinIO/Spaces credentials
   kubectl --context mgmt -n loopzero-system get secret -l app=gitea-backup -o yaml
   # Test upload
   kubectl --context mgmt -n loopzero-system run mc-test --rm -i --restart=Never \
     --image=minio/mc:latest -- mc cp /dev/null spaces/loopzero-artifacts/gitea-backups/test-connectivity
   ```

3. **Trigger manual git bundle backup:**
   ```bash
   # Create manual job from CronJob
   kubectl --context mgmt -n loopzero-system create job manual-gitea-backup \
     --from=cronjob/<gitea-backup-cronjob>
   # Watch completion
   kubectl --context mgmt -n loopzero-system wait --for=condition=complete job/manual-gitea-backup --timeout=300s
   ```

## Escalation
- If not resolved in 15 min: escalate to SRE

## Verification
```bash
# Verify backup completed
kubectl --context mgmt -n loopzero-system get jobs --sort-by=.metadata.creationTimestamp \
  | grep gitea | tail -3

# Verify S3 has recent backup
kubectl --context mgmt -n loopzero-system run mc-verify --rm -i --restart=Never \
  --image=minio/mc:latest -- mc ls --sort-by time spaces/loopzero-artifacts/gitea-backups/ | tail -3

# Verify metric
curl -s 'http://localhost:9090/api/v1/query?query=time()-loopzero_gitea_last_backup_timestamp' \
  | jq '.data.result[0].value[1]'
# Should be < 25200 (7h in seconds)
```

## Post-Incident
- [ ] Update incident log
- [ ] Root cause analysis if recurring
- [ ] Verify S3 bucket lifecycle/retention policies
