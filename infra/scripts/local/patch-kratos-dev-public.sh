#!/usr/bin/env bash
# T2.3 of plan 2026-06-04-dev-public-domain-canonicalization
#
# Idempotently patches LIVE ory-kratos-config ConfigMap to enforce the
# dev-public env configuration. Used when:
#   (a) operator runs `helmfile sync` without LOOPZERO_HELMFILE_ENV=dev-public,
#       which silently reverts the ConfigMap to localhost:4455 defaults.
#   (b) someone manually edits the ConfigMap and breaks it.
#   (c) cluster restart resurrects an old Kratos pod with stale config.
#
# Patches applied:
#   1. OIDC providers: client_id/client_secret from $LOOPZERO_GITHUB_CLIENT_ID and
#      $LOOPZERO_GOOGLE_CLIENT_ID (the legacy apps gained -dev callback URLs per D5)
#   2. selfservice.methods.oidc.config.base_redirect_uri: https://auth-dev.../
#   3. courier Authorization header: Bearer $LOOPZERO_RESEND_API_KEY
#   4. serve.public.base_url + cors.allowed_origins (dev hosts only)
#   5. cookies.domain + session.cookie.domain: loopzero.example (eTLD+1)
#   6. selfservice.flows.*.ui_url + allowed_return_urls (app-dev URLs)
#   7. logout.after.default_browser_return_url
#
# Restarts Deployment ory-kratos + StatefulSet ory-kratos-courier so the new
# config is read at startup (Kratos does not SIGHUP).
#
# Required env (export from .env):
#   LOOPZERO_GITHUB_CLIENT_ID, LOOPZERO_GITHUB_CLIENT_SECRET
#   LOOPZERO_GOOGLE_CLIENT_ID, LOOPZERO_GOOGLE_CLIENT_SECRET
#   LOOPZERO_RESEND_API_KEY
#
# Optional env:
#   KRATOS_PUBLIC_URL    default https://auth-dev.loopzero.example
#   DASHBOARD_URL        default https://app-dev.loopzero.example
#   CLOUD_URL            default https://cloud-dev.loopzero.example
#   COOKIE_DOMAIN        default loopzero.example
#   KUBECTL_CONTEXT      default k3d-loopzero-mgmt
#   KRATOS_NAMESPACE     default loopzero-auth
#
# Flags:
#   --dry-run      print yaml diff without applying
#   --skip-restart skip rollout-restart (test workflows)
#   --verify       after restart, curl /self-service/login/api and assert
#                  providers exposed + action url uses KRATOS_PUBLIC_URL
#   --check-only   read-only canonical check: alias for --dry-run --skip-restart.
#                  Exits 0 when the desired patches can be derived from .env;
#                  used by dogfood-login Suite 0 T5.3 as Cleanroom-survival
#                  gate (per ADR D5 + EC-14).

set -euo pipefail

# Defaults
: "${KRATOS_PUBLIC_URL:=https://auth-dev.loopzero.example}"
: "${DASHBOARD_URL:=https://app-dev.loopzero.example}"
: "${CLOUD_URL:=https://cloud-dev.loopzero.example}"
: "${COOKIE_DOMAIN:=loopzero.example}"
: "${KUBECTL_CONTEXT:=k3d-loopzero-mgmt}"
: "${KRATOS_NAMESPACE:=loopzero-auth}"
: "${ENV_FILE:=/home/paulo/loopzero/.env}"

DRY_RUN=0
SKIP_RESTART=0
VERIFY=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) DRY_RUN=1; shift ;;
    --skip-restart) SKIP_RESTART=1; shift ;;
    --verify) VERIFY=1; shift ;;
    --check-only) DRY_RUN=1; SKIP_RESTART=1; shift ;;
    -h|--help) sed -n '2,/^set -euo/p' "$0"; exit 0 ;;
    *) printf 'unknown flag: %s\n' "$1" >&2; exit 1 ;;
  esac
done

log() { printf '[patch-kratos-dev-public] %s\n' "$*" >&2; }

# Read LOOPZERO_* env from .env if not already exported
load_env() {
  local key="$1"
  local val="${!key:-}"
  if [[ -z "$val" && -f "$ENV_FILE" ]]; then
    val=$(grep -E "^$key=" "$ENV_FILE" | head -1 | cut -d= -f2-)
  fi
  if [[ -z "$val" ]]; then
    log "ERROR: $key not set in env or $ENV_FILE"
    exit 1
  fi
  printf '%s' "$val"
}

GH_ID=$(load_env LOOPZERO_GITHUB_CLIENT_ID)
GH_SEC=$(load_env LOOPZERO_GITHUB_CLIENT_SECRET)
GG_ID=$(load_env LOOPZERO_GOOGLE_CLIENT_ID)
GG_SEC=$(load_env LOOPZERO_GOOGLE_CLIENT_SECRET)
RESEND=$(load_env LOOPZERO_RESEND_API_KEY)

# apply_patch keeps ALL kubectl I/O in bash (D2); the YAML transform lives in
# patch_kratos_dev_public.py. Flow: kubectl get → .py transform (stdin→stdout) →
# (dry) print diff OR (live) kubectl apply the emitted ConfigMap manifest.
apply_patch() {
  local helper live_yaml
  helper="$(dirname "$0")/patch_kratos_dev_public.py"

  live_yaml="$(kubectl --context "$KUBECTL_CONTEXT" -n "$KRATOS_NAMESPACE" \
    get configmap ory-kratos-config -o jsonpath='{.data.kratos\.yaml}')"

  if [ "$DRY_RUN" = "1" ]; then
    printf '%s' "$live_yaml" | \
      KRATOS_PUBLIC_URL="$KRATOS_PUBLIC_URL" DASHBOARD_URL="$DASHBOARD_URL" \
      CLOUD_URL="$CLOUD_URL" COOKIE_DOMAIN="$COOKIE_DOMAIN" \
      GH_ID="$GH_ID" GH_SEC="$GH_SEC" GG_ID="$GG_ID" GG_SEC="$GG_SEC" \
      RESEND="$RESEND" DRY_RUN=1 KRATOS_NAMESPACE="$KRATOS_NAMESPACE" \
      python3 "$helper"
    return 0
  fi

  printf '%s' "$live_yaml" | \
    KRATOS_PUBLIC_URL="$KRATOS_PUBLIC_URL" DASHBOARD_URL="$DASHBOARD_URL" \
    CLOUD_URL="$CLOUD_URL" COOKIE_DOMAIN="$COOKIE_DOMAIN" \
    GH_ID="$GH_ID" GH_SEC="$GH_SEC" GG_ID="$GG_ID" GG_SEC="$GG_SEC" \
    RESEND="$RESEND" DRY_RUN=0 KRATOS_NAMESPACE="$KRATOS_NAMESPACE" \
    python3 "$helper" | kubectl --context "$KUBECTL_CONTEXT" apply -f -
  echo "ConfigMap patched (7 fixes applied)"
}

main() {
  log "context=$KUBECTL_CONTEXT ns=$KRATOS_NAMESPACE dry_run=$DRY_RUN verify=$VERIFY"
  log "auth=$KRATOS_PUBLIC_URL dashboard=$DASHBOARD_URL cloud=$CLOUD_URL cookie=$COOKIE_DOMAIN"

  apply_patch

  if [[ $DRY_RUN -eq 1 ]]; then
    log "dry-run complete (no kubectl apply)"
    return 0
  fi

  if [[ $SKIP_RESTART -eq 0 ]]; then
    log "Restarting Kratos deployment + courier StatefulSet..."
    kubectl --context "$KUBECTL_CONTEXT" -n "$KRATOS_NAMESPACE" \
      rollout restart deployment ory-kratos
    kubectl --context "$KUBECTL_CONTEXT" -n "$KRATOS_NAMESPACE" \
      rollout restart statefulset ory-kratos-courier
    kubectl --context "$KUBECTL_CONTEXT" -n "$KRATOS_NAMESPACE" \
      rollout status deployment ory-kratos --timeout=90s
  fi

  if [[ $VERIFY -eq 1 ]]; then
    log "Verifying via $KRATOS_PUBLIC_URL/self-service/login/api (retries up to 60s)"
    local got=""
    local attempt
    for attempt in 1 2 3 4 5 6 7 8 9 10; do
      sleep 6
      got=$(curl -fsS --max-time 5 "$KRATOS_PUBLIC_URL/self-service/login/api" \
        -H "Accept: application/json" 2>/dev/null || echo "")
      if [[ -n "$got" ]]; then
        log "  reachable on attempt $attempt"
        break
      fi
      log "  attempt $attempt: not reachable yet (port-forward may be re-binding)..."
    done

    if [[ -z "$got" ]]; then
      log "  FAIL: $KRATOS_PUBLIC_URL not reachable after 60s — check Caddy + port-forward state"
      log "        (kubectl rollout restart drops kubectl port-forward; respawn it)"
      exit 1
    fi

    printf '%s' "$got" | python3 -c "
import sys, json
d=json.load(sys.stdin)
nodes=d.get('ui',{}).get('nodes',[])
providers=sorted({n['attributes'].get('value','') for n in nodes if n['attributes'].get('name')=='provider'})
action=d.get('ui',{}).get('action','')
ok=True
expected_action_prefix='$KRATOS_PUBLIC_URL/self-service/login'
if not action.startswith(expected_action_prefix):
    print(f'  FAIL: action url is {action} (expected prefix {expected_action_prefix})'); ok=False
if not providers:
    print(f'  FAIL: no OIDC providers exposed'); ok=False
else:
    print(f'  OK: providers={providers}, action prefix matches')
sys.exit(0 if ok else 1)"
  fi
}

main "$@"
