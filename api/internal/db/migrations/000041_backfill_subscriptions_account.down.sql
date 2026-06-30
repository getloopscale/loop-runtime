-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopZero Authors

UPDATE subscriptions SET account_id = NULL WHERE account_id IS NOT NULL;
