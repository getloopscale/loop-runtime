#!/usr/bin/env bash
# scan-decay.sh — Scans evidence files for expired claims.
# NOTE: docs/STATUS.md was removed in 2026-05-11 governance consolidation.
# Canonical maturity document is now .claude/knowledge-base/audits/2026-05-11/SUMMARY.md.
#
# Per docs/GOVERNANCE.md (§2 Decay Policy) (T0.3 of status-validation-and-doc-consolidation-plan).
#
# Output: Markdown report grouped by severity (CRITICAL >60d, HIGH >30d, MEDIUM ≤30d overdue).
#
# Exit codes:
#   0 — no expired claims
#   1 — expired claims found
#
# Usage:
#   scripts/scan-decay.sh
#   scripts/scan-decay.sh --json
#   scripts/scan-decay.sh --path docs/evidence/

set -euo pipefail

OUTPUT_FORMAT="markdown"
SCAN_PATHS_INPUT="docs/evidence .claude/knowledge-base/audits/2026-05-11/SUMMARY.md"

usage() {
    cat <<EOF
Usage: $(basename "$0") [--json] [--path <path>] ...
EOF
}

PATHS_OVERRIDE=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --json) OUTPUT_FORMAT="json"; shift ;;
        --path) PATHS_OVERRIDE="${PATHS_OVERRIDE} $2"; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        *) echo "Unknown arg: $1" >&2; usage >&2; exit 1 ;;
    esac
done

if [[ -n "$PATHS_OVERRIDE" ]]; then
    SCAN_PATHS_INPUT="$PATHS_OVERRIDE"
fi

export SCAN_PATHS_INPUT
export OUTPUT_FORMAT

python3 - <<'PYEOF'
import os
import re
import sys
import json
from datetime import date

SCAN_PATHS = [p for p in os.environ.get('SCAN_PATHS_INPUT', '').split() if p]
OUTPUT_FORMAT = os.environ.get('OUTPUT_FORMAT', 'markdown')

def parse_frontmatter(file_path):
    try:
        with open(file_path, 'r') as f:
            content = f.read()
    except (OSError, IOError):
        return None

    match = re.match(r'^---\s*\n(.*?)\n---\s*\n', content, re.DOTALL)
    if not match:
        return None

    raw = match.group(1)
    result = {}
    for line in raw.split('\n'):
        if not line.strip() or line.startswith(' '):
            continue
        if ':' in line:
            k, v = line.split(':', 1)
            v = v.strip()
            if v:
                result[k.strip()] = v
    return result

def scan_file(file_path):
    fm = parse_frontmatter(file_path)
    if not fm or 'expires' not in fm or 'last_validated' not in fm:
        return None
    try:
        expires_date = date.fromisoformat(fm['expires'])
    except (ValueError, KeyError):
        return None

    today = date.today()
    days_overdue = (today - expires_date).days

    if days_overdue < 0:
        return None

    if days_overdue > 60:
        severity = "CRITICAL"
    elif days_overdue > 30:
        severity = "HIGH"
    else:
        severity = "MEDIUM"

    return {
        "file": file_path,
        "claim_id": fm.get('claim_id', '<unknown>'),
        "dimension": fm.get('dimension', '<unknown>'),
        "last_validated": fm.get('last_validated', '<unknown>'),
        "expires": fm['expires'],
        "days_overdue": days_overdue,
        "severity": severity,
    }

expired = []
total_scanned = 0
today = date.today()

for path in SCAN_PATHS:
    if not os.path.exists(path):
        continue
    if os.path.isfile(path):
        total_scanned += 1
        result = scan_file(path)
        if result:
            expired.append(result)
    elif os.path.isdir(path):
        for root, dirs, files in os.walk(path):
            dirs[:] = [d for d in dirs if not d.startswith('_')]
            for f in files:
                if f.endswith('.md'):
                    fp = os.path.join(root, f)
                    total_scanned += 1
                    r = scan_file(fp)
                    if r:
                        expired.append(r)

expired.sort(key=lambda x: x['days_overdue'], reverse=True)

if OUTPUT_FORMAT == "json":
    print(json.dumps({
        "scanned": total_scanned,
        "expired_count": len(expired),
        "expired": expired,
        "scan_date": today.isoformat(),
    }, indent=2))
else:
    print("# Decay Scan Report — " + today.isoformat())
    print()
    print("**Scanned:** " + str(total_scanned) + " files")
    print("**Expired:** " + str(len(expired)) + " claims")
    print()
    if not expired:
        print("OK No expired claims.")
        sys.exit(0)

    for sev in ("CRITICAL", "HIGH", "MEDIUM"):
        group = [e for e in expired if e['severity'] == sev]
        if not group:
            continue
        thresh = ">60d" if sev == "CRITICAL" else (">30d" if sev == "HIGH" else "<=30d")
        print("## " + sev + " (" + thresh + " overdue) - " + str(len(group)))
        print()
        for e in group:
            print("- **" + e['claim_id'] + "** (dim: " + e['dimension'] + ")")
            print("  - File: `" + e['file'] + "`")
            print("  - Expires: " + e['expires'] + " | Days overdue: **" + str(e['days_overdue']) + "**")
        print()

sys.exit(0 if not expired else 1)
PYEOF
