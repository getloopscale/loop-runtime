#!/usr/bin/env bash
# infra/tests/drill-jwt-gate_test.sh
#
# Hermetic tests for infra/scripts/lib/drill-jwt-gate.sh.
#
# Plan: docs/plans/2026-05-19-drill-skills-rigor-restoration-plan.md T2.2
#
# Tests verify_jwt_auth_e2e() with mocked mint commands and curl binary.
# No LIVE drill stack required.
#
# Run: bash infra/tests/drill-jwt-gate_test.sh
#
# SPDX-License-Identifier: Apache-2.0

set -u

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
LIB="$REPO_ROOT/infra/scripts/lib/drill-jwt-gate.sh"
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

# --- Mock mint that emits a valid-looking 3-segment JWT ---
cat > "$WORK/mint-ok.sh" <<'EOF'
#!/usr/bin/env bash
echo "header.payload.signature"
EOF
chmod +x "$WORK/mint-ok.sh"

# --- Mock mint that fails ---
cat > "$WORK/mint-fail.sh" <<'EOF'
#!/usr/bin/env bash
exit 1
EOF
chmod +x "$WORK/mint-fail.sh"

# --- Mock mint that emits invalid JWT (no dots) ---
cat > "$WORK/mint-bad.sh" <<'EOF'
#!/usr/bin/env bash
echo "notajwttoken"
EOF
chmod +x "$WORK/mint-bad.sh"

# --- Mock curl emitting 200 ---
cat > "$WORK/curl-200.sh" <<'EOF'
#!/usr/bin/env bash
# Mock: emit 200 to -w
for arg in "$@"; do :; done
# When -w "%{http_code}" is requested, stdout is the http_code
echo -n "200"
EOF
chmod +x "$WORK/curl-200.sh"

# --- Mock curl emitting 401 ---
cat > "$WORK/curl-401.sh" <<'EOF'
#!/usr/bin/env bash
echo -n "401"
EOF
chmod +x "$WORK/curl-401.sh"

# --- Mock curl emitting 500 ---
cat > "$WORK/curl-500.sh" <<'EOF'
#!/usr/bin/env bash
echo -n "500"
EOF
chmod +x "$WORK/curl-500.sh"

# --- Mock curl emitting 000 (network error) ---
cat > "$WORK/curl-000.sh" <<'EOF'
#!/usr/bin/env bash
echo -n "000"
EOF
chmod +x "$WORK/curl-000.sh"

# ---- Case 1: 200 response → exit 0 (PASS) ----
out=$(verify_jwt_auth_e2e "http://test/api" "$WORK/mint-ok.sh" "$WORK/curl-200.sh" "tn-test" 2>&1)
rc=$?
assert_eq "200 response → exit 0" "0" "$rc"
[[ "$out" == *"PASS (HTTP 200)"* ]] && \
  { echo "PASS: 200 produces PASS log"; pass=$((pass+1)); } || \
  { echo "FAIL: 200 log: $out"; fail=$((fail+1)); }

# ---- Case 2: 401 response → exit 1 (FAIL) ----
out=$(verify_jwt_auth_e2e "http://test/api" "$WORK/mint-ok.sh" "$WORK/curl-401.sh" "tn-test" 2>&1)
rc=$?
assert_eq "401 response → exit 1" "1" "$rc"
[[ "$out" == *"FAIL (HTTP 401"* ]] && \
  { echo "PASS: 401 produces FAIL log"; pass=$((pass+1)); } || \
  { echo "FAIL: 401 log: $out"; fail=$((fail+1)); }
[[ "$out" == *"drill-jwt-diagnose"* ]] && \
  { echo "PASS: 401 log points to diagnose script"; pass=$((pass+1)); } || \
  { echo "FAIL: 401 log missing diagnose pointer: $out"; fail=$((fail+1)); }

# ---- Case 3: 500 response → exit 1 (FAIL) ----
out=$(verify_jwt_auth_e2e "http://test/api" "$WORK/mint-ok.sh" "$WORK/curl-500.sh" "tn-test" 2>&1)
rc=$?
assert_eq "500 response → exit 1" "1" "$rc"

# ---- Case 4: 000 (network error) → exit 1 ----
out=$(verify_jwt_auth_e2e "http://test/api" "$WORK/mint-ok.sh" "$WORK/curl-000.sh" "tn-test" 2>&1)
rc=$?
assert_eq "000 response → exit 1" "1" "$rc"

# ---- Case 5: 201 response → exit 0 (PASS) — 2xx wildcard works ----
cat > "$WORK/curl-201.sh" <<'EOF'
#!/usr/bin/env bash
echo -n "201"
EOF
chmod +x "$WORK/curl-201.sh"
out=$(verify_jwt_auth_e2e "http://test/api" "$WORK/mint-ok.sh" "$WORK/curl-201.sh" "tn-test" 2>&1)
rc=$?
assert_eq "201 response → exit 0 (2xx wildcard)" "0" "$rc"

# ---- Case 6: mint failure → exit 1 ----
out=$(verify_jwt_auth_e2e "http://test/api" "$WORK/mint-fail.sh" "$WORK/curl-200.sh" "tn-test" 2>&1)
rc=$?
assert_eq "mint failure → exit 1" "1" "$rc"
[[ "$out" == *"JWT mint failed"* ]] && \
  { echo "PASS: mint failure produces explicit log"; pass=$((pass+1)); } || \
  { echo "FAIL: mint failure log: $out"; fail=$((fail+1)); }

# ---- Case 7: invalid JWT format (no dots) → exit 1 ----
out=$(verify_jwt_auth_e2e "http://test/api" "$WORK/mint-bad.sh" "$WORK/curl-200.sh" "tn-test" 2>&1)
rc=$?
assert_eq "invalid JWT format → exit 1" "1" "$rc"

# ---- Case 8: function preserves source guard idempotency ----
# Sourcing twice must not error
source "$LIB"
source "$LIB"
echo "PASS: lib double-source idempotent (no error)"
pass=$((pass+1))

# ---- Case 9 (iter 2 edge): 3xx redirect treated as FAIL ----
# 2xx wildcard intentionally does NOT match 301/302. JWT auth gates should be
# direct 200/201 — redirects suggest API misconfiguration or proxy interference.
cat > "$WORK/curl-301.sh" <<'EOF'
#!/usr/bin/env bash
echo -n "301"
EOF
chmod +x "$WORK/curl-301.sh"
out=$(verify_jwt_auth_e2e "http://test/api" "$WORK/mint-ok.sh" "$WORK/curl-301.sh" "tn-test" 2>&1)
rc=$?
assert_eq "301 redirect → exit 1 (NOT 2xx wildcard)" "1" "$rc"

# ---- Case 10 (iter 2 edge): 422 unprocessable → FAIL ----
cat > "$WORK/curl-422.sh" <<'EOF'
#!/usr/bin/env bash
echo -n "422"
EOF
chmod +x "$WORK/curl-422.sh"
out=$(verify_jwt_auth_e2e "http://test/api" "$WORK/mint-ok.sh" "$WORK/curl-422.sh" "tn-test" 2>&1)
rc=$?
assert_eq "422 unprocessable → exit 1" "1" "$rc"

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
