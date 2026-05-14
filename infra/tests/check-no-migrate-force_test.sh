#!/usr/bin/env bash
# infra/tests/check-no-migrate-force_test.sh
#
# Hermetic tests for infra/scripts/check-no-migrate-force.sh (plan T1.2).
# Covers 13 cases including 3 from edge-case-plan review:
#   EC-2: broader file-extension scope (.go/.py/.json) + Go-style `// allowlist:`
#   EC-5: multi-line YAML block scalar with `migrate force`
#
# Run: bash infra/tests/check-no-migrate-force_test.sh
# SPDX-License-Identifier: Apache-2.0
#
# shellcheck disable=SC2329  # test_* functions are invoked dynamically via `declare -F` dispatch
# shellcheck disable=SC2155  # local var=$(...) is intentional; tempdir creation is allowed to fail loudly via set -u
# shellcheck disable=SC2064  # trap RETURN is GLOBAL in bash (no set -T) — when test function returns, trap fires in run_test's scope where local $root is out-of-scope. Double-quote expansion at trap-set time captures the literal path so cleanup works correctly.

set -uo pipefail

SCRIPT="$(cd "$(dirname "$0")/.." && pwd)/scripts/check-no-migrate-force.sh"

PASS=0
FAIL=0
FAILED_NAMES=()

run_test() {
  local name=$1
  if "$name"; then
    printf '  PASS  %s\n' "$name"
    PASS=$((PASS + 1))
  else
    printf '  FAIL  %s\n' "$name"
    FAIL=$((FAIL + 1))
    FAILED_NAMES+=("$name")
  fi
}

# Helper: spin up an isolated tempdir as a fake "repo root"
make_tmp_root() {
  mktemp -d -t check-no-migrate-force.XXXXXX
}

# Helper: run the script with --root <dir>, return its exit code.
run_check() {
  local root=$1
  "$SCRIPT" --root "$root" >/dev/null 2>&1
  echo $?
}

# ---------------------------------------------------------------------------
# T1.2 base tests
# ---------------------------------------------------------------------------

test_bare_migrate_force_in_prod_blocks() {
  local root=$(make_tmp_root)
  trap "rm -rf '$root'" RETURN
  echo 'echo "migrate force 5"' > "$root/run.sh"
  [[ "$(run_check "$root")" == "2" ]]
}

test_commented_migrate_force_blocks() {
  local root=$(make_tmp_root)
  trap "rm -rf '$root'" RETURN
  # Just a comment mentioning the phrase (no allowlist marker) — blocks.
  echo '# Documentation: do not migrate force in production' > "$root/run.sh"
  [[ "$(run_check "$root")" == "2" ]]
}

test_allowlist_marker_1_line_above_passes() {
  local root=$(make_tmp_root)
  trap "rm -rf '$root'" RETURN
  cat > "$root/heal.sh" <<'EOF'
# allowlist: migrate force (heal recovery)
echo "Run: migrate force 5"
EOF
  [[ "$(run_check "$root")" == "0" ]]
}

test_allowlist_marker_5_lines_above_blocks() {
  local root=$(make_tmp_root)
  trap "rm -rf '$root'" RETURN
  cat > "$root/heal.sh" <<'EOF'
# allowlist: migrate force (out of range — 5 lines)
echo "filler1"
echo "filler2"
echo "filler3"
echo "filler4"
echo "Run: migrate force 5"
EOF
  [[ "$(run_check "$root")" == "2" ]]
}

test_empty_input_passes() {
  local root=$(make_tmp_root)
  trap "rm -rf '$root'" RETURN
  echo 'echo hello' > "$root/clean.sh"
  [[ "$(run_check "$root")" == "0" ]]
}

test_mixed_allowlisted_and_unmarked_blocks() {
  local root=$(make_tmp_root)
  trap "rm -rf '$root'" RETURN
  cat > "$root/heal.sh" <<'EOF'
# allowlist: migrate force (this one is OK)
echo "Run: migrate force 5"
EOF
  cat > "$root/bad.sh" <<'EOF'
# Just docs without marker
echo "migrate force 6"
EOF
  [[ "$(run_check "$root")" == "2" ]]
}

test_case_insensitive_matching() {
  local root=$(make_tmp_root)
  trap "rm -rf '$root'" RETURN
  echo 'echo "MIGRATE FORCE 5"' > "$root/loud.sh"
  [[ "$(run_check "$root")" == "2" ]]
}

test_pattern_inside_quoted_string_blocks() {
  local root=$(make_tmp_root)
  trap "rm -rf '$root'" RETURN
  echo 'CMD="migrate force 5"' > "$root/cmd.sh"
  [[ "$(run_check "$root")" == "2" ]]
}

test_allowlist_marker_same_line_passes() {
  local root=$(make_tmp_root)
  trap "rm -rf '$root'" RETURN
  echo 'echo "Run migrate force"  # allowlist: migrate force (inline)' > "$root/heal.sh"
  [[ "$(run_check "$root")" == "0" ]]
}

test_excluded_paths_skipped() {
  local root=$(make_tmp_root)
  trap "rm -rf '$root'" RETURN
  mkdir -p "$root/node_modules" "$root/.git" "$root/docs"
  echo 'echo "migrate force 5"' > "$root/node_modules/x.sh"
  echo 'echo "migrate force 5"' > "$root/.git/y.sh"
  echo 'echo "migrate force 5"' > "$root/docs/z.md"
  [[ "$(run_check "$root")" == "0" ]]
}

# ---------------------------------------------------------------------------
# EC-2 (edge-case-plan): broader file-extension scope
# ---------------------------------------------------------------------------

test_scans_go_python_json_files_also() {
  local root=$(make_tmp_root)
  trap "rm -rf '$root'" RETURN
  echo 'const cmd = "migrate force 5"' > "$root/main.go"
  echo 'cmd = "migrate force 5"' > "$root/script.py"
  echo '{"cmd": "migrate force 5"}' > "$root/config.json"
  [[ "$(run_check "$root")" == "2" ]]
}

test_go_style_allowlist_comment_passes() {
  local root=$(make_tmp_root)
  trap "rm -rf '$root'" RETURN
  cat > "$root/heal.go" <<'EOF'
// allowlist: migrate force (heal recovery — see runMigrations)
const cmd = "migrate force 5"
EOF
  [[ "$(run_check "$root")" == "0" ]]
}

# ---------------------------------------------------------------------------
# EC-5 (edge-case-plan): YAML block scalar with migrate force
# ---------------------------------------------------------------------------

test_yaml_block_scalar_with_migrate_force_blocks() {
  local root=$(make_tmp_root)
  trap "rm -rf '$root'" RETURN
  cat > "$root/workflow.yml" <<'EOF'
description: |
  This step runs:
    migrate force 5
  to register the existing DB.
EOF
  [[ "$(run_check "$root")" == "2" ]]
}

# ---------------------------------------------------------------------------
# Run all tests
# ---------------------------------------------------------------------------

echo "check-no-migrate-force.sh — test suite"
echo "script: $SCRIPT"
echo ""

run_test test_bare_migrate_force_in_prod_blocks
run_test test_commented_migrate_force_blocks
run_test test_allowlist_marker_1_line_above_passes
run_test test_allowlist_marker_5_lines_above_blocks
run_test test_empty_input_passes
run_test test_mixed_allowlisted_and_unmarked_blocks
run_test test_case_insensitive_matching
run_test test_pattern_inside_quoted_string_blocks
run_test test_allowlist_marker_same_line_passes
run_test test_excluded_paths_skipped
run_test test_scans_go_python_json_files_also
run_test test_go_style_allowlist_comment_passes
run_test test_yaml_block_scalar_with_migrate_force_blocks

printf '\n%d/%d tests passed\n' "$PASS" "$((PASS + FAIL))"

if [[ "$FAIL" -gt 0 ]]; then
  printf 'Failed tests:\n'
  printf '  %s\n' "${FAILED_NAMES[@]}"
  exit 1
fi
exit 0
