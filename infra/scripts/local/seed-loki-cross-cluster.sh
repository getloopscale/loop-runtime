#!/usr/bin/env bash
# Canonical post-Cleanroom seed for Loki cross-cluster log streaming chain
# (J-17 / J-19 / dogfood-deployments Scenario C). Without this script,
# dogfood-deployments FAILs at Scenario C with "Loki returns 0 ::loopzero::
# markers" because:
#
# 1. Build cluster Promtail's `clients.url` defaults to mgmt cluster
#    NodePort 30094 (cross-cluster) but mgmt cluster has NO Loki, so the
#    push fails connection-refused. Logs are scraped but discarded.
#
# 2. loopzero-api's LOKI_URL defaults to in-mgmt-cluster DNS `loki.monitoring`
#    but Loki is in BUILD cluster — DNS resolves nothing.
#
# Canonical fix (post-Cleanroom):
#
# Layer 1 — Patch build-cluster Promtail Secret to point at LOCAL Loki
#   `http://loki.monitoring.svc.cluster.local:3100/loki/api/v1/push`.
#   Restart Promtail daemonset.
#
# Layer 2 — Convert build-cluster Loki Service to NodePort so loopzero-api in
#   mgmt cluster can scrape across the k3d docker bridge. Allocates 30120
#   if not yet assigned.
#
# Layer 3 — Set loopzero-api LOKI_URL env to build-cluster server-0 IP +
#   NodePort. Restart loopzero-api.
#
# Layer 4 — Verify Promtail pushes succeed (no connection-refused logs in
#   recent window) AND loopzero-api can reach Loki (HTTP 200 on /ready).
#
# Per memory [[j17-aprendizado-promtail-loki-pipeline-canonical-via-helmfile]]:
# the END-STATE canonical is helmfile values overlay per-env, NOT
# kubectl patch. This script is the DEV-PUBLIC bridge until that
# overlay ships. Helmfile values restoration would make this script
# unnecessary; until then, this is canonical post-Cleanroom seed.

set -euo pipefail

MGMT_CONTEXT="${KUBE_CONTEXT_MGMT:-k3d-loopzero-mgmt}"
BUILD_CONTEXT="${KUBE_CONTEXT_BUILD:-k3d-loopzero-build}"
LOOPZERO_NAMESPACE="${LOOPZERO_NAMESPACE:-loopzero-system}"
MONITORING_NAMESPACE="${MONITORING_NAMESPACE:-monitoring}"
LOKI_NODEPORT="${LOKI_NODEPORT:-30120}"

DRY_RUN=0
VERIFY=0
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    --verify) VERIFY=1 ;;
    --help|-h)
      sed -n '2,/^set -euo pipefail/p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) echo "Unknown flag: $arg" >&2; exit 1 ;;
  esac
done

log() { printf "%s [seed-loki-cross-cluster] %s\n" "$(date -u +%H:%M:%SZ)" "$*"; }
fail() { log "FAIL: $*"; exit 1; }

# Pre-flight
kubectl --context "$BUILD_CONTEXT" -n "$MONITORING_NAMESPACE" \
  get svc loki >/dev/null 2>&1 \
  || fail "build cluster Loki service missing — run task setup first"
kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" \
  get deploy loopzero-api >/dev/null 2>&1 \
  || fail "mgmt loopzero-api deployment missing"

if [[ "$DRY_RUN" == "1" ]]; then
  log "DRY_RUN: would patch Promtail URL + Loki NodePort + loopzero-api LOKI_URL + restart"
  exit 0
fi

# Layer 1 — Promtail Secret patch
CURRENT_PROMTAIL=$(kubectl --context "$BUILD_CONTEXT" -n "$MONITORING_NAMESPACE" \
  get secret promtail -o jsonpath='{.data.promtail\.yaml}' | base64 -d)
NEW_PROMTAIL=$(printf '%s' "$CURRENT_PROMTAIL" | sed -E 's|http://[0-9.]+:[0-9]+/loki/api/v1/push|http://loki.monitoring.svc.cluster.local:3100/loki/api/v1/push|g')

if [[ "$CURRENT_PROMTAIL" == "$NEW_PROMTAIL" ]]; then
  log "Layer 1: Promtail clients.url already canonical (no patch needed)"
else
  NEW_B64=$(printf '%s' "$NEW_PROMTAIL" | base64 -w 0)
  kubectl --context "$BUILD_CONTEXT" -n "$MONITORING_NAMESPACE" \
    patch secret promtail --type=json \
    -p="[{\"op\":\"replace\",\"path\":\"/data/promtail.yaml\",\"value\":\"$NEW_B64\"}]" >/dev/null
  kubectl --context "$BUILD_CONTEXT" -n "$MONITORING_NAMESPACE" \
    rollout restart daemonset/promtail >/dev/null
  log "Layer 1: Promtail clients.url patched to local Loki + daemonset restarted"
fi

# Layer 2 — Loki NodePort
CURRENT_TYPE=$(kubectl --context "$BUILD_CONTEXT" -n "$MONITORING_NAMESPACE" \
  get svc loki -o jsonpath='{.spec.type}')
if [[ "$CURRENT_TYPE" == "NodePort" ]]; then
  CURRENT_NP=$(kubectl --context "$BUILD_CONTEXT" -n "$MONITORING_NAMESPACE" \
    get svc loki -o jsonpath='{.spec.ports[?(@.port==3100)].nodePort}')
  log "Layer 2: Loki already NodePort (port=$CURRENT_NP)"
  LOKI_NODEPORT="$CURRENT_NP"
else
  kubectl --context "$BUILD_CONTEXT" -n "$MONITORING_NAMESPACE" \
    patch svc loki --type=json \
    -p="[{\"op\":\"replace\",\"path\":\"/spec/type\",\"value\":\"NodePort\"},{\"op\":\"add\",\"path\":\"/spec/ports/0/nodePort\",\"value\":$LOKI_NODEPORT}]" >/dev/null
  log "Layer 2: Loki Service converted to NodePort (port=$LOKI_NODEPORT)"
fi

# Layer 3 — loopzero-api LOKI_URL
BUILD_SERVER_IP=$(docker network inspect k3d-loopzero 2>/dev/null | python3 -c "
import json,sys
for net in json.load(sys.stdin):
    for c in net.get('Containers', {}).values():
        if c['Name'] == 'k3d-loopzero-build-server-0':
            print(c['IPv4Address'].split('/')[0]); break
" || echo "")
[[ -n "$BUILD_SERVER_IP" ]] || fail "Layer 3: cannot resolve k3d-loopzero-build-server-0 IP"

CANONICAL_LOKI_URL="http://$BUILD_SERVER_IP:$LOKI_NODEPORT"
CURRENT_LOKI_URL=$(kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" \
  get deploy loopzero-api -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="LOKI_URL")].value}' 2>/dev/null || echo "")

if [[ "$CURRENT_LOKI_URL" == "$CANONICAL_LOKI_URL" ]]; then
  log "Layer 3: loopzero-api LOKI_URL already canonical ($CANONICAL_LOKI_URL)"
else
  kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" \
    set env deployment/loopzero-api LOKI_URL="$CANONICAL_LOKI_URL" >/dev/null
  kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" rollout status deployment/loopzero-api --timeout=120s >/dev/null
  log "Layer 3: loopzero-api LOKI_URL canonical ($CANONICAL_LOKI_URL) — restarted"
fi

# Verify
if [[ "$VERIFY" == "1" ]]; then
  # (a) Promtail push success — recent logs should NOT contain connection-refused
  PROMTAIL_ERR=$(kubectl --context "$BUILD_CONTEXT" -n "$MONITORING_NAMESPACE" \
    logs ds/promtail --tail=20 --since=30s 2>/dev/null | grep -c "connection refused" | tr -d '[:space:]' || echo "0")
  if [[ "$PROMTAIL_ERR" -gt 0 ]]; then
    log "FAIL verify: Promtail still reports 'connection refused' in recent logs"
    exit 1
  fi
  log "OK verify: Promtail push chain healthy (no recent connection-refused)"

  # (b) loopzero-api can reach Loki via NodePort
  LOOPZERO_API_REACH=$(kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" \
    exec deploy/loopzero-api -- wget -qO- -S "$CANONICAL_LOKI_URL/ready" 2>&1 | grep -c "200 OK" || echo "0")
  if [[ "$LOOPZERO_API_REACH" -gt 0 ]]; then
    log "OK verify: loopzero-api reaches Loki cross-cluster (HTTP 200 on /ready)"
  else
    log "FAIL verify: loopzero-api cannot reach Loki at $CANONICAL_LOKI_URL"
    exit 1
  fi
fi

log "DONE — Loki cross-cluster chain seeded (3 layers)"
