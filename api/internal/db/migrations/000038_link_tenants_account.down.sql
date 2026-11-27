-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- Unlink tenants from accounts (re-NULL). Pairs with 000038 up.
UPDATE tenants SET account_id = NULL WHERE account_id IS NOT NULL;
