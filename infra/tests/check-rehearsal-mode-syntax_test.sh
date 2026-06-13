#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# check-rehearsal-mode-syntax_test.sh — iter 26 productive-idle hermetic
# structural test for the `--rehearsal` flag added to the T4.1 LIVE
# script (plan 2026-06-10-j22-openbao-transit-eso-sync v1.2).
#
# Locks the contract that the LIVE script:
#   - Parses --rehearsal flag + sets REHEARSAL_MODE=1
#   - Documents --rehearsal in --help/usage
#   - Enforces mutual exclusion with --skip-live (returns exit 2 FATAL)
#   - Defines do_rehearsal() with 5 named probes R1-R5
#   - Probes the canonical cluster surfaces (ESO, CSS, both secret ns,
#     loopzero-api env var)
#   - Early-exits BEFORE the probe-secret POST when REHEARSAL_MODE=1
#     (no encrypt path exercised, no PG row written)
#   - Exit 0 on all-PASS, exit 1 on any-FAIL
#
# Hermetic — NO kubectl, NO cluster access. Pure grep + bash -n on the
# LIVE script source.

set -euo pipefail

SCRIPT="${LOOPZERO_REPO_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}/infra/tests/e2e/dev-public-secrets-zero-leak_test.sh"
TASKFILE="${LOOPZERO_REPO_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}/taskfiles/test.yml"
TMPDIR=$(mktemp -d -t rehearsal-syntax-XXXXXX)
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

# A — file exists + bash -n syntax clean
assert "A1 LIVE script exists" "test -f '$SCRIPT'"

assert "A2 bash -n syntax clean" "bash -n '$SCRIPT'"

# B — Flag declaration + parsing
assert "B1 REHEARSAL_MODE declared" \
  "grep -E '^REHEARSAL_MODE=0' '$SCRIPT'"

assert "B2 case branch parses --rehearsal flag" \
  "grep -E '\-\-rehearsal\) REHEARSAL_MODE=1' '$SCRIPT'"

# C — Usage doc surfaces --rehearsal
assert "C1 usage line mentions --rehearsal" \
  "grep -E '\[--rehearsal\]' '$SCRIPT'"

assert "C2 usage block describes --rehearsal purpose" \
  "grep -E 'rehearsal mode|preflight probes' '$SCRIPT'"

# D — Mutual exclusion guard
assert "D1 mutex check between SKIP_LIVE and REHEARSAL_MODE present" \
  "grep -E 'SKIP_LIVE -eq 1 \] && \[ \\\$REHEARSAL_MODE -eq 1' '$SCRIPT'"

assert "D2 mutex FATAL message uses both flag names verbatim" \
  "grep -E 'FATAL: --skip-live and --rehearsal are mutually exclusive' '$SCRIPT'"

assert "D3 mutex returns exit 2 (usage error per script's own contract)" \
  "awk '/SKIP_LIVE -eq 1 \] && \[ \\\$REHEARSAL_MODE/,/^fi/' '$SCRIPT' | grep -q 'exit 2'"

# E — do_rehearsal function + 5 R1-R5 probes
assert "E1 do_rehearsal() function defined" \
  "grep -E '^do_rehearsal\(\)' '$SCRIPT'"

for probe in R1 R2 R3 R4 R5; do
  assert "E-${probe}: probe ${probe} declared" \
    "grep -E '\"${probe} ' '$SCRIPT'"
done

# F — Canonical probe surfaces named in the 5 probes
assert "F1 R1 probes ESO controller pods" \
  "grep -A2 'R1 ESO' '$SCRIPT' | grep -q 'external-secrets'"

assert "F2 R2 probes ClusterSecretStore openbao-transit Ready=True" \
  "grep -A2 'R2 ClusterSecretStore' '$SCRIPT' | grep -q 'openbao-transit'"

assert "F3 R3 probes Secret in external-secrets ns" \
  "grep -A2 'R3 Secret loopzero-openbao-approle' '$SCRIPT' | grep -q 'external-secrets'"

assert "F4 R4 probes Secret in loopzero-system ns" \
  "grep -A2 'R4 Secret loopzero-openbao-approle' '$SCRIPT' | grep -q 'loopzero-system'"

assert "F5 R5 probes loopzero-api OPENBAO_TRANSIT_ENABLED env var" \
  "grep -A2 'R5 loopzero-api' '$SCRIPT' | grep -q 'OPENBAO_TRANSIT_ENABLED'"

# G — Early-exit BEFORE the encrypt path
# The REHEARSAL_MODE block runs do_rehearsal + exit $? early. This
# means the probe POST + sleep 35 + scenarios A-F never execute under
# --rehearsal. Defense against future drift where someone might
# accidentally let rehearsal fall through to the encrypt path.
assert "G1 REHEARSAL_MODE block early-exits via exit \$?" \
  "awk '/REHEARSAL_MODE -eq 1/,/^fi/' '$SCRIPT' | grep -q 'exit \$?'"

assert "G2 REHEARSAL_MODE block does NOT call curl (no probe POST)" \
  "! awk '/REHEARSAL_MODE -eq 1/,/^fi/' '$SCRIPT' | grep -q 'curl'"

# H — Taskfile target
assert "H1 task test:dogfood:secrets-openbao:rehearsal registered" \
  "grep -q '^  test:dogfood:secrets-openbao:rehearsal:' '$TASKFILE'"

assert "H2 task body invokes the LIVE script with --rehearsal" \
  "sed -n '/^  test:dogfood:secrets-openbao:rehearsal:/,/^  [a-z][a-z]*:/p' '$TASKFILE' | grep -q 'dev-public-secrets-zero-leak_test.sh --rehearsal'"

# I — Anti-patterns
assert "I1 no Co-Authored-By in script comments" \
  "! grep -qi 'Co-Authored-By' '$SCRIPT'"

assert "I2 no --no-verify references" \
  "! grep -q -- '--no-verify' '$SCRIPT'"

# Summary
echo ""
echo "rehearsal-mode contract: pass=$PASS fail=$FAIL"
if [ "$FAIL" -gt 0 ]; then
  echo "snapshot dir preserved: $TMPDIR"
  trap - EXIT INT TERM
  exit 1
fi
exit 0
