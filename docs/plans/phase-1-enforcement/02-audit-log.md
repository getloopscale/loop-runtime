# Audit Log — Wire Existing Infrastructure

> Table exists. Adapter exists. Nothing writes to it.
> Estimated effort: 2-3 days.

---

## Problem

```
api/internal/db/audit.go:
  WriteAuditLog(ctx, db, AuditEntry) — EXISTS, never called from production code

api/internal/middleware/audit.go:
  AuditLog(logger) — EXISTS, applied at server.go:142
  BUT: only writes to slog (structured JSON stdout), NOT to audit_log table

Database:
  audit_log table — EXISTS (migration 000001), 0 rows in production
```

The middleware logs to stdout. The DB function exists but is never called. Two disconnected systems.

## Solution

Connect them: the middleware should call `WriteAuditLog()` for mutation operations, writing to both slog AND the audit_log table.

---

## Task 1: Enhance Audit Middleware to Write to DB

**File:** `api/internal/middleware/audit.go`

**Current behavior (line 26-45):**
```go
// After handler completes:
logger.Info("audit",
    "method", method,
    "path", path,
    "status", ww.Status(),
    // ...
)
```

**New behavior:**
```go
// After handler completes:
logger.Info("audit", /* same slog fields */)

// Also write to database for compliance trail
if db != nil && isMutation(method) {
    uc := GetUserContext(r.Context())
    entry := db.AuditEntry{
        TenantID:      extractTenantID(uc),
        Action:        deriveAction(method, path),  // e.g., "project.created"
        Actor:         extractUserID(uc),
        ResourceType:  extractResourceType(path),    // e.g., "project"
        ResourceID:    extractResourceID(path, body), // e.g., "my-app"
        CorrelationID: getCorrelationID(r),
        Metadata: map[string]any{
            "status": ww.Status(),
            "method": method,
            "path":   path,
        },
    }
    _ = db.WriteAuditLog(r.Context(), dbConn, entry) // fire-and-forget, don't block response
}
```

**Changes needed to middleware signature:**
```go
// Current:
func AuditLog(logger *slog.Logger) func(http.Handler) http.Handler

// New:
func AuditLog(logger *slog.Logger, dbConn db.ExecQueryExecutor) func(http.Handler) http.Handler
```

**DoD:**
- [ ] `AuditLog` middleware accepts DB connection parameter
- [ ] Mutations (POST/PUT/PATCH/DELETE) write to `audit_log` table
- [ ] GET/HEAD/OPTIONS do NOT write (read-only, too noisy)
- [ ] Fire-and-forget: DB write failure does not block HTTP response
- [ ] slog output unchanged (backward compatible)

---

## Task 2: Action Derivation from HTTP Method + Path

**New file:** `api/internal/middleware/audit_actions.go`

The audit log needs human-readable action names, not raw HTTP methods.

**Mapping:**
```go
func deriveAction(method, path string) string {
    // POST /api/v1/projects → project.created
    // DELETE /api/v1/projects/my-app → project.deleted
    // POST /api/v1/builds → build.submitted
    // POST /api/v1/teams/members → member.added
    // DELETE /api/v1/teams/members/paulo → member.removed
    // PATCH /api/v1/teams/members/paulo → member.role_changed
    // POST /api/v1/environments → environment.created
    // DELETE /api/v1/environments/staging → environment.deleted
    // POST /api/v1/secrets → secret.set
    // DELETE /api/v1/secrets/DB_URL → secret.deleted
    // POST /api/v1/projects/my-app/rollback → rollback.executed
}

func extractResourceType(path string) string {
    // /api/v1/projects/... → "project"
    // /api/v1/builds/... → "build"
    // /api/v1/teams/... → "member"
    // /api/v1/environments/... → "environment"
    // /api/v1/secrets/... → "secret"
}
```

**DoD:**
- [ ] `deriveAction()` maps all known mutation endpoints to actions
- [ ] `extractResourceType()` maps all known paths to resource types
- [ ] Unknown paths get action `unknown.mutation` (don't silently drop)
- [ ] Unit tests: each endpoint maps to correct action name

---

## Task 3: Wire DB Connection to Audit Middleware

**File:** `api/internal/server/server.go` (~line 142)

**Current:**
```go
r.Use(middleware.AuditLog(cfg.Logger))
```

**New:**
```go
r.Use(middleware.AuditLog(cfg.Logger, cfg.AuditDB))
```

**File:** `api/internal/server/server.go` — Config struct:
```go
type Config struct {
    // ... existing fields ...
    AuditDB  db.ExecQueryExecutor  // For audit_log table writes
}
```

**File:** `api/cmd/main.go`:
```go
srvCfg := server.Config{
    // ... existing fields ...
    AuditDB: dbAdapter,  // same DB connection as everything else
}
```

**DoD:**
- [ ] Audit middleware receives DB connection
- [ ] `go build ./api/...` succeeds
- [ ] Existing tests pass (backward compatible with nil DB)

---

## Task 4: Verify Audit Trail Works End-to-End

After Tasks 1-3 are wired, verify with real operations:

**Test sequence:**
```bash
LOOPZERO_URL=https://api.dev.loopzero.example

# 1. Create project
loopzero project create test-audit

# 2. Create secret
loopzero secret set DB_URL=postgres://...

# 3. Delete project
loopzero project delete test-audit --yes

# 4. Check audit log
kubectl exec -n platform-system deploy/loopzero-api -- \
  psql "$DATABASE_URL" -c \
  "SELECT action, resource_type, resource_id, actor, created_at FROM audit_log ORDER BY created_at DESC LIMIT 10;"
```

**Expected output:**
```
        action         | resource_type | resource_id  |        actor        |       created_at
-----------------------+---------------+--------------+---------------------+--------------------
 project.deleted       | project       | test-audit   | a1afb37b-12b9-...   | 2026-04-08 12:00:03
 secret.set            | secret        | DB_URL       | a1afb37b-12b9-...   | 2026-04-08 12:00:02
 project.created       | project       | test-audit   | a1afb37b-12b9-...   | 2026-04-08 12:00:01
```

**DoD:**
- [ ] Create project → audit_log row with `action=project.created`
- [ ] Set secret → audit_log row with `action=secret.set`
- [ ] Delete project → audit_log row with `action=project.deleted`
- [ ] All rows have correct `tenant_id`, `actor`, `correlation_id`
- [ ] Build submission → audit_log row with `action=build.submitted`
- [ ] Member invitation → audit_log row with `action=member.added`
- [ ] Rollback → audit_log row with `action=rollback.executed`

---

## Task 5: Expose Audit Log via API (Team+ tier)

**File:** New `api/internal/routes/audit.go`

**Endpoint:** `GET /api/v1/audit`

**Query params:**
- `?action=project.created` — filter by action
- `?resource_type=project` — filter by resource type
- `?since=2026-04-01T00:00:00Z` — filter by date
- `?limit=50&offset=0` — pagination

**Response:**
```json
{
  "entries": [
    {
      "action": "project.created",
      "resource_type": "project",
      "resource_id": "my-app",
      "actor": "a1afb37b-...",
      "actor_email": "paulo@example.com",
      "timestamp": "2026-04-08T12:00:01Z",
      "correlation_id": "req-abc123"
    }
  ],
  "total": 42,
  "limit": 50,
  "offset": 0
}
```

**Authorization:** Requires `owner` or `admin` role.

**DoD:**
- [ ] `GET /api/v1/audit` returns paginated audit entries
- [ ] Filtered by action, resource_type, date range
- [ ] Requires owner or admin role (member gets 403)
- [ ] Scoped to tenant (no cross-tenant leakage)
- [ ] Test: list audit after operations → returns correct entries

---

## Task 6: CLI Command — `loopzero audit`

**File:** New `cli/src/commands/audit.ts`

**Commands:**
- `loopzero audit list` — recent audit entries (default: last 24h)
- `loopzero audit list --action project.created` — filter by action
- `loopzero audit list --since 2026-04-01` — filter by date
- `loopzero audit list --json` — JSON output

**Output:**
```
Recent Activity (last 24h):
  12:00  project.created   project/my-app      paulo@example.com
  12:01  secret.set        secret/DB_URL       paulo@example.com
  12:02  build.submitted   build/bld-abc123    paulo@example.com
  12:05  project.deleted   project/my-app      paulo@example.com
```

**DoD:**
- [ ] `loopzero audit list` shows recent activity
- [ ] Filters work: `--action`, `--since`
- [ ] `--json` output
- [ ] Requires admin+ role

---

## Execution Order

```
Task 2: Action derivation helpers        — 1 hour
Task 1: Enhance audit middleware         — 2 hours
Task 3: Wire DB connection              — 30 min
Task 4: End-to-end verification         — 1 hour
Task 5: API endpoint                    — 2 hours
Task 6: CLI command                     — 2 hours

Total: ~9 hours (~1.5 days)
```

Dependencies:
- Task 2 before Task 1 (helpers needed by middleware)
- Task 3 after Task 1 (middleware signature changes first)
- Task 4 after Task 3 (needs everything wired)
- Tasks 5-6 after Task 4 (need audit data to exist)
