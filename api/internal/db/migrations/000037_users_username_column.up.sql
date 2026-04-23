-- Migration 000037: Decouple user identity from identity provider prefix
-- CHG-2026-04-23-AUDIT-7: `users.id` was doubling as opaque PK AND encoded
-- identity provenance ("github-" prefix). Breaks for non-GitHub providers
-- (Ory Hydra client_credentials inserts raw client_id like "loopzero-e2e").
-- Surface: `loopzero team remove/role <user>` fails with "not a team member" for
-- Hydra-authenticated users because `GetUserByUsername` constructed
-- "github-" + username and queried by PK.
--
-- Forward-only additive migration:
--   (a) Add `username` TEXT column
--   (b) Backfill: prefer display_name, fallback to id with github- prefix stripped
--   (c) Enforce NOT NULL + UNIQUE after backfill
--
-- After this migration, lookups use `WHERE username = $1` with no assumption
-- about identity provider format.

-- Step 1: add column (nullable for backfill)
ALTER TABLE users ADD COLUMN IF NOT EXISTS username TEXT;

-- Step 2: backfill
-- Priority: display_name (if non-empty) > id with github- prefix stripped
UPDATE users
SET username = CASE
    WHEN COALESCE(display_name, '') != '' THEN display_name
    ELSE regexp_replace(id, '^github-', '')
END
WHERE username IS NULL;

-- Step 3: enforce NOT NULL
ALTER TABLE users ALTER COLUMN username SET NOT NULL;

-- Step 4: unique index — migration fails fast if backfill produced duplicates,
-- which would indicate a pre-existing data integrity issue that needs human
-- investigation (never silently resolve identity collisions).
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username_unique ON users(username);

COMMENT ON COLUMN users.username IS 'Human-visible identity, unique per user. '
    'Separate from id (opaque PK) to support multiple auth providers without '
    'encoding provenance in the PK. Added 2026-04-23 by AUDIT-7.';
