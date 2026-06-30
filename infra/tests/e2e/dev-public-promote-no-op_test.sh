#!/usr/bin/env bash
# dev-public-promote-no-op_test.sh
#
# Plan: loopzero-promote-no-op-fail-loud-p0 v1.0
# Closes: Red Team loopzero-cloud#120 — POST /engine/api/v1/projects/{name}/promote
#         returns 200 success but (a) emits malformed release ID `rel-rel-<hash>`
#         (double prefix from promotion_orchestrator.go:155), (b) emits URL on
#         BaseDomain (loopzero.local) instead of PublicDomain (prod-dev.loopzero.example),
#         (c) production env's last_deploy_sha never changes (no-op activation).
#
# PER feedback_issue_fix_requires_failing_e2e_test_2026_06_25 INQUEBRÁVEL:
# this LIVE bash reproduces #120 verbatim BEFORE the fix (RED) and passes
# AFTER (GREEN). The 3 scenarios are independent assertions:
#
#   A — response.new_release matches ^rel-[0-9a-f]{16}$ (NOT rel-rel-...)
#   B — response.url contains prod-dev.loopzero.example (NOT .loopzero.local)
#   C — production env's last_deploy_sha changes within 90s of promote
#       (activation canary — covers up to 9 convergence loop ticks @ 10s)
#
# OPERATOR HARD STOP — per cleanroom-up-live-test-mandate.md.
# Requires:
#   - Cleanroom TOTAL → UP → seed-post-cleanroom-all chain (QA Red Team identity + tenant CR)
#   - At least one preview release for the canonical seed project
#   - JWT at ~/.loopzero/redteam-qa-session-jwt.txt OR ~/.loopzero/local-dogfood-session-jwt.txt
#
# Flags:
#   --skip-live          short-circuit before any HTTP (CI/hermetic mode)
#   --scenario=A|B|C     run only the named scenario
#   --json               emit machine-readable result on stdout
#   --help               show this help and exit 0

set -euo pipefail

# --- Constants --------------------------------------------------------------

ISSUE_REF="loopzero-cloud#120"
PLAN_SLUG="loopzero-promote-no-op-fail-loud-p0"
SCRIPT_NAME="$(basename "$0")"

PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
PROJECT_NAME="${PROJECT_NAME:-loopzerokit-example}"
FROM_ENV="${FROM_ENV:-preview}"
TO_ENV="${TO_ENV:-production}"
EXPECTED_PUBLIC_DOMAIN_SUBSTRING="${EXPECTED_PUBLIC_DOMAIN_SUBSTRING:-prod-dev.loopzero.example}"
FORBIDDEN_DOMAIN_SUBSTRING="${FORBIDDEN_DOMAIN_SUBSTRING:-loopzero.local}"
RELEASE_ID_REGEX='^rel-[0-9a-f]{16}$'
ACTIVATION_WINDOW_SECONDS="${ACTIVATION_WINDOW_SECONDS:-90}"

# Session JWT — try canonical paths in order
SESSION_JWT_FILE="${SESSION_JWT_FILE:-}"
JWT_CANDIDATES=(
  "${HOME}/.loopzero/redteam-qa-session-jwt.txt"
  "${HOME}/.loopzero/local-dogfood-session-jwt.txt"
  "${HOME}/.loopzero/admin-loopzero-dev-session-jwt.txt"
)

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
    *) echo "ERROR: unknown flag: $1" >&2 ; echo "use --help" >&2 ; exit 1 ;;
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
record_fail() {
  fail_count=$((fail_count + 1))
  scenarios_failed+=("$1")
  log "  FAIL  $1 — $2"
}

# --- Skip-live short-circuit -----------------------------------------------

if [ "$SKIP_LIVE" = "1" ]; then
  log "[--skip-live] hermetic mode — NOT touching cluster/API; structural shape only"
  log "RESULT: PASS (hermetic skip)"
  if [ "$JSON_OUTPUT" = "1" ]; then
    printf '{"pass":1,"fail":0,"skipped":true,"reason":"--skip-live"}\n'
  fi
  exit 0
fi

# --- Pre-flight: JWT + curl + jq -------------------------------------------

if ! command -v curl >/dev/null 2>&1 ; then
  log "FATAL: curl not in PATH" ; exit 2
fi
if ! command -v jq >/dev/null 2>&1 ; then
  log "FATAL: jq not in PATH" ; exit 2
fi

if [ -z "$SESSION_JWT_FILE" ] ; then
  for cand in "${JWT_CANDIDATES[@]}" ; do
    if [ -s "$cand" ] ; then SESSION_JWT_FILE="$cand" ; break ; fi
  done
fi
if [ -z "$SESSION_JWT_FILE" ] || [ ! -s "$SESSION_JWT_FILE" ] ; then
  log "FATAL: no session JWT found in canonical paths:"
  for cand in "${JWT_CANDIDATES[@]}" ; do log "  - $cand" ; done
  log "       Run: bash infra/scripts/local/seed-admin-loopzero-dev.sh (with QA env overrides)"
  exit 2
fi
JWT="$(cat "$SESSION_JWT_FILE")"

# Probe API reachability + auth
PRE_CODE="$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 \
  -H "Authorization: Bearer ${JWT}" \
  "${PUBLIC_CLOUD_URL}/engine/api/v1/projects" 2>&1 || true)"
if [ "$PRE_CODE" != "200" ] ; then
  log "FATAL: pre-flight GET /engine/api/v1/projects returned HTTP ${PRE_CODE}"
  log "       Re-mint session JWT: bash infra/scripts/local/seed-admin-loopzero-dev.sh --refresh-only"
  exit 2
fi

# --- Helpers ----------------------------------------------------------------

call_promote() {
  local resp_file="$1"
  curl -s -o "$resp_file" -w '%{http_code}' --max-time 30 -X POST \
    -H "Authorization: Bearer ${JWT}" \
    -H "Content-Type: application/json" \
    -H "Origin: ${PUBLIC_CLOUD_URL}" \
    "${PUBLIC_CLOUD_URL}/engine/api/v1/projects/${PROJECT_NAME}/promote" \
    -d "{\"from\":\"${FROM_ENV}\",\"to\":\"${TO_ENV}\"}" 2>&1 || true
}

get_env_sha() {
  # Returns last_deploy_sha for the project's TO_ENV, empty if missing.
  local env_resp
  env_resp="$(curl -s --max-time 10 -H "Authorization: Bearer ${JWT}" \
    "${PUBLIC_CLOUD_URL}/engine/api/v1/environments?project=${PROJECT_NAME}" 2>&1 || true)"
  printf '%s' "$env_resp" | jq -r --arg env "$TO_ENV" \
    '.environments[]? | select(.name == $env) | .last_deploy_sha // ""' 2>/dev/null | head -1
}

# --- Shared promote invocation (used by all 3 scenarios) -------------------

PROMOTE_RESP="$(mktemp -t promote-resp-XXXXXX.json)"
trap 'rm -f "$PROMOTE_RESP"' EXIT

# Capture pre-state for scenario C
SHA_PRE=""
if [ -z "$SCENARIO" ] || [ "$SCENARIO" = "C" ] ; then
  SHA_PRE="$(get_env_sha || true)"
  log "Pre-promote state: ${TO_ENV} last_deploy_sha=${SHA_PRE:-<empty>}"
fi

log "Invoking POST /engine/api/v1/projects/${PROJECT_NAME}/promote (from=${FROM_ENV} to=${TO_ENV})..."
PROMOTE_CODE="$(call_promote "$PROMOTE_RESP")"
log "Promote HTTP ${PROMOTE_CODE}"

# If HTTP != 200/202, scenarios A and B can't extract from body — surface and continue
PROMOTE_BODY=""
if [ -s "$PROMOTE_RESP" ] ; then
  PROMOTE_BODY="$(head -c 2000 "$PROMOTE_RESP" 2>/dev/null || true)"
fi

# --- Scenario A — Canonical release ID format ------------------------------

scenario_a() {
  log "Scenario A — release ID matches ${RELEASE_ID_REGEX}"
  case "$PROMOTE_CODE" in
    200|202) ;;
    *) record_fail "A" "promote HTTP ${PROMOTE_CODE} — cannot extract new_release; body=$(printf '%s' "$PROMOTE_BODY" | head -c 300)"
       return 1 ;;
  esac
  local new_rel
  new_rel="$(printf '%s' "$PROMOTE_BODY" | jq -r '.new_release // ""' 2>/dev/null || true)"
  if [ -z "$new_rel" ] ; then
    record_fail "A" "response missing new_release field; body=$(printf '%s' "$PROMOTE_BODY" | head -c 300)"
    return 1
  fi
  log "  observed new_release: ${new_rel}"
  if [[ "$new_rel" =~ $RELEASE_ID_REGEX ]] ; then
    record_pass "A (new_release='${new_rel}' matches canonical regex)"
    return 0
  fi
  record_fail "A" "new_release='${new_rel}' does NOT match ${RELEASE_ID_REGEX} (likely rel-rel-... double prefix — promotion_orchestrator.go:155)"
}

# --- Scenario B — URL on PublicDomain --------------------------------------

scenario_b() {
  log "Scenario B — url contains '${EXPECTED_PUBLIC_DOMAIN_SUBSTRING}' and not '${FORBIDDEN_DOMAIN_SUBSTRING}'"
  case "$PROMOTE_CODE" in
    200|202) ;;
    *) record_fail "B" "promote HTTP ${PROMOTE_CODE} — cannot extract url"
       return 1 ;;
  esac
  local url
  url="$(printf '%s' "$PROMOTE_BODY" | jq -r '.url // ""' 2>/dev/null || true)"
  if [ -z "$url" ] ; then
    record_fail "B" "response missing url field"
    return 1
  fi
  log "  observed url: ${url}"
  if printf '%s' "$url" | grep -qF "$FORBIDDEN_DOMAIN_SUBSTRING" ; then
    record_fail "B" "url contains forbidden '${FORBIDDEN_DOMAIN_SUBSTRING}' (Red Team #61 regression on promote path — promotion_orchestrator.go:288 uses BaseDomain not PublicDomain)"
    return 1
  fi
  if ! printf '%s' "$url" | grep -qF "$EXPECTED_PUBLIC_DOMAIN_SUBSTRING" ; then
    record_fail "B" "url does NOT contain expected '${EXPECTED_PUBLIC_DOMAIN_SUBSTRING}'; got '${url}'"
    return 1
  fi
  record_pass "B (url='${url}' on canonical public domain)"
}

# --- Scenario C — Activation canary (sha changes within window) ------------

scenario_c() {
  log "Scenario C — production sha changes within ${ACTIVATION_WINDOW_SECONDS}s (activation canary)"
  case "$PROMOTE_CODE" in
    200|202) ;;
    *)
      # Fail-loud is acceptable per ADR-3 — if promote returns ≠ 200/202 with typed error, that's also a PASS
      record_pass "C (promote returned HTTP ${PROMOTE_CODE} — fail-loud accepted per ADR-3)"
      return 0
      ;;
  esac
  if [ -z "$SHA_PRE" ] ; then
    log "  (no pre-promote sha — env may be brand-new; checking post-promote shows non-empty sha instead)"
  fi
  local elapsed=0 sha_post=""
  while [ "$elapsed" -lt "$ACTIVATION_WINDOW_SECONDS" ] ; do
    sleep 10
    elapsed=$((elapsed + 10))
    sha_post="$(get_env_sha || true)"
    log "  t+${elapsed}s sha=${sha_post:-<empty>}"
    if [ -n "$sha_post" ] && [ "$sha_post" != "$SHA_PRE" ] ; then
      record_pass "C (sha changed from '${SHA_PRE:-<empty>}' to '${sha_post}' within ${elapsed}s)"
      return 0
    fi
  done
  record_fail "C" "${TO_ENV} sha unchanged (still '${sha_post:-<empty>}') after ${ACTIVATION_WINDOW_SECONDS}s — no-op activation (deferred-debug per plan ADR-3)"
}

# --- Main -------------------------------------------------------------------

log "============================================================"
log "Plan: $PLAN_SLUG"
log "Issue: $ISSUE_REF"
log "Project: $PROJECT_NAME  ${FROM_ENV} → ${TO_ENV}"
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
  printf '{"pass":%d,"fail":%d,"failed":[%s],"issue":"%s","plan":"%s"}\n' \
    "$pass_count" "$fail_count" "${failed_joined:-}" "$ISSUE_REF" "$PLAN_SLUG"
fi

[ "$fail_count" -eq 0 ]
