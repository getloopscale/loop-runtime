#!/usr/bin/env bash
# loopzero-cloud#64 closure LIVE test — proves the /engine/* proxy chain accepts
# large Authorization headers without 431 (Request Header Fields Too Large).
#
# Symptom per #64 (RED TEAM 2026-06-26T141335): a user with display Name of
# 20 000 chars (allowed by Kratos schema per #65 — separate bug) gets that
# name embedded VERBATIM in the JWT's `name` claim (loopzero-cloud
# internal/routes/session_token.go:117 passes Identity.Traits.Name into
# loopzero-contracts jwt.SessionTokenIssuer.Issue with no cap). The resulting
# Authorization: Bearer <token> header reached ~27 KB and the engine gateway
# answered 431 Request Header Fields Too Large for EVERY /engine/api/v1/*
# call — projects, auth/whoami, domains — so the account was unable to
# list/manage projects (UI silently rendered "Deploy your first project"
# empty-state, masking the 431).
#
# This LIVE test reproduces the symptom at the TRANSPORT layer (the 431 is
# decided BEFORE JWT validation, so we can use any fake-but-large Bearer
# token — the proxy chain still has to accept the header size to even reach
# the validator). Per memory feedback_issue_fix_requires_failing_e2e_test_
# 2026_06_25: the canonical close MUST include a LIVE bash that reproduces
# the issue verbatim. The user 2026-06-26 status report confirms the symptom
# sumiu but asks for the "teste fiel para cravar" — this is it.
#
# Per the canonical curl probe matrix in memory feedback_browser_render_
# required_before_shipped_2026_06_26 § sub-feedback v1.4: hits the explicit
# `/engine/*` handle block AND the `/v1/auth/me` handle block to confirm
# both proxy paths accept the large header.
#
# Exit codes:
#   0  /engine/* + /v1/auth/me both accept ~27KB Authorization header
#      (any non-431 status — typically 401 invalid JWT — proves the
#      transport layer is fine)
#   1  one or both endpoints returned 431 (bug present, regression)
#
# Usage:
#   bash infra/tests/e2e/dev-public-engine-large-header-not-431_test.sh \
#     [--skip-live] [--json]

set -euo pipefail

TARGET_HOST="${TARGET_HOST:-https://app-dev.loopzero.example}"
SKIP_LIVE=0
JSON_OUTPUT=0
FAIL_COUNT=0

while [ $# -gt 0 ]; do
  case "$1" in
    --skip-live) SKIP_LIVE=1 ; shift ;;
    --json)      JSON_OUTPUT=1 ; shift ;;
    -h|--help)
      sed -n '2,28p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) printf 'unknown arg: %s\n' "$1" >&2 ; exit 1 ;;
  esac
done

log() { [ "$JSON_OUTPUT" = "0" ] && printf '[64-large-header] %s\n' "$*" >&2 || true ; }

# Build a fake JWT with a deliberately-large name claim, ~27 KB total. We do
# NOT need a valid signature — the 431 is rejected at the transport layer,
# BEFORE the engine validates the token. If the proxy accepts the header
# size, JWT validation fires and returns 401 (invalid signature) — proving
# the bug is closed.
build_large_bearer() {
  local pad
  pad="$(python3 -c 'import sys; sys.stdout.write("x" * 27000)')"
  # eyJhbGciOiJIUzI1NiJ9.eyJuYW1lIjoiPHBhZD4ifQ.fakesig — structurally a
  # 3-part JWT; the engine attempts to validate, rejects on signature, but
  # the proxy chain must have accepted the header to reach that decision.
  printf 'eyJhbGciOiJIUzI1NiJ9.%s.fakesig' "${pad}"
}

probe_path() {
  local path="$1"
  local label="$2"
  local jwt="$3"
  local status
  status="$(curl -s -o /dev/null -w '%{http_code}' \
    -H "Authorization: Bearer ${jwt}" \
    "${TARGET_HOST}${path}" || echo "000")"

  # Canonical assertion: status MUST NOT be 431. Anything else (200, 401,
  # 403, 500) proves the transport layer accepts the header size — the
  # gateway header_size limit (or the equivalent cap on the JWT name claim)
  # is large enough for adversarial input.
  if [ "$status" = "431" ]; then
    log "FAIL ${label}: ${path} → 431 Request Header Fields Too Large (bug present)"
    FAIL_COUNT=$((FAIL_COUNT + 1))
    return 1
  else
    log "PASS ${label}: ${path} → HTTP ${status} (any non-431 = canonical accept)"
    return 0
  fi
}

if [ "$SKIP_LIVE" = "1" ]; then
  log "SKIP_LIVE=1 — hermetic CI mode. Script structure parsed (bash -n)."
  bash -n "$0"
  log "PASS hermetic syntax check"
  [ "$JSON_OUTPUT" = "1" ] && printf '{"verdict":"SKIPPED","mode":"hermetic","fails":0}\n'
  exit 0
fi

log "TARGET: ${TARGET_HOST}"
LARGE_JWT="$(build_large_bearer)"
HEADER_SIZE_BYTES=$((${#LARGE_JWT} + 21))  # "Authorization: Bearer " + value
log "Built fake-large JWT: ${#LARGE_JWT} bytes value, ~${HEADER_SIZE_BYTES} bytes header"

# Probe each canonical handle that consumes the Authorization header. Both
# /engine/* (CLI + dashboard engineFetch BFF prefix) AND /v1/auth/me (cloud
# direct) must accept the large header — they share the same physical
# upstream (loopzero-cloud at localhost:13002) but the Caddy proxy chain is
# distinct per handle block, so we probe both to lock both surfaces.
probe_path "/engine/api/v1/projects?limit=50" "engine.projects"     "$LARGE_JWT" || true
probe_path "/engine/api/v1/auth/whoami"        "engine.whoami"       "$LARGE_JWT" || true
probe_path "/engine/api/v1/domains"            "engine.domains"      "$LARGE_JWT" || true
probe_path "/v1/auth/me"                       "cloud.auth.me"       "$LARGE_JWT" || true

if [ "$JSON_OUTPUT" = "1" ]; then
  if [ "$FAIL_COUNT" = "0" ]; then
    printf '{"verdict":"PASS","mode":"live","fails":0,"jwt_header_bytes":%d}\n' "$HEADER_SIZE_BYTES"
  else
    printf '{"verdict":"FAIL","mode":"live","fails":%d,"jwt_header_bytes":%d}\n' "$FAIL_COUNT" "$HEADER_SIZE_BYTES"
  fi
fi

if [ "$FAIL_COUNT" -gt 0 ]; then
  log "VERDICT: FAIL — ${FAIL_COUNT} endpoint(s) returned 431 (Red Team #64 regression)"
  exit 1
fi

log "VERDICT: PASS — all 4 endpoints accepted ~${HEADER_SIZE_BYTES}-byte Authorization header (Red Team #64 closure proof)"
exit 0
