-- Down migration for 000017 — drops cost_counters.
--
-- Forward-only migration policy: this file exists for the migration tooling
-- contract but should NEVER be applied against a tenant-bearing database
-- with active enforcement. If a rollback is required, snapshot counters to
-- usage_records first.

DROP POLICY IF EXISTS cost_counters_tenant_isolation ON cost_counters;
DROP TABLE IF EXISTS cost_counters;
