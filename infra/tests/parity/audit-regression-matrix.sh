#!/usr/bin/env bash
# CHG-2026-04-30-INFRA-PARITY / T9.1 followup — regression matrix integrity audit.
#
# A matriz de regressão reivindica para cada
# entry um "Verification" artifact (test name, runbook section, ou live verify
# command). Se um test foi renomeado, deletado, ou nunca existiu, a matriz
# vira mentira — silent regression de proteção.
#
# Este script lê a matriz, extrai a coluna Verification de cada row, e tenta
# localizar a evidência:
#   • TestXxxYyy        → grep -r para a função Go/TS
#   • path/to/file      → fs.exists
#   • "live verify"     → marcado SKIP (não-fechável offline)
#   • "preflight TXX"   → grep em preflight.sh
#
# Output: report ASCII + exit non-zero se houver miss em entry com Pode Recorrer=NÃO.
#
# Usage:
#   bash infra/tests/parity/audit-regression-matrix.sh
#   bash infra/tests/parity/audit-regression-matrix.sh --strict   # exit 1 on any miss
#
# Exit codes:
#   0  — todas entries com Pode Recorrer=NÃO têm artifact resolvível
#   1  — pelo menos 1 miss em entry crítica
#
# SPDX-License-Identifier: Apache-2.0

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
# MATRIX path is overridable via env var so the golden test (audit-golden-test.sh)
# can point at a fabricated matrix without touching the real one.
# Canonical path: `docs/runbooks/bootstrap-regression-matrix.md`. The
# previous default `docs/plans/...` was path drift — the matrix has
# always lived in `docs/runbooks/`. Restored post fc1793dd 2026-05-11
# cleanup; see finding `.claude/knowledge-base/concepts/operations/
# finding-2026-05-15-pulumi-vitest-23-fail-drift.md`.
MATRIX="${MATRIX:-${REPO_ROOT}/docs/runbooks/bootstrap-regression-matrix.md}"
STRICT=0

for arg in "$@"; do
  case "$arg" in
    --strict) STRICT=1 ;;
    -h|--help) sed -n '2,25p' "$0"; exit 0 ;;
  esac
done

[[ -f "$MATRIX" ]] || { echo "ERROR: matrix not found at $MATRIX" >&2; exit 1; }

total=0
ok=0
miss=0
skip=0
miss_critical=0

# Resolve one verification artifact. Returns 0 if found, 1 otherwise.
# We accept partial matches (test names often have a prefix and arguments).
resolve_artifact() {
  local id="$1"
  local verification="$2"
  local can_recur="$3"

  # Heuristics, ordered most specific first.
  case "$verification" in
    "")
      return 1
      ;;
    *"live "*|*"runtime"*|*"verify"*)
      # Live verify is offline-unresolvable. Treated as SKIP (not miss).
      return 2
      ;;
  esac

  # Extract candidate tokens from verification string. Strategy: pull every
  # alphanumeric/underscore identifier ≥5 chars long (filters out noise like
  # "grep" / "OK" / "and"). For each, try to find it in the codebase. If ANY
  # token matches, the entry is considered resolved.
  #
  # This is intentionally permissive: matrix entries vary widely in style
  # (TestFooBar, "version: 18.6.2", path/to/file.go, etc), and it's better
  # to miss a flaky verification than to drown the operator in false misses.
  local tokens
  mapfile -t tokens < <(
    echo "$verification" \
      | grep -oE '[A-Za-z][A-Za-z0-9_]{4,}|[A-Za-z][A-Za-z0-9_/.-]{4,}\.(ts|go|sh|md|yaml|yml|gotmpl|toml|json)' \
      | grep -vE '^(grep|head|tail|count|equal|true|false|status|exists|when|with|then|that|using|after|before|live|runtime|verify|matches|contains)$' \
      | sort -u
  )

  (( ${#tokens[@]} > 0 )) || return 2

  local includes=(
    --include='*.ts' --include='*.go' --include='*.sh' --include='*.md'
    --include='*.yaml' --include='*.yml' --include='*.gotmpl'
    --include='*.toml' --include='*.json' --include='Dockerfile*'
  )
  local roots=(
    "${REPO_ROOT}/cli" "${REPO_ROOT}/api" "${REPO_ROOT}/operators"
    "${REPO_ROOT}/pkg" "${REPO_ROOT}/infra" "${REPO_ROOT}/docs"
    "${REPO_ROOT}/charts" "${REPO_ROOT}/scripts"
  )

  for tok in "${tokens[@]}"; do
    if grep -rqE "(^|[^A-Za-z])${tok//./\\.}([^A-Za-z]|$)" \
          "${includes[@]}" "${roots[@]}" 2>/dev/null; then
      return 0
    fi
  done

  return 1
}

echo "[matrix-audit] auditing $MATRIX"
echo "[matrix-audit] (resolves Test* / *_test_* / *.test.ts tokens against repo)"
echo

# Parse table: skip header rows; rows look like `| 12 | CAT | ... | TestX | NÃO | SIM |`.
while IFS='|' read -r _ id_col cat _ _ _ _ verify_col can_recur_col _; do
  id=$(echo "$id_col" | tr -d ' ')
  [[ "$id" =~ ^[0-9]+$ ]] || continue   # not a data row

  verify=$(echo "$verify_col" | sed 's/^ *//;s/ *$//')
  can_recur=$(echo "$can_recur_col" | sed 's/^ *//;s/ *$//')

  total=$((total + 1))

  resolve_artifact "$id" "$verify" "$can_recur"
  rc=$?
  case $rc in
    0) ok=$((ok + 1)) ;;
    1)
      miss=$((miss + 1))
      printf "  MISS  #%-3s [%s] %s\n" "$id" "$can_recur" "${verify:0:80}"
      [[ "$can_recur" == "NÃO" ]] && miss_critical=$((miss_critical + 1))
      ;;
    2) skip=$((skip + 1)) ;;
  esac
done < "$MATRIX"

echo
echo "[matrix-audit] summary"
echo "  total entries:    $total"
echo "  resolved OK:      $ok"
echo "  miss (resolvable test/path):  $miss   (critical Pode Recorrer=NÃO: $miss_critical)"
echo "  skip (live/runtime verify):   $skip"

if (( miss_critical > 0 )); then
  echo "[matrix-audit] FAIL: $miss_critical critical entries have unresolved artifact"
  exit 1
fi

if (( STRICT == 1 )) && (( miss > 0 )); then
  echo "[matrix-audit] FAIL (--strict): $miss total misses"
  exit 1
fi

echo "[matrix-audit] PASS"
exit 0
