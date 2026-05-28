#!/usr/bin/env bash
# check-m07-cutover-preflight_test.sh — Hermetic test for T0.3 of plan
# 2026-05-28-meet-001-engineering-closure (M07 cutover sequence preflight).
#
# Validates the 5 pre-condition checks of m07-cutover-preflight.sh:
#   1. Migration 000019 + 000020 applied to target DB
#   2. DOCR image loopzero-cosign-signer:v2.4.3-r0 exists
#   3. DOCR image loopzero-api:<tag> exists
#   4. Chart loopzero-build-cwts bumped + published (helm show chart)
#   5. TUF root present in Pulumi stack config (pulumi stack output)
#
# Mocks: psql, doctl, helm, pulumi via PATH override.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TARGET_SCRIPT="$REPO_ROOT/scripts/production/m07-cutover-preflight.sh"

[[ -f "$TARGET_SCRIPT" ]] || { echo "FATAL: target script not found at $TARGET_SCRIPT"; exit 2; }

PASS=0
FAIL=0

MOCK_BIN="$(mktemp -d)"
trap 'rm -rf "$MOCK_BIN"' EXIT

# Mock psql — exit per env MOCK_PSQL_EXIT (default 0) + emit version per MOCK_PSQL_VERSION
cat > "$MOCK_BIN/psql" <<'EOF'
#!/usr/bin/env bash
echo "${MOCK_PSQL_VERSION:- version | dirty | applied_at
       20 | f      | 2026-05-28 17:00:00+00}"
exit "${MOCK_PSQL_EXIT:-0}"
EOF
chmod +x "$MOCK_BIN/psql"

# Mock doctl registry repository list-tags — emit per env vars
cat > "$MOCK_BIN/doctl" <<'EOF'
#!/usr/bin/env bash
case "$*" in
  *"loopzero-cosign-signer"*) echo "${MOCK_DOCR_COSIGN_TAGS:-v2.4.3-r0}" ; exit "${MOCK_DOCR_COSIGN_EXIT:-0}" ;;
  *"loopzero-api"*) echo "${MOCK_DOCR_API_TAGS:-m07-cutover-1.0.0}" ; exit "${MOCK_DOCR_API_EXIT:-0}" ;;
  *) echo "unknown args: $*" >&2 ; exit 1 ;;
esac
EOF
chmod +x "$MOCK_BIN/doctl"

# Mock helm — show chart returns chart yaml on success, empty + non-zero on fail
cat > "$MOCK_BIN/helm" <<'EOF'
#!/usr/bin/env bash
case "$*" in
  *"show chart"*"loopzero-build-cwts"*)
    if [[ "${MOCK_HELM_EXIT:-0}" != "0" ]]; then
      echo "Error: chart not found in repo" >&2
      exit "${MOCK_HELM_EXIT}"
    fi
    echo "${MOCK_HELM_CHART:-version: 0.3.0}"
    exit 0
    ;;
  *) exit 1 ;;
esac
EOF
chmod +x "$MOCK_BIN/helm"

# Mock pulumi — stack output returns base64 string on success, error on fail
cat > "$MOCK_BIN/pulumi" <<'EOF'
#!/usr/bin/env bash
case "$*" in
  *"stack output"*"tufRoot"*)
    if [[ "${MOCK_PULUMI_EXIT:-0}" != "0" ]]; then
      echo "error: stack output tufRoot not found" >&2
      exit "${MOCK_PULUMI_EXIT}"
    fi
    echo "${MOCK_PULUMI_TUF_ROOT:-eyJrZXlzIjp7fX0=}"
    exit 0
    ;;
  *) exit 1 ;;
esac
EOF
chmod +x "$MOCK_BIN/pulumi"

run_case() {
  local name="$1" expect_exit="$2" expect_pattern="$3"
  shift 3
  local out err rc
  out=$(mktemp); err=$(mktemp)
  PATH="$MOCK_BIN:$PATH" "$@" bash "$TARGET_SCRIPT" --target-db=mock --target-env=staging >"$out" 2>"$err"
  rc=$?
  local ok=1
  if [[ "$expect_exit" != "$rc" ]]; then ok=0; fi
  if [[ -n "$expect_pattern" ]] && ! grep -qE "$expect_pattern" "$out" "$err"; then ok=0; fi
  if [[ $ok -eq 1 ]]; then
    printf '  PASS: %s\n' "$name"; PASS=$((PASS+1))
  else
    printf '  FAIL: %s (rc=%d, expected=%s)\n' "$name" "$rc" "$expect_exit"
    sed 's/^/    [stdout] /' "$out"; sed 's/^/    [stderr] /' "$err"
    FAIL=$((FAIL+1))
  fi
  rm -f "$out" "$err"
}

echo "Running m07-cutover-preflight hermetic tests..."
echo

run_case "test_preflight_passes_when_all_preconditions_met"    "0" "ALL PRE-CONDITIONS MET" env
run_case "test_preflight_fails_when_migration_not_applied"     "1" "migration"  env MOCK_PSQL_VERSION="version | dirty | applied_at
       18 | f      | 2026-05-28 17:00:00+00"
run_case "test_preflight_fails_when_docr_cosign_missing"       "1" "cosign-signer.*not.*DOCR" env MOCK_DOCR_COSIGN_EXIT=1
run_case "test_preflight_fails_when_chart_not_bumped"          "1" "chart.*loopzero-build-cwts"   env MOCK_HELM_EXIT=1
run_case "test_preflight_fails_when_tuf_root_missing"          "1" "TUF root"   env MOCK_PULUMI_EXIT=1

echo
echo "═══════════════════════════════════════════════════════════"
printf "  Results: %d PASS / %d FAIL\n" "$PASS" "$FAIL"
echo "═══════════════════════════════════════════════════════════"

[[ $FAIL -eq 0 ]] || exit 1
