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

# Gate 1+2: shellcheck (if installed) + bash -n
for script in "${PARITY_DIR}"/*.sh; do
  name="$(basename "$script")"

  # Gate 1: shellcheck
  if command -v shellcheck >/dev/null 2>&1; then
    if shellcheck "$script" >/dev/null 2>&1; then
      result 0 "shellcheck:$name"
    else
      if (( STRICT )); then
        shellcheck "$script" 2>&1 | head -5
        result 1 "shellcheck:$name"
      else
        result 2 "shellcheck:$name (warnings, non-strict)"
      fi
    fi
  else
    result 2 "shellcheck:$name (not installed)"
  fi

  # Gate 2: syntax check
  bash -n "$script" 2>/dev/null
  result $? "bash-syntax:$name"
done

# Gate 3: matrix integrity audit
bash "${PARITY_DIR}/audit-regression-matrix.sh" >/dev/null 2>&1
result $? "matrix-integrity-audit"

# Gate 3b: golden test for the audit script itself (catches audit regressions)
bash "${PARITY_DIR}/audit-golden-test.sh" >/dev/null 2>&1
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
