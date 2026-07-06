-- Reverse 000051: drop the deploy-events outbox table (drops its index too).
DROP TABLE IF EXISTS deploy_events;
