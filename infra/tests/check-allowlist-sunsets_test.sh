#!/usr/bin/env bash
# Hermetic test for .claude/scripts/check-allowlist-sunsets.py.
# T5.2.a of plan 2026-06-11-code-review-remediation-147-findings v1.2.
#
# Seeds 3 fixture allowlists (future / today / past) + a malformed one,
# asserts the script's exit codes match the contract:
#   0 — all sunsets future OR today (today is WARN, not block)
#   1 — at least one sunset is past
#   2 — parse error (malformed entry)
#
# Wired in: task quality:gates (per rules/shell-scripts-allowlist.md)

set -uo pipefail

WORKTREE_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$WORKTREE_ROOT"

GATE="$WORKTREE_ROOT/.claude/scripts/check-allowlist-sunsets.py"

if [ ! -x "$GATE" ]; then
    echo "❌ FAIL: gate script $GATE not executable"
    exit 1
fi

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM

FUTURE_DATE=$(date -d "+30 days" +%Y-%m-%d)
TODAY=$(date +%Y-%m-%d)
PAST_DATE=$(date -d "-1 day" +%Y-%m-%d)

# --- Fixture 1: all future entries → exit 0 ---
cat > "$TMP/future.txt" <<EOF
# header comment
fid-1 | refactor | $FUTURE_DATE | test rationale
fid-2 | maturity | $FUTURE_DATE | another rationale
EOF
python3 "$GATE" "$TMP/future.txt" > "$TMP/future.out" 2>&1
RC=$?
if [ "$RC" -ne 0 ]; then
    echo "❌ FAIL: future-only allowlist must exit 0, got $RC"
    cat "$TMP/future.out"
    exit 1
fi
if ! grep -q "PASS" "$TMP/future.out"; then
    echo "❌ FAIL: future-only allowlist output missing PASS marker"
    cat "$TMP/future.out"
    exit 1
fi

# --- Fixture 2: today-only entry → exit 0 + WARN ---
cat > "$TMP/today.txt" <<EOF
fid-today | refactor | $TODAY | test today entry
EOF
python3 "$GATE" "$TMP/today.txt" > "$TMP/today.out" 2>&1
RC=$?
if [ "$RC" -ne 0 ]; then
    echo "❌ FAIL: today-only allowlist must exit 0, got $RC"
    cat "$TMP/today.out"
    exit 1
fi
if ! grep -q "WARN" "$TMP/today.out"; then
    echo "❌ FAIL: today-only allowlist must emit WARN"
    cat "$TMP/today.out"
    exit 1
fi

# --- Fixture 3: past entry → exit 1 ---
cat > "$TMP/past.txt" <<EOF
fid-past | refactor | $PAST_DATE | test past entry
EOF
python3 "$GATE" "$TMP/past.txt" > "$TMP/past.out" 2>&1
RC=$?
if [ "$RC" -ne 1 ]; then
    echo "❌ FAIL: past-sunset allowlist must exit 1, got $RC"
    cat "$TMP/past.out"
    exit 1
fi
if ! grep -q "past sunset" "$TMP/past.out"; then
    echo "❌ FAIL: past-sunset error message missing 'past sunset' marker"
    cat "$TMP/past.out"
    exit 1
fi

# --- Fixture 4: malformed entry → exit 2 ---
cat > "$TMP/malformed.txt" <<EOF
fid-bad | only_two_columns
EOF
python3 "$GATE" "$TMP/malformed.txt" > "$TMP/malformed.out" 2>&1
RC=$?
if [ "$RC" -ne 2 ]; then
    echo "❌ FAIL: malformed allowlist must exit 2, got $RC"
    cat "$TMP/malformed.out"
    exit 1
fi
if ! grep -q "malformed" "$TMP/malformed.out"; then
    echo "❌ FAIL: malformed error message missing 'malformed' marker"
    cat "$TMP/malformed.out"
    exit 1
fi

# --- Fixture 5: bad date format → exit 2 ---
cat > "$TMP/baddate.txt" <<EOF
fid-baddate | refactor | not-a-date | wrong format
EOF
python3 "$GATE" "$TMP/baddate.txt" > "$TMP/baddate.out" 2>&1
RC=$?
if [ "$RC" -ne 2 ]; then
    echo "❌ FAIL: bad-date allowlist must exit 2, got $RC"
    cat "$TMP/baddate.out"
    exit 1
fi
if ! grep -q "bad sunset_date" "$TMP/baddate.out"; then
    echo "❌ FAIL: bad-date error message missing 'bad sunset_date' marker"
    cat "$TMP/baddate.out"
    exit 1
fi

# --- Fixture 6: missing allowlist file → exit 0 ---
python3 "$GATE" "$TMP/does-not-exist.txt" > "$TMP/missing.out" 2>&1
RC=$?
if [ "$RC" -ne 0 ]; then
    echo "❌ FAIL: missing allowlist must exit 0 (no deferrals declared), got $RC"
    cat "$TMP/missing.out"
    exit 1
fi

# --- Live check on the real allowlist file ---
python3 "$GATE" "$WORKTREE_ROOT/rules/code-review-allowlist.txt" > "$TMP/live.out" 2>&1
RC=$?
if [ "$RC" -ne 0 ]; then
    echo "❌ FAIL: real rules/code-review-allowlist.txt has past-sunset entries"
    cat "$TMP/live.out"
    exit 1
fi

echo "✅ check-allowlist-sunsets: 7/7 assertions PASS (future PASS, today WARN, past FAIL, malformed FAIL, bad-date FAIL, missing OK, live OK)"
exit 0
