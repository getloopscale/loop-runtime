-- Migration 000016: Fix github_id constraint that blocks multi-user Kratos auth
-- MEET-2026-04-07-039 Blocker B1
--
-- Problem: github_id BIGINT NOT NULL UNIQUE + hardcoded 0 in EnsureTenant
-- means only ONE Kratos user can exist (second user fails UNIQUE constraint).
--
-- Fix: Make github_id nullable (NULL for Kratos users), drop UNIQUE constraint.
-- Legacy GitHub-auth users keep their real github_id values.
-- Forward-only: no data loss.

-- Drop the UNIQUE constraint (created by inline UNIQUE in CREATE TABLE)
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_github_id_key;

-- Drop the explicit index (created in migration 000002)
DROP INDEX IF EXISTS idx_users_github_id;

-- Make github_id nullable (Kratos users don't have a GitHub ID)
ALTER TABLE users ALTER COLUMN github_id DROP NOT NULL;

-- Set existing sentinel values (0) to NULL
UPDATE users SET github_id = NULL WHERE github_id = 0;

-- Add a partial unique index: github_id must be unique when NOT NULL
-- This preserves uniqueness for real GitHub IDs while allowing multiple NULLs
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_github_id_unique
    ON users (github_id) WHERE github_id IS NOT NULL;
