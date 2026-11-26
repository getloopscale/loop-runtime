-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- Reverses 000034_create_accounts. Safe only after 000035/036 dropped their
-- account_id FK columns (down chain runs 000039 -> 000034 in order).
DROP INDEX IF EXISTS idx_accounts_owner_id;
DROP TABLE IF EXISTS accounts;
