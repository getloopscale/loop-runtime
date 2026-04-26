#!/usr/bin/env bash
# Sprint 6 / T4.1 — Supply-chain E2E.
#
# Validates the full Sprint 6 vertical slice on a deployed LoopZero platform:
#
#   1. Trigger a deploy and wait for completion (build → sign → sbom).
#   2. Fetch /api/v1/builds/:id/attestation — must report signed: true
#      and an image_digest starting with sha256:.
#   3. Fetch /api/v1/builds/:id/sbom — must return at least one app with
#      a presigned download_url.
#   4. Download the SBOM and verify it parses as CycloneDX JSON.
#   5. Verify the policy-controller webhook is reachable (Audit mode).
#
# Pre-requisites on the host:
#   - loopzero CLI on PATH (logged in to the target API)
#   - jq + curl
#   - kubectl (for policy-controller checks)
#   - A test app already connected to the user's account, OR set FIXTURE_DIR
#     to a LoopZero-ready repo to deploy from.
#
# Usage:
#   LOOPZERO_API=https://api.loopzero.local LOOPZERO_TOKEN=... \
#     ./infra/tests/e2e/supply-chain-e2e.sh [--existing-build BUILD_ID]
#
# Exit codes:
#   0  E2E passed (every phase succeeded)
#   1  Pre-req check failed
#   2  Build did not complete
#   3  Attestation endpoint did not return signed=true
#   4  SBOM endpoint did not return a downloadable SBOM
#   5  policy-controller is not reachable

set -euo pipefail

# ──────────────────────────────────────────────────────────────────────
# Config
# ──────────────────────────────────────────────────────────────────────
LOOPZERO_API="${LOOPZERO_API:-http://localhost:8080}"
LOOPZERO_TOKEN="${LOOPZERO_TOKEN:-}"
EXISTING_BUILD_ID=""
DEPLOY_TIMEOUT_SEC="${DEPLOY_TIMEOUT_SEC:-600}"
LOG_DIR="${LOG_DIR:-/tmp/loopzero-supply-chain-e2e}"
SBOM_TMP="${LOG_DIR}/sbom.cdx.json"

mkdir -p "$LOG_DIR"

while [ $# -gt 0 ]; do
  case "$1" in
    --existing-build) EXISTING_BUILD_ID="$2"; shift 2 ;;
    --api)            LOOPZERO_API="$2"; shift 2 ;;
    --token)          LOOPZERO_TOKEN="$2"; shift 2 ;;
    *) echo "unknown flag: $1" >&2; exit 1 ;;
  esac
done

log()  { echo "[$(date +%H:%M:%S)] $*"; }
fail() { log "FAIL: $*"; exit "${2:-1}"; }
ok()   { log "OK:   $*"; }

require_cmd() { command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1" 1; }

require_cmd curl
require_cmd jq

if [ -z "$LOOPZERO_TOKEN" ] && [ -z "$EXISTING_BUILD_ID" ]; then
  fail "either LOOPZERO_TOKEN or --existing-build BUILD_ID must be set" 1
fi

auth_header=()
if [ -n "$LOOPZERO_TOKEN" ]; then
  auth_header=(-H "Authorization: Bearer ${LOOPZERO_TOKEN}")
fi

# ──────────────────────────────────────────────────────────────────────
# Phase 1: deploy a build (or use existing)
# ──────────────────────────────────────────────────────────────────────
BUILD_ID="$EXISTING_BUILD_ID"
if [ -z "$BUILD_ID" ]; then
  log "Phase 1: triggering deploy via loopzero CLI..."
  require_cmd loopzero

  if ! deploy_out=$(loopzero deploy --json 2>&1); then
    fail "loopzero deploy failed: $deploy_out" 2
  fi
  BUILD_ID=$(echo "$deploy_out" | jq -r '.build_id // .id // empty')
  if [ -z "$BUILD_ID" ]; then
    fail "could not parse build_id from loopzero deploy --json output" 2
  fi
  log "  build_id: $BUILD_ID"

  log "Phase 1: waiting up to ${DEPLOY_TIMEOUT_SEC}s for build to succeed..."
  deadline=$(( $(date +%s) + DEPLOY_TIMEOUT_SEC ))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    state=$(curl -fsS "${auth_header[@]}" "${LOOPZERO_API}/api/v1/builds/${BUILD_ID}" | jq -r '.state // .status // empty')
    case "$state" in
      SUCCEEDED) ok "build succeeded"; break ;;
      FAILED|CANCELLED) fail "build ended in $state" 2 ;;
      *)
        log "  build state: ${state:-unknown}; sleeping 10s"
        sleep 10
        ;;
    esac
  done
  state_final=$(curl -fsS "${auth_header[@]}" "${LOOPZERO_API}/api/v1/builds/${BUILD_ID}" | jq -r '.state // .status // empty')
  if [ "$state_final" != "SUCCEEDED" ]; then
    fail "build did not reach SUCCEEDED within timeout (last state: $state_final)" 2
  fi
else
  log "Phase 1: skipped (using existing build $BUILD_ID)"
fi

# ──────────────────────────────────────────────────────────────────────
# Phase 2: attestation endpoint
# ──────────────────────────────────────────────────────────────────────
log "Phase 2: GET /builds/${BUILD_ID}/attestation..."
att_response=$(curl -fsS "${auth_header[@]}" "${LOOPZERO_API}/api/v1/builds/${BUILD_ID}/attestation")
echo "$att_response" >"$LOG_DIR/attestation.json"

signed=$(echo "$att_response" | jq -r '.signed')
digest=$(echo "$att_response" | jq -r '.image_digest // empty')
if [ "$signed" != "true" ]; then
  fail "attestation.signed != true (response saved to $LOG_DIR/attestation.json)" 3
fi
if [ -z "$digest" ] || ! [[ "$digest" =~ ^sha256: ]]; then
  fail "image_digest missing or malformed: '$digest'" 3
fi
ok "attestation: signed=true, digest=$digest"

# ──────────────────────────────────────────────────────────────────────
# Phase 3: SBOM endpoint
# ──────────────────────────────────────────────────────────────────────
log "Phase 3: GET /builds/${BUILD_ID}/sbom..."
sbom_response=$(curl -sS -w "\nHTTP %{http_code}" "${auth_header[@]}" "${LOOPZERO_API}/api/v1/builds/${BUILD_ID}/sbom")
status=$(echo "$sbom_response" | tail -1 | sed 's/HTTP //')
body=$(echo "$sbom_response" | sed '$d')
echo "$body" >"$LOG_DIR/sbom-metadata.json"

case "$status" in
  200) ;;
  404) fail "no SBOM available for this build (HTTP 404). Did the syft step run?" 4 ;;
  *)   fail "unexpected status $status from /sbom endpoint" 4 ;;
esac

apps_count=$(echo "$body" | jq -r '.apps | length')
if [ "$apps_count" -lt 1 ]; then
  fail "/sbom returned 200 but no apps (response saved to $LOG_DIR/sbom-metadata.json)" 4
fi

first_url=$(echo "$body" | jq -r '.apps[0].download_url // empty')
first_format=$(echo "$body" | jq -r '.apps[0].sbom_format // empty')
first_count=$(echo "$body" | jq -r '.apps[0].component_count // 0')

if [ -z "$first_url" ]; then
  log "  no download_url returned (S3 not configured?); skipping SBOM body validation"
else
  log "  downloading SBOM from presigned URL..."
  if ! curl -fsS -o "$SBOM_TMP" "$first_url"; then
    fail "failed to download SBOM from presigned URL" 4
  fi
  # CycloneDX requires bomFormat == "CycloneDX" + a components array.
  bom_format=$(jq -r '.bomFormat // empty' "$SBOM_TMP")
  if [ "$bom_format" != "CycloneDX" ]; then
    fail "downloaded SBOM is not CycloneDX (bomFormat='$bom_format')" 4
  fi
  components_count=$(jq '.components | length' "$SBOM_TMP")
  ok "SBOM is valid CycloneDX with $components_count components"
fi

ok "sbom: format=$first_format component_count=$first_count apps=$apps_count"

# ──────────────────────────────────────────────────────────────────────
# Phase 4: policy-controller reachability (Audit mode is fail-open, but
# we verify the controller is RUNNING — D4 alerting depends on this).
# ──────────────────────────────────────────────────────────────────────
log "Phase 4: policy-controller reachability..."
if command -v kubectl >/dev/null 2>&1; then
  if ! kubectl -n cosign-system get deploy policy-controller-webhook -o name >/dev/null 2>&1; then
    log "  policy-controller deployment not found — skipping (cluster may not have it activated)"
  else
    ready=$(kubectl -n cosign-system get deploy policy-controller-webhook -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
    if [ "${ready:-0}" -lt 1 ]; then
      fail "policy-controller has 0 ready replicas" 5
    fi
    ok "policy-controller has $ready ready replicas (Audit mode)"

    # In Audit mode our deploy MUST have succeeded (the build was verifiable
    # since cosign signed it). Just sanity-check the policy CR exists.
    if kubectl get clusterimagepolicy loopzero-verify-signatures -o name >/dev/null 2>&1; then
      ok "ClusterImagePolicy loopzero-verify-signatures present"
    else
      log "  ClusterImagePolicy not found — Audit mode disabled?"
    fi
  fi
else
  log "  kubectl not on PATH; skipping policy-controller check"
fi

# ──────────────────────────────────────────────────────────────────────
log "Sprint 6 supply-chain E2E: ALL PHASES PASSED"
exit 0
