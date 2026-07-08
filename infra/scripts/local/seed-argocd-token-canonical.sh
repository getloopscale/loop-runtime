#!/usr/bin/env bash
# seed-argocd-token-canonical.sh
#
# T1.2 of plan m2-promote-11-canonical-seeds (v1.1 SHIPPABLE 95.2).
# Canonical ArgoCD admin token mint + sync into loopzero-system/loopzero-api-tokens
# Secret. Used by release_convergence_loop in loopzero-api to trigger
# POST /api/v1/applications/{name}/sync on ArgoCD REST.
#
# Distinct from `loopzero-ops seed argocd-gitea-creds` (Step 6b — repo-creds /
# git auth). This script handles the admin-side OIDC-bypass token.
#
# Idempotent: token mint is name-scoped (canonical-seed); re-running mints
# a fresh token (ArgoCD allows multiple tokens per account). Secret apply
# is idempotent via --dry-run=client | apply.
#
# Flags:
#   --verify   assert argocd-token key present in Secret post-apply
#   --dry-run  print intended actions without mutation
#   --help|-h  print this header
#
# Pre-flight: argocd-initial-admin-secret must exist in argocd ns (created by
# helmfile install of argo-cd chart). Cluster: k3d-loopzero-mgmt only (Step 13 is
# env-gated dev-public + standalone-multi in seed-post-cleanroom-all.sh).
set -euo pipefail

MGMT_CONTEXT="${KUBE_CONTEXT:-k3d-loopzero-mgmt}"
VERIFY=0
DRY_RUN=0
for arg in "$@"; do
  case "$arg" in
    --verify) VERIFY=1 ;;
    --dry-run) DRY_RUN=1 ;;
    --help|-h)
      sed -n '2,/^set -euo pipefail/p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) echo "Unknown flag: $arg" >&2; exit 1 ;;
  esac
done

log() { printf "%s [seed-argocd-token] %s\n" "$(date -u +%H:%M:%SZ)" "$*"; }

# Read ArgoCD admin password from initial-admin-secret
PW=$(kubectl --context "$MGMT_CONTEXT" -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' 2>/dev/null | base64 -d || echo "")
if [[ -z "$PW" ]]; then
  log "FAIL — argocd-initial-admin-secret not found in argocd namespace (run task setup first)"
  exit 1
fi

if [[ "$DRY_RUN" == "1" ]]; then
  log "DRY: mint argocd token via REST + sync to loopzero-api-tokens Secret + rollout restart loopzero-api"
  exit 0
fi

# Port-forward + REST mint (more reliable than CLI for headless ops).
# Canonical 2026-06-23: argocd-server runs in --insecure mode in k3d
# (svc port 80 → targetPort 8080 plaintext). HTTPS svc port 443 also
# targets 8080 with NO TLS termination → curl HTTPS handshake fails.
# Use HTTP via svc port 80 to match the actual argocd-server protocol.
kubectl --context "$MGMT_CONTEXT" -n argocd port-forward svc/argocd-server 14446:80 >/dev/null 2>&1 &
PF_PID=$!
trap "kill $PF_PID 2>/dev/null || true" EXIT INT TERM

# Wait for port-forward to fully establish + argocd-server to accept on the
# forwarded port. Canonical 2026-06-23: --retry-connrefused alone is
# insufficient because kubectl port-forward "Forwarding from..." message
# appears BEFORE the underlying pod->localhost path is reliably routable.
# Explicit poll matches the manual probe pattern that returns 200 OK at
# ~6s steady-state.
for i in $(seq 1 30); do
  if curl -s --max-time 2 -o /dev/null -w '%{http_code}' "http://localhost:14446/api/v1/version" 2>/dev/null | grep -qE '^(200|401)$'; then
    break
  fi
  sleep 1
done

# Step 1: login → session token
SESSION=$(curl -s --fail \
  "http://localhost:14446/api/v1/session" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"admin\",\"password\":\"$PW\"}" \
  | python3 -c 'import sys, json; print(json.load(sys.stdin).get("token", ""))')
if [[ -z "$SESSION" ]]; then
  log "FAIL — session login returned empty token (check argocd-server reachability)"
  exit 1
fi

# Step 2: generate account token (account=loopzero-api per canonical contract)
# curl --fail returns non-zero on HTTP 4xx/5xx (EC-8 absorption).
TOKEN=$(curl -s --fail -X POST "http://localhost:14446/api/v1/account/loopzero-api/token" \
  -H "Authorization: Bearer $SESSION" \
  -H "Content-Type: application/json" \
  -d '{"name":"canonical-seed"}' \
  | python3 -c 'import sys, json; print(json.load(sys.stdin).get("token", ""))')
if [[ -z "$TOKEN" ]]; then
  log "FAIL — account token mint returned empty (check loopzero-api account exists)"
  exit 1
fi

# Step 3: sync to loopzero-api-tokens Secret in loopzero-system
kubectl --context "$MGMT_CONTEXT" -n loopzero-system create secret generic loopzero-api-tokens \
  --from-literal=argocd-token="$TOKEN" \
  --dry-run=client -o yaml | kubectl --context "$MGMT_CONTEXT" apply -f -

# Step 4: restart loopzero-api so env reload
kubectl --context "$MGMT_CONTEXT" -n loopzero-system rollout restart deploy/loopzero-api
kubectl --context "$MGMT_CONTEXT" -n loopzero-system rollout status deploy/loopzero-api --timeout=60s

if [[ "$VERIFY" == "1" ]]; then
  log "verifying argocd-token key in Secret..."
  if kubectl --context "$MGMT_CONTEXT" -n loopzero-system get secret loopzero-api-tokens -o jsonpath='{.data.argocd-token}' 2>/dev/null | grep -q .; then
    log "PASS — argocd-token present"
  else
    log "FAIL — argocd-token key missing in loopzero-api-tokens Secret"
    exit 1
  fi
fi
