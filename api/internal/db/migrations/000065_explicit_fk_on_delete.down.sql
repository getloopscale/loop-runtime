-- Restores the implicit NO ACTION on the seven constraints. Deliberately a real
-- rollback and not a no-op: nothing depends on the new behaviour, and leaving
-- half a decision behind is worse than returning to the state the up migration
-- found. What it does NOT restore is the reasoning — see the up file.
BEGIN;

ALTER TABLE accounts
  DROP CONSTRAINT IF EXISTS accounts_owner_id_fkey,
  ADD  CONSTRAINT accounts_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES users(id);

ALTER TABLE tenants
  DROP CONSTRAINT IF EXISTS tenants_account_id_fkey,
  ADD  CONSTRAINT tenants_account_id_fkey FOREIGN KEY (account_id) REFERENCES accounts(id);

ALTER TABLE subscriptions
  DROP CONSTRAINT IF EXISTS subscriptions_account_id_fkey,
  ADD  CONSTRAINT subscriptions_account_id_fkey FOREIGN KEY (account_id) REFERENCES accounts(id);

ALTER TABLE loop_runtime_api_keys
  DROP CONSTRAINT IF EXISTS loop_runtime_api_keys_account_id_fkey,
  ADD  CONSTRAINT loop_runtime_api_keys_account_id_fkey FOREIGN KEY (account_id) REFERENCES accounts(id);

ALTER TABLE loop_runtime_api_keys
  DROP CONSTRAINT IF EXISTS loop_runtime_api_keys_rotated_from_fkey,
  ADD  CONSTRAINT loop_runtime_api_keys_rotated_from_fkey FOREIGN KEY (rotated_from) REFERENCES loop_runtime_api_keys(id);

ALTER TABLE preview_environments
  DROP CONSTRAINT IF EXISTS preview_environments_build_id_fkey,
  ADD  CONSTRAINT preview_environments_build_id_fkey FOREIGN KEY (build_id) REFERENCES builds(id);

ALTER TABLE preview_environments
  DROP CONSTRAINT IF EXISTS preview_environments_release_id_fkey,
  ADD  CONSTRAINT preview_environments_release_id_fkey FOREIGN KEY (release_id) REFERENCES releases(id);

COMMIT;
