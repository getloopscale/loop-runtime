# Runbook: Emergency RLS (Row-Level Security) Disable

## Severity / Priority

**P0 — Emergency.** Only use this procedure when RLS is actively causing a production outage. Common triggers: tenant data leaking across boundaries, or all queries returning empty result sets because `app.tenant_id` is not being set correctly.

## Symptoms

### Scenario A: Data Leak (tenant sees another tenant's data)

- User reports seeing builds, releases, or secrets belonging to another team.
- API logs show queries returning rows with mismatched `tenant_id`.
- The `app.tenant_id` session variable is being set to the wrong value or not set at all.

### Scenario B: Empty Results (all queries return zero rows)

- `loopzero status`, `loopzero rollback`, `loopzero logs` return no data for all tenants.
- API logs show successful queries with `0 rows returned`.
- The `current_setting('app.tenant_id', true)` returns `NULL` when it should return the tenant ID, AND the RLS policy `OR current_setting('app.tenant_id', true) IS NULL` branch is not working as expected.
- Build reconciliation loop (`FindActiveBuilds`) or reapers (`FindStaleBuilds`) return empty sets, stalling all pipeline processing.

### Scenario C: Performance Degradation from RLS

- All queries on RLS-enabled tables show dramatically increased execution time.
- `EXPLAIN ANALYZE` shows `Filter` steps with `current_setting()` calls dominating execution.
- See [Slow Queries runbook](./slow-queries.md) for diagnosis.

## Diagnosis

### 1. Confirm RLS is the cause

```bash
# Check which tables have RLS enabled
kubectl --context k3d-loopzero-mgmt -n loopzero-system exec -it deploy/postgresql -- \
  psql -U loopzero -d loopzero -c "
    SELECT relname, relrowsecurity, relforcerowsecurity
    FROM pg_class
    WHERE relname IN ('builds', 'releases', 'environment_targets', 'secrets', 'projects', 'tenant_members', 'audit_log', 'idempotency_keys')
    ORDER BY relname;
  "
```

Expected output (RLS active):

```
      relname       | relrowsecurity | relforcerowsecurity
--------------------+----------------+---------------------
 builds             | t              | t
 environment_targets| t              | t
 releases           | t              | t
 secrets            | t              | t
```

### 2. Check RLS policy definitions

```bash
kubectl --context k3d-loopzero-mgmt -n loopzero-system exec -it deploy/postgresql -- \
  psql -U loopzero -d loopzero -c "
    SELECT tablename, policyname, qual
    FROM pg_policies
    WHERE schemaname = 'public'
    ORDER BY tablename;
  "
```

### 3. Test RLS behavior directly

```bash
# With tenant context set (should return only that tenant's rows)
kubectl --context k3d-loopzero-mgmt -n loopzero-system exec -it deploy/postgresql -- \
  psql -U loopzero -d loopzero -c "
    BEGIN;
    SELECT set_config('app.tenant_id', '<tenant-id>', true);
    SELECT count(*), tenant_id FROM builds GROUP BY tenant_id;
    ROLLBACK;
  "

# Without tenant context (should return ALL rows — reconciliation mode)
kubectl --context k3d-loopzero-mgmt -n loopzero-system exec -it deploy/postgresql -- \
  psql -U loopzero -d loopzero -c "
    BEGIN;
    SELECT count(*), tenant_id FROM builds GROUP BY tenant_id;
    ROLLBACK;
  "
```

If the second query (no tenant context) returns 0 rows, RLS is broken and needs to be disabled.

## Mitigation Steps

### Step 1: Disable RLS immediately via direct SQL

**This is the emergency path. Use only when the migration path is too slow.**

```bash
kubectl --context k3d-loopzero-mgmt -n loopzero-system exec -it deploy/postgresql -- \
  psql -U loopzero -d loopzero -c "
    ALTER TABLE builds DISABLE ROW LEVEL SECURITY;
    ALTER TABLE releases DISABLE ROW LEVEL SECURITY;
    ALTER TABLE environment_targets DISABLE ROW LEVEL SECURITY;
    ALTER TABLE secrets DISABLE ROW LEVEL SECURITY;
  "
```

If additional tables have RLS enabled (check migration 000009 and any later migrations):

```bash
kubectl --context k3d-loopzero-mgmt -n loopzero-system exec -it deploy/postgresql -- \
  psql -U loopzero -d loopzero -c "
    ALTER TABLE projects DISABLE ROW LEVEL SECURITY;
    ALTER TABLE tenant_members DISABLE ROW LEVEL SECURITY;
    ALTER TABLE audit_log DISABLE ROW LEVEL SECURITY;
    ALTER TABLE idempotency_keys DISABLE ROW LEVEL SECURITY;
  "
```

### Step 2: Apply via the pre-written forward-fix migration (preferred path)

Migration `000010_disable_rls_emergency.up.sql` exists as a pre-written emergency migration. It is a NO-OP by default — the `ALTER TABLE ... DISABLE` statements are commented out.

1. Edit the migration file to uncomment the disable statements:

   File: `api/internal/db/migrations/000010_disable_rls_emergency.up.sql`

   Uncomment the `ALTER TABLE ... DISABLE ROW LEVEL SECURITY` lines and remove the `SELECT 1;` placeholder.

2. Apply the migration:
   ```bash
   task db:migrate
   ```

3. If the migration runner is not available, apply directly:
   ```bash
   kubectl --context k3d-loopzero-mgmt -n loopzero-system exec -it deploy/postgresql -- \
     psql -U loopzero -d loopzero -c "
       ALTER TABLE builds DISABLE ROW LEVEL SECURITY;
       ALTER TABLE releases DISABLE ROW LEVEL SECURITY;
       ALTER TABLE environment_targets DISABLE ROW LEVEL SECURITY;
       ALTER TABLE secrets DISABLE ROW LEVEL SECURITY;
       ALTER TABLE projects DISABLE ROW LEVEL SECURITY;
       ALTER TABLE tenant_members DISABLE ROW LEVEL SECURITY;
       ALTER TABLE audit_log DISABLE ROW LEVEL SECURITY;
       ALTER TABLE idempotency_keys DISABLE ROW LEVEL SECURITY;
     "
   ```

### Step 3: Verify the fix

See Verification section below.

### Step 4: Restart the API to clear connection state

```bash
kubectl --context k3d-loopzero-mgmt -n loopzero-system rollout restart deploy/loopzero-api
kubectl --context k3d-loopzero-mgmt -n loopzero-system rollout status deploy/loopzero-api
```

## Verification

```bash
# 1. Confirm RLS is disabled
kubectl --context k3d-loopzero-mgmt -n loopzero-system exec -it deploy/postgresql -- \
  psql -U loopzero -d loopzero -c "
    SELECT relname, relrowsecurity, relforcerowsecurity
    FROM pg_class
    WHERE relname IN ('builds', 'releases', 'environment_targets', 'secrets')
    ORDER BY relname;
  "
# All should show 'f' (false)

# 2. Functional verification
loopzero status
# Should return data (exit code 0)

# 3. Verify cross-tenant isolation is still maintained by application-level WHERE clauses
# (RLS was defense-in-depth; the API always includes tenant_id in queries)
kubectl --context k3d-loopzero-mgmt -n loopzero-system exec -it deploy/postgresql -- \
  psql -U loopzero -d loopzero -c "
    SELECT count(*) FROM builds;
  "
# Should return the total count across all tenants
```

## Re-Enable Procedure

After the root cause is identified and fixed, re-enable RLS with a new forward-only migration:

```sql
-- api/internal/db/migrations/000011_reenable_rls.up.sql
ALTER TABLE builds ENABLE ROW LEVEL SECURITY;
ALTER TABLE builds FORCE ROW LEVEL SECURITY;
ALTER TABLE releases ENABLE ROW LEVEL SECURITY;
ALTER TABLE releases FORCE ROW LEVEL SECURITY;
ALTER TABLE environment_targets ENABLE ROW LEVEL SECURITY;
ALTER TABLE environment_targets FORCE ROW LEVEL SECURITY;
ALTER TABLE secrets ENABLE ROW LEVEL SECURITY;
ALTER TABLE secrets FORCE ROW LEVEL SECURITY;
```

**Before re-enabling**, verify:

1. The `set_config('app.tenant_id', $1, true)` call in `api/internal/lock/environment_lock.go` is working correctly for all code paths.
2. Reconciliation and reaper queries (which intentionally do NOT set `app.tenant_id`) still return all rows with the `OR current_setting('app.tenant_id', true) IS NULL` policy branch.
3. Run integration tests that validate tenant isolation.

## Prevention

1. **RLS is defense-in-depth, not the primary isolation mechanism.** The API always includes `WHERE tenant_id = $1` in queries. RLS is a safety net.

2. **Test RLS behavior in integration tests.** Verify both the "tenant scoped" and "system-wide" (no tenant set) code paths.

3. **The `current_setting('app.tenant_id', true)` with `true` as the second argument** means it returns `NULL` (not an error) when the setting is not set. This is intentional — it allows system-wide queries (reconciliation, reapers) to see all rows.

4. **Never use `SET` (session-scoped) for `app.tenant_id`.** Always use `SET LOCAL` or `set_config(..., true)` which scopes to the current transaction. The lock implementation in `environment_lock.go` does this correctly.

## Related Alerts

| Alert | Condition |
|-------|-----------|
| `LoopZeroAPIDown` | API pod not responding to health checks |
| `LoopZeroDeployFailureRate` | Deploy failure rate > 20% in 15 min window |
| `PostgresPoolNearExhaustion` | Active connections > 80% of MaxConns for 2 min |
