#!/usr/bin/env bash
# T-02 admin-evict-tenants — bulk cleanup para testes de carga.
#
# Substitui os workarounds bash-kubectl que usávamos iterativamente em L3 v1-v7.
# Idempotente, com ordem correta de FKs + finalizer handling, seguro para re-run.
#
# Uso:
#   tools/admin-evict-tenants.sh --pattern 'tn-lt-%'          # default pattern loadtest
#   tools/admin-evict-tenants.sh --pattern 'tn-lt-l3-%'       # só tenants L3
#   tools/admin-evict-tenants.sh --pattern '%'  --yes         # TUDO (inclui prod — requer --yes)
#
# Plano: docs/plans/50k-immediate-focus.md (T-02)
set -eu

PATTERN="${PATTERN:-tn-lt-%}"
YES=0
CTX_MGMT="${CTX_MGMT:-do-sfo3-loopzero-infra-dev-sfo3-mgmt}"
CTX_RUNTIME="${CTX_RUNTIME:-do-sfo3-loopzero-infra-dev-sfo3-runtime}"
CTX_BUILD="${CTX_BUILD:-do-sfo3-loopzero-infra-dev-sfo3-build}"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --pattern) PATTERN="$2"; shift 2 ;;
    --yes) YES=1; shift ;;
    -h|--help)
      echo "Usage: $0 [--pattern 'SQL-LIKE-pattern'] [--yes]"
      echo "Env: CTX_MGMT, CTX_RUNTIME, CTX_BUILD (default: dev clusters)"
      exit 0
      ;;
    *) echo "Unknown arg: $1" >&2; exit 1 ;;
  esac
done

if [ "$PATTERN" = "%" ] && [ "$YES" -ne 1 ]; then
  echo "ERROR: pattern='%' matches ALL tenants (including production). Add --yes to confirm."
  exit 1
fi

echo "=== admin-evict-tenants ==="
echo "pattern: $PATTERN"
echo "mgmt:    $CTX_MGMT"
echo "runtime: $CTX_RUNTIME"
echo "build:   $CTX_BUILD"
echo ""

# Translate SQL LIKE pattern → K8s resource name prefix (tn-lt-% → tn-lt-)
RES_PREFIX="${PATTERN%%\%*}"

# ---- Step 1: Kill pending test processes (optional) ----
pkill -9 -f "real_deploy_test\|go test ./infra/tests/load" 2>/dev/null || true
sleep 2

# ---- Step 2: Clean argo workflows that may recreate tenants ----
echo "[1/6] Deleting argo workflows..."
kubectl --context="$CTX_BUILD" -n argo delete wf --all \
  --grace-period=0 --force --wait=false 2>&1 | tail -2 || true

# ---- Step 3: Delete tenant CRs (triggers operator deletionTimestamp) ----
echo "[2/6] Deleting L3/loadtest CRs (finalizers may block)..."
for CRD in tenants environments loopzeroapplications exposedroutes; do
  COUNT=$(kubectl --context="$CTX_MGMT" -n loopzero-system get "$CRD" --no-headers 2>/dev/null | grep -c "$RES_PREFIX" || true)
  if [ "$COUNT" -gt 0 ]; then
    echo "  $CRD: deleting $COUNT"
    kubectl --context="$CTX_MGMT" -n loopzero-system get "$CRD" --no-headers \
      | grep "$RES_PREFIX" | awk '{print $1}' \
      | xargs -r kubectl --context="$CTX_MGMT" -n loopzero-system delete "$CRD" \
          --wait=false --grace-period=0 2>&1 | tail -1 || true
  fi
done

# ---- Step 4: Force-null finalizers on stuck CRs (3 passes) ----
echo "[3/6] Force-null finalizers (3 passes)..."
for i in 1 2 3; do
  for CRD in tenants environments loopzeroapplications exposedroutes applications.argoproj.io; do
    kubectl --context="$CTX_MGMT" -n loopzero-system get "$CRD" --no-headers 2>/dev/null \
      | grep "$RES_PREFIX" | awk '{print $1}' | while read -r name; do
      kubectl --context="$CTX_MGMT" -n loopzero-system patch "$CRD" "$name" \
        --type=json -p='[{"op":"replace","path":"/metadata/finalizers","value":null}]' \
        2>/dev/null || true
    done
  done
  sleep 3
done

# ---- Step 5: Force-finalize runtime namespaces (stuck Terminating) ----
echo "[4/6] Force-finalize runtime namespaces..."
NS_PREFIX="loopzero-${RES_PREFIX}"
kubectl --context="$CTX_RUNTIME" get ns --no-headers 2>/dev/null \
  | grep "$NS_PREFIX" | awk '{print $1}' | while read -r ns; do
  kubectl --context="$CTX_RUNTIME" get ns "$ns" -o json 2>/dev/null \
    | jq '.spec.finalizers = []' \
    | kubectl --context="$CTX_RUNTIME" replace --raw "/api/v1/namespaces/$ns/finalize" -f - 2>/dev/null || true
done

# ---- Step 6: Database cleanup (atomic transaction with FK order) ----
echo "[5/6] Database cleanup..."
DB_URL=$(kubectl --context="$CTX_MGMT" -n loopzero-system get secret loopzero-api-secrets -o json \
  | jq -r '.data.DATABASE_URL' | base64 -d)
CLEAN_URL="${DB_URL%%\?*}?sslmode=require"

# Use a unique job name to avoid conflicts with concurrent runs
JOB="psql-evict-$(date +%s)"
SQL="BEGIN; \
DELETE FROM environment_targets WHERE tenant_id LIKE '${PATTERN}'; \
DELETE FROM releases WHERE build_id IN (SELECT id FROM builds WHERE tenant_id LIKE '${PATTERN}'); \
DELETE FROM build_apps WHERE build_id IN (SELECT id FROM builds WHERE tenant_id LIKE '${PATTERN}'); \
DELETE FROM builds WHERE tenant_id LIKE '${PATTERN}'; \
DELETE FROM projects WHERE tenant_id LIKE '${PATTERN}'; \
DELETE FROM subscriptions WHERE tenant_id LIKE '${PATTERN}'; \
DELETE FROM tenant_members WHERE tenant_id LIKE '${PATTERN}'; \
DELETE FROM tenants WHERE id LIKE '${PATTERN}'; \
COMMIT; \
SELECT 'tenants_remaining', COUNT(*) FROM tenants WHERE id LIKE '${PATTERN}';"

kubectl --context="$CTX_MGMT" -n loopzero-system run "$JOB" --rm -i --restart=Never \
  --image=postgres:16-alpine --timeout=120s \
  --overrides="{\"spec\":{\"securityContext\":{\"runAsNonRoot\":true,\"runAsUser\":999,\"seccompProfile\":{\"type\":\"RuntimeDefault\"}},\"containers\":[{\"name\":\"$JOB\",\"image\":\"postgres:16-alpine\",\"command\":[\"psql\",\"$CLEAN_URL\",\"-c\",\"$SQL\"],\"securityContext\":{\"allowPrivilegeEscalation\":false,\"capabilities\":{\"drop\":[\"ALL\"]},\"readOnlyRootFilesystem\":false}}]}}" \
  2>&1 | tail -8

# ---- Final verification ----
echo "[6/6] Verification..."
echo -n "  Mgmt CRs remaining: "
kubectl --context="$CTX_MGMT" -n loopzero-system get tenants,environments,loopzeroapplications,exposedroutes,applications.argoproj.io --no-headers 2>/dev/null | grep -c "$RES_PREFIX" || echo 0
echo -n "  Runtime ns remaining: "
kubectl --context="$CTX_RUNTIME" get ns --no-headers 2>/dev/null | grep -c "$NS_PREFIX" || echo 0
echo "=== DONE ==="
