#!/usr/bin/env bash
# Test harness for check-decisions-deletion.sh.
#
# Creates a throwaway git repository, simulates the four protected
# scenarios (ADR delete, MEET delete, runbook delete, inventory delete),
# and asserts the guard's exit code + stdout.
#
# Run: bash infra/scripts/check-decisions-deletion_test.sh
# Exit 0 = all tests pass; 1 = at least one failure.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GUARD="${SCRIPT_DIR}/check-decisions-deletion.sh"

if [[ ! -x "$GUARD" ]]; then
  echo "FATAL: guard script not executable at $GUARD" >&2
  exit 1
fi

PASS=0
FAIL=0

# Run a test scenario in an isolated tmp git repo.
#
# Args:
#   $1 — test name
#   $2 — relative path of file to create on main
#   $3 — content for the created file
#   $4 — expected guard exit code (0 = clean, 1 = violation)
#   $5 — optional: name of superseding file to create on PR branch
#   $6 — optional: content of superseding file (typically includes
#        "superseded_by: <basename>")
run_scenario() {
  local name="$1"
  local file="$2"
  local content="$3"
  local expect_exit="$4"
  local superseder="${5:-}"
  local superseder_content="${6:-}"

  local tmp
  tmp=$(mktemp -d)
  pushd "$tmp" >/dev/null

  git init -q
  git config user.email test@example.com
  git config user.name "test"
  git checkout -q -b main

  mkdir -p "$(dirname "$file")"
  printf '%s' "$content" > "$file"
  git add "$file"
  git commit -q -m "main: seed $file"

  git checkout -q -b pr
  git rm -q "$file"
  if [[ -n "$superseder" ]]; then
    mkdir -p "$(dirname "$superseder")"
    printf '%s' "$superseder_content" > "$superseder"
    git add "$superseder"
  fi
  git commit -q -m "pr: delete protected file"

  local actual_exit=0
  bash "$GUARD" main pr >/tmp/guard-out.$$ 2>&1 || actual_exit=$?

  popd >/dev/null
  rm -rf "$tmp"

  if [[ "$actual_exit" -eq "$expect_exit" ]]; then
    echo "PASS  $name (exit=$actual_exit)"
    PASS=$((PASS + 1))
  else
    echo "FAIL  $name (expected exit=$expect_exit, got $actual_exit)"
    echo "      guard output:"
    sed 's/^/        /' /tmp/guard-out.$$
    FAIL=$((FAIL + 1))
  fi
  rm -f /tmp/guard-out.$$
}

# Scenario 1: delete ADR without superseder → violation
run_scenario \
  "decisions: delete without superseder → exit 1" \
  ".claude/knowledge-base/decisions/2026-05-12-test.md" \
  $'---\ntype: decision\n---\nbody' \
  1

# Scenario 2: delete ADR WITH superseder → clean
run_scenario \
  "decisions: delete WITH superseder → exit 0" \
  ".claude/knowledge-base/decisions/2026-05-12-test.md" \
  $'---\ntype: decision\n---\nbody' \
  0 \
  ".claude/knowledge-base/decisions/2026-05-12-replacement.md" \
  $'---\ntype: decision\nsuperseded_by: 2026-05-12-test.md\n---\nreplacement body'

# Scenario 3: delete MEET DR without superseder → violation
run_scenario \
  "meetings: delete MEET without superseder → exit 1" \
  ".claude/knowledge-base/meetings/MEET-2026-05-12-test.md" \
  $'---\ntype: meeting\n---\nbody' \
  1

# Scenario 4: delete operations runbook without superseder → violation
# (this is the T8.5a recurrence guard — the lesson from a6e8b895)
run_scenario \
  "operations: delete runbook without superseder → exit 1" \
  ".claude/knowledge-base/concepts/operations/network-policy-egress-runbook.md" \
  $'---\ntype: reference\n---\nbody' \
  1

# Scenario 5: delete operations inventory without superseder → violation
# (same recurrence guard — covers *-inventory.md glob)
run_scenario \
  "operations: delete inventory without superseder → exit 1" \
  ".claude/knowledge-base/concepts/operations/network-policy-egress-inventory.md" \
  $'---\ntype: reference\n---\nbody' \
  1

# Scenario 6: delete operations note (NOT runbook/inventory) → clean
# (negative test — glob must NOT over-match arbitrary operations notes)
run_scenario \
  "operations: delete arbitrary note → exit 0 (not protected)" \
  ".claude/knowledge-base/concepts/operations/random-notes.md" \
  $'---\ntype: reference\n---\nbody' \
  0

# Scenario 7: delete operations inventory WITH superseder → clean
# (positive test for the new path)
run_scenario \
  "operations: delete inventory WITH superseder → exit 0" \
  ".claude/knowledge-base/concepts/operations/old-inventory.md" \
  $'---\ntype: reference\n---\nbody' \
  0 \
  ".claude/knowledge-base/concepts/operations/new-inventory.md" \
  $'---\ntype: reference\nsuperseded_by: old-inventory.md\n---\nreplacement body'

# Scenario 8: no protected deletions in PR → clean
run_scenario \
  "no deletions → exit 0" \
  "src/code.go" \
  "package main" \
  0

echo ""
echo "Summary: $PASS pass, $FAIL fail"
if [[ "$FAIL" -gt 0 ]]; then
  exit 1
fi
exit 0
