# Pulumi: Role Separation Configuration

MEET-2026-03-31-024 Phase 1 Task A.6-A.9

## Current State

The API connects to PostgreSQL using the DigitalOcean default admin user
(from cluster.user/cluster.password). This user has full DDL access.

## Target State

After migration 000009 creates the roles:
- **API runtime:** connects as `loopzero_api` (DML only, no DDL)
- **Migration runner:** continues using the admin user (DDL required)

## Implementation Steps

### 1. Set loopzero_api password via Pulumi secrets

```bash
pulumi config set --secret loopzero-api-db-password "<strong-password>"
```

### 2. Update migration 000009 password

The migration currently uses placeholder `loopzero-api-change-me`.
Before running in production, update the password in the migration or run:

```sql
ALTER ROLE loopzero_api PASSWORD '<password-from-pulumi-secret>';
```

### 3. Update mgmt-bootstrap.ts

Change `installLoopZeroAPI()` to construct two DATABASE_URLs:

```typescript
// Runtime (DML only — loopzero_api role)
const apiDatabaseUrl = pulumi.interpolate`postgres://loopzero_api:${config.loopzeroApiDbPassword}@${infra.database.privateHost}:${infra.database.port}/loopzero?sslmode=require`;

// Migration (DDL — admin role, used only by migration init container)
const migrateDatabaseUrl = pulumi.interpolate`postgres://${infra.database.user}:${infra.database.password}@${infra.database.privateHost}:${infra.database.port}/loopzero?sslmode=require`;
```

### 4. Update the API Secret

```typescript
stringData: {
  DATABASE_URL: apiDatabaseUrl,        // loopzero_api role (DML)
  MIGRATE_DATABASE_URL: migrateDatabaseUrl, // admin role (DDL)
}
```

### 5. Verify on DigitalOcean

- [ ] loopzero_api can SELECT, INSERT, UPDATE, DELETE
- [ ] loopzero_api CANNOT execute DDL (ALTER TABLE, TRUNCATE)
- [ ] Migrations still run successfully with admin user
- [ ] API starts and functions normally with loopzero_api

## Risks

- DigitalOcean managed PG may restrict CREATE ROLE (verify first)
- If DO does not allow custom roles, use DatabaseUser resource from DO API
- Password must be rotated after initial creation (Phase 2 credential rotation)
