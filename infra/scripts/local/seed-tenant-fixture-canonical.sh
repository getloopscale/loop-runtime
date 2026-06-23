#!/usr/bin/env bash
# Canonical post-Cleanroom seed for tenant fixture state used by LIVE
# dogfood tests. Without this, dogfood-environments Scenario E
# (plan-limit 402 enforcement) cannot run + dogfood-project-connect
# webhook tests fail because PG has no canonical fixture state.
#
# Layers:
# 1. Upsert canonical team-tier subscription for the test tenant so the
#    plan-limit gate doesn't trigger 402 during burst-create tests
#    (env cap = unlimited for Team). For starter-cap testing the
#    operator runs `--starter` to downgrade.
#
# 2. Replay canonical GitHub App installation webhook (action=created)
#    to seed the `github_installations` row WITHOUT direct DB INSERT.
#    Uses the same HMAC chain the real GitHub Apps installation hits,
#    so the broker-validate + handler-process + PG-write paths are
#    exercised canonically (not bypassed).
#
# Idempotent: re-runs upsert subscription via ON CONFLICT DO UPDATE +
# webhook replay is dedup'd by GitHub delivery_id.

set -euo pipefail

MGMT_CONTEXT="${KUBE_CONTEXT:-k3d-loopzero-mgmt}"
LOOPZERO_NAMESPACE="${LOOPZERO_NAMESPACE:-loopzero-system}"
CLOUD_BASE="${LOOPZERO_CLOUD_BASE:-https://app-dev.loopzero.example}"
TENANT_ID="${LOOPZERO_TENANT_ID:-}"
PLAN_TIER="${LOOPZERO_PLAN_TIER:-team}"
GH_INSTALLATION_ID="${LOOPZERO_GH_INSTALLATION_ID:-138325349}"
GH_ACCOUNT_LOGIN="${LOOPZERO_GH_ACCOUNT_LOGIN:-getloopscale}"

SKIP_GH=0
SKIP_PLAN=0
DRY_RUN=0
for arg in "$@"; do
  case "$arg" in
    --skip-github) SKIP_GH=1 ;;
    --skip-plan) SKIP_PLAN=1 ;;
    --starter) PLAN_TIER="starter" ;;
    --pro) PLAN_TIER="pro" ;;
    --team) PLAN_TIER="team" ;;
    --dry-run) DRY_RUN=1 ;;
    --help|-h)
      sed -n '2,/^set -euo pipefail/p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) echo "Unknown flag: $arg" >&2; exit 1 ;;
  esac
done

log() { printf "%s [seed-tenant-fixture-canonical] %s\n" "$(date -u +%H:%M:%SZ)" "$*"; }
fail() { log "FAIL: $*"; exit 1; }

# Pre-flight — extract tenant_id from session JWT if not passed explicitly
if [[ -z "$TENANT_ID" ]] && [[ -f "$HOME/.loopzero/local-dogfood-session-jwt.txt" ]]; then
  TENANT_ID=$(cut -d. -f2 < "$HOME/.loopzero/local-dogfood-session-jwt.txt" \
    | { read -r p; printf '%s' "${p}=="; } | tr '_-' '/+' | base64 -d 2>/dev/null \
    | python3 -c "import json,sys; sys.stdout.write(json.load(sys.stdin).get('tenant_id',''))" 2>/dev/null || echo "")
fi
[[ -n "$TENANT_ID" ]] || fail "TENANT_ID not provided AND not in session JWT — run seed-admin-loopzero-dev.sh first"

if [[ "$DRY_RUN" == "1" ]]; then
  log "DRY_RUN: would upsert subscription ($PLAN_TIER) + replay GH installation webhook"
  exit 0
fi

# Layer 1 — subscription
if [[ "$SKIP_PLAN" == "0" ]]; then
  SUB_ID="sub-dev-$TENANT_ID"
  kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" \
    exec postgresql-0 -c postgresql -- \
    env PGPASSWORD=loopzero-dev-password psql -U loopzero -d loopzero -c \
    "INSERT INTO subscriptions (id, tenant_id, plan, status) VALUES ('$SUB_ID', '$TENANT_ID', '$PLAN_TIER', 'active') ON CONFLICT (tenant_id) DO UPDATE SET plan='$PLAN_TIER', status='active', updated_at=now();" \
    >/dev/null 2>&1
  log "Layer 1: tenant $TENANT_ID subscription upserted → plan=$PLAN_TIER status=active"
else
  log "Layer 1 SKIP: --skip-plan"
fi

# Layer 2 — GitHub installation replay via canonical webhook
if [[ "$SKIP_GH" == "0" ]]; then
  WEBHOOK_SECRET=$(kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" \
    get secret loopzero-github-webhook -o jsonpath='{.data.value}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
  if [[ -z "$WEBHOOK_SECRET" ]]; then
    log "Layer 2 SKIP: loopzero-github-webhook Secret missing — run seed-github-app-dev.sh first"
  else
    PAYLOAD=$(printf '{"action":"created","installation":{"id":%s,"account":{"login":"%s","type":"User"},"target_type":"User","repository_selection":"all"},"sender":{"login":"%s"}}' \
      "$GH_INSTALLATION_ID" "$GH_ACCOUNT_LOGIN" "$GH_ACCOUNT_LOGIN")
    SIG="sha256=$(printf '%s' "$PAYLOAD" | openssl dgst -sha256 -hmac "$WEBHOOK_SECRET" -hex | awk '{print $2}')"
    HTTP_CODE=$(curl -sk -o /dev/null -w '%{http_code}' -X POST \
      -H "Content-Type: application/json" \
      -H "X-GitHub-Event: installation" \
      -H "X-GitHub-Delivery: install-canonical-replay-$(date +%s)" \
      -H "X-Hub-Signature-256: $SIG" \
      -d "$PAYLOAD" \
      "$CLOUD_BASE/api/v1/webhooks/github" 2>/dev/null || echo "000")
    if [[ "$HTTP_CODE" == "200" ]]; then
      log "Layer 2: GitHub installation $GH_INSTALLATION_ID canonical webhook replayed (HTTP 200)"
    else
      log "Layer 2 WARN: webhook replay HTTP $HTTP_CODE (canonical chain may not be ready yet — re-run after seed-github-app-dev.sh + seed-broker-canonical.sh)"
    fi
  fi
else
  log "Layer 2 SKIP: --skip-github"
fi

# Layer 3 (M2 T2.2 2026-06-23) — Tenant CR + AppProject canonical state
# Per qa-readiness-roadmap M2 seeds #6 + #7 + ADR D3 (unified in this script).
#
# EC-4 absorption (v1.1): Tenant CR schema per operators/api/v1/tenant_types.go.
# PlanTier enum (CRD admission validation): starter / growth / enterprise.
# Mapping: starter→starter, pro→growth, team→enterprise (canonical per CLAUDE.md
# § Pricing tier→PlanTier).
#
# Required fields:
#   spec.displayName  (string, MinLength=1, MaxLength=128, Pattern ^[a-zA-Z0-9])
#   spec.plan         (PlanTier enum)
#   spec.environments ([], MinItems=0 — auto-populated by EnvironmentEnsurer)
#
# Flags: --skip-cr / --skip-appproject for partial reruns.
SKIP_CR=0
SKIP_APPPROJECT=0
for arg in "$@"; do
  case "$arg" in
    --skip-cr) SKIP_CR=1 ;;
    --skip-appproject) SKIP_APPPROJECT=1 ;;
  esac
done

# Map PLAN_TIER (script flag) to canonical PlanTier enum (CRD admission)
case "$PLAN_TIER" in
  starter) CR_PLAN="starter" ;;
  pro) CR_PLAN="growth" ;;
  team) CR_PLAN="enterprise" ;;
  *) CR_PLAN="starter" ;;
esac

# Derive canonical tenant_id + display_name
TENANT_ID_RESOLVED="${TENANT_ID:-3c198c99-0000-0000-0000-000000000000}"
DISPLAY_NAME="${LOOPZERO_TENANT_DISPLAY_NAME:-LoopZero Dev Tenant}"

ensure_tenant_cr() {
  if [[ "$SKIP_CR" == "1" ]]; then
    log "Layer 3a SKIP: --skip-cr"
    return 0
  fi
  if [[ "$DRY_RUN" == "1" ]]; then
    log "DRY: kubectl apply Tenant CR tenant-${TENANT_ID_RESOLVED} plan=${CR_PLAN}"
    return 0
  fi
  cat <<TENANT_YAML | kubectl --context "$MGMT_CONTEXT" apply -f - >/dev/null
apiVersion: loopzero.io/v1
kind: Tenant
metadata:
  name: ${TENANT_ID_RESOLVED}
  namespace: ${LOOPZERO_NAMESPACE}
spec:
  cellID: cell-default
  displayName: "${DISPLAY_NAME}"
  plan: ${CR_PLAN}
  environments: []
  gitops:
    branch: main
    repoName: ${TENANT_ID_RESOLVED}-gitops
  limits:
    maxBuildConcurrency: 2
    maxCPU: "4"
    maxMemory: 8Gi
    maxStorage: 20Gi
  preview:
    maxEnvironments: 3
    ttlHours: 72
TENANT_YAML
  log "Layer 3a: Tenant CR tenant-${TENANT_ID_RESOLVED} applied (plan=${CR_PLAN})"
}

ensure_appproject() {
  if [[ "$SKIP_APPPROJECT" == "1" ]]; then
    log "Layer 3b SKIP: --skip-appproject"
    return 0
  fi
  if [[ "$DRY_RUN" == "1" ]]; then
    log "DRY: kubectl apply AppProject loopzero-${TENANT_ID_RESOLVED}"
    return 0
  fi
  cat <<APPPROJ_YAML | kubectl --context "$MGMT_CONTEXT" apply -f - >/dev/null
apiVersion: argoproj.io/v1alpha1
kind: AppProject
metadata:
  name: loopzero-${TENANT_ID_RESOLVED}
  namespace: argocd
spec:
  description: "Canonical AppProject for tenant ${TENANT_ID_RESOLVED}"
  sourceRepos:
    - "*"
  destinations:
    - namespace: "loopzero-${TENANT_ID_RESOLVED}-*"
      server: "*"
    - namespace: "loopzero-system"
      server: "*"
  clusterResourceWhitelist:
    - group: "*"
      kind: "*"
  namespaceResourceWhitelist:
    - group: "*"
      kind: "*"
APPPROJ_YAML
  log "Layer 3b: ArgoCD AppProject loopzero-${TENANT_ID_RESOLVED} applied"
}

ensure_tenant_cr
ensure_appproject

log "DONE — tenant fixture canonical state seeded"
