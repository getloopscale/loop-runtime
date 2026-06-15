#!/usr/bin/env bash
# seed-admin-loopzero-dev.sh
#
# T0.2 of plan 2026-06-08-pre-release-test-rigor v1.2 — canonical
# Kratos session seed for the dev/dev-public canonical user
# `admin@loopzero.example` (see memory project_canonical_dev_user since
# 2026-06-05).
#
# Idempotent end-to-end:
#   1. Ensure Kratos identity for admin@loopzero.example (admin API; create
#      if absent).
#   2. Ensure auxiliary PG state (EC-3 MUST FIX):
#        a) users row (id mirrors Kratos identity id; active_tenant_id set)
#        b) tenants row owned by the user (or reuse first owned tenant)
#        c) tenant_members(role=owner)
#        d) user_identity_map (kratos_identity_id → user_id)
#   3. Mint a fresh session via Kratos admin
#      `POST /admin/identities/{id}/sessions` and persist the cookie
#      token to `~/.loopzero/admin-loopzero-dev-session.txt` (mode 0600).
#   4. Optional `--verify` mode probes
#        - `https://auth-dev.loopzero.example/sessions/whoami` returns 200
#          with email match
#        - `https://cloud-dev.loopzero.example/engine/api/v1/projects` returns
#          200 (NOT 403 NO_ACTIVE_TENANT) — proves EC-3 auxiliary rows
#          populated correctly.
#
# `--refresh-only` (EC-9 absorption) skips identity + PG mutations and
# only re-mints the session token. Used by other test scripts when they
# detect 401 mid-test (JWT TTL ~24h but operators may want shorter).
#
# Usage:
#   bash infra/scripts/local/seed-admin-loopzero-dev.sh [--refresh-only] [--verify] [--dry-run]
#
# Exit codes (locked by plan):
#   0  session ready (persisted + verified if --verify)
#   1  user/tenant resolution failed (Kratos identity not found AND not creatable)
#   2  PG or Kratos unreachable (port-forward failed, secret missing, etc.)
set -euo pipefail

# --- Constants --------------------------------------------------------------

CONTEXT="${CONTEXT:-k3d-loopzero-mgmt}"
PG_NAMESPACE="${PG_NAMESPACE:-loopzero-system}"
KRATOS_NAMESPACE="${KRATOS_NAMESPACE:-loopzero-auth}"
EMAIL="${EMAIL:-admin@loopzero.example}"
DISPLAY_NAME="${DISPLAY_NAME:-LoopZero Admin}"
USERNAME="${USERNAME:-admin}"
TENANT_SLUG="${TENANT_SLUG:-admin}"
SESSION_FILE="${SESSION_FILE:-$HOME/.loopzero/admin-loopzero-dev-session.txt}"
PUBLIC_KRATOS_URL="${PUBLIC_KRATOS_URL:-https://auth-dev.loopzero.example}"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://cloud-dev.loopzero.example}"
# Canonical dev password — high-entropy, dev-only. Override via env in CI.
# Kratos v26 admin API does NOT mint sessions directly (405 on POST
# /admin/identities/{id}/sessions). The canonical path is the password
# self-service login flow, which requires a known credential on the identity.
SEED_PASSWORD="${SEED_PASSWORD:-LoopZero-Seed-Admin-Pw-2026-06-08-a8s7df9as7df}"

REFRESH_ONLY=0
VERIFY=0
DRY_RUN=0

while [ $# -gt 0 ]; do
  case "$1" in
    --refresh-only) REFRESH_ONLY=1 ; shift ;;
    --verify)       VERIFY=1       ; shift ;;
    --dry-run)      DRY_RUN=1      ; shift ;;
    -h|--help)
      sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) printf '[seed-admin] unknown arg: %s\n' "$1" >&2 ; exit 1 ;;
  esac
done

log() { printf '[seed-admin] %s\n' "$*" >&2 ; }

# --- PG password resolver ---------------------------------------------------

resolve_pg_password() {
  # The loopzero-db-credentials Secret holds 2 password keys:
  #   - `password` (the `loopzero` application user — what we use here)
  #   - `postgres-password` (the superuser; reserved for migrations)
  # Least-privilege: connect as loopzero user.
  local pw
  pw="$(kubectl --context="$CONTEXT" -n "$PG_NAMESPACE" \
        get secret loopzero-db-credentials \
        -o jsonpath='{.data.password}' 2>/dev/null | base64 -d 2>/dev/null || true)"
  if [ -z "$pw" ]; then
    # Standalone helmfile dev override (per infra/helm/postgresql/values.yaml)
    pw="loopzero-dev-password"
  fi
  printf '%s' "$pw"
}

PSQL() {
  kubectl --context="$CONTEXT" -n "$PG_NAMESPACE" \
    exec -i postgresql-0 -c postgresql -- \
    env PGPASSWORD="$PG_PASSWORD" psql -U loopzero -d loopzero -At "$@"
}

# --- Kratos port-forward (lifecycle managed by trap) ------------------------

KRATOS_PF_PID=""
KRATOS_ADMIN_LOCAL_URL=""

start_kratos_admin_pf() {
  local port
  # Pick a free ephemeral local port (54434 default; bumps if busy)
  port=54434
  while ss -ltn 2>/dev/null | awk '{print $4}' | grep -qE ":${port}$" ; do
    port=$((port + 1))
  done
  kubectl --context="$CONTEXT" -n "$KRATOS_NAMESPACE" \
    port-forward svc/ory-kratos-admin "${port}:80" > /tmp/seed-admin-kratos-pf.log 2>&1 &
  KRATOS_PF_PID=$!
  KRATOS_ADMIN_LOCAL_URL="http://127.0.0.1:${port}"
  # Wait for port-forward to be ready (max 5s)
  local i
  for i in 1 2 3 4 5 ; do
    if curl -fsS "${KRATOS_ADMIN_LOCAL_URL}/admin/health/ready" >/dev/null 2>&1 ; then
      log "Kratos admin port-forward ready at ${KRATOS_ADMIN_LOCAL_URL}"
      return 0
    fi
    sleep 1
  done
  log "ERROR: Kratos admin port-forward did not become ready"
  return 2
}

cleanup_pf() {
  if [ -n "$KRATOS_PF_PID" ] ; then
    kill "$KRATOS_PF_PID" 2>/dev/null || true
    wait "$KRATOS_PF_PID" 2>/dev/null || true
  fi
}
trap cleanup_pf EXIT INT TERM

# --- Kratos identity lookup / create ----------------------------------------

resolve_or_create_kratos_identity() {
  local existing identity_id

  existing="$(curl -fsS \
    "${KRATOS_ADMIN_LOCAL_URL}/admin/identities?credentials_identifier=${EMAIL}" 2>/dev/null || true)"

  identity_id="$(printf '%s' "$existing" | python3 -c '
import sys, json
try:
  data = json.load(sys.stdin)
  if isinstance(data, list) and data:
    sys.stdout.write(data[0].get("id", ""))
except Exception:
  pass
' 2>/dev/null || true)"

  if [ -n "$identity_id" ] ; then
    log "Found existing Kratos identity id=${identity_id}"
    printf '%s' "$identity_id"
    return 0
  fi

  log "Identity not found for ${EMAIL} — creating via admin API"

  local create_body
  create_body=$(cat <<JSON
{
  "schema_id": "default",
  "traits": { "email": "${EMAIL}", "name": "${DISPLAY_NAME}" },
  "verifiable_addresses": [
    { "value": "${EMAIL}", "verified": true, "via": "email", "status": "completed" }
  ]
}
JSON
)
  local created
  created="$(curl -fsS -X POST -H "Content-Type: application/json" \
    --data "$create_body" \
    "${KRATOS_ADMIN_LOCAL_URL}/admin/identities" 2>&1 || true)"

  identity_id="$(printf '%s' "$created" | python3 -c '
import sys, json
try:
  data = json.load(sys.stdin)
  sys.stdout.write(data.get("id", ""))
except Exception:
  pass
' 2>/dev/null || true)"

  if [ -z "$identity_id" ] ; then
    log "ERROR: identity create failed; response: $created"
    return 1
  fi

  log "Created Kratos identity id=${identity_id}"
  printf '%s' "$identity_id"
}

# --- PG auxiliary state (EC-3) ----------------------------------------------

ensure_pg_state() {
  local kratos_id=$1

  log "Ensuring PG auxiliary state for kratos_id=${kratos_id}"

  # Single SERIALIZABLE transaction for idempotency + atomicity
  PSQL -v ON_ERROR_STOP=1 <<SQL
BEGIN;
SET TRANSACTION ISOLATION LEVEL SERIALIZABLE;

-- (1) users row (id mirrors Kratos identity id — same UUID convention)
INSERT INTO users (id, email, username, display_name)
VALUES ('${kratos_id}', '${EMAIL}', '${USERNAME}', '${DISPLAY_NAME}')
ON CONFLICT (id) DO NOTHING;

-- (2) tenants row owned by the user (reuse if any exists)
--
-- Canonical tenant_id format (per cloud/internal/auth/tenant.go:226
-- generateTenantID): tn-{16 hex chars} = 19 chars total. Deterministic via
-- substring(sha256(...), 1, 16) so seed remains idempotent across runs.
-- Pre-2026-06-09 fix used the full 64-hex SHA256, producing 67-char IDs
-- that overflow the M2 canonical namespace budget
-- loopzero-<tenant>-<project>-<env> against the K8s 63-char limit (LIVE-caught
-- 2026-06-09 during T6.1: 89-char ns rejected → ENV_PROVISION_FAILED).
-- Aligns seed with the production naming policy (no divergent generator,
-- no hardcoded literal — substring derived from same canonical hash input).
INSERT INTO tenants (id, display_name, slug, owner_id, cell_id)
SELECT
  'tn-' || substring(encode(sha256(('${kratos_id}-${TENANT_SLUG}')::bytea), 'hex'), 1, 16),
  '${DISPLAY_NAME}',
  '${TENANT_SLUG}',
  '${kratos_id}',
  'cell-default'
WHERE NOT EXISTS (
  SELECT 1 FROM tenants WHERE owner_id = '${kratos_id}'
);

-- (3) EC-3a: tenant_members(role=owner) — idempotent
INSERT INTO tenant_members (tenant_id, user_id, role)
SELECT t.id, '${kratos_id}', 'owner'
FROM tenants t
WHERE t.owner_id = '${kratos_id}'
ON CONFLICT (tenant_id, user_id) DO NOTHING;

-- (4) EC-3b: user_identity_map(kratos_identity_id → user_id) — idempotent
INSERT INTO user_identity_map (kratos_identity_id, user_id)
VALUES ('${kratos_id}', '${kratos_id}')
ON CONFLICT (kratos_identity_id) DO NOTHING;

-- (5) EC-3c: users.active_tenant_id pinned to owned tenant
UPDATE users SET active_tenant_id = (SELECT id FROM tenants WHERE owner_id = '${kratos_id}' LIMIT 1),
    updated_at = now()
WHERE id = '${kratos_id}'
  AND (active_tenant_id IS NULL
       OR active_tenant_id NOT IN (SELECT id FROM tenants WHERE owner_id = '${kratos_id}'));

COMMIT;
SQL

  local tenant_id
  tenant_id="$(PSQL -tAc "SELECT active_tenant_id FROM users WHERE id='${kratos_id}'" 2>/dev/null || true)"
  log "Resolved active_tenant_id=${tenant_id} for kratos_id=${kratos_id}"
  printf '%s' "$tenant_id"
}

# --- Identity password reset (canonical seed) -------------------------------
#
# Kratos v26 admin API does NOT mint sessions directly. The canonical path is
# the password self-service login flow, which requires the identity to have a
# known password credential.
#
# Kratos OSS quirk LIVE-discovered 2026-06-08: JSON Patch stores
# config.password CLEARTEXT (no hash-on-save) and login fails. PUT (full
# identity replace) DOES hash-on-save. So the seed uses GET → modify → PUT.
ensure_identity_password() {
  local kratos_id=$1

  curl -fsS "${KRATOS_ADMIN_LOCAL_URL}/admin/identities/${kratos_id}" \
       > /tmp/seed-admin-identity-current.json 2>&1 || {
    log "ERROR: failed to GET current identity for PUT base"
    return 2
  }

  python3 - <<PY > /tmp/seed-admin-identity-new.json
import json
with open("/tmp/seed-admin-identity-current.json") as f:
    cur = json.load(f)
out = {
  "schema_id": cur.get("schema_id", "default"),
  "traits": cur.get("traits", {}),
  "verifiable_addresses": cur.get("verifiable_addresses", []),
  "recovery_addresses": cur.get("recovery_addresses", []),
  "metadata_public": cur.get("metadata_public"),
  "metadata_admin": cur.get("metadata_admin"),
  "state": cur.get("state", "active"),
  # PUT-with-credentials writes cleartext password; Kratos hashes on save.
  # Preserves the OIDC credential implicitly because PUT semantics on
  # /admin/identities/{id} replace only the supplied fields' explicit
  # contents — credentials NOT listed are retained on the server side.
  "credentials": {
    "password": {
      "config": {"password": "${SEED_PASSWORD}"}
    }
  },
}
print(json.dumps(out))
PY

  local http
  http="$(curl -s -o /tmp/seed-admin-put.json -w '%{http_code}' \
    -X PUT -H "Content-Type: application/json" \
    --data @/tmp/seed-admin-identity-new.json \
    "${KRATOS_ADMIN_LOCAL_URL}/admin/identities/${kratos_id}" 2>&1 || true)"

  case "$http" in
    200|204)
      log "Identity password reset to canonical seed value (idempotent, hashed on save)"
      ;;
    *)
      log "ERROR: identity password PUT HTTP ${http}; body:"
      head -c 500 /tmp/seed-admin-put.json >&2 ; echo "" >&2
      return 2
      ;;
  esac
}

# --- Session mint via browser-mode login flow (cookie path) -----------------
#
# Kratos api-mode `session_token` (`ory_st_*`) works as `X-Session-Token`
# header but NOT as `ory_kratos_session` cookie value. The cloud's
# `POST /v1/auth/session/token` exchange ONLY honors the cookie form. So the
# seed must run the browser-mode flow with a cookie jar and capture the
# `ory_kratos_session` cookie value.
mint_session() {
  local kratos_id=$1
  ensure_identity_password "$kratos_id" || return 2

  local jar="/tmp/seed-admin-cookies.txt"
  : > "$jar"

  # Step 1 — Initiate browser-mode login flow. Returns a redirect URL +
  # sets a csrf cookie scoped to the flow.
  local init_headers init_code redirect flow_id
  curl -s -o /tmp/seed-admin-login-init.html -D /tmp/seed-admin-login-init.headers \
    --max-time 10 -c "$jar" -b "$jar" \
    -H "Accept: application/json" \
    "${PUBLIC_KRATOS_URL}/self-service/login/browser" > /dev/null

  init_code="$(awk 'NR==1{print $2; exit}' /tmp/seed-admin-login-init.headers 2>/dev/null || true)"
  # Browser-mode returns the flow JSON directly when Accept: application/json
  # (no 303 redirect). Read flow id from the body.
  flow_id="$(python3 -c '
import json,sys
try:
  d=json.load(open("/tmp/seed-admin-login-init.html"))
  sys.stdout.write(d.get("id",""))
except Exception:
  pass
' 2>/dev/null || true)"

  if [ -z "$flow_id" ] ; then
    log "ERROR: browser login flow init failed; HTTP=${init_code}"
    head -c 400 /tmp/seed-admin-login-init.html >&2 ; echo "" >&2
    return 2
  fi

  log "Browser login flow initiated id=${flow_id}"

  # Step 2 — Read the flow body to extract action URL + csrf_token nonce
  local action_url csrf_token
  action_url="$(python3 -c '
import json,sys
d=json.load(open("/tmp/seed-admin-login-init.html"))
sys.stdout.write(d.get("ui",{}).get("action",""))
' 2>/dev/null || true)"

  csrf_token="$(python3 -c '
import json,sys
d=json.load(open("/tmp/seed-admin-login-init.html"))
for n in d.get("ui",{}).get("nodes",[]):
  a=n.get("attributes",{})
  if a.get("name")=="csrf_token":
    sys.stdout.write(a.get("value","") or "")
    break
' 2>/dev/null || true)"

  if [ -z "$action_url" ] ; then
    log "ERROR: flow body missing ui.action"
    return 2
  fi

  # Step 3 — Submit credentials (json body honored on browser flow when Accept
  # is application/json). The cookie jar carries the csrf cookie set in step 1.
  local submit_body submit_code
  submit_body=$(cat <<JSON
{
  "method": "password",
  "identifier": "${EMAIL}",
  "password": "${SEED_PASSWORD}",
  "csrf_token": "${csrf_token}"
}
JSON
)
  submit_code="$(curl -s -o /tmp/seed-admin-login-submit.json -w '%{http_code}' \
    --max-time 10 -c "$jar" -b "$jar" \
    -X POST -H "Content-Type: application/json" -H "Accept: application/json" \
    --data "$submit_body" \
    "$action_url" 2>&1 || true)"

  case "$submit_code" in
    200|303)
      :  # success path — cookie jar now carries ory_kratos_session
      ;;
    *)
      log "ERROR: login submit HTTP ${submit_code}; body:"
      head -c 500 /tmp/seed-admin-login-submit.json >&2 ; echo "" >&2
      return 2
      ;;
  esac

  # Step 4 — Extract ory_kratos_session cookie value from the jar
  local cookie_value
  cookie_value="$(awk '$6=="ory_kratos_session"{print $7}' "$jar" 2>/dev/null | tail -1)"

  if [ -z "$cookie_value" ] ; then
    log "ERROR: ory_kratos_session cookie absent from jar after submit"
    cat "$jar" >&2
    return 2
  fi

  mkdir -p "$(dirname "$SESSION_FILE")"
  printf '%s' "$cookie_value" > "$SESSION_FILE"
  chmod 0600 "$SESSION_FILE"
  log "Session cookie minted (${#cookie_value} chars) → ${SESSION_FILE} (mode 0600)"
}

# --- Verify (--verify flag) -------------------------------------------------
#
# Two-step verification:
#   (1) Kratos /sessions/whoami via cookie — proves the session is valid.
#   (2) Cloud /v1/auth/session/token exchange → JWT → /engine/api/v1/projects
#       — proves EC-3 auxiliary tables populated (otherwise 403 NO_ACTIVE_TENANT).
verify_session() {
  local cookie_value
  cookie_value="$(cat "$SESSION_FILE" 2>/dev/null || true)"
  if [ -z "$cookie_value" ] ; then
    log "ERROR: verify requested but session file empty"
    return 2
  fi

  # (1) Kratos whoami via cookie
  local code email
  code="$(curl -s -o /tmp/seed-admin-whoami.json -w '%{http_code}' \
    --max-time 5 -b "ory_kratos_session=${cookie_value}" \
    "${PUBLIC_KRATOS_URL}/sessions/whoami" 2>&1 || true)"

  if [ "$code" != "200" ] ; then
    log "ERROR: /sessions/whoami HTTP ${code} (expected 200; cookie may be stale)"
    return 2
  fi

  email="$(python3 -c '
import sys, json
try:
  data = json.load(open("/tmp/seed-admin-whoami.json"))
  sys.stdout.write(data.get("identity", {}).get("traits", {}).get("email", ""))
except Exception:
  pass
' 2>/dev/null || true)"

  if [ "$email" != "$EMAIL" ] ; then
    log "ERROR: /sessions/whoami email=${email} (expected ${EMAIL})"
    return 2
  fi

  log "Verified (1/2): /sessions/whoami 200 email=${email}"

  # (2) Cloud JWT exchange + engine API probe
  local jwt_code jwt body
  jwt_code="$(curl -s -o /tmp/seed-admin-jwt.json -w '%{http_code}' \
    --max-time 5 -X POST \
    -b "ory_kratos_session=${cookie_value}" \
    -H "Origin: https://app-dev.loopzero.example" \
    "${PUBLIC_CLOUD_URL}/v1/auth/session/token" 2>&1 || true)"

  if [ "$jwt_code" != "200" ] ; then
    log "ERROR: /v1/auth/session/token HTTP ${jwt_code}; body: $(head -c 200 /tmp/seed-admin-jwt.json)"
    return 2
  fi

  jwt="$(python3 -c '
import json, sys
try:
  d = json.load(open("/tmp/seed-admin-jwt.json"))
  sys.stdout.write(d.get("token") or d.get("access_token") or d.get("session_token") or "")
except Exception:
  pass
' 2>/dev/null || true)"

  if [ -z "$jwt" ] ; then
    log "ERROR: /v1/auth/session/token returned no token; body: $(head -c 200 /tmp/seed-admin-jwt.json)"
    return 2
  fi

  local proj_code
  proj_code="$(curl -s -o /tmp/seed-admin-projects.json -w '%{http_code}' \
    --max-time 5 -H "Authorization: Bearer ${jwt}" \
    "${PUBLIC_CLOUD_URL}/engine/api/v1/projects" 2>&1 || true)"

  case "$proj_code" in
    200)
      log "Verified (2/2 EC-3): /engine/api/v1/projects 200 — active tenant context honored"
      ;;
    403)
      body="$(head -c 200 /tmp/seed-admin-projects.json 2>/dev/null || true)"
      log "ERROR (EC-3): /engine/api/v1/projects 403 — body: ${body}"
      log "      Suggests tenant_members + user_identity_map + active_tenant_id NOT correctly populated."
      return 1
      ;;
    *)
      log "WARN (EC-3): /engine/api/v1/projects HTTP ${proj_code} (expected 200)"
      ;;
  esac
}

# --- HMAC session JWT persist (2026-06-15 canonical) ------------------------
#
# Mints loopzero-cloud HMAC session JWT via /v1/auth/session/token (cookie
# exchange) and persists to SESSION_JWT_FILE for dogfood --live-only
# consumption. Returns non-zero on transient failures (cloud unreachable,
# JWT mint API down); the caller treats as WARN since the Kratos session
# cookie remains valid and the JWT can be re-derived on demand.
SESSION_JWT_FILE="${SESSION_JWT_FILE:-$HOME/.loopzero/local-dogfood-session-jwt.txt}"

persist_session_jwt() {
  local cookie_value
  cookie_value="$(cat "$SESSION_FILE" 2>/dev/null || true)"
  if [ -z "$cookie_value" ] ; then
    return 1
  fi
  local jwt_code
  jwt_code="$(curl -s -o /tmp/seed-admin-jwt-persist.json -w '%{http_code}' \
    --max-time 5 -X POST \
    -b "ory_kratos_session=${cookie_value}" \
    -H "Origin: https://app-dev.loopzero.example" \
    "${PUBLIC_CLOUD_URL}/v1/auth/session/token" 2>&1 || true)"
  if [ "$jwt_code" != "200" ] ; then
    return 1
  fi
  local jwt
  jwt="$(python3 -c '
import json, sys
try:
  d = json.load(open("/tmp/seed-admin-jwt-persist.json"))
  sys.stdout.write(d.get("token") or d.get("access_token") or d.get("session_token") or "")
except Exception:
  pass
' 2>/dev/null || true)"
  if [ -z "$jwt" ] ; then
    return 1
  fi
  mkdir -p "$(dirname "$SESSION_JWT_FILE")"
  chmod 700 "$(dirname "$SESSION_JWT_FILE")" 2>/dev/null || true
  printf '%s' "$jwt" > "$SESSION_JWT_FILE"
  chmod 0600 "$SESSION_JWT_FILE"
  log "Session JWT persisted: ${SESSION_JWT_FILE} (len=${#jwt})"
  return 0
}

# --- Main -------------------------------------------------------------------

main() {
  if [ "$DRY_RUN" = "1" ] ; then
    log "[dry-run] would seed canonical user ${EMAIL} (refresh-only=${REFRESH_ONLY}, verify=${VERIFY})"
    return 0
  fi

  PG_PASSWORD="$(resolve_pg_password)"

  start_kratos_admin_pf || exit 2

  local kratos_id
  if [ "$REFRESH_ONLY" = "1" ] ; then
    log "(EC-9) --refresh-only — skipping identity + PG mutations"
    kratos_id="$(PSQL -tAc "SELECT id FROM users WHERE email='${EMAIL}'" 2>/dev/null || true)"
    if [ -z "$kratos_id" ] ; then
      log "ERROR: --refresh-only requires existing user; ${EMAIL} not in PG"
      exit 1
    fi
  else
    kratos_id="$(resolve_or_create_kratos_identity)" || exit 1
    if [ -z "$kratos_id" ] ; then
      log "ERROR: identity resolution returned empty id"
      exit 1
    fi
    ensure_pg_state "$kratos_id" > /dev/null || exit 2
  fi

  mint_session "$kratos_id" || exit 2

  if [ "$VERIFY" = "1" ] ; then
    verify_session || exit $?
  fi

  # CANONICAL FIX 2026-06-15: persist HMAC session JWT alongside the
  # Kratos session cookie. The HMAC JWT (aud="loopzero-cloud", carries
  # tenant_id from active session) is the only JWT shape that passes
  # loopzero-cloud's middleware on /engine/* proxy paths. dogfood --live-only
  # tests now prefer this file over the bootstrap RSA JWT
  # (~/.loopzero/local-dogfood-token.txt; aud="loopzero-engine", NO tenant_id)
  # which is engine-direct only.
  persist_session_jwt || log "WARN: session JWT persist skipped (--verify did not mint OR cloud unreachable)"

  log "DONE — session ready at ${SESSION_FILE}"
}

main "$@"
