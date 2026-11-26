-- Migration 000018: users.active_tenant_id for explicit multi-tenant selection
--
-- Plan T2.5 ADR D8 — closes F-S2-02 root cause (silent active team swap).
--
-- Pre-migration behavior:
--   Multi-tenant users (membership in 2+ tenants) had their "active team"
--   selected arbitrarily by the JWT middleware (Auth.go selectTenant
--   line N: heuristic "last-created membership"). Result: in concurrent
--   Ginkgo journeys, the active team would swap silently as new tenants
--   were created. F-S2-02 dogfood evidence: whoami returned team=tn-e2etest
--   initially → team=e2e-tenant after Ginkgo created e2e-tenant.
--
-- Post-migration behavior:
--   users.active_tenant_id (nullable, FK to tenants.id ON DELETE SET NULL)
--   stores explicit user-driven selection via POST /api/v1/team/switch.
--   /me handler reads this when present; falls back to JWT middleware
--   selection when NULL (backwards-compat, no migration backfill needed).
--   ON DELETE SET NULL handles tenant deletion gracefully (no dangling
--   reference exception in /me handler — EC-4 self-heal).
--
-- Forward-only:
--   No backfill: existing users default to NULL → middleware picks active
--   team using legacy heuristic. First `loopruntime team switch <name>` call
--   persists explicit selection. Migration is non-blocking (instant on
--   small users table).

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS active_tenant_id TEXT
    REFERENCES tenants (id) ON DELETE SET NULL;

COMMENT ON COLUMN users.active_tenant_id IS
    'Plan T2.5 D8: explicit active-team selection for multi-tenant users. '
    'NULL = legacy JWT middleware heuristic. Updated via POST /api/v1/team/switch. '
    'ON DELETE SET NULL prevents dangling reference on tenant deletion.';

-- Index for /me handler lookups when active_tenant_id is set.
-- Partial index reduces footprint (most users will have NULL initially).
CREATE INDEX IF NOT EXISTS idx_users_active_tenant_id
    ON users (active_tenant_id)
    WHERE active_tenant_id IS NOT NULL;
