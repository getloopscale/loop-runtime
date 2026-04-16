# Runbook: PostgreSQL Credential Rotation

## Severity: High

## Overview

Procedure for rotating PostgreSQL role passwords via External Secrets Operator (ESO) with zero downtime. Requires the BeforeConnect hook in the pgx connection pool (Phase 2 of MEET-2026-03-31-024).

## Prerequisites

- BeforeConnect hook implemented in pgx pool (reads password from K8s Secret file mount)
- ESO configured to sync PG credentials from secret source
- Separate roles: loopzero_api, loopzero_migrate, loopzero_readonly (migration 000009)

## Symptoms (Rotation Failure)

- API pods logging FATAL: password authentication failed for user "loopzero_api"
- loopzero_postgres_connections_active drops to 0
- Health endpoint returns 503
- Pool exhaustion alerts fire (new connections fail, old ones drain)

## Rotation Procedure

### 1. Update password in secret source

```bash
pulumi config set --secret loopzero-api-db-password "new-password-here"
```

### 2. Verify ESO syncs the new secret

```bash
kubectl -n loopzero-system get externalsecret loopzero-api-db-credentials -o yaml
# Status should show: refreshTime updated, status: SecretSynced
```

### 3. Grace period (CRITICAL)

The OLD password must remain valid for at least MaxConnLifetime (1 hour) after the new password is set. Existing connections drain naturally via pool lifecycle.

### 4. Apply new password to PostgreSQL

```sql
ALTER ROLE loopzero_api PASSWORD 'new-password-here';
```

### 5. Verify new connections authenticate

```bash
kubectl -n loopzero-system logs deploy/loopzero-api --since=5m | grep -i "auth\|password\|FATAL"
```

Within MaxConnLifetime (1h), all connections will use new credentials.

## Verification

- No authentication errors in API logs
- loopzero_postgres_connections_active > 0 and stable
- Health endpoint returns 200
- Deploy flow works end-to-end

## Rollback

```sql
ALTER ROLE loopzero_api PASSWORD 'old-password-here';
```

Then restart API pods: `kubectl -n loopzero-system rollout restart deploy/loopzero-api`

## Related Alerts

- LoopZeroCredentialRotationFailure
- LoopZeroPoolExhaustionCritical
- LoopZeroPostgresDown
