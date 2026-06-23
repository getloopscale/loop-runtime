#!/usr/bin/env bash
# check-archive-script-repo-root_test.sh
#
# T1.2 hermetic guard of plan m1-commit-canonical-30-fixes (v1.2 SHIPPABLE 96.4).
# Fails if any script in archive/canonical-tests-v1/scripts/local/ resolves
# REPO_ROOT/WORKDIR via `$(dirname "$0")/../../..` (3 dirs up — incorrect
# post-quarantine commit 289cf9d6 which added 1 extra directory level).
# Canonical post-quarantine is `../../../..` (4 dirs up).
#
# Hermetic: NO network, NO kubectl. Pure regex over working tree.
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"
PASS=0; FAIL=0

# Regex: $(dirname "$0")/../../..  WITHOUT a trailing /..
# Matches exactly 3 segments NOT followed by another `..`
PATTERN='\$\(dirname[[:space:]]+"\$0"\)/\.\./\.\./\.\.[^/]'

violations=$(grep -rEn "$PATTERN" archive/canonical-tests-v1/scripts/local/ 2>/dev/null || true)

if [[ -z "$violations" ]]; then
  printf '[PASS] archive scripts use canonical 4-dirs-up REPO_ROOT\n'
  PASS=$((PASS+1))
else
  printf '[FAIL] 3-dirs-up REPO_ROOT detected post-quarantine 289cf9d6:\n%s\n' "$violations" >&2
  FAIL=$((FAIL+1))
fi

printf 'summary: %d pass / %d fail\n' "$PASS" "$FAIL"
[[ $FAIL -eq 0 ]]
