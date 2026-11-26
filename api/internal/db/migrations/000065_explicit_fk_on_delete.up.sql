-- The last seven foreign keys without a declared ON DELETE.
--
-- `TestForeignKeyConstraintsAreExplicit` has been red for months over the
-- find_data_004 regression: 7 of 42 constraints fall back to the implicit
-- NO ACTION. The test is right to fail on it. NO ACTION is not "no behaviour" —
-- it is a behaviour nobody chose, discovered the first time somebody deletes a
-- parent row in an incident and the delete errors, or (with a deferred
-- constraint) errors at COMMIT after the rest of the transaction already ran.
--
-- Each of the seven gets the behaviour its OWN meaning implies. There is no
-- default worth applying across the board: the same word means "clean up after
-- me" on one edge and "lose the audit trail" on another.
--
-- | constraint                              | choice    | why |
-- |-----------------------------------------|-----------|-----|
-- | accounts.owner_id -> users               | RESTRICT  | deleting the owner would orphan an account that still bills. Ownership must be transferred first, deliberately. NOT NULL, so SET NULL is impossible anyway. |
-- | tenants.account_id -> accounts           | RESTRICT  | an account with live tenants must not vanish by side effect. Deleting the tenants is the explicit act. |
-- | subscriptions.account_id -> accounts     | CASCADE   | a subscription without an account is not a record, it is debris. |
-- | loopruntime_api_keys.account_id -> accounts     | CASCADE   | credentials die with the account they authenticate. Leaving them is a live key for an entity that no longer exists. |
-- | loopruntime_api_keys.rotated_from -> loopruntime_api_keys | SET NULL | the rotation ancestor may be purged; the successor MUST survive it. Cascading here would delete the key currently in use because its predecessor was cleaned up. |
-- | preview_environments.build_id -> builds  | SET NULL  | the build GC prunes builds after 30 days. The preview outlives the pointer and keeps its own state. |
-- | preview_environments.release_id -> releases | SET NULL | same reason as build_id. |
--
-- The two CASCADEs are the only edges where deleting the parent should delete
-- the child, and both are within one account's own data — no cross-tenant reach.
--
-- Forward-only, per the repository's migration discipline: constraints are
-- dropped and recreated with the same names, so `\d` output and any tooling
-- keyed on constraint names is unchanged.
--
-- @see tests/integration/fk_explicit_on_delete_test.go — the gate this closes

BEGIN;

ALTER TABLE accounts
  DROP CONSTRAINT IF EXISTS accounts_owner_id_fkey,
  ADD  CONSTRAINT accounts_owner_id_fkey
       FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE RESTRICT;

ALTER TABLE tenants
  DROP CONSTRAINT IF EXISTS tenants_account_id_fkey,
  ADD  CONSTRAINT tenants_account_id_fkey
       FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE RESTRICT;

ALTER TABLE subscriptions
  DROP CONSTRAINT IF EXISTS subscriptions_account_id_fkey,
  ADD  CONSTRAINT subscriptions_account_id_fkey
       FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE;

ALTER TABLE loopruntime_api_keys
  DROP CONSTRAINT IF EXISTS loopruntime_api_keys_account_id_fkey,
  ADD  CONSTRAINT loopruntime_api_keys_account_id_fkey
       FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE;

ALTER TABLE loopruntime_api_keys
  DROP CONSTRAINT IF EXISTS loopruntime_api_keys_rotated_from_fkey,
  ADD  CONSTRAINT loopruntime_api_keys_rotated_from_fkey
       FOREIGN KEY (rotated_from) REFERENCES loopruntime_api_keys(id) ON DELETE SET NULL;

ALTER TABLE preview_environments
  DROP CONSTRAINT IF EXISTS preview_environments_build_id_fkey,
  ADD  CONSTRAINT preview_environments_build_id_fkey
       FOREIGN KEY (build_id) REFERENCES builds(id) ON DELETE SET NULL;

ALTER TABLE preview_environments
  DROP CONSTRAINT IF EXISTS preview_environments_release_id_fkey,
  ADD  CONSTRAINT preview_environments_release_id_fkey
       FOREIGN KEY (release_id) REFERENCES releases(id) ON DELETE SET NULL;

COMMIT;
