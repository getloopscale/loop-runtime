#!/usr/bin/env bash
# seed-demo-data.sh — T6.1 of plan
# 2026-05-29-dashboard-public-integration-canonicalization
#
# Idempotent seed of the local demo tenant (admin@getloopscale.com) with:
#   - 2 projects (api-gateway, web-app)
#   - 8 builds across both, varied states (SUCCEEDED/FAILED/CANCELLED/RUNNING)
#   - 5 releases (ACTIVE on prod, SUPERSEDED, etc.)
#   - 2 environment_targets (prod for each project)
#   - 12 audit_log entries
#   - subscriptions.plan upgraded to 'pro' for visibility
#
# v1.1 EC-08: FK-respecting DELETE order:
#   environment_targets → releases → builds → projects → audit_log
# Then INSERT in the reverse-dependent order (projects → builds → releases →
# environment_targets → audit_log).
#
# Usage:
#   bash infra/scripts/local/seed-demo-data.sh [--dry-run]
#
# Exit codes:
#   0  seed applied (or already present)
#   1  user admin@getloopscale.com does not exist (run /v1/auth/session/token first)
#   2  PG unreachable
set -euo pipefail

CONTEXT="${CONTEXT:-k3d-loop-runtime-core}"
NAMESPACE="${NAMESPACE:-loop-runtime-system}"
EMAIL="${EMAIL:-admin@getloopscale.com}"
DRY_RUN="${DRY_RUN:-0}"

while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) DRY_RUN=1; shift ;;
    *) echo "Unknown arg: $1" >&2; exit 1 ;;
  esac
done

log() { printf '[seed-demo-data] %s\n' "$*" >&2; }

log "Resolving PG credentials"
PG_PASS=$(kubectl --context="$CONTEXT" -n "$NAMESPACE" \
  get secret loop-runtime-db-credentials -o jsonpath='{.data.postgres-password}' \
  | base64 -d 2>/dev/null || true)
if [ -z "$PG_PASS" ]; then
  log "ERROR: loop-runtime-db-credentials Secret missing or unreadable"
  exit 2
fi

PSQL() {
  kubectl --context="$CONTEXT" -n "$NAMESPACE" exec -i postgresql-0 -c postgresql -- \
    env PGPASSWORD="$PG_PASS" psql -U postgres -d loop_runtime -At "$@"
}

log "Resolving tenant for $EMAIL"
USER_ID=$(PSQL -c "SELECT id FROM users WHERE email = '$EMAIL'" 2>/dev/null || echo "")
if [ -z "$USER_ID" ]; then
  log "ERROR: user $EMAIL does not exist in PG."
  log "       Log in once via the dashboard to provision the user + tenant"
  log "       (POST /v1/auth/session/token), then re-run this seed."
  exit 1
fi

TENANT_ID=$(PSQL -c "SELECT id FROM tenants WHERE owner_id = '$USER_ID' LIMIT 1" 2>/dev/null || echo "")
if [ -z "$TENANT_ID" ]; then
  log "ERROR: no tenant for $EMAIL (owner_id=$USER_ID)"
  exit 1
fi

log "User: $USER_ID  Tenant: $TENANT_ID"

if [ "$DRY_RUN" = "1" ]; then
  log "[dry-run] would seed demo data for tenant=$TENANT_ID"
  exit 0
fi

log "Seeding demo data (idempotent — FK-respecting DELETE first)"

PSQL -v ON_ERROR_STOP=1 <<SQL
BEGIN;

-- v1.1 EC-08: FK-respecting DELETE order. releases.build_id_fkey is
-- ON DELETE RESTRICT, so builds cannot be removed while releases reference
-- them. environment_targets references releases the same way.
DELETE FROM environment_targets WHERE tenant_id = '$TENANT_ID'
  AND project_id IN ('api-gateway', 'web-app');
DELETE FROM releases WHERE tenant_id = '$TENANT_ID'
  AND project_id IN ('api-gateway', 'web-app');
DELETE FROM builds   WHERE tenant_id = '$TENANT_ID'
  AND project_id IN ('api-gateway', 'web-app');
DELETE FROM projects WHERE tenant_id = '$TENANT_ID'
  AND name IN ('api-gateway', 'web-app');
-- audit_log is append-only (PG trigger audit_log_prevent_mutation): we
-- intentionally do NOT delete here. Re-running seed APPENDS another set of
-- audit rows (with new timestamps) — that's correct semantically (every
-- "seed" event IS a real audit-worthy action). The dashboard's /audit page
-- shows the most recent N entries, so duplicates from re-seeds are visible
-- but harmless. v1.1 EC-08 + 2026-06-01 hardening fix.

-- Plan: Pro (so dashboard limits don't hide the seeded data)
UPDATE subscriptions SET plan = 'pro', updated_at = now() WHERE tenant_id = '$TENANT_ID';

-- Projects
INSERT INTO projects (id, tenant_id, name, created_by, created_at, updated_at) VALUES
  ('p-api-gw-${TENANT_ID:0:8}',  '$TENANT_ID', 'api-gateway', '$USER_ID', now() - INTERVAL '14 days', now() - INTERVAL '1 hour'),
  ('p-web-app-${TENANT_ID:0:8}', '$TENANT_ID', 'web-app',     '$USER_ID', now() - INTERVAL '21 days', now() - INTERVAL '2 hours');

-- Builds: api-gateway
INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by, commit_sha,
                    duration_seconds, created_at, started_at, finished_at) VALUES
  ('b-api-001-${TENANT_ID:0:8}', '$TENANT_ID', 'api-gateway', 'production', 'SUCCEEDED', 'cor-001', '$USER_ID', '9a3f2c1b8e4d',
   245, now() - INTERVAL '10 days', now() - INTERVAL '10 days' + INTERVAL '5 sec', now() - INTERVAL '10 days' + INTERVAL '4 min'),
  ('b-api-002-${TENANT_ID:0:8}', '$TENANT_ID', 'api-gateway', 'production', 'SUCCEEDED', 'cor-002', '$USER_ID', '7e8d1a5f2c4b',
   198, now() - INTERVAL '7 days',  now() - INTERVAL '7 days' + INTERVAL '5 sec', now() - INTERVAL '7 days' + INTERVAL '3 min 18 sec'),
  ('b-api-003-${TENANT_ID:0:8}', '$TENANT_ID', 'api-gateway', 'production', 'SUCCEEDED', 'cor-003', '$USER_ID', '3b7c4d8e1f9a',
   267, now() - INTERVAL '3 days',  now() - INTERVAL '3 days' + INTERVAL '5 sec', now() - INTERVAL '3 days' + INTERVAL '4 min 27 sec'),
  ('b-api-004-${TENANT_ID:0:8}', '$TENANT_ID', 'api-gateway', 'staging',    'FAILED',    'cor-004', '$USER_ID', '5d2e9f7a8c3b',
   89,  now() - INTERVAL '2 days',  now() - INTERVAL '2 days' + INTERVAL '5 sec', now() - INTERVAL '2 days' + INTERVAL '1 min 29 sec'),
  ('b-api-005-${TENANT_ID:0:8}', '$TENANT_ID', 'api-gateway', 'staging',    'RUNNING',   'cor-005', '$USER_ID', '8a1b6c4d2e9f',
   NULL, now() - INTERVAL '12 minutes', now() - INTERVAL '12 minutes' + INTERVAL '5 sec', NULL);

UPDATE builds SET error_message = 'go build: ./cmd/api/main.go:42:8: undefined: ServerConfig' WHERE id = 'b-api-004-${TENANT_ID:0:8}';

-- Builds: web-app
INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by, commit_sha,
                    duration_seconds, created_at, started_at, finished_at) VALUES
  ('b-web-001-${TENANT_ID:0:8}', '$TENANT_ID', 'web-app', 'production', 'SUCCEEDED', 'cor-101', '$USER_ID', 'a2b3c4d5e6f7',
   156, now() - INTERVAL '14 days', now() - INTERVAL '14 days' + INTERVAL '5 sec', now() - INTERVAL '14 days' + INTERVAL '2 min 36 sec'),
  ('b-web-002-${TENANT_ID:0:8}', '$TENANT_ID', 'web-app', 'production', 'SUCCEEDED', 'cor-102', '$USER_ID', 'b3c4d5e6f7a8',
   173, now() - INTERVAL '5 days',  now() - INTERVAL '5 days' + INTERVAL '5 sec', now() - INTERVAL '5 days' + INTERVAL '2 min 53 sec'),
  ('b-web-003-${TENANT_ID:0:8}', '$TENANT_ID', 'web-app', 'staging',    'CANCELLED', 'cor-103', '$USER_ID', 'c4d5e6f7a8b9',
   42,  now() - INTERVAL '1 day',   now() - INTERVAL '1 day' + INTERVAL '5 sec',   now() - INTERVAL '1 day' + INTERVAL '47 sec');

-- Releases
INSERT INTO releases (id, build_id, tenant_id, project_id, environment, status, correlation_id, created_by,
                      git_commit, git_mirror_status, image_digest, image_signature, image_signed_at,
                      created_at) VALUES
  ('rel-api-001-${TENANT_ID:0:8}', 'b-api-001-${TENANT_ID:0:8}', '$TENANT_ID', 'api-gateway', 'production', 'SUPERSEDED', 'cor-001', '$USER_ID',
   '9a3f2c1b8e4d', 'committed', 'sha256:1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b',
   'MEUCIQDx...keyless...AhAxYz', now() - INTERVAL '10 days' + INTERVAL '5 min', now() - INTERVAL '10 days' + INTERVAL '4 min'),
  ('rel-api-002-${TENANT_ID:0:8}', 'b-api-002-${TENANT_ID:0:8}', '$TENANT_ID', 'api-gateway', 'production', 'SUPERSEDED', 'cor-002', '$USER_ID',
   '7e8d1a5f2c4b', 'committed', 'sha256:2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c',
   'MEUCIQDy...keyless...AhAzAB', now() - INTERVAL '7 days' + INTERVAL '4 min', now() - INTERVAL '7 days' + INTERVAL '3 min 18 sec'),
  ('rel-api-003-${TENANT_ID:0:8}', 'b-api-003-${TENANT_ID:0:8}', '$TENANT_ID', 'api-gateway', 'production', 'ACTIVE',     'cor-003', '$USER_ID',
   '3b7c4d8e1f9a', 'committed', 'sha256:3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d',
   'MEUCIQDz...keyless...AhA1CD', now() - INTERVAL '3 days' + INTERVAL '5 min', now() - INTERVAL '3 days' + INTERVAL '4 min 27 sec'),
  ('rel-web-001-${TENANT_ID:0:8}', 'b-web-001-${TENANT_ID:0:8}', '$TENANT_ID', 'web-app', 'production', 'SUPERSEDED', 'cor-101', '$USER_ID',
   'a2b3c4d5e6f7', 'committed', 'sha256:4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e',
   'MEUCIQDa...keyless...AhA2EF', now() - INTERVAL '14 days' + INTERVAL '3 min', now() - INTERVAL '14 days' + INTERVAL '2 min 36 sec'),
  ('rel-web-002-${TENANT_ID:0:8}', 'b-web-002-${TENANT_ID:0:8}', '$TENANT_ID', 'web-app', 'production', 'ACTIVE',     'cor-102', '$USER_ID',
   'b3c4d5e6f7a8', 'committed', 'sha256:5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f',
   'MEUCIQDb...keyless...AhA3GH', now() - INTERVAL '5 days' + INTERVAL '3 min', now() - INTERVAL '5 days' + INTERVAL '2 min 53 sec');

-- Environment targets (active prod releases)
INSERT INTO environment_targets (tenant_id, project_id, environment, desired_release_id, active_release_id,
                                  updated_by, correlation_id, updated_at) VALUES
  ('$TENANT_ID', 'api-gateway', 'production', 'rel-api-003-${TENANT_ID:0:8}', 'rel-api-003-${TENANT_ID:0:8}', '$USER_ID', 'cor-003', now() - INTERVAL '3 days'),
  ('$TENANT_ID', 'web-app',     'production', 'rel-web-002-${TENANT_ID:0:8}', 'rel-web-002-${TENANT_ID:0:8}', '$USER_ID', 'cor-102', now() - INTERVAL '5 days');

-- Audit log
INSERT INTO audit_log (tenant_id, action, actor, resource_type, resource_id, created_at) VALUES
  ('$TENANT_ID', 'project.created',  '$USER_ID', 'project',      'api-gateway',                       now() - INTERVAL '14 days'),
  ('$TENANT_ID', 'project.created',  '$USER_ID', 'project',      'web-app',                           now() - INTERVAL '21 days'),
  ('$TENANT_ID', 'build.submitted',  '$USER_ID', 'build',        'b-api-001-${TENANT_ID:0:8}',        now() - INTERVAL '10 days'),
  ('$TENANT_ID', 'release.activated','$USER_ID', 'release',      'rel-api-001-${TENANT_ID:0:8}',      now() - INTERVAL '10 days' + INTERVAL '5 min'),
  ('$TENANT_ID', 'build.submitted',  '$USER_ID', 'build',        'b-api-002-${TENANT_ID:0:8}',        now() - INTERVAL '7 days'),
  ('$TENANT_ID', 'release.activated','$USER_ID', 'release',      'rel-api-002-${TENANT_ID:0:8}',      now() - INTERVAL '7 days' + INTERVAL '4 min'),
  ('$TENANT_ID', 'build.submitted',  '$USER_ID', 'build',        'b-api-003-${TENANT_ID:0:8}',        now() - INTERVAL '3 days'),
  ('$TENANT_ID', 'release.activated','$USER_ID', 'release',      'rel-api-003-${TENANT_ID:0:8}',      now() - INTERVAL '3 days' + INTERVAL '5 min'),
  ('$TENANT_ID', 'build.failed',     '$USER_ID', 'build',        'b-api-004-${TENANT_ID:0:8}',        now() - INTERVAL '2 days'),
  ('$TENANT_ID', 'build.submitted',  '$USER_ID', 'build',        'b-api-005-${TENANT_ID:0:8}',        now() - INTERVAL '12 minutes'),
  ('$TENANT_ID', 'plan.upgraded',    '$USER_ID', 'subscription', 'starter→pro',                       now() - INTERVAL '5 minutes');

COMMIT;
SQL

log "Seed complete. Verification:"
PSQL -c "SELECT 'projects' AS t, count(*) FROM projects WHERE tenant_id = '$TENANT_ID'
  UNION ALL SELECT 'builds', count(*) FROM builds WHERE tenant_id = '$TENANT_ID'
  UNION ALL SELECT 'releases', count(*) FROM releases WHERE tenant_id = '$TENANT_ID'
  UNION ALL SELECT 'env_targets', count(*) FROM environment_targets WHERE tenant_id = '$TENANT_ID'
  UNION ALL SELECT 'audit_log', count(*) FROM audit_log WHERE tenant_id = '$TENANT_ID' AND resource_type IN ('project','build','release','subscription');"
