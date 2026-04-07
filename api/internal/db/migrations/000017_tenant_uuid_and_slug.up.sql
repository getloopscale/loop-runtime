-- Migration 000017: UUID-based tenant ID + human-readable slug
-- MEET-2026-04-07-039 Blocker B2
--
-- Problem: Tenant ID derived from email local-part caused TOCTOU race condition
-- and cross-tenant privilege escalation (two concurrent signups with same email
-- prefix could result in one user joining the other's tenant as owner).
--
-- Fix: New tenants get tn-{UUID} as primary key (collision-free by construction).
-- A slug column provides human-readable display names (UNIQUE, for CLI/UI).
-- Existing tenants keep their current IDs; slug is populated from display_name.

-- Add slug column (human-readable, for CLI output and URLs)
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS slug TEXT;

-- Populate slug from existing display_name for existing tenants
UPDATE tenants SET slug = display_name WHERE slug IS NULL AND display_name != '';

-- For any tenants without display_name, derive slug from ID (strip tn- prefix)
UPDATE tenants SET slug = REPLACE(id, 'tn-', '') WHERE slug IS NULL OR slug = '';

-- De-duplicate slugs: append tenant ID suffix to resolve collisions.
-- display_name was never unique, so duplicates are valid existing data.
-- We must resolve them before creating the UNIQUE index.
UPDATE tenants t
SET slug = t.slug || '-' || SUBSTRING(t.id FROM 4 FOR 8)
WHERE EXISTS (
    SELECT 1 FROM tenants t2
    WHERE t2.slug = t.slug AND t2.id < t.id
);

-- Make slug NOT NULL and UNIQUE after population and de-duplication
ALTER TABLE tenants ALTER COLUMN slug SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_tenants_slug ON tenants (slug);
