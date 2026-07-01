#!/usr/bin/env bash
# Hermetic regression-protector closing Red Team loopzero-cloud#127 (Traefik 502
# sustentado on *.prod-dev.loopzero.example + *.preview-dev.loopzero.example because the
# canonical Caddyfile reverse_proxy upstream was hardcoded to `172.18.0.10:443`
# — a Docker network IP assigned by DHCP that drifted from the workload cluster
# serverlb (k3d-loopzero-workload-serverlb) to the vault cluster serverlb
# (k3d-loopzero-vault-serverlb) after a docker restart. Every customer-app request
# then routed to VAULT → 502 sustentado for the ENTIRE Red Team dev-public
# probe surface, not just one tenant.
#
# Root cause: Docker network subnet 172.18.0.0/16 assigns container IPs via
# DHCP. Container IPs are NOT stable across `docker restart` / `k3d cluster
# create` cycles. k3d's HOST-PORT mapping is stable (declared in cluster
# config: workload serverlb 443 → host 8443). The canonical Caddy upstream
# MUST use the stable host-port form, never a hardcoded docker network IP.
#
# Slug: dashboard-caddyfile-no-docker-network-ip
# Plan: none (single-line canonical fix per parsimony ladder rung 5)
# Issue: loopzero-cloud#127
# Per feedback_issue_fix_requires_failing_e2e_test_2026_06_25 INQUEBRÁVEL:
# every issue closure ships a hermetic guard that fails PRE-fix.
#
# Assertions:
# 1. bash -n syntax clean
# 2. Zero non-comment lines contain a `172\.18\.[0-9]+\.[0-9]+` literal
# 3. The canonical `*.prod-dev.loopzero.example` block uses the stable host-port
#    form `https://127.0.0.1:8443`
# 4. The canonical `*.preview-dev.loopzero.example` block uses the same form
# 5. Comment audit-trail cites Red Team #127 + this test path
#
# NO network, NO docker, NO caddy invocation — pure grep on Caddyfile source.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
CADDYFILE="${REPO_ROOT}/infra/local/Caddyfile"
THIS_TEST="infra/tests/check-caddyfile-no-docker-network-ip_test.sh"

pass=0
fail=0
declare -a failures=()

assert() {
  local label="$1"; shift
  if "$@"; then
    pass=$((pass+1))
    printf '  \033[32mPASS\033[0m %s\n' "$label"
  else
    fail=$((fail+1))
    failures+=("$label")
    printf '  \033[31mFAIL\033[0m %s\n' "$label"
  fi
}

echo "== hermetic guard: Caddyfile MUST NOT hardcode Docker network IPs =="

# A1 file exists
assert "A1 canonical Caddyfile exists" test -f "$CADDYFILE"

# A2 bash -n on this test
assert "A2 bash -n syntax clean" bash -n "${REPO_ROOT}/${THIS_TEST}"

# B1 no non-comment line contains 172.18.x.x
# Strategy: strip full-line comments (starting with #), then grep for the IP.
b1_hits=$(grep -v '^[[:space:]]*#' "$CADDYFILE" | grep -cE '172\.18\.[0-9]+\.[0-9]+' || true)
if [ "$b1_hits" -eq 0 ]; then
  assert "B1 zero non-comment lines contain hardcoded 172.18.x.x" true
else
  printf '     (found %s occurrences — first 3 lines below)\n' "$b1_hits"
  grep -nE '172\.18\.[0-9]+\.[0-9]+' "$CADDYFILE" | grep -vE '^[0-9]+:[[:space:]]*#' | head -3 | sed 's/^/     /'
  assert "B1 zero non-comment lines contain hardcoded 172.18.x.x" false
fi

# C1 prod-dev wildcard block uses stable host-port form
c1=$(awk '/^\*\.prod-dev\.loopzero\.dev[[:space:]]*\{/,/^\}/' "$CADDYFILE" | grep -c 'reverse_proxy[[:space:]]*https://127\.0\.0\.1:8443' || true)
assert "C1 *.prod-dev.loopzero.example block uses https://127.0.0.1:8443" test "$c1" -ge 1

# C2 preview-dev wildcard block uses stable host-port form
c2=$(awk '/^\*\.preview-dev\.loopzero\.dev[[:space:]]*\{/,/^\}/' "$CADDYFILE" | grep -c 'reverse_proxy[[:space:]]*https://127\.0\.0\.1:8443' || true)
assert "C2 *.preview-dev.loopzero.example block uses https://127.0.0.1:8443" test "$c2" -ge 1

# D1 comment audit-trail cites #127 + this test
assert "D1 Caddyfile cites Red Team loopzero-cloud#127" grep -q 'loopzero-cloud#127' "$CADDYFILE"
assert "D2 this test cited in shell-scripts-allowlist.md" grep -q "$THIS_TEST" "${REPO_ROOT}/.claude/rules/shell-scripts-allowlist.md"

echo ""
printf 'Result: %d PASS / %d FAIL\n' "$pass" "$fail"
if [ "$fail" -gt 0 ]; then
  printf '\nFailed assertions:\n'
  for f in "${failures[@]}"; do
    printf '  - %s\n' "$f"
  done
  exit 1
fi
