#!/usr/bin/env bash
# admin-evict-tenants — bulk cleanup of load-test tenants.
#
# Replaces the kubectl workarounds used iteratively in the L3 v1-v7 load runs.
# Idempotent, deletes in foreign-key order, handles finalizers, safe to re-run.
#
# The Kubernetes steps are best-effort: a resource that is already gone is not
# an error. The database step is authoritative: it runs in one transaction and
# a failure there fails the whole run.
#
# Usage:
#   tools/admin-evict-tenants.sh --pattern 'tn-lt-%'          # default: load-test tenants
#   tools/admin-evict-tenants.sh --pattern 'tn-lt-l3-%'       # only the L3 tenants
#   tools/admin-evict-tenants.sh --pattern '%'  --yes         # EVERY tenant, production included
#
# Plan: 50k-immediate-focus (T-02)
set -euo pipefail

PATTERN="${PATTERN:-tn-lt-%}"
YES=0
CTX_MGMT="${CTX_MGMT:-do-sfo3-loop-runtime-infra-dev-sfo3-mgmt}"
CTX_RUNTIME="${CTX_RUNTIME:-do-sfo3-loop-runtime-infra-dev-sfo3-runtime}"
CTX_BUILD="${CTX_BUILD:-do-sfo3-loop-runtime-infra-dev-sfo3-build}"

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

if [ "$PATTERN" = "%" ]; then
  if [ "$YES" -ne 1 ]; then
    echo "ERROR: pattern='%' matches ALL tenants (including production). Add --yes to confirm." >&2
    exit 1
  fi
elif ! [[ "$PATTERN" =~ ^tn-[a-z0-9-]*%$ ]]; then
  echo "ERROR: --pattern must be a tenant-id prefix followed by % (e.g. 'tn-lt-%'), got '$PATTERN'" >&2
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

# ---- Step 1: stop load-test processes that would recreate tenants (none running is fine) ----
pkill -9 -f "real_deploy_test\|go test ./infra/tests/performance/load" 2>/dev/null || true
sleep 2

# ---- Step 2: Clean argo workflows that may recreate tenants ----
echo "[1/6] Deleting argo workflows..."
kubectl --context="$CTX_BUILD" -n argo delete wf --all \
  --grace-period=0 --force --wait=false 2>&1 | tail -2 || true

# ---- Step 3: Delete tenant CRs (triggers operator deletionTimestamp) ----
echo "[2/6] Deleting L3/loadtest CRs (finalizers may block)..."
for CRD in tenants environments loopruntimeapplications exposedroutes; do
  COUNT=$(kubectl --context="$CTX_MGMT" -n loop-runtime-system get "$CRD" --no-headers 2>/dev/null | grep -c "$RES_PREFIX" || true)
  if [ "$COUNT" -gt 0 ]; then
    echo "  $CRD: deleting $COUNT"
    kubectl --context="$CTX_MGMT" -n loop-runtime-system get "$CRD" --no-headers \
      | { grep "$RES_PREFIX" || true; } | awk '{print $1}' \
      | xargs -r kubectl --context="$CTX_MGMT" -n loop-runtime-system delete "$CRD" \
          --wait=false --grace-period=0 2>&1 | tail -1 || true
  fi
done

# ---- Step 4: Force-null finalizers on stuck CRs (3 passes) ----
echo "[3/6] Force-null finalizers (3 passes)..."
for _ in 1 2 3; do
  for CRD in tenants environments loopruntimeapplications exposedroutes applications.argoproj.io; do
    { kubectl --context="$CTX_MGMT" -n loop-runtime-system get "$CRD" --no-headers 2>/dev/null || true; } \
      | { grep "$RES_PREFIX" || true; } | awk '{print $1}' | while read -r name; do
      kubectl --context="$CTX_MGMT" -n loop-runtime-system patch "$CRD" "$name" \
        --type=json -p='[{"op":"replace","path":"/metadata/finalizers","value":null}]' \
        2>/dev/null || true
    done
  done
  sleep 3
done

# ---- Step 5: Force-finalize runtime namespaces (stuck Terminating) ----
echo "[4/6] Force-finalize runtime namespaces..."
NS_PREFIX="loop-runtime-${RES_PREFIX}"
{ kubectl --context="$CTX_RUNTIME" get ns --no-headers 2>/dev/null || true; } \
  | { grep "$NS_PREFIX" || true; } | awk '{print $1}' | while read -r ns; do
  kubectl --context="$CTX_RUNTIME" get ns "$ns" -o json 2>/dev/null \
    | jq '.spec.finalizers = []' \
    | kubectl --context="$CTX_RUNTIME" replace --raw "/api/v1/namespaces/$ns/finalize" -f - 2>/dev/null || true
done

# ---- Step 6: Database cleanup (one transaction, foreign-key order) ----
# psql runs in a pod that reads DATABASE_URL from the api Secret, so the
# password never appears in the pod spec. The pattern reaches psql as a
# variable (:'pattern'), never spliced into the SQL. ON_ERROR_STOP aborts the
# transaction on the first failing statement, and the pod's exit code is the
# exit code of this step.
echo "[5/6] Database cleanup..."
JOB="psql-evict-$(date +%s)" # unique name: concurrent runs do not collide
POD_OVERRIDES=$(jq -cn --arg job "$JOB" --arg pattern "pattern=$PATTERN" '{
  spec: {
    securityContext: {runAsNonRoot: true, runAsUser: 999, seccompProfile: {type: "RuntimeDefault"}},
    containers: [{
      name: $job,
      image: "postgres:16-alpine",
      stdin: true,
      env: [{name: "DATABASE_URL", valueFrom: {secretKeyRef: {name: "loop-runtime-api-secrets", key: "DATABASE_URL"}}}],
      command: ["sh", "-c",
        "exec psql \"${DATABASE_URL%%\\?*}?sslmode=require\" -v ON_ERROR_STOP=1 -v \"$0\" -f -",
        $pattern],
      securityContext: {allowPrivilegeEscalation: false, capabilities: {drop: ["ALL"]}, readOnlyRootFilesystem: false}
    }]
  }
}')

kubectl --context="$CTX_MGMT" -n loop-runtime-system run "$JOB" --rm -i --restart=Never \
  --image=postgres:16-alpine --timeout=120s --overrides="$POD_OVERRIDES" <<'SQL'
BEGIN;
DELETE FROM environment_targets WHERE tenant_id LIKE :'pattern';
DELETE FROM releases WHERE build_id IN (SELECT id FROM builds WHERE tenant_id LIKE :'pattern');
DELETE FROM build_apps WHERE build_id IN (SELECT id FROM builds WHERE tenant_id LIKE :'pattern');
DELETE FROM builds WHERE tenant_id LIKE :'pattern';
DELETE FROM projects WHERE tenant_id LIKE :'pattern';
DELETE FROM subscriptions WHERE tenant_id LIKE :'pattern';
DELETE FROM tenant_members WHERE tenant_id LIKE :'pattern';
DELETE FROM tenants WHERE id LIKE :'pattern';
COMMIT;
SELECT 'tenants_remaining', COUNT(*) FROM tenants WHERE id LIKE :'pattern';
SQL

# ---- Final verification ----
echo "[6/6] Verification..."
echo -n "  Mgmt CRs remaining: "
kubectl --context="$CTX_MGMT" -n loop-runtime-system get tenants,environments,loopruntimeapplications,exposedroutes,applications.argoproj.io --no-headers 2>/dev/null | grep -c "$RES_PREFIX" || echo 0
echo -n "  Runtime ns remaining: "
kubectl --context="$CTX_RUNTIME" get ns --no-headers 2>/dev/null | grep -c "$NS_PREFIX" || echo 0
echo "=== DONE ==="
