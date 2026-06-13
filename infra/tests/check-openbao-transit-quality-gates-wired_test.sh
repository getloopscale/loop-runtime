#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# check-openbao-transit-quality-gates-wired_test.sh — iter 27 productive-idle
# meta-test that travas o J-22 OpenBao Transit anti-drift suite no `task
# quality:gates` umbrella. Defends against silent removal of any of the
# 4 hermetic contract tests shipped iter 20/24/25/26.
#
# Why this exists: each of the 4 hermetic tests guards a canonical
# pattern (chart envFrom shape / task shortcut parity / overlay shape /
# rehearsal early-exit). If a future commit removes one from
# taskfiles/quality.yml's `quality:gates` cmds (or accidentally comments
# it out), the CI gate silently loses anti-drift coverage for that
# specific J-22 invariant. This meta-test makes the removal a CI fail
# instead.
#
# Hermetic — NO kubectl, NO bash subprocess. Pure grep on taskfiles/
# quality.yml + verification that each referenced script file exists.

set -euo pipefail

TASKFILE="${LOOPZERO_REPO_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}/taskfiles/quality.yml"
REPO_ROOT="${LOOPZERO_REPO_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
TMPDIR=$(mktemp -d -t openbao-transit-gates-XXXXXX)
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

# Extract just the `quality:gates` body so we don't accidentally match
# the same script wired by a different task (defense vs misattribution).
sed -n '/^  gates:/,/^  [a-z][a-z]*:/p' "$TASKFILE" > "$TMPDIR/gates-body.txt"

# A — taskfile parseable + gates body non-empty
assert "A1 taskfiles/quality.yml exists" "test -f '$TASKFILE'"
assert "A2 quality:gates body extracted (>20 lines)" \
  "[ \"\$(wc -l < '$TMPDIR/gates-body.txt')\" -gt 20 ]"

# B — Each of the 5 J-22 hermetic tests is wired in the gates body
# (iter 31 extended from 4 to 5 — added write-path-subset)
declare -A TESTS=(
  [t20_envfrom]="infra/tests/helm/loopzero-api-openbao-approle-envfrom_test.sh"
  [t24_shortcut]="infra/tests/check-vault-bootstrap-in-mgmt-task_test.sh"
  [t25_overlay]="infra/tests/check-dev-public-loopzero-external-secrets-overlay_test.sh"
  [t26_rehearsal]="infra/tests/check-rehearsal-mode-syntax_test.sh"
  [t31_writepath]="infra/tests/check-write-path-subset-syntax_test.sh"
  [t33_optionb]="infra/tests/check-option-b-eso-deploy-doc_test.sh"
)

for slot in t20_envfrom t24_shortcut t25_overlay t26_rehearsal t31_writepath t33_optionb; do
  path="${TESTS[$slot]}"
  assert "B-${slot}: '$path' wired in quality:gates body" \
    "grep -F 'bash $path' '$TMPDIR/gates-body.txt'"
  # And the file itself must exist (no dangling reference)
  assert "B-${slot}-file: '$path' file exists on disk" \
    "test -f '$REPO_ROOT/$path'"
done

# C — Each test must be UNCOMMENTED (defense vs silent disable via `#`)
for slot in t20_envfrom t24_shortcut t25_overlay t26_rehearsal t31_writepath t33_optionb; do
  path="${TESTS[$slot]}"
  # Look for the line that has the bash invocation AND starts with `      -` (yaml list item)
  # If anyone changes to a comment, the line will start with `      #`
  assert "C-${slot}: '$path' line starts with yaml list marker (not comment)" \
    "grep -E '^      - bash $path' '$TMPDIR/gates-body.txt'"
done

# D — All 4 tests cite their plan iter in the inline comment (audit trail)
assert "D1 t20 invocation cites iter 20 / T6.1" \
  "grep -A0 'loopzero-api-openbao-approle-envfrom_test.sh' '$TMPDIR/gates-body.txt' | grep -E '(iter 20|T6\\.1)'"

assert "D2 t24 invocation cites iter 24 / Option (c)" \
  "grep -A0 'check-vault-bootstrap-in-mgmt-task_test.sh' '$TMPDIR/gates-body.txt' | grep -E '(iter 24|Option \\(c\\))'"

assert "D3 t25 invocation cites iter 25 / overlay" \
  "grep -A0 'check-dev-public-loopzero-external-secrets-overlay_test.sh' '$TMPDIR/gates-body.txt' | grep -E '(iter 25|overlay)'"

assert "D4 t26 invocation cites iter 26 / rehearsal" \
  "grep -A0 'check-rehearsal-mode-syntax_test.sh' '$TMPDIR/gates-body.txt' | grep -E '(iter 26|rehearsal)'"

assert "D5 t31 invocation cites iter 31 / write-path / Option A" \
  "grep -A0 'check-write-path-subset-syntax_test.sh' '$TMPDIR/gates-body.txt' | grep -E '(iter 31|write-path|Option A)'"

assert "D6 t33 invocation cites iter 33 / Option B / ESO deploy" \
  "grep -A0 'check-option-b-eso-deploy-doc_test.sh' '$TMPDIR/gates-body.txt' | grep -E '(iter 33|Option B|ESO)'"

# E — The 6 invocations cluster together (consistency: all 6 wired in
# one logical block, not scattered). Defense vs partial cherry-pick
# refactors that move only some of them. Now includes iter 31 + iter 33
# additions → first envFrom test, last is Option B doc check.
LINES_BETWEEN_FIRST_AND_LAST=$(awk '
  /loopzero-api-openbao-approle-envfrom_test\.sh/ {first=NR}
  /check-option-b-eso-deploy-doc_test\.sh/ {last=NR}
  END {print last-first}
' "$TMPDIR/gates-body.txt")

assert "E1 all 6 invocations within 14 lines of each other (clustered)" \
  "[ '$LINES_BETWEEN_FIRST_AND_LAST' -le 14 ] && [ '$LINES_BETWEEN_FIRST_AND_LAST' -ge 4 ]"

# F — Anti-pattern: no `|| true` or `2>/dev/null` suffix that would
# silently swallow failures from these critical tests
for slot in t20_envfrom t24_shortcut t25_overlay t26_rehearsal t31_writepath t33_optionb; do
  path="${TESTS[$slot]}"
  assert "F-${slot}: no '|| true' silent-pass on $slot" \
    "! grep -E 'bash $path.*\\|\\| true' '$TMPDIR/gates-body.txt'"

  assert "F-${slot}-redirect: no stderr redirect to /dev/null on $slot" \
    "! grep -E 'bash $path.*2>/dev/null' '$TMPDIR/gates-body.txt'"
done

# Summary
echo ""
echo "openbao-transit quality:gates wire-up: pass=$PASS fail=$FAIL"
if [ "$FAIL" -gt 0 ]; then
  echo "snapshot preserved: $TMPDIR"
  trap - EXIT INT TERM
  exit 1
fi
exit 0
