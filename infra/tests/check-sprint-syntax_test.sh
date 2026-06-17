#!/usr/bin/env bash
# check-sprint-syntax_test.sh
#
# Hermetic test for infra/scripts/local/check-sprint.sh + its homed helper
# check_sprint.py (NEW — the script had no coupled test before plan
# home-unhomed-logic-suspect-keeps T3.2).
#
# Hermetic: NO network, NO kubectl. Asserts bash -n, the de-costume invariant
# (no python3 heredoc), the helper compiles, and the JSON report builder shape.
set -u

REPO_ROOT="$(git rev-parse --show-toplevel)"
SCRIPT="${REPO_ROOT}/infra/scripts/local/check-sprint.sh"
HELPER="${REPO_ROOT}/infra/scripts/local/check_sprint.py"
ALLOWLIST="${REPO_ROOT}/.claude/rules/shell-scripts-allowlist.md"

PASS=0
FAIL=0
assert() {
  local msg=$1 ; shift
  if "$@" > /dev/null 2>&1 ; then printf '[PASS] %s\n' "$msg" ; PASS=$((PASS + 1))
  else printf '[FAIL] %s\n' "$msg" ; FAIL=$((FAIL + 1)) ; fi
}

assert "script exists"     test -f "$SCRIPT"
assert "script bash -n ok" bash -n "$SCRIPT"
assert "helper exists"     test -f "$HELPER"
assert "helper compiles"   python3 -m py_compile "$HELPER"

# --- De-costume gate (T3.2) -------------------------------------------------
NO_HEREDOC() { [ "$(grep -cE 'python3?[[:space:]]+(-[[:space:]]+)?<<|<<[[:space:]]*['\''\"]*?(PY|PYEOF|PYTHON)' "$SCRIPT")" = "0" ] ; }
assert "no python3 heredoc remains" NO_HEREDOC

# --- JSON report builder shape ----------------------------------------------
T="$(mktemp)"
printf 'c1:PASS:OK\nc3:WARN:kubectl context unreachable\n' > "$T"
OUT="$(CS_RESULTS_FILE="$T" CS_PASS=6 CS_WARN=1 CS_FAIL=0 CS_SKIP=1 CS_ELAPSED=12 python3 "$HELPER")"
rm -f "$T"
VALID_JSON() { python3 -c "import json,sys; json.loads(sys.stdin.read())" <<< "$OUT" ; }
assert "report is valid JSON" VALID_JSON
assert "report has pass count" grep -q '"pass": 6' <<< "$OUT"
assert "report preserves spaced detail" grep -q 'kubectl context unreachable' <<< "$OUT"
assert "report results is a list of [id,status,detail]" python3 -c "
import json,sys
d=json.loads('''$OUT''')
assert d['results'][0]==['c1','PASS','OK'], d['results']
"

# --- Allowlist --------------------------------------------------------------
assert "allowlist file exists" test -f "$ALLOWLIST"
assert "script allowlisted"    grep -q 'check-sprint\.sh' "$ALLOWLIST"

printf '\n=== %s ===\n' "$(basename "$0")"
printf 'Asserts: %d / %d PASS\n' "$PASS" "$((PASS + FAIL))"
exit $FAIL
