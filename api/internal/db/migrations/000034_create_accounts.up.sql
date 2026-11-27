-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- ============================================================================
-- 000034_create_accounts
--
-- M0 of the account-level-tenancy roadmap
-- (.claude/knowledge-base/plans/account-foundation-m0-plan.md,
--  ADR .claude/knowledge-base/adrs/ADR-account-level-tenancy.md)
--
-- Introduces the ACCOUNT entity ("Cliente LoopRuntime" / independent developer) ONE
-- level ABOVE the existing tenant. An Account groups 1..N isolated tenants.
-- v1 is 1:1 user<->account, enforced by UNIQUE(owner_id) — which also enables
-- the ON CONFLICT (owner_id) idempotency used by EnsureAccount (race-safe
-- first-login) and by the 000037 backfill.
--
-- Account lives ONLY in the control-plane; nothing here crosses to the
-- data-plane. The tenant remains the hard data-isolation boundary
-- (workspace_id == tenant_id, unchanged).
--
-- Row ID format `acct-{16hex}` mirrors the `tn-{16hex}` / `apk-{16hex}`
-- convention.
-- ============================================================================

CREATE TABLE IF NOT EXISTS accounts (
    id           TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    plan         TEXT NOT NULL DEFAULT 'starter',
    owner_id     TEXT NOT NULL REFERENCES users(id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- v1: one account per owner. Enables ON CONFLICT (owner_id) for race-safe
    -- find-or-create. Relaxed to account_members in v2 (see ADR § Still open).
    CONSTRAINT accounts_owner_id_key UNIQUE (owner_id)
);

CREATE INDEX IF NOT EXISTS idx_accounts_owner_id ON accounts(owner_id);
