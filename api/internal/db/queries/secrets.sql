-- queries/secrets.sql — sqlc query definitions for the secrets
-- table. T2.2 (Batch B #4) of plan
-- 2026-05-29-sqlc-migration-plan v1.3.
--
-- SEC-001: encrypted_value column stores AES-256-GCM ciphertext
-- (nonce || ciphertext) or plaintext (local dev only with nil key).
-- The wrapper enforces encryption — sqlc just persists the bytes.
-- ListSecretNames NEVER returns the value column (defense-in-depth
-- against accidental leakage in admin/audit paths).
--
-- Multi-tenant — secrets is in TenantOwnedTables; every query
-- carries (tenant_id, project_id, environment, app, name) composite
-- scope.

-- name: UpsertSecret :one
-- SEC-001: encrypted_value is AES-256-GCM ciphertext when the
-- wrapper's encryptionKey is non-nil. Single atomic upsert: xmax=0
-- distinguishes INSERT from UPDATE (eliminates race in legacy
-- SELECT+INSERT path).
INSERT INTO secrets (id, tenant_id, project_id, environment, app, name, encrypted_value)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (tenant_id, project_id, environment, app, name)
DO UPDATE SET encrypted_value = EXCLUDED.encrypted_value, updated_at = NOW()
RETURNING (xmax = 0)::bool AS created;

-- name: DeleteSecret :execrows
-- Tenant + project + environment + app + name scoped delete.
-- Returns rows-affected so the wrapper can report "deleted=true"
-- when the secret existed.
DELETE FROM secrets
WHERE tenant_id = $1 AND project_id = $2 AND environment = $3
  AND app = $4 AND name = $5;

-- name: ListSecretNames :many
-- Returns metadata (name, environment, app) — NEVER the value
-- column. Alphabetical by name (predictable CLI output).
SELECT name, environment, app
FROM secrets
WHERE tenant_id = $1 AND project_id = $2 AND environment = $3
ORDER BY name;

-- name: ListSecretsForResync :many
-- V9 L20: Returns name+app+encrypted_value for all secrets in an env tuple.
-- Used by the deploy-side resync hook: after envEnsurer ensures the
-- workload namespace exists, the builds handler iterates these rows and
-- (re)creates ExternalSecrets on the workload cluster so pods can mount
-- secrets that were `loopscale secret set` BEFORE the namespace existed.
--
-- Returns the encrypted column — the wrapper decrypts via SEC-001 AES-GCM
-- before passing values to the K8s sync. Caller MUST be on the resync
-- path (builds.go after EnsureEnvironment); other callers should use
-- ListSecretNames to avoid leaking values into unintended scopes.
SELECT name, app, encrypted_value
FROM secrets
WHERE tenant_id = $1 AND project_id = $2 AND environment = $3
ORDER BY app, name;

-- name: GetSecretEncryptedValue :one
-- One secret's stored value for the broker that External Secrets Operator
-- calls. Scoped by (tenant, project, environment, name); app is not part of
-- the broker's URL contract, so same-name secrets across apps of one
-- environment collapse to one row by design (ADR D2 of
-- 2026-06-10-j22-layer2-code-review-fixes). The wrapper decrypts it through
-- the same dispatcher every other read uses.
SELECT encrypted_value
FROM secrets
WHERE tenant_id = $1 AND project_id = $2 AND environment = $3 AND name = $4
ORDER BY app
LIMIT 1;
