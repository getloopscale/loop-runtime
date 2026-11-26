-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- Reverse M3 Phase-2 enforce: restore the nullable column (M0/M1/M2 window).
ALTER TABLE loopruntime_api_keys ALTER COLUMN account_id DROP NOT NULL;
ALTER TABLE tenants ALTER COLUMN account_id DROP NOT NULL;
