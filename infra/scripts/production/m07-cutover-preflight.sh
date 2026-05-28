#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# m07-cutover-preflight.sh — Validates 5 pre-conditions before M07 production
# cutover per docs/operations/m07-cutover-sequence.md (T0.3 of plan
# 2026-05-28-meet-001-engineering-closure v1.1).
#
# Exit codes:
#   0 = ALL PRE-CONDITIONS MET (safe to proceed)
#   1 = One or more pre-conditions FAILED (abort cutover)
#   2 = Usage error
#
# Pre-conditions checked:
#   (1) Migration 000019 + 000020 applied to target DB
#   (2) DOCR image loopzero-cosign-signer:v2.4.3-r0 exists
#   (3) DOCR image loopzero-api:<target-tag> exists
#   (4) Chart loopzero-build-cwts bumped + published in OCI registry
#   (5) TUF root present in Pulumi stack config
#
# Usage:
#   m07-cutover-preflight.sh --target-db=<connection-string> --target-env=<staging|prod>
#                            [--api-tag=<tag>]
#                            [--cosign-tag=v2.4.3-r0]
#                            [--chart-version=0.3.0]

set -uo pipefail

TARGET_DB=""
TARGET_ENV=""
API_TAG="${LOOPZERO_API_CUTOVER_TAG:-m07-cutover-1.0.0}"
COSIGN_TAG="${LOOPZERO_COSIGN_SIGNER_TAG:-v2.4.3-r0}"
CHART_VER="${LOOPZERO_BUILD_CWTS_VERSION:-0.3.0}"
DOCR_REGISTRY="${DOCR_REGISTRY:-registry.digitalocean.com/loopzero-system}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --target-db=*)      TARGET_DB="${1#*=}";  shift ;;
    --target-env=*)     TARGET_ENV="${1#*=}"; shift ;;
    --api-tag=*)        API_TAG="${1#*=}";    shift ;;
    --cosign-tag=*)     COSIGN_TAG="${1#*=}"; shift ;;
    --chart-version=*)  CHART_VER="${1#*=}";  shift ;;
    -h|--help)
      sed -n '1,30p' "$0"; exit 0 ;;
    *)
      echo "ERROR: unknown arg: $1" >&2; exit 2 ;;
  esac
done

[[ -n "$TARGET_DB"  ]] || { echo "ERROR: --target-db required"  >&2; exit 2; }
[[ -n "$TARGET_ENV" ]] || { echo "ERROR: --target-env required" >&2; exit 2; }

log()  { printf '[m07-cutover-preflight] %s\n' "$*"; }
die()  { printf '[m07-cutover-preflight] FAIL: %s\n' "$*" >&2; exit 1; }

FAILED=0

# (1) Migration 000019 + 000020 applied
log "[1/5] Checking migration version on $TARGET_DB..."
MIGRATION_OUT=$(psql -t -c "SELECT version, dirty FROM schema_migrations ORDER BY version DESC LIMIT 1" "$TARGET_DB" 2>&1) || true
LATEST_VERSION=$(printf '%s' "$MIGRATION_OUT" | awk '/[0-9]+/{print $1; exit}')
if [[ -z "$LATEST_VERSION" || "$LATEST_VERSION" -lt 20 ]]; then
  printf '[m07-cutover-preflight] FAIL: migration version=%s < 20 (require 000019 + 000020 applied)\n' "${LATEST_VERSION:-none}" >&2
  FAILED=$((FAILED+1))
else
  log "  OK: migration version=$LATEST_VERSION"
fi

# (2) DOCR image loopzero-cosign-signer:<tag> exists
log "[2/5] Checking DOCR image loopzero-cosign-signer:$COSIGN_TAG..."
if ! doctl registry repository list-tags loopzero-cosign-signer --format Tag --no-header 2>/dev/null | grep -qx "$COSIGN_TAG"; then
  printf '[m07-cutover-preflight] FAIL: loopzero-cosign-signer:%s not found in DOCR\n' "$COSIGN_TAG" >&2
  FAILED=$((FAILED+1))
else
  log "  OK: loopzero-cosign-signer:$COSIGN_TAG present"
fi

# (3) DOCR image loopzero-api:<tag>
log "[3/5] Checking DOCR image loopzero-api:$API_TAG..."
if ! doctl registry repository list-tags loopzero-api --format Tag --no-header 2>/dev/null | grep -qx "$API_TAG"; then
  printf '[m07-cutover-preflight] FAIL: loopzero-api:%s not found in DOCR\n' "$API_TAG" >&2
  FAILED=$((FAILED+1))
else
  log "  OK: loopzero-api:$API_TAG present"
fi

# (4) Chart loopzero-build-cwts bumped + published
log "[4/5] Checking chart loopzero-build-cwts:$CHART_VER published..."
CHART_OUT=$(helm show chart "oci://${DOCR_REGISTRY}/loopzero-build-cwts" --version "$CHART_VER" 2>&1) || true
if ! printf '%s' "$CHART_OUT" | grep -qE "version:\\s*${CHART_VER}"; then
  printf '[m07-cutover-preflight] FAIL: chart loopzero-build-cwts:%s not published or version mismatch\n' "$CHART_VER" >&2
  FAILED=$((FAILED+1))
else
  log "  OK: chart loopzero-build-cwts:$CHART_VER published"
fi

# (5) TUF root present in Pulumi stack
log "[5/5] Checking Pulumi stack output tufRoot..."
TUF_OUT=$(pulumi stack output tufRoot --stack "loopzero/$TARGET_ENV" 2>&1) || true
if [[ -z "$TUF_OUT" ]] || printf '%s' "$TUF_OUT" | grep -qiE "(error|not found|undefined)"; then
  printf '[m07-cutover-preflight] FAIL: TUF root absent or unreadable in Pulumi stack loopzero/%s\n' "$TARGET_ENV" >&2
  FAILED=$((FAILED+1))
else
  log "  OK: TUF root present (${#TUF_OUT} bytes base64)"
fi

echo
if (( FAILED > 0 )); then
  log "ABORT: $FAILED pre-condition(s) FAILED — cutover NOT safe"
  exit 1
fi
log "ALL PRE-CONDITIONS MET — safe to proceed with cutover sequence per docs/operations/m07-cutover-sequence.md"
exit 0
