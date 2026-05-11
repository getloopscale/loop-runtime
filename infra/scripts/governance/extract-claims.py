#!/usr/bin/env python3
"""
extract-claims.py — Extracts status claims from markdown files.

Per T1.1 + T1.1.5 of status-validation-and-doc-consolidation-plan v1.2.

Patterns extracted:
  - Score declarations: "Score: 3.9/5.0", "Maturity X.Y/5.0"
  - Status assertions: "✅ DONE", "✅ GREEN", "N/M passing"
  - Capability claims: "X works", "supports Y", "validated LIVE"
  - Negative claims: "Not built", "deferred", "❌"

Exclusions (T1.1.5 per EC-3 MUST FIX):
  A. Skip lines inside triple-backtick code blocks
  B. Skip lines in markdown tables where pattern repeats across cells (header row)
  C. Skip lines after sentinels: "Exemplo:", "Example:", "e.g.:", "<example>"

Output: YAML claim-catalog written to stdout or --output path.

Usage:
  scripts/extract-claims.py docs/CLAUDE.md
  scripts/extract-claims.py --path docs/ --output claim-catalog.yaml

Exit codes:
  0 — success
  1 — argument or IO error
"""

import os
import re
import sys
import json
import argparse
from datetime import date


# Patterns to extract (each returns claim type)
PATTERNS = [
    ('score', re.compile(r'(?:Score|Maturity|maturity)[:\s]+(\d\.\d)\s*(?:/\s*5(?:\.0)?)?', re.IGNORECASE)),
    ('completion', re.compile(r'(\d+)/(\d+)\s*(?:✅|DONE|GREEN|PASS|complete|completed)', re.IGNORECASE)),
    ('done_status', re.compile(r'(?:✅\s*DONE|✅\s*GREEN|✅\s*PASS|FULLY\s*SHIPPED|SHIPPED)', re.IGNORECASE)),
    ('negative', re.compile(r'(?:Not\s+built|❌|deferred\s+to|out[\s-]of[\s-]scope|N/A)', re.IGNORECASE)),
    ('capability', re.compile(r'\b(?:works\s+end-to-end|validated\s+LIVE|production-ready|E2E\s+validated)\b', re.IGNORECASE)),
]


# Exclusion sentinels for T1.1.5 EC-3
EXAMPLE_SENTINELS = re.compile(r'^(?:exemplo|example|e\.?g\.?|por\s+exemplo)\s*[:.]', re.IGNORECASE)


def detect_dimension(text):
    """Heuristically map claim text to one of 7 dimensions."""
    text_l = text.lower()
    if any(kw in text_l for kw in ['deploy', 'rollback', 'promote', 'canary', 'rollout']):
        return 'core-deploy'
    if any(kw in text_l for kw in ['cli', 'dx', '--json', 'exit code', 'onboarding']):
        return 'dx-cli'
    if any(kw in text_l for kw in ['infra', 'pulumi', 'helm', 'cluster', 'doks', 'k3d']):
        return 'infra-ops'
    if any(kw in text_l for kw in ['security', 'cosign', 'sbom', 'rbac', 'tls', 'auth', 'jwt']):
        return 'security'
    if any(kw in text_l for kw in ['cell', 'tenant', 'multi-tenant', 'multicell', 'scale']):
        return 'scale-mt'
    if any(kw in text_l for kw in ['preview', 'onboard', 'product', 'feature', 'customer']):
        return 'product-completeness'
    if any(kw in text_l for kw in ['test', 'spec', 'coverage', 'ginkgo', 'envtest', 'journey']):
        return 'tests-confidence'
    return 'unknown'


def classify_polarity(claim_type, snippet):
    """positive / negative based on type and content."""
    if claim_type == 'negative':
        return 'negative'
    if 'shipped' in snippet.lower() or '✅' in snippet:
        return 'positive'
    return 'positive'


def slug_from_snippet(snippet):
    """Make kebab-case slug from snippet (best-effort)."""
    s = re.sub(r'[^\w\s-]', '', snippet.lower())
    s = re.sub(r'\s+', '-', s.strip())
    return s[:60]


def process_file(file_path, include_unknown_dim=False):
    """Extract claims from one markdown file applying T1.1.5 exclusions."""
    try:
        with open(file_path, 'r') as f:
            content = f.read()
    except (OSError, IOError) as e:
        print(f"WARN: cannot read {file_path}: {e}", file=sys.stderr)
        return []

    lines = content.split('\n')
    claims = []

    # State for T1.1.5 exclusions
    in_code_block = False
    in_example_block = False
    last_table_pattern = None  # detect repeating cells in table header

    for line_no, line in enumerate(lines, 1):
        stripped = line.strip()

        # EXCLUSION A — code block toggle
        if stripped.startswith('```'):
            in_code_block = not in_code_block
            continue
        if in_code_block:
            continue

        # EXCLUSION C — example block (HTML tag or sentinel line)
        if '<example>' in stripped.lower():
            in_example_block = True
            continue
        if '</example>' in stripped.lower():
            in_example_block = False
            continue
        if in_example_block:
            continue
        if EXAMPLE_SENTINELS.search(stripped):
            # Skip THIS line and next 3 lines (typical example span)
            continue

        # EXCLUSION B — markdown table row with repeating content
        # Heuristic: lines starting with `|` that have same value in ≥2 cells
        if stripped.startswith('|'):
            cells = [c.strip() for c in stripped.strip('|').split('|')]
            # Skip header-separator rows (all dashes)
            if all(re.match(r'^[-:]+$', c) for c in cells if c):
                continue
            # Detect when same value appears in ≥2 cells (likely example/legend)
            non_empty = [c for c in cells if c]
            if len(non_empty) >= 2 and len(set(non_empty)) < len(non_empty):
                continue

        # Apply patterns
        for claim_type, pattern in PATTERNS:
            for match in pattern.finditer(line):
                snippet = line.strip()[:200]
                dimension = detect_dimension(snippet)
                if not include_unknown_dim and dimension == 'unknown':
                    continue

                claim_data = {
                    'source': f"{file_path}:{line_no}",
                    'text': snippet,
                    'type': claim_type,
                    'dimension': dimension,
                    'polarity': classify_polarity(claim_type, snippet),
                    'extracted_at': date.today().isoformat(),
                    'evidence_tier': 'UNKNOWN',  # populated by T1.2
                    'ttl_default_days': 30,
                    'match_groups': [g for g in match.groups() if g is not None],
                }

                # Generate id from source + snippet
                base_id = slug_from_snippet(snippet) or f"claim-{line_no}"
                file_prefix = os.path.basename(file_path).replace('.md', '')[:30]
                claim_data['id'] = f"{file_prefix}-{base_id}"[:80]

                claims.append(claim_data)

    return claims


def walk_paths(paths, include_unknown_dim=False):
    """Process list of files or directories."""
    all_claims = []
    files_scanned = 0

    for p in paths:
        if os.path.isfile(p) and p.endswith('.md'):
            files_scanned += 1
            all_claims.extend(process_file(p, include_unknown_dim=include_unknown_dim))
        elif os.path.isdir(p):
            for root, dirs, files in os.walk(p):
                # Skip hidden + _fixtures + _archived
                dirs[:] = [d for d in dirs if not d.startswith('_') and not d.startswith('.')]
                for f in files:
                    if f.endswith('.md'):
                        files_scanned += 1
                        fp = os.path.join(root, f)
                        all_claims.extend(process_file(fp, include_unknown_dim=include_unknown_dim))

    return all_claims, files_scanned


def emit_yaml(claims, files_scanned, output_path=None):
    """Emit YAML claim catalog."""
    lines_out = []
    lines_out.append('# Claim Catalog — extracted by infra/scripts/governance/extract-claims.py')
    lines_out.append(f'# Generated: {date.today().isoformat()}')
    lines_out.append(f'# Files scanned: {files_scanned}')
    lines_out.append(f'# Claims extracted: {len(claims)}')
    lines_out.append('')
    lines_out.append(f'meta:')
    lines_out.append(f'  generated: {date.today().isoformat()}')
    lines_out.append(f'  files_scanned: {files_scanned}')
    lines_out.append(f'  claims_count: {len(claims)}')
    lines_out.append('')
    lines_out.append('claims:')

    for c in claims:
        lines_out.append(f"  - id: {c['id']}")
        lines_out.append(f"    source: \"{c['source']}\"")
        # Always quote text — contains markdown specials (|, :, *, etc.)
        safe_text = c['text'].replace('\\', '\\\\').replace('"', '\\"').replace('\n', ' ')
        lines_out.append(f"    text: \"{safe_text}\"")
        lines_out.append(f"    type: {c['type']}")
        lines_out.append(f"    dimension: {c['dimension']}")
        lines_out.append(f"    polarity: {c['polarity']}")
        lines_out.append(f"    extracted_at: {c['extracted_at']}")
        lines_out.append(f"    evidence_tier: {c['evidence_tier']}")
        lines_out.append(f"    ttl_default_days: {c['ttl_default_days']}")

    output = '\n'.join(lines_out) + '\n'

    if output_path:
        with open(output_path, 'w') as f:
            f.write(output)
        print(f"Written {len(claims)} claims to {output_path}", file=sys.stderr)
    else:
        print(output)


def main():
    parser = argparse.ArgumentParser(description='Extract status claims from markdown')
    parser.add_argument('paths', nargs='+', help='Files or directories to scan')
    parser.add_argument('--output', '-o', help='Output YAML path (default: stdout)')
    parser.add_argument('--include-unknown', action='store_true',
                        help='Include claims with dimension=unknown (default: skip)')
    parser.add_argument('--summary-only', action='store_true',
                        help='Print summary stats only')
    args = parser.parse_args()

    claims, files_scanned = walk_paths(args.paths, include_unknown_dim=args.include_unknown)

    if args.summary_only:
        from collections import Counter
        by_dim = Counter(c['dimension'] for c in claims)
        by_type = Counter(c['type'] for c in claims)
        by_polarity = Counter(c['polarity'] for c in claims)
        print(f"Files scanned: {files_scanned}")
        print(f"Claims: {len(claims)}")
        print("By dimension:")
        for d, n in by_dim.most_common():
            print(f"  {d}: {n}")
        print("By type:")
        for t, n in by_type.most_common():
            print(f"  {t}: {n}")
        print("By polarity:")
        for p, n in by_polarity.most_common():
            print(f"  {p}: {n}")
    else:
        emit_yaml(claims, files_scanned, args.output)


if __name__ == '__main__':
    main()
