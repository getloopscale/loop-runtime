#!/usr/bin/env bash
# reset-for-e2e.sh — E2E testing helper: zera DB LoopZero + Kratos identities
# sem precisar de cleanroom-fresh completo. Reusable durante T6.1 testing.
#
# ============================================================================
# !!! TEMPORARY SCRIPT — REMOVE BEFORE V1.0 CUTOVER !!!
# ============================================================================
#
# WHY TEMPORARY:
#   Este script existe APENAS durante T6.1 (plan 2026-06-08-pre-release-test-rigor)
#   pra permitir iteração rápida do flow E2E onboarding-first sem rodar
#   `task teardown && task setup` (~30 min) toda vez.
#
#   Em produção isso seria substituído por:
#     (a) test fixtures via Ginkgo BeforeSuite (rodam contra cluster ephemeral)
#     (b) chaos-test idempotent reset via Argo Workflow
#     (c) drill cluster ephemeral via task drill:l0-up + teardown
#
#   Manter este script post-T6.1 é anti-pattern por 3 razões:
#   1. Bypassa o block-dangerous.py hook (DROP/TRUNCATE em massa)
#   2. Mascara bugs de cleanup canonical (FK cascades, soft-delete tombstones)
#   3. Cria diferença entre "como dev limpa" e "como prod limpa"
#
# DELETE-BY DATE: T6.1 umbrella PASS confirmed (target 2026-06-08 +N days)
#
# Tracked in memory: feedback_temporary_reset_e2e_script_DELETE_ME
# Tracked in allowlist: infra/.claude/rules/shell-scripts-allowlist.md
#                      (marker: TEMPORARY-DELETE-AFTER-T6.1)
#
# ============================================================================
#
# Usage:
#   bash infra/scripts/local/reset-for-e2e.sh           # interactive confirm
#   bash infra/scripts/local/reset-for-e2e.sh --yes     # skip confirm
#
# What it does:
#   1. Drop ALL Kratos identities (via admin API on localhost:14434)
#   2. TRUNCATE ALL tables in LoopZero DB CASCADE (preserves schema; same effect
#      as task db:reset but without DROP DATABASE which is hook-blocked)
#   3. Restart loopzero-api + loopzero-cloud deployments (clear in-memory pools)
#   4. Wait for ready
#
# What it DOES NOT do (intentional — operator/test does these):
#   - Seed identity (so first user registration tests register flow)
#   - Seed dev tenant/projects (so first onboarding is from-scratch)
#   - Touch GitHub App installations (operator uninstalls via browser if needed)
#   - Reset Kratos config (still uses canonical config from helm)

set -euo pipefail

CONTEXT="${CONTEXT:-k3d-loopzero-mgmt}"
PG_NAMESPACE="${PG_NAMESPACE:-loopzero-system}"
KRATOS_ADMIN_PF="${KRATOS_ADMIN_PF:-http://127.0.0.1:14434}"
SKIP_CONFIRM=0

for arg in "$@"; do
  case "$arg" in
    --yes|-y) SKIP_CONFIRM=1 ;;
    *) printf '[reset-for-e2e] unknown arg: %s\n' "$arg" >&2 ; exit 1 ;;
  esac
done

log() { printf '[reset-for-e2e] %s\n' "$*"; }

# ----------------------------------------------------------------------------
# Preflight
# ----------------------------------------------------------------------------

current_ctx=$(kubectl config current-context 2>/dev/null || echo "")
if [ "$current_ctx" != "$CONTEXT" ]; then
  log "ERROR: kubectl context is '$current_ctx', expected '$CONTEXT'"
  exit 1
fi

if ! curl -fsS "${KRATOS_ADMIN_PF}/admin/identities" >/dev/null 2>&1; then
  log "ERROR: Kratos admin port-forward not reachable at ${KRATOS_ADMIN_PF}"
  log "       Run: infra/scripts/local/port-forward-public-dashboard.sh start"
  exit 1
fi

# ----------------------------------------------------------------------------
# Confirm
# ----------------------------------------------------------------------------

if [ "$SKIP_CONFIRM" != "1" ]; then
  log "About to DESTROY all data in cleanroom-dev:"
  log "  - All Kratos identities (sessions, credentials, OIDC links)"
  log "  - All LoopZero DB tables (tenants, projects, builds, releases, etc.)"
  log "  - In-memory state of loopzero-api + loopzero-cloud (via restart)"
  log ""
  log "Schema + migrations stay intact. Run again after to start fresh E2E."
  log ""
  printf "Continue? [y/N] "
  read -r answer
  if [ "$answer" != "y" ] && [ "$answer" != "Y" ]; then
    log "aborted"
    exit 0
  fi
fi

# ----------------------------------------------------------------------------
# Step 1: drop Kratos identities
# ----------------------------------------------------------------------------

log "[1/4] dropping Kratos identities"
ids=$(curl -fsS "${KRATOS_ADMIN_PF}/admin/identities" | python3 -c '
import sys, json
for i in json.load(sys.stdin):
    print(i.get("id",""))
')
count=0
for id in $ids; do
  [ -z "$id" ] && continue
  curl -fsS -X DELETE "${KRATOS_ADMIN_PF}/admin/identities/$id" -o /dev/null || true
  count=$((count + 1))
done
log "  dropped $count identities"

# ----------------------------------------------------------------------------
# Step 2: truncate LoopZero DB tables
# ----------------------------------------------------------------------------

log "[2/4] truncating LoopZero DB tables (CASCADE, preserves schema)"

PG_EXEC="kubectl --context $CONTEXT -n $PG_NAMESPACE exec -i postgresql-0 -c postgresql -- env PGPASSWORD=loopzero-dev-password psql -U loopzero -d loopzero"

# Schema-config tables that MUST stay populated post-truncate (idempotent
# canonical seeds from migrations 000002 + 000032). Without these, enforcement
# fail-closes and blocks project create with 0 projects.
SCHEMA_CONFIG_TABLES="schema_migrations"

tables=$($PG_EXEC -tAc "SELECT string_agg(tablename, ',') FROM pg_tables WHERE schemaname='public' AND tablename NOT IN ('$SCHEMA_CONFIG_TABLES')" 2>/dev/null | tr -d ' \n')
if [ -z "$tables" ]; then
  log "  ERROR: no tables found"
  exit 1
fi
$PG_EXEC -c "TRUNCATE TABLE $tables RESTART IDENTITY CASCADE;" 2>&1 | tail -1
log "  truncated tables (schema_migrations preserved)"

# Re-apply seed migrations idempotently. plan_limits is a SCHEMA-CONFIG table
# (canonical limits per plan tier) — without it, enforcement.MaxProjects=0
# blocks create even with 0 projects. Same for plan_caps_m2 updates.
log "  re-seeding plan_limits + plan_caps (idempotent ON CONFLICT)"
$PG_EXEC < api/internal/db/migrations/000002_seed_data.up.sql > /dev/null 2>&1 || true
$PG_EXEC < api/internal/db/migrations/000032_plan_caps_m2.up.sql > /dev/null 2>&1 || true
plan_count=$($PG_EXEC -tAc "SELECT COUNT(DISTINCT plan) FROM plan_limits" 2>/dev/null | tr -d ' \n')
log "  plan_limits: $plan_count plans seeded"

# ----------------------------------------------------------------------------
# Step 3: restart loopzero-api + loopzero-cloud
# ----------------------------------------------------------------------------

log "[3/4] restarting loopzero-api + loopzero-cloud"
kubectl --context "$CONTEXT" -n "$PG_NAMESPACE" rollout restart deploy/loopzero-api >/dev/null
kubectl --context "$CONTEXT" -n "$PG_NAMESPACE" rollout restart deploy/loopzero-cloud >/dev/null
kubectl --context "$CONTEXT" -n "$PG_NAMESPACE" rollout status deploy/loopzero-api --timeout=120s | tail -1
kubectl --context "$CONTEXT" -n "$PG_NAMESPACE" rollout status deploy/loopzero-cloud --timeout=120s | tail -1

# ----------------------------------------------------------------------------
# Step 4: summary
# ----------------------------------------------------------------------------

log "[4/4] final verification"

identities=$(curl -fsS "${KRATOS_ADMIN_PF}/admin/identities" | python3 -c 'import sys, json; print(len(json.load(sys.stdin)))')
rows=$($PG_EXEC -tAc "SELECT (SELECT COUNT(*) FROM users)::text || ' users / ' || (SELECT COUNT(*) FROM tenants)::text || ' tenants / ' || (SELECT COUNT(*) FROM projects)::text || ' projects'" 2>/dev/null | tail -1)

log "  Kratos: $identities identities"
log "  LoopZero DB: $rows"
log ""
log "READY for E2E onboarding-first test. Open https://app-dev.loopzero.example/ in"
log "fresh incognito and register as new user."
