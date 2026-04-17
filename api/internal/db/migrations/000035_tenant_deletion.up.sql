-- Migration 000035: Tenant deletion lifecycle tables
-- MEET-2026-04-17-052: Approved controller-native deletion with API intent gateway
-- Forward-only: no DROP statements

-- tenants_pending_deletion: soft-delete intent queue.
-- API writes here on DELETE /api/v1/tenants/:id. Grace expiry goroutine
-- processes rows where grace_until < NOW(). The Tenant CR deletion is triggered
-- by the API, and the TenantController finalizer handles infrastructure cleanup.
CREATE TABLE IF NOT EXISTS tenants_pending_deletion (
    tenant_id       TEXT PRIMARY KEY REFERENCES tenants(id),
    requested_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    requested_by    TEXT NOT NULL,          -- user email / "system:grace-expiry" / "admin:bulk"
    reason          TEXT NOT NULL,          -- "user_request" | "trial_expired" | "subscription_cancelled" | "admin_evict"
    grace_until     TIMESTAMPTZ,            -- NULL = immediate (force delete)
    force           BOOLEAN NOT NULL DEFAULT false,
    attempt_count   INT NOT NULL DEFAULT 0,
    last_error      TEXT,
    last_error_at   TIMESTAMPTZ
);

-- Index for grace expiry goroutine: find rows where grace has elapsed
CREATE INDEX IF NOT EXISTS idx_pending_deletion_grace
    ON tenants_pending_deletion (grace_until)
    WHERE grace_until IS NOT NULL;

-- tenant_tombstones: permanent audit record of deleted tenants.
-- Never deleted (audit/compliance). PII in requested_by is hashed after 2 years
-- (anonymized_at column tracks this). MEET-052 GDPR resolution.
CREATE TABLE IF NOT EXISTS tenant_tombstones (
    tenant_id         TEXT PRIMARY KEY,
    tenant_slug       TEXT NOT NULL,
    deleted_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    requested_by      TEXT NOT NULL,
    reason            TEXT NOT NULL,
    resources_cleaned JSONB NOT NULL DEFAULT '{}',  -- {"harbor":{"ms":150},"argocd":{"ms":80},...}
    resources_failed  JSONB,                         -- if force-delete, lists orphaned resources
    anonymized_at     TIMESTAMPTZ                    -- GDPR: set when requested_by is hashed (2y retention)
);

-- Index for GDPR anonymization job: find tombstones older than 2 years not yet anonymized
CREATE INDEX IF NOT EXISTS idx_tombstones_anonymization
    ON tenant_tombstones (deleted_at)
    WHERE anonymized_at IS NULL;
