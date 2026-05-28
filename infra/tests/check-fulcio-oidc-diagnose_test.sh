#!/usr/bin/env bash
# Hermetic test for infra/scripts/local/fulcio-oidc-diagnose.sh
# Per plan v1.1 § T0.2. Mocks kubectl via PATH override to simulate
# each cenário (A/B/C/D/GREEN) + cleanup + dry-run + coverage assertion.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="${REPO_ROOT}/scripts/local/fulcio-oidc-diagnose.sh"
LIB="${REPO_ROOT}/scripts/local/lib/fulcio-probes.sh"
FIXTURES_DIR="${REPO_ROOT}/tests/fixtures/fulcio-diagnose"
MOCK_KUBECTL="${FIXTURES_DIR}/mock-kubectl.sh"

PASS=0
FAIL=0
FAILED_TESTS=()

# ─── Test harness helpers ─────────────────────────────────────────
fail() {
  local test="$1"; shift
  echo "  FAIL: $test — $*" >&2
  FAIL=$((FAIL + 1))
  FAILED_TESTS+=("$test")
}

ok() {
  local test="$1"
  echo "  PASS: $test"
  PASS=$((PASS + 1))
}

# Run script with mock kubectl + fixture, capture exit + verdict.
run_diagnose() {
  local fixture="$1"
  local extra_args="${2:-}"
  local tmp_path tmp_invoc tmp_out_dir exit_code=0

  tmp_path=$(mktemp -d)
  tmp_invoc=$(mktemp)
  tmp_out_dir=$(mktemp -d)

  # Create a PATH shim where `kubectl` resolves to our mock
  ln -sf "$MOCK_KUBECTL" "$tmp_path/kubectl"
  chmod +x "$tmp_path/kubectl"

  # shellcheck disable=SC2086 # intentional: extra_args may be empty or "--dry-run"
  PATH="$tmp_path:$PATH" \
    MOCK_KUBECTL_FIXTURE="$fixture" \
    MOCK_KUBECTL_INVOCATIONS_LOG="$tmp_invoc" \
    PROBE_OUT_DIR="$tmp_out_dir" \
    PROBE_ID="test-$$" \
    BUILD_KUBECONFIG="" \
    bash "$SCRIPT" $extra_args >/tmp/diagnose-stdout.log 2>/tmp/diagnose-stderr.log || exit_code=$?

  echo "$exit_code"
  echo "$tmp_invoc"
  echo "$tmp_out_dir"
}

# ─── Test 1: fixture loading (JSON validity) ──────────────────────
test_fixtures_valid_json() {
  local name="test_fixtures_valid_json"
  local all_ok=1
  for f in "$FIXTURES_DIR"/*.json; do
    if ! jq -e . < "$f" >/dev/null 2>&1; then
      fail "$name" "Invalid JSON: $f"
      all_ok=0
    fi
  done
  [[ "$all_ok" -eq 1 ]] && ok "$name"
}

# ─── Test 2: mock-kubectl coverage (EC-7) ─────────────────────────
test_mock_kubectl_coverage() {
  local name="test_mock_kubectl_coverage"
  local used missing=0
  # Skip comment lines (grep -v ^#) to avoid false-positive matches in doc comments
  used=$(grep -hv '^[[:space:]]*#' "$SCRIPT" "$LIB" 2>/dev/null | grep -oE '\$KUBECTL [a-z]+' | awk '{print $2}' | sort -u)
  if [[ -z "$used" ]]; then
    fail "$name" "Could not grep kubectl invocations from script+lib"
    return
  fi
  for sub in $used; do
    if ! grep -q "^[[:space:]]*${sub})" "$MOCK_KUBECTL"; then
      fail "$name" "mock-kubectl missing subcommand: $sub"
      missing=$((missing + 1))
    fi
  done
  [[ "$missing" -eq 0 ]] && ok "$name"
}

# ─── Test 3: Cenário C (issuer mismatch) ──────────────────────────
test_cenario_c_iss_mismatch() {
  local name="test_cenario_c_iss_mismatch"
  local out exit_code invoc_file out_dir
  out=$(run_diagnose "$FIXTURES_DIR/cenario-c-iss-mismatch.json")
  exit_code=$(echo "$out" | sed -n '1p')
  invoc_file=$(echo "$out" | sed -n '2p')
  out_dir=$(echo "$out" | sed -n '3p')

  if [[ "$exit_code" != "2" ]]; then
    fail "$name" "expected exit=2, got $exit_code (stderr: $(tail -5 /tmp/diagnose-stderr.log | tr '\n' '|'))"
    return
  fi
  local verdict
  verdict=$(jq -r '.cenario' < "$out_dir/verdict.json" 2>/dev/null || echo "MISSING")
  if [[ "$verdict" != "C" ]]; then
    fail "$name" "expected verdict=C, got $verdict"
    return
  fi
  ok "$name"
}

# ─── Test 4: Cenário A (anonymous discovery 403) ──────────────────
test_cenario_a_anon_403() {
  local name="test_cenario_a_anon_403"
  local out exit_code out_dir
  out=$(run_diagnose "$FIXTURES_DIR/cenario-a-anon-403.json")
  exit_code=$(echo "$out" | sed -n '1p')
  out_dir=$(echo "$out" | sed -n '3p')

  if [[ "$exit_code" != "0" ]]; then
    fail "$name" "expected exit=0, got $exit_code"
    return
  fi
  local verdict
  verdict=$(jq -r '.cenario' < "$out_dir/verdict.json" 2>/dev/null || echo "MISSING")
  if [[ "$verdict" != "A" ]]; then
    fail "$name" "expected verdict=A, got $verdict"
    return
  fi
  ok "$name"
}

# ─── Test 4b: Cenário A 401 variant (Ralph iter 4 LIVE discovery) ─
test_cenario_a_401_unauthorized() {
  local name="test_cenario_a_401_unauthorized"
  local out exit_code out_dir
  out=$(run_diagnose "$FIXTURES_DIR/cenario-a-401-unauthorized.json")
  exit_code=$(echo "$out" | sed -n '1p')
  out_dir=$(echo "$out" | sed -n '3p')

  if [[ "$exit_code" != "0" ]]; then
    fail "$name" "expected exit=0, got $exit_code"
    return
  fi
  local verdict
  verdict=$(jq -r '.cenario' < "$out_dir/verdict.json" 2>/dev/null || echo "MISSING")
  if [[ "$verdict" != "A" ]]; then
    fail "$name" "expected verdict=A (401 should classify same as 403), got $verdict"
    return
  fi
  ok "$name"
}

# ─── Test 5: Cenário B (NetworkPolicy timeout) ────────────────────
test_cenario_b_netpol() {
  local name="test_cenario_b_netpol"
  local out exit_code out_dir
  out=$(run_diagnose "$FIXTURES_DIR/cenario-b-netpol-timeout.json")
  exit_code=$(echo "$out" | sed -n '1p')
  out_dir=$(echo "$out" | sed -n '3p')

  if [[ "$exit_code" != "1" ]]; then
    fail "$name" "expected exit=1, got $exit_code"
    return
  fi
  local verdict
  verdict=$(jq -r '.cenario' < "$out_dir/verdict.json" 2>/dev/null || echo "MISSING")
  if [[ "$verdict" != "B" ]]; then
    fail "$name" "expected verdict=B, got $verdict"
    return
  fi
  ok "$name"
}

# ─── Test 6: Cenário D (TLS CA mount) ─────────────────────────────
test_cenario_d_tls_ca() {
  local name="test_cenario_d_tls_ca"
  local out exit_code out_dir
  out=$(run_diagnose "$FIXTURES_DIR/cenario-d-tls-ca-mount.json")
  exit_code=$(echo "$out" | sed -n '1p')
  out_dir=$(echo "$out" | sed -n '3p')

  if [[ "$exit_code" != "3" ]]; then
    fail "$name" "expected exit=3, got $exit_code (stderr: $(tail -5 /tmp/diagnose-stderr.log | tr '\n' '|'))"
    return
  fi
  local verdict
  verdict=$(jq -r '.cenario' < "$out_dir/verdict.json" 2>/dev/null || echo "MISSING")
  if [[ "$verdict" != "D" ]]; then
    fail "$name" "expected verdict=D, got $verdict"
    return
  fi
  ok "$name"
}

# ─── Test 7: GREEN happy path ─────────────────────────────────────
test_green_happy_path() {
  local name="test_green_happy_path"
  local out exit_code out_dir
  out=$(run_diagnose "$FIXTURES_DIR/green-happy-path.json")
  exit_code=$(echo "$out" | sed -n '1p')
  out_dir=$(echo "$out" | sed -n '3p')

  if [[ "$exit_code" != "0" ]]; then
    fail "$name" "expected exit=0, got $exit_code"
    return
  fi
  local verdict
  verdict=$(jq -r '.cenario' < "$out_dir/verdict.json" 2>/dev/null || echo "MISSING")
  if [[ "$verdict" != "GREEN" ]]; then
    fail "$name" "expected verdict=GREEN, got $verdict"
    return
  fi
  ok "$name"
}

# ─── Test 8: --dry-run no mutation ────────────────────────────────
test_dry_run_no_mutation() {
  local name="test_dry_run_no_mutation"
  local out exit_code invoc_file
  out=$(run_diagnose "$FIXTURES_DIR/green-happy-path.json" "--dry-run")
  exit_code=$(echo "$out" | sed -n '1p')
  invoc_file=$(echo "$out" | sed -n '2p')

  if [[ "$exit_code" != "0" ]]; then
    fail "$name" "expected exit=0, got $exit_code"
    return
  fi
  # No kubectl invocations during --dry-run (mock log file stays empty or absent)
  if [[ -s "$invoc_file" ]]; then
    fail "$name" "--dry-run should not invoke kubectl, but log has: $(wc -l < "$invoc_file") lines"
    return
  fi
  ok "$name"
}

# ─── Test 9: cleanup on failure (EC trap rollback) ────────────────
test_cleanup_on_failure() {
  local name="test_cleanup_on_failure"
  local out invoc_file
  # Cenário B timeout — script reaches verdict but cleanup must run
  out=$(run_diagnose "$FIXTURES_DIR/cenario-b-netpol-timeout.json")
  invoc_file=$(echo "$out" | sed -n '2p')

  # Assert cleanup invocations: patch deploy (rollback --log-level) + delete pod
  if ! grep -q "patch deployment fulcio-server" "$invoc_file"; then
    fail "$name" "cleanup did not call 'patch deployment fulcio-server' (rollback)"
    return
  fi
  if ! grep -q "delete pod" "$invoc_file"; then
    fail "$name" "cleanup did not call 'delete pod'"
    return
  fi
  ok "$name"
}

# ─── Runner ────────────────────────────────────────────────────────
echo "Running fulcio-oidc-diagnose hermetic tests..."
echo ""

test_fixtures_valid_json
test_mock_kubectl_coverage
test_cenario_c_iss_mismatch
test_cenario_a_anon_403
test_cenario_a_401_unauthorized
test_cenario_b_netpol
test_cenario_d_tls_ca
test_green_happy_path
test_dry_run_no_mutation
test_cleanup_on_failure

echo ""
echo "═══════════════════════════════════════════════════════════"
echo "  Results: ${PASS} PASS / ${FAIL} FAIL"
echo "═══════════════════════════════════════════════════════════"

if [[ "$FAIL" -gt 0 ]]; then
  echo "Failed: ${FAILED_TESTS[*]}"
  exit 1
fi
exit 0
