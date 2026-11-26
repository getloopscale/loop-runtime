-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- Reverse 000053: drop the lens saved views table (drops its index with it).
DROP TABLE IF EXISTS lens_saved_views;
