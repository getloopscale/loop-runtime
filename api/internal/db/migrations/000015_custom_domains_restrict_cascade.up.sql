-- Migration 000015: Fix ON DELETE CASCADE on custom_domains.tenant_id
-- CRIT-04: CASCADE deletes domain rows without triggering gitops cleanup,
-- leaving orphaned IngressRoutes in the cluster (subdomain takeover vector).
-- Change to RESTRICT — domains must be explicitly removed (→ "removing" status)
-- before a tenant can be deleted.
-- Forward-only migration.

-- Drop the existing FK constraint and recreate with RESTRICT
ALTER TABLE custom_domains
    DROP CONSTRAINT IF EXISTS custom_domains_tenant_id_fkey;

ALTER TABLE custom_domains
    ADD CONSTRAINT custom_domains_tenant_id_fkey
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE RESTRICT;

COMMENT ON CONSTRAINT custom_domains_tenant_id_fkey ON custom_domains
    IS 'RESTRICT prevents tenant deletion while custom domains exist. Domains must be removed (status=removing → gitops cleanup) before tenant deletion.';
