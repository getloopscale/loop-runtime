#!/usr/bin/env bash
# Hermetic gate: every job in .github/workflows/*.yml MUST have
# timeout-minutes set, EXCEPT for reusable-workflow caller jobs
# (which receive timeout from the called workflow).
#
# Plan 2026-05-21-deep-review-remediation T4.4.
#
# Background: deep review v2 flagged 8 workflows as NO_TIMEOUT. Exhaustive
# scan found 58 jobs without timeout-minutes across 30+ workflows. T4.4
# bulk-added timeout-minutes: 30 to 57 of them. The 58th
# (infra-deploy.yml:219 bootstrap) is a reusable-workflow caller — timeout
# is set inside the called workflow (infra-bootstrap.yml). GitHub Actions
# doesn't accept timeout-minutes on `uses:` jobs.
#
# Default value chosen: 30 min. Most jobs complete in <10 min; runaway jobs
# get cut. Long-running soaks override explicitly (e.g., chaos-suite,
# drill-l2-scale set higher values).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$REPO_ROOT"

VIOLATIONS=0
fail() { VIOLATIONS=$((VIOLATIONS + 1)); echo "FAIL: $*" >&2; }
info() { echo "[job-timeout] $*"; }

python3 <<'PY'
import re, glob, sys

violations = []
total = 0
covered = 0
reusable_callers = 0

for path in sorted(glob.glob('.github/workflows/*.yml')):
    with open(path) as f:
        lines = f.readlines()

    in_jobs = False
    current_job = None
    current_line = None
    has_timeout = False
    is_reusable_caller = False

    def finalize():
        global total, covered, reusable_callers, violations
        if current_job is None:
            return
        total += 1
        if is_reusable_caller:
            reusable_callers += 1
        elif has_timeout:
            covered += 1
        else:
            violations.append(f"{path}:{current_line} job '{current_job}' missing timeout-minutes")

    for i, line in enumerate(lines, 1):
        if line.rstrip() == 'jobs:':
            in_jobs = True
            continue
        if not in_jobs:
            continue
        if re.match(r'^[a-zA-Z]', line):
            finalize()
            current_job = None
            in_jobs = False
            continue
        m = re.match(r'^  ([a-zA-Z][a-zA-Z0-9_-]*):\s*$', line)
        if m:
            finalize()
            current_job = m.group(1)
            current_line = i
            has_timeout = False
            is_reusable_caller = False
        elif current_job:
            if re.match(r'^    timeout-minutes:', line):
                has_timeout = True
            # Reusable-workflow caller: job has `uses: ./.github/workflows/<wf>.yml`
            if re.match(r'^    uses:\s*\./\.github/workflows/', line):
                is_reusable_caller = True

    finalize()

print(f"Total jobs: {total}")
print(f"Has timeout-minutes: {covered}")
print(f"Reusable-workflow callers (exempt): {reusable_callers}")
print(f"Violations: {len(violations)}")
for v in violations:
    print(f"FAIL: {v}", file=sys.stderr)

sys.exit(1 if violations else 0)
PY
RC=$?

if [ "$RC" -ne 0 ]; then
  echo "" >&2
  echo "[job-timeout] FAIL — jobs without timeout-minutes detected" >&2
  echo "" >&2
  echo "To fix: add 'timeout-minutes: N' under runs-on: in each violating job." >&2
  echo "Default suggested: 30 (extend explicitly for long-running soaks)." >&2
  exit 1
fi

echo "[job-timeout] PASS — all non-reusable-caller jobs have timeout-minutes"
