-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- Reverse migration 000045 — drop data-plane snapshot columns +
-- partial index. Round-trip target for `task db:reset` (down → up).
--
-- IF EXISTS guards handle the case where the up migration partially
-- applied (e.g., ALTER TABLE succeeded but CREATE INDEX failed) so
-- the rollback stays idempotent.

DROP INDEX IF EXISTS idx_env_targets_last_replicas_ready_at;

ALTER TABLE environment_targets
  DROP COLUMN IF EXISTS last_replicas_ready_at,
  DROP COLUMN IF EXISTS last_replicas_ready_value;
