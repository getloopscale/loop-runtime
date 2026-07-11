#!/usr/bin/env bash
# check-migrate-legacy-namespaces_test.sh — Hermetic test for T5.1 of plan
# 2026-06-09-m2-operator-canonical-fix.
#
# Validates that migrate-legacy-namespaces.sh implements:
#   - --mode={delete|adopt} dispatch (ADR D3)
#   - --include-pvcs=false default + --pv-rebind-strategy gate (ADR D6 / EC-6)
#   - Active-release psql precondition gate (EC-7 / R6)
#   - Backwards compat: --execute aliases to --mode=delete
#   - ArgoCD AppProject patch precedes namespace delete in --mode=adopt (EC-15)
#   - Idempotent: multiple legacy envs iteration (EC-14)
#   - Bash syntax sanity (bash -n)
#
# All tests use mock kubectl + psql via PATH override (zero live cluster
# dependency). Asserts run in PASS/FAIL counters; final exit code reflects
# any failure.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TARGET_SCRIPT="$REPO_ROOT/scripts/local/migrate-legacy-namespaces.sh"

[[ -f "$TARGET_SCRIPT" ]] || { echo "FATAL: script not found at $TARGET_SCRIPT" >&2; exit 2; }

PASS=0
FAIL=0
FAILED_CASES=()

assert() {
  local name="$1"; local actual="$2"; local expected="$3"
  if [[ "$actual" == "$expected" ]]; then
    PASS=$((PASS+1))
    printf "  PASS  %s\n" "$name"
  else
    FAIL=$((FAIL+1))
    FAILED_CASES+=("$name (got $actual, want $expected)")
    printf "  FAIL  %s — got %q, want %q\n" "$name" "$actual" "$expected"
  fi
}

assert_contains() {
  local name="$1"; local haystack="$2"; local needle="$3"
  if [[ "$haystack" == *"$needle"* ]]; then
    PASS=$((PASS+1))
    printf "  PASS  %s\n" "$name"
  else
    FAIL=$((FAIL+1))
    FAILED_CASES+=("$name (needle %q not in haystack)")
    printf "  FAIL  %s — needle %q not in haystack\n" "$name" "$needle"
  fi
}

# Mock-binary harness: each test sets up a fresh tempdir with mock kubectl + psql
# whose behavior is fixture-driven via env vars.
make_mock_env() {
  local tmp; tmp="$(mktemp -d)"
  cat > "$tmp/kubectl" <<'MOCK_KUBECTL'
#!/usr/bin/env bash
# Fixture-driven mock kubectl. Behavior tuned via MOCK_NS_LIST, MOCK_PVC_LIST,
# MOCK_KUBECTL_FAIL.
case "$*" in
  *"SELECT COUNT"*|*"FROM releases"*)
    # EC-7 active-release gate query
    printf '%s\n' "${MOCK_PSQL_ACTIVE_RELEASE_COUNT:-0}"
    exit 0
    ;;
  *"get ns"*)
    if [[ -n "${MOCK_NS_LIST:-}" ]]; then
      printf '%s\n' "$MOCK_NS_LIST"
      exit 0
    fi
    exit 0
    ;;
  *"FROM projects"*)
    printf '%s\n' "${MOCK_PROJECTS_MAP:-tn-test1|teste}"
    exit 0
    ;;
  *"exec -n loopzero-system postgresql-0"*)
    # PG project lookup (matches lookup_projects)
    printf '%s\n' "${MOCK_PROJECTS_MAP:-tn-test1|teste}"
    exit 0
    ;;
  *"get pvc"*)
    printf '%s\n' "${MOCK_PVC_LIST:-}"
    exit 0
    ;;
  *"patch appproject"*)
    echo "appproject patched (mock)"
    [[ -n "${MOCK_KUBECTL_PATCH_FAIL:-}" ]] && exit 1
    # Append the call to the trace so test can assert ordering
    echo "PATCH_APPPROJECT" >> "${MOCK_CALL_TRACE:-/dev/null}"
    exit 0
    ;;
  *"delete ns"*)
    echo "DELETE_NS" >> "${MOCK_CALL_TRACE:-/dev/null}"
    [[ -n "${MOCK_KUBECTL_DELETE_FAIL:-}" ]] && exit 1
    echo "ns deleted (mock)"
    exit 0
    ;;
  *"get all"*)
    echo "no resources (mock)"
    exit 0
    ;;
  *)
    exit 0
    ;;
esac
MOCK_KUBECTL
  chmod +x "$tmp/kubectl"

  # psql mock: by default returns 0 rows (no active release). Override via
  # MOCK_PSQL_ACTIVE_RELEASE_COUNT to simulate active releases.
  cat > "$tmp/psql" <<'MOCK_PSQL'
#!/usr/bin/env bash
case "$*" in
  *"SELECT COUNT"*|*"COUNT(*)"*|*"count(*)"*)
    printf '%s\n' "${MOCK_PSQL_ACTIVE_RELEASE_COUNT:-0}"
    exit 0
    ;;
  *"SELECT status FROM releases"*|*"FROM releases"*)
    # Return one row per simulated active release
    local n="${MOCK_PSQL_ACTIVE_RELEASE_COUNT:-0}"
    for ((i=0; i<n; i++)); do
      printf 'PENDING\n'
    done
    exit 0
    ;;
  *"SELECT tenant_id"*|*"FROM projects"*)
    printf '%s\n' "${MOCK_PROJECTS_MAP:-tn-test1|teste}"
    exit 0
    ;;
  *)
    exit 0
    ;;
esac
MOCK_PSQL
  chmod +x "$tmp/psql"

  echo "$tmp"
}

# Run script with mock env. $1 = test name. $@... = script args.
run_target() {
  local name="$1"; shift
  local mock; mock="$(make_mock_env)"
  trap "rm -rf '$mock'" RETURN

  local trace; trace="$(mktemp)"
  local out
  out="$(MOCK_CALL_TRACE="$trace" \
         MOCK_NS_LIST="${MOCK_NS_LIST:-}" \
         MOCK_PROJECTS_MAP="${MOCK_PROJECTS_MAP:-tn-test1|teste}" \
         MOCK_PSQL_ACTIVE_RELEASE_COUNT="${MOCK_PSQL_ACTIVE_RELEASE_COUNT:-0}" \
         MOCK_PVC_LIST="${MOCK_PVC_LIST:-}" \
         PATH="$mock:$PATH" \
         bash "$TARGET_SCRIPT" "$@" 2>&1)"
  local ec=$?
  rm -f "$trace"
  printf '%s\n' "$out"
  return "$ec"
}

echo "=== check-migrate-legacy-namespaces_test.sh ==="
echo

# T1 — bash syntax sanity
if bash -n "$TARGET_SCRIPT" 2>/dev/null; then
  assert "T1_bash_syntax" "ok" "ok"
else
  assert "T1_bash_syntax" "syntax-error" "ok"
fi

# T2 — --help exits 0
HELP_OUT="$(bash "$TARGET_SCRIPT" --help 2>&1)"
HELP_EC=$?
assert "T2_help_exits_zero" "$HELP_EC" "0"
assert_contains "T2b_help_mentions_mode" "$HELP_OUT" "--mode"

# T3 — unknown flag exits 1
UNKNOWN_OUT="$(bash "$TARGET_SCRIPT" --bogus-flag 2>&1)"
UNKNOWN_EC=$?
assert "T3_unknown_flag_exits_one" "$UNKNOWN_EC" "1"

# T4 — backwards compat: --execute still works (aliased to --mode=delete)
out="$(MOCK_NS_LIST="loopzero-tn-test1-production" run_target "T4" --execute --dry-run)"
ec=$?
# Both --execute and --dry-run set — dry-run wins
assert "T4_execute_dry_run_exit_zero" "$ec" "0"

# T5 — --mode=delete --dry-run lists planned deletions, no actual delete
trace_file="$(mktemp)"
out="$(MOCK_CALL_TRACE="$trace_file" \
       MOCK_NS_LIST="loopzero-tn-test1-production" \
       MOCK_PROJECTS_MAP="tn-test1|teste" \
       PATH="$(make_mock_env):$PATH" \
       bash "$TARGET_SCRIPT" --mode=delete --dry-run 2>&1)"
ec=$?
trace_content="$(cat "$trace_file" 2>/dev/null || echo "")"
rm -f "$trace_file"
assert "T5_mode_delete_dry_run_exit_zero" "$ec" "0"
DELETE_CALLS="$(echo "$trace_content" | grep -c "DELETE_NS" || true)"
assert "T5b_mode_delete_dry_run_no_actual_delete" "$DELETE_CALLS" "0"

# T6 — EC-7 active-release precondition BLOCKS --mode=adopt (status=PENDING)
out="$(MOCK_NS_LIST="loopzero-tn-test1-production" \
       MOCK_PROJECTS_MAP="tn-test1|teste" \
       MOCK_PSQL_ACTIVE_RELEASE_COUNT=1 \
       PATH="$(make_mock_env):$PATH" \
       bash "$TARGET_SCRIPT" --mode=adopt --tenant tn-test1 2>&1)"
ec=$?
assert "T6_active_release_blocks_adopt_exit_2" "$ec" "2"
assert_contains "T6b_active_release_message" "$out" "active release"

# T7 — EC-7: 0 active releases → --mode=adopt --dry-run proceeds (exit 0)
out="$(MOCK_NS_LIST="loopzero-tn-test1-production" \
       MOCK_PROJECTS_MAP="tn-test1|teste" \
       MOCK_PSQL_ACTIVE_RELEASE_COUNT=0 \
       PATH="$(make_mock_env):$PATH" \
       bash "$TARGET_SCRIPT" --mode=adopt --tenant tn-test1 --dry-run 2>&1)"
ec=$?
assert "T7_no_active_release_adopt_dry_run_exit_zero" "$ec" "0"

# T8 — EC-6: --mode=adopt --include-pvcs=true WITHOUT --pv-rebind-strategy → exit 1
out="$(MOCK_NS_LIST="loopzero-tn-test1-production" \
       MOCK_PROJECTS_MAP="tn-test1|teste" \
       PATH="$(make_mock_env):$PATH" \
       bash "$TARGET_SCRIPT" --mode=adopt --include-pvcs=true --tenant tn-test1 2>&1)"
ec=$?
assert "T8_include_pvcs_requires_rebind_strategy_exit_1" "$ec" "1"
assert_contains "T8b_pvcs_strategy_message" "$out" "pv-rebind-strategy"

# T9 — EC-6: --mode=adopt --include-pvcs=true --pv-rebind-strategy=retain accepted
out="$(MOCK_NS_LIST="loopzero-tn-test1-production" \
       MOCK_PROJECTS_MAP="tn-test1|teste" \
       PATH="$(make_mock_env):$PATH" \
       bash "$TARGET_SCRIPT" --mode=adopt --include-pvcs=true --pv-rebind-strategy=retain --tenant tn-test1 --dry-run 2>&1)"
ec=$?
assert "T9_include_pvcs_with_strategy_dry_run_exit_zero" "$ec" "0"

# T10 — EC-6: --mode=adopt without --include-pvcs but PVCs detected → WARNING + continue
out="$(MOCK_NS_LIST="loopzero-tn-test1-production" \
       MOCK_PROJECTS_MAP="tn-test1|teste" \
       MOCK_PVC_LIST="data-pvc" \
       PATH="$(make_mock_env):$PATH" \
       bash "$TARGET_SCRIPT" --mode=adopt --tenant tn-test1 --dry-run 2>&1)"
ec=$?
assert "T10_pvcs_skipped_warning_continues_exit_zero" "$ec" "0"
assert_contains "T10b_pvcs_skipped_warning" "$out" "PVC"

# T11 — Invalid --mode value rejected
out="$(MOCK_NS_LIST="" \
       MOCK_PROJECTS_MAP="tn-test1|teste" \
       PATH="$(make_mock_env):$PATH" \
       bash "$TARGET_SCRIPT" --mode=bogus 2>&1)"
ec=$?
assert "T11_invalid_mode_exits_1" "$ec" "1"

# T12 — --json output is valid JSON shape (top-level keys)
out="$(MOCK_NS_LIST="loopzero-tn-test1-production" \
       MOCK_PROJECTS_MAP="tn-test1|teste" \
       PATH="$(make_mock_env):$PATH" \
       bash "$TARGET_SCRIPT" --json 2>&1)"
ec=$?
assert "T12_json_exit_zero" "$ec" "0"
assert_contains "T12b_json_canonical_key" "$out" '"canonical"'
assert_contains "T12c_json_legacy_key" "$out" '"legacy"'

# T13 — EC-15: --mode=adopt patches AppProject BEFORE delete ns
trace_file="$(mktemp)"
out="$(MOCK_CALL_TRACE="$trace_file" \
       MOCK_NS_LIST="loopzero-tn-test1-production" \
       MOCK_PROJECTS_MAP="tn-test1|teste" \
       MOCK_PSQL_ACTIVE_RELEASE_COUNT=0 \
       PATH="$(make_mock_env):$PATH" \
       bash "$TARGET_SCRIPT" --mode=adopt --tenant tn-test1 2>&1)"
ec=$?
trace_content="$(cat "$trace_file" 2>/dev/null || echo "")"
rm -f "$trace_file"
# First non-empty line should be PATCH_APPPROJECT; DELETE_NS after.
FIRST_CALL="$(echo "$trace_content" | head -1)"
LAST_CALL="$(echo "$trace_content" | tail -1)"
# Both must be present AND patch must precede delete
if echo "$trace_content" | grep -q "PATCH_APPPROJECT" && echo "$trace_content" | grep -q "DELETE_NS"; then
  PATCH_LINE="$(echo "$trace_content" | grep -n "PATCH_APPPROJECT" | head -1 | cut -d: -f1)"
  DELETE_LINE="$(echo "$trace_content" | grep -n "DELETE_NS" | head -1 | cut -d: -f1)"
  if [[ "$PATCH_LINE" -lt "$DELETE_LINE" ]]; then
    assert "T13_appproject_patch_before_delete" "ok" "ok"
  else
    assert "T13_appproject_patch_before_delete" "delete_first" "ok"
  fi
else
  assert "T13_appproject_patch_before_delete" "missing_calls" "ok"
fi

# T14 — EC-14: multiple legacy envs iterated
trace_file="$(mktemp)"
out="$(MOCK_CALL_TRACE="$trace_file" \
       MOCK_NS_LIST=$'loopzero-tn-test1-staging\nloopzero-tn-test1-production\nloopzero-tn-test1-dev' \
       MOCK_PROJECTS_MAP="tn-test1|teste" \
       PATH="$(make_mock_env):$PATH" \
       bash "$TARGET_SCRIPT" --mode=delete 2>&1)"
ec=$?
trace_content="$(cat "$trace_file" 2>/dev/null || echo "")"
rm -f "$trace_file"
DELETE_COUNT="$(echo "$trace_content" | grep -c "DELETE_NS" || true)"
assert "T14_multi_env_iteration_count" "$DELETE_COUNT" "3"

# T15 — Idempotent: no legacy ns → exit 0 with "no tenant namespaces found" OR "all clear"
out="$(MOCK_NS_LIST="" \
       MOCK_PROJECTS_MAP="tn-test1|teste" \
       PATH="$(make_mock_env):$PATH" \
       bash "$TARGET_SCRIPT" 2>&1)"
ec=$?
assert "T15_idempotent_empty_state_exit_zero" "$ec" "0"

# T16 — --tenant flag scopes operation (no error when tenant has no legacy ns)
out="$(MOCK_NS_LIST="loopzero-tn-other-production" \
       MOCK_PROJECTS_MAP=$'tn-test1|teste\ntn-other|other-proj' \
       PATH="$(make_mock_env):$PATH" \
       bash "$TARGET_SCRIPT" --mode=delete --tenant tn-test1 --dry-run 2>&1)"
ec=$?
assert "T16_tenant_scope_no_match_exit_zero" "$ec" "0"

# T17 — Default (no flags) is dry-run, exit 0
out="$(MOCK_NS_LIST="loopzero-tn-test1-production" \
       MOCK_PROJECTS_MAP="tn-test1|teste" \
       PATH="$(make_mock_env):$PATH" \
       bash "$TARGET_SCRIPT" 2>&1)"
ec=$?
assert "T17_default_dry_run_exit_zero" "$ec" "0"

# T18 — Help message includes EC-6, EC-7 acknowledgments
HELP_OUT="$(bash "$TARGET_SCRIPT" --help 2>&1)"
assert_contains "T18_help_mentions_pvcs" "$HELP_OUT" "include-pvcs"
assert_contains "T18b_help_mentions_release_gate" "$HELP_OUT" "active release"

# Summary
echo
echo "=== Summary ==="
echo "PASS: $PASS"
echo "FAIL: $FAIL"
if (( FAIL > 0 )); then
  echo
  echo "Failed cases:"
  printf '  - %s\n' "${FAILED_CASES[@]}"
  exit 1
fi
exit 0
