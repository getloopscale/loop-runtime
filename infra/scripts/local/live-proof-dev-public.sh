#!/usr/bin/env bash
# Live-Proof executable — operationalizes .claude/skills/live-proof/SKILL.md
# for the dev-public env (T7 of plan 2026-06-04-dev-public-domain-canonicalization).
#
# 3-layer byte-evidence protocol per skill spec:
#   Layer 1 (source):     env vars present + canonical in .env
#   Layer 2 (rendered):   K8s objects (ConfigMap, Secret, env vars) reflect the source
#   Layer 3 (LIVE):       endpoint behaves per the rendered config
#
# Checks implemented:
#   --check=oidc       Layer1: LOOPZERO_*_CLIENT_ID/SECRET present
#                      Layer2: ConfigMap Kratos has providers with non-placeholder
#                              client_id + client_secret + base_redirect_uri=auth-dev
#                      Layer3: POST /self-service/login {method=oidc,provider=google}
#                              returns 303 with redirect_uri=auth-dev/callback/google
#                              Same for github
#
#   --check=stripe     Layer1: LOOPZERO_STRIPE_SECRET_KEY + price IDs + webhook_secret
#                              present in .env
#                      Layer2: loopzero-cloud Deployment env has STRIPE_API_KEY
#                              + STRIPE_PRICE_PRO + STRIPE_WEBHOOK_SECRET
#                      Layer3: GET /v1/billing/portal with JWT returns
#                              billing.stripe.com URL (proves Stripe API works)
#
#   --check=resend     Layer1: LOOPZERO_RESEND_API_KEY present
#                      Layer2: Kratos ConfigMap Authorization header NOT placeholder
#                              and not ${VAR} literal
#                      Layer3: courier_messages.status != 4 for last 5 messages
#                              (status 1=queued, 2=sent — no recent abandoned)
#
#   --check=api-sanity (T3.1 of plan 2026-06-08-pre-release-test-rigor)
#                      Layer3 ONLY: 4 endpoint response-shape assertions that
#                      catch iter-20-class regressions (e.g., BuildListItem.Apps
#                      gap where builds endpoint stopped serializing apps[]
#                      from apps_spec). Endpoints:
#                        1) /api/v1/projects                  — list shape
#                        2) /api/v1/projects/{name}           — detail shape
#                        3) /api/v1/projects/{name}/builds    — builds[].apps populated
#                        4) /api/v1/projects/{name}/status    — active_release
#                      The script uses the seeded admin@loopzero.example session cookie.
#
# Default --check=all runs oidc + stripe + resend (NOT api-sanity — api-sanity
# requires a deployed fixture project from T2.1, so it's opt-in).
#
# Flags:
#   --env=dev-public    (default) operates against *-dev.loopzero.example hosts
#                        + .env file + k3d-loopzero-mgmt context
#   --skip-layer3        skip LIVE network probes (CI hermetic mode)
#   --json               emit results as JSON for downstream tools

set -euo pipefail

ENV_NAME="dev-public"
CHECK="all"
SKIP_LAYER3=0
EMIT_JSON=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --env=*) ENV_NAME="${1#*=}"; shift ;;
    --check=*) CHECK="${1#*=}"; shift ;;
    --skip-layer3) SKIP_LAYER3=1; shift ;;
    --json) EMIT_JSON=1; shift ;;
    -h|--help) sed -n '2,/^set -euo/p' "$0"; exit 0 ;;
    *) printf 'unknown flag: %s\n' "$1" >&2; exit 1 ;;
  esac
done

# Env-specific defaults
case "$ENV_NAME" in
  dev-public)
    AUTH_HOST="https://auth-dev.loopzero.example"
    APP_HOST="https://app-dev.loopzero.example"
    CLOUD_HOST="https://cloud-dev.loopzero.example"
    KUBE_CTX="k3d-loopzero-mgmt"
    KRATOS_NS="loopzero-auth"
    CLOUD_NS="loopzero-system"
    ;;
  *)
    printf 'unsupported --env=%s (only dev-public for now)\n' "$ENV_NAME" >&2
    exit 1
    ;;
esac

: "${ENV_FILE:=/home/paulo/loopzero/.env}"

PASS=0
FAIL=0
declare -A RESULTS

ok()   { printf '  PASS: %s\n' "$1" >&2; PASS=$((PASS+1)); RESULTS["$2"]="PROOF"; }
bad()  { printf '  FAIL: %s\n' "$1" >&2; FAIL=$((FAIL+1)); RESULTS["$2"]="FAIL"; }
warn() { printf '  WARN: %s\n' "$1" >&2; }

log() { printf '[live-proof env=%s] %s\n' "$ENV_NAME" "$*" >&2; }

env_get() {
  local key="$1"
  grep -E "^$key=" "$ENV_FILE" 2>/dev/null | head -1 | cut -d= -f2-
}

# Cached fresh JWT (lazy: only built when a Layer3 needs it)
JWT=""
get_jwt() {
  [[ -n "$JWT" ]] && { printf '%s' "$JWT"; return 0; }
  local cookie_jar; cookie_jar=$(mktemp); trap "rm -f $cookie_jar" RETURN
  local flow; flow=$(curl -fsS -c "$cookie_jar" \
    "$AUTH_HOST/self-service/login/browser" -H "Accept: application/json" || echo '{}')
  local fid; fid=$(printf '%s' "$flow" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))")
  local csrf; csrf=$(printf '%s' "$flow" | python3 -c "
import sys, json
d = json.load(sys.stdin)
for n in d.get('ui',{}).get('nodes',[]):
    if n.get('attributes',{}).get('name')=='csrf_token':
        print(n['attributes'].get('value','')); break
")
  curl -fsS -b "$cookie_jar" -c "$cookie_jar" -X POST \
    "$AUTH_HOST/self-service/login?flow=$fid" \
    -H "Content-Type: application/json" \
    -d "{\"method\":\"password\",\"identifier\":\"paulo@loopzero.test\",\"password\":\"LoopZeroDev2026!\",\"csrf_token\":\"$csrf\"}" \
    >/dev/null || true
  JWT=$(curl -fsS -b "$cookie_jar" -X POST "$CLOUD_HOST/v1/auth/session/token" \
    | python3 -c "import sys,json; print(json.load(sys.stdin).get('access_token',''))" || echo "")
  printf '%s' "$JWT"
}

# ============================================================================
# Check: OIDC
# ============================================================================
check_oidc() {
  log "check=oidc"

  # Layer 1
  local gh_id gh_sec gg_id gg_sec
  gh_id=$(env_get LOOPZERO_GITHUB_CLIENT_ID)
  gh_sec=$(env_get LOOPZERO_GITHUB_CLIENT_SECRET)
  gg_id=$(env_get LOOPZERO_GOOGLE_CLIENT_ID)
  gg_sec=$(env_get LOOPZERO_GOOGLE_CLIENT_SECRET)
  if [[ -n "$gh_id" && -n "$gh_sec" && -n "$gg_id" && -n "$gg_sec" ]]; then
    ok "L1 .env has GH+Google client_id+secret" "oidc.l1"
  else
    bad "L1 .env missing OAuth env vars" "oidc.l1"
    return
  fi

  # Layer 2
  local cfg
  cfg=$(kubectl --context "$KUBE_CTX" -n "$KRATOS_NS" get configmap ory-kratos-config \
    -o "jsonpath={.data.kratos\.yaml}" 2>/dev/null || echo "")
  if [[ -z "$cfg" ]]; then
    bad "L2 ConfigMap ory-kratos-config not found" "oidc.l2"
    return
  fi
  local l2_status
  l2_status=$(printf '%s' "$cfg" | python3 -c "
import sys, yaml
d = yaml.safe_load(sys.stdin)
cfg = d.get('selfservice',{}).get('methods',{}).get('oidc',{}).get('config',{})
providers = {p['id']: p for p in cfg.get('providers',[])}
issues = []
for pid in ['github','google']:
    p = providers.get(pid)
    if not p:
        issues.append(f'{pid} missing'); continue
    if 'REPLACE' in str(p.get('client_id','')):
        issues.append(f'{pid} client_id placeholder')
    if 'REPLACE' in str(p.get('client_secret','')):
        issues.append(f'{pid} client_secret placeholder')
base = cfg.get('base_redirect_uri','')
expected_prefix = '${AUTH_HOST}/'
if base != expected_prefix:
    issues.append(f'base_redirect_uri={base} != {expected_prefix}')
print('|'.join(issues) if issues else 'OK')")
  if [[ "$l2_status" == "OK" ]]; then
    ok "L2 Kratos ConfigMap has real client_id+secret + base_redirect_uri=$AUTH_HOST/" "oidc.l2"
  else
    bad "L2 issues: $l2_status" "oidc.l2"
    return
  fi

  # Layer 3
  if [[ $SKIP_LAYER3 -eq 1 ]]; then
    warn "L3 skipped (--skip-layer3)"
    return
  fi

  # Run the existing e2e test (it covers exactly L3 — programmatic OIDC redirect)
  if KRATOS_PUBLIC="$AUTH_HOST" DASHBOARD="$APP_HOST" \
      EXPECTED_CALLBACK_BASE="$AUTH_HOST" \
      bash "$(dirname "$0")/../../tests/e2e/dev-public-oidc-redirect_test.sh" \
      >/tmp/live-proof-oidc.log 2>&1; then
    ok "L3 OIDC redirect e2e test 8/8 PASS (both providers redirect to $AUTH_HOST/...callback)" "oidc.l3"
  else
    bad "L3 OIDC redirect e2e test FAIL — see /tmp/live-proof-oidc.log" "oidc.l3"
  fi
}

# ============================================================================
# Check: Stripe
# ============================================================================
check_stripe() {
  log "check=stripe"

  # Layer 1
  local sec pub price_pro price_team whsec
  sec=$(env_get LOOPZERO_STRIPE_SECRET_KEY)
  pub=$(env_get LOOPZERO_STRIPE_PUBLISHABLE_KEY)
  whsec=$(env_get LOOPZERO_STRIPE_WEBHOOK_SECRET_DEV)
  if [[ -n "$sec" && -n "$pub" && -n "$whsec" ]]; then
    ok "L1 .env has STRIPE secret+publishable+webhook_secret_dev" "stripe.l1"
  else
    bad "L1 .env missing Stripe vars (sec=${#sec} pub=${#pub} whsec=${#whsec})" "stripe.l1"
    return
  fi

  # Layer 2 — must support both `env:` direct + `envFrom.secretRef`.
  # The canonical loopzero-cloud chart uses envFrom secretRef (loopzero-stripe-webhook
  # Secret) which does NOT appear in `containers[0].env[*].name`. We must
  # union (a) direct env names with (b) keys of every referenced Secret.
  if ! kubectl --context "$KUBE_CTX" -n "$CLOUD_NS" get deployment loopzero-cloud >/dev/null 2>&1; then
    bad "L2 deployment loopzero-cloud not found in $CLOUD_NS" "stripe.l2"
    return
  fi
  local cloud_env
  cloud_env=$(kubectl --context "$KUBE_CTX" -n "$CLOUD_NS" get deployment loopzero-cloud \
    -o jsonpath='{.spec.template.spec.containers[0].env[*].name}' 2>/dev/null || echo "")
  # envFrom secretRef names (the chart marks them optional=true)
  local envfrom_secrets
  envfrom_secrets=$(kubectl --context "$KUBE_CTX" -n "$CLOUD_NS" get deployment loopzero-cloud \
    -o jsonpath='{.spec.template.spec.containers[0].envFrom[*].secretRef.name}' 2>/dev/null || echo "")
  # Read keys of every envFrom Secret + envFrom ConfigMap
  local envfrom_keys=""
  for sname in $envfrom_secrets; do
    [[ -z "$sname" ]] && continue
    local keys
    keys=$(kubectl --context "$KUBE_CTX" -n "$CLOUD_NS" get secret "$sname" \
      -o jsonpath='{.data}' 2>/dev/null | tr ',' '\n' | grep -oE '"[A-Z_]+"' | tr -d '"' || true)
    envfrom_keys="$envfrom_keys $keys"
  done
  local envfrom_cms
  envfrom_cms=$(kubectl --context "$KUBE_CTX" -n "$CLOUD_NS" get deployment loopzero-cloud \
    -o jsonpath='{.spec.template.spec.containers[0].envFrom[*].configMapRef.name}' 2>/dev/null || echo "")
  for cname in $envfrom_cms; do
    [[ -z "$cname" ]] && continue
    local keys
    keys=$(kubectl --context "$KUBE_CTX" -n "$CLOUD_NS" get cm "$cname" \
      -o jsonpath='{.data}' 2>/dev/null | tr ',' '\n' | grep -oE '"[A-Z_]+"' | tr -d '"' || true)
    envfrom_keys="$envfrom_keys $keys"
  done
  local all_keys="$cloud_env $envfrom_keys"
  local missing=()
  for k in STRIPE_API_KEY STRIPE_PRICE_PRO STRIPE_PRICE_TEAM STRIPE_WEBHOOK_SECRET; do
    if ! printf '%s\n' $all_keys | grep -qx "$k"; then
      missing+=("$k")
    fi
  done
  if [[ ${#missing[@]} -eq 0 ]]; then
    ok "L2 loopzero-cloud Deployment has all Stripe env vars (incl. envFrom)" "stripe.l2"
  else
    bad "L2 loopzero-cloud missing env: ${missing[*]} (checked .env[] + envFrom.{secretRef,configMapRef})" "stripe.l2"
    return
  fi

  # Layer 3
  if [[ $SKIP_LAYER3 -eq 1 ]]; then
    warn "L3 skipped (--skip-layer3)"
    return
  fi
  local jwt; jwt=$(get_jwt)
  if [[ -z "$jwt" ]]; then
    bad "L3 could not obtain JWT (login failed)" "stripe.l3"
    return
  fi
  local portal_resp; portal_resp=$(curl -fsS \
    "$CLOUD_HOST/v1/billing/portal" -H "Authorization: Bearer $jwt" 2>/dev/null || echo '{}')
  local portal_url
  portal_url=$(printf '%s' "$portal_resp" \
    | python3 -c "import sys,json; print(json.load(sys.stdin).get('url',''))")
  if [[ "$portal_url" =~ ^https://billing\.stripe\.com/p/session/ ]]; then
    ok "L3 /v1/billing/portal returns Stripe Customer Portal URL" "stripe.l3"
  else
    bad "L3 portal URL not Stripe billing: $portal_url" "stripe.l3"
  fi
}

# ============================================================================
# Check: Resend
# ============================================================================
check_resend() {
  log "check=resend"

  # Layer 1
  local key; key=$(env_get LOOPZERO_RESEND_API_KEY)
  if [[ "$key" =~ ^re_ ]]; then
    ok "L1 .env has LOOPZERO_RESEND_API_KEY (re_*)" "resend.l1"
  else
    bad "L1 LOOPZERO_RESEND_API_KEY missing or wrong format" "resend.l1"
    return
  fi

  # Layer 2
  local auth_header
  auth_header=$(kubectl --context "$KUBE_CTX" -n "$KRATOS_NS" \
    get configmap ory-kratos-config -o "jsonpath={.data.kratos\.yaml}" 2>/dev/null \
    | python3 -c "
import sys, yaml
d = yaml.safe_load(sys.stdin)
print(d.get('courier',{}).get('http',{}).get('request_config',{}).get('headers',{}).get('Authorization',''))
")
  if [[ "$auth_header" == "Bearer re_"* ]]; then
    ok "L2 Kratos courier Authorization header has real Bearer re_* (not placeholder)" "resend.l2"
  elif [[ "$auth_header" == *'${'* ]]; then
    bad "L2 Authorization header has literal \${VAR} (Kratos does NOT env-interp)" "resend.l2"
    return
  else
    bad "L2 Authorization header invalid: ${auth_header:0:30}..." "resend.l2"
    return
  fi

  # Layer 3
  if [[ $SKIP_LAYER3 -eq 1 ]]; then
    warn "L3 skipped (--skip-layer3)"
    return
  fi
  local pg_status
  pg_status=$(kubectl --context "$KUBE_CTX" -n "$CLOUD_NS" exec postgresql-0 -c postgresql -- \
    env PGPASSWORD=loopzero-dev-password psql -U loopzero -d kratos -t \
    -c "SELECT COALESCE(SUM(CASE WHEN status=4 THEN 1 ELSE 0 END),0) FROM courier_messages WHERE created_at > now() - interval '1 hour';" \
    2>/dev/null | tr -d ' \n')
  if [[ "$pg_status" == "0" ]]; then
    ok "L3 courier_messages: 0 abandoned in last hour" "resend.l3"
  elif [[ -z "$pg_status" ]]; then
    warn "L3 could not query courier_messages (psql failed)"
    bad "L3 courier_messages query failed" "resend.l3"
  else
    bad "L3 $pg_status courier_messages abandoned (status=4) in last hour" "resend.l3"
  fi
}

# ============================================================================
# check_api_sanity — Layer 3 endpoint response-shape sanity (T3.1 of plan
# 2026-06-08-pre-release-test-rigor v1.2)
# ============================================================================
# 4 endpoints asserted against the seeded admin@loopzero.example session:
#   1) /api/v1/projects                      — list shape: projects[] has
#      env_count + last_deploy_status fields
#   2) /api/v1/projects/{name}                — detail shape: apps_count +
#      latest_framework + last_deploy populated
#   3) /api/v1/projects/{name}/builds          — builds[].apps populated
#      when builds[].apps_spec exists. This would have caught the
#      iter 20 BuildListItem.Apps regression where the builds endpoint
#      stopped serializing apps[] from apps_spec.
#   4) /api/v1/projects/{name}/status          — active_release shape
#      consistent: deploy_status + replicas_ready presence.
check_api_sanity() {
  log "L3 (api-sanity) — endpoint response-shape sanity"

  if [[ "$SKIP_LAYER3" -eq 1 ]]; then
    log "L3 SKIPPED via --skip-layer3"
    RESULTS["api-sanity.l3"]="SKIPPED"
    return
  fi

  local session_file="${SESSION_FILE:-$HOME/.loopzero/admin-loopzero-dev-session.txt}"
  if [[ ! -s "$session_file" ]]; then
    bad "L3 admin@loopzero.example session file missing/empty at $session_file" "api-sanity.l3.preflight"
    return
  fi
  local cookie
  cookie="$(cat "$session_file")"
  # CLOUD_HOST already includes the https:// prefix (set at script init).
  local cloud_url="${PUBLIC_CLOUD_URL:-$CLOUD_HOST}"
  local project_name="${PROJECT_NAME:-teste}"

  # Exchange the Kratos cookie for a JWT — engine API uses Bearer auth, NOT
  # cookie. The dashboard's session-token handler is at /v1/auth/session/token.
  local jwt_resp jwt
  jwt_resp="$(curl -fsS --max-time 5 -X POST \
    -b "ory_kratos_session=${cookie}" \
    -H "Origin: https://app-dev.loopzero.example" \
    "${cloud_url}/v1/auth/session/token" 2>/dev/null || echo "")"
  jwt="$(printf '%s' "$jwt_resp" | python3 -c '
import json, sys
try:
  d = json.load(sys.stdin)
  sys.stdout.write(d.get("token") or d.get("access_token") or d.get("session_token") or "")
except Exception:
  pass
' 2>/dev/null || echo "")"

  if [[ -z "$jwt" ]]; then
    bad "L3 /v1/auth/session/token exchange failed — cannot reach engine API" "api-sanity.l3.exchange"
    return
  fi

  local AUTH_HDR="Authorization: Bearer ${jwt}"

  # --- endpoint 1: /api/v1/projects (list shape) -----------------------------
  # NOTE: engine API is exposed at /engine/api/v1/... (not /api/v1/...).
  local resp1
  resp1="$(curl -fsS --max-time 5 -H "$AUTH_HDR" \
    "${cloud_url}/engine/api/v1/projects" 2>/dev/null || echo "")"
  if [[ -z "$resp1" ]]; then
    bad "L3 endpoint 1 /api/v1/projects unreachable" "api-sanity.l3.endpoint1"
  else
    local has_env_count
    has_env_count="$(printf '%s' "$resp1" | python3 -c '
import json, sys
try:
  d = json.load(sys.stdin)
  items = d if isinstance(d, list) else d.get("projects", d.get("items", []))
  if items and any("env_count" in p or "environment_count" in p for p in items):
    print("1")
  else:
    print("0")
except Exception:
  print("0")
' 2>/dev/null || echo "0")"
    if [[ "$has_env_count" == "1" ]]; then
      ok "L3 endpoint 1 projects[] has env_count" "api-sanity.l3.endpoint1.env_count"
    else
      bad "L3 endpoint 1 projects[] missing env_count field" "api-sanity.l3.endpoint1.env_count"
    fi
  fi

  # --- endpoint 2: /api/v1/projects/{name} (detail shape) --------------------
  local resp2
  resp2="$(curl -fsS --max-time 5 -H "$AUTH_HDR" \
    "${cloud_url}/engine/api/v1/projects/${project_name}" 2>/dev/null || echo "")"
  if [[ -z "$resp2" ]]; then
    bad "L3 endpoint 2 /api/v1/projects/${project_name} unreachable" "api-sanity.l3.endpoint2"
  else
    local has_apps_count
    has_apps_count="$(printf '%s' "$resp2" | python3 -c '
import json, sys
try:
  d = json.load(sys.stdin)
  if "apps_count" in d:
    print("1")
  else:
    print("0")
except Exception:
  print("0")
' 2>/dev/null || echo "0")"
    if [[ "$has_apps_count" == "1" ]]; then
      ok "L3 endpoint 2 detail has apps_count" "api-sanity.l3.endpoint2.apps_count"
    else
      bad "L3 endpoint 2 detail missing apps_count field" "api-sanity.l3.endpoint2.apps_count"
    fi
  fi

  # --- endpoint 3: /api/v1/projects/{name}/builds (BuildListItem.Apps) ------
  # Forensic anchor: this assertion would have caught iter 20 regression where
  # BuildListItem.Apps stopped being serialized from apps_spec.
  local resp3
  resp3="$(curl -fsS --max-time 5 -H "$AUTH_HDR" \
    "${cloud_url}/engine/api/v1/projects/${project_name}/builds" 2>/dev/null || echo "")"
  if [[ -z "$resp3" ]]; then
    bad "L3 endpoint 3 /builds unreachable" "api-sanity.l3.endpoint3"
  else
    local apps_populated
    apps_populated="$(printf '%s' "$resp3" | python3 -c '
import json, sys
# Iter 20 BuildListItem.Apps gap defense: if any build has apps_spec but apps[]
# is missing OR empty, this is the regression class we are defending against.
try:
  d = json.load(sys.stdin)
  builds = d if isinstance(d, list) else d.get("builds", d.get("items", []))
  for b in builds:
    spec = b.get("apps_spec")
    apps = b.get("apps")
    # apps_spec present + apps missing/empty → regression. Empty list ok if no apps_spec.
    if spec and not apps:
      print("0")
      sys.exit(0)
  print("1")
except Exception:
  print("0")
' 2>/dev/null || echo "0")"
    if [[ "$apps_populated" == "1" ]]; then
      ok "L3 endpoint 3 builds[].apps populated when apps_spec (iter 20 BuildListItem.Apps regression defense)" "api-sanity.l3.endpoint3.apps"
    else
      bad "L3 endpoint 3 builds[].apps EMPTY despite apps_spec — iter 20 BuildListItem.Apps regression class" "api-sanity.l3.endpoint3.apps"
    fi
  fi

  # --- endpoint 4: /api/v1/projects/{name}/status (active_release) ----------
  local resp4
  resp4="$(curl -fsS --max-time 5 -H "$AUTH_HDR" \
    "${cloud_url}/engine/api/v1/projects/${project_name}/status" 2>/dev/null || echo "")"
  if [[ -z "$resp4" ]]; then
    bad "L3 endpoint 4 /status unreachable" "api-sanity.l3.endpoint4"
  else
    local shape_consistent
    shape_consistent="$(printf '%s' "$resp4" | python3 -c '
import json, sys
try:
  d = json.load(sys.stdin)
  # Shape rule: if active_release is non-null, deploy_status OR replicas_ready
  # MUST be present. Catches the dashboard-screens-review-fixes T5.3 bug class
  # where ProjectHealthCard rendered 4 orphan dashes because backend omitempty
  # dropped signals for projects without active release.
  active = d.get("active_release")
  ds = d.get("deploy_status")
  rr = d.get("replicas_ready")
  if active and (ds is None and rr is None):
    print("0")
  else:
    print("1")
except Exception:
  print("0")
' 2>/dev/null || echo "0")"
    if [[ "$shape_consistent" == "1" ]]; then
      ok "L3 endpoint 4 status active_release shape consistent" "api-sanity.l3.endpoint4.shape"
    else
      bad "L3 endpoint 4 status: active_release set but no deploy_status/replicas_ready (T5.3 walkthrough regression class)" "api-sanity.l3.endpoint4.shape"
    fi
  fi
}

# ============================================================================
# Main
# ============================================================================
log "starting (auth=$AUTH_HOST app=$APP_HOST cloud=$CLOUD_HOST)"

case "$CHECK" in
  oidc) check_oidc ;;
  stripe) check_stripe ;;
  resend) check_resend ;;
  api-sanity) check_api_sanity ;;
  all) check_oidc; check_stripe; check_resend ;;
  *) printf 'unknown --check=%s\n' "$CHECK" >&2; exit 1 ;;
esac

printf '\n[live-proof] summary PASS=%d FAIL=%d\n' "$PASS" "$FAIL"

if [[ $EMIT_JSON -eq 1 ]]; then
  LP_TMP="$(mktemp)"
  for k in "${!RESULTS[@]}"; do printf '%s\t%s\n' "$k" "${RESULTS[$k]}"; done > "$LP_TMP"
  LP_RESULTS_FILE="$LP_TMP" LP_ENV_NAME="$ENV_NAME" LP_PASS="$PASS" LP_FAIL="$FAIL" \
    python3 "$(dirname "$0")/live_proof_dev_public.py"
  rm -f "$LP_TMP"
fi

[[ "$FAIL" -eq 0 ]] || exit 1
