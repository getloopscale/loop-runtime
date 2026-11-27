-- A GitHub App installation records who confirmed its owner, and when.
--
-- `github_installations.tenant_id` says which tenant an installation belongs to.
-- The binding is written after GitHub confirms, through the App's user
-- authorization, that the signed-in user can access the installation:
--
--   verified_at  when GitHub confirmed it
--   verified_by  the LoopScale user who authorized (users.id)
--
-- Both are nullable and the migration fills neither. A binding made before it
-- carries no confirmation, so it counts as unverified: listing the
-- installation's repositories and connecting one wait until a member of the
-- tenant authorizes on GitHub. Repositories already connected keep
-- deploying on push, which reads project_repositories and not this binding.
--
-- verified_by has no foreign key: the row must outlive the user who authorized,
-- and the value is for the audit trail only.
--
-- @see api/internal/db/queries/repositories.sql (ClaimGitHubInstallation)

BEGIN;

ALTER TABLE github_installations
  ADD COLUMN IF NOT EXISTS verified_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS verified_by TEXT;

COMMIT;
