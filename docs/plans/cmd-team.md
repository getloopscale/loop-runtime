# Execution Plan: `loopzero team` Subcommands

**Date:** 2026-03-31
**Status:** Draft
**Tier:** T2 (API contract + CLI output changes)
**Depends on:** Sprint 1 (auth + tenants), membership_store.go, auth middleware

---

## Open Question (Requires Decision)

**How does adding a member work?**

Two possible flows:

| Option | Flow | Pros | Cons |
|--------|------|------|------|
| **A: Add by GitHub username** | `loopzero team add octocat` inserts a row in `tenant_members` using `github-{username}` as user_id. The user authenticates on their next `loopzero login`. | Simple, no invitation flow, no state machine. Works today with existing auth. | User must exist in `users` table first (must have logged in at least once). If they never logged in, we need to decide: reject or create a stub user record? |
| **B: Invitation flow** | `loopzero team add octocat` creates a pending invitation. User sees it on `loopzero login` and accepts. | Explicit consent, professional UX. | Requires new `invitations` table, state machine (pending/accepted/expired), expiry logic, and notification mechanism. Significant scope. |

**Recommendation:** Option A for v1. The user must have logged in at least once (`users` table has their record). If the user doesn't exist, return a clear error: `"User 'octocat' has not signed up yet. They must run 'loopzero login' first."` This is YAGNI-compliant and matches the current auth model where `user_id = github-{username}`.

**This decision must be confirmed before implementation begins.**

---

## 1. `loopzero team list`

### Syntax

```
loopzero team list [--json]
```

No arguments. Lists members of the current team (resolved from auth context).

### Exit Codes

| Code | Condition |
|------|-----------|
| 0 | Success |
| 4 | Not authenticated |

### Human Output

```
Team: acme (3 members)

  USERNAME       ROLE      JOINED
  alice          owner     2026-01-15
  bob            admin     2026-02-01
  charlie        member    2026-03-10
```

### JSON Output (`--json`)

```json
{
  "team": "acme",
  "members": [
    {
      "username": "alice",
      "role": "owner",
      "joined_at": "2026-01-15T10:00:00Z"
    },
    {
      "username": "bob",
      "role": "admin",
      "joined_at": "2026-02-01T14:30:00Z"
    }
  ]
}
```

### API Endpoint

```
GET /api/v1/teams/:teamId/members
Authorization: Bearer <token>
```

**Response 200:**
```json
{
  "team": "acme",
  "members": [
    {
      "username": "alice",
      "role": "owner",
      "joined_at": "2026-01-15T10:00:00Z"
    }
  ]
}
```

**Response 403:** User is not a member of the team.

### Backend

**New store method:** `ListMembers(ctx context.Context, tenantID string) ([]TeamMember, error)`

```sql
SELECT u.id, u.display_name, tm.role, tm.created_at
FROM tenant_members tm
JOIN users u ON u.id = tm.user_id
WHERE tm.tenant_id = $1
ORDER BY tm.created_at ASC
```

**Authorization:** Any team member can list members (owner, admin, member).

### Tests

- CLI: success with human output, success with `--json`, not authenticated (exit 4)
- API: 200 with members, 403 for non-member, empty team (shouldn't happen but handle gracefully)
- Store: returns correct members, handles non-existent tenant

### DoD: `loopzero team list`

- [ ] `ListMembers` store method implemented and tested
- [ ] `GET /api/v1/teams/:teamId/members` handler implemented
- [ ] Authorization check: any team member can list (owner, admin, member)
- [ ] 403 returned for non-members
- [ ] `TeamListCommand` class implemented with `execute()`
- [ ] Human output matches format (table with USERNAME, ROLE, JOINED)
- [ ] `--json` output matches schema (team, members array)
- [ ] Exit code 0 on success
- [ ] Exit code 4 when not authenticated
- [ ] Zero internal terms in any string (`tenant_id`, `tenant_members`, `github-` prefix)
- [ ] CLI unit tests: success human, success `--json`, not authenticated (exit 4)
- [ ] API tests: 200 with members, 403 non-member, empty team
- [ ] Store tests: correct members returned, non-existent tenant handled
- [ ] `npm run build` passes
- [ ] `go test ./...` passes

---

## 2. `loopzero team add`

### Syntax

```
loopzero team add <github-username> [--role member|admin] [--json]
```

Default role: `member`. Cannot add with role `owner` (ownership transfer is a separate operation, out of scope).

### Exit Codes

| Code | Condition |
|------|-----------|
| 0 | Success (member added) |
| 1 | Usage error (missing username, invalid role) |
| 3 | Operation failed (user not found, already a member) |
| 4 | Not authenticated or not authorized |

### Human Output

**Success:**
```
Added alice to team acme as member.
```

**User not found:**
```
User 'octocat' has not signed up yet. They must run 'loopzero login' first.
```

**Already a member:**
```
User 'alice' is already a member of team acme.
```

**Not authorized:**
```
Only team owners and admins can add members. Your role: member.
```

### JSON Output (`--json`)

**Success:**
```json
{
  "username": "alice",
  "team": "acme",
  "role": "member",
  "added": true
}
```

**Error:**
```json
{
  "error": "USER_NOT_FOUND",
  "message": "User 'octocat' has not signed up yet. They must run 'loopzero login' first."
}
```

### API Endpoint

```
POST /api/v1/teams/:teamId/members
Authorization: Bearer <token>
Content-Type: application/json

{
  "username": "alice",
  "role": "member"
}
```

**Response 201:** Member added.
```json
{
  "username": "alice",
  "team": "acme",
  "role": "member",
  "joined_at": "2026-03-31T12:00:00Z"
}
```

**Response 400:** Invalid role value.
**Response 404:** User not found in `users` table.
**Response 403:** Caller is not owner or admin.
**Response 409:** User is already a member (PK conflict on `tenant_members`).

### Backend

**New store methods:**

- `GetUserByUsername(ctx context.Context, username string) (*User, error)` -- queries `users` table by `id = 'github-{username}'`
- `AddMember(ctx context.Context, tenantID, userID, role string) error` -- inserts into `tenant_members`
- `GetMember(ctx context.Context, tenantID, userID string) (*TeamMember, error)` -- checks if already a member

**Authorization:** Only `owner` or `admin` can add members. The `role` field in `UserContext` is checked in the route handler. Cannot assign `owner` role via this endpoint.

**Validation:**
- `role` must be `member` or `admin` (not `owner`)
- `username` must match an existing user in `users` table
- User must not already be a member

### Tests

- CLI: success, `--json`, missing username (exit 1), invalid role (exit 1), not authenticated (exit 4), not authorized (exit 4), user not found (exit 3), already member (exit 3)
- API: 201 created, 400 invalid role, 403 not owner/admin, 404 user not found, 409 already member
- Store: insert succeeds, PK conflict detected, user lookup by github username

### DoD: `loopzero team add`

- [ ] `GetUserByUsername` store method implemented and tested
- [ ] `AddMember` store method implemented and tested
- [ ] `GetMember` store method implemented and tested (membership check)
- [ ] `POST /api/v1/teams/:teamId/members` handler implemented
- [ ] Authorization check: only `owner` or `admin` can add members
- [ ] Validation: `role` must be `member` or `admin` (not `owner`)
- [ ] Validation: `username` must match existing user in `users` table
- [ ] Validation: user must not already be a member (409)
- [ ] 201 on success, 400 invalid role, 403 not authorized, 404 user not found, 409 already member
- [ ] `TeamAddCommand` class implemented with `execute()`
- [ ] Human output: success, user not found, already a member, not authorized
- [ ] `--json` output for all scenarios (success + all errors)
- [ ] Exit code 0 on success
- [ ] Exit code 1 for missing username or invalid role
- [ ] Exit code 3 for user not found or already member
- [ ] Exit code 4 for not authenticated or not authorized
- [ ] `github-` prefix stripped from all user-facing output
- [ ] Zero internal terms in any string
- [ ] CLI unit tests: 8 scenarios (success, `--json`, missing username, invalid role, not authenticated, not authorized, user not found, already member)
- [ ] API tests: 201, 400, 403, 404, 409
- [ ] Store tests: insert, PK conflict, user lookup by github username
- [ ] `npm run build` passes
- [ ] `go test ./...` passes

---

## 3. `loopzero team remove`

### Syntax

```
loopzero team remove <github-username> [--yes] [--json]
```

`--yes` skips confirmation prompt.

### Exit Codes

| Code | Condition |
|------|-----------|
| 0 | Success (member removed) |
| 1 | Usage error (missing username, trying to remove owner) |
| 3 | Operation failed (user not a member) |
| 4 | Not authenticated or not authorized |

### Human Output

**Confirmation prompt (unless `--yes`):**
```
Remove alice from team acme? This will revoke all access. [y/N]
```

**Success:**
```
Removed alice from team acme.
```

**Cannot remove owner:**
```
Cannot remove the team owner. Transfer ownership first.
```

**Not a member:**
```
User 'alice' is not a member of team acme.
```

### JSON Output (`--json`)

```json
{
  "username": "alice",
  "team": "acme",
  "removed": true
}
```

### API Endpoint

```
DELETE /api/v1/teams/:teamId/members/:userId
Authorization: Bearer <token>
```

Where `:userId` is `github-{username}`.

**Response 200:** Member removed.
```json
{
  "username": "alice",
  "removed": true
}
```

**Response 400:** Cannot remove the team owner.
**Response 403:** Caller is not owner or admin.
**Response 404:** User is not a member.

### Backend

**New store method:** `RemoveMember(ctx context.Context, tenantID, userID string) error` -- deletes from `tenant_members`.

**Authorization:** Only `owner` or `admin` can remove members. Additional rules:
- **Nobody can remove the owner.** The owner row in `tenant_members` has `role = 'owner'`. Check before delete.
- **Admins cannot remove other admins.** Only the owner can remove admins.
- **Members cannot remove anyone** (already blocked by role check).

**Self-removal:** An admin or member can remove themselves (leave the team). This is allowed even though they couldn't remove others of their rank. The owner cannot self-remove.

### Tests

- CLI: success, `--json`, `--yes` skips prompt, missing username (exit 1), not authenticated (exit 4), not authorized (exit 4), user not member (exit 3), cannot remove owner (exit 1)
- API: 200 removed, 400 cannot remove owner, 403 not authorized, 404 not member, admin cannot remove admin
- Store: delete succeeds, handles non-existent membership

### DoD: `loopzero team remove`

- [ ] `RemoveMember` store method implemented and tested
- [ ] `DELETE /api/v1/teams/:teamId/members/:userId` handler implemented
- [ ] Authorization check: only `owner` or `admin` can remove members
- [ ] Authorization check: admins cannot remove other admins (only owner can)
- [ ] Self-removal: admin/member can remove themselves, owner cannot
- [ ] Owner protection: nobody can remove the owner (400)
- [ ] 200 on success, 400 cannot remove owner, 403 not authorized, 404 not member
- [ ] `TeamRemoveCommand` class implemented with `execute()`
- [ ] Confirmation prompt implemented (unless `--yes`)
- [ ] Human output: success, cannot remove owner, not a member, not authorized
- [ ] `--json` output for all scenarios
- [ ] Exit code 0 on success
- [ ] Exit code 1 for missing username or trying to remove owner
- [ ] Exit code 3 for user not a member
- [ ] Exit code 4 for not authenticated or not authorized
- [ ] Zero internal terms in any string
- [ ] CLI unit tests: 8 scenarios (success, `--json`, `--yes` skips prompt, missing username, not authenticated, not authorized, user not member, cannot remove owner)
- [ ] API tests: 200, 400 (owner), 403, 404, admin cannot remove admin
- [ ] Store tests: delete succeeds, non-existent membership handled
- [ ] `npm run build` passes
- [ ] `go test ./...` passes

---

## 4. `loopzero team role`

### Syntax

```
loopzero team role <github-username> <member|admin> [--json]
```

Changes a member's role. Cannot change to/from `owner` (ownership transfer is out of scope).

### Exit Codes

| Code | Condition |
|------|-----------|
| 0 | Success (role changed) |
| 1 | Usage error (missing args, invalid role, cannot change owner role) |
| 3 | Operation failed (user not a member) |
| 4 | Not authenticated or not authorized |

### Human Output

**Success:**
```
Changed alice's role to admin in team acme.
```

**Cannot change owner's role:**
```
Cannot change the owner's role. Transfer ownership first.
```

**Same role:**
```
User 'alice' already has role admin in team acme.
```

### JSON Output (`--json`)

```json
{
  "username": "alice",
  "team": "acme",
  "previous_role": "member",
  "role": "admin",
  "updated": true
}
```

### API Endpoint

```
PATCH /api/v1/teams/:teamId/members/:userId
Authorization: Bearer <token>
Content-Type: application/json

{
  "role": "admin"
}
```

Where `:userId` is `github-{username}`.

**Response 200:** Role updated.
```json
{
  "username": "alice",
  "team": "acme",
  "previous_role": "member",
  "role": "admin"
}
```

**Response 400:** Invalid role, or target is the owner.
**Response 403:** Caller is not the owner.
**Response 404:** User is not a member.

### Backend

**New store method:** `UpdateMemberRole(ctx context.Context, tenantID, userID, newRole string) (previousRole string, err error)` -- updates `tenant_members` and returns the old role.

**Authorization:** **Only the owner can change roles.** Admins cannot promote/demote other members. This is more restrictive than add/remove (where admins can act) because role changes affect authorization boundaries.

**Validation:**
- Target role must be `member` or `admin` (not `owner`)
- Target user must not be the owner
- Target user must be a member
- No-op if role is already the requested value (return success with `updated: false`)

### Tests

- CLI: success, `--json`, missing args (exit 1), invalid role (exit 1), not authenticated (exit 4), not owner (exit 4), target not member (exit 3), cannot change owner (exit 1), same role (exit 0)
- API: 200 updated, 400 invalid role, 400 target is owner, 403 not owner, 404 not member
- Store: update succeeds, returns previous role, handles non-existent membership

### DoD: `loopzero team role`

- [ ] `UpdateMemberRole` store method implemented and tested (returns previous role)
- [ ] `PATCH /api/v1/teams/:teamId/members/:userId` handler implemented
- [ ] Authorization check: only `owner` can change roles
- [ ] Validation: target role must be `member` or `admin` (not `owner`)
- [ ] Validation: target user must not be the owner (400)
- [ ] Validation: target user must be a member (404)
- [ ] No-op if role is already the requested value (return success with `updated: false`)
- [ ] 200 on success, 400 invalid role, 400 target is owner, 403 not owner, 404 not member
- [ ] `TeamRoleCommand` class implemented with `execute()`
- [ ] Human output: success, cannot change owner, same role, not authorized
- [ ] `--json` output with `previous_role` and `role` fields for all scenarios
- [ ] Exit code 0 on success (including same-role no-op)
- [ ] Exit code 1 for missing args, invalid role, or cannot change owner
- [ ] Exit code 3 for target not a member
- [ ] Exit code 4 for not authenticated or not owner
- [ ] Zero internal terms in any string
- [ ] CLI unit tests: 9 scenarios (success, `--json`, missing args, invalid role, not authenticated, not owner, target not member, cannot change owner, same role)
- [ ] API tests: 200, 400 (invalid role), 400 (target is owner), 403, 404
- [ ] Store tests: update succeeds with previous role returned, non-existent membership handled
- [ ] `npm run build` passes
- [ ] `go test ./...` passes

---

## Implementation Order

| Step | Component | Description | Depends on |
|------|-----------|-------------|------------|
| 1 | API store | New methods: `ListMembers`, `GetUserByUsername`, `GetMember`, `AddMember`, `RemoveMember`, `UpdateMemberRole` | Existing `membership_store.go` |
| 2 | API store tests | Unit tests for all 6 new store methods | Step 1 |
| 3 | API routes | New file `teams.go` with 4 handlers + authorization checks | Steps 1-2 |
| 4 | API route tests | Tests for all endpoints (success + error paths + authorization) | Step 3 |
| 5 | API wiring | Register routes in `main.go` / server setup | Steps 3-4 |
| 6 | CLI commands | New file `team.ts` with 4 subcommands + deps interface | Steps 3-5 |
| 7 | CLI tests | Unit tests for all subcommands (all exit codes + JSON shapes) | Step 6 |
| 8 | CLI wiring | Register `team` command in Commander setup | Steps 6-7 |

---

## Rule 3 Compliance

| Internal term | User-facing replacement |
|---------------|----------------------|
| `tenant_id` | Never exposed. Route uses `:teamId` but internally resolved from auth context. |
| `tenant` | `team` everywhere in CLI output and JSON responses. |
| `user_id` | `username` in CLI and API responses. Internal `github-{username}` format is never shown. |
| `tenant_members` | Never mentioned. Table is an implementation detail. |

The API endpoint uses `/teams/` not `/tenants/`. The `UserContext.TenantID` is used internally to resolve the team but is never exposed in responses -- instead the team slug (e.g., "acme") is returned as `team`.

---

## Data Model Notes

Existing schema (no migration needed for v1):

```sql
-- tenant_members (PK: tenant_id, user_id)
-- roles: owner, admin, member
-- owner is set during loopzero init (tenant creation)
```

The `username` in CLI maps to `user_id = 'github-{username}'` in the database. The `github-` prefix is an internal convention and must never appear in user-facing output. Strip it when returning data.

The `users` table `id` field uses format `github-{username}`. The `display_name` field exists but may be empty. Use the username (stripped of `github-` prefix) as the display value when `display_name` is empty.

---

## Authorization Matrix

| Action | Owner | Admin | Member |
|--------|-------|-------|--------|
| `team list` | Yes | Yes | Yes |
| `team add` | Yes | Yes | No |
| `team remove` (member) | Yes | Yes | No |
| `team remove` (admin) | Yes | No | No |
| `team remove` (self) | No (owner can't leave) | Yes | Yes |
| `team role` | Yes | No | No |

---

## Files to Create/Modify

### New files

| File | Description |
|------|-------------|
| `api/internal/routes/teams.go` | 4 HTTP handlers + authorization logic |
| `api/tests/unit/teams_handler_test.go` | Route handler tests |
| `cli/src/commands/team.ts` | 4 subcommand classes (TeamListCommand, TeamAddCommand, TeamRemoveCommand, TeamRoleCommand) |
| `cli/tests/unit/commands/team.test.ts` | CLI unit tests |

### Modified files

| File | Change |
|------|--------|
| `api/internal/db/membership_store.go` | Add 6 new methods to `PostgresMembershipStore` |
| `api/tests/unit/membership_store_test.go` | Tests for new store methods |
| `api/cmd/main.go` (or server setup) | Register `/api/v1/teams/` routes |
| `cli/src/index.ts` (or command registration) | Register `team` subcommand |
| `CHANGELOG.md` | Entry under `[Unreleased] > Added` |

---

## Gate: Complete

All items below must be checked before this plan is considered DONE.

### Store Layer
- [ ] `ListMembers` — implemented, tested, passing
- [ ] `GetUserByUsername` — implemented, tested, passing
- [ ] `GetMember` — implemented, tested, passing
- [ ] `AddMember` — implemented, tested, passing
- [ ] `RemoveMember` — implemented, tested, passing
- [ ] `UpdateMemberRole` — implemented, tested, passing

### API Layer
- [ ] `GET /api/v1/teams/:teamId/members` — implemented, tested, wired
- [ ] `POST /api/v1/teams/:teamId/members` — implemented, tested, wired
- [ ] `DELETE /api/v1/teams/:teamId/members/:userId` — implemented, tested, wired
- [ ] `PATCH /api/v1/teams/:teamId/members/:userId` — implemented, tested, wired
- [ ] All routes registered in server setup (`main.go` or equivalent)

### Authorization
- [ ] `team list` — any member can access
- [ ] `team add` — only owner/admin
- [ ] `team remove` (member) — only owner/admin
- [ ] `team remove` (admin) — only owner
- [ ] `team remove` (self) — admin/member can self-remove, owner cannot
- [ ] `team role` — only owner
- [ ] Owner cannot be removed via any path
- [ ] Owner role cannot be changed via any path

### CLI Layer
- [ ] `TeamListCommand` — implemented, tested, wired
- [ ] `TeamAddCommand` — implemented, tested, wired
- [ ] `TeamRemoveCommand` — implemented, tested, wired (with `--yes` prompt skip)
- [ ] `TeamRoleCommand` — implemented, tested, wired
- [ ] `team` subcommand registered in Commander setup
- [ ] `loopzero team --help` shows all subcommands

### Cross-Cutting
- [ ] All exit codes follow Rule 4 (0, 1, 3, 4 only)
- [ ] All `--json` output follows spec §0.3 stability contract
- [ ] Zero internal terms in CLI output (Rule 3: no `tenant_id`, `tenant_members`, `github-`, `tenant`)
- [ ] `github-` prefix stripped from all user-facing responses
- [ ] All human output uses `team` not `tenant`
- [ ] `npm run build` passes
- [ ] `npm test` passes (all CLI tests)
- [ ] `go test ./...` passes (all API + store tests)
- [ ] `task lint` passes
- [ ] CHANGELOG.md updated under `[Unreleased] > Added`
