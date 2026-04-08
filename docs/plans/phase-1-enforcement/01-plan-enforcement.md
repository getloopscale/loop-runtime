# Plan Enforcement — Wire Limits to Routes

> Without this, Free = Pro = Team = Enterprise. No revenue possible.
> Estimated effort: 1-2 days.

---

## Problem

`PlanLimiter.CheckLimit()` exists in `cloud/internal/enforcement/limiter.go` with full logic (hard limits block, soft limits allow with overage flag). The middleware exists in `cloud/internal/enforcement/middleware.go` returning 402 Payment Required.

**But ZERO routes call it.** Builds, project creation, environment creation, member invitation — none check plan limits.

## Solution

Wire enforcement middleware to every mutation that has a plan limit.

## Tasks

### 1.1 Wire enforcement to build submission

**Files:**
- Edit: `api/internal/routes/builds.go` — check `builds_per_month` before accepting build
- Edit: `api/internal/server/server.go` — inject PlanLimiter dependency

**Implementation:**
- Before `submitBuild()`: call `limiter.CheckLimit(tenantID, "builds_per_month", currentBuildCount)`
- If `!result.Allowed`: return 402 with upgrade message from `FormatLimitError()`
- If `result.SoftOver`: log overage, allow build (Team plan behavior)
- Count current builds: `SELECT COUNT(*) FROM builds WHERE tenant_id = $1 AND created_at >= $2` (current month)

**DoD:**
- [ ] Build submission checks `builds_per_month` limit
- [ ] Starter user with 50+ builds gets 402 with "Upgrade to Pro"
- [ ] Pro user with 500+ builds gets 402 with "Upgrade to Team"
- [ ] Team user with soft limit exceeded gets build + overage logged
- [ ] Test: mock limiter returns Allowed=false → build returns 402

### 1.2 Wire enforcement to project creation

**Files:**
- Edit: `api/internal/routes/projects.go` — check `projects` limit before creating

**Implementation:**
- Before `createProject()`: count existing projects for tenant
- Call `limiter.CheckLimit(tenantID, "projects", existingCount)`
- If blocked: return 402 with upgrade message

**DoD:**
- [ ] Starter user with 1 project cannot create a 2nd
- [ ] Pro user with 3 projects cannot create a 4th
- [ ] Team user with 10 projects cannot create an 11th
- [ ] Error message: "Plan limit reached: Starter allows 1 project. Upgrade your plan."
- [ ] Test: create project at limit → 402

### 1.3 Wire enforcement to environment creation

**Files:**
- Edit: `api/internal/routes/environments.go` — check `environments` limit

**DoD:**
- [ ] Starter user limited to 1 environment
- [ ] Pro user limited to 3 environments
- [ ] Team user limited to 10 environments
- [ ] Test: create environment at limit → 402

### 1.4 Wire enforcement to team member invitation

**Files:**
- Edit: `api/internal/routes/teams.go` — check `seats` limit before adding member

**DoD:**
- [ ] Starter limited to 3 members
- [ ] Pro limited to 10 members
- [ ] Team limited to 25 members
- [ ] Test: add member at limit → 402

### 1.5 Update plan_limits seed data

**Files:**
- New: `api/internal/db/migrations/000023_update_plan_limits.up.sql`

**New limits (aligned with pricing tiers):**

| Plan | projects | environments | builds_per_month | seats |
|------|----------|-------------|-----------------|-------|
| starter | 1 | 1 | 50 | 3 |
| pro | 3 | 3 | 500 | 10 |
| team | 10 (soft: 15) | 10 | NULL (unlimited) | 25 |
| enterprise | NULL | NULL | NULL | NULL |

**DoD:**
- [ ] Migration updates plan_limits to match pricing tiers
- [ ] `projects` limit exists for all plans
- [ ] `seats` limit exists for all plans
- [ ] All existing tests pass
