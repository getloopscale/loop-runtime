#!/usr/bin/env bash
# Canonical post-Cleanroom seed for the Layer 2 ESO sync broker chain
# (J-22 plan v1.2 D3). Without this script, dogfood-secrets-openbao
# Scenarios B/C/F FAIL because the ExternalSecret CRs loopzero-api emits
# cannot reconcile (CSS missing OR token missing OR webhook label missing).
#
# Materializes the canonical chain in 4 layers:
#
# Layer 1 — Generate canonical broker token + persist in mgmt cluster
#   loopzero-system/loopzero-broker-token Secret with keys {token, LOOPZERO_BROKER_TOKEN}
#   (loopzero-api chart envFrom secretRef matches LOOPZERO_BROKER_TOKEN).
#
# Layer 2 — Mirror token to workload cluster's external-secrets ns with
#   the canonical `external-secrets.io/type=webhook` label (required by
#   ESO v1beta1 webhook auth section validator).
#
# Layer 3 — Install loopzero-external-secrets chart in workload cluster's
#   external-secrets ns with broker.enabled=true + apiURL pointing at
#   mgmt cluster's loopzero-api NodePort (cross-cluster via mgmt server-0).
#
# Layer 4 — Restart loopzero-api so envFrom picks up LOOPZERO_BROKER_TOKEN +
#   wait for ClusterSecretStore Ready=True.
#
# Idempotent — re-running with the same token preserves state. Re-running
# with --rotate generates a new token + restarts.
#
# Per cleanroom-up-live-test-mandate.md INQUEBRÁVEL: this script is part
# of the canonical post-Cleanroom chain alongside seed-admin-loopzero-dev.sh
# + loopzero-ops seed github-app-dev + patch-kratos-dev-public.

set -euo pipefail

MGMT_CONTEXT="${KUBE_CONTEXT_MGMT:-k3d-loopzero-mgmt}"
WORKLOAD_CONTEXT="${KUBE_CONTEXT_WORKLOAD:-k3d-loopzero-workload}"
LOOPZERO_NAMESPACE="${LOOPZERO_NAMESPACE:-loopzero-system}"
ESO_NAMESPACE="${ESO_NAMESPACE:-external-secrets}"

ROTATE=0
VERIFY=0
DRY_RUN=0
for arg in "$@"; do
  case "$arg" in
    --rotate) ROTATE=1 ;;
    --verify) VERIFY=1 ;;
    --dry-run) DRY_RUN=1 ;;
    --help|-h)
      sed -n '2,/^set -euo pipefail/p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) echo "Unknown flag: $arg" >&2; exit 1 ;;
  esac
done

log() { printf "%s [seed-broker-canonical] %s\n" "$(date -u +%H:%M:%SZ)" "$*"; }
fail() { log "FAIL: $*"; exit 1; }

# Pre-flight
kubectl --context "$MGMT_CONTEXT" get ns "$LOOPZERO_NAMESPACE" >/dev/null 2>&1 \
  || fail "mgmt context $MGMT_CONTEXT unreachable OR ns $LOOPZERO_NAMESPACE missing"
kubectl --context "$WORKLOAD_CONTEXT" cluster-info >/dev/null 2>&1 \
  || fail "workload context $WORKLOAD_CONTEXT unreachable"

# Layer 1 — Get or generate canonical token
EXISTING_TOKEN=""
if kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" get secret loopzero-broker-token >/dev/null 2>&1; then
  EXISTING_TOKEN=$(kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" \
    get secret loopzero-broker-token -o jsonpath='{.data.token}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
fi

if [[ "$ROTATE" == "1" || -z "$EXISTING_TOKEN" ]]; then
  TOKEN=$(openssl rand -hex 32)
  log "Layer 1: $([ "$ROTATE" == "1" ] && echo rotated || echo generated) canonical token (len=${#TOKEN})"
else
  TOKEN="$EXISTING_TOKEN"
  log "Layer 1: reusing existing canonical token (len=${#TOKEN})"
fi

if [[ "$DRY_RUN" == "1" ]]; then
  log "DRY_RUN: would patch mgmt + workload Secret + install CSS + restart loopzero-api"
  exit 0
fi

# Layer 1 apply — mgmt cluster Secret with both `token` (legacy) + `LOOPZERO_BROKER_TOKEN`
# (canonical envFrom key per loopzero-api chart deployment.yaml)
TOKEN_B64=$(printf "%s" "$TOKEN" | base64 -w 0)
cat <<EOF | kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" apply -f - >/dev/null
apiVersion: v1
kind: Secret
metadata:
  name: loopzero-broker-token
  labels:
    app.kubernetes.io/name: loopzero-broker-token
    app.kubernetes.io/managed-by: seed-broker-canonical
type: Opaque
data:
  token: $TOKEN_B64
  LOOPZERO_BROKER_TOKEN: $TOKEN_B64
EOF
log "Layer 1: mgmt loopzero-system/loopzero-broker-token applied (canonical keys: token + LOOPZERO_BROKER_TOKEN)"

# Layer 2 — Mirror to workload cluster's ESO ns with canonical webhook label
kubectl --context "$WORKLOAD_CONTEXT" get ns "$ESO_NAMESPACE" >/dev/null 2>&1 \
  || kubectl --context "$WORKLOAD_CONTEXT" create ns "$ESO_NAMESPACE" >/dev/null

cat <<EOF | kubectl --context "$WORKLOAD_CONTEXT" -n "$ESO_NAMESPACE" apply -f - >/dev/null
apiVersion: v1
kind: Secret
metadata:
  name: loopzero-broker-token
  labels:
    external-secrets.io/type: webhook
    app.kubernetes.io/name: loopzero-broker-token
    app.kubernetes.io/managed-by: seed-broker-canonical
type: Opaque
data:
  token: $TOKEN_B64
EOF
log "Layer 2: workload $ESO_NAMESPACE/loopzero-broker-token applied (canonical label external-secrets.io/type=webhook)"

# Layer 3 — Install/upgrade loopzero-broker CSS in workload cluster
# Cross-cluster: mgmt server-0 IP + loopzero-api NodePort 30001
MGMT_SERVER_IP=$(docker network inspect k3d-loopzero 2>/dev/null | python3 -c "
import json,sys
for net in json.load(sys.stdin):
    for c in net.get('Containers', {}).values():
        if c['Name'] == 'k3d-loopzero-mgmt-server-0':
            print(c['IPv4Address'].split('/')[0]); break
" || echo "")
[[ -n "$MGMT_SERVER_IP" ]] || fail "Layer 3: cannot resolve k3d-loopzero-mgmt-server-0 IP from docker network k3d-loopzero"
log "Layer 3: cross-cluster broker.apiURL = http://$MGMT_SERVER_IP:30001"

CHART_PATH="${LOOPZERO_CHART_PATH:-$(cd "$(dirname "$0")/../../helm/loopzero-external-secrets" && pwd)}"
[[ -d "$CHART_PATH" ]] || fail "Layer 3: chart path missing: $CHART_PATH"

# Ensure loopzero-build ns exists (chart fails without it — observed iter 1)
kubectl --context "$WORKLOAD_CONTEXT" get ns loopzero-build >/dev/null 2>&1 \
  || kubectl --context "$WORKLOAD_CONTEXT" create ns loopzero-build >/dev/null

helm upgrade --install loopzero-broker "$CHART_PATH" \
  --kube-context "$WORKLOAD_CONTEXT" \
  -n "$ESO_NAMESPACE" \
  --create-namespace \
  --set envName=dev-public \
  --set broker.enabled=true \
  --set broker.apiURL="http://$MGMT_SERVER_IP:30001" \
  --set broker.tokenSecret=loopzero-broker-token \
  --set broker.cellID=cell-default \
  >/dev/null
log "Layer 3: loopzero-external-secrets chart installed (broker.enabled=true, apiURL points at mgmt cross-cluster)"

# Layer 4 — Restart loopzero-api so envFrom picks LOOPZERO_BROKER_TOKEN
# Capture pre-restart timestamp so post-restart log verification scopes
# only to the FRESH pod (avoids matching legacy "broker disabled" lines
# from pre-token startup).
RESTART_SINCE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" rollout restart deployment/loopzero-api >/dev/null
kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" rollout status deployment/loopzero-api --timeout=120s >/dev/null
log "Layer 4: loopzero-api restarted (env LOOPZERO_BROKER_TOKEN now wired via envFrom)"

# Verify
if [[ "$VERIFY" == "1" ]]; then
  log "verify: waiting up to 60s for CSS Ready=True + loopzero-api broker startup"
  CSS_READY=0
  for _ in $(seq 1 12); do
    if kubectl --context "$WORKLOAD_CONTEXT" get clustersecretstore loopzero-broker -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null | grep -q "True"; then
      CSS_READY=1; break
    fi
    sleep 5
  done
  if [[ "$CSS_READY" == "1" ]]; then
    log "OK verify: ClusterSecretStore loopzero-broker Ready=True"
  else
    log "FAIL verify: ClusterSecretStore loopzero-broker NOT Ready after 60s"
    log "  diagnose: kubectl --context $WORKLOAD_CONTEXT describe clustersecretstore loopzero-broker"
    exit 1
  fi
  # Post-restart log check — scope to lines AFTER RESTART_SINCE so legacy
  # "broker disabled" warnings from the pre-token pod don't false-fail.
  BROKER_DISABLED=0
  for _ in 1 2 3 4 5; do
    while IFS= read -r POD; do
      [[ -z "$POD" ]] && continue
      if kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" logs "$POD" --since-time="$RESTART_SINCE" 2>/dev/null \
           | grep -q "broker disabled"; then
        BROKER_DISABLED=1
        break 2
      fi
    done < <(kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" get pods -l app=loopzero-api \
              -o jsonpath='{range .items[?(@.status.phase=="Running")]}{.metadata.name}{"\n"}{end}')
    sleep 2
  done
  if [[ "$BROKER_DISABLED" == "0" ]]; then
    log "OK verify: loopzero-api broker NOT disabled in post-restart logs (LOOPZERO_BROKER_TOKEN wired)"
  else
    log "FAIL verify: loopzero-api log says 'broker disabled' in post-restart window — env var missing"
    exit 1
  fi
fi

log "DONE — canonical broker chain seeded (4 layers)"
