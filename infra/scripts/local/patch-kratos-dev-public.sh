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

apply_patch() {
  KRATOS_PUBLIC_URL="$KRATOS_PUBLIC_URL" \
  DASHBOARD_URL="$DASHBOARD_URL" \
  CLOUD_URL="$CLOUD_URL" \
  COOKIE_DOMAIN="$COOKIE_DOMAIN" \
  GH_ID="$GH_ID" GH_SEC="$GH_SEC" \
  GG_ID="$GG_ID" GG_SEC="$GG_SEC" \
  RESEND="$RESEND" \
  DRY_RUN="$DRY_RUN" \
  KUBECTL_CONTEXT="$KUBECTL_CONTEXT" \
  KRATOS_NAMESPACE="$KRATOS_NAMESPACE" \
  python3 <<'PY'
import os, subprocess, sys, tempfile, yaml

CTX = os.environ["KUBECTL_CONTEXT"]
NS = os.environ["KRATOS_NAMESPACE"]
DRY = os.environ.get("DRY_RUN", "0") == "1"

raw = subprocess.check_output([
    "kubectl", "--context", CTX, "-n", NS,
    "get", "configmap", "ory-kratos-config",
    "-o", "jsonpath={.data.kratos\\.yaml}"
]).decode()
cfg = yaml.safe_load(raw)

# Patch 1 — OIDC providers
keys = {
    "github": (os.environ["GH_ID"], os.environ["GH_SEC"]),
    "google": (os.environ["GG_ID"], os.environ["GG_SEC"]),
}
for p in cfg["selfservice"]["methods"]["oidc"]["config"].get("providers", []):
    if p["id"] in keys:
        cid, sec = keys[p["id"]]
        p["client_id"] = cid
        p["client_secret"] = sec

# Patch 2 — OIDC base_redirect_uri
cfg["selfservice"]["methods"]["oidc"]["config"]["base_redirect_uri"] = (
    os.environ["KRATOS_PUBLIC_URL"].rstrip("/") + "/"
)

# Patch 3 — Resend Authorization
cfg["courier"]["http"]["request_config"]["headers"]["Authorization"] = (
    "Bearer " + os.environ["RESEND"]
)

# Patch 4 — serve.public
cfg.setdefault("serve", {}).setdefault("public", {})
cfg["serve"]["public"]["base_url"] = os.environ["KRATOS_PUBLIC_URL"].rstrip("/") + "/"
cfg["serve"]["public"].setdefault("port", 4433)
cfg["serve"]["public"]["cors"] = {
    "enabled": True,
    "allow_credentials": True,
    "allowed_origins": [os.environ["DASHBOARD_URL"], os.environ["CLOUD_URL"]],
    "allowed_methods": ["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"],
    "allowed_headers": ["Authorization", "Content-Type", "Cookie", "X-Session-Token"],
    "exposed_headers": ["Content-Type", "Set-Cookie"],
}

# Patch 5 — cookies + session.cookie
domain = os.environ["COOKIE_DOMAIN"]
cfg["cookies"] = {"domain": domain, "same_site": "Lax"}
cfg.setdefault("session", {}).setdefault("cookie", {})
cfg["session"]["cookie"]["domain"] = domain
cfg["session"].setdefault("lifespan", "24h")

# Patch 6 — selfservice flows
ss = cfg["selfservice"]
app = os.environ["DASHBOARD_URL"].rstrip("/")
ss["default_browser_return_url"] = app + "/"
ss["allowed_return_urls"] = [app, app + "/", os.environ["CLOUD_URL"], os.environ["CLOUD_URL"] + "/"]
flows = ss["flows"]
flows.setdefault("login", {})["ui_url"] = app + "/login"
flows.setdefault("registration", {})["ui_url"] = app + "/register"
flows.setdefault("verification", {})["ui_url"] = app + "/verification"
flows.setdefault("recovery", {})["ui_url"] = app + "/recovery"
flows.setdefault("settings", {})["ui_url"] = app + "/settings"
flows.setdefault("error", {})["ui_url"] = app + "/login"
flows.setdefault("logout", {}).setdefault("after", {})["default_browser_return_url"] = app + "/login"

new_yaml = yaml.safe_dump(cfg, default_flow_style=False, sort_keys=False)

if DRY:
    # Print a unified diff-like summary
    old_yaml = raw
    if old_yaml == new_yaml:
        print("no-op: ConfigMap already in desired state")
    else:
        import difflib
        diff = difflib.unified_diff(
            old_yaml.splitlines(keepends=True),
            new_yaml.splitlines(keepends=True),
            fromfile="ConfigMap (live)",
            tofile="ConfigMap (desired)",
            n=2,
        )
        sys.stdout.writelines(diff)
    sys.exit(0)

patched = {
    "apiVersion": "v1", "kind": "ConfigMap",
    "metadata": {"name": "ory-kratos-config", "namespace": NS},
    "data": {"kratos.yaml": new_yaml},
}
with tempfile.NamedTemporaryFile(mode="w", suffix=".yaml", delete=False) as f:
    yaml.safe_dump(patched, f)
    fname = f.name

subprocess.run(["kubectl", "--context", CTX, "apply", "-f", fname], check=True)
os.unlink(fname)
print("ConfigMap patched (7 fixes applied)")
PY
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
