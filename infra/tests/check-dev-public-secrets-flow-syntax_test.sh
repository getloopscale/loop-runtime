#!/usr/bin/env bash
# T5.4 hermetic structural test for dev-public-secrets-flow_test.sh.
# Plan: .claude/knowledge-base/plans/2026-06-06-dashboard-journey-j22-secrets-crud-plan.md
#
# Validates structure WITHOUT executing the LIVE script: bash -n syntax,
# 4 scenario functions (A create, B rotate, C bulk, D ESO), pre-flight
# helper, 3 flags wired (--skip-live, --scenario=, --json), key probes
# (POST /api/v1/secrets path, POST /api/v1/secrets/bulk path, T1.1
# results-length invariant, ESO ClusterSecretStore Ready check),
# exit-code contract, allowlist entries.
# NO network. NO kubectl.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TARGET="$REPO_ROOT/infra/tests/e2e/dev-public-secrets-flow_test.sh"
ALLOWLIST="$REPO_ROOT/.claude/rules/shell-scripts-allowlist.md"

PASS=0
FAIL=0

assert() {
  local desc="$1" cond="$2"
  if [[ "$cond" == "1" ]]; then
    printf '  PASS: %s\n' "$desc"
    PASS=$((PASS + 1))
  else
    printf '  FAIL: %s\n' "$desc"
    FAIL=$((FAIL + 1))
  fi
}

contains() { grep -qF -- "$2" "$1" && echo 1 || echo 0; }
contains_re() { grep -qE -- "$2" "$1" && echo 1 || echo 0; }

# --- Structural assertions ---

# Target exists
[[ -f "$TARGET" ]] && exists=1 || exists=0
assert "target script exists at $TARGET" "$exists"
[[ "$exists" == "0" ]] && { printf 'Summary: PASS=%d FAIL=%d\n' "$PASS" "$FAIL"; exit 1; }

# bash -n syntax check
bash -n "$TARGET" 2>/dev/null && syntax=1 || syntax=0
assert "bash -n syntax check passes" "$syntax"

# Scenario function definitions (per plan: A create / B rotate / C bulk / D ESO)
assert "scenario_a_create() defined" "$(contains_re "$TARGET" '^scenario_a_create\(\)')"
assert "scenario_b_rotate() defined" "$(contains_re "$TARGET" '^scenario_b_rotate\(\)')"
assert "scenario_c_bulk() defined" "$(contains_re "$TARGET" '^scenario_c_bulk\(\)')"
assert "scenario_d_eso() defined" "$(contains_re "$TARGET" '^scenario_d_eso\(\)')"

# Pre-flight helper
assert "preflight() defined" "$(contains_re "$TARGET" '^preflight\(\)')"

# JWT helper
assert "mint_jwt() defined" "$(contains_re "$TARGET" '^mint_jwt\(\)')"

# Flag handling (--skip-live, --scenario=, --json)
assert "--skip-live flag wired" "$(contains "$TARGET" "--skip-live")"
assert "--scenario= flag wired" "$(contains "$TARGET" "--scenario=")"
assert "--json flag wired" "$(contains "$TARGET" "--json")"

# Endpoint probes — POST /api/v1/secrets (single) + POST /api/v1/secrets/bulk
assert "POST /api/v1/secrets path probed" "$(contains "$TARGET" "/api/v1/secrets")"
assert "POST /api/v1/secrets/bulk path probed" "$(contains "$TARGET" "/api/v1/secrets/bulk")"

# T1.1 invariant — results length check in scenario C
assert "scenario C asserts results length (T1.1 invariant)" \
  "$(contains "$TARGET" 'results length')"

# Scenario D ESO infra probe
assert "scenario D checks ClusterSecretStore Ready" \
  "$(contains "$TARGET" 'clustersecretstore')"

# Deferred-scenario honesty (B + D explicitly DEFERRED in body comments)
assert "scenario B notes DEFERRED rotate-with-version" "$(contains "$TARGET" "DEFERRED")"

# Exit-code contract (non-zero on FAIL count)
assert "exit 1 on FAIL count > 0" "$(contains_re "$TARGET" 'exit 1')"

# Summary helper printed
assert "Summary line emitted" "$(contains "$TARGET" "Summary:")"

# JSON output mode
assert "JSON verdict emitted in --json mode" "$(contains "$TARGET" '"verdict"')"

# Allowlist cross-check
assert "target script in allowlist" \
  "$(contains "$ALLOWLIST" 'infra/tests/e2e/dev-public-secrets-flow_test.sh')"
assert "this syntax test in allowlist OR self-acknowledged" \
  "$(contains "$ALLOWLIST" 'infra/tests/check-dev-public-secrets-flow-syntax_test.sh')"

# Anti-patterns
assert "no --no-verify usage" "$(grep -qF -- '--no-verify' "$TARGET" && echo 0 || echo 1)"
assert "no Co-Authored-By trailer" "$(grep -qF -- 'Co-Authored-By' "$TARGET" && echo 0 || echo 1)"

# Summary
printf 'Summary: PASS=%d FAIL=%d\n' "$PASS" "$FAIL"
[[ $FAIL -gt 0 ]] && exit 1 || exit 0
