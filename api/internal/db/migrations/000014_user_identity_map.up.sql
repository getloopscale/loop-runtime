-- Migration 000012: User identity map for Kratos migration (Sprint 11, Phase 3)
--
-- Maps legacy user IDs (github-{username}) to Ory Kratos identity IDs.
-- Used during lazy migration: when an existing user authenticates via Kratos
-- for the first time, their legacy user record is linked to the new identity.
--
-- Forward-only migration (no DROP).

CREATE TABLE IF NOT EXISTS user_identity_map (
    user_id              TEXT NOT NULL REFERENCES users(id),
    kratos_identity_id   TEXT NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (kratos_identity_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_identity_map_user_id
    ON user_identity_map(user_id);

COMMENT ON TABLE user_identity_map IS 'Maps legacy users to Ory Kratos identity IDs for lazy migration';
