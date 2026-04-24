# LoopZeroPostgresBackupStale

**Severity:** Critical
**Alert:** `time() - pg_last_backup_timestamp > 90000`
**Duration:** 0m (fires immediately when stale >25h)

## Detection
- Alert fires in Prometheus/Grafana
- Slack notification via Alertmanager
- Data loss risk: if PG fails now, RPO is exceeded

## Triage (copy-paste commands)
```bash
# 1. Check backup CronJob status
kubectl --context mgmt -n loopzero-system get cronjobs | grep -i backup
kubectl --context mgmt -n loopzero-system get jobs --sort-by=.metadata.creationTimestamp | tail -5

# 2. Check last backup job logs
LAST_JOB=$(kubectl --context mgmt -n loopzero-system get jobs --sort-by=.metadata.creationTimestamp -o name | tail -1)
kubectl --context mgmt -n loopzero-system logs $LAST_JOB --tail=50

# 3. Check Prometheus metric directly
curl -s 'http://localhost:9090/api/v1/query?query=pg_last_backup_timestamp' | jq .

# 4. For DO managed PG: check automated backups
doctl databases backups list <db-id>
```

## Resolution
1. **CronJob suspended or misconfigured:**
   ```bash
   kubectl --context mgmt -n loopzero-system get cronjob <backup-cronjob> -o yaml | grep -A5 "suspend\|schedule"
   # Resume if suspended
   kubectl --context mgmt -n loopzero-system patch cronjob <backup-cronjob> -p '{"spec":{"suspend":false}}'
   ```

2. **S3/Spaces upload failing:**
   ```bash
   # Check MinIO/Spaces connectivity from backup pod
   kubectl --context mgmt -n loopzero-system run s3-check --rm -i --restart=Never \
     --image=minio/mc:latest -- mc alias set spaces $S3_ENDPOINT $S3_KEY $S3_SECRET
   ```

3. **Trigger manual backup (DO managed):**
   ```bash
   doctl databases backups create <db-id>
   ```

4. **Trigger manual pg_dump:**
   ```bash
   kubectl --context mgmt -n loopzero-system run manual-backup --rm -i --restart=Never \
     --image=postgres:16-alpine -- \
     pg_dump "$DATABASE_URL" --format=custom -f /tmp/loopzero-backup-$(date +%Y%m%d).dump
   ```

## Escalation
- If not resolved in 15 min: escalate to SRE
- If DO managed PG and no automated backups visible: open DO support ticket

## Verification
```bash
# Verify backup completed
kubectl --context mgmt -n loopzero-system get jobs --sort-by=.metadata.creationTimestamp | tail -3

# Verify metric updated
curl -s 'http://localhost:9090/api/v1/query?query=time()-pg_last_backup_timestamp' \
  | jq '.data.result[0].value[1]'
# Should be < 90000 (25h in seconds)
```

## Post-Incident
- [ ] Update incident log
- [ ] Root cause analysis if recurring
- [ ] Verify CronJob schedule aligns with RPO requirements
