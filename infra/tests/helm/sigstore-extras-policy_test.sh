#!/usr/bin/env bash
# T2.3 Test 4 (NetworkPolicy contract) of plan
# 2026-05-28-meet-001-engineering-closure-plan.md.
#
# Asserts that the sigstore-extras chart renders three NetworkPolicies
# (fulcio, rekor, ctlog) with the canonical target ports — locking the
# port contract independently of T3.1's selector refactor. T3.1 adds
# podSelector + SA allowlist; the rendered ports must not change.
#
# Expected port set per the LIVE 2026-05-28 keyless GREEN:
#   - fulcio HTTP  5555
#   - fulcio gRPC  5554
#   - rekor HTTP   3000
#   - ctlog HTTP   6962
#
# Skips when:
#   - helm CLI not in PATH (CI without helm)
#   - sigstore-extras chart directory missing
#
# Run from repo root or anywhere; resolves chart path relative to the
# script's own location.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
CHART_DIR="$REPO_ROOT/infra/helm/sigstore-extras"

if ! command -v helm >/dev/null 2>&1; then
  echo "[sigstore-extras-policy] SKIP: helm CLI not in PATH"
  exit 0
fi

if [ ! -d "$CHART_DIR" ]; then
  echo "[sigstore-extras-policy] SKIP: chart dir $CHART_DIR missing"
  exit 0
fi

# Render the chart with default values.
RENDERED=$(helm template sigstore-extras "$CHART_DIR" 2>&1) || {
  echo "[sigstore-extras-policy] FAIL: helm template failed" >&2
  echo "$RENDERED" >&2
  exit 1
}

# Helpers ------------------------------------------------------------------

failures=0

require() {
  local label="$1" needle="$2"
  if ! grep -F -- "$needle" <<<"$RENDERED" >/dev/null; then
    echo "[sigstore-extras-policy] FAIL: $label — expected to find $needle" >&2
    failures=$((failures + 1))
  fi
}

require_count() {
  local label="$1" needle="$2" want="$3"
  local got
  got=$(grep -cF -- "$needle" <<<"$RENDERED" || true)
  if [ "$got" != "$want" ]; then
    echo "[sigstore-extras-policy] FAIL: $label — expected $want occurrences of $needle, got $got" >&2
    failures=$((failures + 1))
  fi
}

# Assertions ---------------------------------------------------------------

# Three distinct NetworkPolicy kinds (fulcio + rekor + ctlog).
require_count "NetworkPolicy kind count" "kind: NetworkPolicy" 3

# Fulcio target ports.
require "fulcio HTTP port (5555)" "port: 5555"
require "fulcio gRPC port (5554)" "port: 5554"

# Rekor target port.
require "rekor HTTP port (3000)" "port: 3000"

# CTLog target port.
require "ctlog HTTP port (6962)" "port: 6962"

# Each NetworkPolicy targets its own pod via app.kubernetes.io/name label.
require "fulcio podSelector label"    "app.kubernetes.io/name: fulcio"
require "rekor podSelector label"     "app.kubernetes.io/name: rekor"
require "ctlog podSelector label"     "app.kubernetes.io/name: ctlog"

# Every NP must declare Ingress policy type (egress is implicit-allow).
require_count "policyTypes: Ingress" "- Ingress" 3

# Result -------------------------------------------------------------------

if [ "$failures" -gt 0 ]; then
  echo "[sigstore-extras-policy] $failures assertion(s) FAILED" >&2
  exit 1
fi

echo "[sigstore-extras-policy] PASS — fulcio/rekor/ctlog ports + selectors match CR-7 contract"
