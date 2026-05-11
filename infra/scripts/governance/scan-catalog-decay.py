#!/usr/bin/env python3
"""
scan-catalog-decay.py — Scan claim-catalog.yaml for stale claims.

Per T1.4 of status-validation-and-doc-consolidation-plan.

Different from scan-decay.sh (which scans evidence files) — this scans the
extracted claim catalog to produce a prioritized stale-claims report.

Output: Markdown report sorted by days_overdue DESC, grouped by severity.

Usage:
  scan-catalog-decay.py --catalog claim-catalog.yaml --output stale-claims-YYYY-MM-DD.md
"""

import sys
import argparse
from datetime import date
from collections import Counter


def load_catalog(path):
    try:
        import yaml
        with open(path, 'r') as f:
            return yaml.safe_load(f)
    except ImportError:
        sys.exit("PyYAML required for catalog scan")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--catalog', required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()

    catalog = load_catalog(args.catalog)
    today = date.today()

    stale = []
    for claim in catalog['claims']:
        if 'expires' not in claim:
            continue
        try:
            expires = date.fromisoformat(str(claim['expires']))
        except (ValueError, TypeError):
            continue
        days_overdue = (today - expires).days
        if days_overdue < 0:
            continue

        if days_overdue > 60:
            severity = 'CRITICAL'
        elif days_overdue > 30:
            severity = 'HIGH'
        else:
            severity = 'MEDIUM'

        stale.append({
            'claim_id': claim.get('id', '<unknown>'),
            'source': claim.get('source', '<unknown>'),
            'text': claim.get('text', '')[:120],
            'dimension': claim.get('dimension', 'unknown'),
            'last_validated': claim.get('last_validated', '<unknown>'),
            'expires': str(claim['expires']),
            'days_overdue': days_overdue,
            'severity': severity,
        })

    stale.sort(key=lambda x: x['days_overdue'], reverse=True)

    # Stats
    severity_counts = Counter(s['severity'] for s in stale)
    dim_counts = Counter(s['dimension'] for s in stale)
    total = len(catalog['claims'])

    # Emit report
    lines = []
    lines.append(f"# Stale Claims Report — {today.isoformat()}")
    lines.append('')
    lines.append(f"**Total claims:** {total}")
    lines.append(f"**Stale claims:** {len(stale)} ({len(stale)/total*100:.1f}% of catalog)")
    lines.append('')
    lines.append('## By Severity')
    lines.append('')
    lines.append('| Severity | Count |')
    lines.append('|----------|------:|')
    for sev in ('CRITICAL', 'HIGH', 'MEDIUM'):
        lines.append(f"| {sev} | {severity_counts.get(sev, 0)} |")
    lines.append('')
    lines.append('## By Dimension')
    lines.append('')
    lines.append('| Dimension | Stale Count |')
    lines.append('|-----------|------------:|')
    for dim, n in dim_counts.most_common():
        lines.append(f"| {dim} | {n} |")
    lines.append('')

    for sev in ('CRITICAL', 'HIGH', 'MEDIUM'):
        bucket = [s for s in stale if s['severity'] == sev]
        if not bucket:
            continue
        thresh = '>60d' if sev == 'CRITICAL' else ('>30d' if sev == 'HIGH' else '<=30d')
        lines.append(f"## {sev} ({thresh} overdue) — {len(bucket)} claims")
        lines.append('')
        for s in bucket[:30]:  # cap at 30 per bucket for readability
            lines.append(f"### {s['claim_id']}")
            lines.append('')
            lines.append(f"- **Source:** `{s['source']}`")
            lines.append(f"- **Dimension:** {s['dimension']}")
            lines.append(f"- **Last validated:** {s['last_validated']}")
            lines.append(f"- **Expired:** {s['expires']} ({s['days_overdue']}d overdue)")
            lines.append(f"- **Text:** {s['text']}")
            lines.append('')
        if len(bucket) > 30:
            lines.append(f"_...{len(bucket) - 30} more {sev} claims (see full catalog)_")
            lines.append('')

    lines.append('## Phase 2 Priority Queue')
    lines.append('')
    lines.append('Cross-validation order:')
    lines.append('1. CRITICAL severity (oldest first) — high regression risk')
    lines.append('2. BROKEN refs from evidence-gaps-report.md')
    lines.append('3. HIGH severity by dimension (security/scale first)')
    lines.append('4. MEDIUM as bandwidth allows')

    with open(args.output, 'w') as f:
        f.write('\n'.join(lines) + '\n')

    print(f"Stale report written to {args.output}", file=sys.stderr)
    print(f"Stale claims: {len(stale)} / {total} ({len(stale)/total*100:.1f}%)", file=sys.stderr)
    print(f"By severity: {dict(severity_counts)}", file=sys.stderr)


if __name__ == '__main__':
    main()
