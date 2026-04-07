-- Migration 000019: Enrich webhook_events for operational visibility
-- PLAN-auth-billing-hardening §1.2

ALTER TABLE webhook_events ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'processed';
ALTER TABLE webhook_events ADD COLUMN IF NOT EXISTS received_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE webhook_events ADD COLUMN IF NOT EXISTS last_error TEXT;
ALTER TABLE webhook_events ADD COLUMN IF NOT EXISTS attempt_count INT NOT NULL DEFAULT 1;
ALTER TABLE webhook_events ADD COLUMN IF NOT EXISTS payload_hash TEXT;
