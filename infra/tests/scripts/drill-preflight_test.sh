#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Hermetic tests for drill-preflight.sh.
#
# Coverage:
#   test_all_pass_exits_0
#   test_missing_credentials_exits_1
#   test_missing_scripts_exits_2
#   test_isolation_check_fails_exits_3
#   test_leftover_stack_exits_4
#   test_unknown_flag_exits_5
#   test_json_output_format
#   test_help_flag_exits_0

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SCRIPT="$ROOT/infra/scripts/drill-preflight.sh"

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# Build hermetic environment with all 5 scripts present (touched as
# executable files) + isolation script + pulumi stub returning no stack.
build_env_all_pass() {
  local tmpdir="$1"

  mkdir -p "$tmpdir/infra/scripts" "$tmpdir/infra/pulumi-drill" "$tmpdir/bin"

  # Create executable script placeholders (drill-preflight only checks -x)
  for script in drill-state-reset drill-helmfile-bootstrap drill-cancel-refresh \
                drill-evidence-capture drill-teardown-validator \
                check-drill-credentials-isolation; do
    echo '#!/usr/bin/env bash' > "$tmpdir/infra/scripts/$script.sh"
    echo 'exit 0' >> "$tmpdir/infra/scripts/$script.sh"
    chmod +x "$tmpdir/infra/scripts/$script.sh"
  done

  # pulumi stub returning no stack
  cat > "$tmpdir/bin/pulumi" <<'EOF'
#!/usr/bin/env bash
if [[ "$*" == "stack ls --json" ]]; then
  echo '[]'
fi
exit 0
EOF
  chmod +x "$tmpdir/bin/pulumi"
}

# Run with all-pass tokens by default
run_preflight() {
  local tmpdir="$1"
  shift
  cd "$tmpdir"
  PATH="$tmpdir/bin:/usr/bin:/bin" \
    DIGITALOCEAN_TOKEN=fake \
    PULUMI_ACCESS_TOKEN=fake \
    CLOUDFLARE_API_TOKEN=fake \
    PULUMI_CONFIG_PASSPHRASE=fake \
    PULUMI_DRILL_DIR="$tmpdir/infra/pulumi-drill" \
    bash "$SCRIPT" "$@" 2>"$tmpdir/stderr.log" >"$tmpdir/stdout.log"
}

# ---- Tests ----

test_all_pass_exits_0() {
  local tmpdir="$1"
  build_env_all_pass "$tmpdir"
  set +e
  run_preflight "$tmpdir"
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || {
    echo "  expected exit 0, got $rc" >&2
    cat "$tmpdir/stderr.log" >&2
    return 1
  }
  grep -q "verdict: PASS" "$tmpdir/stderr.log" || return 1
}

test_missing_credentials_exits_1() {
  local tmpdir="$1"
  build_env_all_pass "$tmpdir"
  cd "$tmpdir"
  set +e
  # Drop DIGITALOCEAN_TOKEN explicitly (mandatory token per iter 10 backend-aware classification)
  PATH="$tmpdir/bin:/usr/bin:/bin" \
    PULUMI_ACCESS_TOKEN=fake \
    CLOUDFLARE_API_TOKEN=fake \
    PULUMI_CONFIG_PASSPHRASE=fake \
    PULUMI_DRILL_DIR="$tmpdir/infra/pulumi-drill" \
    env -u DIGITALOCEAN_TOKEN \
    bash "$SCRIPT" 2>"$tmpdir/stderr.log" >"$tmpdir/stdout.log"
  local rc=$?
  set -e
  [[ "$rc" -eq 1 ]] || return 1
  # Iter 10 wording: "Missing MANDATORY tokens: DIGITALOCEAN_TOKEN ..."
  grep -q "Missing MANDATORY tokens.*DIGITALOCEAN_TOKEN" "$tmpdir/stderr.log" || return 1
}

test_pulumi_access_token_missing_is_advisory_not_failure() {
  # Iter 10 backend-aware: S3 backend doesn't need PULUMI_ACCESS_TOKEN.
  # When only advisory token missing + mandatory all present → exit 0 (PASS).
  local tmpdir="$1"
  build_env_all_pass "$tmpdir"
  cd "$tmpdir"
  set +e
  PATH="$tmpdir/bin:/usr/bin:/bin" \
    DIGITALOCEAN_TOKEN=fake \
    CLOUDFLARE_API_TOKEN=fake \
    PULUMI_CONFIG_PASSPHRASE=fake \
    PULUMI_DRILL_DIR="$tmpdir/infra/pulumi-drill" \
    env -u PULUMI_ACCESS_TOKEN \
    bash "$SCRIPT" 2>"$tmpdir/stderr.log" >"$tmpdir/stdout.log"
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || {
    echo "  expected exit 0 (advisory missing OK with S3 backend), got $rc" >&2
    tail -10 "$tmpdir/stderr.log" >&2
    return 1
  }
  # Stderr must mention advisory missing wording (operator transparency)
  grep -q "advisory missing.*PULUMI_ACCESS_TOKEN" "$tmpdir/stderr.log" || return 1
}

test_missing_scripts_exits_2() {
  local tmpdir="$1"
  build_env_all_pass "$tmpdir"
  # Remove one of the 5 required scripts
  rm "$tmpdir/infra/scripts/drill-helmfile-bootstrap.sh"
  set +e
  run_preflight "$tmpdir"
  local rc=$?
  set -e
  [[ "$rc" -eq 2 ]] || {
    echo "  expected exit 2, got $rc" >&2
    cat "$tmpdir/stderr.log" >&2
    return 1
  }
  grep -q "Missing/non-executable.*drill-helmfile-bootstrap" "$tmpdir/stderr.log" || return 1
}

test_isolation_check_fails_exits_3() {
  local tmpdir="$1"
  build_env_all_pass "$tmpdir"
  # Make isolation script always fail (simulating credentials collision)
  cat > "$tmpdir/infra/scripts/check-drill-credentials-isolation.sh" <<'EOF'
#!/usr/bin/env bash
echo "stub: collision" >&2
exit 1
EOF
  chmod +x "$tmpdir/infra/scripts/check-drill-credentials-isolation.sh"
  set +e
  run_preflight "$tmpdir"
  local rc=$?
  set -e
  [[ "$rc" -eq 3 ]] || {
    echo "  expected exit 3 (collision), got $rc" >&2
    return 1
  }
  # Pretty-print stderr (iter 8 wording): "CATASTROPHIC: DRILL_*_TOKEN matches prod hash"
  grep -q "DRILL_\*_TOKEN matches prod hash" "$tmpdir/stderr.log" || {
    echo "  expected collision wording in stderr" >&2
    tail -10 "$tmpdir/stderr.log" >&2
    return 1
  }
}

test_leftover_stack_exits_4() {
  local tmpdir="$1"
  build_env_all_pass "$tmpdir"
  # Replace pulumi stub to return existing drill stack
  cat > "$tmpdir/bin/pulumi" <<'EOF'
#!/usr/bin/env bash
if [[ "$*" == "stack ls --json" ]]; then
  echo '[{"name":"drill","current":true}]'
fi
exit 0
EOF
  chmod +x "$tmpdir/bin/pulumi"
  set +e
  run_preflight "$tmpdir"
  local rc=$?
  set -e
  [[ "$rc" -eq 4 ]] || {
    echo "  expected exit 4 (leftover stack), got $rc" >&2
    return 1
  }
  grep -q "Leftover drill stack detected" "$tmpdir/stderr.log" || return 1
}

test_unknown_flag_exits_5() {
  local tmpdir="$1"
  build_env_all_pass "$tmpdir"
  set +e
  run_preflight "$tmpdir" --unknown-xyz
  local rc=$?
  set -e
  [[ "$rc" -eq 5 ]] || return 1
}

test_json_output_format() {
  local tmpdir="$1"
  build_env_all_pass "$tmpdir"
  set +e
  run_preflight "$tmpdir" --json
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
  # stdout should be JSON
  grep -q '"verdict":"PASS"' "$tmpdir/stdout.log" || {
    echo "  expected JSON verdict field" >&2
    cat "$tmpdir/stdout.log" >&2
    return 1
  }
  grep -q '"checks":' "$tmpdir/stdout.log" || return 1
  grep -q '"next_action":' "$tmpdir/stdout.log" || return 1
  grep -q '"exit_code":0' "$tmpdir/stdout.log" || return 1
  # stderr should NOT contain pretty output in --json mode (log() returns early)
  if grep -q "verdict: PASS" "$tmpdir/stderr.log"; then
    echo "  FAIL: pretty output emitted in --json mode" >&2
    return 1
  fi
}

test_help_flag_exits_0() {
  local tmpdir="$1"
  set +e
  bash "$SCRIPT" --help >"$tmpdir/stdout.log" 2>&1
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
  grep -q "drill-preflight" "$tmpdir/stdout.log" || return 1
}

test_no_tokens_skips_check3_no_spurious_collision() {
  # Iter 8 bug fix: when DIGITALOCEAN_TOKEN missing, isolation script exits 2
  # ("no token at all"). Old code labeled ANY non-zero as "credentials-collision"
  # — spurious + misleading. Fix: skip check 3 when check 1 already caught
  # missing DO token + handle isolation exit 2 distinctly.
  local tmpdir="$1"
  build_env_all_pass "$tmpdir"
  # Make isolation script exit 2 (real behavior with no token)
  cat > "$tmpdir/infra/scripts/check-drill-credentials-isolation.sh" <<'EOF'
#!/usr/bin/env bash
echo "stub: no DO token" >&2
exit 2
EOF
  chmod +x "$tmpdir/infra/scripts/check-drill-credentials-isolation.sh"
  cd "$tmpdir"
  set +e
  # No DIGITALOCEAN_TOKEN
  PATH="$tmpdir/bin:/usr/bin:/bin" \
    PULUMI_ACCESS_TOKEN=fake \
    CLOUDFLARE_API_TOKEN=fake \
    PULUMI_CONFIG_PASSPHRASE=fake \
    PULUMI_DRILL_DIR="$tmpdir/infra/pulumi-drill" \
    env -u DIGITALOCEAN_TOKEN \
    bash "$SCRIPT" 2>"$tmpdir/stderr.log" >"$tmpdir/stdout.log"
  local rc=$?
  set -e
  # Should exit 1 (creds missing) — NOT 3 (collision)
  [[ "$rc" -eq 1 ]] || {
    echo "  expected exit 1 (creds), got $rc" >&2
    return 1
  }
  # Check 3 should show SKIP, NOT "Credentials collision"
  grep -q "Skipped" "$tmpdir/stderr.log" || {
    echo "  expected check 3 SKIP wording" >&2
    return 1
  }
  if grep -q "CATASTROPHIC" "$tmpdir/stderr.log"; then
    echo "  FAIL: spurious 'CATASTROPHIC' collision message with no token set" >&2
    return 1
  fi
}

test_isolation_check_exit_2_no_do_token() {
  # Iter 8 distinction: isolation exit 2 reported as "no-do-token", not collision
  local tmpdir="$1"
  build_env_all_pass "$tmpdir"
  cat > "$tmpdir/infra/scripts/check-drill-credentials-isolation.sh" <<'EOF'
#!/usr/bin/env bash
exit 2
EOF
  chmod +x "$tmpdir/infra/scripts/check-drill-credentials-isolation.sh"
  # All tokens SET (so check 1 passes + check 3 actually runs the isolation script)
  cd "$tmpdir"
  set +e
  PATH="$tmpdir/bin:/usr/bin:/bin" \
    DIGITALOCEAN_TOKEN=fake \
    PULUMI_ACCESS_TOKEN=fake \
    CLOUDFLARE_API_TOKEN=fake \
    PULUMI_CONFIG_PASSPHRASE=fake \
    PULUMI_DRILL_DIR="$tmpdir/infra/pulumi-drill" \
    bash "$SCRIPT" 2>"$tmpdir/stderr.log" >"$tmpdir/stdout.log"
  local rc=$?
  set -e
  [[ "$rc" -eq 3 ]] || {
    echo "  expected exit 3 (isolation fail), got $rc" >&2
    return 1
  }
  grep -q "No DO token configured" "$tmpdir/stderr.log" || {
    echo "  expected 'No DO token configured' wording (not collision)" >&2
    return 1
  }
}

run_test() {
  local name="$1"
  TESTS_RUN=$((TESTS_RUN + 1))
  local tmpdir
  tmpdir=$(mktemp -d -t drill-preflight-test-XXXXXX)
  if "$name" "$tmpdir"; then
    echo "  [PASS] $name"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  else
    echo "  [FAIL] $name"
    TESTS_FAILED=$((TESTS_FAILED + 1))
  fi
  rm -rf "$tmpdir"
}

echo "=== drill-preflight hermetic tests ==="

run_test test_all_pass_exits_0
run_test test_missing_credentials_exits_1
run_test test_missing_scripts_exits_2
run_test test_isolation_check_fails_exits_3
run_test test_leftover_stack_exits_4
run_test test_unknown_flag_exits_5
run_test test_json_output_format
run_test test_help_flag_exits_0
run_test test_no_tokens_skips_check3_no_spurious_collision
run_test test_isolation_check_exit_2_no_do_token
run_test test_pulumi_access_token_missing_is_advisory_not_failure

echo ""
echo "Tests run:    $TESTS_RUN"
echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"

if [[ "$TESTS_FAILED" -gt 0 ]]; then
  exit 1
fi
exit 0
