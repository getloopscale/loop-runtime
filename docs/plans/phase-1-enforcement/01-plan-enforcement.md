# Plan Enforcement — Wire Limits to Routes

> Without this, Free = Pro = Team = Enterprise. No revenue possible.
> Estimated effort: 1-2 days.

---

## Problem

`PlanLimiter.CheckLimit()` exists with full logic. Enforcement `Middleware()` exists with
`OperationCheck` pattern (metric + CountFn closure). Tests exist proving it returns 402.

**ZERO routes use it.** The middleware is never registered in `server.go`.

## Architecture (Already Built)

```
cloud/internal/enforcement/limiter.go
  PlanLimiter.CheckLimit(tenantID, metric, currentCount) → LimitResult
    ├─ Enterprise → always Allowed
    ├─ Team + soft_limit exceeded → Allowed + SoftOver=true
    ├─ hard_limit exceeded → Allowed=false (402)
    └─ NULL limit → Allowed (unlimited)

cloud/internal/enforcement/middleware.go
  Middleware(limiter, check, tenantExtractor, upgradeURL) → chi middleware
    ├─ OperationCheck { Metric: "projects", CountFn: func(tenantID) int }
    ├─ Calls limiter.CheckLimit(tenantID, check.Metric, check.CountFn(tenantID))
    └─ If !Allowed → 402 with FormatLimitError()
```

## What Needs to Happen

1. Create `PlanLimiter` in `api/cmd/main.go` (after DB connection, ~line 186)
2. Pass it to `server.Config`
3. Build enforcement middleware per route in `server.NewRouter()`
4. Each middleware needs a `CountFn` that queries the DB
5. Apply to POST routes: builds, projects, environments, members

---

## Task 1: Update plan_limits Seed Data

New migration aligned with pricing tiers from `docs/strategy/competitive-analysis-and-pricing.md`.

**File:** `api/internal/db/migrations/000023_update_plan_limits_tiers.up.sql`

```sql
-- Delete old seed and re-insert with new tier values
DELETE FROM plan_limits;

INSERT INTO plan_limits (plan, metric, hard_limit, soft_limit) VALUES
  -- Starter (Free): 1 project, 1 env, 50 builds, 3 seats
  ('starter', 'projects', 1, NULL),
  ('starter', 'environments', 1, NULL),
  ('starter', 'builds_per_month', 50, NULL),
  ('starter', 'seats', 3, NULL),
  ('starter', 'services_per_project', 3, NULL),
  ('starter', 'secrets_per_env', 10, NULL),
  ('starter', 'ram_mb', 512, NULL),
  ('starter', 'vcpu_millicores', 500, NULL),
  ('starter', 'build_timeout_seconds', 600, NULL),
  ('starter', 'api_requests_per_min', 60, NULL),

  -- Pro ($29/mo): 3 projects, 3 envs, 500 builds, 10 seats
  ('pro', 'projects', 3, NULL),
  ('pro', 'environments', 3, NULL),
  ('pro', 'builds_per_month', 500, NULL),
  ('pro', 'seats', 10, NULL),
  ('pro', 'services_per_project', 10, NULL),
  ('pro', 'secrets_per_env', 50, NULL),
  ('pro', 'ram_mb', 2048, NULL),
  ('pro', 'vcpu_millicores', 2000, NULL),
  ('pro', 'build_timeout_seconds', 1800, NULL),
  ('pro', 'api_requests_per_min', 300, NULL),

  -- Team ($79/mo): 10 projects, 10 envs, unlimited builds, 25 seats
  ('team', 'projects', 10, 15),
  ('team', 'environments', 10, NULL),
  ('team', 'builds_per_month', NULL, 5000),
  ('team', 'seats', 25, NULL),
  ('team', 'services_per_project', NULL, 20),
  ('team', 'secrets_per_env', 200, NULL),
  ('team', 'ram_mb', 8192, NULL),
  ('team', 'vcpu_millicores', 8000, NULL),
  ('team', 'build_timeout_seconds', 3600, NULL),
  ('team', 'api_requests_per_min', 1000, NULL),

  -- Enterprise: all unlimited
  ('enterprise', 'projects', NULL, NULL),
  ('enterprise', 'environments', NULL, NULL),
  ('enterprise', 'builds_per_month', NULL, NULL),
  ('enterprise', 'seats', NULL, NULL),
  ('enterprise', 'services_per_project', NULL, NULL),
  ('enterprise', 'secrets_per_env', NULL, NULL),
  ('enterprise', 'ram_mb', NULL, NULL),
  ('enterprise', 'vcpu_millicores', NULL, NULL),
  ('enterprise', 'build_timeout_seconds', 7200, NULL),
  ('enterprise', 'api_requests_per_min', 5000, NULL);
```

**DoD:**
- [ ] Migration file created
- [ ] `SELECT * FROM plan_limits WHERE plan='starter' AND metric='projects'` → `hard_limit=1`
- [ ] `SELECT * FROM plan_limits WHERE plan='pro' AND metric='projects'` → `hard_limit=3`
- [ ] `SELECT * FROM plan_limits WHERE plan='team' AND metric='builds_per_month'` → `hard_limit=NULL` (unlimited)
- [ ] All existing tests pass

---

## Task 2: Add PlanLimiter to Engine API Server

The enforcement package lives in `cloud/internal/enforcement/`. The engine API (`api/`) needs to import it. Since `cloud` and `api` are separate Go modules, we need to either:
- **(A)** Move enforcement to `pkg/enforcement/` (shared module)
- **(B)** Import `cloud` module from `api` (creates dependency)
- **(C)** Duplicate the enforcement logic in `api/` (violates DRY)
- **(D)** Call enforcement via the cloud service proxy (adds latency)

**Decision: Option A — move to `pkg/enforcement/`**

Enforcement is a shared concern (both API and cloud need it). It only depends on `database/sql` and `log/slog` — zero external deps.

**Files:**
- Move: `cloud/internal/enforcement/limiter.go` → `pkg/enforcement/limiter.go`
- Move: `cloud/internal/enforcement/middleware.go` → `pkg/enforcement/middleware.go`
- Update all imports in `cloud/` that reference the old path
- Add import in `api/cmd/main.go` and `api/internal/server/server.go`

**DoD:**
- [ ] `pkg/enforcement/` package exists with `PlanLimiter` and `Middleware`
- [ ] `cloud/internal/enforcement/` is deleted (no duplication)
- [ ] `go build ./cloud/...` passes
- [ ] `go build ./api/...` passes
- [ ] `go build ./pkg/...` passes
- [ ] All existing tests pass

---

## Task 3: Wire PlanLimiter in main.go

**File:** `api/cmd/main.go` (~line 186, after dbAdapter)

```go
// Plan enforcement — limits builds, projects, environments, seats per plan.
var planLimiter *enforcement.PlanLimiter
if pool != nil {
    // PlanLimiter needs *sql.DB, not pgx pool. Use the stdlib adapter.
    planLimiter = enforcement.NewPlanLimiter(dbAdapter.StdDB(), logger)
}
```

**File:** `api/internal/server/server.go` — add to Config struct (~line 26):

```go
type Config struct {
    // ... existing fields ...
    PlanLimiter *enforcement.PlanLimiter  // Plan limit enforcement
}
```

**DoD:**
- [ ] `PlanLimiter` created in `main.go` with DB connection
- [ ] Passed to `server.Config`
- [ ] Build succeeds

---

## Task 4: Wire Enforcement to Project Creation

**File:** `api/internal/server/server.go` (~line 185, project route)

**Current:**
```go
r.With(rateLimitMiddlewares...).Post("/api/v1/projects", projects.Create)
```

**After:**
```go
projectEnforcement := enforcement.Middleware(
    cfg.PlanLimiter,
    enforcement.OperationCheck{
        Metric: "projects",
        CountFn: func(tenantID string) int {
            projects, _ := cfg.ProjectStore.ListByTenant(ctx, tenantID)
            return len(projects)
        },
    },
    func(r *http.Request) string {
        uc := middleware.GetUserContext(r.Context())
        if uc == nil { return "" }
        return uc.TenantID
    },
    "https://cloud.dev.loopzero.example/billing",
)
r.With(append(rateLimitMiddlewares, projectEnforcement)...).Post("/api/v1/projects", projects.Create)
```

**Note:** The `CountFn` closure captures `cfg.ProjectStore`. The `tenantExtractor` reads from `UserContext` set by auth middleware.

**DoD:**
- [ ] `POST /api/v1/projects` checks `projects` limit before creation
- [ ] Starter user with 1 project → `POST /api/v1/projects` returns 402
- [ ] 402 response body: `{"code":"PLAN_LIMIT_EXCEEDED","error":"Plan limit reached: Starter allows 1 project...","upgrade_url":"..."}`
- [ ] Pro user with 3 projects → 402
- [ ] Team user with 10 projects → 402 (or soft overage if < 15)
- [ ] Enterprise → always allowed
- [ ] Test: mock limiter → verify 402 response

---

## Task 5: Wire Enforcement to Build Submission

**File:** `api/internal/server/server.go` (~line 230, build route)

**CountFn for builds:**
```go
func(tenantID string) int {
    now := time.Now().UTC()
    periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
    count, _ := cfg.BuildStore.CountByTenantSince(ctx, tenantID, periodStart)
    return count
}
```

**Note:** Need to add `CountByTenantSince(ctx, tenantID, since)` to `BuildStore` interface and implement it.

**DoD:**
- [ ] `POST /api/v1/builds` checks `builds_per_month` limit
- [ ] Starter with 50+ builds this month → 402
- [ ] Pro with 500+ builds → 402
- [ ] Team → unlimited (NULL limit), soft limit tracked
- [ ] `BuildStore.CountByTenantSince()` implemented
- [ ] Test: build at limit → 402

---

## Task 6: Wire Enforcement to Environment Creation

**File:** `api/internal/server/server.go` (~line 268, environment route)

**CountFn for environments:**
```go
func(tenantID string) int {
    envs, _ := cfg.EnvironmentHandler.ListEnvironments(ctx, tenantID)
    return len(envs)
}
```

**DoD:**
- [ ] `POST /api/v1/environments` checks `environments` limit
- [ ] Starter with 1 environment → 402 on 2nd
- [ ] Pro with 3 environments → 402 on 4th
- [ ] Test: create environment at limit → 402

---

## Task 7: Wire Enforcement to Member Invitation

**File:** `api/internal/server/server.go` (~line 259, team route)

**CountFn for seats:**
```go
func(tenantID string) int {
    members, _ := cfg.MemberStore.GetMemberships(ctx, tenantID)
    return len(members)
}
```

**Note:** `GetMemberships` returns memberships for a user, not for a tenant. Need `ListMembersByTenant(ctx, tenantID)` — check if `TeamStore` has it.

**DoD:**
- [ ] `POST /api/v1/teams/members` checks `seats` limit
- [ ] Starter with 3 members → 402 on 4th
- [ ] Pro with 10 members → 402 on 11th
- [ ] Test: add member at limit → 402

---

## Task 8: CLI Error Handling for 402

**File:** `cli/src/api/client.ts` or relevant error handler

The CLI needs to display the 402 response nicely:

```
Plan limit reached: Starter allows 1 project (current: 1).
Upgrade your plan: https://cloud.dev.loopzero.example/billing
```

**DoD:**
- [ ] CLI detects 402 status code
- [ ] Displays plan name, limit, current count
- [ ] Shows upgrade URL
- [ ] Exit code: 1 (usage error)
- [ ] `--json` includes `code`, `plan`, `limit`, `current`, `upgrade_url`

---

## Execution Order

```
Task 1: Migration (plan_limits seed)     — 30 min
Task 2: Move enforcement to pkg/         — 1 hour
Task 3: Wire PlanLimiter in main.go      — 30 min
Task 4: Project enforcement              — 1 hour
Task 5: Build enforcement                — 1.5 hours (needs CountByTenantSince)
Task 6: Environment enforcement          — 30 min
Task 7: Member enforcement               — 1 hour (needs ListMembersByTenant)
Task 8: CLI 402 handling                 — 1 hour

Total: ~7 hours (~1 day)
```

Dependencies:
- Task 2 before Tasks 3-7 (package must be in pkg/ before importing)
- Task 3 before Tasks 4-7 (PlanLimiter must exist before wiring)
- Task 1 can run in parallel with everything (migration is independent)
- Task 8 can run in parallel with server-side tasks
