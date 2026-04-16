-- Migration 000028: GitHub webhook delivery deduplication
-- Prevents duplicate processing of the same webhook event.
-- MEET-2026-04-09-044 P1.

CREATE TABLE IF NOT EXISTS github_webhook_deliveries (
    delivery_id  TEXT PRIMARY KEY,
    event_type   TEXT NOT NULL,
    received_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Auto-cleanup: deliveries older than 7 days (GC handles this)
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_received
    ON github_webhook_deliveries(received_at);
