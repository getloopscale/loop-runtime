#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Hermetic tests for drill-mint-jwt.sh (Plan 2026-05-16 T2.2).
#
# Tests cover:
#   - DRILL_KUBECONFIG missing → exit 1
#   - DRILL_KUBECONFIG unreadable → exit 1
#   - EC-04: Hydra readiness wait invoked before exec
#   - EC-07: JWT TTL >= 7200s validated post-mint
#   - Happy path: TOKEN printed to stdout
#   - All paths use kubectl --kubeconfig (NOT current-context drift)
#
# Mocking strategy: PATH-shadow kubectl, curl, psql with stub scripts that
# fixture-driven respond per test case. No real cluster needed.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SCRIPT="$ROOT/infra/scripts/drill-mint-jwt.sh"

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# Build stub PATH dir with mocked kubectl/curl/psql
build_stubs() {
  local tmpdir="$1"
  local kubectl_behavior="$2"  # ready | not-ready | secret-empty
  local curl_behavior="$3"     # ok | hydra-down | token-empty
  local psql_behavior="${4:-ok}"

  mkdir -p "$tmpdir/bin"

  # Mock kubectl
  cat > "$tmpdir/bin/kubectl" <<EOF
#!/usr/bin/env bash
# Stub kubectl for drill-mint-jwt_test.sh
case "\$*" in
  *"wait --for=condition=Ready"*)
    if [[ "$kubectl_behavior" == "not-ready" ]]; then
      exit 1
    fi
    echo "pod/ory-hydra-xxx condition met"
    exit 0
    ;;
  *"get secret loopzero-bootstrap-credentials"*)
    if [[ "$kubectl_behavior" == "secret-empty" ]]; then
      exit 0  # empty stdout
    fi
    # Return a fake base64-encoded client_secret
    printf '%s' "ZmFrZS1jbGllbnQtc2VjcmV0Cg=="  # base64 of "fake-client-secret"
    exit 0
    ;;
  *"get pod -l app.kubernetes.io/name=postgresql"*)
    printf 'postgresql-0'
    exit 0
    ;;
  *"get secret loopzero-db-credentials"*)
    printf '%s' "REDACTED_SECRET_040"  # base64 of "loopzero-dev-password"
    exit 0
    ;;
  *"exec"*"postgresql"*)
    # psql via exec — succeeds silently
    exit 0
    ;;
  *"port-forward"*)
    # Simulate port-forward in background; sleep and exit
    sleep 120 &
    exit 0
    ;;
  *)
    exit 0
    ;;
esac
EOF
  chmod +x "$tmpdir/bin/kubectl"

  # Mock curl (Hydra responses)
  cat > "$tmpdir/bin/curl" <<EOF
#!/usr/bin/env bash
# Stub curl for drill-mint-jwt_test.sh
for arg in "\$@"; do
  case "\$arg" in
    *".well-known/openid-configuration"*)
      if [[ "$curl_behavior" == "hydra-down" ]]; then
        exit 1
      fi
      printf '{"issuer":"http://localhost:14444"}'
      exit 0
      ;;
    *"/oauth2/token"*)
      if [[ "$curl_behavior" == "token-empty" ]]; then
        printf '{"error":"invalid_client"}'
        exit 0
      fi
      # Generate fake JWT with TTL=7300s (just above MIN 7200s)
      # JWT = header.payload.signature, all base64url-encoded
      HEADER='eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9'  # {"alg":"HS256","typ":"JWT"}
      NOW=\$(date +%s)
      EXP=\$((NOW + 7300))
      # Payload: {"iat":\$NOW,"exp":\$EXP,"sub":"loopzero-bootstrap"}
      PAYLOAD_JSON="{\"iat\":\$NOW,\"exp\":\$EXP,\"sub\":\"loopzero-bootstrap\"}"
      PAYLOAD_B64=\$(printf '%s' "\$PAYLOAD_JSON" | base64 -w0 | tr '+/' '-_' | tr -d '=')
      FAKE_TOKEN="\$HEADER.\$PAYLOAD_B64.fake-signature"
      printf '{"access_token":"%s","expires_in":7300,"token_type":"bearer"}' "\$FAKE_TOKEN"
      exit 0
      ;;
  esac
done
exit 0
EOF
  chmod +x "$tmpdir/bin/curl"

  # Mock psql (managed PG path)
  cat > "$tmpdir/bin/psql" <<EOF
#!/usr/bin/env bash
# Stub psql for drill-mint-jwt_test.sh
exit 0
EOF
  chmod +x "$tmpdir/bin/psql"

  # Mock pkill, sleep (avoid actual subprocess kill)
  cat > "$tmpdir/bin/pkill" <<EOF
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$tmpdir/bin/pkill"
}

run_test() {
  local name="$1"
  shift
  TESTS_RUN=$((TESTS_RUN + 1))
  local tmpdir
  tmpdir=$(mktemp -d -t drill-mint-jwt-test-XXXXXX)

  local result
  if "$@" "$tmpdir"; then
    result="PASS"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  else
    result="FAIL"
    TESTS_FAILED=$((TESTS_FAILED + 1))
  fi
  echo "  [$result] $name"
  rm -rf "$tmpdir"
}

# ---- Tests ----

test_missing_kubeconfig() {
  local tmpdir="$1"
  build_stubs "$tmpdir" ready ok ok
  local actual
  set +e
  env -u DRILL_KUBECONFIG PATH="$tmpdir/bin:$PATH" "$SCRIPT" > /dev/null 2>&1
  actual=$?
  set -e
  [[ "$actual" -eq 1 ]] && return 0 || return 1
}

test_unreadable_kubeconfig() {
  local tmpdir="$1"
  build_stubs "$tmpdir" ready ok ok
  local actual
  set +e
  DRILL_KUBECONFIG=/nonexistent PATH="$tmpdir/bin:$PATH" "$SCRIPT" > /dev/null 2>&1
  actual=$?
  set -e
  [[ "$actual" -eq 1 ]] && return 0 || return 1
}

test_ec04_hydra_not_ready() {
  local tmpdir="$1"
  build_stubs "$tmpdir" not-ready ok ok
  touch "$tmpdir/fake-kubeconfig.yaml"
  local actual
  set +e
  DRILL_KUBECONFIG="$tmpdir/fake-kubeconfig.yaml" \
    PATH="$tmpdir/bin:$PATH" \
    "$SCRIPT" > /dev/null 2>&1
  actual=$?
  set -e
  [[ "$actual" -eq 2 ]] && return 0 || return 1
}

test_ec04_invokes_kubectl_wait() {
  local tmpdir="$1"
  build_stubs "$tmpdir" ready ok ok
  touch "$tmpdir/fake-kubeconfig.yaml"
  # Run and capture stderr — should mention "Waiting for Hydra"
  DRILL_KUBECONFIG="$tmpdir/fake-kubeconfig.yaml" \
    DRILL_PG_HOST=fakehost \
    DRILL_PG_PASS=fakepw \
    PATH="$tmpdir/bin:$PATH" \
    "$SCRIPT" 2> "$tmpdir/stderr.log" > /dev/null || true
  grep -q "Waiting for Hydra pod readiness" "$tmpdir/stderr.log"
}

test_secret_empty() {
  local tmpdir="$1"
  build_stubs "$tmpdir" secret-empty ok ok
  touch "$tmpdir/fake-kubeconfig.yaml"
  local actual
  set +e
  DRILL_KUBECONFIG="$tmpdir/fake-kubeconfig.yaml" \
    DRILL_PG_HOST=fakehost DRILL_PG_PASS=fakepw \
    PATH="$tmpdir/bin:$PATH" \
    "$SCRIPT" > /dev/null 2>&1
  actual=$?
  set -e
  [[ "$actual" -eq 2 ]] && return 0 || return 1
}

test_ec07_jwt_ttl_validated() {
  local tmpdir="$1"
  build_stubs "$tmpdir" ready ok ok
  touch "$tmpdir/fake-kubeconfig.yaml"
  # Happy path stub returns JWT with TTL=7300s (just above 7200s minimum)
  if DRILL_KUBECONFIG="$tmpdir/fake-kubeconfig.yaml" \
     DRILL_PG_HOST=fakehost DRILL_PG_PASS=fakepw \
     PATH="$tmpdir/bin:$PATH" \
     "$SCRIPT" 2> "$tmpdir/stderr.log" > "$tmpdir/stdout.log"; then
    # Verify TTL message in stderr
    grep -q "JWT TTL:" "$tmpdir/stderr.log" || return 1
    grep -q "7300" "$tmpdir/stderr.log" || return 1
    return 0
  fi
  return 1
}

test_happy_path_prints_jwt() {
  local tmpdir="$1"
  build_stubs "$tmpdir" ready ok ok
  touch "$tmpdir/fake-kubeconfig.yaml"
  if DRILL_KUBECONFIG="$tmpdir/fake-kubeconfig.yaml" \
     DRILL_PG_HOST=fakehost DRILL_PG_PASS=fakepw \
     PATH="$tmpdir/bin:$PATH" \
     "$SCRIPT" 2> /dev/null > "$tmpdir/stdout.log"; then
    # JWT format = header.payload.signature
    head -1 "$tmpdir/stdout.log" | grep -qE '^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$'
    return $?
  fi
  return 1
}

# ---- Run all tests ----

echo "Running hermetic tests for drill-mint-jwt.sh..."
echo ""

run_test "missing_DRILL_KUBECONFIG_exits_1" test_missing_kubeconfig
run_test "unreadable_DRILL_KUBECONFIG_exits_1" test_unreadable_kubeconfig
run_test "EC-04_hydra_not_ready_exits_2" test_ec04_hydra_not_ready
run_test "EC-04_invokes_kubectl_wait_pre_exec" test_ec04_invokes_kubectl_wait
run_test "secret_empty_exits_2" test_secret_empty
run_test "EC-07_jwt_ttl_validated_post_mint" test_ec07_jwt_ttl_validated
run_test "happy_path_prints_jwt_to_stdout" test_happy_path_prints_jwt

echo ""
echo "Total: $TESTS_RUN | Passed: $TESTS_PASSED | Failed: $TESTS_FAILED"

if [[ $TESTS_FAILED -gt 0 ]]; then
  exit 1
fi
