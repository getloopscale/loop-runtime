#!/usr/bin/env bash
# Test suite for audit-helm-drift.sh.
# Plan: charts-sota-implementation-plan T1.3 TDD.

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
    PASS=$((PASS+1))
  else
    echo "  ✗ $name (expected '$expected', got '$actual')"
    FAIL=$((FAIL+1))
  fi
}

echo "[Test 1] Drift detector emits exit 0 when all drifts have ADR"
bash scripts/audit-helm-drift.sh > /tmp/drift_output.txt 2>&1
RC=$?
assert "exit code 0" "0" "$RC"

echo "[Test 2] Output contains intentional marker for approved drifts"
INTENTIONAL_LINES=$(grep -c "intentional (approved by" /tmp/drift_output.txt || true)
[[ "$INTENTIONAL_LINES" -ge 2 ]] && assert "≥2 intentional drifts" "ok" "ok" || assert "≥2 intentional drifts" "ok" "fail"

echo "[Test 3] JSON output is valid JSON"
JSON=$(bash scripts/audit-helm-drift.sh json 2>/dev/null || true)
echo "$JSON" | python3 -c "import sys, json; json.loads(sys.stdin.read())" 2>/dev/null && assert "valid JSON" "ok" "ok" || assert "valid JSON" "ok" "fail"

echo "[Test 4] github-annotations format emits ::notice when clean"
ANNOTATIONS=$(bash scripts/audit-helm-drift.sh github-annotations 2>&1)
if echo "$ANNOTATIONS" | grep -q "::notice::"; then
  assert "emits ::notice::" "ok" "ok"
else
  assert "emits ::notice::" "ok" "fail (no ::notice:: line)"
fi

echo "[Test 5] Pre-T1.2 regression: temporarily move 1 template, expect exit 1"
TMPFILE="/tmp/drift_test_template.yaml"
TARGET="charts/loopzero-admission-policies/templates/cluster-image-policy.yaml"
if [[ -f "$TARGET" ]]; then
  mv "$TARGET" "$TMPFILE"
  set +e
  bash scripts/audit-helm-drift.sh > /tmp/drift_output_test5.txt 2>&1
  RC=$?
  set -e
  mv "$TMPFILE" "$TARGET"  # restore
  assert "exit code 1 when drift unapproved" "1" "$RC"
else
  echo "  ⊘ skip (target file missing)"
fi

echo
echo "Results: $PASS passed, $FAIL failed"
exit $FAIL
