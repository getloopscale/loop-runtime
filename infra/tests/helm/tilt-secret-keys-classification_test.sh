#!/bin/bash
# Hermetic test: assert sensitive keys NEVER appear in runtimeConfig sections of values.yaml files.
# Per ADR D1 EC-2 (decisions/2026-06-11-tilt-yaml-configmap-mount.md): sensitive values must be in runtimeSecrets.
#
# This test prevents the regression where a developer accidentally adds a SECRET / PASSWORD / TOKEN /
# API_KEY / DATABASE_URL key to runtimeConfig (which renders into ConfigMap, world-readable to any pod).
#
# Wired in: task quality:gates (per rules/shell-scripts-allowlist.md)
# Allowlist entry: infra/tests/helm/tilt-secret-keys-classification_test.sh — T1.1 NEW

set -uo pipefail

WORKTREE_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
cd "$WORKTREE_ROOT"

SENSITIVE_PATTERNS=(
  "SECRET"
  "PASSWORD"
  "PRIVATE_KEY"
  "API_KEY"
  "TOKEN"
  "DATABASE_URL"
  "DSN"
  "WEBHOOK_SECRET"
  "STRIPE_API_KEY"
)

VALUES_FILES=(
  "infra/helm/loopzero-api/values.yaml"
  "infra/helm/loopzero-api/values-dev.yaml"
  "infra/helm/loopzero-cloud/values.yaml"
  "infra/helm/loopzero-cloud/values-dev.yaml"
)

FAIL_COUNT=0
CHECKED=0

for vf in "${VALUES_FILES[@]}"; do
  if [ ! -f "$vf" ]; then
    continue
  fi

  # Extract the runtimeConfig section (between `runtimeConfig:` and next top-level key)
  # Use python3 for safe YAML parsing.
  LEAKS=$(python3 - "$vf" <<'PYEOF'
import sys
import yaml

path = sys.argv[1]
SENSITIVE = ['SECRET', 'PASSWORD', 'PRIVATE_KEY', 'API_KEY', 'TOKEN', 'DATABASE_URL', 'DSN', 'WEBHOOK_SECRET', 'STRIPE_API_KEY']

try:
    with open(path) as f:
        # Comments may contain example keys — only parse actual yaml keys
        doc = yaml.safe_load(f)
    if not isinstance(doc, dict):
        sys.exit(0)
    runtime_config = doc.get('runtimeConfig')
    if not isinstance(runtime_config, dict):
        sys.exit(0)
    leaks = [k for k in runtime_config.keys() if any(s in k for s in SENSITIVE)]
    for leak in leaks:
        print(leak)
except Exception:
    sys.exit(0)
PYEOF
)

  CHECKED=$((CHECKED + 1))

  if [ -n "$LEAKS" ]; then
    while IFS= read -r key; do
      echo "❌ FAIL: $vf runtimeConfig leaks sensitive key '$key' — must be moved to runtimeSecrets"
      FAIL_COUNT=$((FAIL_COUNT + 1))
    done <<< "$LEAKS"
  fi
done

echo ""
echo "=== Result ==="
echo "  Files checked: $CHECKED"
echo "  Leaks detected: $FAIL_COUNT"

if [ "$FAIL_COUNT" -eq 0 ]; then
  echo "✅ tilt-secret-keys-classification: PASS — no sensitive keys in runtimeConfig"
  exit 0
else
  echo "❌ tilt-secret-keys-classification: $FAIL_COUNT leaks detected"
  echo "   Fix: move flagged keys from values.yaml runtimeConfig to runtimeSecrets section"
  exit 1
fi
