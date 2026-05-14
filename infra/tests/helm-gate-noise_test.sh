#!/usr/bin/env bash
# infra/tests/helm-gate-noise_test.sh
#
# Unit tests for infra/scripts/helm-gate-noise.sh.
# Plan 2026-05-15-close-all-gaps T4.1 — instrument Helm gate noise.
#
# Each case feeds a synthetic JSON workflow-runs array to the script
# (via stdin) and asserts the resulting metric block.
#
# Run: bash infra/tests/helm-gate-noise_test.sh
#
# SPDX-License-Identifier: Apache-2.0

set -u

SCRIPT="$(cd "$(dirname "$0")/../.." && pwd)/infra/scripts/helm-gate-noise.sh"
[[ -x "$SCRIPT" ]] || { echo "FAIL: $SCRIPT not executable (chmod +x?)"; exit 1; }

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

pass=0
fail=0

assert_eq() {
  local label="$1"
  local want="$2"
  local got="$3"
  if [[ "$want" == "$got" ]]; then
    echo "PASS: $label"
    pass=$((pass + 1))
  else
    echo "FAIL: $label"
    echo "  want: $want"
    echo "  got : $got"
    fail=$((fail + 1))
  fi
}

# ---- Fixture: 3 runs, 1 failure on chartsnap ----
cat >"$WORK/runs-mixed.json" <<'EOF'
[
  {"name": "chartsnap whole-chart snapshot (CH-G12 / T4.2)", "conclusion": "success", "createdAt": "2026-05-14T10:00:00Z"},
  {"name": "chartsnap whole-chart snapshot (CH-G12 / T4.2)", "conclusion": "failure", "createdAt": "2026-05-13T10:00:00Z"},
  {"name": "helm-diff PR gate (CH-G05 / T4.3)",              "conclusion": "success", "createdAt": "2026-05-12T10:00:00Z"}
]
EOF

# ---- Case 1: counts failures per gate ----
OUT_JSON=$("$SCRIPT" --window-days 14 --reference 2026-05-15T00:00:00Z <"$WORK/runs-mixed.json")
chartsnap_fail=$(echo "$OUT_JSON" | grep -oE '"chartsnap":[[:space:]]*[0-9]+' | head -1 | awk -F: '{print $2}' | xargs)
helmdiff_fail=$(echo "$OUT_JSON" | grep -oE '"helm_diff":[[:space:]]*[0-9]+' | head -1 | awk -F: '{print $2}' | xargs)
assert_eq "chartsnap failure count == 1" "1" "$chartsnap_fail"
assert_eq "helm-diff failure count == 0" "0" "$helmdiff_fail"

# ---- Case 2: window filter excludes old runs ----
cat >"$WORK/runs-window.json" <<'EOF'
[
  {"name": "chartsnap whole-chart snapshot (CH-G12 / T4.2)", "conclusion": "failure", "createdAt": "2026-05-14T10:00:00Z"},
  {"name": "chartsnap whole-chart snapshot (CH-G12 / T4.2)", "conclusion": "failure", "createdAt": "2026-04-01T10:00:00Z"}
]
EOF
OUT_JSON=$("$SCRIPT" --window-days 14 --reference 2026-05-15T00:00:00Z <"$WORK/runs-window.json")
chartsnap_fail=$(echo "$OUT_JSON" | grep -oE '"chartsnap":[[:space:]]*[0-9]+' | head -1 | awk -F: '{print $2}' | xargs)
assert_eq "window-14d excludes 2026-04-01 run" "1" "$chartsnap_fail"

# ---- Case 3: emits runs_total and window metadata ----
OUT_JSON=$("$SCRIPT" --window-days 14 --reference 2026-05-15T00:00:00Z <"$WORK/runs-mixed.json")
runs_total=$(echo "$OUT_JSON" | grep -oE '"runs_total":[[:space:]]*[0-9]+' | head -1 | awk -F: '{print $2}' | xargs)
assert_eq "runs_total counts all in window" "3" "$runs_total"
echo "$OUT_JSON" | grep -q '"window_days":[[:space:]]*14'    && assert_eq "window_days field present" "ok" "ok" || assert_eq "window_days field present" "ok" "missing"
echo "$OUT_JSON" | grep -q '"reference_utc"'                 && assert_eq "reference_utc field present" "ok" "ok" || assert_eq "reference_utc field present" "ok" "missing"

# ---- Case 4: 4 known gates always present, default 0 ----
cat >"$WORK/runs-empty.json" <<'EOF'
[]
EOF
OUT_JSON=$("$SCRIPT" --window-days 14 --reference 2026-05-15T00:00:00Z <"$WORK/runs-empty.json")
for gate in conftest chartsnap helm_diff drift_check; do
  count=$(echo "$OUT_JSON" | grep -oE "\"${gate}\":[[:space:]]*[0-9]+" | head -1 | awk -F: '{print $2}' | xargs)
  assert_eq "empty-input -> $gate=0" "0" "$count"
done

# ---- Case 5: usage error on missing --window-days ----
"$SCRIPT" </dev/null >/dev/null 2>&1
got_exit=$?
assert_eq "missing args -> exit 1" "1" "$got_exit"

# ---- Case 6: ignores non-failure conclusions (cancelled, skipped) ----
cat >"$WORK/runs-mixed-conclusion.json" <<'EOF'
[
  {"name": "chartsnap whole-chart snapshot (CH-G12 / T4.2)", "conclusion": "cancelled", "createdAt": "2026-05-14T10:00:00Z"},
  {"name": "chartsnap whole-chart snapshot (CH-G12 / T4.2)", "conclusion": "failure",   "createdAt": "2026-05-13T10:00:00Z"},
  {"name": "chartsnap whole-chart snapshot (CH-G12 / T4.2)", "conclusion": "skipped",   "createdAt": "2026-05-12T10:00:00Z"}
]
EOF
OUT_JSON=$("$SCRIPT" --window-days 14 --reference 2026-05-15T00:00:00Z <"$WORK/runs-mixed-conclusion.json")
chartsnap_fail=$(echo "$OUT_JSON" | grep -oE '"chartsnap":[[:space:]]*[0-9]+' | head -1 | awk -F: '{print $2}' | xargs)
assert_eq "only conclusion=failure counts" "1" "$chartsnap_fail"

echo ""
echo "===== RESULT: ${pass} passed, ${fail} failed ====="
[[ "$fail" -eq 0 ]] || exit 1
