-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- Reverse 000054: drop the lens alert rules table (drops its indexes with it).
DROP TABLE IF EXISTS lens_alert_rules;
