#!/usr/bin/env bash
# T6.1 of plan 2026-05-29-dashboard-public-integration-canonicalization
# v1.1 EC-08: hermetic verification that the seed script issues DELETEs in
# FK-respecting order (env_targets → releases → builds → projects → audit_log).
# Hermetic — text-grep against the script source.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$REPO_ROOT/scripts/local/seed-demo-data.sh"

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

# v1.1 EC-08: extract the DELETE statement order and assert env_targets
# precedes releases precedes builds precedes projects precedes audit_log.
DELETE_ORDER=$(grep -nE "^DELETE FROM (environment_targets|releases|builds|projects|audit_log)" "$SCRIPT" | head -5)
echo ""
echo "DELETE statement order (line:statement):"
echo "$DELETE_ORDER" | sed 's/^/    /'

check "env_targets DELETEs before releases" bash -c "diff <(echo '$DELETE_ORDER' | grep -n environment_targets | head -1 | cut -d: -f1) <(echo '$DELETE_ORDER' | grep -n releases | head -1 | cut -d: -f1) | awk '/</{a=\$2} />/{b=\$2} END{exit (a<b ? 0:1)}'"
check "releases DELETEs before builds" bash -c "diff <(echo '$DELETE_ORDER' | grep -n releases | head -1 | cut -d: -f1) <(echo '$DELETE_ORDER' | grep -n builds | head -1 | cut -d: -f1) | awk '/</{a=\$2} />/{b=\$2} END{exit (a<b ? 0:1)}'"
check "builds DELETEs before projects" bash -c "diff <(echo '$DELETE_ORDER' | grep -n builds | head -1 | cut -d: -f1) <(echo '$DELETE_ORDER' | grep -n projects | head -1 | cut -d: -f1) | awk '/</{a=\$2} />/{b=\$2} END{exit (a<b ? 0:1)}'"

# v1.1 EC-08: simpler positional grep — DELETE FROM env_targets appears
# textually BEFORE DELETE FROM releases in the script.
ENV_LINE=$(grep -n "DELETE FROM environment_targets" "$SCRIPT" | head -1 | cut -d: -f1)
REL_LINE=$(grep -n "DELETE FROM releases" "$SCRIPT" | head -1 | cut -d: -f1)
BLD_LINE=$(grep -n "DELETE FROM builds" "$SCRIPT" | head -1 | cut -d: -f1)
PRJ_LINE=$(grep -n "DELETE FROM projects" "$SCRIPT" | head -1 | cut -d: -f1)
AUD_LINE=$(grep -n "DELETE FROM audit_log" "$SCRIPT" | head -1 | cut -d: -f1)

check "lineno(env_targets) < lineno(releases)" test "$ENV_LINE" -lt "$REL_LINE"
check "lineno(releases) < lineno(builds)" test "$REL_LINE" -lt "$BLD_LINE"
check "lineno(builds) < lineno(projects)" test "$BLD_LINE" -lt "$PRJ_LINE"
check "lineno(projects) < lineno(audit_log)" test "$PRJ_LINE" -lt "$AUD_LINE"

# Idempotency: script must wrap inserts in BEGIN/COMMIT and DELETE before INSERT
check "wrapped in transaction" grep -q "^BEGIN;" "$SCRIPT"
check "ends with COMMIT" grep -q "^COMMIT;" "$SCRIPT"

# Pro plan upgrade
check "upgrades subscription to pro" grep -q "UPDATE subscriptions SET plan = 'pro'" "$SCRIPT"

# Sanity: required fixtures
check "creates 2 projects" bash -c "[ \$(grep -c 'INSERT INTO projects' '$SCRIPT') -ge 1 ] && [ \$(grep -E 'api-gateway|web-app' '$SCRIPT' | wc -l) -ge 2 ]"
check "creates at least 5 builds" bash -c "[ \$(grep -E \"^  \\('b-(api|web)-[0-9]+\" '$SCRIPT' | wc -l) -ge 5 ]"
check "creates at least 5 releases" bash -c "[ \$(grep -E \"^  \\('rel-(api|web)-[0-9]+\" '$SCRIPT' | wc -l) -ge 5 ]"

echo ""
echo "============================="
echo "PASS=$PASS  FAIL=$FAIL"
echo "============================="
[ "$FAIL" -eq 0 ]
