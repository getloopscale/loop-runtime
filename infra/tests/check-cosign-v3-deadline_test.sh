#!/usr/bin/env bash
# check-cosign-v3-deadline_test.sh — Hermetic test for T0.2 of plan
# 2026-05-28-meet-001-engineering-closure (ADR-2026-05-28-cosign-v2.4.3-deadline).
#
# Validates that check-cosign-cutover-deadline.sh enforces the v3 upgrade
# deadline based on ConfigMap `loopzero-cosign-version-config` with REQUIRED
# `target_version` + `set_at` fields:
#
#   - target_version absent → PASS (no upgrade pending)
#   - target_version=v3 + set_at within 60d → PASS
#   - target_version=v3 + set_at 60-90d ago → PASS with WARN to stderr
#   - target_version=v3 + set_at >90d ago → FAIL exit 1
#   - target_version=v3 without set_at → FAIL exit 1 (EC-1 ambiguity guard)
#
# All tests use mock kubectl to avoid live cluster dependency.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TARGET_SCRIPT="$REPO_ROOT/scripts/security/check-cosign-cutover-deadline.sh"

[[ -f "$TARGET_SCRIPT" ]] || { echo "FATAL: target script not found at $TARGET_SCRIPT"; exit 2; }

PASS=0
FAIL=0

# Mock kubectl that reads fixtures by ConfigMap name + jsonpath key.
# Fixtures defined inline via env vars set per case:
#   MOCK_LOOPZERO_COSIGN_CONFIG_MODE         (default: keyless — keyless mode bypasses existing deadline)
#   MOCK_V3_TARGET_VERSION               (empty / "v3")
#   MOCK_V3_SET_AT                       (empty / "YYYY-MM-DD")

MOCK_BIN="$(mktemp -d)"
trap 'rm -rf "$MOCK_BIN"' EXIT

cat > "$MOCK_BIN/kubectl" <<'EOF'
#!/usr/bin/env bash
# Mock: kubectl get cm <name> -n <ns> -o jsonpath="{.data.<field>}"
# Reads field by name from MOCK_<NAME>_<UPPER_FIELD> env vars.
NAME=""
FIELD=""
for ((i=1; i<=$#; i++)); do
  arg="${!i}"
  case "$arg" in
    cm)
      next=$((i+1))
      NAME="${!next}"
      ;;
    -o)
      next=$((i+1))
      JP="${!next}"
      # jsonpath="{.data.X}" → X
      FIELD="${JP#*.data.}"
      FIELD="${FIELD%%\}*}"
      ;;
  esac
done
case "$NAME:$FIELD" in
  "loopzero-cosign-config:mode")            echo "${MOCK_LOOPZERO_COSIGN_CONFIG_MODE:-keyless}" ;;
  "loopzero-cosign-config:cutover_deadline") echo "${MOCK_LOOPZERO_COSIGN_CONFIG_DEADLINE:-2099-12-31T00:00:00Z}" ;;
  "loopzero-cosign-version-config:target_version") echo "${MOCK_V3_TARGET_VERSION:-}" ;;
  "loopzero-cosign-version-config:set_at")          echo "${MOCK_V3_SET_AT:-}" ;;
  *) echo "" ;;
esac
EOF
chmod +x "$MOCK_BIN/kubectl"

run_case() {
  local name="$1"
  local target_version="$2"
  local set_at="$3"
  local expect_exit="$4"
  local expect_stderr_pattern="$5"

  local out err rc
  out=$(mktemp); err=$(mktemp)
  PATH="$MOCK_BIN:$PATH" \
    MOCK_LOOPZERO_COSIGN_CONFIG_MODE="keyless" \
    MOCK_V3_TARGET_VERSION="$target_version" \
    MOCK_V3_SET_AT="$set_at" \
    bash "$TARGET_SCRIPT" >"$out" 2>"$err"
  rc=$?

  local ok=1
  if [[ "$expect_exit" != "$rc" ]]; then ok=0; fi
  if [[ -n "$expect_stderr_pattern" ]] && ! grep -qE "$expect_stderr_pattern" "$err" "$out"; then ok=0; fi

  if [[ $ok -eq 1 ]]; then
    printf '  PASS: %s\n' "$name"
    PASS=$((PASS+1))
  else
    printf '  FAIL: %s (rc=%d, expected=%s)\n' "$name" "$rc" "$expect_exit"
    sed 's/^/    [stdout] /' "$out"
    sed 's/^/    [stderr] /' "$err"
    FAIL=$((FAIL+1))
  fi
  rm -f "$out" "$err"
}

echo "Running cosign-v3-deadline hermetic tests..."
echo

TODAY=$(date -u +%Y-%m-%d)
DAYS_AGO_50=$(date -u -d "$TODAY -50 days" +%Y-%m-%d 2>/dev/null || date -u -j -v-50d +%Y-%m-%d)
DAYS_AGO_70=$(date -u -d "$TODAY -70 days" +%Y-%m-%d 2>/dev/null || date -u -j -v-70d +%Y-%m-%d)
DAYS_AGO_100=$(date -u -d "$TODAY -100 days" +%Y-%m-%d 2>/dev/null || date -u -j -v-100d +%Y-%m-%d)

# 5 paths per plan T0.2 TDD
run_case "test_cosign_v3_deadline_no_target_configmap_passes"       ""    ""             "0" ""
run_case "test_cosign_v3_deadline_within_60d_passes"                "v3"  "$DAYS_AGO_50" "0" ""
run_case "test_cosign_v3_deadline_60_to_90d_warns_but_passes"       "v3"  "$DAYS_AGO_70" "0" "WARN.*cosign v3 upgrade deadline T0\\+60d crossed"
run_case "test_cosign_v3_deadline_past_90d_fails"                   "v3"  "$DAYS_AGO_100" "1" "FAIL.*cosign v3 upgrade deadline T0\\+90d crossed"
run_case "test_cosign_v3_deadline_target_without_set_at_fails"      "v3"  ""             "1" "set_at.*field"

echo
echo "═══════════════════════════════════════════════════════════"
printf "  Results: %d PASS / %d FAIL\n" "$PASS" "$FAIL"
echo "═══════════════════════════════════════════════════════════"

[[ $FAIL -eq 0 ]] || exit 1
