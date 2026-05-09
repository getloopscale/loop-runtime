-- 000012_fk_on_delete_explicit.down.sql
--
-- Reverts to NO ACTION default (the implicit pre-000012 behavior).
-- WARNING: removes the explicit semantics that find_data_004 was raised
-- against. Only use if migration 000012 itself is broken.

ALTER TABLE tenants
  DROP CONSTRAINT IF EXISTS tenants_owner_id_fkey,
  ADD CONSTRAINT tenants_owner_id_fkey
    FOREIGN KEY (owner_id) REFERENCES users(id);

ALTER TABLE tenant_members
  DROP CONSTRAINT IF EXISTS tenant_members_tenant_id_fkey,
  ADD CONSTRAINT tenant_members_tenant_id_fkey
    FOREIGN KEY (tenant_id) REFERENCES tenants(id);

ALTER TABLE tenant_members
  DROP CONSTRAINT IF EXISTS tenant_members_user_id_fkey,
  ADD CONSTRAINT tenant_members_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id);

ALTER TABLE projects
  DROP CONSTRAINT IF EXISTS projects_tenant_id_fkey,
  ADD CONSTRAINT projects_tenant_id_fkey
    FOREIGN KEY (tenant_id) REFERENCES tenants(id);

ALTER TABLE projects
  DROP CONSTRAINT IF EXISTS projects_created_by_fkey,
  ADD CONSTRAINT projects_created_by_fkey
    FOREIGN KEY (created_by) REFERENCES users(id);

ALTER TABLE build_apps
  DROP CONSTRAINT IF EXISTS build_apps_build_id_fkey,
  ADD CONSTRAINT build_apps_build_id_fkey
    FOREIGN KEY (build_id) REFERENCES builds(id);

ALTER TABLE releases
  DROP CONSTRAINT IF EXISTS releases_build_id_fkey,
  ADD CONSTRAINT releases_build_id_fkey
    FOREIGN KEY (build_id) REFERENCES builds(id);

ALTER TABLE environment_targets
  DROP CONSTRAINT IF EXISTS environment_targets_desired_release_id_fkey,
  ADD CONSTRAINT environment_targets_desired_release_id_fkey
    FOREIGN KEY (desired_release_id) REFERENCES releases(id);

ALTER TABLE environment_targets
  DROP CONSTRAINT IF EXISTS environment_targets_active_release_id_fkey,
  ADD CONSTRAINT environment_targets_active_release_id_fkey
    FOREIGN KEY (active_release_id) REFERENCES releases(id);

ALTER TABLE subscriptions
  DROP CONSTRAINT IF EXISTS subscriptions_tenant_id_fkey,
  ADD CONSTRAINT subscriptions_tenant_id_fkey
    FOREIGN KEY (tenant_id) REFERENCES tenants(id);

ALTER TABLE usage_records
  DROP CONSTRAINT IF EXISTS usage_records_tenant_id_fkey,
  ADD CONSTRAINT usage_records_tenant_id_fkey
    FOREIGN KEY (tenant_id) REFERENCES tenants(id);

ALTER TABLE user_identity_map
  DROP CONSTRAINT IF EXISTS user_identity_map_user_id_fkey,
  ADD CONSTRAINT user_identity_map_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id);

ALTER TABLE github_installations
  DROP CONSTRAINT IF EXISTS github_installations_tenant_id_fkey,
  ADD CONSTRAINT github_installations_tenant_id_fkey
    FOREIGN KEY (tenant_id) REFERENCES tenants(id);

ALTER TABLE project_repositories
  DROP CONSTRAINT IF EXISTS project_repositories_installation_id_fkey,
  ADD CONSTRAINT project_repositories_installation_id_fkey
    FOREIGN KEY (installation_id) REFERENCES github_installations(id);

ALTER TABLE tenants_pending_deletion
  DROP CONSTRAINT IF EXISTS tenants_pending_deletion_tenant_id_fkey,
  ADD CONSTRAINT tenants_pending_deletion_tenant_id_fkey
    FOREIGN KEY (tenant_id) REFERENCES tenants(id);
