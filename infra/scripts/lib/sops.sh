#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopRuntime Authors
#
# Plan v1.2 T7.1 / D6 — SOPS + age wrappers for secrets-in-git encryption.
#
# Per ADR D6 (REJECT Infisical, ADOPT SOPS):
#   - LoopRuntime already has External Secrets Operator (ESO) for runtime sync.
#   - SOPS + age fills the gap of secrets-IN-GIT safely encrypted at rest.
#   - No new stateful service (vs Vault/Infisical which need their own DB).
#
# This lib provides:
#   sops_encrypt FILE  — encrypt a YAML file in-place (per .sops.yaml rules)
#   sops_decrypt FILE  — decrypt to stdout
#   sops_edit FILE     — interactive edit (sops opens $EDITOR with cleartext)
#   sops_check FILE    — verify file is properly encrypted (no plaintext leaked)
#
# Per EC-24: prod isolation via .sops.yaml creation_rules per path.
# Per EC-19: the real CI must inject SOPS_AGE_KEY env (GitHub Actions removed
# 2026-05-23 — the prior sops_audit_ci workflow check was dropped).
#
# Usage:
#   source infra/scripts/lib/sops.sh
#   sops_encrypt infra/secrets/drill/cosign-key.yaml
#   sops_decrypt infra/secrets/drill/cosign-key.yaml > /tmp/decrypted.yaml

set -euo pipefail

# --- Pre-flight check ---
sops_available() {
  command -v sops >/dev/null 2>&1 && command -v age >/dev/null 2>&1
}

sops_assert_available() {
  if ! sops_available; then
    echo "[sops] ERROR: sops + age required but not installed." >&2
    echo "[sops]   Install:" >&2
    echo "[sops]     brew install sops age           # macOS" >&2
    echo "[sops]     apt-get install age && curl -LO https://github.com/getsops/sops/releases/latest/download/sops-v3.10.2.linux.amd64 && sudo install sops-v3.10.2.linux.amd64 /usr/local/bin/sops  # Linux" >&2
    echo "[sops]   OR: task install:sops" >&2
    exit 1
  fi
}

# --- Encryption ---
sops_encrypt() {
  local FILE="${1:?sops_encrypt requires file path}"
  sops_assert_available
  if [[ ! -f "$FILE" ]]; then
    echo "[sops] ERROR: file not found: $FILE" >&2
    return 1
  fi
  # Check .sops.yaml exists at repo root or in parent dirs
  local SOPS_CONFIG
  SOPS_CONFIG="$(find . -maxdepth 3 -name '.sops.yaml' -type f 2>/dev/null | head -1)"
  if [[ -z "$SOPS_CONFIG" ]]; then
    echo "[sops] ERROR: .sops.yaml not found. Create one with creation_rules + age recipients first." >&2
    return 1
  fi
  echo "[sops] encrypting $FILE (rules from $SOPS_CONFIG)..."
  sops --encrypt --in-place "$FILE"
}

# --- Decryption ---
sops_decrypt() {
  local FILE="${1:?sops_decrypt requires file path}"
  sops_assert_available
  sops --decrypt "$FILE"
}

# --- Interactive edit ---
sops_edit() {
  local FILE="${1:?sops_edit requires file path}"
  sops_assert_available
  sops "$FILE"
}

# --- Audit: verify encrypted ---
sops_check() {
  local FILE="${1:?sops_check requires file path}"
  if grep -qE "^sops:" "$FILE" 2>/dev/null; then
    echo "[sops] ✓ $FILE is encrypted"
    return 0
  else
    echo "[sops] ✗ $FILE appears to be PLAINTEXT — encrypt it before commit" >&2
    return 1
  fi
}

# NOTE: sops_audit_ci() (audited .github/workflows for SOPS_AGE_KEY, EC-19) was
# removed 2026-05-23 — GitHub Actions was deleted; the function scanned a
# non-existent dir and had no callers. Re-add against the real CI if needed.

# Self-test if invoked directly (not sourced)
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  echo "infra/scripts/lib/sops.sh — SOPS wrapper library (sourced, not executed directly)"
  echo ""
  echo "Functions provided:"
  echo "  sops_available, sops_assert_available, sops_encrypt FILE,"
  echo "  sops_decrypt FILE, sops_edit FILE, sops_check FILE"
  echo ""
  echo "Pre-flight:"
  if sops_available; then
    echo "  ✓ sops + age binaries present"
  else
    echo "  ✗ sops or age missing — see install instructions above"
    exit 1
  fi
fi
