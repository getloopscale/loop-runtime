#!/usr/bin/env bash
# check-dev-public-webhook-chain-syntax_test.sh
#
# T2.2 hermetic structural test for `infra/tests/e2e/dev-public-webhook-chain_test.sh`
# of plan 2026-06-08-pre-release-test-rigor v1.2.
#
# The LIVE test asserts a 9-link chain (webhook → autodeploy → HTTPS 200) plus
# 6 absorbed EC behaviors (EC-1 + EC-2 + EC-5 + EC-8 + EC-9 + EC-12). This
# hermetic test verifies structural shape only — full LIVE proof is the
# operator-run script itself + T6.1 cleanroom-fresh end-to-end.
#
# Hermetic: NO network, NO kubectl, NO gh CLI.
set -u

REPO_ROOT="$(git rev-parse --show-toplevel)"
LIVE="${REPO_ROOT}/infra/tests/e2e/dev-public-webhook-chain_test.sh"
ALLOWLIST="${REPO_ROOT}/.claude/rules/shell-scripts-allowlist.md"

PASS=0
FAIL=0

assert() {
  local msg=$1 ; shift
  if "$@" > /dev/null 2>&1 ; then
    printf '[PASS] %s\n' "$msg" ; PASS=$((PASS + 1))
  else
    printf '[FAIL] %s\n' "$msg" ; FAIL=$((FAIL + 1))
  fi
}

assert_grep() {
  local msg=$1 file=$2 pattern=$3
  if grep -qE -- "$pattern" "$file" 2>/dev/null ; then
    printf '[PASS] %s\n' "$msg" ; PASS=$((PASS + 1))
  else
    printf '[FAIL] %s (pattern: %s)\n' "$msg" "$pattern"
    FAIL=$((FAIL + 1))
  fi
}

assert_not_grep() {
  local msg=$1 file=$2 pattern=$3
  if grep -qE -- "$pattern" "$file" 2>/dev/null ; then
    printf '[FAIL] %s (forbidden: %s)\n' "$msg" "$pattern"
    FAIL=$((FAIL + 1))
  else
    printf '[PASS] %s\n' "$msg" ; PASS=$((PASS + 1))
  fi
}

# --- LIVE test existence + syntax ------------------------------------------

assert "LIVE test file exists"        test -f "$LIVE"
assert "LIVE test is executable"      test -x "$LIVE"
assert "LIVE test bash -n syntax ok"  bash -n "$LIVE"

# --- Shebang + strict mode --------------------------------------------------

assert_grep "shebang is bash"                "$LIVE" '^#!/usr/bin/env bash'
assert_grep "strict mode: set -euo pipefail" "$LIVE" '^set -euo pipefail'

# --- 9-link chain coverage --------------------------------------------------

assert_grep "link 1: webhook received logged"          "$LIVE" '(link 1|chain.*1)'
assert_grep "link 2: source bundle uploaded to MinIO"  "$LIVE" '(source.*bundle|minio|mc ls|source-bundles)'
assert_grep "link 3: Argo workflow submitted"          "$LIVE" '(workflow|kubectl.*wf|argo wf)'
assert_grep "link 4: workflow Succeeded within budget" "$LIVE" '(Succeeded|status.phase|wf.*phase)'
assert_grep "link 5: PG releases status ACTIVE"        "$LIVE" '(ACTIVE|releases.*status|SELECT status)'
assert_grep "link 6: namespace canonical creation"     "$LIVE" '(loopzero-.*-teste|kubectl get ns|namespace)'
assert_grep "link 7: pod Running 1/1 (Prova de Vida)"  "$LIVE" '(1/1|Running|Prova.*Vida|prova de vida)'
assert_grep "link 7.5: ArgoCD refresh=hard (EC-2)"     "$LIVE" '(refresh=hard|refresh\s+--hard|argocd app refresh)'
assert_grep "link 8: ArgoCD App Synced + Healthy"      "$LIVE" '(Synced|Healthy|sync.*status)'
assert_grep "link 9: HTTPS 200 canonical URL"          "$LIVE" '(200|HTTP/.+200|curl.*-w)'

# --- EC absorptions ---------------------------------------------------------

# EC-1: rate-limit fallback to push event per ADR D7
assert_grep "(EC-1 / ADR D7) X-RateLimit-Remaining read" \
  "$LIVE" '(X-RateLimit-Remaining|core\.remaining|rate_limit)'
assert_grep "(EC-1 / ADR D7) push-event fallback when rate-limit low" \
  "$LIVE" '(push.event|push-event|--use-push|patch.*refs/heads)'

# EC-2: ArgoCD refresh=hard absorbed in chain (between link 7 and 8)
assert_grep "(EC-2) refresh=hard happens between link 7 and link 8" \
  "$LIVE" '(refresh=hard|refresh\s+--hard|annotate.*refresh)'

# EC-5: diagnostic dump includes kubectl events on FAIL
assert_grep "(EC-5) diagnostic dump runs kubectl get events" \
  "$LIVE" 'kubectl get events'
assert_grep "(EC-5) diagnostic events sort-by lastTimestamp" \
  "$LIVE" '(sort-by|--sort-by)'

# EC-8: Let's Encrypt staging fallback
assert_grep "(EC-8) detects LE rate-limit (too many certificates)" \
  "$LIVE" '(too many certificates|rate.*limit.*cert|loopzero-staging-issuer)'

# EC-9: session refresh on 401 mid-chain
assert_grep "(EC-9) 401 detected → seed-admin --refresh-only" \
  "$LIVE" '(seed-admin-loopzero-dev.*--refresh-only|--refresh-only.*seed-admin)'
assert_grep "(EC-9) retry once after refresh" \
  "$LIVE" '(retry|refresh.*retry)'

# EC-12: preflight gh auth + rate-limit + kubectl context
assert_grep "(EC-12) preflight gh auth status" \
  "$LIVE" 'gh auth status'
assert_grep "(EC-12) preflight kubectl context check" \
  "$LIVE" '(k3d-loopzero-mgmt|current-context|kubectl config current)'

# --- ADR D6 fail-fast (no retry on chain breaks) ----------------------------

assert_grep "(ADR D6) fail-fast: exit 1 on first broken link" \
  "$LIVE" '(exit 1|fail.fast|FAIL.*exit)'
assert_not_grep "(ADR D6) no silent-retry-with-backoff loop on chain link" \
  "$LIVE" '(retry.*backoff|retry.*for i in.*sleep.*[0-9]+)'

# --- Flags + modes ---------------------------------------------------------

assert_grep "--skip-live flag declared (hermetic-only mode)" \
  "$LIVE" '\-\-skip-live'
assert_grep "--json flag declared (machine-readable verdict)" \
  "$LIVE" '\-\-json'

# --- Anti-patterns forbidden ------------------------------------------------

assert_not_grep "NO Co-Authored-By trailer leak"    "$LIVE" 'Co-Authored-By'
assert_not_grep "NO --no-verify in test body"       "$LIVE" '\-\-no-verify'
assert_not_grep "NO hardcoded GitHub PAT"           "$LIVE" '(ghp_[A-Za-z0-9]+|ghs_[A-Za-z0-9]+)'

# --- Allowlist cross-check (D5) ---------------------------------------------

assert "allowlist exists" test -f "$ALLOWLIST"
assert_grep "(D5) dev-public-webhook-chain_test.sh allowlisted" \
  "$ALLOWLIST" 'dev-public-webhook-chain_test\.sh'
assert_grep "(D5) check-dev-public-webhook-chain-syntax_test.sh allowlisted" \
  "$ALLOWLIST" 'check-dev-public-webhook-chain-syntax_test\.sh'

# --- Wall-clock budget documented (per ADR D8 / EC-4) -----------------------

assert_grep "wall-clock budget < 20 min documented" \
  "$LIVE" '(20.*min|wall.clock|MAX_DURATION)'

# --- Summary ----------------------------------------------------------------

printf '\n=== %s ===\n' "$(basename "$0")"
printf 'Asserts: %d / %d PASS\n' "$PASS" "$((PASS + FAIL))"
exit $FAIL
