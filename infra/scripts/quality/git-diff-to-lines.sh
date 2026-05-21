#!/usr/bin/env bash
# Convert git diff output to "path:line" pairs for coverage-diff gate.
# Plan 2026-05-21-comprehensive-quality-gates v1.1 T2.1 helper.

set -euo pipefail

BASE_REF="${BASE_REF:-origin/main}"
HEAD_REF="${HEAD_REF:-HEAD}"

git diff --unified=0 "${BASE_REF}...${HEAD_REF}" | python3 -c '
import sys, re
cur = None
for line in sys.stdin:
    m = re.match(r"^\+\+\+ b/(.+)", line)
    if m:
        cur = m.group(1)
        continue
    m = re.match(r"^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))?", line)
    if m and cur:
        start = int(m.group(1))
        count = int(m.group(2) or 1)
        for i in range(start, start + count):
            print(f"{cur}:{i}")
'
