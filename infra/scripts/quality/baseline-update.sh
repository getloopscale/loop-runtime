#!/usr/bin/env bash
# Update coverage baseline JSON from current coverage measurement.
# Plan 2026-05-21-comprehensive-quality-gates v1.1 T1.2.
#
# Behavior:
#   - Reads /tmp/coverage.out (Go) + /tmp/cli-coverage.lcov (TS)
#   - Computes per-package coverage %
#   - Preserves min_required for existing packages (idempotent)
#   - Sets initial min_required = floor(coverage - 5pp), min 50, max 90
#   - Critical packages (D11 list) get min_required=90 (no auto-downgrade)
#   - Writes infra/quality/coverage-baseline.json
#
# Used by:
#   - .github/workflows/baseline-update.yml (post-merge to main, cron)
#   - Manual operator runs (after large coverage push)
#
# Env vars:
#   GO_PROFILE         — Go coverage profile (default: /tmp/coverage.out)
#   CLI_LCOV           — CLI lcov (default: /tmp/cli-coverage.lcov)
#   BASELINE_FILE      — output JSON (default: infra/quality/coverage-baseline.json)
#   DRY_RUN            — if "1", print to stdout instead of writing file

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
GO_PROFILE="${GO_PROFILE:-/tmp/coverage.out}"
CLI_LCOV="${CLI_LCOV:-/tmp/cli-coverage.lcov}"
BASELINE_FILE="${BASELINE_FILE:-$REPO_ROOT/infra/quality/coverage-baseline.json}"
DRY_RUN="${DRY_RUN:-0}"

# Critical packages (per ADR D11) — min_required pinned to 90, never downgraded
CRITICAL_PACKAGES=(
  "github.com/loopzero/loopzero/pkg/errors"
  "github.com/loopzero/loopzero/pkg/errors/http"
  "github.com/loopzero/loopzero/pkg/jwt"
  "github.com/loopzero/loopzero/pkg/statemachine"
  "github.com/loopzero/loopzero/pkg/naming"
  "github.com/loopzero/loopzero/pkg/lock"
  "github.com/loopzero/loopzero/pkg/enforcement"
  "github.com/loopzero/loopzero/pkg/id"
  "github.com/loopzero/loopzero/pkg/circuitbreaker"
  "github.com/loopzero/loopzero/pkg/healthcheck"
  "github.com/loopzero/loopzero/pkg/conditions"
  "github.com/loopzero/loopzero/pkg/logging"
)

is_critical() {
  local pkg="$1"
  for c in "${CRITICAL_PACKAGES[@]}"; do
    [ "$pkg" = "$c" ] && return 0
  done
  return 1
}

# Compute per-package coverage % from Go profile by parsing the profile
# directly (NOT via `go tool cover -func` which requires source files to
# exist on disk — fails on fixtures + CI runners without full checkout).
#
# Profile format:
#   mode: <atomic|count|set>
#   <file>:<startLine>.<col>,<endLine>.<col> <numStatements> <count>
#   ...
#
# Per-file coverage = sum(numStatements where count>0) / sum(numStatements)
# Per-package coverage = sum across files in same dir / count across files
compute_go_coverage_per_package() {
  if [ ! -s "$GO_PROFILE" ]; then
    echo "[baseline-update] Go profile $GO_PROFILE missing/empty — skipping" >&2
    return
  fi
  awk '
    NR == 1 { next }  # skip mode header
    {
      # Field 1: <file>:<range>
      split($1, parts, ":")
      file = parts[1]
      if (file == "") next
      stmts = $2 + 0
      count = $3 + 0
      file_stmts[file] += stmts
      if (count > 0) {
        file_covered[file] += stmts
      } else {
        # Ensure key exists for files with no covered statements
        if (!(file in file_covered)) file_covered[file] = 0
      }
    }
    END {
      # Aggregate per-package (dirname of file)
      for (f in file_stmts) {
        n = split(f, p, "/")
        pkg = ""
        for (i = 1; i < n; i++) {
          if (pkg == "") pkg = p[i]
          else pkg = pkg "/" p[i]
        }
        pkg_stmts[pkg] += file_stmts[f]
        pkg_covered[pkg] += file_covered[f]
      }
      for (pkg in pkg_stmts) {
        if (pkg_stmts[pkg] > 0) {
          pct = (pkg_covered[pkg] / pkg_stmts[pkg]) * 100
        } else {
          pct = 0
        }
        printf "%s|%.1f\n", pkg, pct
      }
    }
  ' "$GO_PROFILE"
}

# Compute per-file CLI coverage from lcov
# Aggregates by directory (1 level above file)
compute_cli_coverage_per_package() {
  if [ ! -s "$CLI_LCOV" ]; then
    echo "[baseline-update] CLI lcov $CLI_LCOV missing/empty — skipping" >&2
    return
  fi
  # lcov format:
  #   SF:src/cli/auth/login.ts
  #   ...
  #   LF:100  (lines found)
  #   LH:80   (lines hit)
  #   end_of_record
  awk '
    /^SF:/ { file = substr($0, 4); lf = 0; lh = 0 }
    /^LF:/ { lf = substr($0, 4) + 0 }
    /^LH:/ { lh = substr($0, 4) + 0 }
    /^end_of_record/ {
      if (file == "") next
      # Package = dirname of file
      n = split(file, parts, "/")
      pkg = ""
      for (i = 1; i < n; i++) {
        if (pkg == "") pkg = parts[i]
        else pkg = pkg "/" parts[i]
      }
      if (lf > 0) {
        pct = (lh / lf) * 100
      } else {
        pct = 0
      }
      pkg_sum[pkg] += pct
      pkg_count[pkg]++
      file = ""
    }
    END {
      for (pkg in pkg_sum) {
        avg = pkg_sum[pkg] / pkg_count[pkg]
        printf "%s|%.1f\n", pkg, avg
      }
    }
  ' "$CLI_LCOV"
}

# Read existing baseline (if any) to preserve min_required
read_existing_min_required() {
  local pkg="$1"
  if [ ! -f "$BASELINE_FILE" ]; then
    echo ""
    return
  fi
  python3 -c "
import json, sys
try:
    with open('$BASELINE_FILE') as f:
        d = json.load(f)
    pkgs = d.get('packages', {})
    if '$pkg' in pkgs:
        print(pkgs['$pkg'].get('min_required', ''))
except Exception:
    pass
" 2>/dev/null
}

# Decide min_required for a package
compute_min_required() {
  local pkg="$1"
  local current_pct="$2"
  local existing
  existing=$(read_existing_min_required "$pkg")

  if is_critical "$pkg"; then
    # Critical: always 90 (D11)
    echo "90"
    return
  fi

  if [ -n "$existing" ]; then
    # Preserve existing min_required (no auto-downgrade on regression)
    echo "$existing"
    return
  fi

  # Initial: floor(current - 5), clamped [50, 90]
  awk -v c="$current_pct" 'BEGIN {
    v = c - 5
    if (v < 50) v = 50
    if (v > 90) v = 90
    printf "%d", v
  }'
}

echo "[baseline-update] computing per-package coverage…" >&2

# Collect all packages
declare -A PKG_COVERAGE=()

while IFS='|' read -r pkg pct; do
  [ -z "$pkg" ] && continue
  PKG_COVERAGE["$pkg"]="$pct"
done < <(compute_go_coverage_per_package)

while IFS='|' read -r pkg pct; do
  [ -z "$pkg" ] && continue
  PKG_COVERAGE["$pkg"]="$pct"
done < <(compute_cli_coverage_per_package)

# Defensive check for empty associative array under set -u
# (bash does NOT support ${#array[@]:-default} syntax)
set +u
PKG_COUNT=${#PKG_COVERAGE[@]}
set -u
if [ "$PKG_COUNT" -eq 0 ]; then
  echo "[baseline-update] no packages discovered — exit 0 (nothing to update)" >&2
  exit 0
fi

# Build JSON
TMPFILE="$(mktemp -t baseline-XXXXXX.json)"
trap 'rm -f "$TMPFILE"' EXIT

{
  echo "{"
  echo '  "schema_version": "1.0",'
  echo "  \"generated_at\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\","
  echo "  \"critical_packages\": ["
  for i in "${!CRITICAL_PACKAGES[@]}"; do
    if [ "$i" -lt $((${#CRITICAL_PACKAGES[@]} - 1)) ]; then
      printf '    "%s",\n' "${CRITICAL_PACKAGES[$i]}"
    else
      printf '    "%s"\n' "${CRITICAL_PACKAGES[$i]}"
    fi
  done
  echo "  ],"
  echo '  "packages": {'

  # Sort packages for stable output
  pkgs_sorted=()
  while IFS= read -r p; do
    pkgs_sorted+=("$p")
  done < <(printf '%s\n' "${!PKG_COVERAGE[@]}" | sort)

  total=${#pkgs_sorted[@]}
  i=0
  for pkg in "${pkgs_sorted[@]}"; do
    i=$((i + 1))
    pct="${PKG_COVERAGE[$pkg]}"
    min_req=$(compute_min_required "$pkg" "$pct")
    if [ "$i" -lt "$total" ]; then
      printf '    "%s": { "line": %s, "min_required": %s },\n' "$pkg" "$pct" "$min_req"
    else
      printf '    "%s": { "line": %s, "min_required": %s }\n' "$pkg" "$pct" "$min_req"
    fi
  done

  echo "  }"
  echo "}"
} > "$TMPFILE"

# Sanity: validate JSON
python3 -c "import json; json.load(open('$TMPFILE'))" 2>/dev/null || {
  echo "[baseline-update] ERROR: generated JSON is invalid" >&2
  cat "$TMPFILE" >&2
  exit 1
}

if [ "$DRY_RUN" = "1" ]; then
  cat "$TMPFILE"
else
  mkdir -p "$(dirname "$BASELINE_FILE")"
  mv "$TMPFILE" "$BASELINE_FILE"
  trap - EXIT
  echo "[baseline-update] wrote $BASELINE_FILE (${#PKG_COVERAGE[@]} packages)" >&2
fi
