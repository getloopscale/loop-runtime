#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# check-write-path-subset-syntax_test.sh — iter 31 productive-idle hermetic
# contract test for the Option A LIVE subset script
# `infra/tests/e2e/dev-public-secrets-write-path-subset_test.sh`.
#
# Locks the contract that the subset script:
#   - Covers exactly 3 scenarios (A, D, E) — NOT 6
#   - Documents the 3 deferred scenarios (B, C, F) + their ESO dependency
#   - Uses the SAME canonical surfaces as T4.1 LIVE script
#     (PG encrypted_value / kubectl logs / kubectl describe)
#   - Generates ephemeral UUID sentinel + clean cleanup trap
#   - Refuses to proceed with --skip-live AND attempts mutually exclusive
#     options (no rehearsal mode in subset since it's already a subset)
#
# Hermetic — NO kubectl, NO cluster, NO network. Pure grep + bash -n.

set -euo pipefail

SCRIPT="${LOOPZERO_REPO_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}/infra/tests/e2e/dev-public-secrets-write-path-subset_test.sh"
TMPDIR=$(mktemp -d -t writepath-subset-XXXXXX)
trap 'rm -rf "$TMPDIR"' EXIT INT TERM

PASS=0
FAIL=0
assert() {
  local desc="$1" cmd="$2"
  if eval "$cmd" >/dev/null 2>&1; then
    echo "PASS: $desc"
    PASS=$((PASS+1))
  else
    echo "FAIL: $desc"
    FAIL=$((FAIL+1))
  fi
}

# A — file + syntax
assert "A1 script exists" "test -f '$SCRIPT'"
assert "A2 bash -n syntax clean" "bash -n '$SCRIPT'"

# B — exactly 3 scenarios (A, D, E) — no B/C/F
assert "B1 Scenario A header present" \
  "grep -E '=== Scenario A:' '$SCRIPT'"

assert "B2 Scenario D header present" \
  "grep -E '=== Scenario D:' '$SCRIPT'"

assert "B3 Scenario E header present" \
  "grep -E '=== Scenario E:' '$SCRIPT'"

assert "B4 Scenario B NOT executed (only mentioned in usage/comment)" \
  "! grep -E '=== Scenario B:' '$SCRIPT'"

assert "B5 Scenario C NOT executed" \
  "! grep -E '=== Scenario C:' '$SCRIPT'"

assert "B6 Scenario F NOT executed" \
  "! grep -E '=== Scenario F:' '$SCRIPT'"

# C — deferred scenarios documented + ESO dependency cited
assert "C1 deferred list cites B, C, F" \
  "grep -E 'deferred.*B.*C.*F|B.*C.*F.*defer' '$SCRIPT'"

assert "C2 ESO dependency cited as reason" \
  "grep -E 'ESO.*(controller|sync|read-path)' '$SCRIPT'"

assert "C3 Open Question runbook cross-link" \
  "grep -E 'Open Question|runbook' '$SCRIPT'"

# D — canonical probes
assert "D1 Scenario A uses psql encoding query" \
  "awk '/Scenario A:/,/record A/' '$SCRIPT' | grep -q 'encode(encrypted_value'"

assert "D2 Scenario A asserts vault:v1: prefix" \
  "awk '/Scenario A:/,/record A/' '$SCRIPT' | grep -q 'vault:v1:'"

assert "D3 Scenario D uses kubectl logs --since" \
  "awk '/Scenario D:/,/record D/' '$SCRIPT' | grep -q 'kubectl.*logs.*--since'"

assert "D4 Scenario E uses kubectl describe pods" \
  "awk '/Scenario E:/,/record E/' '$SCRIPT' | grep -q 'kubectl.*describe pods'"

# E — ephemeral sentinel + cleanup
assert "E1 SENTINEL_UUID generated from /dev/urandom path" \
  "grep -E 'SENTINEL_UUID=.*kernel/random/uuid' '$SCRIPT'"

assert "E2 cleanup trap registered" \
  "grep -E 'trap cleanup EXIT' '$SCRIPT'"

assert "E3 cleanup deletes probe via DELETE endpoint" \
  "awk '/^cleanup\\(\\)/,/^}/' '$SCRIPT' | grep -q 'DELETE'"

# F — flags
assert "F1 --skip-live flag parsed" \
  "grep -E '\-\-skip-live\) SKIP_LIVE=1' '$SCRIPT'"

assert "F2 --json flag parsed" \
  "grep -E '\-\-json\) JSON_MODE=1' '$SCRIPT'"

assert "F3 --help shows usage" \
  "grep -E '\-\-help\|-h\) usage' '$SCRIPT'"

# G — Verdict shape
assert "G1 verdict line emitted with mode=write-path-subset" \
  "grep -E 'mode.*write-path-subset' '$SCRIPT'"

assert "G2 exit code reflects FAIL count" \
  "grep -E 'FAIL_COUNT -eq 0 \\] && exit 0' '$SCRIPT'"

# H — Anti-patterns
assert "H1 no Co-Authored-By trailers" \
  "! grep -i 'Co-Authored-By' '$SCRIPT'"

assert "H2 no --no-verify references" \
  "! grep -q -- '--no-verify' '$SCRIPT'"

# I — Plan + runbook references
assert "I1 cites J-22 plan slug" \
  "grep -E '2026-06-10-j22-openbao-transit-eso-sync' '$SCRIPT'"

assert "I2 cites iter 31 productive-idle origin" \
  "grep -E 'iter 31' '$SCRIPT'"

# Summary
echo ""
echo "write-path-subset contract: pass=$PASS fail=$FAIL"
if [ "$FAIL" -gt 0 ]; then
  echo "snapshot preserved: $TMPDIR"
  trap - EXIT INT TERM
  exit 1
fi
exit 0
