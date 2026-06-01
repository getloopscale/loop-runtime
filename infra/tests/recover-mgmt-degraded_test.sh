#!/usr/bin/env bash
# Hermetic test for infra/scripts/local/recover-mgmt-degraded.sh
# T0.2 of plan 2026-05-29-dashboard-public-integration-canonicalization
# v1.1 EC-03 + EC-D1
#
# Test angles:
#   1. Dry-run mode does not invoke kubectl delete/rollout
#   2. Backoff retry: when first 2 kubectl calls fail, 3rd succeeds → script proceeds
#   3. After 3 retries all failing, script exits 1 with operator hint
#   4. Stuck pod (deletionTimestamp > GRACE_SECONDS) triggers force-delete
#   5. Degraded deployment (unavailableReplicas > 0) triggers rollout restart
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$REPO_ROOT/scripts/local/recover-mgmt-degraded.sh"

if [ ! -x "$SCRIPT" ]; then
  chmod +x "$SCRIPT"
fi

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

# Stage a mock kubectl on PATH
cat > "$TMPDIR/kubectl" <<'EOF'
#!/usr/bin/env bash
# Mock kubectl driven by MOCK_KUBECTL_BEHAVIOR file
LOG_FILE="${MOCK_KUBECTL_LOG:-/dev/null}"
echo "kubectl $*" >> "$LOG_FILE"

# Drop the --context= and --request-timeout= prefixes for matching
ARGS=()
for a in "$@"; do
  case "$a" in
    --context=*|--request-timeout=*) ;;
    *) ARGS+=("$a") ;;
  esac
done

BEHAVIOR="${MOCK_KUBECTL_BEHAVIOR:-pass}"

case "$BEHAVIOR" in
  pass)
    # Default: emit empty json items list
    if [[ "${ARGS[0]:-}" == "get" ]]; then
      echo '{"items":[]}'
    fi
    exit 0
    ;;
  fail-then-pass)
    # Counter file tracks calls
    COUNT_FILE="${MOCK_KUBECTL_COUNTER:-/tmp/.mock_kubectl_count}"
    if [ ! -f "$COUNT_FILE" ]; then echo 0 > "$COUNT_FILE"; fi
    COUNT=$(cat "$COUNT_FILE")
    NEW=$((COUNT + 1))
    echo "$NEW" > "$COUNT_FILE"
    if [ "$COUNT" -lt 2 ]; then
      echo "context deadline exceeded" >&2
      exit 1
    fi
    if [[ "${ARGS[0]:-}" == "get" ]]; then
      echo '{"items":[]}'
    fi
    exit 0
    ;;
  always-fail)
    echo "context deadline exceeded" >&2
    exit 1
    ;;
  stuck-pod)
    # Return one pod with deletionTimestamp 10 min ago
    if [[ "${ARGS[0]:-}" == "get" && "${ARGS[1]:-}" == "pods" ]]; then
      cat <<JSON
{"items":[{"metadata":{"name":"stuck-pod-x","deletionTimestamp":"$(date -u -d '10 minutes ago' '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || date -u -v-10M '+%Y-%m-%dT%H:%M:%SZ')"},"status":{"containerStatuses":[{"ready":true}]}}]}
JSON
    elif [[ "${ARGS[0]:-}" == "delete" && "${ARGS[1]:-}" == "pod" ]]; then
      : # accept the delete
    else
      echo '{"items":[]}'
    fi
    exit 0
    ;;
  degraded-deploy)
    if [[ "${ARGS[0]:-}" == "get" && "${ARGS[1]:-}" == "deploy" ]]; then
      cat <<JSON
{"items":[{"metadata":{"name":"loopzero-cloud"},"spec":{"replicas":2},"status":{"unavailableReplicas":1}}]}
JSON
    elif [[ "${ARGS[0]:-}" == "rollout" ]]; then
      : # accept rollout restart
    else
      echo '{"items":[]}'
    fi
    exit 0
    ;;
  *)
    echo "Unknown MOCK_KUBECTL_BEHAVIOR: $BEHAVIOR" >&2
    exit 99
    ;;
esac
EOF
chmod +x "$TMPDIR/kubectl"

export PATH="$TMPDIR:$PATH"
# Speed up retry sleeps for hermetic tests (production uses 5/15/30)
export KC_RETRY_DELAYS="1 1 1"

PASS=0
FAIL=0
check() {
  local name="$1"; shift
  if "$@"; then
    echo "  PASS: $name"; PASS=$((PASS+1))
  else
    echo "  FAIL: $name"; FAIL=$((FAIL+1))
  fi
}

echo "=== Test 1: dry-run mode does not invoke kubectl delete or rollout ==="
export MOCK_KUBECTL_LOG="$TMPDIR/log1"
: > "$MOCK_KUBECTL_LOG"
unset MOCK_KUBECTL_COUNTER
export MOCK_KUBECTL_BEHAVIOR=pass
"$SCRIPT" --dry-run >/dev/null 2>&1
check "no delete in dry-run" sh -c "! grep -q '^kubectl delete' $MOCK_KUBECTL_LOG"
check "no rollout in dry-run" sh -c "! grep -q '^kubectl rollout' $MOCK_KUBECTL_LOG"

echo ""
echo "=== Test 2: backoff retry — 2 failures then success ==="
export MOCK_KUBECTL_LOG="$TMPDIR/log2"
export MOCK_KUBECTL_COUNTER="$TMPDIR/counter2"
: > "$MOCK_KUBECTL_LOG"
rm -f "$MOCK_KUBECTL_COUNTER"
export MOCK_KUBECTL_BEHAVIOR=fail-then-pass
# Use a very short timeout to avoid eating the full 5+15+30 sleep cascade in the test
# We modify behavior: 1st kubectl call (get pods) fails twice, succeeds 3rd.
# That single call retries internally; remainder of script proceeds.
# We'll patch the script's delays via env? No — script hardcodes (5 15 30) but
# the GRACE between attempts is bounded by `sleep` calls. To test reasonable
# time we accept up to ~50s. For hermetic gate we accept the time hit.
timeout 80 "$SCRIPT" --dry-run >/dev/null 2>&1 || true
ATTEMPTS=$(cat "$MOCK_KUBECTL_COUNTER" 2>/dev/null || echo "0")
check "retried >= 3 times" sh -c "[ '$ATTEMPTS' -ge 3 ]"

echo ""
echo "=== Test 3: always-fail exits 1 with operator hint ==="
export MOCK_KUBECTL_LOG="$TMPDIR/log3"
: > "$MOCK_KUBECTL_LOG"
unset MOCK_KUBECTL_COUNTER
export MOCK_KUBECTL_BEHAVIOR=always-fail
set +e
OUT=$(timeout 80 "$SCRIPT" --dry-run 2>&1)
EXIT_CODE=$?
set -e
check "exits non-zero" sh -c "[ '$EXIT_CODE' -ne 0 ]"
check "prints operator hint" bash -c "grep -q 'Apiserver unreachable' <<< \"\$1\"" -- "$OUT"
check "mentions restart options" bash -c "grep -q 'docker restart k3d-loopzero-mgmt-server' <<< \"\$1\"" -- "$OUT"

echo ""
echo "=== Test 4: stuck pod triggers force-delete ==="
export MOCK_KUBECTL_LOG="$TMPDIR/log4"
: > "$MOCK_KUBECTL_LOG"
unset MOCK_KUBECTL_COUNTER
export MOCK_KUBECTL_BEHAVIOR=stuck-pod
"$SCRIPT" --dry-run >/dev/null 2>&1 || true
# In dry-run we expect the log to MENTION the delete intent but NOT the actual command
LOGGED=$("$SCRIPT" --dry-run 2>&1 || true)
check "dry-run shows force-delete intent for stuck-pod-x" sh -c "echo '$LOGGED' | grep -q 'force-deleting stuck Terminating pod: stuck-pod-x'"

echo ""
echo "=== Test 5: degraded deployment triggers rollout restart ==="
export MOCK_KUBECTL_LOG="$TMPDIR/log5"
: > "$MOCK_KUBECTL_LOG"
export MOCK_KUBECTL_BEHAVIOR=degraded-deploy
LOGGED=$("$SCRIPT" --dry-run 2>&1 || true)
check "dry-run shows rollout restart intent for loopzero-cloud" sh -c "echo '$LOGGED' | grep -q 'rollout restart: deploy/loopzero-cloud'"

echo ""
echo "============================="
echo "PASS=$PASS  FAIL=$FAIL"
echo "============================="
[ "$FAIL" -eq 0 ]
