#!/usr/bin/env bash
# check-networking-cross-cluster-coredns-syntax_test.sh
#
# Hermetic test for infra/scripts/local/networking-cross-cluster-coredns.sh +
# its homed helper networking_cross_cluster_coredns.py (NEW — the script had no
# coupled test before plan home-unhomed-logic-suspect-keeps T3.1).
#
# Hermetic: NO network, NO kubectl, NO docker. Asserts bash -n, the de-costume
# invariant (no python3 heredoc), the helper compiles, and the merge/set-nodehosts
# transforms behave (override + keep + add; NodeHosts replaced).
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
SCRIPT="${REPO_ROOT}/infra/scripts/local/networking-cross-cluster-coredns.sh"
HELPER="${REPO_ROOT}/infra/scripts/local/networking_cross_cluster_coredns.py"
ALLOWLIST="${REPO_ROOT}/.claude/rules/shell-scripts-allowlist.md"

PASS=0
FAIL=0
assert() {
  local msg=$1 ; shift
  if "$@" > /dev/null 2>&1 ; then printf '[PASS] %s\n' "$msg" ; PASS=$((PASS + 1))
  else printf '[FAIL] %s\n' "$msg" ; FAIL=$((FAIL + 1)) ; fi
}

# --- Existence + syntax -----------------------------------------------------
assert "script exists"        test -f "$SCRIPT"
assert "script bash -n ok"    bash -n "$SCRIPT"
assert "helper exists"        test -f "$HELPER"
assert "helper compiles"      python3 -m py_compile "$HELPER"

# --- De-costume gate (T3.1) -------------------------------------------------
NO_HEREDOC() { [ "$(grep -cE 'python3?[[:space:]]+(-[[:space:]]+)?<<|<<[[:space:]]*['\''\"]*?(PY|PYEOF|PYTHON)' "$SCRIPT")" = "0" ] ; }
assert "no python3 heredoc remains" NO_HEREDOC
# Also ensure no fragile inline `python3 -c` interpolation survived.
NO_PY_C() { [ "$(grep -cE 'python3 -c' "$SCRIPT")" = "0" ] ; }
assert "no inline python3 -c remains" NO_PY_C

# --- merge transform: override + keep + add ---------------------------------
MERGE_OUT="$(NCC_CURRENT=$'1.1.1.1 old-host\n2.2.2.2 k3d-loopzero-build-serverlb' \
             NCC_TARGET=$'9.9.9.9 k3d-loopzero-build-serverlb\n8.8.8.8 k3d-loopzero-workload-serverlb' \
             python3 "$HELPER" merge)"
assert "merge overrides target host (build → 9.9.9.9)" grep -qx '9.9.9.9 k3d-loopzero-build-serverlb' <<< "$MERGE_OUT"
assert "merge keeps existing host (old-host)"          grep -qx '1.1.1.1 old-host' <<< "$MERGE_OUT"
assert "merge adds new target host (workload)"         grep -qx '8.8.8.8 k3d-loopzero-workload-serverlb' <<< "$MERGE_OUT"

# --- set-nodehosts transform ------------------------------------------------
SET_OUT="$(printf 'apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: coredns\ndata:\n  NodeHosts: "old"\n' | \
           NCC_NODEHOSTS=$'1.2.3.4 host-a' python3 "$HELPER" set-nodehosts)"
assert "set-nodehosts replaces data.NodeHosts" grep -q '1.2.3.4 host-a' <<< "$SET_OUT"

# --- Allowlist --------------------------------------------------------------
assert "allowlist file exists" test -f "$ALLOWLIST"
assert "script allowlisted"    grep -q 'networking-cross-cluster-coredns\.sh' "$ALLOWLIST"

printf '\n=== %s ===\n' "$(basename "$0")"
printf 'Asserts: %d / %d PASS\n' "$PASS" "$((PASS + FAIL))"
exit $FAIL
