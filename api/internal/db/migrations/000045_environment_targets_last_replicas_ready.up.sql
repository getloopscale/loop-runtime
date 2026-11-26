-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- Migration 000045 — Data-plane snapshot columns on environment_targets for
-- Red Team loopruntime-cloud#128 hardening (dashboard /projects list badge
-- lies "Live" when data-plane serves 502 despite control-plane ACTIVE).
--
-- Two nullable snapshot columns absorb periodic Prometheus scrape
-- writes from the loopruntime-app-operator data-plane poller (plan T2.1):
--   * last_replicas_ready_value — value of
--     kube_deployment_status_replicas_ready at last scrape.
--   * last_replicas_ready_at    — wall-clock of last scrape success.
--
-- Nullable so existing environment_targets rows absorb without backfill; the
-- poller populates them on the first natural tick (or on the
-- synchronous first-tick invocation appended to
-- seed-post-cleanroom-all.sh per plan T6.1).
--
-- Partial index over (last_replicas_ready_at DESC) WHERE NOT NULL is
-- the blueprint Q4 answer: skips NULL rows so the index stays
-- proportional to actually-polled envs (not the full environment_targets
-- table), keeping the LATERAL JOIN in projects.sql (plan T3.1) fast
-- during the Cleanroom warm-up window before the first tick populates
-- every row.
--
-- Forward-only per repository migration discipline. Rollback lives in
-- the paired .down.sql for `task db:reset` round-trips.

ALTER TABLE environment_targets
  ADD COLUMN last_replicas_ready_value INT NULL,
  ADD COLUMN last_replicas_ready_at    TIMESTAMPTZ NULL;

CREATE INDEX IF NOT EXISTS idx_env_targets_last_replicas_ready_at
  ON environment_targets (last_replicas_ready_at DESC)
  WHERE last_replicas_ready_at IS NOT NULL;
