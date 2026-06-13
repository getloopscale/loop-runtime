#!/usr/bin/env bash
# T5.2 of plan 2026-06-11-cleanroom-canonical-5-defense-layers v1.2.
#
# Hermetic structural test for T5.1 dev-public-cleanroom-canonical-9-journeys_test.sh.
# Validates the master LIVE orchestrator's shape WITHOUT running journeys
# (no kubectl, no network). Closes Phase 5 RED for T5.1 GREEN target.
#
# ~15 structural assertions cover:
#   1. bash -n syntax clean
#   2. All 9 journey IDs declared (J-22 J-09 J-23 J-07 J-08 J-10 J-17 J-05 J-04)
#   3. 4 flags wired (--skip-live, --include-semi-auto, --journey=, --json)
#   4. Auto vs Semi-auto distinction enforced (J-07 + J-08 marked semi-auto)
#   5. Pre-flight JWT seed check present
#   6. Acceptance criteria comment present (9/9 GREEN OR BLOCKED with reason)
#   7. Engineering-side IMPLEMENTATION_COMPLETE marker conditional
#   8. ADR D6/D7/D10 citations present (rationale traceability)

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TARGET="$SCRIPT_DIR/e2e/dev-public-cleanroom-canonical-9-journeys_test.sh"

pass=0; fail=0

log() { printf '[check-9-journeys-syntax] %s\n' "$*" >&2; }

assert_grep() {
  local desc="$1" pattern="$2"
  if grep -qE "$pattern" "$TARGET" 2>/dev/null; then
    log "PASS [$desc]"
    pass=$((pass+1))
  else
    log "FAIL [$desc]: pattern not matched: $pattern"
    fail=$((fail+1))
  fi
}

# === A: file + syntax ===
if [[ ! -f "$TARGET" ]]; then
  log "FAIL [target file not found]: $TARGET"
  exit 1
fi
if bash -n "$TARGET" 2>/dev/null; then
  log "PASS [bash -n syntax clean]"
  pass=$((pass+1))
else
  log "FAIL [bash -n syntax errors]"
  fail=$((fail+1))
fi

# === B: 9 journeys declared ===
for j in J-22 J-09 J-23 J-07 J-08 J-10 J-17 J-05 J-04; do
  assert_grep "journey $j declared in JOURNEYS array" "^[[:space:]]*\"$j:"
done

# === C: flags wired ===
assert_grep "--skip-live flag wired" '\-\-skip-live'
assert_grep "--include-semi-auto flag wired" '\-\-include-semi-auto'
assert_grep "--journey= flag wired" '\-\-journey='
assert_grep "--json flag wired" '\-\-json'

# === D: Auto vs Semi-auto distinction ===
assert_grep "J-07 marked semi-auto (sponsor walkthrough HARD STOP)" '^[[:space:]]*"J-07:.*:semi-auto'
assert_grep "J-08 marked semi-auto (sponsor walkthrough HARD STOP)" '^[[:space:]]*"J-08:.*:semi-auto'

# === E: pre-flight + acceptance ===
assert_grep "pre-flight checks JWT seed file" 'local-dogfood-token\.txt'
assert_grep "acceptance criteria documented (9/9 GREEN)" '9/9 GREEN'

# === F: engineering-side marker conditional ===
assert_grep "IMPLEMENTATION_COMPLETE-ENGINEERING-SIDE conditional marker" 'IMPLEMENTATION_COMPLETE-ENGINEERING-SIDE'

# === G: ADR citations (traceability) ===
assert_grep "ADR D6 cited (bash NOT Ginkgo)" 'ADR D6'
assert_grep "ADR D7 cited (sponsor scheduling)" 'ADR D7'
assert_grep "ADR D10 cited (auto vs semi-auto)" 'ADR D10'

# === H: anti-patterns absence ===
if ! grep -q "Co-Authored-By" "$TARGET" 2>/dev/null; then
  log "PASS [no Co-Authored-By trailer in script]"
  pass=$((pass+1))
else
  log "FAIL [Co-Authored-By trailer found — violates project mandate]"
  fail=$((fail+1))
fi

log ""
log "════════════════════════════════════════════════════════"
log "  check-cleanroom-canonical-9-journeys-syntax: $pass PASS / $fail FAIL"
log "════════════════════════════════════════════════════════"

[[ "$fail" -eq 0 ]]
