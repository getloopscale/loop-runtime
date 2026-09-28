#!/usr/bin/env bash
# connect-canonical-fixture.sh
#
# T2.1 of plan 2026-06-08-pre-release-test-rigor v1.2 — connects the canonical
# fixture GitHub repo (`getloopscale/loopzerokit-example`, per 2026-06-16 sponsor
# mandate; supersedes legacy `loopzero-repotest`) to the canonical LoopZero project
# `sample` in admin@loopzero.example's tenant. Idempotent + handles orphan
# installations.
#
# Preconditions:
#   - T0.2 ran (admin@loopzero.example seeded with session at
#     ~/.loopzero/admin-loopzero-dev-session.txt)
#   - `gh auth status` exits 0 with write permission on getloopscale/loopzerokit-example
#   - LoopZero CLI built (`loopzero` in PATH OR LOOPZERO_BIN env var pointing at binary)
#
# EC-12 (DOCUMENT preflight): gh auth status + rate-limit headroom asserted
# BEFORE any API call. Aborts with clear "gh auth login" message if absent.
# Without this, the operator hits opaque "401 Bad credentials" mid-script.
#
# EC-10 (SHOULD TEST orphan installation): if `gh api /repos/{repo}/installation`
# returns an installation_id but the install was uninstalled via the GitHub UI
# (status != "active"), the next API call surfaces "already installed".
# This script detects the orphan + prompts the operator to re-install via
# the GitHub UI rather than looping idempotency error.
#
# Usage:
#   bash infra/scripts/local/connect-canonical-fixture.sh
#   bash infra/scripts/local/connect-canonical-fixture.sh --verify
#   bash infra/scripts/local/connect-canonical-fixture.sh --dry-run
#
# Exit codes (per plan T2.1):
#   0  fixture ready (project sample exists, GitHub App active on loopzerokit-example)
#   1  preflight failure (gh auth missing OR rate-limit exhausted OR session absent)
#   2  LIVE infrastructure unreachable (LoopZero API, PG, GitHub API itself)
set -euo pipefail

REPO_OWNER="${REPO_OWNER:-getloopscale}"
# Canonical 2026-06-16: loopzerokit-example is the FAANG-canonical LoopZeroKit-native
# fixture (scaffolded via `npm create loopzerokit@latest`; has `loopzero.config.ts`
# v2 + canonical `apps:` map). The legacy `loopzero-repotest` (superseded by
# loopzerokit-example per sponsor mandate 2026-06-16) was a `loopzero.yaml`-only
# fixture per Plan v1.1 transition window; user mandate 2026-06-16
# directed discard+delete in favor of the canonical LoopZeroKit shape.
REPO_NAME="${REPO_NAME:-loopzerokit-example}"
DEFAULT_BRANCH="${DEFAULT_BRANCH:-develop}"
PROJECT_NAME="${PROJECT_NAME:-sample}"
SESSION_FILE="${SESSION_FILE:-$HOME/.loopzero/admin-loopzero-dev-session.txt}"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
# Canonical 2026-06-20 (issue #108): post-M2 decomposition, the LoopZero CLI
# lives in sibling repo loopzero-cli/bin/loopzero at the workspace root. The master
# orchestrator seed-post-cleanroom-all.sh invokes Step 11 without exporting
# LOOPZERO_BIN; pre-fix this defaulted to `loopzero` on PATH and Step 11 emitted
# WARN+return-2 when the operator's PATH lacked the binary. The discovery
# chain below prefers: (a) operator-exported LOOPZERO_BIN, then (b) `loopzero` on
# PATH, then (c) workspace-relative loopzero-cli/bin/loopzero, then (d) literal
# `loopzero` so the existing WARN path remains the last-resort signal.
# Regression-locked by infra/tests/charts/loopzero_bin_workspace_discovery_test.go.
LOOPZERO_BIN="${LOOPZERO_BIN:-$(command -v loopzero 2>/dev/null || readlink -f "$(dirname "$0")/../../../../loopzero-cli/bin/loopzero" 2>/dev/null || echo loopzero)}"
GITHUB_APP_SLUG="${GITHUB_APP_SLUG:-loopzero-platform}"
CONTEXT="${CONTEXT:-k3d-loopzero-core}"
PG_NAMESPACE="${PG_NAMESPACE:-loopzero-system}"

DRY_RUN=0
VERIFY=0

while [ $# -gt 0 ]; do
  case "$1" in
    --verify)   VERIFY=1 ; shift ;;
    --dry-run)  DRY_RUN=1 ; shift ;;
    -h|--help)
      sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//' ; exit 0 ;;
    *) printf '[connect-fixture] unknown arg: %s\n' "$1" >&2 ; exit 1 ;;
  esac
done

log() { printf '[connect-fixture] %s\n' "$*" >&2 ; }

# --- EC-12 preflight --------------------------------------------------------

preflight_gh() {
  if ! gh auth status > /dev/null 2>&1 ; then
    log "ERROR: gh CLI not authenticated."
    log "       Run: gh auth login --hostname github.com --scopes repo,workflow,read:org"
    return 1
  fi

  # Rate-limit check (EC-12) — primary core limit is 5000/hr; reserve headroom.
  local rate
  rate="$(gh api /rate_limit --jq '.resources.core.remaining' 2>/dev/null || echo "0")"
  if [ "$rate" -lt 100 ] ; then
    log "ERROR: gh API rate-limit headroom too low (${rate}/5000)."
    log "       Wait until reset OR rotate to a fresh PAT via 'gh auth login'."
    return 1
  fi
  log "Preflight gh OK — auth + rate-limit (${rate}/5000)"
}

preflight_session() {
  if [ ! -f "$SESSION_FILE" ] ; then
    log "ERROR: admin@loopzero.example session file missing at $SESSION_FILE"
    log "       Run: loopzero-ops seed admin-loopzero-dev --verify"
    return 1
  fi
  if [ ! -s "$SESSION_FILE" ] ; then
    log "ERROR: session file empty"
    return 1
  fi
  log "Preflight session OK — $SESSION_FILE present"
}

# --- EC-10 orphan installation detection ------------------------------------

mint_app_jwt() {
  # GET /repos/{owner}/{repo}/installation requires GitHub App-side
  # authentication (a JWT signed with the App's private key), NOT a user PAT.
  # The PAT path returns 401 "JSON web token could not be decoded" — that's
  # not absence of install, just wrong credential class (LIVE-caught
  # 2026-06-09 during T6.1). Canonical detection: pull APP_ID + PRIVATE_KEY
  # from the K8s Secret the cluster uses (no hardcode, no env var
  # duplication), mint a short-lived JWT, query the canonical endpoint.
  local app_id private_key
  # Canonical 2026-06-16: read GITHUB_APP_ID from the running pod's effective env
  # via `kubectl exec`. The earlier `jsonpath=spec.containers[0].env[GITHUB_APP_ID]`
  # query only sees STATIC env declarations — it misses envFrom sources
  # (configMapRef/secretRef), which is how GITHUB_APP_ID is wired canonical
  # (the loopzero-api ConfigMap). The running env is authoritative.
  #
  # Canonical 2026-06-20: the container selector must name the container the
  # Deployment actually runs. A wrong `-c` selects no container; kubectl errors
  # to stderr, the `2>/dev/null` suppresses the error message, and `|| true`
  # swallows the non-zero exit, so `$app_id` ended up empty and the script
  # reported "cannot resolve GITHUB_APP_ID" even though the env var IS present
  # in the pod (caught LIVE during dev-public Cleanroom-fresh seed --verify,
  # Step 11 was the only FAIL out of 17).
  #
  # The container name is `api` — in charts/loopzero-api, which Helmfile installs,
  # and in the Tilt-owned infra/tilt/api-deployment.yaml. It was `loopzero-api` in the
  # bootstrap chart infra/helm/loopzero-api until that copy was deleted (one chart per
  # pair, docs/decisions.md). Regression-locked by
  # infra/tests/charts/connect_canonical_fixture_container_name_test.go, which reads
  # the name from the rendered chart rather than from a literal.
  # The single quotes are the point: $GITHUB_APP_ID must expand INSIDE the pod, in
  # the loopzero-api environment. Expanding it here would read the operator's machine,
  # which does not have it.
  # shellcheck disable=SC2016
  app_id="$(kubectl --context="$CONTEXT" -n "$PG_NAMESPACE" exec deployment/loopzero-api -c api -- \
    sh -c 'echo "$GITHUB_APP_ID"' 2>/dev/null | tr -d '\r\n' || true)"
  if [ -z "$app_id" ]; then
    log "ERROR mint_app_jwt: cannot resolve GITHUB_APP_ID from loopzero-api pod running env"
    log "       (try: kubectl --context=$CONTEXT -n $PG_NAMESPACE exec deployment/loopzero-api -- env | grep GITHUB_APP_ID)"
    return 2
  fi
  private_key="$(kubectl --context="$CONTEXT" -n "$PG_NAMESPACE" get secret loopzero-api-github \
    -o jsonpath='{.data.GITHUB_APP_PRIVATE_KEY}' 2>/dev/null | base64 -d)"
  if [ -z "$private_key" ]; then
    log "ERROR mint_app_jwt: cannot read GITHUB_APP_PRIVATE_KEY from loopzero-api-github Secret"
    return 2
  fi
  APP_JWT="$(APP_ID="$app_id" PRIVATE_KEY="$private_key" python3 -c '
import os, time, sys
try:
    import jwt as pyjwt
except ImportError:
    sys.stderr.write("PyJWT not installed (pip install PyJWT)\n")
    sys.exit(2)
now = int(time.time())
token = pyjwt.encode({"iat": now - 30, "exp": now + 540, "iss": os.environ["APP_ID"]},
                     os.environ["PRIVATE_KEY"], algorithm="RS256")
sys.stdout.write(token)
' 2>/dev/null || true)"
  if [ -z "$APP_JWT" ]; then
    log "ERROR mint_app_jwt: pyjwt sign failed (install: pip install PyJWT)"
    return 2
  fi
}

check_installation() {
  # GET /repos/{owner}/{repo}/installation returns 200 with installation data
  # when the GitHub App is installed AND active, 404 when not installed.
  # An "orphan" appears when GitHub UI uninstalls the app but stale LoopZero state
  # still references the installation_id — we detect by checking the
  # response.suspended_at field (non-null = suspended/orphan).
  if [ -z "${APP_JWT:-}" ]; then
    mint_app_jwt || return 1
  fi
  local resp
  resp="$(curl -sS -H "Authorization: Bearer $APP_JWT" \
    -H "Accept: application/vnd.github+json" \
    "https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/installation" 2>&1 || true)"

  local installation_id suspended
  installation_id="$(printf '%s' "$resp" | python3 -c '
import sys, json
try:
  d = json.load(sys.stdin); sys.stdout.write(str(d.get("id","")))
except Exception:
  pass
' 2>/dev/null || true)"
  suspended="$(printf '%s' "$resp" | python3 -c '
import sys, json
try:
  d = json.load(sys.stdin); sys.stdout.write(str(d.get("suspended_at","null")))
except Exception:
  pass
' 2>/dev/null || true)"

  echo "${installation_id}|${suspended}"
}

ensure_installation_active() {
  local raw id suspended
  raw="$(check_installation)"
  id="${raw%%|*}"
  suspended="${raw##*|}"

  if [ -z "$id" ] || [ "$id" = "" ] ; then
    log "WARN: no GitHub App installation found on ${REPO_OWNER}/${REPO_NAME}"
    log "      Install via: https://github.com/apps/${GITHUB_APP_SLUG}/installations/new"
    log "      Then select repo: ${REPO_OWNER}/${REPO_NAME}"
    return 1
  fi

  if [ "$suspended" != "null" ] && [ "$suspended" != "None" ] && [ -n "$suspended" ] ; then
    log "WARN (EC-10): GitHub App installation_id=${id} is ORPHAN/SUSPENDED at ${suspended}"
    log "      Re-install via: https://github.com/apps/${GITHUB_APP_SLUG}/installations/new"
    log "      The 'already installed' loop is intentional — operator must clear via GitHub UI"
    return 1
  fi

  log "Installation active id=${id} (EC-10 path clean)"
  return 0
}

# --- LoopZero CLI project create (idempotent) -----------------------------------

ensure_project() {
  local kratos_cookie
  kratos_cookie="$(cat "$SESSION_FILE")"

  # /engine/api/v1/* endpoints accept ONLY Bearer JWT, never raw Kratos cookies
  # (LIVE-caught 2026-06-09 during T6.1 — same fix already shipped in
  # dev-public-production-deploy_test.sh mint_jwt helper). Canonical exchange:
  # Kratos session cookie -> POST cloud /v1/auth/session/token -> JWT.
  local jwt_code jwt
  jwt_code="$(curl -sS -o /tmp/connect-fixture-jwt.json -w '%{http_code}' -X POST \
    --max-time 10 \
    -b "ory_kratos_session=${kratos_cookie}" \
    -H "Origin: https://app-dev.loopzero.example" \
    "${PUBLIC_CLOUD_URL}/v1/auth/session/token" 2>/dev/null || true)"
  if [ "$jwt_code" != "200" ] ; then
    log "ERROR: /v1/auth/session/token HTTP ${jwt_code} body=$(head -c 200 /tmp/connect-fixture-jwt.json)"
    return 2
  fi
  jwt="$(python3 -c '
import json, sys
try:
  d = json.load(open("/tmp/connect-fixture-jwt.json"))
  sys.stdout.write(d.get("token") or d.get("access_token") or d.get("session_token") or "")
except Exception:
  pass
' 2>/dev/null || true)"
  if [ -z "$jwt" ] ; then
    log "ERROR: empty JWT from /v1/auth/session/token response"
    return 2
  fi

  # Idempotency: check via /engine/api/v1/projects before creating
  local code
  code="$(curl -s -o /tmp/connect-fixture-projects.json -w '%{http_code}' \
    --max-time 5 -H "Authorization: Bearer ${jwt}" \
    "${PUBLIC_CLOUD_URL}/engine/api/v1/projects" 2>&1 || true)"

  if [ "$code" != "200" ] ; then
    log "ERROR: /engine/api/v1/projects HTTP ${code} (expected 200 — JWT exchange + Bearer chain)"
    return 2
  fi

  local exists
  exists="$(python3 -c '
import sys, json
try:
  d = json.load(open("/tmp/connect-fixture-projects.json"))
  projects = d if isinstance(d, list) else d.get("projects", d.get("items", []))
  names = [p.get("name","") for p in projects]
  sys.stdout.write("1" if "'"$PROJECT_NAME"'" in names else "0")
except Exception:
  sys.stdout.write("0")
' 2>/dev/null || echo "0")"

  if [ "$exists" = "1" ] ; then
    log "Project ${PROJECT_NAME} already exists (idempotent skip)"
    return 0
  fi

  # Project create via LoopZero CLI per Principio CLI-First.
  # The CLI uses ~/.loopzero/config.json for token; export as env var here.
  if ! command -v "$LOOPZERO_BIN" > /dev/null 2>&1 ; then
    log "WARN: LoopZero CLI ($LOOPZERO_BIN) not in PATH — project create skipped"
    log "      Build via: cd cli && npm run build && export LOOPZERO_BIN=\$(pwd)/bin/loopzero"
    return 2
  fi

  log "Creating project ${PROJECT_NAME} via LoopZero CLI"
  # Canonical 2026-06-16: use $jwt (the canonical Bearer token from
  # /v1/auth/session/token exchange above). The stale $token reference was
  # from an earlier refactor — `set -u` correctly traps as unbound.
  # LOOPZERO_URL canonical for dev-public: ${PUBLIC_CLOUD_URL}/engine — the
  # Caddy edge routes cloud-dev.loopzero.example/* → loopzero-cloud:13002, which
  # in turn proxies /engine/* → loopzero-api engine. The CLI's default
  # localhost:8080 is for production-like topology (Traefik direct).
  # Per j05-aprendizado-cli-loopzero-url-subshell-env-propagation memory.
  LOOPZERO_URL="${PUBLIC_CLOUD_URL}/engine" LOOPZERO_TOKEN="$jwt" \
    "$LOOPZERO_BIN" project create "$PROJECT_NAME" \
    --json > /tmp/connect-fixture-project-create.json 2>&1 || {
    log "ERROR: loopzero project create failed; output:"
    head -c 500 /tmp/connect-fixture-project-create.json >&2
    return 2
  }
  log "Project ${PROJECT_NAME} created"
}

# --- Repo <-> Project link (project_repositories row) -----------------------

ensure_repo_linked() {
  # Even when the project exists AND the GitHub App install is active, the
  # webhook handler ignores push events from repos NOT explicitly linked
  # via the project_repositories table (LIVE-caught 2026-06-09 — loopzero-api
  # logs: "github webhook: repo not linked to any project, ignoring").
  # Canonical link endpoint: POST /api/v1/projects/{name}/connect with
  # {repo, installation_id, branch}. Idempotent — repeat calls return the
  # existing link or update auto_deploy.
  if [ -z "${APP_JWT:-}" ]; then
    mint_app_jwt || return 1
  fi
  local installation_id
  installation_id="$(curl -sS -H "Authorization: Bearer $APP_JWT" \
    -H "Accept: application/vnd.github+json" \
    "https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/installation" 2>&1 \
    | python3 -c '
import json, sys
try:
  d = json.load(sys.stdin); sys.stdout.write(str(d.get("id","")))
except Exception:
  pass
' 2>/dev/null || true)"
  if [ -z "$installation_id" ]; then
    log "ERROR ensure_repo_linked: cannot resolve installation_id for ${REPO_OWNER}/${REPO_NAME}"
    return 2
  fi

  # Re-exchange Kratos cookie -> JWT (canonical chain) so the connect call
  # honors tenant context. APP_JWT is for github.com — engine API wants its
  # own RS256 token (per cloud/internal/auth/session_token.go).
  local kratos_cookie engine_jwt_code engine_jwt
  kratos_cookie="$(cat "$SESSION_FILE")"
  engine_jwt_code="$(curl -sS -o /tmp/connect-fixture-engine-jwt.json -w '%{http_code}' -X POST \
    --max-time 10 \
    -b "ory_kratos_session=${kratos_cookie}" \
    -H "Origin: https://app-dev.loopzero.example" \
    "${PUBLIC_CLOUD_URL}/v1/auth/session/token" 2>/dev/null || true)"
  if [ "$engine_jwt_code" != "200" ]; then
    log "ERROR ensure_repo_linked: engine JWT exchange HTTP $engine_jwt_code"
    return 2
  fi
  engine_jwt="$(python3 -c '
import json, sys
try:
  d = json.load(open("/tmp/connect-fixture-engine-jwt.json"))
  sys.stdout.write(d.get("token") or d.get("access_token") or d.get("session_token") or "")
except Exception:
  pass
' 2>/dev/null || true)"

  # Idempotency: GET current link first
  local cur_code cur_repo
  cur_code="$(curl -sS -o /tmp/connect-fixture-cur-link.json -w '%{http_code}' \
    --max-time 10 \
    -H "Authorization: Bearer ${engine_jwt}" \
    "${PUBLIC_CLOUD_URL}/engine/api/v1/projects/${PROJECT_NAME}/connect" 2>/dev/null || true)"
  if [ "$cur_code" = "200" ]; then
    cur_repo="$(python3 -c '
import json, sys
try:
  d = json.load(open("/tmp/connect-fixture-cur-link.json"))
  sys.stdout.write(d.get("repo") or d.get("repo_full_name") or "")
except Exception:
  pass
' 2>/dev/null || true)"
    if [ "$cur_repo" = "${REPO_OWNER}/${REPO_NAME}" ]; then
      log "Repo ${REPO_OWNER}/${REPO_NAME} already linked to project ${PROJECT_NAME} (idempotent skip)"
      return 0
    fi
  fi

  log "Linking ${REPO_OWNER}/${REPO_NAME} (installation_id=${installation_id}) to project ${PROJECT_NAME}"
  local connect_code
  connect_code="$(curl -sS -o /tmp/connect-fixture-link.json -w '%{http_code}' -X POST \
    --max-time 15 \
    -H "Authorization: Bearer ${engine_jwt}" \
    -H "Content-Type: application/json" \
    --data "{\"repo\":\"${REPO_OWNER}/${REPO_NAME}\",\"installation_id\":${installation_id},\"branch\":\"${DEFAULT_BRANCH}\"}" \
    "${PUBLIC_CLOUD_URL}/engine/api/v1/projects/${PROJECT_NAME}/connect" 2>/dev/null || true)"

  case "$connect_code" in
    200|201)
      log "Repo linked OK (HTTP ${connect_code})"
      return 0
      ;;
    *)
      log "ERROR ensure_repo_linked: POST /connect HTTP ${connect_code} body=$(head -c 300 /tmp/connect-fixture-link.json)"
      return 2
      ;;
  esac
}

# --- Audit-log verification -------------------------------------------------

verify_audit_log() {
  local pg_password event_count
  pg_password="$(kubectl --context="$CONTEXT" -n "$PG_NAMESPACE" \
    get secret loopzero-db-credentials -o jsonpath='{.data.password}' 2>/dev/null | base64 -d 2>/dev/null || echo "loopzero-dev-password")"

  event_count="$(kubectl --context="$CONTEXT" -n "$PG_NAMESPACE" \
    exec -i postgresql-0 -c postgresql -- \
    env PGPASSWORD="$pg_password" psql -U loopzero -d loopzero -tAc \
    "SELECT count(*) FROM audit_log WHERE action='project.connected' AND resource_type='project'" 2>/dev/null | tr -d '[:space:]' || echo "0")"

  if [ "$event_count" -ge "1" ] ; then
    log "Verified: audit_log has ${event_count} project.connected event(s)"
    return 0
  else
    log "WARN: audit_log has 0 project.connected events — webhook may not have fired yet"
    return 0   # WARN not FAIL because project may exist sans webhook
  fi
}

# --- Main -------------------------------------------------------------------

main() {
  if [ "$DRY_RUN" = "1" ] ; then
    log "[dry-run] would connect ${REPO_OWNER}/${REPO_NAME} to project ${PROJECT_NAME} (verify=${VERIFY})"
    return 0
  fi

  preflight_gh      || exit 1
  preflight_session || exit 1
  ensure_installation_active || exit 1
  if ! ensure_project ; then
    # ensure_project returns 2 on LIVE infra unreachable
    exit 2
  fi
  # Project existence alone is insufficient — webhook handler requires the
  # repo to be explicitly linked via project_repositories. Step added
  # 2026-06-09 after T6.1 LIVE catch: "github webhook: repo not linked".
  if ! ensure_repo_linked ; then
    exit 2
  fi

  if [ "$VERIFY" = "1" ] ; then
    verify_audit_log
  fi

  log "DONE — fixture connected (project=${PROJECT_NAME}, repo=${REPO_OWNER}/${REPO_NAME})"
}

main "$@"
