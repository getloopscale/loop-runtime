#!/usr/bin/env bash
# C1 check-sprint category: helmfile rendering canonical state.
# Per T4.1 of plan 2026-06-11-cleanroom-canonical-5-defense-layers v1.2.
# Hermetic (no kubectl) — runs all 5 Phase 3 hermetic gates which validate
# per-env Helmfile overlays render canonical URL/secret/pipeline contracts.

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../../.." && pwd)"

PASS=0; FAIL=0
GATES=(
  "ory-hydra-canonical-urls"
  "loopzero-cloud-kratos-url-split"
  "promtail-canonical-pipeline"
  "loopzero-api-spki-not-hardcoded"
  "loopzero-external-secrets-github-canonical"
)
for g in "${GATES[@]}"; do
  if bash "$REPO_ROOT/infra/tests/helm/${g}_test.sh" >/dev/null 2>&1; then
    PASS=$((PASS+1))
  else
    FAIL=$((FAIL+1))
    echo "  c1 sub-FAIL: ${g}_test.sh" >&2
  fi
done

if [[ "$FAIL" -eq 0 ]]; then
  echo "PASS c1-helmfile-rendering ($PASS/$((PASS+FAIL)) gates GREEN)"
else
  echo "FAIL c1-helmfile-rendering ($FAIL of $((PASS+FAIL)) gates RED)"
  exit 1
fi
