#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Hermetic structural test for the k6 load-harness rigor fixes
# (plan k6-load-test-rigor-fixes). Pure text-presence + YAML-syntax
# checks over local files — NO cluster, NO network, NO k6 run.
#
# Asserts the three review findings are closed:
#   1. dropped_iterations threshold present in lib/thresholds.js `base`
#      (count<, since dropped_iterations is a k6 Counter) — EC-2.
#   2. testrun-l5-distributed.yaml exists, valid YAML (EC-1), TestRun
#      shape, pinned image, Prometheus RW, secretKeyRef; k6:distributed:l5
#      task applies it.
#   3. README documents the per-level runner path.
#   4. This test is wired into quality:gates AND allowlisted (EC-3 row shape).
#
# Run: bash infra/tests/check-k6-load-rigor_test.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"   # repo root (infra/tests -> infra -> root)
THRESHOLDS="$ROOT/infra/tests/k6/scripts/lib/thresholds.js"
L5_MANIFEST="$ROOT/infra/tests/k6/manifests/testrun-l5-distributed.yaml"
TESTFILE="$ROOT/taskfiles/test.yml"
QUALITY="$ROOT/taskfiles/quality.yml"
README="$ROOT/infra/tests/k6/README.md"
ALLOWLIST="$ROOT/.claude/rules/shell-scripts-allowlist.md"

PASS=0; FAIL=0
ok()   { echo "  PASS: $1"; PASS=$((PASS+1)); }
bad()  { echo "  FAIL: $1"; FAIL=$((FAIL+1)); }

echo "── check-k6-load-rigor ──"

# ---- Phase 1: dropped_iterations threshold (EC-2) -----------------------
echo "[1] dropped_iterations threshold"
if grep -q "dropped_iterations" "$THRESHOLDS"; then ok "dropped_iterations present in thresholds.js"; else bad "dropped_iterations missing in thresholds.js"; fi
# Must be in `base` (before the perLevel block) so thresholdsFor merges it for every level.
if awk '/export const base/{b=1} /export const perLevel/{b=0} b && /dropped_iterations/{found=1} END{exit !found}' "$THRESHOLDS"; then ok "dropped_iterations is in base (applies to all levels)"; else bad "dropped_iterations not in base block"; fi
# EC-2: must be a count<… threshold (Counter), not rate<… (silent no-op).
if grep -E "dropped_iterations.*count<" "$THRESHOLDS" >/dev/null; then ok "threshold uses count< (correct for a Counter)"; else bad "threshold not count< (EC-2: dropped_iterations is a Counter, rate< is a no-op)"; fi

# ---- Phase 2: L5 distributed manifest + task ----------------------------
echo "[2] L5 distributed path"
if [ -f "$L5_MANIFEST" ]; then ok "testrun-l5-distributed.yaml exists"; else bad "testrun-l5-distributed.yaml missing"; fi
# EC-1 (MUST): hermetic YAML-syntax check — grep can't catch invalid YAML.
if [ -f "$L5_MANIFEST" ] && python3 -c "import yaml,sys; list(yaml.safe_load_all(open(sys.argv[1])))" "$L5_MANIFEST" 2>/dev/null; then ok "L5 manifest is valid YAML (EC-1)"; else bad "L5 manifest is NOT valid YAML (EC-1)"; fi
if [ -f "$L5_MANIFEST" ]; then
  grep -q "kind: TestRun" "$L5_MANIFEST"                 && ok "kind: TestRun"            || bad "missing kind: TestRun"
  grep -q "apiVersion: k6.io/v1alpha1" "$L5_MANIFEST"    && ok "apiVersion k6.io/v1alpha1" || bad "missing apiVersion"
  grep -Eq "parallelism: *[0-9]+" "$L5_MANIFEST"         && ok "parallelism set"          || bad "missing parallelism"
  grep -q "image: grafana/k6:0.52.0" "$L5_MANIFEST"      && ok "pinned grafana/k6:0.52.0"  || bad "image not pinned to 0.52.0"
  grep -q "K6_PROMETHEUS_RW_SERVER_URL" "$L5_MANIFEST"   && ok "Prometheus remote-write"   || bad "missing Prometheus RW"
  grep -q "secretKeyRef" "$L5_MANIFEST"                  && ok "secretKeyRef (no inline secret)" || bad "missing secretKeyRef"
  grep -q "file: scripts/l5-multicell.js" "$L5_MANIFEST" && ok "references l5-multicell.js" || bad "wrong/missing script ref"
  grep -qE "value: *\"?[a-f0-9]{32,}\"?" "$L5_MANIFEST"  && bad "inline secret-looking value present" || ok "no inline secret value"
fi
grep -q "k6:distributed:l5:" "$TESTFILE"                          && ok "k6:distributed:l5 task defined" || bad "k6:distributed:l5 task missing"
grep -q "testrun-l5-distributed.yaml" "$TESTFILE"                 && ok "task applies the L5 manifest"   || bad "task does not apply L5 manifest"

# ---- Phase 3: per-level path doc ---------------------------------------
echo "[3] per-level runner doc"
grep -qiE "which runner per level|runner per level|per-level" "$README" && ok "README has per-level path section" || bad "README missing per-level path section"
grep -qi "distributed" "$README" && grep -qiE "single.pod|single pod" "$README" && ok "README names both paths" || bad "README does not name both single-pod + distributed"

# ---- Phase 4: gate wiring + allowlist (EC-3) ---------------------------
echo "[4] gate wiring + allowlist"
grep -q "check-k6-load-rigor_test.sh" "$QUALITY" && ok "wired into quality:gates" || bad "not wired into quality:gates"
# EC-3: the allowlist row must match the 3-column table format.
if grep -E "^\| *infra/tests/check-k6-load-rigor_test\.sh *\|[^|]+\|[^|]+\|" "$ALLOWLIST" >/dev/null 2>&1; then ok "allowlist row well-formed (3 columns, EC-3)"; else bad "allowlist row missing or malformed (EC-3)"; fi

echo ""
echo "── result: $PASS passed, $FAIL failed ──"
[ "$FAIL" -eq 0 ]
