#!/usr/bin/env bash
# T6.7 — Hermetic gate: cli/src/ exit codes must be 0-5 only.
#
# Plan 2026-05-21-deep-review-remediation T6.7 +
# .claude/rules/cli-output.md Rule 4 + Rule 4 of CLAUDE.md.
#
# Allowed exit codes (permanent contract):
#   0  success
#   1  usage error (bad args, missing flags)
#   2  build failure
#   3  deploy failure
#   4  auth error
#   5  partial failure
#
# Allowed exceptions:
#   - 130 (SIGINT) + 143 (SIGTERM): Unix signal-handler convention
#     (exit = 128 + signal). MUST be inside a process.on('SIG*', ...).
#   - process.exitCode = N (not process.exit) — this allows main() to
#     return naturally and run cleanup.
#   - Test files (*.test.ts, *.spec.ts, __tests__/).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$REPO_ROOT"

if [ ! -d cli/src ]; then
  echo "[cli-exit-codes] SKIP — cli/src/ not present"
  exit 0
fi

VIOLATIONS=0
fail() { VIOLATIONS=$((VIOLATIONS + 1)); echo "FAIL: $*" >&2; }

# Find all process.exit(<number>) literals
while IFS= read -r match; do
  [ -z "$match" ] && continue
  file_line="${match%:*}"  # "cli/src/X.ts:NN"
  code=$(echo "$match" | grep -oE 'process\.exit\([0-9]+' | grep -oE '[0-9]+$')

  # Allowed: 0-5
  if [ "$code" -ge 0 ] && [ "$code" -le 5 ]; then
    continue
  fi

  # Signal handler exceptions: 130 (SIGINT) + 143 (SIGTERM)
  if [ "$code" = "130" ] || [ "$code" = "143" ]; then
    # Verify inside a process.on('SIG...') handler context (within 3 lines above)
    file="${file_line%:*}"
    lineno="${file_line##*:}"
    start=$((lineno - 3))
    [ "$start" -lt 1 ] && start=1
    if sed -n "${start},${lineno}p" "$file" 2>/dev/null | grep -q "process\.on('SIG"; then
      continue  # Legit signal handler
    fi
  fi

  fail "$file_line — process.exit($code) outside 0-5 contract"
done < <(
  grep -rEn 'process\.exit\([0-9]+\)' cli/src/ 2>/dev/null \
    | grep -vE "\.test\.|\.spec\.|__tests__/|node_modules" \
    || true
)

if [ "$VIOLATIONS" -gt 0 ]; then
  echo "[cli-exit-codes] FAIL: $VIOLATIONS process.exit() call(s) outside 0-5 contract" >&2
  echo "" >&2
  echo "Permanent contract (Rule 4): 0/1/2/3/4/5 only." >&2
  echo "Signal handlers (process.on SIGINT/SIGTERM) may use 130/143 — verify context." >&2
  exit 1
fi

echo "[cli-exit-codes] PASS — all cli/src/ exit codes within 0-5 contract (signal handlers exempt)"
