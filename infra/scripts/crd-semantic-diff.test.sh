#!/usr/bin/env bash
# Test suite for crd-semantic-diff.sh.
# Plan: charts-sota-implementation-plan T1.4 TDD.

set -euo pipefail
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

PASS=0
FAIL=0

assert() {
  local name="$1"
  local expected="$2"
  local actual="$3"
  if [[ "$expected" == "$actual" ]]; then
    echo "  ✓ $name"
    PASS=$((PASS + 1))
  else
    echo "  ✗ $name (expected '$expected', got '$actual')"
    FAIL=$((FAIL + 1))
  fi
}

# Find yq
YQ=""
if command -v yq >/dev/null 2>&1; then
  YQ="yq"
elif [[ -x /tmp/yq ]]; then
  YQ="/tmp/yq"
fi
if [[ -z "$YQ" ]]; then
  echo "yq not available; skipping crd-semantic-diff tests."
  exit 0
fi

EXTRA_PATH=""
if [[ "$YQ" == "/tmp/yq" ]]; then
  EXTRA_PATH="PATH=/tmp:\$PATH"
fi

echo "[Test 1] Identical specs return exit 0"
set +e
eval "$EXTRA_PATH bash scripts/crd-semantic-diff.sh > /tmp/crd_diff.txt 2>&1"
RC=$?
set -e
assert "exit code 0 when in sync" "0" "$RC"

echo "[Test 2] Output reports all 10 CRDs scanned"
SCANNED=$(grep -c '^  ✓ ' /tmp/crd_diff.txt || true)
assert "10 CRDs scanned" "10" "$SCANNED"

echo "[Test 3] --crd filter scans single CRD"
set +e
eval "$EXTRA_PATH bash scripts/crd-semantic-diff.sh --crd tenants > /tmp/crd_single.txt 2>&1"
RC=$?
set -e
assert "single-CRD exit 0" "0" "$RC"
SCANNED=$(grep -c '^  ✓ ' /tmp/crd_single.txt || true)
assert "1 CRD scanned with filter" "1" "$SCANNED"

echo "[Test 4] Drift simulation: temporarily mutate source CRD"
TARGET="operators/config/crd/bases/loopzero.io_tenants.yaml"
BACKUP="/tmp/tenants.yaml.bak"
cp "$TARGET" "$BACKUP"
# Inject a fake property that the chart template won't have.
# Use yq to mutate the source.
if [[ "$YQ" == "/tmp/yq" ]]; then
  /tmp/yq -i '.spec.versions[0].schema.openAPIV3Schema.properties.injected_test_field = {"type": "string"}' "$TARGET"
else
  yq -i '.spec.versions[0].schema.openAPIV3Schema.properties.injected_test_field = {"type": "string"}' "$TARGET"
fi
set +e
eval "$EXTRA_PATH bash scripts/crd-semantic-diff.sh --crd tenants > /tmp/crd_drift.txt 2>&1"
RC=$?
set -e
mv "$BACKUP" "$TARGET"
assert "exit code 1 when drift exists" "1" "$RC"

echo
echo "Results: $PASS passed, $FAIL failed"
exit $FAIL
