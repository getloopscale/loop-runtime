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

-- De-duplicate slugs: append incremental suffix until globally unique.
-- Checks generated slug against ALL existing slugs, not just same-base duplicates.
-- This handles the case where tenant A already has slug 'acme-deadbeef' and
-- tenant B's de-dup would also produce 'acme-deadbeef'.
DO $$
DECLARE
  rec RECORD;
  candidate TEXT;
  suffix INT;
BEGIN
  -- Find all duplicate slugs (keep the first occurrence, rename the rest)
  FOR rec IN
    SELECT t.id, t.slug
    FROM tenants t
    WHERE EXISTS (
      SELECT 1 FROM tenants t2
      WHERE t2.slug = t.slug AND t2.id < t.id
    )
    ORDER BY t.id
  LOOP
    suffix := 1;
    candidate := rec.slug || '-' || SUBSTRING(rec.id FROM 4 FOR 8);
    -- Check if candidate already exists anywhere in the table
    WHILE EXISTS (SELECT 1 FROM tenants WHERE slug = candidate AND id != rec.id) LOOP
      suffix := suffix + 1;
      candidate := rec.slug || '-' || suffix;
    END LOOP;
    UPDATE tenants SET slug = candidate WHERE id = rec.id;
  END LOOP;
END $$;

-- Make slug NOT NULL and UNIQUE after population and de-duplication
ALTER TABLE tenants ALTER COLUMN slug SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_tenants_slug ON tenants (slug);
