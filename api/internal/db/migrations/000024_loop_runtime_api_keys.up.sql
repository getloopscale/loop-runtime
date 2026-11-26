-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- ============================================================================
-- 000024_loop_runtime_api_keys
--
-- Plan 0 of dashboard-memory-section-roadmap.md
-- (see .claude/knowledge-base/plans/2026-06-06-platform-api-keys-with-scopes-plan.md
--  + .claude/knowledge-base/plans/2026-06-06-dashboard-memory-section-roadmap.md)
--
-- Platform-wide unified API keys with scopes. SINGLE table covers all future
-- platform verticals (Memory v0.1 + RAG/Traces/Agents/Deploys reserved) via
-- the `scopes` jsonb column.
--
-- ADRs locked by the plan (v1.2):
--   D1 — single unified loop_runtime_live_* key with multi-scope declaration
--        (rejects per-service keys + OAuth client_credentials at v0.1)
--   D2 — scope enum locked v0.1; reserved scopes recognized but rejected 503
--   D3 — secret format loop_runtime_live_{base64url(32)} (Stripe prefix discipline);
--        stored as `key_hash` (SHA-256 hex) + `key_prefix` (first 12 chars)
--   D4 — SHA-256 single-pass hashing (NOT bcrypt) — 256-bit entropy already
--        rules out brute-force; O(1) `WHERE key_hash = $1` lookup
--   D5 amendment — tenant scoping via Ory (subject, tenant_id) discriminator
--        (matches existing loop-console convention, NOT the original plan-skeleton
--        `owner_user_id`)
--   D6 — `expires_at` optional at v0.1 (NULL = never); rotation deferred v1.x
--   D7 — revocation via soft-delete (`revoked_at`); hard DELETE forbidden
--
-- Row ID format `apk-{16hex}` per pkg/id convention (ADR-0028, gate PKG-G06
-- "Deterministic Naming And IDs"). The plan's EC-2 wording uses "ULID" but
-- the SECURITY invariant (validate-format-BEFORE-DB-query) is preserved
-- verbatim by the handler regex `^apk-[0-9a-f]{16}$`.
--
-- Forward-only migration (no DOWN file) per CLAUDE.md migration rules.
-- Rollback path: ship a new 0000NN+1_drop_loop_runtime_api_keys.up.sql.

-- ============================================================================
-- Table: loop_runtime_api_keys
-- ============================================================================
CREATE TABLE IF NOT EXISTS loop_runtime_api_keys (
    id              TEXT PRIMARY KEY,                              -- apk-{16hex}
    subject         TEXT NOT NULL,                                 -- Ory user subject (owner) — populated by auth.GetUser(ctx).Subject
    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    key_name        TEXT NOT NULL CHECK (length(key_name) BETWEEN 1 AND 64),
    key_hash        TEXT NOT NULL,                                 -- SHA-256 hex (64 chars) of the raw secret
    key_prefix      TEXT NOT NULL,                                 -- first 12 chars of the raw secret (e.g. 'loop_runtime_live_Ab') — masked display
    scopes          JSONB NOT NULL,                                -- e.g. ["memory:read","memory:write"]
    expires_at      TIMESTAMPTZ,                                   -- NULL = never expires (D6)
    last_used_at    TIMESTAMPTZ,                                   -- updated async by RequireScope middleware (monitoring signal, not audit-grade per EC-21)
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at      TIMESTAMPTZ,                                   -- soft-delete (D7); NULL = active
    CHECK (jsonb_typeof(scopes) = 'array')                         -- EC-7 defense: rejects malformed jsonb at DB layer
);

-- ============================================================================
-- Indexes (hot paths)
-- ============================================================================

-- UNIQUE: middleware looks up `WHERE key_hash = $1` on every Bearer-authed
-- request. UNIQUE because two rows with the same hash would create
-- non-determinism in scope decisions. Covering index — no table touch on
-- the hot path until we need scopes payload (single covered fetch).
CREATE UNIQUE INDEX IF NOT EXISTS idx_loop_runtime_api_keys_key_hash
    ON loop_runtime_api_keys (key_hash);

-- Partial: list handler always filters `WHERE revoked_at IS NULL`.
-- Excluding revoked rows from the index keeps it tight + correlates exactly
-- with the active-key working set. Composite (tenant_id, subject) matches
-- the D5 multi-tenancy invariant query shape.
CREATE INDEX IF NOT EXISTS idx_loop_runtime_api_keys_tenant
    ON loop_runtime_api_keys (tenant_id, subject)
    WHERE revoked_at IS NULL;
