-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- Down for 000048 — drop split fns first (call period_end), then period fns.
DROP FUNCTION IF EXISTS metering_split_head(timestamptz, interval);
DROP FUNCTION IF EXISTS metering_split_tail(timestamptz, interval);
DROP FUNCTION IF EXISTS metering_cost(bigint, interval);
DROP FUNCTION IF EXISTS metering_period_of(timestamptz);
DROP FUNCTION IF EXISTS metering_period_end(timestamptz);
DROP FUNCTION IF EXISTS metering_period_start(timestamptz);
