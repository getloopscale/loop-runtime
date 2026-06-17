#!/usr/bin/env bash
# check-baseline-diff_test.sh
#
# T1.1 hermetic test for `infra/scripts/local/baseline-diff.sh` of plan
# 2026-06-08-pre-release-test-rigor v1.2.
#
# 4 fixture-driven scenarios:
#   - identical: A == B → exit 0, classification verdict OK
#   - missing:   A has 5 projects, B has 4 → exit 1, classification missing N=1
#   - additive:  A has 4 projects, B has 5 → exit 0, classification additive N=1, verdict OK
#   - mismatch:  A has Tenant plan=starter, B has plan=pro → exit 1, classification mismatch N=1
#   - volatile (EC-7): A and B identical except metadata.resourceVersion +
#     status.conditions[].lastTransitionTime + metadata.managedFields +
#     metadata.generation → exit 0, verdict OK (normalization strips them)
#
# Hermetic: NO network, NO kubectl.
set -u

REPO_ROOT="$(git rev-parse --show-toplevel)"
DIFF="${REPO_ROOT}/infra/scripts/local/baseline-diff.sh"
FIXTURES="$(mktemp -d /tmp/baseline-diff-fixtures.XXXXXX)"
trap 'rm -rf "$FIXTURES"' EXIT INT TERM

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

assert_eq() {
  local msg=$1 expected=$2 actual=$3
  if [ "$expected" = "$actual" ] ; then
    printf '[PASS] %s\n' "$msg" ; PASS=$((PASS + 1))
  else
    printf '[FAIL] %s (expected=%s actual=%s)\n' "$msg" "$expected" "$actual"
    FAIL=$((FAIL + 1))
  fi
}

# --- Fixture builders -------------------------------------------------------

baseline_skel() {
  cat <<'JSON'
{
  "captured_at": "2026-06-08T12:00:00Z",
  "code_state": { "commit": "abc123", "branch": "main", "ahead": 0, "behind": 0 },
  "runtime_state": {
    "tenants": [
      { "name": "tn-a", "phase": "Active", "plan": "starter" },
      { "name": "tn-b", "phase": "Active", "plan": "starter" }
    ]
  },
  "config_state": { "pg_max_migration": 28, "cosign_mode": "keyed" },
  "live_probes": { "https_app_root": 200 }
}
JSON
}

# Scenario 1 — identical
echo "$(baseline_skel)" > "$FIXTURES/a-identical.json"
echo "$(baseline_skel)" > "$FIXTURES/b-identical.json"

# Scenario 2 — missing (A has 5 tenants, B has 4)
python3 - <<PY > "$FIXTURES/a-missing.json"
import json
d = json.loads('''$(baseline_skel)''')
d["runtime_state"]["tenants"] = [
  {"name": f"tn-{i}", "phase": "Active", "plan": "starter"} for i in range(5)
]
print(json.dumps(d))
PY
python3 - <<PY > "$FIXTURES/b-missing.json"
import json
d = json.loads('''$(baseline_skel)''')
d["runtime_state"]["tenants"] = [
  {"name": f"tn-{i}", "phase": "Active", "plan": "starter"} for i in range(4)
]
print(json.dumps(d))
PY

# Scenario 3 — additive (A has 4 tenants, B has 5)
cp "$FIXTURES/b-missing.json" "$FIXTURES/a-additive.json"
cp "$FIXTURES/a-missing.json" "$FIXTURES/b-additive.json"

# Scenario 4 — mismatch (A plan=starter, B plan=pro for the same tenant)
python3 - <<PY > "$FIXTURES/a-mismatch.json"
import json
d = json.loads('''$(baseline_skel)''')
d["runtime_state"]["tenants"] = [{"name": "tn-1", "phase": "Active", "plan": "starter"}]
print(json.dumps(d))
PY
python3 - <<PY > "$FIXTURES/b-mismatch.json"
import json
d = json.loads('''$(baseline_skel)''')
d["runtime_state"]["tenants"] = [{"name": "tn-1", "phase": "Active", "plan": "pro"}]
print(json.dumps(d))
PY

# Scenario 5 — volatile (EC-7): identical except volatile fields
python3 - <<PY > "$FIXTURES/a-volatile.json"
import json
d = json.loads('''$(baseline_skel)''')
d["runtime_state"]["tenants"] = [{
  "name": "tn-1", "phase": "Active", "plan": "starter",
  "metadata": {
    "resourceVersion": "1234",
    "managedFields": [{"manager": "kubectl"}],
    "generation": 1,
    "creationTimestamp": "2026-06-01T00:00:00Z"
  },
  "status": {"conditions": [{"type": "Ready", "lastTransitionTime": "2026-06-01T00:00:00Z"}]}
}]
print(json.dumps(d))
PY
python3 - <<PY > "$FIXTURES/b-volatile.json"
import json
d = json.loads('''$(baseline_skel)''')
d["runtime_state"]["tenants"] = [{
  "name": "tn-1", "phase": "Active", "plan": "starter",
  "metadata": {
    "resourceVersion": "9999",
    "managedFields": [{"manager": "operator"}],
    "generation": 99,
    "creationTimestamp": "2026-06-08T00:00:00Z"
  },
  "status": {"conditions": [{"type": "Ready", "lastTransitionTime": "2026-06-08T12:00:00Z"}]}
}]
print(json.dumps(d))
PY

# --- Script existence -------------------------------------------------------

assert "diff script exists"     test -f "$DIFF"
assert "diff script executable" test -x "$DIFF"
assert "diff script bash -n ok" bash -n "$DIFF"

# --- De-costume gate (home-unhomed-logic-suspect-keeps T1.2) ----------------
NO_HEREDOC_DIFF() { [ "$(grep -cE 'python3?[[:space:]]+(-[[:space:]]+)?<<|<<[[:space:]]*['\''\"]*?(PY|PYEOF|PYTHON)' "$DIFF")" = "0" ] ; }
assert "no python3 heredoc remains in baseline-diff.sh" NO_HEREDOC_DIFF
assert "baseline_diff.py compiles" python3 -m py_compile "${REPO_ROOT}/infra/scripts/local/baseline_diff.py"

# --- Scenario assertions ----------------------------------------------------

run_diff() {
  bash "$DIFF" "$1" "$2"
}

# 1. Identical → exit 0
run_diff "$FIXTURES/a-identical.json" "$FIXTURES/b-identical.json" > "$FIXTURES/r1.json" 2>&1
RC1=$?
assert_eq "identical → exit 0" "0" "$RC1"
if [ -s "$FIXTURES/r1.json" ] ; then
  V1=$(python3 -c 'import json,sys; print(json.load(open("'"$FIXTURES/r1.json"'")).get("verdict",""))' 2>/dev/null || echo "")
  assert_eq "identical → verdict OK" "OK" "$V1"
fi

# 2. Missing → exit 1, classification missing N=1
run_diff "$FIXTURES/a-missing.json" "$FIXTURES/b-missing.json" > "$FIXTURES/r2.json" 2>&1
RC2=$?
assert_eq "missing → exit 1" "1" "$RC2"
if [ -s "$FIXTURES/r2.json" ] ; then
  V2=$(python3 -c 'import json,sys; print(json.load(open("'"$FIXTURES/r2.json"'")).get("verdict",""))' 2>/dev/null || echo "")
  M2=$(python3 -c 'import json,sys; print(len(json.load(open("'"$FIXTURES/r2.json"'")).get("classification",{}).get("missing",[])))' 2>/dev/null || echo 0)
  assert_eq "missing → verdict FAIL" "FAIL" "$V2"
  assert_eq "missing → 1 missing item classified" "1" "$M2"
fi

# 3. Additive → exit 0
run_diff "$FIXTURES/a-additive.json" "$FIXTURES/b-additive.json" > "$FIXTURES/r3.json" 2>&1
RC3=$?
assert_eq "additive → exit 0" "0" "$RC3"
if [ -s "$FIXTURES/r3.json" ] ; then
  V3=$(python3 -c 'import json,sys; print(json.load(open("'"$FIXTURES/r3.json"'")).get("verdict",""))' 2>/dev/null || echo "")
  A3=$(python3 -c 'import json,sys; print(len(json.load(open("'"$FIXTURES/r3.json"'")).get("classification",{}).get("additive",[])))' 2>/dev/null || echo 0)
  assert_eq "additive → verdict OK" "OK" "$V3"
  assert_eq "additive → 1 additive item classified" "1" "$A3"
fi

# 4. Mismatch → exit 1
run_diff "$FIXTURES/a-mismatch.json" "$FIXTURES/b-mismatch.json" > "$FIXTURES/r4.json" 2>&1
RC4=$?
assert_eq "mismatch → exit 1" "1" "$RC4"
if [ -s "$FIXTURES/r4.json" ] ; then
  M4=$(python3 -c 'import json,sys; print(len(json.load(open("'"$FIXTURES/r4.json"'")).get("classification",{}).get("mismatch",[])))' 2>/dev/null || echo 0)
  V4=$(python3 -c 'import json,sys; print(json.load(open("'"$FIXTURES/r4.json"'")).get("verdict",""))' 2>/dev/null || echo "")
  assert_eq "mismatch → verdict FAIL" "FAIL" "$V4"
  assert_eq "mismatch → 1 mismatch item classified" "1" "$M4"
fi

# 5. Volatile (EC-7) → exit 0
run_diff "$FIXTURES/a-volatile.json" "$FIXTURES/b-volatile.json" > "$FIXTURES/r5.json" 2>&1
RC5=$?
assert_eq "volatile (EC-7) → exit 0" "0" "$RC5"
if [ -s "$FIXTURES/r5.json" ] ; then
  V5=$(python3 -c 'import json,sys; print(json.load(open("'"$FIXTURES/r5.json"'")).get("verdict",""))' 2>/dev/null || echo "")
  assert_eq "volatile (EC-7) → verdict OK" "OK" "$V5"
fi

# --- Summary ----------------------------------------------------------------

printf '\n=== %s ===\n' "$(basename "$0")"
printf 'Asserts: %d / %d PASS\n' "$PASS" "$((PASS + FAIL))"
exit $FAIL
