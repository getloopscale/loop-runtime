#!/usr/bin/env bash
# Phase 3+6 of plan 2026-06-15-100pct-dashboard-coverage-roadmap — umbrella
# dogfood runner that invokes EVERY per-journey wrapper currently registered.
#
# Maps to sponsor goal "Dogfood --full → all 12 dogfood wrappers --live-only"
# (currently 12 wrappers for delivered journeys; will grow to 23 when the
# 10 backlog journeys + J-25 live-url ship — see roadmap Phase 3).
#
# Modes (mutually exclusive):
#   --hermetic-only  CI-safe mode: invoke each wrapper with --hermetic-only.
#                    No cluster + no LIVE HTTP probes. Fast (~5min total).
#   --live-only      Operator + sponsor JUNTOS mode: invoke each wrapper
#                    with --live-only against dev-public. Requires Cleanroom
#                    TOTAL precedence + sponsor visual verification per
#                    .claude/rules/cleanroom-up-live-test-mandate.md
#                    INQUEBRÁVEL. SLOW (~60-90min total).
#
# Flags:
#   --json           Machine-readable verdict aggregation.
#   --bail-on-fail   Stop at first failing wrapper (default: continue + sum).
#   --help|-h        Print usage.
#
# Exit codes (Rule 4 contract):
#   0  All wrappers exit 0 (full chain PASS).
#   1  At least one wrapper exited non-zero (chain FAIL — see summary).
#   2  Usage error or flag combination invalid.

set -euo pipefail

SCRIPT_NAME="$(basename "$0")"
HERMETIC_ONLY=0
LIVE_ONLY=0
JSON_OUTPUT=0
BAIL_ON_FAIL=0

usage() {
  cat <<EOF
Usage: $SCRIPT_NAME --hermetic-only|--live-only [--json] [--bail-on-fail]

Umbrella dogfood runner — invokes every dev-public journey wrapper
in one chain. Maps to sponsor goal "Dogfood --full".

REQUIRED: pick exactly one of --hermetic-only or --live-only.

  --hermetic-only  Hermetic mode (CI-safe; no cluster needed).
  --live-only      LIVE mode (requires Cleanroom + sponsor JUNTOS per
                   .claude/rules/cleanroom-up-live-test-mandate.md
                   INQUEBRÁVEL).
  --json           Machine-readable JSON verdict.
  --bail-on-fail   Stop at first failing wrapper.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --hermetic-only) HERMETIC_ONLY=1 ;;
    --live-only) LIVE_ONLY=1 ;;
    --json) JSON_OUTPUT=1 ;;
    --bail-on-fail) BAIL_ON_FAIL=1 ;;
    --help|-h) usage; exit 0 ;;
    *) echo "Unknown arg: $1" >&2; usage; exit 2 ;;
  esac
  shift
done

if [[ $HERMETIC_ONLY -eq 0 && $LIVE_ONLY -eq 0 ]]; then
  echo "ERROR: must specify exactly one of --hermetic-only or --live-only" >&2
  usage
  exit 2
fi

if [[ $HERMETIC_ONLY -eq 1 && $LIVE_ONLY -eq 1 ]]; then
  echo "ERROR: --hermetic-only and --live-only are mutually exclusive" >&2
  usage
  exit 2
fi

# Canonical 2026-06-20: 4 `..` climbs (NOT 3). The script lives 4 levels deep
# at loopzero/archive/canonical-tests-v1/scripts/local/. WRAPPERS paths
# `archive/canonical-tests-v1/scripts/local/dogfood-*.sh` are relative to
# `loopzero/` (the repo root). Pre-fix 3-climb landed at `loopzero/archive/`,
# resolving wrappers to `loopzero/archive/archive/...` → 13/13 SKIP "script
# missing on disk" during the canonical seed-layer close session. Issue
# #106. Regression-locked by infra/tests/charts/dogfood_all_repo_root_test.go.
REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
cd "$REPO_ROOT"

# Canonical per-journey wrapper list (sorted by journey ID for deterministic
# ordering). NEW journeys: add the wrapper here when its 3-script triplet
# ships per roadmap Phase 3 + matching `task test:dogfood:{slug}` target.
WRAPPERS=(
  "archive/canonical-tests-v1/scripts/local/dogfood-recovery.sh"          # J-04
  "archive/canonical-tests-v1/scripts/local/dogfood-device.sh"            # J-05
  "archive/canonical-tests-v1/scripts/local/dogfood-project-connect.sh"   # J-09
  "archive/canonical-tests-v1/scripts/local/dogfood-environments.sh"      # J-10
  "archive/canonical-tests-v1/scripts/local/dogfood-domains.sh"           # J-11
  "archive/canonical-tests-v1/scripts/local/dogfood-deployments.sh"       # J-17 + J-19
  "archive/canonical-tests-v1/scripts/local/dogfood-scale.sh"             # J-20
  "archive/canonical-tests-v1/scripts/local/dogfood-readiness.sh"         # J-21
  "archive/canonical-tests-v1/scripts/local/dogfood-secrets.sh"           # J-22
  "archive/canonical-tests-v1/scripts/local/dogfood-secrets-openbao.sh"   # J-22 OpenBao Transit
  "archive/canonical-tests-v1/scripts/local/dogfood-preview.sh"           # J-23
  "archive/canonical-tests-v1/scripts/local/dogfood-live-url.sh"          # J-25
  "archive/canonical-tests-v1/scripts/local/dogfood-login.sh"             # J-01 (M0 Plan A v0.19.0)
  "archive/canonical-tests-v1/scripts/local/dogfood-seed-post-cleanroom-tenant-cr.sh"  # Red Team #118 (2026-06-29)
  "archive/canonical-tests-v1/scripts/local/dogfood-promote-no-op.sh"                  # Red Team #120 P0 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-dashboard-deployments-active-row-attribution.sh"  # Red Team #121 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-dashboard-health-truth.sh"                       # Red Team #124 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-dashboard-projects-badge-data-plane-aware.sh"    # Red Team #128 (2026-07-01) — plan dashboard-badge-data-plane-poller
  "archive/canonical-tests-v1/scripts/local/dogfood-billing-stripe-seed-restarts-cloud.sh"          # Red Team loopzero/loopzero#154 (2026-07-01) — billing 404 upgrade dead
  "archive/canonical-tests-v1/scripts/local/dogfood-billing-stripe-price-ids-seed-canonical.sh"    # Red Team loopzero/loopzero#155 (2026-07-01) — BILLING_NOT_CONFIGURED (Stripe price IDs unset)
  "archive/canonical-tests-v1/scripts/local/dogfood-billing-webhook-fulfillment.sh"                 # Red Team loopzero-cloud#156 (2026-07-01) — payment succeeds but plan stays starter (webhook not delivered)
  "archive/canonical-tests-v1/scripts/local/dogfood-billing-pro-price-canonical.sh"                  # Red Team loopzero-cloud#157 (2026-07-01) — preço divergente (Stripe $19 vs UI $29)
  "archive/canonical-tests-v1/scripts/local/dogfood-promote-ensure-env.sh"                           # Red Team loopzero-cloud#158 (2026-07-01)
  "archive/canonical-tests-v1/scripts/local/dogfood-promote-canonical-url.sh"                           # Red Team loopzero-cloud#159 (2026-07-01) — loopzero promote imprime URL sem prefixo de tenant (404)
  "archive/canonical-tests-v1/scripts/local/dogfood-cli-logs-listbuilds-inference.sh"                    # Red Team loopzero-cloud#160 (2026-07-01) — loopzero logs sem --build "No builds found" apesar de builds existirem
  "archive/canonical-tests-v1/scripts/local/dogfood-build-error-canonical-manifest.sh"               # Red Team #123 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-deployment-detail-fail-loud.sh"                  # Red Team #122 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-dashboard-status-preview-only-honest.sh"        # Red Team #98 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-dashboard-project-repository-route-canonical.sh" # Red Team #102 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-dashboard-project-status-no-retry-4xx.sh"       # Red Team #96 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-dashboard-overview-recent-deploys-status-env-badges.sh" # Red Team #97 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-dashboard-profile-name-layout-bounded.sh"       # Red Team #71 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-dashboard-route-swap-no-stale-content.sh"      # Red Team #73 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-dashboard-register-preserve-name-email-on-error.sh" # Red Team #74 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-dashboard-register-duplicate-account-product-copy.sh" # Red Team #75 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-dashboard-overview-humanized-last-deploy-time.sh" # Red Team #76 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-dashboard-deployments-humanize-author-uuid.sh"  # Red Team #78 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-dashboard-heading-hierarchy-no-h2-skip.sh"     # Red Team #79 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-dashboard-recovery-code-input-otp-a11y.sh"    # Red Team #80 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-dashboard-onboarding-progressbar-a11y.sh"     # Red Team #81 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-dashboard-create-project-sheet-no-roadmap-jargon.sh" # Red Team #82 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-dashboard-create-project-input-error-aria-link.sh"  # Red Team #83 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-loopzero-cloud-memory-exports-race-deterministic.sh"  # Red Team #104 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-dashboard-auth-form-id-name.sh"                  # Red Team #51 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-dashboard-onboarding-derive-deploy.sh"          # Red Team #56 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-dashboard-registration-alias.sh"                # Red Team #55 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-dashboard-defer-jwt-exchange.sh"                # Red Team #50 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-loopzero-cloud-billing-seats-from-tenant-members.sh" # Red Team #42 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-dashboard-upgrade-error-actionable.sh"          # Red Team #40 (2026-06-30)
  "archive/canonical-tests-v1/scripts/local/dogfood-engine-project-cap-rls-independent.sh"         # Red Team #111 (2026-06-30, latent-risk)
  "archive/canonical-tests-v1/scripts/local/dogfood-engine-redeploy-empty-source-message.sh"       # Red Team #126 (2026-06-30)
  # Backlog journeys (added as their wrappers ship per roadmap Phase 3):
  #   J-02 dogfood-register.sh
  #   J-03 dogfood-verification.sh
  #   J-06 dogfood-overview.sh
  #   J-12 dogfood-team-rbac.sh
  #   J-13 dogfood-billing.sh
  #   J-14 dogfood-audit-log.sh
  #   J-15 dogfood-profile.sh
  #   J-16 dogfood-settings.sh
  #   J-24 dogfood-configuration.sh
  #   J-18 dogfood-status-health.sh
)

PASS=0
FAIL=0
SKIP=0
RESULTS=()

MODE_FLAG=""
[[ $HERMETIC_ONLY -eq 1 ]] && MODE_FLAG="--hermetic-only"
[[ $LIVE_ONLY -eq 1 ]] && MODE_FLAG="--live-only"

[[ $JSON_OUTPUT -eq 0 ]] && echo "=== dogfood-all umbrella — mode=$MODE_FLAG — ${#WRAPPERS[@]} wrappers ==="

for wrapper in "${WRAPPERS[@]}"; do
  label="$(basename "$wrapper" .sh)"

  if [[ ! -f "$wrapper" ]]; then
    SKIP=$((SKIP+1))
    RESULTS+=("SKIP: $label (script missing on disk)")
    [[ $JSON_OUTPUT -eq 0 ]] && echo "⊘ $label (script missing)"
    continue
  fi

  if bash "$wrapper" "$MODE_FLAG" >/tmp/dogfood-all-${label}.log 2>&1; then
    PASS=$((PASS+1))
    RESULTS+=("PASS: $label")
    [[ $JSON_OUTPUT -eq 0 ]] && echo "✓ $label"
  else
    FAIL=$((FAIL+1))
    RESULTS+=("FAIL: $label (see /tmp/dogfood-all-${label}.log)")
    [[ $JSON_OUTPUT -eq 0 ]] && echo "✗ $label (see /tmp/dogfood-all-${label}.log)"
    if [[ $BAIL_ON_FAIL -eq 1 ]]; then
      [[ $JSON_OUTPUT -eq 0 ]] && echo "[bail-on-fail] stopping at first failure"
      break
    fi
  fi
done

echo
if [[ $JSON_OUTPUT -eq 1 ]]; then
  jq -n \
    --argjson pass "$PASS" \
    --argjson fail "$FAIL" \
    --argjson skip "$SKIP" \
    --argjson total "${#WRAPPERS[@]}" \
    --arg mode "$MODE_FLAG" \
    --arg verdict "$([[ $FAIL -eq 0 ]] && echo PASS || echo FAIL)" \
    --argjson results "$(printf '%s\n' "${RESULTS[@]}" | jq -R . | jq -s .)" \
    '{mode:$mode,pass:$pass,fail:$fail,skip:$skip,total:$total,verdict:$verdict,results:$results}'
else
  echo "=== summary: $PASS pass / $FAIL fail / $SKIP skip / ${#WRAPPERS[@]} total ==="
  if [[ $FAIL -gt 0 ]]; then
    echo
    echo "Failing wrappers (see /tmp/dogfood-all-*.log for diagnostics):"
    for r in "${RESULTS[@]}"; do
      [[ "$r" == FAIL:* ]] && echo "  $r"
    done
  fi
fi

exit $(( FAIL > 0 ? 1 : 0 ))
