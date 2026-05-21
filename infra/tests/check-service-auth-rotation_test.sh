#!/usr/bin/env bash
# infra/tests/check-service-auth-rotation_test.sh
#
# ADR 2026-05-13-service-auth-ed25519-adoption regression suite. The
# check-service-auth-rotation.sh script is dormant pre-rotation and
# ratcheting once a rotation date is recorded. These tests prove each
# stage of the ratchet — 60d slip-alert window starts at day 61 (not 76
# like cosign-cutover because rotation cadence is shorter), 90d hard
# deadline matches cosign-cutover.

set -euo pipefail

set -u

SCRIPT="$(cd "$(dirname "$0")/../.." && pwd)/infra/scripts/check-service-auth-rotation.sh"
[[ -x "$SCRIPT" ]] || { echo "FAIL: $SCRIPT not executable"; exit 1; }

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/.github"
mkdir -p "$WORK/infra/scripts"
cp "$SCRIPT" "$WORK/infra/scripts/check-service-auth-rotation.sh"
chmod +x "$WORK/infra/scripts/check-service-auth-rotation.sh"
TESTABLE="$WORK/infra/scripts/check-service-auth-rotation.sh"

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

reset() {
  rm -f "$WORK/.github/service-auth-ed25519-rotation-date.txt"
  rm -f "$WORK/.github/service-auth-rotation-exception.txt"
}

# CASE 1 — pre-rotation (no marker) → DORMANT → exit 0
reset
run_case "pre-rotation-no-marker" 0

# CASE 2 — within 60-day budget (day 30 of 90) → exit 0
reset
echo "2026-06-01" > "$WORK/.github/service-auth-ed25519-rotation-date.txt"
run_case "within-budget-day30" 0 --as-of "2026-07-01"

# CASE 3 — at exact day 60 (boundary, still in budget) → exit 0
reset
echo "2026-06-01" > "$WORK/.github/service-auth-ed25519-rotation-date.txt"
run_case "day60-boundary" 0 --as-of "2026-07-31"

# CASE 4 — slip-alert day 61 → exit 0 (warns, doesn't block)
reset
echo "2026-06-01" > "$WORK/.github/service-auth-ed25519-rotation-date.txt"
run_case "slip-alert-day61" 0 --as-of "2026-08-01"

# CASE 5 — exact day 90 → exit 0 (still inside slip-alert window)
reset
echo "2026-06-01" > "$WORK/.github/service-auth-ed25519-rotation-date.txt"
run_case "day90-boundary" 0 --as-of "2026-08-30"

# CASE 6 — day 91 without exception → BLOCK exit 2
reset
echo "2026-06-01" > "$WORK/.github/service-auth-ed25519-rotation-date.txt"
run_case "day91-block" 2 --as-of "2026-08-31"

# CASE 7 — day 91 WITH signed exception → exit 0
reset
echo "2026-06-01" > "$WORK/.github/service-auth-ed25519-rotation-date.txt"
cat > "$WORK/.github/service-auth-rotation-exception.txt" <<'EOF'
slip-rationale: HSM provisioning delay on rotation infra
approved-by: paulohenrique.java@gmail.com 2026-08-30
EOF
run_case "day91-with-exception" 0 --as-of "2026-08-31"

# CASE 8 — day 91 with unsigned exception → BLOCK
reset
echo "2026-06-01" > "$WORK/.github/service-auth-ed25519-rotation-date.txt"
cat > "$WORK/.github/service-auth-rotation-exception.txt" <<'EOF'
slip-rationale: filed but never signed
EOF
run_case "day91-unsigned-exception" 2 --as-of "2026-08-31"

# CASE 9 — marker exists but empty → usage error exit 1
reset
echo "" > "$WORK/.github/service-auth-ed25519-rotation-date.txt"
run_case "empty-marker" 1

# CASE 10 — marker garbage → usage error exit 1
reset
echo "not-a-date" > "$WORK/.github/service-auth-ed25519-rotation-date.txt"
run_case "garbage-marker" 1

echo ""
echo "Results: $pass passed, $fail failed"
[[ "$fail" -eq 0 ]] && exit 0 || exit 1
