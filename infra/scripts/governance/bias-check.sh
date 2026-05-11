#!/usr/bin/env bash
# bias-check.sh — Scans markdown files for 6 known bias patterns.
#
# Per docs/governance/bias-taxonomy.md (T0.4 of status-validation-and-doc-consolidation-plan).
#
# Detects: counting, aggregation, recency, self-attestation, documentation-lag, implemented-not-wired
#
# Output: Markdown report with bias type + line numbers + severity 0-3.
#
# Exit codes:
#   0 — no FAIL-level findings
#   1 — FAIL findings present (severity >=2)
#
# Usage:
#   scripts/bias-check.sh <file.md>
#   scripts/bias-check.sh --threshold warn|fail file.md
#   scripts/bias-check.sh --all docs/plans/

set -euo pipefail

OUTPUT_FORMAT="markdown"
THRESHOLD="fail"  # 'warn' = exit on warnings too, 'fail' = only severity >=2 blocks

usage() {
    cat <<EOF
Usage: $(basename "$0") [--threshold warn|fail] <file.md>
       $(basename "$0") --all <directory>

Scans markdown for 6 bias patterns from docs/governance/bias-taxonomy.md.

Exit codes:
  0 = no FAIL findings (severity <2)
  1 = FAIL findings present (severity >=2)
EOF
}

FILES=()
while [[ $# -gt 0 ]]; do
    case "$1" in
        --threshold) THRESHOLD="$2"; shift 2 ;;
        --all)
            DIR="$2"
            if [[ ! -d "$DIR" ]]; then
                echo "Directory not found: $DIR" >&2
                exit 1
            fi
            while IFS= read -r -d '' f; do
                FILES+=("$f")
            done < <(find "$DIR" -name "*.md" -type f -print0)
            shift 2
            ;;
        -h|--help) usage; exit 0 ;;
        --json) OUTPUT_FORMAT="json"; shift ;;
        *) FILES+=("$1"); shift ;;
    esac
done

if [[ ${#FILES[@]} -eq 0 ]]; then
    usage >&2
    exit 1
fi

export THRESHOLD
export OUTPUT_FORMAT

python3 - "${FILES[@]}" <<'PYEOF'
import sys
import re
import os
import json
from datetime import date

FILES = sys.argv[1:]
THRESHOLD = os.environ.get('THRESHOLD', 'fail')
OUTPUT_FORMAT = os.environ.get('OUTPUT_FORMAT', 'markdown')

# Bias detectors. Each returns list of findings: (severity, line_no, snippet, hint)
def check_counting_bias(content, lines):
    findings = []
    # Pattern: "N/M ✅" or "N/M DONE" without nearby evidence/link
    pattern = re.compile(r'(\d+)/(\d+)\s*(?:✅|DONE|GREEN|PASS)', re.IGNORECASE)
    for i, line in enumerate(lines, 1):
        for m in pattern.finditer(line):
            n, total = int(m.group(1)), int(m.group(2))
            if total == 0:
                continue
            ratio = n / total
            # Check if line or 2 lines around has link to evidence
            context = ' '.join(lines[max(0, i-2):min(len(lines), i+2)])
            has_evidence_link = re.search(r'docs/evidence/|commit:[0-9a-f]+|ci:|live:', context)
            if ratio >= 0.9 and not has_evidence_link:
                findings.append((1, i, line.strip()[:80], f"Ratio {n}/{total} sem evidence link próximo"))
    return findings

def check_aggregation_bias(content, lines):
    findings = []
    # Pattern: "score X.Y" or "média X.Y" without nearby table/list
    pattern = re.compile(r'(?:score|média|average|maturity)[:\s]+(\d\.\d)', re.IGNORECASE)
    # Heuristic: count dimensions enumerated near each score mention
    dim_words = ('core deploy', 'dx', 'cli', 'infra', 'security', 'scale', 'product', 'tests')
    for i, line in enumerate(lines, 1):
        if pattern.search(line):
            window = ' '.join(lines[max(0, i-5):min(len(lines), i+10)]).lower()
            dims_present = sum(1 for d in dim_words if d in window)
            if dims_present < 3:
                findings.append((1, i, line.strip()[:80], f"Score citado com {dims_present} dims (need >=3 enumeradas)"))
    return findings

def check_recency_bias(content, lines):
    findings = []
    # Heuristic: extract dates; if >50% are in last 14 days vs all dates, flag
    today = date.today()
    date_pattern = re.compile(r'(\d{4}-\d{2}-\d{2})')
    dates = []
    for m in date_pattern.finditer(content):
        try:
            d = date.fromisoformat(m.group(1))
            if d <= today:  # ignore future dates
                dates.append(d)
        except ValueError:
            continue
    if len(dates) >= 5:
        recent = [d for d in dates if (today - d).days <= 14]
        if len(recent) / len(dates) > 0.5:
            findings.append((1, 0, f"{len(recent)}/{len(dates)} dates in last 14 days",
                           f"Recency bias risk: doc dominated by very-recent events"))
    return findings

def check_self_attestation(content, lines, file_path):
    findings = []
    # Heuristic: scoring/maturity assertions in file authored by single contributor (git blame)
    # Cheap proxy: check if file mentions score AND was created in last commit by single author
    score_present = re.search(r'(?:score|maturity)\s*[:=]?\s*\d', content, re.IGNORECASE)
    if score_present:
        try:
            import subprocess
            result = subprocess.run(
                ['git', 'log', '--format=%an', '--', file_path],
                capture_output=True, text=True, timeout=5
            )
            authors = set(l.strip() for l in result.stdout.split('\n') if l.strip())
            if len(authors) <= 1:
                findings.append((2, 0, f"Single author ({len(authors)}) in score-affecting file",
                               "Self-attestation: score author == file sole contributor"))
        except (subprocess.SubprocessError, FileNotFoundError, OSError):
            pass
    return findings

def check_documentation_lag(content, lines, file_path):
    findings = []
    # Look for references to dated documents older than 30 days
    today = date.today()
    date_pattern = re.compile(r'\((\d{4}-\d{2}-\d{2})\)')
    for i, line in enumerate(lines, 1):
        for m in date_pattern.finditer(line):
            try:
                d = date.fromisoformat(m.group(1))
                days_old = (today - d).days
                if days_old > 30 and 'last_validated' not in line and 'expires' not in line:
                    # Skip if line marks deferral or history
                    if re.search(r'(?:diferido|deferred|history|archived|snapshot)', line, re.IGNORECASE):
                        continue
                    findings.append((1, i, line.strip()[:80],
                                   f"Reference {d} ({days_old}d old) sem revalidation marker"))
            except ValueError:
                continue
    return findings

def check_implemented_not_wired(content, lines):
    findings = []
    # Detect CLI/feature claims without cross-validation marker
    suspect_patterns = [
        r'implemented\s+(?:but|though|however)',
        r'shipped\s+(?:but|though|however)',
        r'defined\s+but\s+(?:unwired|not\s+wired)',
        r'CRD\s+only',
        r'placeholder',
    ]
    for i, line in enumerate(lines, 1):
        for pat in suspect_patterns:
            if re.search(pat, line, re.IGNORECASE):
                findings.append((2, i, line.strip()[:80],
                               "Possible 'implemented but not wired' — verify call site"))
                break
    return findings

# Run all checks
total_findings = {}
for f in FILES:
    try:
        with open(f, 'r') as fp:
            content = fp.read()
    except (OSError, IOError):
        continue
    lines = content.split('\n')
    findings = {
        "counting": check_counting_bias(content, lines),
        "aggregation": check_aggregation_bias(content, lines),
        "recency": check_recency_bias(content, lines),
        "self_attestation": check_self_attestation(content, lines, f),
        "documentation_lag": check_documentation_lag(content, lines, f),
        "implemented_not_wired": check_implemented_not_wired(content, lines),
    }
    total_findings[f] = findings

# Report
max_severity = 0

if OUTPUT_FORMAT == "json":
    serializable = {}
    for f, biases in total_findings.items():
        serializable[f] = {}
        for bias, items in biases.items():
            serializable[f][bias] = [
                {"severity": s, "line": l, "snippet": sn, "hint": h}
                for (s, l, sn, h) in items
            ]
            for (s, _, _, _) in items:
                max_severity = max(max_severity, s)
    print(json.dumps(serializable, indent=2))
else:
    print("# Bias Check Report")
    print()
    print("Files scanned: " + str(len(total_findings)))
    print("Threshold: " + THRESHOLD)
    print()
    any_findings = False
    for f, biases in total_findings.items():
        file_has_findings = any(items for items in biases.values())
        if not file_has_findings:
            continue
        any_findings = True
        print("## " + f)
        for bias_name, items in biases.items():
            if not items:
                continue
            print("### " + bias_name)
            for (sev, line_no, snippet, hint) in items:
                max_severity = max(max_severity, sev)
                tag = "INFO" if sev == 0 else ("WARN" if sev == 1 else ("FAIL" if sev == 2 else "CRITICAL"))
                line_info = ("L" + str(line_no)) if line_no else "doc-level"
                print("- [" + tag + "] " + line_info + ": " + snippet)
                print("  > " + hint)
        print()
    if not any_findings:
        print("OK No bias patterns detected.")

# Exit based on threshold
if THRESHOLD == "warn":
    sys.exit(0 if max_severity == 0 else 1)
else:  # 'fail' default
    sys.exit(0 if max_severity < 2 else 1)
PYEOF
