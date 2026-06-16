#!/usr/bin/env bash
# T4.1 of plan 2026-06-15-code-review-49-findings-remediation (R1-F5 HIGH).
#
# Shell-lint advisory step invoked from `task quality:gates`. Conditional:
# runs shellcheck across the operator/CI script set WHEN the tool is
# installed; emits WARN + exits 0 when absent so first-landing of this
# step doesn't break unrelated CI flows. Install via `sudo apt install
# shellcheck` (Ubuntu) or `brew install shellcheck` (macOS). Once
# universally installed across dev + CI, drop the `command -v` guard
# OR convert this step to a hard gate.
#
# Extracted from `taskfiles/quality.yml` after the inline `bash -c '...'`
# form broke YAML parsing (shell metachars + nested quotes). Re-homing
# as a script file is the canonical pattern + matches the "no inline
# multi-statement bash in taskfiles" convention already used elsewhere.

set -euo pipefail

if ! command -v shellcheck >/dev/null 2>&1; then
  echo "[shellcheck] WARN: shellcheck not installed — gate is advisory-only."
  echo "[shellcheck] Install via apt/brew/go to enable enforcement (R1-F5 T4.1)."
  exit 0
fi

echo "[shellcheck] running across infra/scripts/local + infra/tests/check-*.sh + infra/tests/e2e/dev-public-*.sh"

# SC1091 — Not following sourced files (dynamic source paths in dogfood wrappers).
# SC2155 — Declare and assign separately (style; many existing files use combined).
# SC2086 — Double-quote to prevent globbing (intentional in some operator scripts).
# SC2046 — Quote to prevent word splitting (intentional in some operator scripts).
if ! shellcheck \
    -e SC1091,SC2155,SC2086,SC2046 \
    infra/scripts/local/*.sh \
    infra/tests/check-*-syntax_test.sh \
    infra/tests/e2e/dev-public-*.sh 2>&1; then
  echo "[shellcheck] violations — failing gate"
  exit 1
fi

echo "[shellcheck] clean"
