#!/usr/bin/env bash
# infra/tests/check-cosign-cutover-deadline_test.sh
#
# Audit gap §6.B.4 regression suite. The check-cosign-cutover-deadline.sh
# script is dormant pre-GA and ratcheting post-GA. These tests prove each
# stage of the ratchet.

set -u

SCRIPT="$(cd "$(dirname "$0")/../.." && pwd)/infra/scripts/check-cosign-cutover-deadline.sh"
[[ -x "$SCRIPT" ]] || { echo "FAIL: $SCRIPT not executable"; exit 1; }

# Isolate marker + exception files in a temp workspace so we don't touch
# the real .github/ directory.
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/.github"

# The script reads files relative to its repo-root resolution. We point
# the script at our fake repo root by symlinking the script under WORK
# and resolving its repo via that path.
mkdir -p "$WORK/infra/scripts"
cp "$SCRIPT" "$WORK/infra/scripts/check-cosign-cutover-deadline.sh"
chmod +x "$WORK/infra/scripts/check-cosign-cutover-deadline.sh"
TESTABLE="$WORK/infra/scripts/check-cosign-cutover-deadline.sh"

pass=0
fail=0

run_case() {
  local name="$1"
  local want_exit="$2"
  shift 2

  "$TESTABLE" "$@" >"$WORK/${name}.out" 2>&1
  local got_exit=$?

  if [[ "$got_exit" -eq "$want_exit" ]]; then
    echo "PASS: $name (exit=$got_exit)"
    pass=$((pass + 1))
  else
    echo "FAIL: $name (want exit=$want_exit, got=$got_exit)"
    echo "  stdout: $(cat "$WORK/${name}.out")"
    fail=$((fail + 1))
  fi
}

# Reset the marker + exception to baseline between cases.
reset() {
  rm -f "$WORK/.github/v1.0-ga-date.txt"
  rm -f "$WORK/.github/cosign-cutover-exception.txt"
}

# ---------------------------------------------------------------------------
# CASE 1 — pre-GA (no marker) → DORMANT → exit 0
# ---------------------------------------------------------------------------
reset
run_case "pre-ga-no-marker" 0

# ---------------------------------------------------------------------------
# CASE 2 — within 75-day budget (day 30 of 90) → exit 0
# ---------------------------------------------------------------------------
reset
echo "2026-06-01" > "$WORK/.github/v1.0-ga-date.txt"
run_case "within-budget-day30" 0 --as-of "2026-07-01"

# ---------------------------------------------------------------------------
# CASE 3 — at exact day 75 (boundary) → exit 0 (silent OK, just under warn)
# ---------------------------------------------------------------------------
reset
echo "2026-06-01" > "$WORK/.github/v1.0-ga-date.txt"
run_case "day75-boundary" 0 --as-of "2026-08-15"

# ---------------------------------------------------------------------------
# CASE 4 — slip-alert window day 76 → exit 0 (warns, doesn't block)
# ---------------------------------------------------------------------------
reset
echo "2026-06-01" > "$WORK/.github/v1.0-ga-date.txt"
run_case "slip-alert-day76" 0 --as-of "2026-08-16"

# ---------------------------------------------------------------------------
# CASE 5 — exactly day 90 → exit 0 (still inside the slip-alert window)
# ---------------------------------------------------------------------------
reset
echo "2026-06-01" > "$WORK/.github/v1.0-ga-date.txt"
run_case "day90-boundary" 0 --as-of "2026-08-30"

# ---------------------------------------------------------------------------
# CASE 6 — day 91 without exception → BLOCK exit 2
# ---------------------------------------------------------------------------
reset
echo "2026-06-01" > "$WORK/.github/v1.0-ga-date.txt"
run_case "day91-block" 2 --as-of "2026-08-31"

# ---------------------------------------------------------------------------
# CASE 7 — day 91 WITH signed exception → exit 0 (slip-permitted)
# ---------------------------------------------------------------------------
reset
echo "2026-06-01" > "$WORK/.github/v1.0-ga-date.txt"
cat > "$WORK/.github/cosign-cutover-exception.txt" <<'EOF'
slip-rationale: vendor outage on cosign keyless infrastructure
approved-by: paulohenrique.java@gmail.com 2026-08-30
EOF
run_case "day91-with-exception" 0 --as-of "2026-08-31"

# ---------------------------------------------------------------------------
# CASE 8 — day 91 with EXCEPTION file but no `approved-by:` line → BLOCK
# ---------------------------------------------------------------------------
reset
echo "2026-06-01" > "$WORK/.github/v1.0-ga-date.txt"
cat > "$WORK/.github/cosign-cutover-exception.txt" <<'EOF'
slip-rationale: filed but never signed
EOF
run_case "day91-unsigned-exception" 2 --as-of "2026-08-31"

# ---------------------------------------------------------------------------
# CASE 9 — marker file exists but is empty → usage error exit 1
# ---------------------------------------------------------------------------
reset
echo "" > "$WORK/.github/v1.0-ga-date.txt"
run_case "empty-marker" 1

# ---------------------------------------------------------------------------
# CASE 10 — marker file contains garbage → usage error exit 1
# ---------------------------------------------------------------------------
reset
echo "not-a-date" > "$WORK/.github/v1.0-ga-date.txt"
run_case "garbage-marker" 1

echo ""
echo "Results: $pass passed, $fail failed"
[[ "$fail" -eq 0 ]] && exit 0 || exit 1
