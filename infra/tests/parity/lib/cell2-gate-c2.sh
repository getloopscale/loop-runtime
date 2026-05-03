#!/usr/bin/env bash
# cell-2-activation-plan T1.1 + ADR C2 — routing determinism regression test.
# Runs the Go test suite that pins the deterministic-Resolve invariant.

gate_c2() {
  local repo_root="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)}"
  if ! command -v go >/dev/null 2>&1; then
    echo "    C2 SKIP: go not installed"
    return 0
  fi
  if (cd "$repo_root/api" && go test -race -run "TestDynamicRouter_Resolve_Deterministic" \
        ./internal/placement/... >/dev/null 2>&1); then
    echo "    C2 PASS: TestDynamicRouter_Resolve_Deterministic{AcrossCalls,Concurrent} GREEN with -race"
    return 0
  else
    echo "    C2 FAIL: routing determinism tests fail (run: cd api && go test -race ./internal/placement/...)"
    return 1
  fi
}
