#!/usr/bin/env bash
# validate-evidence.sh — Validates an evidence file against the 3-tier policy.
#
# Per docs/GOVERNANCE.md (§1 Evidence Bar) (T0.1 of status-validation-and-doc-consolidation-plan).
#
# Exit codes:
#   0 — valid (EVIDENCED or PARTIAL_CONFIDENCE)
#   1 — invalid (UNEVIDENCED, BROKEN, refs not independent, schema violation)
#   2 — expired (TTL vencido)
#
# Usage:
#   scripts/validate-evidence.sh <evidence-file.md>
#   scripts/validate-evidence.sh --all docs/evidence/
#
# Dependencies: bash 4+, python3 (for YAML parsing; yq optional)

set -euo pipefail

SCRIPT_NAME="$(basename "$0")"

usage() {
    cat <<EOF
Usage: $SCRIPT_NAME <evidence-file.md>
       $SCRIPT_NAME --all <directory>

Validates evidence file(s) against docs/GOVERNANCE.md (§1 Evidence Bar).

Exit codes:
  0 = valid (EVIDENCED | PARTIAL_CONFIDENCE)
  1 = invalid (UNEVIDENCED | BROKEN | INDEPENDENCE_VIOLATION)
  2 = expired
EOF
}

if [[ $# -lt 1 ]]; then
    usage >&2
    exit 1
fi

# Extract YAML frontmatter via Python (portable, no yq dependency required).
# Outputs JSON for downstream processing.
parse_frontmatter() {
    local file="$1"
    python3 <<PYEOF
import sys
import json
import re
from datetime import datetime

with open("$file", "r") as f:
    content = f.read()

match = re.match(r'^---\s*\n(.*?)\n---\s*\n', content, re.DOTALL)
if not match:
    print(json.dumps({"error": "no_frontmatter"}))
    sys.exit(0)

raw = match.group(1)
# Minimal YAML parser (avoid pyyaml dep). Supports flat keys + list of dicts.
result = {}
current_list_key = None
current_list_item = None

for line in raw.split('\n'):
    if not line.strip():
        continue
    if line.startswith('  - '):
        # New list item
        if current_list_item is not None:
            result[current_list_key].append(current_list_item)
        current_list_item = {}
        rest = line[4:].strip()
        if ':' in rest:
            k, v = rest.split(':', 1)
            current_list_item[k.strip()] = v.strip()
    elif line.startswith('    ') and current_list_item is not None:
        # Continuation of current list item
        rest = line.strip()
        if ':' in rest:
            k, v = rest.split(':', 1)
            current_list_item[k.strip()] = v.strip()
    elif ':' in line and not line.startswith(' '):
        # Flush pending list
        if current_list_item is not None:
            result[current_list_key].append(current_list_item)
            current_list_item = None
            current_list_key = None
        k, v = line.split(':', 1)
        v = v.strip()
        if v == '':
            current_list_key = k.strip()
            result[current_list_key] = []
        else:
            result[k.strip()] = v

# Flush trailing list
if current_list_item is not None:
    result[current_list_key].append(current_list_item)

print(json.dumps(result))
PYEOF
}

validate_single() {
    local file="$1"
    local fm_json
    fm_json="$(parse_frontmatter "$file")"

    # Skip files without frontmatter when in --all mode (README, indexes, etc.)
    # but FAIL when validating a specific file directly.
    if echo "$fm_json" | grep -q '"error": "no_frontmatter"'; then
        if [[ "${VALIDATE_MODE:-single}" == "all" ]]; then
            # Skip non-evidence files silently
            return 0
        fi
        echo "❌ FAIL [$file] no YAML frontmatter found"
        return 1
    fi

    # Required fields
    for field in claim_id dimension evidence ttl_days last_validated expires; do
        if ! echo "$fm_json" | python3 -c "import sys, json; d = json.load(sys.stdin); sys.exit(0 if '$field' in d else 1)"; then
            echo "❌ FAIL [$file] missing required field: $field"
            return 1
        fi
    done

    # Future timestamp rejection (EC-5)
    local last_validated
    last_validated="$(echo "$fm_json" | python3 -c "import sys, json; print(json.load(sys.stdin)['last_validated'])")"
    if python3 -c "
from datetime import date
lv = date.fromisoformat('$last_validated')
today = date.today()
import sys
sys.exit(0 if lv <= today else 1)
" 2>/dev/null; then
        :
    else
        echo "❌ FAIL [$file] last_validated in the future: $last_validated"
        return 1
    fi

    # Independence check (EC-6) — all refs must be distinct
    local independence_violation
    independence_violation="$(echo "$fm_json" | python3 -c "
import sys, json
d = json.load(sys.stdin)
ev = d.get('evidence', [])
refs = [e.get('ref', '') for e in ev]
if len(refs) != len(set(refs)):
    print('VIOLATION')
" 2>/dev/null)"
    if [[ "$independence_violation" == "VIOLATION" ]]; then
        echo "❌ FAIL [$file] EVIDENCE_REFS_NOT_INDEPENDENT (duplicate ref in T1/T2/T3 slots)"
        return 1
    fi

    # Count evidence sources
    local count
    count="$(echo "$fm_json" | python3 -c "import sys, json; print(len(json.load(sys.stdin).get('evidence', [])))")"

    # Expiry check
    local expires
    expires="$(echo "$fm_json" | python3 -c "import sys, json; print(json.load(sys.stdin)['expires'])")"
    if ! python3 -c "
from datetime import date
exp = date.fromisoformat('$expires')
today = date.today()
import sys
sys.exit(0 if exp >= today else 1)
" 2>/dev/null; then
        echo "⏰ EXPIRED [$file] expires=$expires (today=$(date +%Y-%m-%d))"
        return 2
    fi

    # Classification
    if [[ "$count" -eq 0 ]]; then
        echo "❌ FAIL [$file] UNEVIDENCED (0 sources)"
        return 1
    elif [[ "$count" -eq 1 ]]; then
        echo "⚠️  WARN [$file] PARTIAL_CONFIDENCE (1 source — not score-eligible)"
        return 0
    elif [[ "$count" -eq 2 ]]; then
        echo "⚠️  WARN [$file] PARTIAL_CONFIDENCE (2 sources — not score-eligible)"
        return 0
    elif [[ "$count" -ge 3 ]]; then
        echo "✅ PASS [$file] EVIDENCED ($count sources, expires $expires)"
        return 0
    fi
}

# Main dispatcher
if [[ "$1" == "--all" ]]; then
    if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
    fi
    DIR="$2"
    if [[ ! -d "$DIR" ]]; then
        echo "Directory not found: $DIR" >&2
        exit 1
    fi
    export VALIDATE_MODE=all
    EXIT_CODE=0
    while IFS= read -r -d '' f; do
        validate_single "$f" || EXIT_CODE=$?
    done < <(find "$DIR" -name "*.md" -type f -print0)
    exit "$EXIT_CODE"
elif [[ "$1" == "-h" || "$1" == "--help" ]]; then
    usage
    exit 0
else
    if [[ ! -f "$1" ]]; then
        echo "File not found: $1" >&2
        exit 1
    fi
    validate_single "$1"
fi
