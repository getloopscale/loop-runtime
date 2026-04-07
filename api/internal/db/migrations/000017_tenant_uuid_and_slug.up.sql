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

-- Make slug NOT NULL and UNIQUE after population
ALTER TABLE tenants ALTER COLUMN slug SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_tenants_slug ON tenants (slug);
