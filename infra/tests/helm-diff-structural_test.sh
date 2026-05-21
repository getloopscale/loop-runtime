#!/usr/bin/env bash
# infra/tests/helm-diff-structural_test.sh
#
# Unit tests for infra/scripts/helm-diff-structural.sh.
# Audit gap M06 #21 / §6.E.6b regression suite.
#
# Each test synthesizes a base + PR rendered-yaml pair embodying one
# structural-risk pattern, runs the script, and asserts the expected
# exit code (0 = safe, 2 = structural-risk-blocked).
#
# Run: bash infra/tests/helm-diff-structural_test.sh

set -euo pipefail

set -u

SCRIPT="$(cd "$(dirname "$0")/../.." && pwd)/infra/scripts/helm-diff-structural.sh"
[[ -x "$SCRIPT" ]] || { echo "FAIL: $SCRIPT not executable"; exit 1; }

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

pass=0
fail=0

run_case() {
  local name="$1"
  local base="$2"
  local pr="$3"
  local want_exit="$4"

  local base_file="$WORK/${name}-base.yaml"
  local pr_file="$WORK/${name}-pr.yaml"
  printf '%s' "$base" >"$base_file"
  printf '%s' "$pr" >"$pr_file"

  "$SCRIPT" "$name" "$base_file" "$pr_file" >"$WORK/${name}.out" 2>&1
  local got_exit=$?

  if [[ "$got_exit" -eq "$want_exit" ]]; then
    echo "PASS: $name (exit=$got_exit)"
    pass=$((pass + 1))
  else
    echo "FAIL: $name (want exit=$want_exit, got=$got_exit)"
    echo "  stdout: $(cat "$WORK/${name}.out")"
    fail=$((fail + 1))
  fi
}

# ---------------------------------------------------------------------------
# CASE 1: no diff → exit 0 (safe)
# ---------------------------------------------------------------------------
SAME=$'---\nkind: Service\nmetadata:\n  name: api\n  namespace: loopzero-system\n'
run_case "no-diff" "$SAME" "$SAME" 0

# ---------------------------------------------------------------------------
# CASE 2: cosmetic diff (image tag bump) → exit 0 (informational)
# ---------------------------------------------------------------------------
BASE_IMG=$'---\nkind: Deployment\nmetadata:\n  name: api\n  namespace: loopzero-system\nspec:\n  template:\n    spec:\n      containers:\n        - name: api\n          image: registry/api:v1.0.0\n'
PR_IMG=$'---\nkind: Deployment\nmetadata:\n  name: api\n  namespace: loopzero-system\nspec:\n  template:\n    spec:\n      containers:\n        - name: api\n          image: registry/api:v1.1.0\n'
run_case "cosmetic-image-bump" "$BASE_IMG" "$PR_IMG" 0

# ---------------------------------------------------------------------------
# CASE 3: STRUCT-1 — NetworkPolicy removed → exit 2 (blocked)
# ---------------------------------------------------------------------------
BASE_NP=$'---\nkind: NetworkPolicy\nmetadata:\n  name: deny-all\n  namespace: loopzero-system\n---\nkind: Service\nmetadata:\n  name: api\n  namespace: loopzero-system\n'
PR_NP=$'---\nkind: Service\nmetadata:\n  name: api\n  namespace: loopzero-system\n'
run_case "struct1-networkpolicy-removed" "$BASE_NP" "$PR_NP" 2

# ---------------------------------------------------------------------------
# CASE 4: STRUCT-1 — RoleBinding removed → exit 2 (blocked)
# ---------------------------------------------------------------------------
BASE_RB=$'---\nkind: RoleBinding\nmetadata:\n  name: api-rb\n  namespace: loopzero-system\n---\nkind: Service\nmetadata:\n  name: api\n  namespace: loopzero-system\n'
PR_RB=$'---\nkind: Service\nmetadata:\n  name: api\n  namespace: loopzero-system\n'
run_case "struct1-rolebinding-removed" "$BASE_RB" "$PR_RB" 2

# ---------------------------------------------------------------------------
# CASE 5: STRUCT-2 — namespace changed → exit 2 (blocked)
# ---------------------------------------------------------------------------
BASE_NS=$'---\nkind: Service\nmetadata:\n  name: api\n  namespace: loopzero-system\n'
PR_NS=$'---\nkind: Service\nmetadata:\n  name: api\n  namespace: default\n'
run_case "struct2-namespace-changed" "$BASE_NS" "$PR_NS" 2

# ---------------------------------------------------------------------------
# CASE 6: STRUCT-3a — runAsNonRoot:true removed → exit 2
# ---------------------------------------------------------------------------
BASE_RNN=$'---\nkind: Deployment\nmetadata:\n  name: api\n  namespace: loopzero-system\nspec:\n  template:\n    spec:\n      securityContext:\n        runAsNonRoot: true\n'
PR_RNN=$'---\nkind: Deployment\nmetadata:\n  name: api\n  namespace: loopzero-system\nspec:\n  template:\n    spec:\n      securityContext: {}\n'
run_case "struct3a-runAsNonRoot-removed" "$BASE_RNN" "$PR_RNN" 2

# ---------------------------------------------------------------------------
# CASE 7: STRUCT-3b — readOnlyRootFilesystem:true removed → exit 2
# ---------------------------------------------------------------------------
BASE_RORF=$'---\nkind: Deployment\nmetadata:\n  name: api\n  namespace: loopzero-system\nspec:\n  template:\n    spec:\n      containers:\n        - name: api\n          securityContext:\n            readOnlyRootFilesystem: true\n'
PR_RORF=$'---\nkind: Deployment\nmetadata:\n  name: api\n  namespace: loopzero-system\nspec:\n  template:\n    spec:\n      containers:\n        - name: api\n          securityContext:\n            readOnlyRootFilesystem: false\n'
run_case "struct3b-readOnlyRootFs-flipped" "$BASE_RORF" "$PR_RORF" 2

# ---------------------------------------------------------------------------
# CASE 8: STRUCT-3c — allowPrivilegeEscalation:true added → exit 2
# ---------------------------------------------------------------------------
BASE_APE=$'---\nkind: Deployment\nmetadata:\n  name: api\n  namespace: loopzero-system\nspec:\n  template:\n    spec:\n      containers:\n        - name: api\n          securityContext:\n            allowPrivilegeEscalation: false\n'
PR_APE=$'---\nkind: Deployment\nmetadata:\n  name: api\n  namespace: loopzero-system\nspec:\n  template:\n    spec:\n      containers:\n        - name: api\n          securityContext:\n            allowPrivilegeEscalation: true\n'
run_case "struct3c-allowPrivEsc-added" "$BASE_APE" "$PR_APE" 2

# ---------------------------------------------------------------------------
# CASE 9: STRUCT-3d — privileged:true added → exit 2
# ---------------------------------------------------------------------------
BASE_PRIV=$'---\nkind: Deployment\nmetadata:\n  name: api\n  namespace: loopzero-system\nspec:\n  template:\n    spec:\n      containers:\n        - name: api\n'
PR_PRIV=$'---\nkind: Deployment\nmetadata:\n  name: api\n  namespace: loopzero-system\nspec:\n  template:\n    spec:\n      containers:\n        - name: api\n          securityContext:\n            privileged: true\n'
run_case "struct3d-privileged-added" "$BASE_PRIV" "$PR_PRIV" 2

# ---------------------------------------------------------------------------
# CASE 10: ADDED NetworkPolicy (not removal) → exit 0 (safe; more security)
# ---------------------------------------------------------------------------
BASE_ADD=$'---\nkind: Service\nmetadata:\n  name: api\n  namespace: loopzero-system\n'
PR_ADD=$'---\nkind: NetworkPolicy\nmetadata:\n  name: deny-all\n  namespace: loopzero-system\n---\nkind: Service\nmetadata:\n  name: api\n  namespace: loopzero-system\n'
run_case "added-networkpolicy-is-safe" "$BASE_ADD" "$PR_ADD" 0

echo ""
echo "Results: $pass passed, $fail failed"
[[ "$fail" -eq 0 ]] && exit 0 || exit 1
