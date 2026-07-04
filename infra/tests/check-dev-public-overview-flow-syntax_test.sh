#!/usr/bin/env bash
# Hermetic structural guard for infra/tests/e2e/dev-public-overview-flow_test.sh
# (J-06 Overview LIVE-test triplet). No network, no kubectl — pure grep + bash -n.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
LIVE="$REPO_ROOT/infra/tests/e2e/dev-public-overview-flow_test.sh"
SELF="$REPO_ROOT/infra/tests/check-dev-public-overview-flow-syntax_test.sh"
ALLOWLIST="$REPO_ROOT/.claude/rules/shell-scripts-allowlist.md"

pass=0
fail=0
check() {
  local desc="$1"; shift
  if "$@"; then pass=$((pass + 1)); else fail=$((fail + 1)); printf 'FAIL: %s\n' "$desc"; fi
}
has() { grep -qE "$1" "$LIVE"; }
hasF() { grep -qF "$1" "$LIVE"; }

check "LIVE script exists" test -f "$LIVE"
check "bash -n clean" bash -n "$LIVE"
check "strict mode set -euo pipefail" has '^set -euo pipefail'

# Scenarios A/B/C present
check "scenario A (billing plan_price)" has 'scenario_a_billing_plan_price'
check "scenario B (deploys stats)" has 'scenario_b_deploys_stats'
check "scenario C (kpi strip shipped)" has 'scenario_c_kpi_strip_shipped'
check "preflight present" has 'preflight\(\)'
check "read_jwt helper" has 'read_jwt\(\)'

# Flags wired
check "--skip-live flag" has '\-\-skip-live'
check "--scenario flag" has '\-\-scenario'
check "--json flag" has '\-\-json'
check "--help flag" has '\-h\|--help'
check "--skip-live short-circuits (exit 0 before network)" has 'SKIP_LIVE.*==.*1'

# Contract probes
check "scenario A hits /v1/billing" hasF '/v1/billing'
check "scenario A rejects dishonest spend field" has 'current_spend\|spend_mtd'
check "scenario A asserts plan_price value object" hasF 'plan_price'
check "scenario B hits /engine/api/v1/builds/stats" hasF '/engine/api/v1/builds/stats?window=7d'
check "scenario B asserts dense 7-day series" has 'len\(by_day\) == 7'
check "scenario B asserts UTC timezone" has 'timezone.*UTC'
check "scenario C asserts KPI labels shipped" hasF 'Deploys (7d)'

# Governance / mandate
check "OPERATOR HARD STOP header" has 'OPERATOR HARD STOP'
check "cleanroom-up-live-test-mandate cited" hasF 'cleanroom-up-live-test-mandate'
check "MEET-2026-07-03-003 cited" hasF 'MEET-2026-07-03-003'
check "no cloud-dev default (Caddy 301 trap)" bash -c '! grep -qE ":-.*cloud-dev" "'"$LIVE"'"'

# Anti-patterns
check "no Co-Authored-By trailer" bash -c '! grep -qi "Co-Authored-By" "'"$LIVE"'"'
check "no --no-verify" bash -c '! grep -q -- "--no-verify" "'"$LIVE"'"'

# Allowlist cross-check
check "LIVE script allowlisted" grep -qF 'dev-public-overview-flow_test.sh' "$ALLOWLIST"
check "syntax gate allowlisted" grep -qF 'check-dev-public-overview-flow-syntax_test.sh' "$ALLOWLIST"

printf '[overview-flow-syntax-test] %d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
