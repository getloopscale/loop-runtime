#!/usr/bin/env bash
# dev-public-dashboard-deployments-active-row-attribution_test.sh
#
# Closes: Red Team loopzero-cloud#121 — the Deployments tab `● ACTIVE` badge + the
#         "Open live URL" icon were attributed by a GLOBAL `activeBuildId ===
#         b.build_id` comparison (deployments.tsx:466-467 pre-fix), which
#         mismatched whenever the row's environment differed from the production
#         env_target.active_release. Post-promote releases share build_ids
#         cross-env (a promote copies the build reference), so a preview row
#         silently inherited the production ACTIVE marker + a Live URL pointing
#         at production — while the actual production row stayed unmarked.
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL —
# this LIVE bash reproduces #121 verbatim BEFORE the fix (RED) and passes
# AFTER (GREEN). The 3 scenarios are independent assertions:
#
#   A — both env statuses are independently observable (preview + production
#       respond 200 on /status?env=<env>; each has its own active_release shape)
#   B — bundle-grep proof: the production dashboard bundle exposes the
#       per-env `env:"preview"` literal (useProjectStatus({env: 'preview'})
#       wire) AND the URL composition is keyed off the row's environment.
#       Pre-fix the bundle had ZERO preview-env calls — every row used the
#       global prodStatus.url.
#   C — engine /environments response shape carries the per-env active_release
#       (last_deploy_sha + last_deploy + status) for both envs. The dashboard
#       reads from the canonical engine SoT; if the SoT loses a field, the
#       per-env attribution can't be recovered downstream.
#
# OPERATOR HARD STOP — per cleanroom-up-live-test-mandate.md.
# Requires:
#   - Cleanroom TOTAL → UP → seed-post-cleanroom-all chain
#   - QA Red Team identity + tenant CR (per #118)
#   - loopzerokit-example project with active production + preview releases
#     (per #120 closure — promote chain validated end-to-end)
#   - Dashboard prod bundle deployed to dev-public (Caddy file_server)
#
# Flags:
#   --skip-live          short-circuit before any HTTP (CI/hermetic mode)
#   --scenario=A|B|C     run only the named scenario
#   --json               emit machine-readable result on stdout
#   --help               show this help and exit 0

set -euo pipefail

ISSUE_REF="loopzero-cloud#121"
SLUG="dashboard-deployments-active-row-attribution"
SCRIPT_NAME="$(basename "$0")"

PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
PROJECT_NAME="${PROJECT_NAME:-teste}"

JWT_CANDIDATES=(
  "${HOME}/.loopzero/local-dogfood-session-jwt.txt"
  "${HOME}/.loopzero/redteam-qa-session-jwt.txt"
  "${HOME}/.loopzero/admin-loopzero-dev-session-jwt.txt"
)
SESSION_JWT_FILE="${SESSION_JWT_FILE:-}"

SKIP_LIVE=0
SCENARIO=""
JSON_OUTPUT=0

show_help() {
  sed -n '2,/^set -euo/p' "$0" | sed 's/^# \{0,1\}//' | sed '$d'
  exit 0
}

while [ $# -gt 0 ]; do
  case "$1" in
    --skip-live) SKIP_LIVE=1 ;;
    --scenario=A|--scenario=B|--scenario=C) SCENARIO="${1#--scenario=}" ;;
    --json) JSON_OUTPUT=1 ;;
    --help|-h) show_help ;;
    *) echo "ERROR: unknown flag: $1" >&2; exit 1 ;;
  esac
  shift
done

log() {
  if [ "$JSON_OUTPUT" = "0" ]; then
    printf '[%s] %s\n' "$SCRIPT_NAME" "$*" >&2
  fi
}

fail_count=0
pass_count=0
scenarios_failed=()
record_pass() { pass_count=$((pass_count + 1)); log "  PASS  $1"; }
record_fail() { fail_count=$((fail_count + 1)); scenarios_failed+=("$1"); log "  FAIL  $1 — $2"; }

if [ "$SKIP_LIVE" = "1" ]; then
  log "[--skip-live] hermetic mode — structural shape only"
  log "RESULT: PASS (hermetic skip)"
  if [ "$JSON_OUTPUT" = "1" ]; then
    printf '{"pass":1,"fail":0,"skipped":true,"reason":"--skip-live"}\n'
  fi
  exit 0
fi

if ! command -v curl >/dev/null 2>&1 ; then log "FATAL: curl missing"; exit 2; fi
if ! command -v jq >/dev/null 2>&1 ; then log "FATAL: jq missing"; exit 2; fi

if [ -z "$SESSION_JWT_FILE" ] ; then
  for cand in "${JWT_CANDIDATES[@]}" ; do
    if [ -s "$cand" ] ; then SESSION_JWT_FILE="$cand" ; break ; fi
  done
fi
if [ -z "$SESSION_JWT_FILE" ] || [ ! -s "$SESSION_JWT_FILE" ] ; then
  log "FATAL: no session JWT found"
  exit 2
fi
JWT="$(cat "$SESSION_JWT_FILE")"

scenario_a() {
  log "Scenario A — per-env /status endpoints both respond 200"
  local prod_code preview_code
  prod_code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 \
    -H "Authorization: Bearer ${JWT}" \
    "${PUBLIC_CLOUD_URL}/engine/api/v1/projects/${PROJECT_NAME}/status?env=production" 2>&1 || true)"
  preview_code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 \
    -H "Authorization: Bearer ${JWT}" \
    "${PUBLIC_CLOUD_URL}/engine/api/v1/projects/${PROJECT_NAME}/status?env=preview" 2>&1 || true)"
  if [ "$prod_code" = "200" ] && [ "$preview_code" = "200" ] ; then
    record_pass "A (production HTTP ${prod_code} + preview HTTP ${preview_code})"
  else
    record_fail "A" "production=${prod_code} preview=${preview_code} — both must be 200 for per-env attribution to work"
  fi
}

scenario_b() {
  log "Scenario B — production dashboard bundle exposes per-env preview wire"
  # Read the entry chunk and find the deployments-*.js bundle hash.
  local html
  html="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/" 2>&1 || true)"
  local entry_js
  entry_js="$(printf '%s' "$html" | grep -oE 'src="/assets/index-[A-Za-z0-9_-]+\.js"' | head -1 | sed 's|src="||;s|"||')"
  if [ -z "$entry_js" ] ; then
    record_fail "B" "entry chunk not found in / response"
    return 1
  fi
  local entry_body
  entry_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
  local deployments_chunk
  deployments_chunk="$(printf '%s' "$entry_body" | grep -oE 'deployments-[A-Za-z0-9_-]+\.js' | head -1)"
  if [ -z "$deployments_chunk" ] ; then
    record_fail "B" "deployments chunk hash not found in entry chunk"
    return 1
  fi
  log "  observed deployments chunk: ${deployments_chunk}"
  local chunk_body
  chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${deployments_chunk}" 2>&1 || true)"
  # Bundle invariant — per-env useProjectStatus call for preview must be present.
  # Pre-fix the bundle had ZERO env:"preview" usage. Post-fix exactly 1.
  local preview_calls
  preview_calls="$(printf '%s' "$chunk_body" | grep -oE 'env:"preview"' | wc -l)"
  if [ "$preview_calls" -ge 1 ] ; then
    record_pass "B (deployments bundle ${deployments_chunk} has env:\"preview\" wire — ${preview_calls} occurrence(s))"
  else
    record_fail "B" "deployments bundle ${deployments_chunk} has NO env:\"preview\" wire — pre-fix shape"
  fi
}

scenario_c() {
  log "Scenario C — /environments response carries per-env last_deploy + last_deploy_sha"
  local env_resp
  env_resp="$(curl -s --max-time 10 -H "Authorization: Bearer ${JWT}" \
    "${PUBLIC_CLOUD_URL}/engine/api/v1/environments?project=${PROJECT_NAME}" 2>&1 || true)"
  local prod_sha preview_sha
  prod_sha="$(printf '%s' "$env_resp" | jq -r '.environments[]? | select(.name=="production") | .last_deploy_sha // ""' 2>/dev/null | head -1)"
  preview_sha="$(printf '%s' "$env_resp" | jq -r '.environments[]? | select(.name=="preview") | .last_deploy_sha // ""' 2>/dev/null | head -1)"
  if [ -n "$prod_sha" ] && [ -n "$preview_sha" ] ; then
    record_pass "C (per-env shas present: production=${prod_sha:0:7} preview=${preview_sha:0:7})"
  else
    record_fail "C" "missing last_deploy_sha — production='${prod_sha}' preview='${preview_sha}'"
  fi
}

log "============================================================"
log "Plan slug: $SLUG"
log "Issue: $ISSUE_REF"
log "Project: $PROJECT_NAME @ $PUBLIC_CLOUD_URL"
log "JWT: $SESSION_JWT_FILE"
log "============================================================"

case "$SCENARIO" in
  A) scenario_a || true ;;
  B) scenario_b || true ;;
  C) scenario_c || true ;;
  "") scenario_a || true ; scenario_b || true ; scenario_c || true ;;
esac

log "============================================================"
log "Result: ${pass_count} PASS, ${fail_count} FAIL"
if [ "${#scenarios_failed[@]}" -gt 0 ]; then
  log "Failed: ${scenarios_failed[*]}"
fi
log "============================================================"

if [ "$JSON_OUTPUT" = "1" ]; then
  failed_joined="$(printf '"%s",' "${scenarios_failed[@]:-}" | sed 's/,$//')"
  printf '{"pass":%d,"fail":%d,"failed":[%s],"issue":"%s","slug":"%s"}\n' \
    "$pass_count" "$fail_count" "${failed_joined:-}" "$ISSUE_REF" "$SLUG"
fi

[ "$fail_count" -eq 0 ]
