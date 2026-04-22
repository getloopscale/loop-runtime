# PostgreSQL Disaster Recovery

**Owner:** Platform Team
**Last Tested:** (fill after first DR test)
**RTO Target:** < 30 minutes
**RPO Target:** < 1 hour (DO automatic daily backup + WAL)

---

## Prerequisites

- `doctl` CLI authenticated with platform account
- `psql` client installed
- Access to DO dashboard
- Pulumi state access (for connection string)

---

## Scenario 1: Managed PG Down (DigitalOcean)

### Detection
- Alert: `LoopZeroPostgresDown` fires
- Symptom: API returns 500s, `loopzero deploy` fails, `loopzero status` hangs

### Triage
```bash
# Check DO managed database status
doctl databases list
doctl databases get $DB_ID

# Check from inside cluster
kubectl exec -n platform-system deployment/loopzero-api -- wget -qO- http://localhost:8080/api/v1/health/deep | jq .
```

### Resolution
```bash
# Option A: Wait for DO auto-recovery (most common)
# DO managed PG has automatic failover for HA clusters
# Check DO Status Page: https://status.digitalocean.com/

# Option B: Restore from DO automatic backup
doctl databases backups list $DB_ID
doctl databases backups restore $DB_ID --backup-id $BACKUP_ID

# Option C: Create new DB from backup (if restore fails)
doctl databases create loopzero-restore --engine pg --version 16 --size db-s-2vcpu-4gb --region sfo3 --backup-restore $BACKUP_ID

# Update connection string in Pulumi
pulumi config set --secret DATABASE_URL "new-connection-string" --stack prod
pulumi up --stack prod --yes
```

### Verification
```bash
# Verify connectivity from API
kubectl exec -n platform-system deployment/loopzero-api -- wget -qO- http://localhost:8080/api/v1/health/deep | jq .postgres

# Verify data integrity
psql $DATABASE_URL -c "SELECT count(*) FROM tenants"
psql $DATABASE_URL -c "SELECT count(*) FROM builds WHERE status = 'RUNNING'"
```

---

## Scenario 2: Restore from pg_dump (Belt-and-Suspenders)

### When to use
- DO automatic backup corrupted or unavailable
- Need point-in-time data from pg_dump

### Restore procedure
```bash
# List available dumps in Spaces
aws s3 ls s3://loopzero-pg-backups/ --endpoint-url https://sfo3.digitaloceanspaces.com

# Download latest dump
aws s3 cp s3://loopzero-pg-backups/loopzero-$(date +%Y-%m-%d).sql.gz ./restore.sql.gz \
  --endpoint-url https://sfo3.digitaloceanspaces.com

# Decompress
gunzip restore.sql.gz

# Restore to new database
psql $NEW_DATABASE_URL < restore.sql

# Verify row counts
psql $NEW_DATABASE_URL -c "SELECT 'tenants', count(*) FROM tenants UNION ALL SELECT 'builds', count(*) FROM builds UNION ALL SELECT 'releases', count(*) FROM releases"
```

---

## Scenario 3: Schema Migration Issue

### When to use
- Migration failed mid-way, DB in inconsistent state

### Resolution
```bash
# Check migration version
psql $DATABASE_URL -c "SELECT * FROM schema_migrations"

# If dirty, fix manually:
psql $DATABASE_URL -c "UPDATE schema_migrations SET dirty = false WHERE version = $BROKEN_VERSION"

# Re-run migrations
kubectl exec -n platform-system deployment/loopzero-api -- /usr/local/bin/loopzero-api migrate
```

---

## Post-Incident
- [ ] Document RTO achieved
- [ ] Verify all tenants can deploy
- [ ] Verify audit log continuity
- [ ] Run `loopzero status` for each active project
- [ ] Update this runbook if steps changed
