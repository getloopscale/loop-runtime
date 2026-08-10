-- Migration 000030: Leader generations for fencing
-- Roadmap 02 — API Loop Extraction (MEET-2026-04-10-046)
--
-- Problem: pg_try_advisory_lock is session-scoped. A connection can appear
-- alive to Postgres while the holder pod's kernel has already detected a
-- partition (11–90s detection window). During that window, two pods can
-- each believe they are the leader; a stale leader's writes to builds /
-- releases / gitops are a correctness hazard.
--
-- Solution: a monotonic per-loop generation counter, checked on every
-- side-effecting write. A stale leader's generation is strictly less than
-- the current generation, so its UPDATE ... WHERE leader_generation = $N
-- matches zero rows and silently no-ops. The live leader retries.
--
-- Table semantics:
--   name            — loop name (e.g. "build-reconcile", "release-convergence",
--                     "build-reaper", "release-reaper", "domain-health",
--                     "idempotency-cleaner", "sse-pruner"). Composite with
--                     cell_id so each cell has its own per-loop leader.
--   cell_id         — roadmap 03 integration: leaders are cell-scoped.
--   holder          — pod name or opaque worker identifier.
--   generation_id   — monotonic counter, bumped on acquisition only.
--   acquired_at     — timestamp of most recent successful acquire.
--   heartbeat_at    — updated every Heartbeat() call. Used by peers to
--                     detect a dead leader (heartbeat_at < now() - stale).
--
-- Forward-only: additive table + indexes, no data loss on re-run.

CREATE TABLE IF NOT EXISTS leader_generations (
    name          TEXT        NOT NULL,
    cell_id       TEXT        NOT NULL DEFAULT 'cell-0',
    holder        TEXT        NOT NULL,
    generation_id BIGINT      NOT NULL DEFAULT 1,
    acquired_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    heartbeat_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (name, cell_id)
);

-- Peers discover stale leaders by ordering on heartbeat_at.
CREATE INDEX IF NOT EXISTS idx_leader_generations_heartbeat
    ON leader_generations (heartbeat_at);

COMMENT ON TABLE leader_generations IS
    'Per-loop, per-cell leader generation counter for fencing. See roadmap 02.';
COMMENT ON COLUMN leader_generations.generation_id IS
    'Monotonic counter — new leader acquisition bumps by 1. Side-effecting writes guard on this value.';
