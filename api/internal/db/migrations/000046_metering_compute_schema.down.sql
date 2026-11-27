-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- Down for 000046 — drop the metering tables and narrow the CHECK back.
-- NOTE: narrowing the CHECK fails if any cost_counters row already holds a
-- compute metric. That is intended — a down migration after real compute
-- counters exist is lossy and must be a conscious operator decision.

DROP TABLE IF EXISTS metering_config;
DROP TABLE IF EXISTS metering_cursor;

ALTER TABLE cost_counters DROP CONSTRAINT cost_counters_metric_check;
ALTER TABLE cost_counters ADD CONSTRAINT cost_counters_metric_check
    CHECK (metric IN ('egress_bytes', 'docr_pulls', 'build_minutes'));
