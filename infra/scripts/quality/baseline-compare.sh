#!/usr/bin/env bash
# Baseline regression gate — fail PR if any package coverage drops below
# its baseline.min_required (with SLACK_PP slack tolerance).
# Plan 2026-05-21-comprehensive-quality-gates v1.1 T2.2 + EC-2.
#
# Env vars:
#   BASELINE_FILE          — checked-in baseline (default: infra/quality/coverage-baseline.json)
#   CURRENT_FILE           — current PR coverage JSON (same schema, packages only)
#   DEFAULT_MIN            — min_required for new packages not in baseline (default: 80)
#   SLACK_PP               — slack tolerance in percentage points (default: 3)
#   DEFER_LABEL_PRESENT    — "1" if PR has `coverage-baseline-defer` label (EC-2)
#   DEFER_REASON           — non-empty string with reason (EC-2 guard against blank label)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
BASELINE_FILE="${BASELINE_FILE:-$REPO_ROOT/infra/quality/coverage-baseline.json}"
CURRENT_FILE="${CURRENT_FILE:-/tmp/current-coverage.json}"
DEFAULT_MIN="${DEFAULT_MIN:-80}"
SLACK_PP="${SLACK_PP:-3}"
DEFER_LABEL_PRESENT="${DEFER_LABEL_PRESENT:-0}"
DEFER_REASON="${DEFER_REASON:-}"

# EC-2 escape valve: defer label + reason → PASS (records deferral)
if [ "$DEFER_LABEL_PRESENT" = "1" ]; then
  if [ -n "$DEFER_REASON" ]; then
    echo "[baseline-compare] EC-2: coverage-baseline-defer label active"
    echo "[baseline-compare] reason: $DEFER_REASON"
    echo "[baseline-compare] PASS — gate deferred per EC-2 (7-day TTL applies)"
    echo "[baseline-compare] NOTE: bot will open follow-up issue for coverage push"
    exit 0
  else
    echo "[baseline-compare] EC-2 guard: defer label present but DEFER_REASON empty" >&2
    echo "[baseline-compare] add 'Defer reason: <explanation>' to PR body" >&2
    exit 1
  fi
fi

if [ ! -f "$BASELINE_FILE" ]; then
  echo "[baseline-compare] ERROR: baseline file not found: $BASELINE_FILE" >&2
  exit 1
fi

if [ ! -f "$CURRENT_FILE" ]; then
  echo "[baseline-compare] ERROR: current coverage file not found: $CURRENT_FILE" >&2
  exit 1
fi

# Python comparison engine (avoid shell float math)
python3 - "$BASELINE_FILE" "$CURRENT_FILE" "$DEFAULT_MIN" "$SLACK_PP" <<'PY'
import json
import sys

baseline_path, current_path, default_min, slack_pp = sys.argv[1:]
default_min = float(default_min)
slack_pp = float(slack_pp)

with open(baseline_path) as f:
    baseline = json.load(f)
with open(current_path) as f:
    current = json.load(f)

baseline_pkgs = baseline.get("packages", {})
current_pkgs = current.get("packages", {})

regressions = []
for pkg, info in current_pkgs.items():
    current_line = float(info.get("line", 0))
    if pkg in baseline_pkgs:
        b = baseline_pkgs[pkg]
        min_req = float(b.get("min_required", default_min))
        baseline_line = float(b.get("line", current_line))
        # Effective floor = max(min_required, baseline_line - slack)
        floor = max(min_req, baseline_line - slack_pp)
        if current_line < floor:
            regressions.append({
                "package": pkg,
                "baseline": baseline_line,
                "min_required": min_req,
                "current": current_line,
                "floor": floor,
                "delta_from_floor": current_line - floor,
            })
    else:
        # New package — apply DEFAULT_MIN
        if current_line < default_min:
            regressions.append({
                "package": pkg,
                "baseline": None,
                "min_required": default_min,
                "current": current_line,
                "floor": default_min,
                "delta_from_floor": current_line - default_min,
                "note": "NEW package, no baseline",
            })

if regressions:
    print(f"[baseline-compare] FAIL — {len(regressions)} package(s) below floor:", file=sys.stderr)
    for r in regressions:
        note = f" ({r['note']})" if r.get("note") else ""
        print(
            f"  {r['package']}: current={r['current']:.1f}% < floor={r['floor']:.1f}% "
            f"(min_required={r['min_required']:.0f}, baseline={r['baseline']}){note}",
            file=sys.stderr,
        )
    sys.exit(1)

print(f"[baseline-compare] PASS — all {len(current_pkgs)} packages above floor (slack={slack_pp}pp)")
PY
