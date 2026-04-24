-- Migration 000026: Add Cloudflare Zone Management columns to custom_domains
-- Custom Domains v2 — CF Zone per domain (model Vercel).
-- Validated with aquim.ai on 2026-04-08.
-- Forward-only migration.

-- New columns for CF zone management
ALTER TABLE custom_domains
    ADD COLUMN IF NOT EXISTS cf_zone_id TEXT,
    ADD COLUMN IF NOT EXISTS cf_zone_status TEXT DEFAULT 'none'
        CHECK (cf_zone_status IN ('none', 'pending', 'active', 'failed', 'removed')),
    ADD COLUMN IF NOT EXISTS cf_nameservers TEXT[],
    ADD COLUMN IF NOT EXISTS dns_records_imported BOOLEAN NOT NULL DEFAULT false;

-- Update status CHECK to include v2 states (pending_ns, ns_active)
-- Drop existing constraint first (name may vary by PG version)
ALTER TABLE custom_domains DROP CONSTRAINT IF EXISTS custom_domains_status_check;
ALTER TABLE custom_domains ADD CONSTRAINT custom_domains_status_check
    CHECK (status IN (
        'pending_verification',  -- v1: waiting for TXT record
        'verified',              -- v1: TXT confirmed
        'pending_ns',            -- v2: CF zone created, waiting for NS propagation
        'ns_active',             -- v2: NS propagated, waiting for deploy
        'provisioning',          -- shared: IngressRoute being created
        'active',                -- shared: domain fully operational
        'degraded',              -- v1: CNAME lost, grace period
        'failed',                -- shared: provisioning/NS failed
        'removing'               -- shared: cleanup in progress
    ));

-- Index for v2 background poller (find domains waiting for NS propagation)
CREATE INDEX IF NOT EXISTS idx_custom_domains_pending_ns
    ON custom_domains(cf_zone_status) WHERE status = 'pending_ns';

-- Index for v2 provisioner (find domains ready for provisioning after NS active)
CREATE INDEX IF NOT EXISTS idx_custom_domains_ns_active
    ON custom_domains(status) WHERE status = 'ns_active';

COMMENT ON COLUMN custom_domains.cf_zone_id IS 'Cloudflare zone ID (v2: per-domain zone management)';
COMMENT ON COLUMN custom_domains.cf_zone_status IS 'CF zone activation: none (v1) | pending → active (v2)';
COMMENT ON COLUMN custom_domains.cf_nameservers IS 'Assigned CF nameservers the user must configure at registrar';
COMMENT ON COLUMN custom_domains.dns_records_imported IS 'Whether DNS records were imported from previous provider via CF scan';
