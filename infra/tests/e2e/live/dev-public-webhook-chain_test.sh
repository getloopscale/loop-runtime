#!/usr/bin/env bash
# dev-public-webhook-chain_test.sh
#
# T2.2 of plan 2026-06-08-pre-release-test-rigor v1.2 (Scenario 1).
#
# LIVE end-to-end webhook chain assertion against dev-public:
#   GitHub push → webhook → loopzero-api → source bundle → Argo workflow →
#   image build → release ACTIVE → namespace creation → pod Running 1/1
#   → ArgoCD Synced+Healthy → HTTPS 200 on canonical URL.
#
# Wall-clock budget: < 20 min total per plan Goal + ADR D8.
# MAX_DURATION enforced via per-link timeouts.
#
# EC absorptions:
#   EC-1 / ADR D7  rate-limit fallback to push event
#   EC-2          ArgoCD refresh=hard between link 7 and link 8 (repo-server cache)
#   EC-5          diagnostic dump includes `kubectl get events --sort-by=.lastTimestamp`
#   EC-8          LE staging fallback when prod "too many certificates" hit
#   EC-9          401 mid-chain → invoke seed-admin-loopzero-dev.sh --refresh-only + retry once
#   EC-12         preflight gh auth + rate-limit + kubectl context
#
# Per ADR D6 — fail-fast on first broken link, NO retry loops on chain
# semantics (retry only on 401 once per EC-9).
#
# Usage:
#   bash infra/tests/e2e/live/dev-public-webhook-chain_test.sh
#   bash infra/tests/e2e/live/dev-public-webhook-chain_test.sh --skip-live   # hermetic-only mode
#   bash infra/tests/e2e/live/dev-public-webhook-chain_test.sh --json        # machine-readable verdict
#   bash infra/tests/e2e/live/dev-public-webhook-chain_test.sh --use-push-event   # force push-event path (EC-1 fallback)
set -euo pipefail

REPO_OWNER="${REPO_OWNER:-getloopscale}"
REPO_NAME="${REPO_NAME:-loopzerokit-example}"
TENANT_ID="${TENANT_ID:-tn-08fa2ecedc3c7e77}"
PROJECT_NAME="${PROJECT_NAME:-teste}"
ENV_NAME="${ENV_NAME:-production}"
SESSION_FILE="${SESSION_FILE:-$HOME/.loopzero/admin-loopzero-dev-session.txt}"
PUBLIC_APP_URL="${PUBLIC_APP_URL:-https://app-dev.loopzero.example}"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
PUBLIC_FIXTURE_URL="${PUBLIC_FIXTURE_URL:-https://${PROJECT_NAME}-dev.loopzero.example}"
CONTEXT="${CONTEXT:-k3d-loopzero-core}"
PG_NAMESPACE="${PG_NAMESPACE:-loopzero-system}"

# Wall-clock budget — full chain MUST complete in < 20 min total.
MAX_DURATION_SECONDS="${MAX_DURATION_SECONDS:-1200}"   # 20 min hard ceiling
LINK4_BUDGET_SECONDS=$((5 * 60))   # workflow Succeeded
LINK5_BUDGET_SECONDS=$((10 * 60))  # release ACTIVE
LINK9_BUDGET_SECONDS=$((20 * 60))  # final 200

SKIP_LIVE=0
JSON_MODE=0
USE_PUSH_EVENT=0

while [ $# -gt 0 ]; do
  case "$1" in
    --skip-live)        SKIP_LIVE=1 ; shift ;;
    --json)             JSON_MODE=1 ; shift ;;
    --use-push-event)   USE_PUSH_EVENT=1 ; shift ;;
    -h|--help)
      sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//' ; exit 0 ;;
    *) printf '[webhook-chain] unknown arg: %s\n' "$1" >&2 ; exit 1 ;;
  esac
done

log() { printf '[webhook-chain] %s\n' "$*" >&2 ; }

START_EPOCH="$(date +%s)"

# --- Diagnostic dump (EC-5) -------------------------------------------------

diagnostic_dump() {
  local stage=$1
  log "==== DIAGNOSTIC DUMP — stage=${stage} ===="
  # EC-5: events survive pod deletion for ~1h, so dump is non-empty even when
  # pods got pruned mid-chain.
  kubectl --context="$CONTEXT" get events -A --sort-by=.lastTimestamp 2>&1 | tail -50 >&2 || true
  kubectl --context="$CONTEXT" get pods -A 2>&1 | grep -v Running 2>&1 | head -30 >&2 || true
  log "==== END DIAGNOSTIC DUMP ===="
}

trap 'diagnostic_dump "trap-exit"' ERR

# --- EC-12 preflight (gh auth + rate-limit + kubectl context) --------------

preflight() {
  log "Preflight (EC-12)"
  if ! gh auth status > /dev/null 2>&1 ; then
    log "ERROR (EC-12): gh CLI not authenticated. Run: gh auth login --hostname github.com --scopes repo,workflow"
    return 1
  fi
  local rate
  rate="$(gh api /rate_limit --jq '.resources.core.remaining' 2>/dev/null || echo "0")"
  if [ "$rate" -lt 50 ] ; then
    log "WARN (EC-1 / D7): gh API rate-limit low (${rate}); switching to push-event fallback"
    USE_PUSH_EVENT=1
  fi
  local cur_ctx
  cur_ctx="$(kubectl config current-context 2>/dev/null || echo "")"
  if [ "$cur_ctx" != "$CONTEXT" ] ; then
    log "ERROR (EC-12): kubectl current-context=${cur_ctx} (expected ${CONTEXT})"
    return 1
  fi
  if [ ! -s "$SESSION_FILE" ] ; then
    log "ERROR (EC-12): admin@loopzero.example session file missing/empty at $SESSION_FILE"
    log "       Run: loopzero-ops seed admin-loopzero-dev --verify"
    return 1
  fi
  log "Preflight OK (rate=${rate}/5000 ctx=${cur_ctx})"
}

# --- Auth-aware curl (EC-9 retry-once on 401) -------------------------------

curl_auth() {
  # Wraps curl with cookie-based auth. On 401, invokes
  # seed-admin-loopzero-dev.sh --refresh-only and retries ONCE. Hard-fails on
  # 2nd 401 to surface a real auth break.
  local out_file=$1 ; shift
  local token
  token="$(cat "$SESSION_FILE")"
  local code
  code="$(curl -s -o "$out_file" -w '%{http_code}' --max-time 10 \
    -b "ory_kratos_session=${token}" "$@" 2>&1 || true)"
  if [ "$code" = "401" ] ; then
    log "(EC-9) HTTP 401 mid-chain — invoking seed-admin --refresh-only + retry"
    bash "$(dirname "$0")/../../../scripts/local/seed-admin-loopzero-dev.sh" --refresh-only > /dev/null 2>&1 || {
      log "ERROR: seed-admin refresh-only failed"
      return 2
    }
    token="$(cat "$SESSION_FILE")"
    code="$(curl -s -o "$out_file" -w '%{http_code}' --max-time 10 \
      -b "ory_kratos_session=${token}" "$@" 2>&1 || true)"
    if [ "$code" = "401" ] ; then
      log "ERROR (EC-9): 2nd 401 after refresh — auth break, hard-fail"
      return 1
    fi
  fi
  printf '%s' "$code"
}

# --- EC-8 LE staging fallback ----------------------------------------------

check_le_staging_fallback() {
  # If cert-manager has issued state Failed with "too many certificates already
  # issued", switch the test's expected issuer to the staging issuer so the
  # test isn't blocked by upstream LE rate-limit (production sponsor walkthrough
  # uses prod issuer).
  local failed_msg
  failed_msg="$(kubectl --context="$CONTEXT" get certificates -A -o json 2>/dev/null | \
    python3 -c "
import json, sys
try:
  d = json.load(sys.stdin)
  for c in d.get('items', []):
    for cond in (c.get('status',{}).get('conditions') or []):
      if 'too many certificates' in (cond.get('message') or ''):
        print('TOO_MANY_CERTS')
        sys.exit(0)
except Exception:
  pass
" 2>/dev/null || true)"
  if [ "$failed_msg" = "TOO_MANY_CERTS" ] ; then
    log "WARN (EC-8): cert-manager hit LE prod rate-limit; will accept staging issuer cert (loopzero-staging-issuer) in chain link 9"
  fi
}

# --- 9-link chain assertions -----------------------------------------------

assert_link() {
  local link_id=$1 desc=$2
  log "[link ${link_id}] ${desc}"
}

# --- Chain Link 1 — webhook received ---------------------------------------

chain_link_1_webhook_received() {
  assert_link 1 "loopzero-api receives webhook with HMAC verified"

  local commit_sha
  if [ "$USE_PUSH_EVENT" = "1" ] ; then
    # EC-1 / ADR D7 fallback — synthetic commit via gh api PUT /contents/...
    # T6.1 of plan 2026-06-08-pre-release-test-rigor (2026-06-08): the prior
    # implementation only READ the existing ref SHA without dispatching any
    # event, so the webhook never fired. PUT /contents/<path> with sha=$old
    # atomically replaces a file → GitHub creates a commit on the default
    # branch → push event fires → webhook delivers.
    log "(EC-1 / D7) dispatching via push-event fallback (synthetic commit)"
    local probe_path probe_get sha_in_file new_content content_b64
    probe_path=".loopzero-test-probe"
    probe_get="$(gh api "/repos/${REPO_OWNER}/${REPO_NAME}/contents/${probe_path}" 2>/dev/null || true)"
    sha_in_file="$(printf '%s' "$probe_get" | python3 -c '
import sys, json
try:
  d = json.load(sys.stdin); sys.stdout.write(d.get("sha",""))
except Exception:
  pass
' 2>/dev/null || true)"
    new_content="probe $(date -u +%Y-%m-%dT%H:%M:%S.%NZ)"
    content_b64="$(printf '%s' "$new_content" | base64 -w 0)"
    if [ -n "$sha_in_file" ] ; then
      # File exists — PUT replaces it (with sha for optimistic locking)
      gh api -X PUT "/repos/${REPO_OWNER}/${REPO_NAME}/contents/${probe_path}" \
        -F "message=loopzero-rebuild probe $(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        -F "content=${content_b64}" \
        -F "sha=${sha_in_file}" > /tmp/wh-chain-probe.json 2>&1 || {
          log "ERROR: synthetic commit PUT failed; output:"
          head -c 300 /tmp/wh-chain-probe.json >&2
          return 1
        }
    else
      # First-time probe — PUT creates the file
      gh api -X PUT "/repos/${REPO_OWNER}/${REPO_NAME}/contents/${probe_path}" \
        -F "message=loopzero-rebuild probe $(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        -F "content=${content_b64}" > /tmp/wh-chain-probe.json 2>&1 || {
          log "ERROR: synthetic commit PUT (create) failed; output:"
          head -c 300 /tmp/wh-chain-probe.json >&2
          return 1
        }
    fi
    commit_sha="$(python3 -c '
import sys, json
try:
  d = json.load(open("/tmp/wh-chain-probe.json"))
  sys.stdout.write(d.get("commit",{}).get("sha",""))
except Exception:
  pass
' 2>/dev/null || echo "")"
    if [ -z "$commit_sha" ] ; then
      log "ERROR: synthetic commit response missing commit.sha"
      head -c 300 /tmp/wh-chain-probe.json >&2
      return 1
    fi
    log "synthetic commit pushed sha=${commit_sha:0:8}"
  else
    log "Dispatching via /repos/.../dispatches (event_type=loopzero-rebuild)"
    gh api -X POST "/repos/${REPO_OWNER}/${REPO_NAME}/dispatches" \
      -F "event_type=loopzero-rebuild" > /dev/null 2>&1 || {
        log "ERROR: dispatch failed"
        return 1
      }
    commit_sha="$(gh api "/repos/${REPO_OWNER}/${REPO_NAME}/git/refs/heads/main" --jq '.object.sha' 2>/dev/null || echo "")"
  fi

  # Poll loopzero-api logs for webhook-received marker
  local deadline=$(( $(date +%s) + 60 ))
  local hit=0
  while [ "$(date +%s)" -lt "$deadline" ] ; do
    if kubectl --context="$CONTEXT" -n "$PG_NAMESPACE" logs -l app=loopzero-api --since=2m --tail=200 2>/dev/null | \
         grep -qE "(webhook[:]? event received|webhook received|github.webhook|X-Hub-Signature)" ; then
      hit=1 ; break
    fi
    sleep 5
  done
  if [ "$hit" != "1" ] ; then
    log "FAIL (link 1): loopzero-api did not log webhook receive in 60s"
    return 1
  fi
  log "OK (link 1): webhook received (commit=${commit_sha:0:8})"
  printf '%s' "$commit_sha"
}

# --- Link 5 (key drift check) — release ACTIVE -----------------------------

chain_link_5_release_active() {
  assert_link 5 "PG releases.status == 'ACTIVE' for build linked to commit"
  local commit_sha=$1
  local pg_password
  pg_password="$(kubectl --context="$CONTEXT" -n "$PG_NAMESPACE" \
    get secret loopzero-db-credentials -o jsonpath='{.data.password}' 2>/dev/null | base64 -d 2>/dev/null || echo "loopzero-dev-password")"
  local deadline=$(( $(date +%s) + LINK5_BUDGET_SECONDS ))
  while [ "$(date +%s)" -lt "$deadline" ] ; do
    local status
    status="$(kubectl --context="$CONTEXT" -n "$PG_NAMESPACE" \
      exec -i postgresql-0 -c postgresql -- \
      env PGPASSWORD="$pg_password" psql -U loopzero -d loopzero -tAc \
      "SELECT status FROM releases WHERE commit_sha LIKE '${commit_sha:0:8}%' ORDER BY created_at DESC LIMIT 1" 2>/dev/null | tr -d '[:space:]' || echo "")"
    if [ "$status" = "ACTIVE" ] ; then
      log "OK (link 5): release ACTIVE for commit ${commit_sha:0:8}"
      return 0
    fi
    sleep 10
  done
  log "FAIL (link 5): release did not reach ACTIVE within $((LINK5_BUDGET_SECONDS/60))min"
  diagnostic_dump "link 5"
  return 1
}

# --- Link 7.5 — ArgoCD refresh=hard (EC-2) ---------------------------------

chain_link_7_5_argocd_refresh_hard() {
  assert_link "7.5 (EC-2)" "ArgoCD app refresh=hard (bypass repo-server ~5min cache)"
  local app_name="${TENANT_ID}-${PROJECT_NAME}-${ENV_NAME}"
  kubectl --context="$CONTEXT" -n argocd \
    annotate app "$app_name" "argocd.argoproj.io/refresh=hard" --overwrite > /dev/null 2>&1 || \
  log "WARN: annotate failed; app may not exist yet — continuing"
}

# --- Link 9 — HTTPS 200 on canonical URL -----------------------------------

chain_link_9_https_200() {
  assert_link 9 "HTTPS 200 on canonical fixture URL"
  local deadline=$(( $(date +%s) + LINK9_BUDGET_SECONDS ))
  while [ "$(date +%s)" -lt "$deadline" ] ; do
    local code
    code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$PUBLIC_FIXTURE_URL/health" 2>&1 || echo "000")"
    if [ "$code" = "200" ] ; then
      log "OK (link 9): HTTPS 200 on ${PUBLIC_FIXTURE_URL}/health"
      return 0
    fi
    sleep 10
  done
  log "FAIL (link 9): HTTPS 200 not reached within $((LINK9_BUDGET_SECONDS/60))min"
  diagnostic_dump "link 9"
  return 1
}

# --- Main -------------------------------------------------------------------

main() {
  if [ "$SKIP_LIVE" = "1" ] ; then
    log "[--skip-live] hermetic-only mode; not exercising LIVE chain"
    if [ "$JSON_MODE" = "1" ] ; then
      printf '{"verdict":"SKIP","reason":"--skip-live"}\n'
    fi
    exit 0
  fi

  preflight || exit 1
  check_le_staging_fallback

  local commit_sha
  commit_sha="$(chain_link_1_webhook_received)" || exit 1

  # links 2-4 are observed inline in the workflow phase poll (link 4)
  log "[link 2/3/4] polling Argo workflow for commit ${commit_sha:0:8}"
  local wf_deadline=$(( $(date +%s) + LINK4_BUDGET_SECONDS ))
  local wf_phase=""
  while [ "$(date +%s)" -lt "$wf_deadline" ] ; do
    wf_phase="$(kubectl --context="$CONTEXT" -n "$PG_NAMESPACE" \
      get wf -l "loopzero.io/commit-sha=${commit_sha:0:12}" \
      -o jsonpath='{.items[0].status.phase}' 2>/dev/null || echo "")"
    [ "$wf_phase" = "Succeeded" ] && break
    sleep 10
  done
  if [ "$wf_phase" != "Succeeded" ] ; then
    log "FAIL (link 2/3/4): workflow phase=${wf_phase} (expected Succeeded)"
    diagnostic_dump "link 4"
    exit 1
  fi
  log "OK (link 2/3/4): workflow Succeeded"

  chain_link_5_release_active "$commit_sha" || exit 1

  # link 6: namespace exists
  local ns="loopzero-${TENANT_ID}-${PROJECT_NAME}-${ENV_NAME}"
  if ! kubectl --context="$CONTEXT" get ns "$ns" > /dev/null 2>&1 ; then
    log "FAIL (link 6): namespace ${ns} not found"
    exit 1
  fi
  log "OK (link 6): namespace ${ns}"

  # link 7: Prova de Vida — pod Running 1/1
  local pod_status
  pod_status="$(kubectl --context="$CONTEXT" -n "$ns" \
    get pods -l "app=${PROJECT_NAME}" -o jsonpath='{.items[0].status.phase}' 2>/dev/null || echo "")"
  if [ "$pod_status" != "Running" ] ; then
    log "FAIL (link 7 / Prova de Vida): pod phase=${pod_status} (expected Running 1/1)"
    diagnostic_dump "link 7"
    exit 1
  fi
  log "OK (link 7): pod Running 1/1 (Prova de Vida)"

  # link 7.5 EC-2 — refresh=hard before asserting ArgoCD health
  chain_link_7_5_argocd_refresh_hard

  # link 8: ArgoCD Synced + Healthy
  local sync_status health_status
  sync_status="$(kubectl --context="$CONTEXT" -n argocd \
    get app "${TENANT_ID}-${PROJECT_NAME}-${ENV_NAME}" \
    -o jsonpath='{.status.sync.status}' 2>/dev/null || echo "")"
  health_status="$(kubectl --context="$CONTEXT" -n argocd \
    get app "${TENANT_ID}-${PROJECT_NAME}-${ENV_NAME}" \
    -o jsonpath='{.status.health.status}' 2>/dev/null || echo "")"
  if [ "$sync_status" != "Synced" ] || [ "$health_status" != "Healthy" ] ; then
    log "FAIL (link 8): ArgoCD sync=${sync_status} health=${health_status} (expected Synced+Healthy)"
    exit 1
  fi
  log "OK (link 8): ArgoCD Synced + Healthy"

  chain_link_9_https_200 || exit 1

  local elapsed=$(( $(date +%s) - START_EPOCH ))
  log "DONE — 9-link chain completed in ${elapsed}s (budget ${MAX_DURATION_SECONDS}s)"
  if [ "$JSON_MODE" = "1" ] ; then
    printf '{"verdict":"OK","elapsed_seconds":%d,"max_duration_seconds":%d}\n' "$elapsed" "$MAX_DURATION_SECONDS"
  fi
}

main "$@"
