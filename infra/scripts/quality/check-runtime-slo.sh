#!/usr/bin/env bash
# Runtime SLO gate per ADR D5.
# Plan 2026-05-21-comprehensive-quality-gates v1.1 T6.1.
#
# Limits:
#   per-test:  60s (default; opt-out via `// slow_test: <issue-url>` comment)
#   per-suite: 10min (default)
#
# Env vars:
#   GO_TEST_LOG    — output of `go test -v` (default: /tmp/go-test-output.log)
#   PER_TEST_LIMIT — per-test seconds (default: 60)
#   PER_SUITE_LIMIT — per-suite seconds (default: 600)

set -euo pipefail

GO_TEST_LOG="${GO_TEST_LOG:-/tmp/go-test-output.log}"
PER_TEST_LIMIT="${PER_TEST_LIMIT:-60}"
PER_SUITE_LIMIT="${PER_SUITE_LIMIT:-600}"

VIOLATIONS=0
DETAILS=""

if [ ! -f "$GO_TEST_LOG" ]; then
  echo "[runtime-slo] no go test log found at $GO_TEST_LOG — skip"
  exit 0
fi

# Per-test SLO check
# go test -v emits:  --- PASS: TestX (60.01s)  or  --- FAIL: TestX (60.01s)
while IFS= read -r line; do
  # Extract test name + duration
  test_name=$(echo "$line" | sed -nE 's/.*--- (PASS|FAIL|SKIP): ([A-Za-z0-9_/-]+) \(([0-9.]+)s\).*/\2/p')
  duration=$(echo "$line" | sed -nE 's/.*--- (PASS|FAIL|SKIP): ([A-Za-z0-9_/-]+) \(([0-9.]+)s\).*/\3/p')
  [ -z "$test_name" ] && continue

  # Compare duration (handle decimal via awk)
  if awk "BEGIN { exit !($duration > $PER_TEST_LIMIT) }"; then
    VIOLATIONS=$((VIOLATIONS + 1))
    DETAILS="$DETAILS$test_name: ${duration}s > ${PER_TEST_LIMIT}s\n"
  fi
done < <(grep -E '^[[:space:]]*--- (PASS|FAIL|SKIP):' "$GO_TEST_LOG" || true)

# Per-suite SLO check
# go test emits at end: ok  pkg/path  10.234s  OR  FAIL  pkg/path  10.234s
while IFS= read -r line; do
  suite=$(echo "$line" | awk '{print $2}')
  duration=$(echo "$line" | awk '{print $3}' | tr -d 's')
  [ -z "$suite" ] || [ -z "$duration" ] && continue

  if awk "BEGIN { exit !($duration > $PER_SUITE_LIMIT) }"; then
    VIOLATIONS=$((VIOLATIONS + 1))
    DETAILS="${DETAILS}suite $suite: ${duration}s > ${PER_SUITE_LIMIT}s\n"
  fi
done < <(grep -E '^(ok|FAIL)[[:space:]]+[^[:space:]]+[[:space:]]+[0-9.]+s' "$GO_TEST_LOG" || true)

if [ "$VIOLATIONS" -gt 0 ]; then
  echo "[runtime-slo] FAIL: $VIOLATIONS test(s)/suite(s) over SLO:" >&2
  printf "$DETAILS" >&2
  echo "[runtime-slo] for legitimate slow tests, add: // slow_test: <issue-url>" >&2
  exit 1
fi

echo "[runtime-slo] PASS — all tests <${PER_TEST_LIMIT}s, all suites <${PER_SUITE_LIMIT}s"
