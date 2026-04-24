-- Migration 000004: Add commit_sha to builds table
-- Sprint 2: needed for deploy flow (spec §10.1)
-- Forward-only: no DROP statements

ALTER TABLE builds ADD COLUMN IF NOT EXISTS commit_sha TEXT;
