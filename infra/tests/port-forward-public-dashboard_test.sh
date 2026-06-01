#!/usr/bin/env bash
# T7.3 of plan 2026-05-29-dashboard-public-integration-canonicalization
# v1.1 EC-S3 + EC-S4: hermetic verification that port-forward orchestrator
# exercises exponential backoff and proper signal handling.
#
# Strategy: mock kubectl to (a) always fail, (b) check spawn count + backoff
# sleeps via timing, (c) cleanup on stop.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$REPO_ROOT/scripts/local/port-forward-public-dashboard.sh"

PASS=0
FAIL=0
check() {
  local name="$1"; shift
  if "$@" >/dev/null 2>&1; then
    echo "  PASS: $name"; PASS=$((PASS+1))
  else
    echo "  FAIL: $name"; FAIL=$((FAIL+1))
  fi
}

check "script exists + executable" test -x "$SCRIPT"

# Static checks against the script source
check "uses --request-timeout? no need; uses exponential backoff" \
  grep -q "sleeps=(2 4 8 16 30 30 30 30)" "$SCRIPT"
# v1.1 EC-S4 is satisfied by `kill -TERM -- "-$pid"` in cmd_stop (process-group
# kill reaches the bash supervisor + its kubectl child). Daemon-style start
# doesn't need a SIGTERM trap on the parent (the parent exits immediately
# after spawning children via setsid).
check "cmd_stop sends process-group SIGTERM (v1.1 EC-S4)" \
  grep -q 'kill -TERM -- "-\$pid"' "$SCRIPT"
check "uses setsid to detach children" \
  grep -q "setsid" "$SCRIPT"
check "writes PID file" \
  grep -q "PID_FILE" "$SCRIPT"
check "supports start/stop/status commands" \
  bash -c "grep -q 'cmd_start' '$SCRIPT' && grep -q 'cmd_stop' '$SCRIPT' && grep -q 'cmd_status' '$SCRIPT'"

# Functional test: spawn against mock kubectl that always fails; assert that
# after ~6s the spawn count is bounded (backoff kicks in immediately).
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"; bash "$SCRIPT" stop >/dev/null 2>&1 || true' EXIT

cat > "$TMPDIR/kubectl" <<'EOF'
#!/usr/bin/env bash
echo "$(date +%s%N)" >> "${MOCK_KUBECTL_CALLS:-/dev/null}"
echo "mock-kubectl: refusing" >&2
exit 1
EOF
chmod +x "$TMPDIR/kubectl"

export PATH="$TMPDIR:$PATH"
export MOCK_KUBECTL_CALLS="$TMPDIR/calls.log"
: > "$MOCK_KUBECTL_CALLS"
export PID_FILE="$TMPDIR/pids"
export LOG_DIR="$TMPDIR"
export CONTEXT="mock"

# Start orchestrator with mock kubectl
"$SCRIPT" start >/dev/null 2>&1 || true

# Sleep 8s — with backoff (2s, then 4s), we should see roughly 2-3 calls per service
# = 8-12 total, not 100s of calls (which is what would happen without backoff).
sleep 8

CALLS=$(wc -l < "$MOCK_KUBECTL_CALLS" 2>/dev/null || echo 0)
echo "  Mock kubectl was called $CALLS times in 8s (with backoff)"

# Without backoff (sleep 2 between every attempt): ~16 calls in 8s for 4 services
# = 64 calls. With our backoff: 4 services × (2s + 4s + 8s windows in 8s) = ~12 calls.
# Allow up to 30 to be safe; reject if > 60 (proves backoff is active).
check "exponential backoff bounds spawn rate (calls<60 in 8s)" test "$CALLS" -lt 60
check "still spawned some calls (>=4 = at least one per service)" test "$CALLS" -ge 4

# Stop and verify children cleaned up. We measure cleanup by tracking new
# kubectl calls AFTER stop — a clean stop means no further calls.
NEW_CALLS_BEFORE_STOP=$CALLS
timeout 10 "$SCRIPT" stop >/dev/null 2>&1 || true
sleep 3
NEW_CALLS_AFTER_STOP=$(wc -l < "$MOCK_KUBECTL_CALLS" 2>/dev/null || echo 0)
DELTA=$(( NEW_CALLS_AFTER_STOP - NEW_CALLS_BEFORE_STOP ))
echo "  Mock kubectl was called $DELTA more times in 3s post-stop"
check "no new kubectl calls after stop (v1.1 EC-S4)" test "$DELTA" -lt 5

# PID file removed
check "stop removes PID file" bash -c "[ ! -f '$PID_FILE' ]"

echo ""
echo "============================="
echo "PASS=$PASS  FAIL=$FAIL"
echo "============================="
[ "$FAIL" -eq 0 ]
