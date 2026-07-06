#!/usr/bin/env bash
# seed-github-webhook-fixture-canonical.sh
#
# Canonical POST-CLEANROOM seed for the GitHub webhook on the canonical
# fixture repository (getloopscale/loopzerokit-example). Without this webhook,
# `loopzero push` on the fixture repo silently no-ops in production — GitHub
# never delivers the `push` event to `loopzero-api /api/v1/webhooks/github`,
# and autodeploy never fires. Every LIVE chain that depends on
# push→autodeploy→ArgoCD→HTTPS 200 (dogfood-webhook-chain, chaos c11
# webhook-concurrent-podkill, contra-prova sponsor JUNTOS) then FAILs
# with "chain link N (workflow submit) not reached".
#
# The GitHub App itself (installation, PEM, webhook secret) is already
# canonical-seeded by seed-github-app-dev.sh. This script closes the
# separate seam: the REPO-level webhook is a per-repo config that lives
# on the GitHub side, NOT in the App installation, and it MUST point at
# the dev-public Caddy edge (https://app-dev.loopzero.example/api/v1/webhooks/github)
# with the HMAC secret from ~/.loopzero/dev-credentials.json.github.webhook_secret.
#
# Behavior:
#   - Reads webhook secret from ~/.loopzero/dev-credentials.json (mode 0600
#     enforced pre-read) — the same SoT as seed-github-app-dev.sh.
#   - GET /repos/{owner}/{repo}/hooks → checks for a hook whose
#     config.url equals the canonical URL; PATCHes it in place (secret
#     rotation) OR POSTs a new one.
#   - Enables events: push + pull_request (the autodeploy chain payload
#     shape; loopzero-api's OnPush handler routes both).
#   - Sends a test ping via POST /hooks/{id}/tests to prove delivery.
#
# Idempotent. Fails loud on any anti-pattern (no gh auth, repo access,
# missing secret). Per the INQUEBRÁVEL feedback
# `feedback_issue_fix_requires_failing_e2e_test_2026_06_25`: this is the
# canonical setup step that makes the LIVE webhook chain reproducible
# on a fresh Cleanroom.
#
# Usage:
#   bash infra/scripts/local/seed-github-webhook-fixture-canonical.sh [--verify] [--dry-run]

set -euo pipefail

REPO_OWNER="${LOOPZERO_FIXTURE_REPO_OWNER:-getloopscale}"
REPO_NAME="${LOOPZERO_FIXTURE_REPO_NAME:-loopzerokit-example}"
WEBHOOK_URL="${LOOPZERO_WEBHOOK_URL:-https://app-dev.loopzero.example/api/v1/webhooks/github}"
DEV_CREDENTIALS="${LOOPZERO_DEV_CREDENTIALS:-$HOME/.loopzero/dev-credentials.json}"

VERIFY=0
DRY_RUN=0
for arg in "$@"; do
  case "$arg" in
    --verify)  VERIFY=1 ;;
    --dry-run) DRY_RUN=1 ;;
    -h|--help)
      sed -n '2,35p' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) echo "unknown arg: $arg" >&2 ; exit 1 ;;
  esac
done

log() { printf '[seed-webhook] %s\n' "$*" ; }

# --- Pre-flight -------------------------------------------------------------

if ! command -v gh >/dev/null 2>&1 ; then
  log "ERROR: gh CLI not found on PATH"
  exit 2
fi

if ! gh auth status >/dev/null 2>&1 ; then
  log "ERROR: gh not authenticated. Run: gh auth login --hostname github.com --scopes repo,workflow,admin:repo_hook"
  exit 2
fi

if [ ! -f "$DEV_CREDENTIALS" ] ; then
  log "SKIP: $DEV_CREDENTIALS not found — cannot resolve webhook secret"
  exit 0
fi

mode="$(stat -c '%a' "$DEV_CREDENTIALS" 2>/dev/null || stat -f '%A' "$DEV_CREDENTIALS" 2>/dev/null || echo '???')"
if [ "$mode" != "600" ] ; then
  log "ERROR: $DEV_CREDENTIALS mode is ${mode} (require 0600). Run: chmod 0600 $DEV_CREDENTIALS"
  exit 2
fi

WHSEC="$(python3 -c "import json,sys;d=json.load(open('$DEV_CREDENTIALS'));print((d.get('github') or {}).get('webhook_secret') or '')" 2>/dev/null || true)"
if [ -z "$WHSEC" ] ; then
  log "SKIP: dev-credentials.json has no github.webhook_secret"
  exit 0
fi

# Repo admin/push permission gate — cannot mutate hooks without it.
perms_json="$(gh api "/repos/${REPO_OWNER}/${REPO_NAME}" 2>/dev/null || true)"
if [ -z "$perms_json" ] ; then
  log "ERROR: cannot read /repos/${REPO_OWNER}/${REPO_NAME} — check gh auth + repo access"
  exit 2
fi
admin="$(printf '%s' "$perms_json" | python3 -c "import sys,json;d=json.load(sys.stdin);p=d.get('permissions',{});print(bool(p.get('admin')))" 2>/dev/null || echo "False")"
if [ "$admin" != "True" ] ; then
  log "ERROR: token lacks admin permission on ${REPO_OWNER}/${REPO_NAME} — need admin:repo_hook scope"
  exit 2
fi

log "canonical target: ${REPO_OWNER}/${REPO_NAME} → ${WEBHOOK_URL}"

# --- Idempotency probe: existing hook matching canonical URL? ---------------

existing_id="$(gh api "/repos/${REPO_OWNER}/${REPO_NAME}/hooks" 2>/dev/null \
  | python3 -c "
import sys,json
hooks=json.load(sys.stdin)
for h in hooks:
    cfg=h.get('config') or {}
    if cfg.get('url')=='${WEBHOOK_URL}':
        print(h.get('id',''))
        sys.exit(0)
" 2>/dev/null || true)"

if [ -n "$existing_id" ] ; then
  log "existing webhook id=${existing_id} at canonical URL — updating secret + events"
  if [ "$DRY_RUN" = "1" ] ; then
    log "DRY: PATCH /hooks/${existing_id} events + secret"
  else
    gh api -X PATCH "/repos/${REPO_OWNER}/${REPO_NAME}/hooks/${existing_id}" \
      -F "active=true" \
      -f "events[]=push" -f "events[]=pull_request" \
      -f "config[url]=${WEBHOOK_URL}" \
      -f "config[content_type]=json" \
      -f "config[secret]=${WHSEC}" \
      -f "config[insecure_ssl]=0" \
      >/dev/null
    log "PATCH OK — canonical webhook refreshed"
  fi
  HOOK_ID="$existing_id"
else
  log "no matching webhook — creating"
  if [ "$DRY_RUN" = "1" ] ; then
    log "DRY: POST /hooks (name=web, active=true, push+pull_request)"
    HOOK_ID=""
  else
    HOOK_ID="$(gh api -X POST "/repos/${REPO_OWNER}/${REPO_NAME}/hooks" \
      -f "name=web" -F "active=true" \
      -f "events[]=push" -f "events[]=pull_request" \
      -f "config[url]=${WEBHOOK_URL}" \
      -f "config[content_type]=json" \
      -f "config[secret]=${WHSEC}" \
      -f "config[insecure_ssl]=0" \
      2>/dev/null | python3 -c "import sys,json;print(json.load(sys.stdin).get('id',''))" 2>/dev/null || true)"
    if [ -z "$HOOK_ID" ] ; then
      log "ERROR: POST /hooks failed"
      exit 3
    fi
    log "POST OK — new hook id=${HOOK_ID}"
  fi
fi

# --- Verification (--verify): send test ping, poll delivery status ----------

if [ "$VERIFY" = "1" ] && [ "$DRY_RUN" != "1" ] && [ -n "$HOOK_ID" ] ; then
  log "sending test ping via POST /hooks/${HOOK_ID}/tests"
  gh api -X POST "/repos/${REPO_OWNER}/${REPO_NAME}/hooks/${HOOK_ID}/tests" >/dev/null 2>&1 || true
  sleep 4
  status="$(gh api "/repos/${REPO_OWNER}/${REPO_NAME}/hooks/${HOOK_ID}/deliveries?per_page=3" 2>/dev/null \
    | python3 -c "
import sys,json
d=json.load(sys.stdin)
if not d:
    print('no_deliveries'); sys.exit(0)
first=d[0]
print(f\"{first.get('status_code','?')}:{first.get('event','?')}\")
" 2>/dev/null || echo "unknown")"
  case "$status" in
    200:*|201:*|202:*|204:*)
      log "  verify OK — most recent delivery ${status}" ;;
    *)
      log "  verify WARN — most recent delivery ${status} (loopzero-api may need rollout / Caddyfile handle absent)"
      log "  investigate: gh api /repos/${REPO_OWNER}/${REPO_NAME}/hooks/${HOOK_ID}/deliveries" ;;
  esac
fi

log "DONE"
exit 0
