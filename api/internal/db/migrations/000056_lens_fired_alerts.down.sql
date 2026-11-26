-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- Reverse 000056: drop the fired-alerts history table (drops its indexes with it).
DROP TABLE IF EXISTS lens_fired_alerts;
