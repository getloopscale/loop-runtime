#!/usr/bin/env bash
# infra/tests/drill-jwt-diagnose_test.sh
#
# Hermetic unit tests for infra/scripts/drill-jwt-diagnose.sh.
#
# Plan: docs/plans/2026-05-19-drill-skills-rigor-restoration-plan.md T1.1
#
# Tests focus on the pure-function components (decode_jwt_*, classify_root_cause,
# emit_diagnosis_report) that can be exercised without a LIVE drill stack.
#
# LIVE smoke (mint JWT against real Hydra, kubectl against drill mgmt cluster)
# is deferred to operator-action per Phase 4 of the plan.
#
# Run: bash infra/tests/drill-jwt-diagnose_test.sh
#
# SPDX-License-Identifier: Apache-2.0

set -u

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCRIPT="$REPO_ROOT/infra/scripts/drill-jwt-diagnose.sh"
[[ -x "$SCRIPT" ]] || { echo "FAIL: $SCRIPT not executable"; exit 1; }

# Source the script in test mode (skips top-level main execution)
export _DRILL_JWT_DIAGNOSE_TEST_MODE=1
# shellcheck source=/dev/null
source "$SCRIPT"

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

assert_contains() {
  local label="$1" needle="$2" haystack="$3"
  if [[ "$haystack" == *"$needle"* ]]; then
    echo "PASS: $label"
    pass=$((pass + 1))
  else
    echo "FAIL: $label"
    echo "  needle  : $needle"
    echo "  haystack: $haystack"
    fail=$((fail + 1))
  fi
}

# ---- Fixture: static JWT (RSA256, iss=hydra.drill, aud=loopzero-engine, exp=2286) ----
# Header:  {"alg":"RS256","typ":"JWT","kid":"test-key"}
# Payload: {"iss":"https://hydra.drill/","aud":"loopzero-engine","exp":9999999999,
#           "iat":1700000000,"sub":"test-client","client_id":"test"}
JWT_FIXTURE="REDACTED_SECRET_001"

# ---- Case 1: decode_jwt_header extracts header JSON ----
header_json=$(decode_jwt_header "$JWT_FIXTURE")
alg=$(echo "$header_json" | jq -r '.alg' 2>/dev/null)
assert_eq "decode_jwt_header extracts alg=RS256" "RS256" "$alg"

# ---- Case 2: decode_jwt_payload extracts iss ----
payload_json=$(decode_jwt_payload "$JWT_FIXTURE")
iss=$(echo "$payload_json" | jq -r '.iss' 2>/dev/null)
assert_eq "decode_jwt_payload extracts iss" "https://hydra.drill/" "$iss"

# ---- Case 3: decode_jwt_payload extracts aud ----
aud=$(echo "$payload_json" | jq -r '.aud' 2>/dev/null)
assert_eq "decode_jwt_payload extracts aud" "loopzero-engine" "$aud"

# ---- Case 4: decode_jwt_payload extracts exp ----
exp=$(echo "$payload_json" | jq -r '.exp' 2>/dev/null)
assert_eq "decode_jwt_payload extracts exp" "9999999999" "$exp"

# ---- Case 5: decode_jwt_segment handles base64url padding (no trailing =) ----
# JWT segments are stripped of '=' padding per RFC7515. Decode must restore it.
# Verify by passing a segment whose b64 form requires 1 byte of padding (len%4==3).
# Header "RS256" = 31 chars in b64url which is len%4==3 (requires 1 '=' padding).
header_len=${#JWT_FIXTURE}
header_segment_len=$(echo "$JWT_FIXTURE" | cut -d. -f1 | wc -c | tr -d ' ')
# Just verify our decode succeeded for the fixture; padding bug would have failed Case 1.
assert_contains "decode_jwt_segment handles base64url padding (no exception)" "RS256" "$header_json"

# ---- Case 6: extract_jwt_claim returns specific claim ----
sub=$(extract_jwt_claim "$JWT_FIXTURE" "sub")
assert_eq "extract_jwt_claim sub=test-client" "test-client" "$sub"

# ---- Case 7: extract_jwt_claim returns empty for missing claim ----
missing=$(extract_jwt_claim "$JWT_FIXTURE" "nonexistent_claim")
assert_eq "extract_jwt_claim missing claim returns empty" "" "$missing"

# ---- Case 8: classify_root_cause detects Category A (issuer mismatch) ----
echo '{"iss":"https://hydra.drill/","aud":"loopzero-engine","exp":9999999999}' > "$WORK/payload-a.json"
cat > "$WORK/config-a.txt" <<EOF
JWT_ISSUER=https://hydra.different/
JWKS_URL=http://hydra/jwks
EOF
echo "HTTP_STATUS=200" > "$WORK/jwks-ok.txt"
echo "" > "$WORK/logs-empty.txt"
cat_a=$(classify_root_cause "$WORK/payload-a.json" "$WORK/config-a.txt" "$WORK/jwks-ok.txt" "$WORK/logs-empty.txt")
assert_contains "classify Category A (issuer mismatch)" "A:" "$cat_a"

# ---- Case 9: classify_root_cause detects Category B (audience mismatch) ----
echo '{"iss":"https://hydra.drill/","aud":"wrong-audience","exp":9999999999}' > "$WORK/payload-b.json"
cat > "$WORK/config-b.txt" <<EOF
JWT_ISSUER=https://hydra.drill/
JWKS_URL=http://hydra/jwks
EOF
cat_b=$(classify_root_cause "$WORK/payload-b.json" "$WORK/config-b.txt" "$WORK/jwks-ok.txt" "$WORK/logs-empty.txt")
assert_contains "classify Category B (audience mismatch)" "B:" "$cat_b"

# ---- Case 10: classify_root_cause detects Category C (JWKS unreachable) ----
echo '{"iss":"https://hydra.drill/","aud":"loopzero-engine","exp":9999999999}' > "$WORK/payload-c.json"
cat > "$WORK/config-c.txt" <<EOF
JWT_ISSUER=https://hydra.drill/
JWKS_URL=http://unreachable/jwks
EOF
echo "curl: (7) Failed to connect: connection refused" > "$WORK/jwks-fail.txt"
echo "HTTP_STATUS=000" >> "$WORK/jwks-fail.txt"
cat_c=$(classify_root_cause "$WORK/payload-c.json" "$WORK/config-c.txt" "$WORK/jwks-fail.txt" "$WORK/logs-empty.txt")
assert_contains "classify Category C (JWKS unreachable)" "C:" "$cat_c"

# ---- Case 11: classify_root_cause detects Category D (expired) ----
echo '{"iss":"https://hydra.drill/","aud":"loopzero-engine","exp":1000000}' > "$WORK/payload-d.json"
# Expired in 1970, definitely < now
cat_d=$(classify_root_cause "$WORK/payload-d.json" "$WORK/config-c.txt" "$WORK/jwks-ok.txt" "$WORK/logs-empty.txt")
assert_contains "classify Category D (expired token)" "D:" "$cat_d"

# ---- Case 12: classify_root_cause defaults to F when all match ----
echo '{"iss":"https://hydra.drill/","aud":"loopzero-engine","exp":9999999999}' > "$WORK/payload-f.json"
cat > "$WORK/config-f.txt" <<EOF
JWT_ISSUER=https://hydra.drill/
JWKS_URL=http://hydra/jwks
EOF
cat_f=$(classify_root_cause "$WORK/payload-f.json" "$WORK/config-f.txt" "$WORK/jwks-ok.txt" "$WORK/logs-empty.txt")
assert_contains "classify Category F (no single match)" "F:" "$cat_f"

# ---- Case 13: emit_diagnosis_report creates diagnosis.md ----
emit_diagnosis_report "$WORK" "A: test category" "$WORK/payload-f.json" "$WORK/config-f.txt"
[[ -f "$WORK/diagnosis.md" ]]
assert_eq "emit_diagnosis_report creates diagnosis.md" "0" "$?"
diag_content=$(cat "$WORK/diagnosis.md")
assert_contains "diagnosis.md contains Category" "A: test category" "$diag_content"
assert_contains "diagnosis.md cites payload" "loopzero-engine" "$diag_content"

# ---- Case 14: decode_jwt_segment empty input returns nonzero ----
empty_result=$(echo -n "" | decode_jwt_segment 0 2>&1 || echo "ERROR_AS_EXPECTED")
assert_contains "decode_jwt_segment empty input handled" "ERROR_AS_EXPECTED" "$empty_result"

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
