#!/usr/bin/env bash
# CHG-2026-04-30-INFRA-PARITY — parity-suite preflight gate.
#
# Runs every offline-checkable validation across the parity test suite, in
# parallel where possible. Designed to run in CI as a fast pre-merge gate:
# total runtime <30s, no cloud cost, no cluster mutation.
#
# Checks performed:
#   1) shellcheck on every parity .sh (skipped if shellcheck not installed)
#   2) bash -n syntax on every parity .sh
#   3) audit-regression-matrix.sh — matrix integrity, exit 0 expected
#   4) droplet-install.sh --dry-run with mock creds (offline preflight path)
#   5) doks-install.sh --dry-run with mock creds (offline preflight path)
#   6) k3d-virgin-install.sh — script structure check (does NOT run live)
#
# Usage:
#   bash infra/tests/parity/preflight.sh
#   bash infra/tests/parity/preflight.sh --strict  # fail on shellcheck warnings
#
# Exit codes:
#   0 — all gates GREEN
#   1 — at least one gate FAILED
#
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
PARITY_DIR="${REPO_ROOT}/infra/tests/parity"
STRICT=0

for arg in "$@"; do
  case "$arg" in
    --strict) STRICT=1 ;;
    -h|--help) sed -n '2,25p' "$0"; exit 0 ;;
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

echo "[parity-preflight] gates:"

# Gate 1+2: shellcheck (if installed OR via Docker) + bash -n
#
# Per memory `feedback_tool_unavailability_alternative_routes` (Ralph
# iter 2026-05-15): "Antes de marcar 'tool unavailable' como Ralph-
# immutable host limitation, enumerar rotas alternativas". Docker
# `koalaman/shellcheck:stable` is the canonical alternative when the
# binary is not native; it is pinned + reproducible across machines.
# Disable via `SHELLCHECK_NO_DOCKER=1` if Docker is unavailable in CI.
declare -a SHELLCHECK_CMD
if command -v shellcheck >/dev/null 2>&1; then
  SHELLCHECK_CMD=(shellcheck)
  SHELLCHECK_MODE="native"
elif [[ -z "${SHELLCHECK_NO_DOCKER:-}" ]] && command -v docker >/dev/null 2>&1; then
  SHELLCHECK_CMD=(docker run --rm -v "${REPO_ROOT}:/mnt" -w /mnt koalaman/shellcheck:stable)
  SHELLCHECK_MODE="docker"
else
  SHELLCHECK_CMD=()
  SHELLCHECK_MODE="unavailable"
fi

for script in "${PARITY_DIR}"/*.sh; do
  name="$(basename "$script")"

  # Gate 1: shellcheck
  if [[ "$SHELLCHECK_MODE" != "unavailable" ]]; then
    # Make path relative to REPO_ROOT for the Docker volume mount.
    if [[ "$SHELLCHECK_MODE" == "docker" ]]; then
      rel_script="${script#"${REPO_ROOT}/"}"
    else
      rel_script="$script"
    fi
    if "${SHELLCHECK_CMD[@]}" "$rel_script" >/dev/null 2>&1; then
      result 0 "shellcheck:$name (${SHELLCHECK_MODE})"
    else
      if (( STRICT )); then
        "${SHELLCHECK_CMD[@]}" "$rel_script" 2>&1 | head -5
        result 1 "shellcheck:$name (${SHELLCHECK_MODE})"
      else
        result 2 "shellcheck:$name (warnings, non-strict)"
      fi
    fi
  else
    result 2 "shellcheck:$name (not installed + Docker unavailable)"
  fi

  # Gate 2: syntax check
  bash -n "$script" 2>/dev/null
  result $? "bash-syntax:$name"
done

# Gate 3: matrix integrity audit — migrated to the Go gate
# `loopzero-ops quality regression-matrix` (Track B TB5). The bash
# audit-regression-matrix.sh aborted at exit 2 under set -e (a helper return 2
# propagated as a fatal) so it never scanned — see
# finding-2026-05-24-audit-regression-matrix-set-e-abort. The Go gate has no
# such fragility.
( cd "${REPO_ROOT}/cmd/loopzero-ops" && go run . quality regression-matrix --root "${REPO_ROOT}" ) >/dev/null 2>&1
result $? "matrix-integrity-audit"

# Gate 3b: golden test for the audit logic itself (catches audit regressions).
# Now a Go unit test (TestGolden_*) — the faithful port of the deleted
# audit-golden-test.sh, folded into the gate's own package.
( cd "${REPO_ROOT}" && go test ./pkg/quality/regressionmatrix/... ) >/dev/null 2>&1
result $? "matrix-audit:golden-test"

# Gate 4: droplet --dry-run with mock creds
DIGITALOCEAN_ACCESS_TOKEN=mock DROPLET_SSH_KEY_FP=00:00:00:00 \
  bash "${PARITY_DIR}/droplet-install.sh" --dry-run >/dev/null 2>&1
result $? "droplet-install:--dry-run"

# Gate 5: doks --dry-run with mock creds
DIGITALOCEAN_ACCESS_TOKEN=mock \
  bash "${PARITY_DIR}/doks-install.sh" --dry-run >/dev/null 2>&1
result $? "doks-install:--dry-run"

# Gate 6: k3d-virgin script structure (script exists + contains expected step labels)
expect_steps=("step 1" "step 2" "step 3" "step 4" "DONE")
all_present=1
for step in "${expect_steps[@]}"; do
  if ! grep -qF "$step" "${PARITY_DIR}/k3d-virgin-install.sh"; then
    all_present=0
    break
  fi
done
result $((1 - all_present)) "k3d-virgin-install:structure"

# === Phase 6 T6.3 extensions ===
# Gate 7: ONE Pulumi project (closes T-1.2 condition + T2.2 sprint8-fixes
# Fix #10). After Phase 3 stack-rm completes, only infra/pulumi/ should
# remain. For now we count and warn if multiple exist (becomes hard
# fail post-Phase 3 commit).
pulumi_count=$(find "${REPO_ROOT}/infra" -maxdepth 2 -name 'Pulumi.yaml' -type f 2>/dev/null | wc -l)
if [[ $pulumi_count -le 1 ]]; then
  result 0 "pulumi:singleton (count=$pulumi_count)"
else
  # Phase 3 not yet executed; SKIP rather than FAIL so this gate doesn't
  # block CI before consolidation lands. Will flip to hard-fail in T3.3.
  result 2 "pulumi:singleton (count=$pulumi_count, Phase 3 pending)"
fi

# Gate 8: cells migration present + matches kubebuilder cell_id pattern.
# Closes T0.1 audit Gap 1 + T-1.3 condition C1 (migration must create
# table AND seed in one forward-only file).
# Numbered 000006 (renumbered from 000003 on 2026-04-30 — original number
# would have been silently skipped on every existing DB at version >= 5).
if [[ -f "${REPO_ROOT}/api/internal/db/migrations/000006_cells.up.sql" ]]; then
  if grep -qF "cell_id ~ '^cell-[a-z0-9-]+\$'" "${REPO_ROOT}/api/internal/db/migrations/000006_cells.up.sql"; then
    result 0 "cells-migration:pattern-matches-CC-kubebuilder"
  else
    result 1 "cells-migration:CHECK-constraint-missing-or-mismatched"
  fi
else
  result 1 "cells-migration:000006_cells.up.sql-missing"
fi

# Gate 9: ClusterCredential bootstrap lib present.
# Closes T0.1 audit Gap 2 + Action Item A2.
if [[ -x "${REPO_ROOT}/infra/helmfile/scripts/lib/clustercredential-create.sh" ]]; then
  result 0 "cc-bootstrap-lib:executable"
else
  result 1 "cc-bootstrap-lib:missing-or-not-executable"
fi

# Gate 10: wire-clusters.sh wires the CC lib (closes A3).
if grep -qF "clustercredential-create.sh" "${REPO_ROOT}/infra/helmfile/scripts/wire-clusters.sh" 2>/dev/null; then
  result 0 "wire-clusters:invokes-cc-bootstrap-lib"
else
  result 1 "wire-clusters:does-not-invoke-cc-bootstrap-lib"
fi

# Gate 11: multi-cell PromRule present + has the 7 alerts T5.4 specifies.
mc_rule="${REPO_ROOT}/infra/helm/monitoring/manifests/prometheusrule-multi-cell.yaml"
if [[ -f "$mc_rule" ]]; then
  alert_count=$(grep -cE "^        - alert: " "$mc_rule")
  if (( alert_count >= 7 )); then
    result 0 "multi-cell-alerts:rule-present (alerts=$alert_count)"
  else
    result 1 "multi-cell-alerts:expected ≥7 alerts, got $alert_count"
  fi
else
  result 1 "multi-cell-alerts:rule-missing"
fi

# Gate 12: admin pin endpoint present + has cell pattern validation.
pin_h="${REPO_ROOT}/api/internal/routes/admin_pin_cell.go"
if [[ -f "$pin_h" ]] && grep -qF '^cell-[a-z0-9-]+$' "$pin_h"; then
  result 0 "admin-pin-cell:handler-present-with-pattern"
else
  result 1 "admin-pin-cell:handler-missing-or-pattern-missing"
fi

echo
echo "[parity-preflight] summary"
echo "  PASS: $pass"
echo "  FAIL: $fail"
echo "  SKIP: $skip"

if (( fail > 0 )); then
  echo "[parity-preflight] FAIL — $fail gate(s) failed"
  exit 1
fi

echo "[parity-preflight] GREEN"
exit 0
