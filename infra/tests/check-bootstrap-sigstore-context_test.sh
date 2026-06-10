#!/usr/bin/env bash
# check-bootstrap-sigstore-context_test.sh — Hermetic test for T0.1 of plan
# 2026-05-28-meet-001-engineering-closure (ADR-2026-05-28-T2.2-LOCAL-EXCEPTION).
#
# Validates that bootstrap-sigstore-local.sh enforces the regex allowlist:
#   - k3d-loopzero-build → PASSES gate
#   - k3d-loopzero-drill-build → PASSES gate (drill prod-identical LOCAL)
#   - k3d-loopzero-mgmt → FAILS gate with exit 1
#   - DOKS prod context → FAILS gate
#   - Operator override via env var → WARN to stderr but PASSES
#
# All tests use mock kubectl to avoid live cluster dependency.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BOOTSTRAP_SCRIPT="$REPO_ROOT/scripts/local/bootstrap-sigstore-local.sh"

[[ -f "$BOOTSTRAP_SCRIPT" ]] || { echo "FATAL: bootstrap script not found at $BOOTSTRAP_SCRIPT"; exit 2; }

# Mock kubectl: any cluster-info call passes; everything else dies with EXIT_GATE
# so the test only exercises the gate logic.
MOCK_BIN="$(mktemp -d)"
trap 'rm -rf "$MOCK_BIN"' EXIT

cat > "$MOCK_BIN/kubectl" <<'EOF'
#!/usr/bin/env bash
# Mock: cluster-info => OK; everything else => exit 99 (lets test see gate result first)
case "$*" in
  *"cluster-info"*) echo "Kubernetes control plane is running" ; exit 0 ;;
  *) exit 99 ;;
esac
EOF
chmod +x "$MOCK_BIN/kubectl"

cat > "$MOCK_BIN/helm" <<'EOF'
#!/usr/bin/env bash
exit 99
EOF
chmod +x "$MOCK_BIN/helm"

PASS=0
FAIL=0

run_case() {
  local name="$1"
  local context="$2"
  local override="$3"   # empty or regex
  local expect_exit="$4"  # 0=continues past gate (we proxy via exit 99 from mock); 1=gate rejects with 1
  local expect_stderr_pattern="$5"  # empty or regex

  local out err rc
  out=$(mktemp); err=$(mktemp)
  if [[ -n "$override" ]]; then
    PATH="$MOCK_BIN:$PATH" SIGSTORE_CONTEXT="$context" SIGSTORE_ALLOWED_CONTEXTS_REGEX="$override" \
      bash "$BOOTSTRAP_SCRIPT" >"$out" 2>"$err"
  else
    PATH="$MOCK_BIN:$PATH" SIGSTORE_CONTEXT="$context" \
      bash "$BOOTSTRAP_SCRIPT" >"$out" 2>"$err"
  fi
  rc=$?

  local ok=1
  if [[ "$expect_exit" == "1" && $rc -ne 1 ]]; then ok=0; fi
  if [[ "$expect_exit" == "0" && $rc -eq 1 ]]; then ok=0; fi
  if [[ -n "$expect_stderr_pattern" ]] && ! grep -qE "$expect_stderr_pattern" "$err"; then ok=0; fi

  if [[ $ok -eq 1 ]]; then
    printf '  PASS: %s\n' "$name"
    PASS=$((PASS+1))
  else
    printf '  FAIL: %s (rc=%d, stderr below)\n' "$name" "$rc"
    sed 's/^/    /' "$err"
    FAIL=$((FAIL+1))
  fi
  rm -f "$out" "$err"
}

echo "Running bootstrap-sigstore-context hermetic tests..."
echo

# EC-9: regex allowlist test cases
run_case "test_bootstrap_sigstore_accepts_k3d_loopzero_build_context"        "k3d-loopzero-build"       ""  "0" ""
run_case "test_bootstrap_sigstore_accepts_k3d_loopzero_drill_build_context"  "k3d-loopzero-drill-build" ""  "0" ""
run_case "test_bootstrap_sigstore_rejects_non_local_context"             "k3d-loopzero-mgmt"        ""  "1" "not in allowlist"
run_case "test_bootstrap_sigstore_rejects_prod_doks_context"             "do-sfo3-loopzero-mgmt"    ""  "1" "not in allowlist"
run_case "test_bootstrap_sigstore_rejects_random_context"                "kind-some-cluster"    ""  "1" "not in allowlist"
run_case "test_bootstrap_sigstore_override_regex_emits_warn"             "k3d-loopzero-staging"     "^k3d-loopzero-staging$" "0" "WARN.*SIGSTORE_ALLOWED_CONTEXTS_REGEX overridden"
run_case "test_bootstrap_sigstore_override_still_enforces_pattern"       "k3d-loopzero-mgmt"        "^k3d-loopzero-staging$" "1" "not in allowlist"

echo
echo "═══════════════════════════════════════════════════════════"
printf "  Results: %d PASS / %d FAIL\n" "$PASS" "$FAIL"
echo "═══════════════════════════════════════════════════════════"

[[ $FAIL -eq 0 ]] || exit 1
