#!/usr/bin/env bash
# CHG-2026-04-30-INFRA-PARITY — golden test for audit-regression-matrix.sh.
#
# An audit script is only useful if it actually detects miss conditions.
# This test fabricates a matrix with deliberately-broken entries and
# confirms the audit reports them as miss + exits non-zero.
#
# Without this golden test, the audit could silently regress (e.g., always
# returning OK because the regex stopped matching anything) and we'd lose
# the protection guarantee.
#
# Usage:
#   bash infra/tests/parity/audit-golden-test.sh
#
# Exit codes:
#   0 — golden expectations met (audit catches breakage as designed)
#   1 — golden expectations failed (audit logic broken)
#
# SPDX-License-Identifier: Apache-2.0

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
AUDIT="${REPO_ROOT}/infra/tests/parity/audit-regression-matrix.sh"
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

[[ -x "$AUDIT" ]] || { echo "ERROR: audit script not executable at $AUDIT" >&2; exit 1; }

echo "[golden] fabricating broken matrix at $TMPDIR/broken-matrix.md"

# Generate a token guaranteed unique to this run — no chance of accidental
# collision with anything checked into the repo. Hex-only is safe under our
# regex extraction rules.
GHOST_TOKEN="ghost$(head -c 32 /dev/urandom | tr -dc 'a-f0-9' | head -c 24)token"

# Minimal matrix with 3 cases:
#   row 1: token resolves (TestImageTagFormat exists in real codebase)
#   row 2: token DOES NOT resolve, Pode Recorrer? = NÃO  → must trigger MISS critical
#   row 3: live verify → SKIP
cat > "$TMPDIR/broken-matrix.md" <<EOF
# Broken Matrix (golden test — synthetic)

| #  | Category | Problem | Resolved By | Protection | Type | Verification | Pode Recorrer? | Exercitado por L3? |
|----|----------|---------|-------------|------------|------|--------------|----------------|--------------------|
| 1  | TEST     | real    | something   | something  | (a)  | TestImageTagFormat exists | NÃO | SIM |
| 2  | TEST     | broken  | something   | something  | (a)  | ${GHOST_TOKEN} | NÃO | SIM |
| 3  | TEST     | live    | something   | something  | (a)  | live verify by operator | NÃO | SIM |
EOF

# Run the audit pointing at the fabricated matrix.
# Capture exit code separately — `output=$(... || true)` masks the real
# exit code (assignment always returns 0). Use a temp file instead so
# both the textual output AND the actual exit status survive.
out_file="$TMPDIR/audit.out"
set +e
MATRIX="$TMPDIR/broken-matrix.md" bash "$AUDIT" >"$out_file" 2>&1
exit_code=$?
set -e
output="$(cat "$out_file")"

# Patched audit must support MATRIX env override; if it doesn't, golden has
# to exec via a wrapper that writes to the real location. Detect and fall
# back below.
if ! grep -q "broken-matrix.md\|${GHOST_TOKEN}" <<<"$output"; then
  echo "[golden] audit does not honor MATRIX env override — re-running via path swap"
  # Save real matrix, swap fabricated in, run, restore. Atomic.
  REAL="${REPO_ROOT}/docs/runbooks/bootstrap-regression-matrix.md"
  BACKUP="$TMPDIR/real-matrix.bak"
  cp "$REAL" "$BACKUP"
  cp "$TMPDIR/broken-matrix.md" "$REAL"
  set +e
  bash "$AUDIT" >"$out_file" 2>&1
  exit_code=$?
  set -e
  output="$(cat "$out_file")"
  # Restore IMMEDIATELY (must not leave the real matrix corrupted).
  cp "$BACKUP" "$REAL"
fi

echo "---audit output---"
echo "$output"
echo "---end output---"

# Golden expectations:
#   1) Output mentions row #2 token (the unresolvable one) as MISS
#   2) Exit code is non-zero (critical NÃO entry has miss)
#   3) Output reports total=3 (rows parsed correctly)
fail=0

if grep -q "MISS  #2" <<<"$output" || grep -q "${GHOST_TOKEN}" <<<"$output"; then
  echo "[golden]   PASS  audit detected the broken row #2"
else
  echo "[golden]   FAIL  audit did NOT report row #2 as miss"
  fail=$((fail + 1))
fi

if [[ $exit_code -ne 0 ]]; then
  echo "[golden]   PASS  audit exited non-zero on critical miss (exit=$exit_code)"
else
  echo "[golden]   FAIL  audit exited 0 despite critical miss"
  fail=$((fail + 1))
fi

if grep -q "total entries: *3" <<<"$output"; then
  echo "[golden]   PASS  audit parsed 3 rows from fabricated matrix"
else
  echo "[golden]   FAIL  audit row count mismatch (expected 3)"
  fail=$((fail + 1))
fi

if (( fail > 0 )); then
  echo "[golden] FAIL: $fail expectation(s) missed — audit logic regressed"
  exit 1
fi

echo "[golden] PASS — audit script behaves as specified"
exit 0
