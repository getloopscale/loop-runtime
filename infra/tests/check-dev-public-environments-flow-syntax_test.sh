#!/usr/bin/env bash
# T4.4 hermetic structural test for dev-public-environments-flow_test.sh.
# Plan: .claude/knowledge-base/plans/2026-06-06-dashboard-journey-j10-environments-plan.md
#
# Validates structure WITHOUT executing the LIVE script: bash -n syntax,
# 4 scenario functions (A create, B delete, C promote canonical body,
# D preflight), pre-flight helper, 3 flags wired (--skip-live,
# --scenario=, --json), key probes (canonical {from,to} body NOT
# {from_environment,to_environment} v1.0 bug; /environments path;
# /promote path), exit-code contract, allowlist entries.
# NO network. NO kubectl.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TARGET="$REPO_ROOT/infra/tests/e2e/dev-public-environments-flow_test.sh"
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

contains() {
  grep -qF -- "$2" "$1" && echo 1 || echo 0
}

contains_re() {
  grep -qE -- "$2" "$1" && echo 1 || echo 0
}

printf '[environments-flow-syntax-test] gates:\n'

# 1. Target script exists
[[ -f "$TARGET" ]] && exists=1 || exists=0
assert "target script exists" "$exists"
if [[ "$exists" != "1" ]]; then
  printf 'ABORT: %s missing\n' "$TARGET"
  exit 1
fi

# 2. bash -n syntax check
if bash -n "$TARGET" 2>/dev/null; then
  assert "bash -n syntax check" 1
else
  assert "bash -n syntax check" 0
fi

# 3. 6 scenario functions present per plan v1.1 § Phase 4 T4.3:
#    A=create, B=delete, C=promote canonical, D=KPI shape, E=402 plan,
#    F=409 concurrent
for fn in scenario_a scenario_b scenario_c scenario_d scenario_e scenario_f; do
  assert "function ${fn} defined" "$(contains_re "$TARGET" "^${fn}\(\)")"
done

# 4. pre-flight HEALTHCHECK helper
assert "preflight() helper defined" "$(contains_re "$TARGET" "^preflight\(\)")"

# 5. Three flags wired: --skip-live, --scenario=, --json
assert "--skip-live flag wired" "$(contains "$TARGET" "--skip-live")"
assert "--scenario= flag wired" "$(contains "$TARGET" "--scenario=")"
assert "--json flag wired" "$(contains "$TARGET" "--json")"

# 6. Exit-code contract: non-zero on any FAIL, zero otherwise
assert "exit 1 on FAIL" "$(contains "$TARGET" "exit 1")"
assert "exit 0 on PASS" "$(contains "$TARGET" "exit 0")"

# 7. Key probes from the plan
#    Scenario A+B touch the canonical /environments collection
assert "/environments path probed" "$(contains "$TARGET" "/api/v1/environments")"
#    Scenario C asserts the canonical promote body shape ({from,to} — NOT v1.0 bug
#    {from_environment,to_environment} which the backend silently ignored)
assert "canonical promote body shape {\"from\":..,\"to\":..}" \
  "$(contains "$TARGET" '"from":"staging","to":"production"')"
#    Scenario C uses the canonical /promote URL
assert "/promote endpoint probed" "$(contains "$TARGET" "/promote")"

# 8. JWT helper expected (mint_jwt) — every scenario mutating state needs auth
assert "mint_jwt() helper defined" "$(contains_re "$TARGET" "^mint_jwt\(\)")"

# 8.5 Scenario E (402): probes the 402 PLAN_LIMIT_EXCEEDED contract via burst create
assert "scenario E probes 402 PLAN_LIMIT_EXCEEDED" "$(contains "$TARGET" "402")"
assert "scenario E uses burst loop" "$(contains "$TARGET" "j10-burst")"

# 8.6 Scenario F: probes concurrent same-name race via parallel curl.
#     Per K8s optimistic-CC + idempotent AddEnvironment, success modes are
#     (a) first-wins 201+409 OR (b) idempotent 201+201. Both leave env
#     exists exactly once. Test asserts the steady-state invariant, NOT
#     a specific HTTP code combination.
assert "scenario F uses parallel background curls" "$(contains "$TARGET" "j10-race")"
assert "scenario F asserts steady-state invariant" \
  "$(contains "$TARGET" "exactly once after concurrent")"
assert "scenario F rejects 5xx server errors" \
  "$(contains "$TARGET" "no server-error")"

# 9. Defensive: scenario C MUST NOT regress to v1.0 bug body shape.
#    The v1.0 dashboard bug sent {from_environment, to_environment}, which the
#    backend silently ignored, causing the promote to fail at state-machine
#    layer instead of request-parse. The check skips comment lines so that
#    the LIVE script can DOCUMENT the bug it guards against without
#    tripping this gate.
non_comment_hits=$(grep -E "from_environment|to_environment" "$TARGET" \
  | grep -Ev '^\s*#' || true)
if [[ -z "$non_comment_hits" ]]; then
  assert "scenario C does NOT regress to v1.0 buggy body keys (non-comment)" 1
else
  assert "scenario C does NOT regress to v1.0 buggy body keys (non-comment)" 0
  printf '    offending line(s):\n%s\n' "$non_comment_hits" >&2
fi

# 10. Allowlist entries exist for both this hermetic test and the LIVE target
if [[ -f "$ALLOWLIST" ]]; then
  assert "allowlist contains dev-public-environments-flow_test.sh" \
    "$(contains "$ALLOWLIST" "dev-public-environments-flow_test.sh")"
  assert "allowlist contains check-dev-public-environments-flow-syntax_test.sh" \
    "$(contains "$ALLOWLIST" "check-dev-public-environments-flow-syntax_test.sh")"
fi

TOTAL=$((PASS + FAIL))
printf '\n[environments-flow-syntax-test] verdict: %d/%d PASS, %d FAIL\n' \
  "$PASS" "$TOTAL" "$FAIL"
[[ "$FAIL" == "0" ]] && exit 0 || exit 1
