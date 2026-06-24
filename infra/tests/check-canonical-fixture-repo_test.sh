#!/usr/bin/env bash
# check-canonical-fixture-repo_test.sh
#
# Canonical fixture repo hermetic guard (post-2026-06-23 dev-public Cleanroom
# audit). Fails if any test/seed script defaults `REPO_NAME` (or its e2e Go
# equivalents `testRepoURL` + `--repo "getloopscale/..."` + `repo_full_name`)
# to the **legacy** `loopzero-repotest` instead of the **canonical** `loopzerokit-example`.
#
# Why this guard exists:
#   - 2026-06-16 sponsor mandate: `loopzerokit-example` is the FAANG-canonical
#     LoopZeroKit-native fixture (scaffolded via `npm create loopzerokit@latest`;
#     has `loopzero.config.ts` v2 + canonical `apps:` map). The legacy
#     `loopzero-repotest` was a `loopzero.yaml`-only fixture per Plan v1.1
#     transition; user mandate directed discard+delete in favor of the
#     canonical LoopZeroKit shape (memory [[project_canonical_dev_user_credentials]]
#     companion: post-Cleanroom canonical chain assumes loopzerokit-example).
#   - 2026-06-23 LIVE post-Cleanroom audit: `project_repositories` only
#     contained `getloopscale/loopzerokit-example` (canonical); pushes to the
#     legacy `loopzero-repotest` would silently no-op in `setup_github.go`
#     OnPush handler (`if linked == nil { log.Debug("not linked"); return nil }`).
#     Sister of T1.4 (`check-oidc-cred-admin-loopzero_test.sh`) which guards
#     against the fabricated `paulo@loopzero.test` credential.
#
# Scope (intentionally narrow):
#   - LIVE/production-side scripts only: `infra/scripts/local/`,
#     `infra/scripts/local/chaos-scenarios/`, `infra/tests/e2e/`.
#   - Allowed by design: comments naming the legacy for migration audit
#     (e.g., `# legacy loopzero-repotest superseded by canonical loopzerokit-example`).
#     The grep filter looks for the literal repo name **without** the word
#     "legacy" on the same line — comments that reference the legacy in the
#     context of explaining the deprecation are allowlisted.
#
# Hermetic: NO network, NO kubectl, NO git fetch. Pure regex over working tree.
#
# Exit codes:
#   0  PASS — no LIVE script defaults to the legacy fixture repo
#   1  FAIL — at least one LIVE script defaults to the legacy fixture repo
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

PASS=0
FAIL=0

# Legacy fixture repo that MUST NOT appear as the canonical value in LIVE
# scripts. We grep for `loopzero-repotest` (the literal legacy name) in the LIVE
# script roots, then filter out lines that explicitly mark themselves as
# legacy-audit comments. Allowed shapes that pass the filter:
#   - `# legacy loopzero-repotest superseded by ...` (migration audit)
#   - `# loopzero-repotest deprecated 2026-06-16` (migration audit)
# Disallowed shapes (gate fails):
#   - `REPO_NAME="${REPO_NAME:-loopzero-repotest}"` (default value drift)
#   - `--repo getloopscale/loopzero-repotest` (LIVE invocation drift)
#   - `repo_full_name": "getloopscale/loopzero-repotest"` (canonical fixture drift)
#
# Allowlist: lines that contain `legacy` OR `deprecated` OR `superseded` on
# the same line (deliberately permissive — explicit migration audit comments
# may use the legacy name verbatim).
ALLOWLIST_PATTERN='legacy|deprecated|superseded|allowlist|guard|MUST NOT|never|forbidden'

violations=$(grep -rnE 'loopzero-repotest' \
  infra/scripts/local/ \
  infra/tests/e2e/ \
  2>/dev/null | grep -vE "$ALLOWLIST_PATTERN" || true)

if [[ -z "$violations" ]]; then
  printf '[PASS] no LIVE script defaults to legacy loopzero-repotest (canonical: getloopscale/loopzerokit-example)\n'
  PASS=$((PASS+1))
else
  printf '[FAIL] LIVE script(s) reference legacy loopzero-repotest (expected canonical getloopscale/loopzerokit-example):\n%s\n' "$violations" >&2
  printf '\nFix:\n  - Default REPO_NAME to "loopzerokit-example" (NOT "loopzero-repotest")\n  - Use "getloopscale/loopzerokit-example" in --repo / repo_full_name / testRepoURL\n  - If the reference is an intentional migration-audit comment, add "legacy"/"deprecated"/"superseded" on the same line\n' >&2
  FAIL=$((FAIL+1))
fi

printf 'summary: %d pass / %d fail\n' "$PASS" "$FAIL"
[[ $FAIL -eq 0 ]]
