-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- ============================================================================
-- 000035_tenants_account_id
--
-- M0: add NULLABLE account_id to tenants (additive, backward-compatible).
-- Nullable so legacy rows stay valid until 000038 backfills the link.
-- NOT NULL enforcement is deferred to M3 (ADR § non-destructive 2-phase).
-- ============================================================================

ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS account_id TEXT REFERENCES accounts(id);

CREATE INDEX IF NOT EXISTS idx_tenants_account_id ON tenants(account_id);
