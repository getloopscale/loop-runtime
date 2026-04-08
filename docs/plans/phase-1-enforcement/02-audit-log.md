# Audit Log — Wire Existing Table

> Table exists, adapter exists, nothing writes to it.
> Estimated effort: 2-3 days.

---

## Problem

`audit_log` table exists since migration 000001. `WriteAuditLog()` function exists in `api/internal/db/audit.go`. `AuditAdapter` exists. But `AuditLog` middleware in `api/internal/middleware/` only logs to slog — it never writes to the database.

The table has 0 rows in production (confirmed: `SELECT COUNT(*) FROM audit_log = 11` were from manual testing, now truncated to 0).

## Solution

Wire `AuditAdapter` to write mutation events to the `audit_log` table for every state-changing operation.

## Tasks

### 2.1 Wire AuditAdapter in API server

**Files:**
- Edit: `api/internal/server/server.go` — inject AuditAdapter as middleware or post-hook
- Edit: `api/cmd/main.go` — create AuditAdapter with DB connection

**What to audit:**
- `project.created`, `project.deleted`
- `build.submitted`, `build.cancelled`
- `environment.created`, `environment.deleted`
- `secret.set`, `secret.deleted`
- `member.added`, `member.removed`, `member.role_changed`
- `rollback.executed`

**DoD:**
- [ ] Every mutation endpoint writes to `audit_log` table
- [ ] Audit record includes: `tenant_id`, `user_id`, `action`, `resource_type`, `resource_id`, `timestamp`
- [ ] `SELECT * FROM audit_log` shows entries after CLI operations
- [ ] Test: create project → audit_log has entry with `action=project.created`

### 2.2 Wire AuditAdapter in Cloud service

**Files:**
- Edit: `cloud/cmd/main.go` — audit billing and auth events

**What to audit:**
- `billing.checkout_started`
- `billing.plan_changed` (from webhook)
- `member.invited`

**DoD:**
- [ ] Billing mutations write to audit_log
- [ ] Test: checkout webhook → audit_log has `billing.plan_changed`

### 2.3 Expose audit log via API

**Files:**
- New or edit: `api/internal/routes/audit.go` — GET /api/v1/audit

**DoD:**
- [ ] `GET /api/v1/audit` returns paginated audit entries for tenant
- [ ] Filtered by `resource_type`, `action`, date range
- [ ] Requires `owner` or `admin` role
- [ ] Test: list audit → returns recent entries
