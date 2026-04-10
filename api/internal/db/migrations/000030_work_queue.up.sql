-- Distributed work queue for decoupling leader-only background loops.
-- Uses SELECT ... FOR UPDATE SKIP LOCKED for lock-free concurrent processing.
-- Each API pod (leader or non-leader) can claim and process work items.
--
-- P2-8: Eliminates singleton bottleneck — leader produces work items,
-- all pods consume them. Survives leader failover without work loss.

CREATE TABLE IF NOT EXISTS work_queue (
    id          BIGSERIAL PRIMARY KEY,
    queue       TEXT        NOT NULL,   -- queue name: "build_reconcile", "release_converge", "event_prune"
    payload     JSONB       NOT NULL,   -- queue-specific data (build_id, tenant, etc.)
    status      TEXT        NOT NULL DEFAULT 'pending',  -- pending, processing, completed, failed
    attempts    INT         NOT NULL DEFAULT 0,
    max_attempts INT        NOT NULL DEFAULT 3,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    claimed_by  TEXT,                   -- pod hostname that claimed this item
    claimed_at  TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    error       TEXT                    -- last error message if failed
);

-- Index for efficient dequeue: pending items ordered by creation
CREATE INDEX idx_work_queue_dequeue ON work_queue (queue, status, created_at)
    WHERE status = 'pending';

-- Index for stale claim detection: items claimed but not completed
CREATE INDEX idx_work_queue_stale ON work_queue (status, claimed_at)
    WHERE status = 'processing';

-- Automatic cleanup: delete completed items older than 24h
-- (handled by EventPruner loop, not a PG trigger)
