#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# T6.1 of plan deploy-timeline-canonical-5-phases v1.1 — hermetic
# structural test for dev-public-deploy-timeline_test.sh.
#
# Asserts the LIVE bash script is canonical-shaped: bash -n syntax,
# 5 scenarios A/B/C/D/E, 4 flags wired, OPERATOR HARD STOP cited,
# anti-pattern guards, allowlist cross-check.
#
# NO network, NO kubectl, NO curl. Pure grep on source.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
LIVE_SCRIPT="$ROOT_DIR/tests/e2e/dev-public-deploy-timeline_test.sh"

PASS=0
FAIL=0

assert() {
    local name="$1"; local cond="$2"
    if eval "$cond"; then
        PASS=$((PASS+1))
    else
        FAIL=$((FAIL+1))
        echo "  FAIL $name (cond: $cond)" >&2
    fi
}

# --- A: file existence + bash -n ---------------------------------------
assert "A1.file-exists" "[[ -f '$LIVE_SCRIPT' ]]"
assert "A2.bash-n-clean" "bash -n '$LIVE_SCRIPT'"
assert "A3.strict-mode" "grep -q 'set -euo pipefail' '$LIVE_SCRIPT'"

# --- B: 5 scenarios A-E ------------------------------------------------
assert "B1.scenario-A" "grep -q '^scenario_A()' '$LIVE_SCRIPT'"
assert "B2.scenario-B" "grep -q '^scenario_B()' '$LIVE_SCRIPT'"
assert "B3.scenario-C" "grep -q '^scenario_C()' '$LIVE_SCRIPT'"
assert "B4.scenario-D" "grep -q '^scenario_D()' '$LIVE_SCRIPT'"
assert "B5.preflight-E" "grep -q '^preflight_E()' '$LIVE_SCRIPT'"

# --- C: flags wired ----------------------------------------------------
assert "C1.flag-skip-live" "grep -q -- '--skip-live' '$LIVE_SCRIPT'"
assert "C2.flag-scenario" "grep -q -- '--scenario=' '$LIVE_SCRIPT'"
assert "C3.flag-json" "grep -q -- '--json' '$LIVE_SCRIPT'"
assert "C4.flag-help" "grep -q -- '--help' '$LIVE_SCRIPT'"

# --- D: defense-in-depth + canonical citations -------------------------
assert "D1.HARD-STOP-cited" "grep -q 'OPERATOR HARD STOP' '$LIVE_SCRIPT'"
assert "D2.mandate-cited" "grep -q 'cleanroom-up-live-test-mandate' '$LIVE_SCRIPT'"
assert "D3.plan-cited" "grep -q 'deploy-timeline-canonical-5-phases' '$LIVE_SCRIPT'"
assert "D4.JWT-seed-doc" "grep -q 'local-dogfood-token.txt' '$LIVE_SCRIPT'"

# --- E: scrub denylist coverage (Scenario D) ---------------------------
assert "E1.denylist-ArgoCD" "grep -q 'ArgoCD' '$LIVE_SCRIPT'"
assert "E2.denylist-cosign" "grep -q 'cosign' '$LIVE_SCRIPT'"
assert "E3.denylist-Gitea" "grep -q 'Gitea' '$LIVE_SCRIPT'"

# --- F: tenant defense (Scenario B) ------------------------------------
assert "F1.tenant-404-asserted" "grep -q '404' '$LIVE_SCRIPT'"
assert "F2.foreign-deploy-probe" "grep -q 'dep-does-not-exist' '$LIVE_SCRIPT'"

# --- G: anti-patterns absent ------------------------------------------
assert "G1.no-Co-Authored-By" "! grep -q 'Co-Authored-By' '$LIVE_SCRIPT'"
assert "G2.no-no-verify" "! grep -q -- '--no-verify' '$LIVE_SCRIPT'"

# --- H: exit-code reflects FAIL count ---------------------------------
assert "H1.exit-on-fail" "grep -q '\[\[ \$FAIL -eq 0 \]\]' '$LIVE_SCRIPT'"

# --- Verdict -----------------------------------------------------------
if [[ $FAIL -eq 0 ]]; then
    echo "check-dev-public-deploy-timeline-syntax: $PASS/$PASS PASS"
    exit 0
else
    echo "check-dev-public-deploy-timeline-syntax: $PASS PASS / $FAIL FAIL"
    exit 1
fi
