-- 000012_fk_on_delete_explicit.up.sql
--
-- T7.2 of deep-review-remediation-plan (closes find_data_004 HIGH).
--
-- BACKGROUND
-- ----------
-- Migration 000001 declared 15 FK references without explicit ON DELETE
-- behavior. PostgreSQL silently defaults to NO ACTION (effectively RESTRICT),
-- but this is opaque: future contributors cannot tell which dependents
-- block parent deletion vs. cascade vs. nullify.
--
-- This migration makes the behavior explicit. Choices reflect current
-- LoopRuntime semantics:
--
-- RESTRICT — keeps current behavior (deletion blocked while children exist).
--            Used where the parent is critical infrastructure that should
--            never be force-deleted while audit trail or downstream rows
--            depend on it.
--
-- CASCADE  — applies when the parent's deletion intentionally cascades
--            (membership rows, project_repositories already had this).
--
-- We do NOT use SET NULL anywhere because all the affected FKs are NOT NULL.
--
-- ADR D2: forward-only. The corresponding test
-- TestForeignKeyConstraintsAreExplicit (api/tests/integration/) reads
-- pg_constraint.confdeltype and asserts no constraint with confdeltype='a'
-- (NO ACTION default) remains.
--
-- Constraints touched (15 total per find_data_004):
--   tenants.owner_id                            → users(id)            RESTRICT
--   tenant_members.tenant_id                    → tenants(id)          CASCADE
--   tenant_members.user_id                      → users(id)            CASCADE
--   projects.tenant_id                          → tenants(id)          RESTRICT (delete tenant via tenants_pending_deletion grace flow)
--   projects.created_by                         → users(id)            RESTRICT
--   builds.build_id (build_apps.build_id)       → builds(id)           CASCADE (build deletion takes apps with it)
--   releases.build_id                           → builds(id)           RESTRICT (releases retain history independently)
--   environment_targets.desired_release_id      → releases(id)         RESTRICT
--   environment_targets.active_release_id       → releases(id)         RESTRICT
--   subscriptions.tenant_id                     → tenants(id)          RESTRICT
--   usage_records.tenant_id                     → tenants(id)          RESTRICT
--   user_identity_map.user_id                   → users(id)            CASCADE (orphan map row useless without user)
--   github_installations.tenant_id              → tenants(id)          SET NULL via ALTER (column is nullable)
--   project_repositories.installation_id        → github_installations(id) RESTRICT
--   tenants_pending_deletion.tenant_id          → tenants(id)          CASCADE (lifecycle bound to tenant row)

-- Drop + re-add each constraint with explicit ON DELETE.
-- Each ALTER acquires ACCESS EXCLUSIVE only briefly; no scan needed
-- because we're not changing the existence of the constraint, only its
-- behavior. PostgreSQL re-validates referenced rows.

ALTER TABLE tenants
  DROP CONSTRAINT IF EXISTS tenants_owner_id_fkey,
  ADD CONSTRAINT tenants_owner_id_fkey
    FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE RESTRICT;

ALTER TABLE tenant_members
  DROP CONSTRAINT IF EXISTS tenant_members_tenant_id_fkey,
  ADD CONSTRAINT tenant_members_tenant_id_fkey
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE;

ALTER TABLE tenant_members
  DROP CONSTRAINT IF EXISTS tenant_members_user_id_fkey,
  ADD CONSTRAINT tenant_members_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE projects
  DROP CONSTRAINT IF EXISTS projects_tenant_id_fkey,
  ADD CONSTRAINT projects_tenant_id_fkey
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE RESTRICT;

ALTER TABLE projects
  DROP CONSTRAINT IF EXISTS projects_created_by_fkey,
  ADD CONSTRAINT projects_created_by_fkey
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE RESTRICT;

ALTER TABLE build_apps
  DROP CONSTRAINT IF EXISTS build_apps_build_id_fkey,
  ADD CONSTRAINT build_apps_build_id_fkey
    FOREIGN KEY (build_id) REFERENCES builds(id) ON DELETE CASCADE;

ALTER TABLE releases
  DROP CONSTRAINT IF EXISTS releases_build_id_fkey,
  ADD CONSTRAINT releases_build_id_fkey
    FOREIGN KEY (build_id) REFERENCES builds(id) ON DELETE RESTRICT;

ALTER TABLE environment_targets
  DROP CONSTRAINT IF EXISTS environment_targets_desired_release_id_fkey,
  ADD CONSTRAINT environment_targets_desired_release_id_fkey
    FOREIGN KEY (desired_release_id) REFERENCES releases(id) ON DELETE RESTRICT;

ALTER TABLE environment_targets
  DROP CONSTRAINT IF EXISTS environment_targets_active_release_id_fkey,
  ADD CONSTRAINT environment_targets_active_release_id_fkey
    FOREIGN KEY (active_release_id) REFERENCES releases(id) ON DELETE RESTRICT;

ALTER TABLE subscriptions
  DROP CONSTRAINT IF EXISTS subscriptions_tenant_id_fkey,
  ADD CONSTRAINT subscriptions_tenant_id_fkey
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE RESTRICT;

ALTER TABLE usage_records
  DROP CONSTRAINT IF EXISTS usage_records_tenant_id_fkey,
  ADD CONSTRAINT usage_records_tenant_id_fkey
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE RESTRICT;

ALTER TABLE user_identity_map
  DROP CONSTRAINT IF EXISTS user_identity_map_user_id_fkey,
  ADD CONSTRAINT user_identity_map_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE github_installations
  DROP CONSTRAINT IF EXISTS github_installations_tenant_id_fkey,
  ADD CONSTRAINT github_installations_tenant_id_fkey
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE SET NULL;

ALTER TABLE project_repositories
  DROP CONSTRAINT IF EXISTS project_repositories_installation_id_fkey,
  ADD CONSTRAINT project_repositories_installation_id_fkey
    FOREIGN KEY (installation_id) REFERENCES github_installations(id) ON DELETE RESTRICT;

ALTER TABLE tenants_pending_deletion
  DROP CONSTRAINT IF EXISTS tenants_pending_deletion_tenant_id_fkey,
  ADD CONSTRAINT tenants_pending_deletion_tenant_id_fkey
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE;
