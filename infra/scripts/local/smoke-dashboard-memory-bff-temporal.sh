#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# T4.1 + EC-10 of dashboard-memories-tab-standardization-plan.md v1.5.
#
# Smoke test asserting BFF /v1/memory/list?scope=user returns the EXACT
# `created_at` value that upstream TheMemory REST emits at
# /v1/scopes/user/{userID}/memories. The Go unit test
# `TestTheMemoryClient_ListPassesThroughUpstreamCreatedAt` covers the BFF
# contract with a stubbed upstream; THIS smoke runs the full chain end-to-end
# against a REAL TheMemory server boot + REAL BFF + REAL curl to surface
# any drift the unit test can't see (e.g., BFF middleware time-stamping,
# JSON marshaling losing precision, scope routing bug at the real REST surface).
#
# EC-10 absorption: this script REFUSES to run against a mock. If the
# THEMORY_BASE_URL env points at anything other than a real themory boot,
# the assertion is vacuous and silently masks BFF bugs. Honest failure
# (refusing to run) > false-pass.
#
# Pre-conditions:
#   - TheMemory server running at $THEMORY_BASE_URL (default http://localhost:8080)
#   - BFF running at $BFF_BASE_URL (default http://localhost:13002)
#   - Auth: $THEMORY_API_KEY (master key) + $LOOPZERO_LIVE_KEY (customer key for BFF)
#   - jq + curl in PATH
#
# Usage:
#   THEMORY_BASE_URL=http://localhost:8080 \
#   BFF_BASE_URL=http://localhost:13002 \
#   THEMORY_API_KEY=<master> \
#   LOOPZERO_LIVE_KEY=loopzero_live_<token> \
#   USER_ID=alice \
#     bash infra/scripts/local/smoke-dashboard-memory-bff-temporal.sh
#
# Exit codes:
#   0  smoke PASS — BFF pass-through verified
#   1  pre-condition missing (env var unset)
#   2  HTTP error (themory or BFF unreachable)
#   3  contract violation (BFF createdAt != upstream createdAt)

set -euo pipefail

# ---- pre-conditions ----
: "${THEMORY_BASE_URL:?THEMORY_BASE_URL required (e.g., http://localhost:8080)}"
: "${BFF_BASE_URL:?BFF_BASE_URL required (e.g., http://localhost:13002)}"
: "${THEMORY_API_KEY:?THEMORY_API_KEY required (master key for direct upstream call)}"
: "${LOOPZERO_LIVE_KEY:?LOOPZERO_LIVE_KEY required (customer key for BFF call)}"
: "${USER_ID:?USER_ID required (subject for the test query)}"

if ! command -v jq >/dev/null 2>&1; then
  echo >&2 "ERROR: jq not in PATH"; exit 1
fi
if ! command -v curl >/dev/null 2>&1; then
  echo >&2 "ERROR: curl not in PATH"; exit 1
fi

# ---- EC-10 honesty guard: refuse if either endpoint looks like a mock ----
# httpbin / httptest signatures usually carry "httptest" or "httpbin" in
# the URL. If we see those, the smoke is vacuous — refuse rather than
# false-pass.
for url in "$THEMORY_BASE_URL" "$BFF_BASE_URL"; do
  case "$url" in
    *httpbin*|*httptest*|*mock*|*localhost:0*)
      echo >&2 "ERROR (EC-10): URL looks mock-like ($url) — refusing to run vacuous smoke"
      exit 1
      ;;
  esac
done

echo "T4.1 smoke — BFF pass-through createdAt fidelity"
echo "  THEMORY_BASE_URL: $THEMORY_BASE_URL"
echo "  BFF_BASE_URL:     $BFF_BASE_URL"
echo "  USER_ID:          $USER_ID"
echo

# ---- Step 1: call upstream TheMemory directly ----
echo "Step 1: GET ${THEMORY_BASE_URL}/v1/scopes/user/${USER_ID}/memories"
UPSTREAM_BODY=$(curl -sf \
  -H "Authorization: Bearer ${THEMORY_API_KEY}" \
  "${THEMORY_BASE_URL}/v1/scopes/user/${USER_ID}/memories?limit=5" || true)
if [ -z "$UPSTREAM_BODY" ]; then
  echo >&2 "ERROR: TheMemory upstream unreachable at $THEMORY_BASE_URL"
  exit 2
fi

# Extract first item's createdAt (REST shape: {items: [{createdAt: ...}]}).
UPSTREAM_CREATED_AT=$(echo "$UPSTREAM_BODY" | jq -r '.items[0]?.createdAt // empty')
UPSTREAM_ID=$(echo "$UPSTREAM_BODY" | jq -r '.items[0]?.id // empty')
if [ -z "$UPSTREAM_CREATED_AT" ]; then
  echo >&2 "ERROR: upstream returned no items OR no createdAt field"
  echo >&2 "Body: $UPSTREAM_BODY"
  exit 2
fi
echo "  upstream item: id=$UPSTREAM_ID createdAt=$UPSTREAM_CREATED_AT"

# ---- Step 2: call BFF via customer-facing endpoint ----
echo
echo "Step 2: GET ${BFF_BASE_URL}/v1/memory/list?scope=user&limit=5"
BFF_BODY=$(curl -sf \
  -H "Authorization: Bearer ${LOOPZERO_LIVE_KEY}" \
  "${BFF_BASE_URL}/v1/memory/list?scope=user&limit=5" || true)
if [ -z "$BFF_BODY" ]; then
  echo >&2 "ERROR: BFF unreachable at $BFF_BASE_URL"
  exit 2
fi

# Extract first row's created_at (BFF shape: {memories: [{created_at: ...}]}).
BFF_CREATED_AT=$(echo "$BFF_BODY" | jq -r '.memories[0]?.created_at // empty')
BFF_ID=$(echo "$BFF_BODY" | jq -r '.memories[0]?.id // empty')
if [ -z "$BFF_CREATED_AT" ]; then
  echo >&2 "ERROR: BFF returned no memories OR no created_at field"
  echo >&2 "Body: $BFF_BODY"
  exit 2
fi
echo "  bff      row:  id=$BFF_ID created_at=$BFF_CREATED_AT"

# ---- Step 3: assert pass-through ----
echo
if [ "$UPSTREAM_CREATED_AT" = "$BFF_CREATED_AT" ]; then
  echo "PASS — BFF passes upstream createdAt VERBATIM (T4.1 contract verified)."
  echo "       Both endpoints returned: $UPSTREAM_CREATED_AT"
  exit 0
fi

echo >&2 "FAIL (T4.1 contract violation): BFF rewrote upstream createdAt"
echo >&2 "  upstream: $UPSTREAM_CREATED_AT"
echo >&2 "  bff:      $BFF_CREATED_AT"
echo >&2 "Per plan ADR D4, BFF is HTTP-only pass-through; createdAt MUST surface verbatim."
echo >&2 "Investigate: cloud/internal/themory/client.go::List + cloud/internal/routes/memory.go::List"
exit 3
