#!/usr/bin/env bash
# check-vault-bootstrap-unseal-loop_test.sh
#
# Regression guard for the 2026-06-15 dev-public Cleanroom canonical fix
# of `infra/scripts/local/vault-bootstrap-dev.sh` (Steps 2 + 3).
#
# Bug class: `bao status` uses exit-code semantics — rc=0 unsealed,
# rc=2 sealed-but-initialized (NORMAL), rc=1 error. The script was
# piping `bao status | python3` under `set -o pipefail`, which propagated
# rc=2 as PIPELINE FAILURE, triggering the `|| echo "true"` (lowercase)
# fallback REGARDLESS of stdout JSON. The lowercase string then failed the
# `== "True"` (Python-capitalized) compare → unseal loop SILENTLY SKIPPED
# on every fresh init. Vault stayed sealed; operator manually invoked
# `bao operator unseal $SHARD x3` as workaround.
#
# Sponsor mandate 2026-06-15 "sem WORKAROUNDs": fix canonical means
# capture stdout first, parse rc-independent, normalize the bool string.
#
# This test asserts the canonical fix is present and stays present.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TARGET_SCRIPT="$REPO_ROOT/scripts/local/vault-bootstrap-dev.sh"

[[ -f "$TARGET_SCRIPT" ]] || { echo "FATAL: target script not found at $TARGET_SCRIPT"; exit 2; }

PASS=0
FAIL=0

assert_grep() {
  local desc="$1" pattern="$2"
  if grep -qE "$pattern" "$TARGET_SCRIPT"; then
    echo "PASS [$desc]"
    PASS=$((PASS + 1))
  else
    echo "FAIL [$desc]: pattern not matched: $pattern"
    FAIL=$((FAIL + 1))
  fi
}

assert_no_grep() {
  local desc="$1" pattern="$2"
  if grep -qE "$pattern" "$TARGET_SCRIPT"; then
    echo "FAIL [$desc]: forbidden anti-pattern matched: $pattern"
    FAIL=$((FAIL + 1))
  else
    echo "PASS [$desc]"
    PASS=$((PASS + 1))
  fi
}

# --- Section A: Bash syntax sanity ---
if bash -n "$TARGET_SCRIPT" 2>/dev/null; then
  echo "PASS [bash -n syntax]"
  PASS=$((PASS + 1))
else
  echo "FAIL [bash -n syntax]"
  FAIL=$((FAIL + 1))
fi

# --- Section B: Step 2 canonical fix is present ---
# B1: stdout captured BEFORE parse (separates pipefail rc from parse logic)
assert_grep "Step 2: stdout captured into BAO_STATUS_JSON before parse" \
  'BAO_STATUS_JSON=\$\(bao status -format=json 2>/dev/null \|\| true\)'

# B2: Parse uses captured stdout (echo), NOT direct pipe (which triggers pipefail)
assert_grep "Step 2: parse via echo \$BAO_STATUS_JSON (rc-independent)" \
  'echo "\$BAO_STATUS_JSON" \| python3'

# B3: Python parse uses sys.stdin.read() to handle empty input cleanly
assert_grep "Step 2: Python parse handles empty input via or '{}'" \
  "json.loads\\(sys.stdin.read\\(\\) or '\\{\\}'\\)"

# B4: Fallback for parse failure normalized to "False" (capitalized, matches Python bool repr)
assert_grep "Step 2: fallback echo False (capitalized — matches Python repr)" \
  'echo "False"'

# B5: Compare uses "True" (capitalized) for consistency
assert_grep "Step 2: compare against capitalized 'True'" \
  '\[\[ "\$INIT_STATUS" != "True" \]\]'

# --- Section C: Step 3 canonical fix is present ---
# C1: Step 3 also captures stdout first (same pattern as Step 2)
# Count: BAO_STATUS_JSON= must appear at LEAST 2x (once per Step 2, once per Step 3).
# Allow leading whitespace — T2.1 of issue #111 wrapped Step 2 in an
# `if [[ "$AUTO_UNSEAL_MODE" != "transit" ]]` block which indents the
# capture. Intent (canonical capture-first pattern) is unchanged.
count=$(grep -cE '^[[:space:]]*BAO_STATUS_JSON=\$\(bao status -format=json' "$TARGET_SCRIPT")
if [[ "$count" -ge 2 ]]; then
  echo "PASS [Step 3: BAO_STATUS_JSON capture pattern present (count=$count)]"
  PASS=$((PASS + 1))
else
  echo "FAIL [Step 3: expected ≥2 BAO_STATUS_JSON captures, got $count]"
  FAIL=$((FAIL + 1))
fi

# C2: SEALED parse uses captured stdout
assert_grep "Step 3: SEALED parsed via echo \$BAO_STATUS_JSON" \
  'SEALED=\$\(echo "\$BAO_STATUS_JSON" \| python3'

# C3: SEALED fallback also capitalized "True"
assert_grep "Step 3: SEALED fallback 'True' capitalized" \
  'echo "True"'

# C4: Compare uses capitalized "True"
assert_grep "Step 3: compare against capitalized 'True'" \
  '\[\[ "\$SEALED" == "True" \]\]'

# --- Section D: Anti-pattern guard ---
# D1: Forbid the ORIGINAL buggy pattern (direct pipe `bao status | python3`)
#     The bug pattern was: `bao status -format=json 2>/dev/null | python3 ...`
#     Detect: bao status -format=json followed by a pipe on the same line.
#     Use Python (no PCRE in BSD grep) to scan the file line-by-line.
if python3 -c '
import re, sys
buggy = re.compile(r"^[^#]*bao status -format=json 2>/dev/null \| python3")
target = open(sys.argv[1]).read().splitlines()
hits = [l for l in target if buggy.search(l)]
sys.exit(1 if hits else 0)
' "$TARGET_SCRIPT"; then
  echo "PASS [anti-pattern: buggy direct-pipe parse absent]"
  PASS=$((PASS + 1))
else
  echo "FAIL [anti-pattern: buggy direct-pipe parse STILL PRESENT — regression!]"
  FAIL=$((FAIL + 1))
fi

# D2: Forbid lowercase "true" fallback (the original silent-skip trap)
#     Compare line MUST be capitalized "True" — lowercase would silently
#     mismatch the Python repr `True`.
#     Check: `|| echo "true"` (lowercase) used in the status parse paths must NOT exist.
#     Look for the lowercase fallback specifically in lines that ALSO contain `bao status` or `INIT_STATUS` or `SEALED`.
if python3 -c '
import sys
target = open(sys.argv[1]).read().splitlines()
hits = []
for i, line in enumerate(target):
    stripped = line.strip()
    if stripped.startswith("#"):
        continue
    # Lowercase fallback on parse paths only
    if "||" in line and "echo \"true\"" in line and ("INIT_STATUS=" in line or "SEALED=" in line):
        hits.append(f"{i+1}:{line}")
sys.exit(1 if hits else 0)
' "$TARGET_SCRIPT"; then
  echo "PASS [anti-pattern: lowercase \"true\" fallback absent on parse paths]"
  PASS=$((PASS + 1))
else
  echo "FAIL [anti-pattern: lowercase \"true\" fallback STILL PRESENT — silent-skip regression!]"
  FAIL=$((FAIL + 1))
fi

# --- Section E: Audit trail (canonical fix is documented) ---
# E1: Comment cites canonical fix + the exit-code semantics root cause
#     (grep `.*` doesn't span newlines, so split into 2 line-level asserts)
assert_grep "Step 2 fix audit trail: CANONICAL FIX 2026-06-15 marker present" \
  'CANONICAL FIX 2026-06-15'
assert_grep "Step 2 fix audit trail: exit-code SEMANTICS rationale present" \
  'exit-code SEMANTICS'

# E2: Step 3 comment cites pipefail+rc=2 (the specific bug class)
assert_grep "Step 3 fix audit trail: pipefail+bao rc=2 rationale present" \
  'pipefail.*bao rc=2'

# E3: Sponsor mandate "sem WORKAROUNDs" cited
assert_grep "sponsor mandate 'sem WORKAROUNDs' cited in fix audit trail" \
  'sem WORKAROUNDs'

# --- Verdict ---
echo ""
echo "════════════════════════════════════════════════════════"
echo "  vault-bootstrap-unseal-loop aggregate: $PASS PASS / $FAIL FAIL"
echo "════════════════════════════════════════════════════════"

[[ "$FAIL" -eq 0 ]]
