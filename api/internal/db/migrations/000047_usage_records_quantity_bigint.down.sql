-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- Down for 000047 — narrow usage_records.quantity BIGINT -> INTEGER.
-- LOSSY after data: this fails (out-of-range) if any row already holds a
-- value > 2^31. Narrowing after real compute counters exist must be a
-- conscious operator decision, not an automatic rollback.

ALTER TABLE usage_records ALTER COLUMN quantity TYPE INTEGER;
