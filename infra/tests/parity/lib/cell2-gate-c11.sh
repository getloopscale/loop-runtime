#!/usr/bin/env bash
# cell-2-activation-plan T1.4 + ADR C11 — authz on --cell flag, 3 paths.
#
# Path 1 (CLI): unit-tested by Go suite (TestAdminPinCell_AuthzInvariantC11Path1).
# Path 2 (API): unit-tested by Go suite (TestAdminPinCell_AuthzInvariantC11Path2).
# Path 3 (webhook): code-grep — autodeploy.go MUST NOT contain cell_id parsing.
#
# This gate runs the Go test trio + the path-3 grep regression.

gate_c11() {
  local repo_root="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)}"
  if ! command -v go >/dev/null 2>&1; then
    echo "    C11 SKIP: go not installed"
    return 0
  fi
  if ! (cd "$repo_root/api" && go test -run "TestAdminPinCell_AuthzInvariantC11" \
        ./internal/routes/... >/dev/null 2>&1); then
    echo "    C11 FAIL: TestAdminPinCell_AuthzInvariantC11* tests failed"
    return 1
  fi

  # Path 3 grep regression: webhook handlers MUST NOT carry cell_id parsing.
  local autodeploy="$repo_root/api/internal/github/autodeploy.go"
  if [[ -r "$autodeploy" ]]; then
    if grep -qE 'payload\.cell_id|"cell_id"|webhook\.CellID|pushEvent\.CellID|AutoDeployInput\.Cell' "$autodeploy"; then
      echo "    C11 FAIL: autodeploy.go contains forbidden cell_id parsing (path 3 violation)"
      return 1
    fi
    if ! grep -q "ADR C11 path 3" "$autodeploy"; then
      echo "    C11 FAIL: autodeploy.go missing 'ADR C11 path 3' godoc invariant marker"
      return 1
    fi
  fi
  echo "    C11 PASS: 3 authz paths verified (CLI, API, webhook code-grep)"
  return 0
}
