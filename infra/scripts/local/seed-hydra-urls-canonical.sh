#!/usr/bin/env bash
# Canonical post-Cleanroom seed for Ory Hydra URL configuration. Without
# this, dogfood-device Scenario A FAILS because Hydra's verification_uri
# is the cluster-internal DNS `http://ory-hydra-public.loopzero-auth.svc.cluster.local:4444/oauth2/device/verify`
# instead of the canonical dev-public `https://app-dev.loopzero.example/device-verify`.
#
# Sibling of patch-kratos-dev-public.sh — patches the Hydra ConfigMap
# the same way:
#
# Layer 1 — Patch `urls.self.{issuer,public}` to canonical
#   https://auth-dev.loopzero.example/ (was http://ory-hydra-public...:4444/).
#   Without this, the verification_uri scheme + host built by Hydra is
#   internal DNS.
#
# Layer 2 — Patch `urls.device.{verification,success}` to canonical
#   https://app-dev.loopzero.example/device-{verify,success}. Default is
#   http://localhost:4455/device-* (the seed-local-user.sh dev seed
#   that assumes single-cluster + port-forward).
#
# Layer 3 — Patch `urls.{login,consent,logout,error}` similarly so the
#   OAuth flows route to the canonical dev-public dashboard.
#
# Layer 4 — Restart Hydra so the new config takes effect. New config is
#   read at startup only (no SIGHUP support per Ory Hydra v2.x docs).
#
# Idempotent: re-runs are no-ops if URLs are already canonical.

set -euo pipefail

MGMT_CONTEXT="${KUBE_CONTEXT:-k3d-loopzero-mgmt}"
AUTH_NAMESPACE="${AUTH_NAMESPACE:-loopzero-auth}"
AUTH_HOST="${LOOPZERO_AUTH_HOST:-auth-dev.loopzero.example}"
APP_HOST="${LOOPZERO_APP_HOST:-app-dev.loopzero.example}"

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

log() { printf "%s [seed-hydra-urls-canonical] %s\n" "$(date -u +%H:%M:%SZ)" "$*"; }
fail() { log "FAIL: $*"; exit 1; }

# Pre-flight
kubectl --context "$MGMT_CONTEXT" -n "$AUTH_NAMESPACE" get cm ory-hydra >/dev/null 2>&1 \
  || fail "ory-hydra ConfigMap missing in $AUTH_NAMESPACE — run task setup first"

if [[ "$DRY_RUN" == "1" ]]; then
  log "DRY_RUN: would patch ory-hydra ConfigMap (urls.self + urls.device + urls.{login,consent,logout,error}) + restart"
  exit 0
fi

# Layer 1+2+3 — patch the ConfigMap key hydra.yaml in-place via Python
CURRENT=$(kubectl --context "$MGMT_CONTEXT" -n "$AUTH_NAMESPACE" \
  get cm ory-hydra -o jsonpath='{.data.hydra\.yaml}' 2>/dev/null)
[[ -n "$CURRENT" ]] || fail "ory-hydra ConfigMap data.hydra.yaml is empty"

NEW=$(echo "$CURRENT" | python3 -c "
import sys, re
data = sys.stdin.read()
auth = '$AUTH_HOST'
app = '$APP_HOST'

# Layer 1: urls.self.{issuer,public}
data = re.sub(r'issuer: http://ory-hydra-public[^\s]*', f'issuer: https://{auth}/', data)
data = re.sub(r'public: http://ory-hydra-public[^\s]*', f'public: https://{auth}/', data)

# Layer 2: urls.device.{verification,success}
data = re.sub(r'verification: http://localhost:4455/device-verify', f'verification: https://{app}/device-verify', data)
data = re.sub(r'success: http://localhost:4455/device-success', f'success: https://{app}/device-success', data)

# Layer 3: urls.{login,consent,logout,error}
data = re.sub(r'login: http://localhost:4455/login', f'login: https://{app}/login', data)
data = re.sub(r'consent: http://localhost:4455/consent', f'consent: https://{app}/consent', data)
data = re.sub(r'logout: http://localhost:4455/logout', f'logout: https://{app}/logout', data)
data = re.sub(r'error: http://localhost:4455/error', f'error: https://{app}/error', data)

sys.stdout.write(data)
")

if [[ "$NEW" == "$CURRENT" ]]; then
  log "no-op: ory-hydra URLs already canonical"
else
  PATCH_FILE=$(mktemp -t hydra-canonical.XXXXXX.json)
  trap 'rm -f \"$PATCH_FILE\"' EXIT INT TERM
  python3 -c "
import json
data = {'data': {'hydra.yaml': '''$NEW'''}}
print(json.dumps(data))" > "$PATCH_FILE"
  kubectl --context "$MGMT_CONTEXT" -n "$AUTH_NAMESPACE" patch cm ory-hydra --type=merge --patch-file="$PATCH_FILE" >/dev/null
  log "Layer 1+2+3: ory-hydra ConfigMap patched (urls.self + urls.device + urls.{login,consent,logout,error})"

  # Layer 4 — restart Hydra
  kubectl --context "$MGMT_CONTEXT" -n "$AUTH_NAMESPACE" rollout restart deployment/ory-hydra >/dev/null
  kubectl --context "$MGMT_CONTEXT" -n "$AUTH_NAMESPACE" rollout status deployment/ory-hydra --timeout=120s >/dev/null
  log "Layer 4: ory-hydra restarted (new config active)"
fi

# Verify
if [[ "$VERIFY" == "1" ]]; then
  # Probe device authorize endpoint + assert verification_uri matches canonical
  CLIENT_RESP=$(curl -sk -X POST -d "client_id=loopzero-cli" \
    "https://${AUTH_HOST}/oauth2/device/auth" 2>/dev/null || echo "")
  if [[ -z "$CLIENT_RESP" ]]; then
    log "FAIL verify: cannot reach https://${AUTH_HOST}/oauth2/device/auth"
    exit 1
  fi
  VU=$(echo "$CLIENT_RESP" | python3 -c "import json,sys; sys.stdout.write(json.load(sys.stdin).get('verification_uri',''))" 2>/dev/null || echo "")
  if [[ "$VU" == "https://${APP_HOST}/device-verify"* ]] || [[ "$VU" == "https://${AUTH_HOST}/"* ]]; then
    log "OK verify: verification_uri canonical = $VU"
  else
    log "FAIL verify: verification_uri='$VU' — expected canonical https://${AUTH_HOST}/* or https://${APP_HOST}/device-verify"
    exit 1
  fi
fi

log "DONE — canonical Hydra URLs seeded"
