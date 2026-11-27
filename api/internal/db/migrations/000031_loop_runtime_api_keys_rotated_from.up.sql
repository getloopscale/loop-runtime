-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- ============================================================================
-- 000029_loop_runtime_api_keys_rotated_from
--
-- API Key Rotation plan, T6.2
-- (see .claude/knowledge-base/plans/2026-06-06-api-key-rotation-plan.md)
--
-- Adds the optional `rotated_from` audit column. Populated when a key was
-- created by POST /v1/api-keys/{id}/rotate; NULL when the key was created
-- via POST /v1/api-keys directly.
--
-- Closes Plan 0 ADR D6 deferral ("Rotation flow is DEFERRED to v1.x").
--
-- Forward-only per CLAUDE.md convention.
-- ============================================================================

ALTER TABLE loop_runtime_api_keys ADD COLUMN rotated_from TEXT REFERENCES loop_runtime_api_keys(id);

COMMENT ON COLUMN loop_runtime_api_keys.rotated_from IS
    'Optional audit pointer to the predecessor key id. Populated when a key '
    'was created by POST /v1/api-keys/{id}/rotate (API Key Rotation plan 2026-06-06); '
    'NULL when the key was created via POST /v1/api-keys directly. '
    'Self-FK ensures the audit chain cannot dangle.';
