-- Migration 000024: Custom domains for tenant applications
-- Custom Domains feature (MEET-2026-04-05-033)
--
-- Allows tenants to map their own domains (e.g., api.mycompany.com)
-- to specific apps deployed on the platform.
-- Forward-only migration.

CREATE TABLE IF NOT EXISTS custom_domains (
    id                         TEXT PRIMARY KEY,
    tenant_id                  TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    project_name               TEXT NOT NULL,
    app_name                   TEXT NOT NULL,
    environment                TEXT NOT NULL,
    domain                     TEXT NOT NULL,
    status                     TEXT NOT NULL DEFAULT 'pending_verification'
                               CHECK (status IN (
                                   'pending_verification',
                                   'verified',
                                   'provisioning',
                                   'active',
                                   'degraded',
                                   'failed',
                                   'removing'
                               )),
    verification_token         TEXT NOT NULL,
    verified_at                TIMESTAMPTZ,
    tls_status                 TEXT DEFAULT 'pending'
                               CHECK (tls_status IN ('pending', 'issuing', 'issued', 'failed')),
    tls_expires_at             TIMESTAMPTZ,
    failure_reason             TEXT,
    last_cname_check           TIMESTAMPTZ,
    cname_consecutive_failures INT NOT NULL DEFAULT 0,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One domain globally unique (no two tenants can claim the same domain)
CREATE UNIQUE INDEX IF NOT EXISTS idx_custom_domains_domain
    ON custom_domains(domain);

-- Lookup by tenant + project + app + environment
CREATE INDEX IF NOT EXISTS idx_custom_domains_tenant_project_app_env
    ON custom_domains(tenant_id, project_name, app_name, environment);

-- Background service: find domains needing provisioning or removal
CREATE INDEX IF NOT EXISTS idx_custom_domains_status
    ON custom_domains(status) WHERE status IN ('verified', 'provisioning', 'removing');

-- Background service: find active domains needing weekly CNAME re-check
CREATE INDEX IF NOT EXISTS idx_custom_domains_active_cname_check
    ON custom_domains(last_cname_check) WHERE status = 'active';

-- Seed custom domain limits into plan_limits (if table exists from migration 000013)
INSERT INTO plan_limits (plan, metric, hard_limit, soft_limit) VALUES
    ('starter', 'custom_domains_per_env', 0, NULL),
    ('pro', 'custom_domains_per_env', 3, NULL),
    ('team', 'custom_domains_per_env', 10, NULL),
    ('enterprise', 'custom_domains_per_env', 999999, NULL)
ON CONFLICT DO NOTHING;

COMMENT ON TABLE custom_domains IS 'User-owned custom domains mapped to LoopZero applications';
COMMENT ON COLUMN custom_domains.domain IS 'FQDN owned by the user (e.g., api.mycompany.com)';
COMMENT ON COLUMN custom_domains.app_name IS 'Specific app within the project this domain routes to';
COMMENT ON COLUMN custom_domains.verification_token IS 'TXT record value for DNS ownership verification';
COMMENT ON COLUMN custom_domains.status IS 'Lifecycle: pending_verification → verified → provisioning → active | failed → removing';
COMMENT ON COLUMN custom_domains.cname_consecutive_failures IS 'Consecutive weekly CNAME check failures. At 2 (14 days), domain degrades to failed';
