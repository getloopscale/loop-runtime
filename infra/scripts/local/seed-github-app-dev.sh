#!/usr/bin/env bash
# Canonical GitHub App credentials seed for dev-public LIVE testing.
#
# Reads ~/.loopzero/dev-credentials.json (mode 0600 enforced pre-read) and creates
# two K8s Secrets in the loopzero-system namespace:
#
#   loopzero-api-github   key=GITHUB_APP_PRIVATE_KEY  (PKCS1 RSA PEM)
#   loopzero-github-webhook key=value                  (HMAC-SHA256 shared secret)
#
# Both are consumed by loopzero-api via envFrom/secretKeyRef in
# infra/helm/loopzero-api/templates/deployment.yaml (canonical chart paths
# added 2026-06-15 alongside GITHUB_APP_PRIVATE_KEY + GITHUB_WEBHOOK_SECRET
# bindings). The same secrets in production are materialized via ESO from
# OpenBao kv/loopzero/dev/api/secrets/github (canonical end-state per CLAUDE.md
# § Dashboard Journeys J-09); this script is the dev-public bridge while
# the ESO ClusterSecretStore deployment in mgmt cluster is pending
# (architectural finding documented in docs/operations/option-b-mgmt-helmfile-eso-deploy.md).
#
# Idempotent: kubectl create --dry-run=client -o yaml | kubectl apply -f -.
# Triggers loopzero-api rollout when secrets change (env propagation requires
# pod restart; deployment.yaml does not inject pod annotation hash).
#
# Provenance:
#   - Per cleanroom-up-live-test-mandate.md INQUEBRÁVEL: post-Cleanroom seed
#     chain MUST include this script alongside vault-bootstrap-dev.sh +
#     seed-admin-loopzero-dev.sh + patch-kratos-dev-public.sh.
#   - Cross-ref: feedback_post_cleanroom_seed_admin_canonical (memory)
#   - Cross-ref: feedback_no_hardcode_gate (memory) — secrets NEVER inlined,
#     always read from ~/.loopzero/dev-credentials.json mode 0600 SoT.
#
# Flags:
#   --dry-run   render planned actions, exit 0 without mutation
#   --verify    after apply, restart loopzero-api and assert webhook handler
#               registered in startup logs
#   --help      print this header
#
# Exit codes:
#   0 OK (idempotent apply succeeded; optionally verify passed)
#   1 missing credentials file OR fields
#   2 kubectl/cluster unreachable

set -euo pipefail

# ---- flags
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

CONTEXT="${KUBE_CONTEXT:-k3d-loopzero-mgmt}"
NAMESPACE="${LOOPZERO_NAMESPACE:-loopzero-system}"
CREDS_PATH="${LOOPZERO_DEV_CREDENTIALS:-$HOME/.loopzero/dev-credentials.json}"

log() { printf "%s [seed-github-app-dev] %s\n" "$(date -u +%H:%M:%SZ)" "$*"; }
fail() { log "FAIL: $*"; exit 1; }

# ---- pre-flight
[[ -r "$CREDS_PATH" ]] || fail "credentials missing: $CREDS_PATH (expected JSON with .github.{app_id,app_private_key,webhook_secret})"

# Mode check — file MUST be 0600 (operator security floor)
PERMS=$(stat -c '%a' "$CREDS_PATH" 2>/dev/null || stat -f '%A' "$CREDS_PATH" 2>/dev/null || echo "")
if [[ -n "$PERMS" && "$PERMS" != "600" ]]; then
  log "WARN: $CREDS_PATH mode=$PERMS (expected 600); enforcing 600"
  chmod 0600 "$CREDS_PATH"
fi

kubectl --context "$CONTEXT" get ns "$NAMESPACE" >/dev/null 2>&1 || fail "namespace $NAMESPACE not found in context $CONTEXT (cluster reachable?)"

# ---- extract canonical fields from JSON
extract() {
  python3 -c "import json,sys; d=json.load(open('$CREDS_PATH')); v=d$1; sys.stdout.write(v if v else '')" 2>/dev/null || true
}

APP_ID=$(extract "['github']['app_id']")
APP_PRIVKEY=$(extract "['github']['app_private_key']")
WEBHOOK_SECRET=$(extract "['github']['webhook_secret']")

[[ -n "$APP_ID" ]] || fail "github.app_id missing in $CREDS_PATH"
[[ -n "$APP_PRIVKEY" ]] || fail "github.app_private_key missing in $CREDS_PATH"
[[ -n "$WEBHOOK_SECRET" ]] || fail "github.webhook_secret missing in $CREDS_PATH"

# Validate PEM parses as PKCS1 (matches api/internal/github/appclient.go:75
# x509.ParsePKCS1PrivateKey expectation).
if ! echo "$APP_PRIVKEY" | openssl rsa -noout -check >/dev/null 2>&1; then
  fail "github.app_private_key is not a valid PKCS1 RSA PEM (api/internal/github/appclient.go expects ParsePKCS1PrivateKey)"
fi

log "OK pre-flight: app_id=$APP_ID, pem_len=${#APP_PRIVKEY}, webhook_secret_len=${#WEBHOOK_SECRET}"

# ---- dry-run short-circuit
if [[ "$DRY_RUN" == "1" ]]; then
  log "DRY_RUN: would create/update loopzero-api-github (PKCS1 RSA PEM) + loopzero-github-webhook (HMAC secret) in $NAMESPACE"
  exit 0
fi

# ---- apply loopzero-api-github
PEM_TMP=$(mktemp -t loopzero-github-app-pem.XXXXXX)
trap 'rm -f "$PEM_TMP"' EXIT INT TERM
chmod 0600 "$PEM_TMP"
printf "%s" "$APP_PRIVKEY" > "$PEM_TMP"

kubectl --context "$CONTEXT" -n "$NAMESPACE" create secret generic loopzero-api-github \
  --from-file=GITHUB_APP_PRIVATE_KEY="$PEM_TMP" \
  --dry-run=client -o yaml | kubectl --context "$CONTEXT" -n "$NAMESPACE" apply -f - >/dev/null
log "OK loopzero-api-github applied (key=GITHUB_APP_PRIVATE_KEY)"

# ---- apply loopzero-github-webhook
kubectl --context "$CONTEXT" -n "$NAMESPACE" create secret generic loopzero-github-webhook \
  --from-literal=value="$WEBHOOK_SECRET" \
  --dry-run=client -o yaml | kubectl --context "$CONTEXT" -n "$NAMESPACE" apply -f - >/dev/null
log "OK loopzero-github-webhook applied (key=value)"

# ---- optional verify
if [[ "$VERIFY" == "1" ]]; then
  log "verify: restarting loopzero-api to pick up env (envFrom does NOT auto-rollout)"
  # Capture pre-restart timestamp (RFC3339) so we only grep logs emitted by
  # the freshly-rolled pod — avoids tail-size races on hot clusters.
  SINCE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  kubectl --context "$CONTEXT" -n "$NAMESPACE" rollout restart deployment/loopzero-api >/dev/null
  kubectl --context "$CONTEXT" -n "$NAMESPACE" rollout status deployment/loopzero-api --timeout=120s >/dev/null
  # Two-stage poll: handler registration is a startup-only log line (~200ms
  # after readiness). Try up to 10s for it to land.
  REGISTERED=0
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    # Iterate Ready pods individually — `kubectl logs -l` does not span
    # rollout cleanly when old pods are Terminating and new ones not yet
    # fully Ready. Per-pod logs --since-time honors the start window.
    while IFS= read -r POD; do
      [[ -z "$POD" ]] && continue
      if kubectl --context "$CONTEXT" -n "$NAMESPACE" logs "$POD" --since-time="$SINCE" 2>/dev/null \
           | grep -q "github webhook: handler registered"; then
        REGISTERED=1
        break 2
      fi
    done < <(kubectl --context "$CONTEXT" -n "$NAMESPACE" get pods -l app=loopzero-api \
              -o jsonpath='{range .items[?(@.status.phase=="Running")]}{.metadata.name}{"\n"}{end}')
    sleep 1
  done
  if [[ "$REGISTERED" == "1" ]]; then
    log "OK verify: webhook handler registered (post-rollout since $SINCE)"
  else
    log "FAIL verify: 'github webhook: handler registered' not found in loopzero-api logs since $SINCE"
    log "      diagnostic: kubectl -n $NAMESPACE logs -l app=loopzero-api --since-time=$SINCE | grep -iE 'github|webhook'"
    exit 1
  fi
fi

log "DONE"
