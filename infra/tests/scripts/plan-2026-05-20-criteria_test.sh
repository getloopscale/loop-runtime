#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Plan 2026-05-20 T2.5 ext — plan-specific criteria zero-count audit.
#
# Validates the 4 measurable plan criteria from
# docs/plans/2026-05-20-infra-standardization-and-secrets-plan.md
# Global DoD section. Each criterion is a "ZERO of X" assertion that
# can be mechanically verified.
#
# Why this gate matters:
#   - Plan v1.2 has measurable engineering criteria that should pass
#     hermetic check before claiming completion
#   - Without this gate, regression of any cleaned-up area is invisible
#     (e.g., someone re-adds 'http://localhost:' to a canonical values.yaml
#     defeating Phase 2 work)
#   - Locks in cleanup achievements as permanent guards
#
# Criteria checked:
#   C1. ZERO infra/helm/loopzero-*-operator-drill/ directories
#       (Phase 1 T1.1 — operator drill values relocated to layered)
#   C2. ZERO `{{ if eq .Environment.Name "drill" }}` in .gotmpl files
#       (Phase 2 T2.2 — 9 inline conditionals eliminated via layered values)
#   C3. ZERO http://localhost: in canonical infra/helm/*/values.yaml
#       (Phase 2 T2.3 — localhost hardcodes replaced with placeholders)
#   C4. infra/scripts/drill-* scripts within amended-scope limit (≤ 2)
#       (Phase 4 T4.3 amended via finding-2026-05-20-t4-3-scope-rethink:
#       drill-helmfile-bootstrap.sh + drill-mint-jwt.sh kept as
#       drill-DOKS-specific orchestrators; ALL OTHER 11 drill-* scripts
#       relocated to infra/scripts/dr/. Original "ZERO" criterion
#       sponsor-amended to "≤ 2 documented domain-specific")
#
# Exit codes:
#   0 — all criteria pass
#   1 — one or more criteria fail
#   5 — missing test dependency

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$ROOT"

log() { printf '[plan-criteria] %s\n' "$*" >&2; }

PASS=0
FAIL=0
FAIL_DETAIL=()

# C1 — ZERO infra/helm/loopzero-*-operator-drill/ directories
c1_count=$(find infra/helm -maxdepth 1 -name "loopzero-*-operator-drill" -type d 2>/dev/null | wc -l)
if [[ "$c1_count" -eq 0 ]]; then
  log "  [PASS] C1: ZERO loopzero-*-operator-drill/ directories"
  PASS=$((PASS + 1))
else
  log "  [FAIL] C1: $c1_count loopzero-*-operator-drill/ directories remain"
  FAIL=$((FAIL + 1))
  FAIL_DETAIL+=("C1: $(find infra/helm -maxdepth 1 -name 'loopzero-*-operator-drill' -type d)")
fi

# C2 — ZERO `{{ if eq .Environment.Name "drill" }}` in .gotmpl
c2_count=$( { grep -rE '\{\{\s*if eq \.Environment\.Name "drill"' infra/helmfile/*.gotmpl 2>/dev/null || true; } | wc -l)
if [[ "$c2_count" -eq 0 ]]; then
  log "  [PASS] C2: ZERO inline drill conditionals in .gotmpl"
  PASS=$((PASS + 1))
else
  log "  [FAIL] C2: $c2_count inline drill conditionals found"
  FAIL=$((FAIL + 1))
  FAIL_DETAIL+=("C2: $(grep -rE '\{\{\s*if eq \.Environment\.Name "drill"' infra/helmfile/*.gotmpl | head -3)")
fi

# C3 — ZERO http://localhost: in canonical infra/helm/*/values.yaml
# (canonical = values.yaml at chart root, NOT values.yaml.gotmpl which is
# helmfile-processed and may contain placeholders)
c3_count=$( { grep -E 'http://localhost:' infra/helm/*/values.yaml 2>/dev/null || true; } | wc -l)
if [[ "$c3_count" -eq 0 ]]; then
  log "  [PASS] C3: ZERO http://localhost: in canonical values.yaml"
  PASS=$((PASS + 1))
else
  log "  [FAIL] C3: $c3_count http://localhost: refs in canonical values.yaml"
  FAIL=$((FAIL + 1))
  FAIL_DETAIL+=("C3: $(grep -E 'http://localhost:' infra/helm/*/values.yaml | head -3)")
fi

# C4 — infra/scripts/drill-* ≤ 2 (per T4.3 amendment)
# Per finding-2026-05-20-t4-3-scope-rethink.md: drill-helmfile-bootstrap.sh
# (1109 LOC orchestrator with 11 LIVE patches) and drill-mint-jwt.sh
# (DOKS-specific JWT mint flow) are intentionally retained as drill-DOKS-
# specific. Original "ZERO" criterion amended to "≤ 2 documented".
c4_count=$(find infra/scripts -maxdepth 1 -name "drill-*" -type f 2>/dev/null | wc -l)
EXPECTED=(
  "infra/scripts/drill-helmfile-bootstrap.sh"
  "infra/scripts/drill-mint-jwt.sh"
)
if [[ "$c4_count" -le 2 ]]; then
  # Verify the 2 remaining are the expected amended-scope set
  unexpected=()
  while IFS= read -r f; do
    found=0
    for exp in "${EXPECTED[@]}"; do
      [[ "$f" == "$exp" ]] && found=1
    done
    [[ "$found" -eq 0 ]] && unexpected+=("$f")
  done < <(find infra/scripts -maxdepth 1 -name "drill-*" -type f)

  if [[ ${#unexpected[@]} -eq 0 ]]; then
    log "  [PASS] C4: infra/scripts/drill-* count=$c4_count (≤2, T4.3 amended scope)"
    PASS=$((PASS + 1))
  else
    log "  [FAIL] C4: unexpected drill-* scripts present:"
    for u in "${unexpected[@]}"; do log "    - $u"; done
    FAIL=$((FAIL + 1))
    FAIL_DETAIL+=("C4 unexpected: ${unexpected[*]}")
  fi
else
  log "  [FAIL] C4: $c4_count drill-* scripts (>2, T4.3 amended scope violated)"
  FAIL=$((FAIL + 1))
  FAIL_DETAIL+=("C4: $c4_count > 2")
fi

echo ""
log "Plan-specific criteria: $PASS PASS / $FAIL FAIL (of 4 measurable)"

if [[ "$FAIL" -gt 0 ]]; then
  echo ""
  log "Failures:"
  for d in "${FAIL_DETAIL[@]}"; do
    log "  $d"
  done
  exit 1
fi

log "All 4 measurable plan-specific criteria PASS"
exit 0
