-- queries/domains.sql — sqlc query definitions for the
-- custom_domains table (custom-domain lifecycle: create, verify,
-- TLS provision, CF zone management, background reaper claims).
--
-- T2.1 (Batch A #9) of plan 2026-05-29-sqlc-migration-plan
-- v1.3. Final Batch A store. Migrates the 3-file legacy split
-- (domain_store.go + domain_store_background.go +
-- domain_store_cfzone.go) into a unified sqlc query file +
-- single wrapper.
--
-- Tenant predicate model:
--   - User-facing CRUD (Create, GetByDomainAndTenant, ListByTenant,
--     CountByTenant, ListActiveByApp): tenant_id predicate enforced
--   - Single-domain mutations (UpdateStatus, TransitionStatus,
--     UpdateVerified, UpdateTLS, UpdateFailure, Delete,
--     UpdateCNAMECheck, UpdateCFZoneStatus): keyed by domain
--     (globally unique); tenantpredicate:exempt because domain
--     names are non-guessable + the route handler authenticates
--     ownership upstream
--   - Background reapers (Find*, ClaimByStatus, findByStatus):
--     tenantpredicate:exempt by design (admin scheduler path)
--   - FindPendingForAutoVerify is cell-scoped via JOIN to tenants
--     (multi-cell isolation per roadmap 03), still exempt because
--     it crosses tenants within a cell

-- =========================================================================
-- USER-FACING CRUD
-- =========================================================================

-- name: InsertCustomDomain :exec
-- 23505 unique_violation on domain UNIQUE → wrapper maps to
-- domain.ErrDomainExists.
INSERT INTO custom_domains (
  id, tenant_id, project_name, app_name, environment, domain, status,
  verification_token, tls_status, created_at, updated_at
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW()
);

-- name: InsertCustomDomainWithCFZone :exec
-- v2 variant — sets CF zone metadata at create-time.
INSERT INTO custom_domains (
  id, tenant_id, project_name, app_name, environment, domain, status,
  verification_token, tls_status,
  cf_zone_id, cf_zone_status, cf_nameservers, dns_records_imported,
  created_at, updated_at
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NOW(), NOW()
);

-- name: GetCustomDomainByDomainAndTenant :one
-- Tenant-scoped lookup; NoRows → (nil, nil).
SELECT id, tenant_id, project_name, app_name, environment, domain, status,
       verification_token, verified_at, tls_status, tls_expires_at,
       failure_reason, last_cname_check, cname_consecutive_failures, created_at, updated_at,
       cf_zone_id, cf_zone_status, cf_nameservers, dns_records_imported
FROM custom_domains
WHERE domain = $1 AND tenant_id = $2;

-- name: ListCustomDomainsByTenant :many
-- Dynamic filters via sqlc.narg() so the SQL shape is constant
-- regardless of which filters the caller supplied. Same plan-cache
-- shape as legacy fmt.Sprintf approach but without the
-- isParameterizedCondition gate (sqlc-emitted SQL never has raw
-- value interpolation by construction).
SELECT id, tenant_id, project_name, app_name, environment, domain, status,
       verification_token, verified_at, tls_status, tls_expires_at,
       failure_reason, last_cname_check, cname_consecutive_failures, created_at, updated_at,
       cf_zone_id, cf_zone_status, cf_nameservers, dns_records_imported
FROM custom_domains
WHERE tenant_id = @tenant_id::text
  AND (sqlc.narg('project_name')::text IS NULL OR project_name = sqlc.narg('project_name')::text)
  AND (sqlc.narg('app_name')::text IS NULL OR app_name = sqlc.narg('app_name')::text)
  AND (sqlc.narg('environment')::text IS NULL OR environment = sqlc.narg('environment')::text)
ORDER BY created_at DESC
LIMIT @lim::int OFFSET @off::int;

-- name: CountCustomDomainsByTenant :one
-- Matching COUNT for pagination total — WHERE shape mirrors the
-- list query above.
SELECT COUNT(*) FROM custom_domains
WHERE tenant_id = @tenant_id::text
  AND (sqlc.narg('project_name')::text IS NULL OR project_name = sqlc.narg('project_name')::text)
  AND (sqlc.narg('app_name')::text IS NULL OR app_name = sqlc.narg('app_name')::text)
  AND (sqlc.narg('environment')::text IS NULL OR environment = sqlc.narg('environment')::text);

-- name: CountActiveCustomDomainsByTenant :one
-- SEC-007 quota-enforcement count (excludes status='removing').
-- Separate query from the filtered Count above because it's a
-- different predicate shape used by Create handler.
SELECT COUNT(*) FROM custom_domains
WHERE tenant_id = $1 AND status <> 'removing';

-- name: ListActiveCustomDomainsByApp :many
-- Used by manifest renderer to surface custom domain Ingress
-- entries. Tenant-scoped via (tenant_id, project_name, app_name,
-- environment) tuple.
SELECT id, tenant_id, project_name, app_name, environment, domain, status,
       verification_token, verified_at, tls_status, tls_expires_at,
       failure_reason, last_cname_check, cname_consecutive_failures, created_at, updated_at,
       cf_zone_id, cf_zone_status, cf_nameservers, dns_records_imported
FROM custom_domains
WHERE tenant_id = $1 AND project_name = $2 AND app_name = $3 AND environment = $4
  AND status IN ('active', 'provisioning')
ORDER BY domain;

-- =========================================================================
-- SINGLE-DOMAIN MUTATIONS (keyed by globally-unique domain)
-- =========================================================================

-- tenantpredicate:exempt
-- name: UpdateCustomDomainStatus :execrows
-- Wrapper maps rows-affected=0 → "domain not found" error. Domain
-- names are globally unique + non-guessable; the route handler
-- authenticates ownership upstream via GetByDomainAndTenant before
-- invoking this mutation.
UPDATE custom_domains
SET status = $1, updated_at = NOW()
WHERE domain = $2;

-- tenantpredicate:exempt
-- name: TransitionCustomDomainStatus :execrows
-- CRIT-01 fix: CAS (compare-and-swap) prevents TOCTOU race
-- conditions. Returns rows-affected so wrapper can surface
-- ErrStaleTransition-equivalent error when domain is no longer in
-- expectedStatus.
UPDATE custom_domains
SET status = $1, updated_at = NOW()
WHERE domain = $2 AND status = $3;

-- tenantpredicate:exempt
-- name: UpdateCustomDomainVerified :execrows
UPDATE custom_domains
SET status = 'verified', verified_at = $1, updated_at = NOW()
WHERE domain = $2;

-- tenantpredicate:exempt
-- name: UpdateCustomDomainTLS :execrows
UPDATE custom_domains
SET tls_status = $1, tls_expires_at = $2, updated_at = NOW()
WHERE domain = $3;

-- tenantpredicate:exempt
-- name: UpdateCustomDomainFailure :execrows
UPDATE custom_domains
SET status = $1, failure_reason = $2, updated_at = NOW()
WHERE domain = $3;

-- tenantpredicate:exempt
-- name: DeleteCustomDomain :execrows
DELETE FROM custom_domains
WHERE domain = $1;

-- tenantpredicate:exempt
-- name: UpdateCustomDomainCNAMECheck :execrows
UPDATE custom_domains
SET last_cname_check = $1, cname_consecutive_failures = $2, updated_at = NOW()
WHERE domain = $3;

-- tenantpredicate:exempt
-- name: UpdateCustomDomainCFZoneStatus :execrows
UPDATE custom_domains
SET cf_zone_status = $1, updated_at = NOW()
WHERE domain = $2;

-- =========================================================================
-- BACKGROUND REAPERS (admin scheduler paths — cross-tenant by design)
-- =========================================================================

-- tenantpredicate:exempt
-- name: ClaimCustomDomainsByStatus :many
-- FIND-001 fix: atomic UPDATE...WHERE id IN (SELECT...FOR UPDATE
-- SKIP LOCKED) RETURNING. Single statement so the lock is never
-- released before the transition completes. Replaces broken
-- legacy findByStatus that used FOR UPDATE SKIP LOCKED in a plain
-- SELECT (lock released before processing).
--
-- Inner SELECT aliased to `cd2` to avoid sqlc parser ambiguity
-- between the outer UPDATE target and the inner SELECT (sqlc
-- emits "column 'status' is ambiguous" without the alias).
UPDATE custom_domains AS cd
SET status = $2, updated_at = NOW()
WHERE cd.id IN (
  SELECT cd2.id FROM custom_domains AS cd2
  WHERE cd2.status = $1
  ORDER BY cd2.created_at ASC
  LIMIT 50
  FOR UPDATE SKIP LOCKED
)
RETURNING cd.id, cd.tenant_id, cd.project_name, cd.app_name, cd.environment, cd.domain, cd.status,
          cd.verification_token, cd.verified_at, cd.tls_status, cd.tls_expires_at,
          cd.failure_reason, cd.last_cname_check, cd.cname_consecutive_failures, cd.created_at, cd.updated_at,
          cd.cf_zone_id, cd.cf_zone_status, cd.cf_nameservers, cd.dns_records_imported;

-- tenantpredicate:exempt
-- name: FindCustomDomainsByStatus :many
-- Simple read-only listing by status (no locking, no claim). Used
-- by FindPendingNS + FindNSActive non-critical paths.
SELECT id, tenant_id, project_name, app_name, environment, domain, status,
       verification_token, verified_at, tls_status, tls_expires_at,
       failure_reason, last_cname_check, cname_consecutive_failures, created_at, updated_at,
       cf_zone_id, cf_zone_status, cf_nameservers, dns_records_imported
FROM custom_domains
WHERE status = $1
ORDER BY created_at ASC
LIMIT 50;

-- tenantpredicate:exempt
-- name: ListActiveCustomDomainHosts :many
-- Every host the edge may route, with the tenant whose cell serves it. Read by
-- the edge route index, which spans tenants. No LIMIT: a host left out of the
-- index is a host the edge refuses.
SELECT domain, tenant_id
FROM custom_domains
WHERE status = 'active'
ORDER BY domain ASC;

-- tenantpredicate:exempt
-- name: FindPendingCustomDomainsForAutoVerify :many
-- Cell-scoped via JOIN to tenants table (multi-cell isolation per
-- roadmap 03). Empty cellID is handled by the wrapper (defaults to
-- domain.DefaultCellID before invocation).
SELECT cd.id, cd.tenant_id, cd.project_name, cd.app_name, cd.environment, cd.domain, cd.status,
       cd.verification_token, cd.verified_at, cd.tls_status, cd.tls_expires_at,
       cd.failure_reason, cd.last_cname_check, cd.cname_consecutive_failures, cd.created_at, cd.updated_at,
       cd.cf_zone_id, cd.cf_zone_status, cd.cf_nameservers, cd.dns_records_imported
FROM custom_domains cd
JOIN tenants t ON cd.tenant_id = t.id
WHERE cd.status = 'pending_verification' AND (sqlc.arg(cell_id)::text = '' OR t.cell_id = sqlc.arg(cell_id))
ORDER BY cd.created_at ASC
LIMIT 50;

-- tenantpredicate:exempt
-- name: FindActiveCustomDomainsForCNAMECheck :many
SELECT id, tenant_id, project_name, app_name, environment, domain, status,
       verification_token, verified_at, tls_status, tls_expires_at,
       failure_reason, last_cname_check, cname_consecutive_failures, created_at, updated_at,
       cf_zone_id, cf_zone_status, cf_nameservers, dns_records_imported
FROM custom_domains
WHERE status = 'active' AND (last_cname_check IS NULL OR last_cname_check < $1)
ORDER BY last_cname_check ASC NULLS FIRST
LIMIT 50;

-- tenantpredicate:exempt
-- name: FindStalePendingCustomDomains :many
SELECT id, tenant_id, project_name, app_name, environment, domain, status,
       verification_token, verified_at, tls_status, tls_expires_at,
       failure_reason, last_cname_check, cname_consecutive_failures, created_at, updated_at,
       cf_zone_id, cf_zone_status, cf_nameservers, dns_records_imported
FROM custom_domains
WHERE status = 'pending_verification' AND created_at < $1
ORDER BY created_at ASC
LIMIT 50;

-- tenantpredicate:exempt
-- name: FindStaleProvisioningCustomDomains :many
SELECT id, tenant_id, project_name, app_name, environment, domain, status,
       verification_token, verified_at, tls_status, tls_expires_at,
       failure_reason, last_cname_check, cname_consecutive_failures, created_at, updated_at,
       cf_zone_id, cf_zone_status, cf_nameservers, dns_records_imported
FROM custom_domains
WHERE status = 'provisioning' AND updated_at < $1
ORDER BY updated_at ASC
LIMIT 50;

-- tenantpredicate:exempt
-- name: FindActiveCustomDomainsWithTLSExpiry :many
SELECT id, tenant_id, project_name, app_name, environment, domain, status,
       verification_token, verified_at, tls_status, tls_expires_at,
       failure_reason, last_cname_check, cname_consecutive_failures, created_at, updated_at,
       cf_zone_id, cf_zone_status, cf_nameservers, dns_records_imported
FROM custom_domains
WHERE status = 'active' AND tls_expires_at IS NOT NULL
  AND tls_expires_at < NOW() + make_interval(days => $1)
ORDER BY tls_expires_at ASC
LIMIT 50;
