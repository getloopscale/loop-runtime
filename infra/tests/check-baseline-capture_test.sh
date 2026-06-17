#!/usr/bin/env bash
# check-baseline-capture_test.sh
#
# T1.1 hermetic test for `infra/scripts/local/baseline-capture.sh` of plan
# 2026-06-08-pre-release-test-rigor v1.2.
#
# Mock-fixture mode (BASELINE_FIXTURE_DIR env var) bypasses kubectl/curl and
# reads from a fixture directory. Asserts the script produces JSON with the
# 4 mandatory top-level layer keys and shape.
#
# Hermetic: NO network, NO kubectl.
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
CAPTURE="${REPO_ROOT}/infra/scripts/local/baseline-capture.sh"
ALLOWLIST="${REPO_ROOT}/.claude/rules/shell-scripts-allowlist.md"
FIX_DIR="$(mktemp -d /tmp/baseline-capture-fixtures.XXXXXX)"
OUT_DIR="$(mktemp -d /tmp/baseline-capture-out.XXXXXX)"
trap 'rm -rf "$FIX_DIR" "$OUT_DIR"' EXIT INT TERM

PASS=0
FAIL=0

assert() {
  local msg=$1 ; shift
  if "$@" > /dev/null 2>&1 ; then
    printf '[PASS] %s\n' "$msg" ; PASS=$((PASS + 1))
  else
    printf '[FAIL] %s\n' "$msg" ; FAIL=$((FAIL + 1))
  fi
}

assert_grep() {
  local msg=$1 file=$2 pattern=$3
  if grep -qE -- "$pattern" "$file" 2>/dev/null ; then
    printf '[PASS] %s\n' "$msg" ; PASS=$((PASS + 1))
  else
    printf '[FAIL] %s (pattern: %s)\n' "$msg" "$pattern"
    FAIL=$((FAIL + 1))
  fi
}

# --- Fixture setup ----------------------------------------------------------

# Layer 1 — code (the script reads git directly when no fixture; fixture mode
# supplies a code_state.json file with predetermined values).
cat > "$FIX_DIR/code_state.json" <<'JSON'
{ "commit": "abc1234fixture", "branch": "main", "ahead": 0, "behind": 0, "working_tree_clean": true }
JSON

# Layer 2 — runtime (mock pods + helm + argocd + tenants)
cat > "$FIX_DIR/pods.json" <<'JSON'
{ "items": [
  { "metadata": { "name": "loopzero-api-0", "namespace": "loopzero-system" }, "status": { "phase": "Running" } }
] }
JSON
cat > "$FIX_DIR/helm-list.json" <<'JSON'
[{ "name": "loopzero-api", "namespace": "loopzero-system", "chart": "loopzero-api-1.0.0" }]
JSON
cat > "$FIX_DIR/argocd-apps.json" <<'JSON'
{ "items": [
  { "metadata": { "name": "test-app" }, "status": { "sync": { "status": "Synced" }, "health": { "status": "Healthy" } } }
] }
JSON
cat > "$FIX_DIR/tenants.json" <<'JSON'
{ "items": [
  { "metadata": { "name": "tn-fixture" }, "status": { "phase": "Active" }, "spec": { "plan": "starter" } }
] }
JSON

# Layer 3 — config
cat > "$FIX_DIR/pg_max_migration.txt" <<'TXT'
28
TXT
cat > "$FIX_DIR/plan_limits_present.txt" <<'TXT'
true
TXT

# Layer 4 — live probes (mock HTTP codes)
cat > "$FIX_DIR/probes.json" <<'JSON'
{
  "https_app_root": 200,
  "https_app_projects": 200,
  "https_app_envs": 200,
  "https_cloud_api_projects": 200,
  "https_app_health": 200,
  "https_cloud_webhook": 401
}
JSON

# --- Script existence + syntax ----------------------------------------------

assert "capture script exists"     test -f "$CAPTURE"
assert "capture script executable" test -x "$CAPTURE"
assert "capture script bash -n ok" bash -n "$CAPTURE"

# --- De-costume gate (home-unhomed-logic-suspect-keeps T1.1) ----------------
# The Python data-processing must live in baseline_capture.py, NOT an inline
# heredoc (the unhomed-logic suspect-keep gate forbids python3 heredocs in
# KEEP-tagged scripts). Assert zero heredocs remain + the helper compiles.
NO_HEREDOC() { [ "$(grep -cE 'python3?[[:space:]]+(-[[:space:]]+)?<<|<<[[:space:]]*['\''\"]*?(PY|PYEOF|PYTHON)' "$CAPTURE")" = "0" ] ; }
assert "no python3 heredoc remains in baseline-capture.sh" NO_HEREDOC
assert "baseline_capture.py compiles" python3 -m py_compile "${REPO_ROOT}/infra/scripts/local/baseline_capture.py"

# --- Run capture with fixture ----------------------------------------------

OUT="$OUT_DIR/snap.json"
if BASELINE_FIXTURE_DIR="$FIX_DIR" bash "$CAPTURE" --output "$OUT" > "$OUT_DIR/stdout.log" 2>&1; then RC=0; else RC=$?; fi

assert "fixture run exits 0" test "$RC" = "0"
assert "output file exists" test -f "$OUT"

# --- JSON structural assertions ---------------------------------------------

JQ_HAS() {
  python3 -c "
import json, sys
d = json.load(open('$OUT'))
keys = '$1'.split('.')
cur = d
for k in keys:
    if not isinstance(cur, dict) or k not in cur:
        sys.exit(1)
    cur = cur[k]
sys.exit(0)
"
}

assert "JSON has captured_at field"            JQ_HAS "captured_at"
assert "JSON has code_state layer"             JQ_HAS "code_state"
assert "JSON has runtime_state layer"          JQ_HAS "runtime_state"
assert "JSON has config_state layer"           JQ_HAS "config_state"
assert "JSON has live_probes layer"            JQ_HAS "live_probes"
assert "code_state.commit populated"           JQ_HAS "code_state.commit"
assert "runtime_state.tenants populated"       JQ_HAS "runtime_state.tenants"
assert "config_state.pg_max_migration"         JQ_HAS "config_state.pg_max_migration"
assert "live_probes has https_app_root"        JQ_HAS "live_probes.https_app_root"

# --- Volatile-field normalization (EC-7) ------------------------------------

# Capture must strip volatile fields from cluster JSON before serializing —
# ensures repeated captures of an identical state produce byte-identical JSON
# (modulo captured_at). The capture script should expose a deterministic mode
# (DETERMINISTIC=1) that also zeroes the timestamp for diff-friendliness.

DETERMINISTIC=1 BASELINE_FIXTURE_DIR="$FIX_DIR" bash "$CAPTURE" --output "$OUT_DIR/snap-1.json" > /dev/null 2>&1
DETERMINISTIC=1 BASELINE_FIXTURE_DIR="$FIX_DIR" bash "$CAPTURE" --output "$OUT_DIR/snap-2.json" > /dev/null 2>&1

if [ -s "$OUT_DIR/snap-1.json" ] && [ -s "$OUT_DIR/snap-2.json" ] ; then
  if diff -q "$OUT_DIR/snap-1.json" "$OUT_DIR/snap-2.json" > /dev/null 2>&1 ; then
    printf '[PASS] DETERMINISTIC mode: 2 captures byte-identical\n'
    PASS=$((PASS + 1))
  else
    printf '[FAIL] DETERMINISTIC mode: 2 captures differ\n'
    FAIL=$((FAIL + 1))
  fi
else
  printf '[FAIL] DETERMINISTIC captures did not produce outputs\n'
  FAIL=$((FAIL + 1))
fi

# --- Shebang + strict mode --------------------------------------------------

assert_grep "shebang is bash"                "$CAPTURE" '^#!/usr/bin/env bash'
assert_grep "strict mode set -euo pipefail"  "$CAPTURE" '^set -euo pipefail'
assert_grep "BASELINE_FIXTURE_DIR honored"   "$CAPTURE" 'BASELINE_FIXTURE_DIR'
assert_grep "--output flag declared"         "$CAPTURE" '\-\-output'

# --- Allowlist cross-check -------------------------------------------------

assert "allowlist file exists" test -f "$ALLOWLIST"
assert_grep "(D5) baseline-capture.sh allowlisted" \
  "$ALLOWLIST" 'baseline-capture\.sh'
assert_grep "(D5) check-baseline-capture_test.sh allowlisted" \
  "$ALLOWLIST" 'check-baseline-capture_test\.sh'

# --- Summary ----------------------------------------------------------------

printf '\n=== %s ===\n' "$(basename "$0")"
printf 'Asserts: %d / %d PASS\n' "$PASS" "$((PASS + FAIL))"
exit $FAIL
