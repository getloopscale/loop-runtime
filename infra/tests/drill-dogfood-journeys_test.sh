#!/usr/bin/env bash
# infra/tests/drill-dogfood-journeys_test.sh
#
# Hermetic tests for infra/scripts/lib/drill-dogfood-journeys.sh.
#
# Plan: docs/plans/2026-05-19-drill-skills-rigor-restoration-plan.md T3.2
#
# Tests cover: resolve_loopzero_cli (node version gate), prova_de_vida (kubectl mock),
# wait_for_argocd_prune (timeout), run_cli_journey (exit code propagation + counters),
# discover_workload_context (env var override + default fallback).
#
# Run: bash infra/tests/drill-dogfood-journeys_test.sh
#
# SPDX-License-Identifier: Apache-2.0

set -u

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
LIB="$REPO_ROOT/infra/scripts/lib/drill-dogfood-journeys.sh"
[[ -f "$LIB" ]] || { echo "FAIL: $LIB missing"; exit 1; }

# shellcheck source=/dev/null
source "$LIB"

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

pass=0
fail=0

assert_eq() {
  local label="$1" want="$2" got="$3"
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

# ============================================================
# resolve_loopzero_cli — node version gate
# ============================================================

# Mock node v18 (too old per EC-5)
mkdir -p "$WORK/bin-node18"
cat > "$WORK/bin-node18/node" <<'EOF'
#!/usr/bin/env bash
echo "v18.20.0"
EOF
chmod +x "$WORK/bin-node18/node"

# Mock node v20 (acceptable)
mkdir -p "$WORK/bin-node20"
cat > "$WORK/bin-node20/node" <<'EOF'
#!/usr/bin/env bash
echo "v20.10.0"
EOF
chmod +x "$WORK/bin-node20/node"

# Fake cli/ dir with package.json that npm-builds successfully
mkdir -p "$WORK/repo-ok/cli/bin"
cat > "$WORK/repo-ok/cli/package.json" <<'EOF'
{"name":"loopzero-cli","scripts":{"build":"echo built"}}
EOF
cat > "$WORK/repo-ok/cli/bin/loopzero" <<'EOF'
#!/usr/bin/env bash
echo "loopzero CLI v1"
EOF
chmod +x "$WORK/repo-ok/cli/bin/loopzero"

# Mock npm (just runs the build script)
mkdir -p "$WORK/bin-mock"
cat > "$WORK/bin-mock/npm" <<'EOF'
#!/usr/bin/env bash
# Minimal: when called with "run build", succeed
[[ "$1" == "run" && "$2" == "build" ]] && exit 0
exit 1
EOF
chmod +x "$WORK/bin-mock/npm"

# ---- Case 1: resolve_loopzero_cli fails with node v18 ----
out=$(PATH="$WORK/bin-node18:$WORK/bin-mock:$PATH" resolve_loopzero_cli "$WORK/repo-ok" 2>&1)
rc=$?
assert_eq "node v18 → resolve_loopzero_cli exit 1" "1" "$rc"
[[ "$out" == *"requires node ≥ 20"* ]] && \
  { echo "PASS: v18 error message clear"; pass=$((pass+1)); } || \
  { echo "FAIL: v18 message: $out"; fail=$((fail+1)); }

# ---- Case 2: resolve_loopzero_cli succeeds with node v20 ----
out=$(PATH="$WORK/bin-node20:$WORK/bin-mock:$PATH" resolve_loopzero_cli "$WORK/repo-ok" 2>/dev/null)
rc=$?
assert_eq "node v20 → resolve_loopzero_cli exit 0" "0" "$rc"
[[ "$out" == *"$WORK/repo-ok/cli/bin/loopzero"* ]] && \
  { echo "PASS: v20 returns loopzero binary path"; pass=$((pass+1)); } || \
  { echo "FAIL: v20 output: $out"; fail=$((fail+1)); }

# ---- Case 3: resolve_loopzero_cli fails when cli/ directory missing ----
out=$(PATH="$WORK/bin-node20:$WORK/bin-mock:$PATH" resolve_loopzero_cli "$WORK/nonexistent" 2>&1)
rc=$?
assert_eq "missing cli/ dir → exit 1" "1" "$rc"

# ============================================================
# discover_workload_context — env override + default fallback
# ============================================================

# ---- Case 4: WORKLOAD_CONTEXT env var wins ----
out=$(WORKLOAD_CONTEXT="env-override-ctx" discover_workload_context "$WORK/no-pulumi-dir")
assert_eq "env var override" "env-override-ctx" "$out"

# ---- Case 5: default fallback when no pulumi available ----
out=$(unset WORKLOAD_CONTEXT; discover_workload_context "$WORK/no-pulumi-dir")
assert_eq "default fallback k8s-drill-workload" "k8s-drill-workload" "$out"

# ============================================================
# run_cli_journey — exit code propagation + counters
# ============================================================

# Mock commands
cat > "$WORK/cmd-ok.sh" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$WORK/cmd-ok.sh"

cat > "$WORK/cmd-fail.sh" <<'EOF'
#!/usr/bin/env bash
exit 2
EOF
chmod +x "$WORK/cmd-fail.sh"

# ---- Case 6: command succeeds (exit 0) → JOURNEYS_PASSED++ ----
JOURNEYS_PASSED=0
JOURNEYS_FAILED=0
run_cli_journey "TJ1" "ok command" "$WORK/cmd-ok.sh" 0 >/dev/null 2>&1
assert_eq "ok cmd increments JOURNEYS_PASSED" "1" "$JOURNEYS_PASSED"
assert_eq "ok cmd does not increment JOURNEYS_FAILED" "0" "$JOURNEYS_FAILED"

# ---- Case 7: command fails (exit 2) → JOURNEYS_FAILED++ ----
JOURNEYS_PASSED=0
JOURNEYS_FAILED=0
run_cli_journey "TJ2" "fail command" "$WORK/cmd-fail.sh" 0 >/dev/null 2>&1
assert_eq "fail cmd increments JOURNEYS_FAILED" "1" "$JOURNEYS_FAILED"
assert_eq "fail cmd does not increment JOURNEYS_PASSED" "0" "$JOURNEYS_PASSED"

# ---- Case 8: expected exit code matching (non-zero expected) ----
JOURNEYS_PASSED=0
JOURNEYS_FAILED=0
run_cli_journey "TJ3" "expects exit 2" "$WORK/cmd-fail.sh" 2 >/dev/null 2>&1
assert_eq "matching non-zero exit → PASS" "1" "$JOURNEYS_PASSED"

# ============================================================
# prova_de_vida — kubectl mock
# ============================================================

# Stub kubectl: emit fixture-controlled output
mkdir -p "$WORK/bin-kubectl"
cat > "$WORK/bin-kubectl/kubectl" <<'EOF'
#!/usr/bin/env bash
# Mock kubectl. Reads $MOCK_KUBECTL_RESPONSE for the jsonpath value.
# Args (relevant): "--context X get pod -n NS -l app=APP -o jsonpath='{.items[0].status.phase}'"
case "$*" in
  *"jsonpath={.items[0].status.phase}"*)
    echo "${MOCK_KUBECTL_PHASE:-Running}"
    ;;
  *"jsonpath={.items[0].status.containerStatuses[0].ready}"*)
    echo "${MOCK_KUBECTL_READY:-true}"
    ;;
  *)
    echo ""
    ;;
esac
EOF
chmod +x "$WORK/bin-kubectl/kubectl"

# ---- Case 9: pod Running + ready=true → PASS ----
out=$(PATH="$WORK/bin-kubectl:$PATH" MOCK_KUBECTL_PHASE="Running" MOCK_KUBECTL_READY="true" \
  prova_de_vida "test-ctx" "test-ns" "test-app" 2>&1)
rc=$?
assert_eq "Running + ready → exit 0" "0" "$rc"
[[ "$out" == *"PASS"* ]] && \
  { echo "PASS: prova_de_vida PASS log"; pass=$((pass+1)); } || \
  { echo "FAIL: prova_de_vida log: $out"; fail=$((fail+1)); }

# ---- Case 10: pod Pending → FAIL ----
out=$(PATH="$WORK/bin-kubectl:$PATH" MOCK_KUBECTL_PHASE="Pending" MOCK_KUBECTL_READY="false" \
  prova_de_vida "test-ctx" "test-ns" "test-app" 2>&1)
rc=$?
assert_eq "Pending → exit 1" "1" "$rc"
[[ "$out" == *"phase=Pending"* ]] && \
  { echo "PASS: Pending error message clear"; pass=$((pass+1)); } || \
  { echo "FAIL: Pending log: $out"; fail=$((fail+1)); }

# ---- Case 11: Running but container not ready → FAIL ----
out=$(PATH="$WORK/bin-kubectl:$PATH" MOCK_KUBECTL_PHASE="Running" MOCK_KUBECTL_READY="false" \
  prova_de_vida "test-ctx" "test-ns" "test-app" 2>&1)
rc=$?
assert_eq "Running but ready=false → exit 1" "1" "$rc"

# ============================================================
# wait_for_argocd_prune — kubectl-based polling
# ============================================================

# Stub kubectl that always says app missing
mkdir -p "$WORK/bin-kubectl-missing"
cat > "$WORK/bin-kubectl-missing/kubectl" <<'EOF'
#!/usr/bin/env bash
case "$*" in
  *"annotate application"*) exit 0 ;;
  *"get application"*)      exit 1 ;;
  *)                        exit 0 ;;
esac
EOF
chmod +x "$WORK/bin-kubectl-missing/kubectl"

# ---- Case 12: ArgoCD app already gone → wait_for_argocd_prune exit 0 quickly ----
out=$(PATH="$WORK/bin-kubectl-missing:$PATH" DRILL_PRUNE_TIMEOUT=5 \
  wait_for_argocd_prune "test-ctx" "test-app" 2>&1)
rc=$?
assert_eq "app gone → exit 0" "0" "$rc"
[[ "$out" == *"pruned"* ]] && \
  { echo "PASS: wait_for_argocd_prune log clear"; pass=$((pass+1)); } || \
  { echo "FAIL: prune log: $out"; fail=$((fail+1)); }

# ============================================================
# Source guard
# ============================================================

# ---- Case 13: source guard prevents double-load ----
# (already loaded once above; second source must be no-op)
prev_sourced="${_DRILL_DOGFOOD_JOURNEYS_SOURCED:-}"
source "$LIB"
assert_eq "source guard idempotent" "$prev_sourced" "$_DRILL_DOGFOOD_JOURNEYS_SOURCED"

# ---- Case 14 (iter 2 edge): resolve_loopzero_cli fails when npm build fails ----
# Iter 2: simulate npm build returning non-zero (e.g., TS compile error).
mkdir -p "$WORK/bin-npm-fail"
cat > "$WORK/bin-npm-fail/npm" <<'EOF'
#!/usr/bin/env bash
exit 1
EOF
chmod +x "$WORK/bin-npm-fail/npm"
out=$(PATH="$WORK/bin-node20:$WORK/bin-npm-fail:$PATH" resolve_loopzero_cli "$WORK/repo-ok" 2>&1)
rc=$?
assert_eq "npm build failure → exit 1" "1" "$rc"
[[ "$out" == *"npm run build failed"* ]] && \
  { echo "PASS: npm fail error message clear"; pass=$((pass+1)); } || \
  { echo "FAIL: npm fail log: $out"; fail=$((fail+1)); }

# ---- Case 15 (iter 2 edge): prova_de_vida with 0 pods (selector matches nothing) ----
# Stub kubectl emitting empty (no matching pods)
mkdir -p "$WORK/bin-kubectl-empty"
cat > "$WORK/bin-kubectl-empty/kubectl" <<'EOF'
#!/usr/bin/env bash
echo ""
EOF
chmod +x "$WORK/bin-kubectl-empty/kubectl"
out=$(PATH="$WORK/bin-kubectl-empty:$PATH" \
  prova_de_vida "test-ctx" "test-ns" "no-such-app" 2>&1)
rc=$?
assert_eq "no pods matching → exit 1" "1" "$rc"

# ---- Case 16 (iter 2 edge): wait_for_argocd_prune timeout (app always present) ----
# Stub kubectl that always says app present (get application exit 0)
mkdir -p "$WORK/bin-kubectl-present"
cat > "$WORK/bin-kubectl-present/kubectl" <<'EOF'
#!/usr/bin/env bash
case "$*" in
  *"annotate application"*) exit 0 ;;
  *"get application"*)      exit 0 ;;  # app STILL present
  *)                        exit 0 ;;
esac
EOF
chmod +x "$WORK/bin-kubectl-present/kubectl"
# Use very short timeout (4s) so test doesn't drag
out=$(PATH="$WORK/bin-kubectl-present:$PATH" DRILL_PRUNE_TIMEOUT=4 \
  wait_for_argocd_prune "test-ctx" "test-app" 2>&1)
rc=$?
assert_eq "timeout exceeded → exit 1" "1" "$rc"
[[ "$out" == *"still present after"* ]] && \
  { echo "PASS: timeout error message clear"; pass=$((pass+1)); } || \
  { echo "FAIL: timeout log: $out"; fail=$((fail+1)); }

# ---- Final report ----
total=$((pass + fail))
echo ""
echo "Tests run:    $total"
echo "Tests passed: $pass"
echo "Tests failed: $fail"

if [[ $fail -gt 0 ]]; then
  exit 1
fi
exit 0
