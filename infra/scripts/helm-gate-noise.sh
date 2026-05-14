#!/usr/bin/env bash
# infra/scripts/helm-gate-noise.sh
#
# Plan 2026-05-15-close-all-gaps T4.1 — measure Helm gate noise.
#
# READS a JSON array of GitHub Actions workflow runs from stdin and
# emits a metric block with the count of failures per Helm gate in
# the configured rolling window. Pure script — no `gh` dependency.
# The wrapper that calls `gh run list --json ...` is responsible for
# producing the input; this script ONLY counts.
#
# Input shape (compatible with `gh run list --json conclusion,name,createdAt`):
#   [
#     {"name": "chartsnap ...", "conclusion": "failure", "createdAt": "2026-05-13T10:00:00Z"},
#     ...
#   ]
#
# Output: a JSON object with metadata + per-gate failure counts.
#
# Usage:
#   gh run list --workflow=helm-charts.yml --limit 100 \
#     --json conclusion,name,createdAt \
#     | bash helm-gate-noise.sh --window-days 14
#
#   bash helm-gate-noise.sh --window-days 14 --reference 2026-05-15T00:00:00Z < fixture.json
#
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

die() { printf 'error: %s\n' "$*" >&2; exit 1; }

usage() {
  cat >&2 <<'EOF'
helm-gate-noise.sh — count Helm gate failures in a rolling window.

Required:
  --window-days <N>            window size in days (e.g. 14)

Optional:
  --reference <ISO8601-UTC>    window end (default: now UTC)

Reads JSON workflow-runs array from stdin.
Emits JSON metric block on stdout.
EOF
  exit 1
}

WINDOW_DAYS=""
REFERENCE=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --window-days) WINDOW_DAYS="$2"; shift 2 ;;
    --reference)   REFERENCE="$2"; shift 2 ;;
    -h|--help)     usage ;;
    *)             die "unknown flag: $1" ;;
  esac
done

[[ -n "$WINDOW_DAYS" ]] || usage
[[ "$WINDOW_DAYS" =~ ^[0-9]+$ ]] || die "--window-days must be an integer, got: $WINDOW_DAYS"
[[ -z "$REFERENCE" ]] && REFERENCE=$(date -u +%Y-%m-%dT%H:%M:%SZ)

# Use python for portable JSON parsing + ISO-8601 arithmetic; jq is
# available in CI but python is in every dev environment too.
#
# The heredoc below is fed to python's stdin as the SCRIPT body, so
# we capture the workflow-runs JSON to a tmpfile first and redirect
# it as stdin to the python invocation explicitly. Otherwise the
# heredoc would shadow the runs JSON.
STDIN_TMP=$(mktemp)
trap 'rm -f "$STDIN_TMP"' EXIT
cat >"$STDIN_TMP"

SCRIPT_TMP=$(mktemp)
trap 'rm -f "$STDIN_TMP" "$SCRIPT_TMP"' EXIT
cat >"$SCRIPT_TMP" <<'PY'
import json
import sys
from datetime import datetime, timedelta, timezone

window_days = int(sys.argv[1])
reference_str = sys.argv[2]

reference = datetime.fromisoformat(reference_str.replace("Z", "+00:00"))
horizon = reference - timedelta(days=window_days)

raw = sys.stdin.read().strip()
if not raw:
    runs = []
else:
    try:
        runs = json.loads(raw)
    except json.JSONDecodeError as e:
        print(f"error: invalid JSON on stdin: {e}", file=sys.stderr)
        sys.exit(2)

if not isinstance(runs, list):
    print("error: stdin must be a JSON array", file=sys.stderr)
    sys.exit(2)

# Stable gate key map (Plan T4.1 — 4 known gates per audit M06).
# Match the workflow `name:` field by substring; case-sensitive on
# the canonical labels used in .github/workflows/helm-charts.yml.
GATE_PATTERNS = {
    "conftest":    ["conftest"],
    "chartsnap":   ["chartsnap"],
    "helm_diff":   ["helm-diff", "helm diff"],
    "drift_check": ["drift", "Drift"],
}

counts = {gate: 0 for gate in GATE_PATTERNS}
runs_total = 0
runs_failure = 0

for run in runs:
    name = run.get("name", "")
    conclusion = run.get("conclusion", "")
    created_at = run.get("createdAt", "")
    if not created_at:
        continue
    try:
        ts = datetime.fromisoformat(created_at.replace("Z", "+00:00"))
    except ValueError:
        continue
    if ts < horizon or ts > reference:
        continue

    runs_total += 1
    if conclusion != "failure":
        continue
    runs_failure += 1

    for gate, patterns in GATE_PATTERNS.items():
        if any(p in name for p in patterns):
            counts[gate] += 1
            break  # first matching gate wins (no double-counting)

result = {
    "schema": "loopzero.helm-gate-noise.v1",
    "window_days": window_days,
    "reference_utc": reference_str,
    "horizon_utc": horizon.isoformat().replace("+00:00", "Z"),
    "runs_total": runs_total,
    "runs_failure": runs_failure,
    "gate_failures": counts,
    "noise_threshold_per_week": 1,
    "noise_status": "OK" if runs_failure <= window_days // 7 else "REVIEW",
}
print(json.dumps(result, indent=2))
PY

python3 "$SCRIPT_TMP" "$WINDOW_DAYS" "$REFERENCE" <"$STDIN_TMP"
