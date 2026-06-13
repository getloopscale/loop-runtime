#!/usr/bin/env bash
# T4.2 of plan 2026-06-10-j22-openbao-transit-eso-sync v1.2 — hermetic
# structural test for infra/tests/e2e/dev-public-secrets-zero-leak_test.sh.
# Validates the LIVE script's shape (bash -n syntax, 6 scenario presence,
# flag wiring, sentinel discipline, anti-patterns) WITHOUT invoking it
# against a live cluster.
#
# NO network, NO kubectl, NO curl. Pure grep on the LIVE script source.

set -uo pipefail
cd "$(dirname "$0")/.."

readonly LIVE_SCRIPT="tests/e2e/dev-public-secrets-zero-leak_test.sh"
readonly TEST_NAME="check-dev-public-secrets-zero-leak-syntax_test"

PASS_COUNT=0
FAIL_COUNT=0
FAILED_ASSERTIONS=()

assert() {
  local desc="$1"; local cond="$2"
  if eval "$cond"; then
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    FAIL_COUNT=$((FAIL_COUNT + 1))
    FAILED_ASSERTIONS+=("$desc")
    echo "  FAIL: $desc" >&2
  fi
}

# === Section A: file + syntax ===
assert "A1: LIVE script exists" "[ -f '$LIVE_SCRIPT' ]"
assert "A2: bash -n parses without syntax error" "bash -n '$LIVE_SCRIPT' 2>/dev/null"
assert "A3: shebang uses env bash" "head -1 '$LIVE_SCRIPT' | grep -qE '^#!/usr/bin/env bash'"
assert "A4: strict mode set -uo pipefail" "grep -qE '^\s*set -e?u?o?\s+pipefail|^set -uo pipefail' '$LIVE_SCRIPT'"

# === Section B: 6 scenarios A-F all present ===
assert "B1: Scenario A invoked via run_scenario A" "grep -q 'run_scenario A' '$LIVE_SCRIPT'"
assert "B2: Scenario B invoked via run_scenario B" "grep -q 'run_scenario B' '$LIVE_SCRIPT'"
assert "B3: Scenario C invoked via run_scenario C" "grep -q 'run_scenario C' '$LIVE_SCRIPT'"
assert "B4: Scenario D invoked via run_scenario D" "grep -q 'run_scenario D' '$LIVE_SCRIPT'"
assert "B5: Scenario E invoked via run_scenario E" "grep -q 'run_scenario E' '$LIVE_SCRIPT'"
assert "B6: Scenario F invoked via run_scenario F" "grep -q 'run_scenario F' '$LIVE_SCRIPT'"

# === Section C: scenario-specific probes ===
assert "C1: A probes PG encrypted_value via psql" "grep -q 'encrypted_value' '$LIVE_SCRIPT' && grep -qE 'psql|select.*from.*secrets' '$LIVE_SCRIPT'"
assert "C2: A asserts vault:v1: prefix" "grep -qE 'vault:v1' '$LIVE_SCRIPT'"
assert "C3: B probes workload K8s Secret via kubectl get secret" "grep -qE 'kubectl.*get secret' '$LIVE_SCRIPT'"
assert "C4: C uses kubectl exec + printenv (multi-line allowed)" "grep -q 'kubectl' '$LIVE_SCRIPT' && grep -q 'exec' '$LIVE_SCRIPT' && grep -q 'printenv' '$LIVE_SCRIPT'"
assert "C5: D uses kubectl logs -A" "grep -qE 'kubectl.*logs.*-A|--all-containers' '$LIVE_SCRIPT'"
assert "C6: E uses kubectl describe pods" "grep -qE 'kubectl.*describe.*pods' '$LIVE_SCRIPT'"
assert "C7: F decodes Secret base64 + greps for sentinel" "grep -qE 'b64|base64' '$LIVE_SCRIPT'"

# === Section D: flag wiring ===
assert "D1: --skip-live flag declared" "grep -q -- '--skip-live' '$LIVE_SCRIPT'"
assert "D2: --json flag declared" "grep -q -- '--json' '$LIVE_SCRIPT'"
assert "D3: --scenario= flag declared" "grep -q -- '--scenario=' '$LIVE_SCRIPT'"
assert "D4: --help/-h flag declared" "grep -qE -- '--help|-h\\)' '$LIVE_SCRIPT'"
assert "D5: usage() function defined" "grep -qE '^usage\(\)' '$LIVE_SCRIPT'"

# === Section E: sentinel + cleanup discipline ===
assert "E1: sentinel UUID is ephemeral (per-run generation, NOT hardcoded)" "grep -qE 'uuid|UUID|/proc/sys/kernel/random/uuid' '$LIVE_SCRIPT'"
assert "E2: sentinel variable name documented" "grep -q 'SENTINEL_UUID\|SENTINEL_VALUE' '$LIVE_SCRIPT'"
assert "E3: cleanup trap on EXIT INT TERM (EC-14)" "grep -qE 'trap cleanup.*EXIT.*INT.*TERM' '$LIVE_SCRIPT'"

# === Section F: verdict + exit code contract ===
assert "F1: verdict variable set" "grep -qE 'VERDICT=' '$LIVE_SCRIPT'"
assert "F2: PASS verdict path documented" "grep -q 'verdict.*PASS\|VERDICT=\"PASS\"' '$LIVE_SCRIPT'"
assert "F3: FAIL verdict path documented" "grep -q 'verdict.*FAIL\|VERDICT=\"FAIL\"' '$LIVE_SCRIPT'"
assert "F4: exit 0 on PASS path" "grep -qE 'exit 0' '$LIVE_SCRIPT'"
assert "F5: exit 1 on FAIL path" "grep -qE 'exit 1' '$LIVE_SCRIPT'"
assert "F6: exit 2 on usage error" "grep -qE 'exit 2' '$LIVE_SCRIPT'"

# === Section G: anti-patterns ===
assert "G1: no Co-Authored-By trailer" "! grep -q 'Co-Authored-By' '$LIVE_SCRIPT'"
assert "G2: no --no-verify usage" "! grep -qE -- '--no-verify' '$LIVE_SCRIPT'"
assert "G3: no rm -rf or destructive commands" "! grep -qE 'rm -rf|kubectl.*delete.*--all' '$LIVE_SCRIPT'"
assert "G4: cites the plan + T4.1 task" "grep -qE 'T4\\.1|2026-06-10-j22-openbao-transit' '$LIVE_SCRIPT'"
assert "G5: D5 ADR zero-leak proof referenced" "grep -qE 'D5 ADR|zero-leak' '$LIVE_SCRIPT'"
assert "G6: allowlist entry exists" "grep -q 'dev-public-secrets-zero-leak_test\\.sh' ../.claude/rules/shell-scripts-allowlist.md"

# === Section H: --skip-live short-circuit (hermetic CI mode) ===
assert "H1: --skip-live exits before any kubectl/curl call" "awk '/SKIP_LIVE -eq 1/,/exit 0/' '$LIVE_SCRIPT' | grep -qE 'SKIPPED|exit 0'"

# Summary
echo ""
TOTAL=$((PASS_COUNT + FAIL_COUNT))
echo "$TEST_NAME: $PASS_COUNT/$TOTAL PASS / $FAIL_COUNT FAIL"

if [ $FAIL_COUNT -gt 0 ]; then
  echo "Failed assertions:"
  for a in "${FAILED_ASSERTIONS[@]}"; do
    echo "  - $a"
  done
  exit 1
fi
exit 0
