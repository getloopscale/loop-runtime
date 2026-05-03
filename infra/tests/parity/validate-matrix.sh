#!/usr/bin/env bash
# CHG-2026-04-30-INFRA-NORMALIZE / Phase 8 T8.1 — master validation
# matrix wrapper. Runs every offline gate end-to-end and reports a
# single PASS/FAIL summary. Designed for `task validate:matrix`.
#
# Plan v2 D5 mandates: "All green = the only objective signal. Any
# individual gate green is necessary; the conjunction is sufficient."
# This script IS the conjunction.
#
# Live gates (k3d-virgin GREEN, cell-2 canary HTTP 200, prod stages)
# are reported as STATUS only, NOT executed here — they require cloud.
#
# Gates orchestrated:
#   1. parity-preflight.sh        — 16 sub-gates (parity infra)
#   2. CLI unit tests             — 1243
#   3. CLI integration tests      — 72 (excluding cli-e2e gated suite)
#   4. API tests                  — routes + services + placement + db
#   5. Pulumi vitest              — 269 (was 96 in plan baseline; iters 16-27 expanded)
#   6. infra/tests Go suite       — capacity + chaos + e2e + load + unit
#   7. helm lint × 4 split charts
#   8. matrix-integrity audit
#   9. matrix-audit golden test
#  9.5. validate:slos                — 7 Sloth SLO files (Coverage Matrix #8)
#  9.55 validate:rules               — 12 PrometheusRule files (Plan T5.4 TDD analog)
#  9.6. validate:helm-deps           — 11 charts buildable (Coverage Matrix #7)
#  10. operators tests (envtest)  — when KUBEBUILDER_ASSETS is set
#
# Usage:
#   bash infra/tests/parity/validate-matrix.sh           # full run
#   bash infra/tests/parity/validate-matrix.sh --quick   # skip operators envtest
#
# Exit codes:
#   0 — every gate GREEN (modulo SKIP for env-dependent ones)
#   1 — at least one gate FAILED
#
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
QUICK=0

for arg in "$@"; do
  case "$arg" in
    --quick) QUICK=1 ;;
    -h|--help) sed -n '2,30p' "$0"; exit 0 ;;
  esac
done

pass=0
fail=0
skip=0

result() {
  local rc="$1"; local label="$2"
  case "$rc" in
    0) printf "  PASS  %s\n" "$label"; pass=$((pass + 1)) ;;
    2) printf "  SKIP  %s\n" "$label"; skip=$((skip + 1)) ;;
    *) printf "  FAIL  %s (exit=$rc)\n" "$label"; fail=$((fail + 1)) ;;
  esac
}

echo "[validate-matrix] gates:"

# Gate 1: parity preflight (16 sub-gates including matrix audit + golden + T6.3 extensions; verified 2026-05-01).
bash "${REPO_ROOT}/infra/tests/parity/preflight.sh" >/dev/null 2>&1
result $? "parity-preflight (16 sub-gates)"

# Gate 2: CLI unit tests (1243 verified 2026-05-01).
if (cd "${REPO_ROOT}/cli" && npx jest tests/unit/ --silent 2>&1 | grep -qE "^Tests:.*[0-9]+ passed"); then
  result 0 "cli:unit-tests (1243+)"
else
  # Re-run loud to surface the failure.
  (cd "${REPO_ROOT}/cli" && npx jest tests/unit/ 2>&1 | tail -5) >&2 || true
  result 1 "cli:unit-tests"
fi

# Gate 3: CLI integration (72 cases — excludes cli-e2e gated suite).
# Use --testPathIgnorePatterns to skip cli-e2e (needs real API).
if (cd "${REPO_ROOT}/cli" && npx jest tests/integration/ --silent --testPathIgnorePatterns='cli-e2e' 2>&1 | grep -qE "^Tests:.*[0-9]+ passed"); then
  result 0 "cli:integration-tests"
else
  result 1 "cli:integration-tests"
fi

# Gate 4: API tests for the multi-cell-relevant packages.
if command -v go >/dev/null 2>&1; then
  if (cd "${REPO_ROOT}/api" && go test -short ./internal/routes/... ./internal/services/... ./internal/placement/... ./internal/metrics/... ./tests/unit/... >/dev/null 2>&1); then
    result 0 "api:multi-cell-relevant-tests"
  else
    result 1 "api:multi-cell-relevant-tests"
  fi
else
  result 2 "api: go not installed"
fi

# Gate 5: Pulumi vitest (269+ verified 2026-05-01).
if (cd "${REPO_ROOT}/infra/pulumi" && npx vitest run --reporter=basic >/dev/null 2>&1); then
  result 0 "pulumi:vitest"
else
  result 1 "pulumi:vitest"
fi

# Gate 6: infra/tests Go suite (capacity + chaos + e2e + load + unit).
if command -v go >/dev/null 2>&1; then
  if (cd "${REPO_ROOT}/infra/tests" && go test -short -timeout=120s ./... >/dev/null 2>&1); then
    result 0 "infra:tests-go"
  else
    result 1 "infra:tests-go"
  fi
else
  result 2 "infra:tests-go (go missing)"
fi

# Gate 7: helm lint on 4 split operator charts.
if command -v helm >/dev/null 2>&1; then
  helm_fail=0
  for c in loopzero-tenant-operator loopzero-app-operator loopzero-preview-operator loopzero-platform-operator; do
    if ! helm lint "${REPO_ROOT}/charts/$c" >/dev/null 2>&1; then
      helm_fail=$((helm_fail + 1))
    fi
  done
  if (( helm_fail == 0 )); then
    result 0 "helm:lint-split-operators (4 charts)"
  else
    result 1 "helm:lint-split-operators ($helm_fail fail)"
  fi
else
  result 2 "helm:lint-split-operators (helm missing)"
fi

# Gate 8: matrix integrity audit (already invoked transitively by
# parity-preflight Gate 1 sub-gate, but call it explicitly so a failure
# is surfaced unambiguously here too).
bash "${REPO_ROOT}/infra/tests/parity/audit-regression-matrix.sh" >/dev/null 2>&1
result $? "matrix-integrity-audit:explicit"

# Gate 9: matrix-audit golden test (audit script self-test).
bash "${REPO_ROOT}/infra/tests/parity/audit-golden-test.sh" >/dev/null 2>&1
result $? "matrix-audit-golden:explicit"

# Gate 9.5: Sloth SLO file validation (Coverage Matrix #8).
if command -v python3 >/dev/null 2>&1; then
  if python3 "${REPO_ROOT}/infra/sloth/validate.py" >/dev/null 2>&1; then
    result 0 "validate:slos (7 SLO files)"
  else
    result 1 "validate:slos"
  fi
else
  result 2 "validate:slos (python3 missing)"
fi

# Gate 9.55: PrometheusRule structural validation (Plan T5.4 TDD analog).
if command -v python3 >/dev/null 2>&1; then
  if python3 "${REPO_ROOT}/infra/helm/monitoring/validate-rules.py" >/dev/null 2>&1; then
    result 0 "validate:rules (12 PrometheusRule files)"
  else
    result 1 "validate:rules"
  fi
else
  result 2 "validate:rules (python3 missing)"
fi

# Gate 9.6: helm dependency build for umbrella charts (Coverage Matrix #7).
# Closes "charts/loopzero-platform requires helm dep build" gap. Reaches network
# to refresh helm repos — gate is SKIP when offline (HELM_REPOS_UNREACHABLE=1).
if command -v helm >/dev/null 2>&1; then
  if [[ "${HELM_REPOS_UNREACHABLE:-0}" == "1" ]]; then
    result 2 "validate:helm-deps (HELM_REPOS_UNREACHABLE=1)"
  else
    deps_fail=0
    for chart in loopzero-api loopzero-app-operator loopzero-cloud loopzero-controller loopzero-crds \
                 loopzero-dashboard loopzero-platform loopzero-platform-operator \
                 loopzero-preview-operator loopzero-tenant-controller loopzero-tenant-operator; do
      cd="${REPO_ROOT}/charts/$chart"
      [[ -d "$cd" ]] || continue
      if ! (cd "$cd" && helm dependency build >/dev/null 2>&1); then
        deps_fail=$((deps_fail + 1))
      fi
    done
    if (( deps_fail == 0 )); then
      result 0 "validate:helm-deps (umbrella charts buildable)"
    else
      result 1 "validate:helm-deps ($deps_fail charts failed)"
    fi
  fi
else
  result 2 "validate:helm-deps (helm missing)"
fi

# Gate 10: operators envtest (only when KUBEBUILDER_ASSETS set).
if [[ -n "${KUBEBUILDER_ASSETS:-}" ]]; then
  if (( QUICK == 1 )); then
    result 2 "operators:envtest (--quick skipped)"
  else
    if (cd "${REPO_ROOT}/operators" && go test -short -timeout=300s ./... >/dev/null 2>&1); then
      result 0 "operators:envtest"
    else
      result 1 "operators:envtest"
    fi
  fi
else
  result 2 "operators:envtest (KUBEBUILDER_ASSETS not set)"
fi

# === Live gates STATUS reporting (NOT executed) ===
echo ""
echo "[validate-matrix] live gates (status only — not executed):"
echo "  STATUS  k3d-virgin-install: run via 'task parity:k3d-virgin' (\$0, ~10min)"
# cell-2-activation-plan T0.3 / matrix entry 156: discovery invariant — needs
# live k3d 4-cluster topology + loopzero-api + Prometheus port-forward to assert.
echo "  STATUS  cell-2 discovery count >= 3: run via 'bash infra/tests/parity/cell2-activation-gates.sh --gates c1' after task k3d:heal (matrix #156)"
# cell-2-activation-plan T1.5 / Phase 1 master gates: 12 ADR conditions verified.
echo "  STATUS  cell-2 activation gates 12/12: run via 'bash infra/tests/parity/cell2-activation-gates.sh' after Phase 0 (matrix #157)"
echo "  STATUS  cell-2 canary stage 1: 1h soak (cloud, awaiting Phase 5 T5.6)"
echo "  STATUS  cell-2 canary stage 2: 24h soak (awaiting T5.7)"
echo "  STATUS  prod canary stage 4: 7d soak (awaiting T7.1, customer opt-in)"
echo "  STATUS  prod canary stage 5: 7d holds × 3 (awaiting T7.2)"

echo ""
echo "[validate-matrix] summary"
echo "  PASS: $pass"
echo "  FAIL: $fail"
echo "  SKIP: $skip"

if (( fail > 0 )); then
  echo "[validate-matrix] FAIL — $fail gate(s) failed"
  exit 1
fi

echo "[validate-matrix] GREEN (offline gates)"
exit 0
