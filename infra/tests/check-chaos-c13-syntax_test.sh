#!/usr/bin/env bash
# check-chaos-c13-syntax_test.sh
#
# Plan c13-valid-destructive-config-chaos (T2.2). Hermetic structural guard for
# the c13 LIVE chaos scenario. Pure grep + bash -n — NO network, NO kubectl, NO
# cluster. Wired into `task quality:gates`. Catches drift of the c13 LIVE bash
# (flags, scenario shape, NO-chaos-mesh invariant, EC-3 boundary guard, HARD STOP
# banner) so the operator-gated script cannot silently rot.
set -uo pipefail

SCRIPT="infra/scripts/local/chaos-scenarios/c13-valid-destructive-config.sh"
GUARD="infra/tests/check-chaos-c13-syntax_test.sh"
ALLOWLIST=".claude/rules/shell-scripts-allowlist.md"

pass=0
fail=0
ok()   { printf '  ok   %s\n' "$1"; pass=$((pass+1)); }
bad()  { printf '  FAIL %s\n' "$1"; fail=$((fail+1)); }

# A1 — file exists
[ -f "$SCRIPT" ] && ok "c13 script exists" || bad "c13 script missing: $SCRIPT"

# A2 — bash -n syntax clean
if bash -n "$SCRIPT" 2>/dev/null; then ok "bash -n clean"; else bad "bash -n failed"; fi

# A3 — strict mode
grep -q 'set -euo pipefail' "$SCRIPT" && ok "set -euo pipefail" || bad "missing set -euo pipefail"

# A4 — three flags wired
grep -q -- '--skip-live)' "$SCRIPT" && ok "flag --skip-live" || bad "missing --skip-live"
grep -q -- '--json)' "$SCRIPT" && ok "flag --json" || bad "missing --json"
grep -q -- '--scenario=\*)' "$SCRIPT" && ok "flag --scenario=" || bad "missing --scenario="

# A5 — scenario A function present
grep -q 'scenario_a()' "$SCRIPT" && ok "scenario_a() present" || bad "missing scenario_a()"

# A6 — NO chaos-mesh CRD USAGE (config fault, not infra fault; ADR D3).
#       Prose comments mentioning chaos-mesh's ABSENCE are allowed; an actual
#       chaos CRD (kind: PodChaos/NetworkChaos or chaos-mesh.org apiVersion) is NOT.
if grep -qE 'kind:[[:space:]]*(PodChaos|NetworkChaos)|chaos-mesh\.org' "$SCRIPT"; then
  bad "c13 must NOT use a chaos-mesh CRD (config fault, not infra fault — ADR D3)"
else
  ok "no chaos-mesh CRD usage (config fault, ADR D3)"
fi

# A7 — OPERATOR HARD STOP + cleanroom mandate citation
grep -q 'OPERATOR HARD STOP' "$SCRIPT" && ok "OPERATOR HARD STOP banner" || bad "missing HARD STOP banner"
grep -q 'cleanroom-up-live-test-mandate' "$SCRIPT" && ok "cites cleanroom-up-live-test-mandate" || bad "missing cleanroom mandate citation"

# A8 — fail-loud reason assertion (health/timeout)
grep -q 'did not become healthy' "$SCRIPT" && ok "asserts health reason" || bad "missing health-reason assertion"

# A9 — EC-3 boundary guard (release_id / build-SUCCEEDED-or-PENDING before poll)
grep -q 'EC-3' "$SCRIPT" && ok "EC-3 boundary guard documented" || bad "missing EC-3 boundary guard"
grep -q 'no release_id returned' "$SCRIPT" && ok "EC-3 build-stage-failure guard" || bad "missing build-stage-failure guard"

# A10 — --skip-live short-circuits BEFORE any cluster call (the if-block precedes preflight/main)
if awk '/if \[ "\$SKIP_LIVE" -eq 1 \]/{sl=NR} /^preflight_deploy_path\(\)/{pf=NR} END{exit !(sl>0 && pf>0 && sl<pf)}' "$SCRIPT"; then
  ok "--skip-live short-circuits before preflight"
else
  bad "--skip-live must short-circuit before any cluster call"
fi

# A11 — asserts SYNC_FAILED terminal status
grep -q 'SYNC_FAILED' "$SCRIPT" && ok "asserts SYNC_FAILED" || bad "missing SYNC_FAILED assertion"

# A12 — asserts ACTIVE is a FAIL (contradicts health-gate)
grep -q 'went ACTIVE despite' "$SCRIPT" && ok "ACTIVE is a FAIL (health-gate contradiction)" || bad "missing ACTIVE-is-fail assertion"

# A13 — allowlist cross-check (both new .sh files listed)
grep -q 'chaos-scenarios/c13-valid-destructive-config.sh' "$ALLOWLIST" && ok "allowlist: c13 LIVE bash" || bad "allowlist missing c13 LIVE bash"
grep -q 'check-chaos-c13-syntax_test.sh' "$ALLOWLIST" && ok "allowlist: c13 guard" || bad "allowlist missing c13 guard"

# A14 — anti-patterns: the LIVE script must not carry a Co-Authored-By trailer
#       nor a --no-verify bypass. Scoped to $SCRIPT (the artifact under test);
#       $GUARD legitimately names these strings in its own assertion labels.
if grep -q 'Co-Authored-By' "$SCRIPT"; then bad "Co-Authored-By forbidden in c13 script"; else ok "no Co-Authored-By in c13 script"; fi
if grep -q -- '--no-verify' "$SCRIPT"; then bad "--no-verify forbidden in c13 script"; else ok "no --no-verify in c13 script"; fi

printf '\n[check-chaos-c13] %d ok / %d fail\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
