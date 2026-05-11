#!/usr/bin/env python3
"""
classify-evidence.py — Classify each claim in catalog as T1/T2/T3/UNEVIDENCED/BROKEN.

Per T1.2 of status-validation-and-doc-consolidation-plan v1.2.

Classification heuristics (per docs/GOVERNANCE.md (§1 Evidence Bar)):
  T1 — Code+Test: claim mentions Go/TS test file that exists + not t.Skip()
  T2 — LIVE Run: claim mentions archived log path that exists
  T3 — CI Run: claim mentions workflow file that exists
  UNEVIDENCED: no testable reference
  BROKEN: reference points to non-existent artifact

Output: Updated catalog YAML in-place + gaps report markdown.

Usage:
  classify-evidence.py --catalog claim-catalog.yaml --gaps-report evidence-gaps-report.md

Exit codes:
  0 — success (regardless of gaps count)
  1 — argument or IO error
"""

import os
import re
import sys
import argparse
import subprocess
from datetime import date
from collections import Counter


# Reference patterns
RE_GO_TEST = re.compile(r'\b([\w/]+_test\.go)\b')
RE_TS_TEST = re.compile(r'\b([\w/.-]+\.(?:test|spec)\.(?:ts|tsx|js|jsx))\b')
RE_GINKGO = re.compile(r'(?:ginkgo|describe|it)\(["\']([^"\']+)["\']')
RE_FILE_PATH = re.compile(r'(docs/[\w./-]+\.md|infra/[\w./-]+|api/[\w./-]+|cli/[\w./-]+|operators/[\w./-]+)')
RE_CI_WORKFLOW = re.compile(r'\b([\w-]+\.ya?ml)\b')
RE_COMMIT_SHA = re.compile(r'\b([0-9a-f]{7,40})\b')


def file_exists(path):
    """Check file exists relative to repo root."""
    return os.path.isfile(path) or os.path.isdir(path)


def check_test_not_skipped(test_file):
    """Verify test file exists and is not entirely t.Skip()."""
    if not os.path.isfile(test_file):
        return False
    try:
        with open(test_file, 'r') as f:
            content = f.read()
        # If file is empty or just stubs
        if len(content.strip()) < 50:
            return False
        # If all funcs are t.Skip()
        skip_count = len(re.findall(r't\.Skip\(', content))
        test_count = len(re.findall(r'^func Test', content, re.MULTILINE))
        if test_count > 0 and skip_count >= test_count:
            return False
        return True
    except (OSError, IOError):
        return False


def commit_exists(sha):
    """Check git commit exists."""
    try:
        result = subprocess.run(
            ['git', 'cat-file', '-e', sha],
            capture_output=True, timeout=3
        )
        return result.returncode == 0
    except (subprocess.SubprocessError, FileNotFoundError):
        return False


def classify_claim(claim, text=None):
    """Classify single claim. Returns (tier, status, evidence_refs)."""
    text = text or claim.get('text', '')
    source_file = claim.get('source', '').split(':')[0]

    evidence_refs = {'T1': None, 'T2': None, 'T3': None}
    broken_refs = []

    # T1 — code + test
    for m in RE_GO_TEST.finditer(text):
        test_path = m.group(1)
        if not test_path.startswith(('api/', 'operators/', 'pkg/', 'infra/', 'cli/')):
            continue
        if check_test_not_skipped(test_path):
            evidence_refs['T1'] = f"test:{test_path}"
            break
        else:
            broken_refs.append(f"test_missing_or_skipped:{test_path}")

    # T2 — LIVE log
    for m in RE_FILE_PATH.finditer(text):
        ref_path = m.group(1)
        if not ref_path.endswith('.md'):
            continue
        if 'evidence/' in ref_path or 'reports/' in ref_path:
            if file_exists(ref_path):
                evidence_refs['T2'] = f"live:{ref_path}"
                break
            else:
                broken_refs.append(f"log_missing:{ref_path}")

    # T3 — CI workflow
    for m in RE_CI_WORKFLOW.finditer(text):
        wf_name = m.group(1)
        wf_path = f".github/workflows/{wf_name}"
        if file_exists(wf_path):
            evidence_refs['T3'] = f"ci:{wf_name}"
            break

    # Commit references
    commit_match = RE_COMMIT_SHA.search(text)
    if commit_match and not evidence_refs['T1']:
        sha = commit_match.group(1)
        if commit_exists(sha):
            evidence_refs['T1'] = f"commit:{sha[:10]}"
        else:
            broken_refs.append(f"commit_missing:{sha[:10]}")

    # Count valid tiers
    valid_tiers = sum(1 for v in evidence_refs.values() if v)

    if broken_refs and valid_tiers == 0:
        return 'BROKEN', evidence_refs, broken_refs
    if valid_tiers >= 3:
        return 'EVIDENCED', evidence_refs, broken_refs
    if valid_tiers >= 1:
        return 'PARTIAL_CONFIDENCE', evidence_refs, broken_refs
    return 'UNEVIDENCED', evidence_refs, broken_refs


def load_catalog(path):
    """Minimal YAML loader for catalog (avoid pyyaml dep)."""
    try:
        import yaml
        with open(path, 'r') as f:
            return yaml.safe_load(f)
    except ImportError:
        # Manual parse fallback
        result = {'meta': {}, 'claims': []}
        with open(path, 'r') as f:
            content = f.read()
        # Crude parser — assumes our exact format
        current_claim = None
        in_claims = False
        for line in content.split('\n'):
            if line.startswith('claims:'):
                in_claims = True
                continue
            if not in_claims:
                continue
            if line.startswith('  - id:'):
                if current_claim:
                    result['claims'].append(current_claim)
                current_claim = {'id': line.split(':', 1)[1].strip()}
            elif current_claim and line.startswith('    '):
                k, v = line.strip().split(':', 1)
                v = v.strip().strip('"')
                current_claim[k] = v
        if current_claim:
            result['claims'].append(current_claim)
        return result


def update_catalog(catalog, classifications):
    """Update each claim's evidence_tier based on classification."""
    by_id = {c['id']: c for c in catalog['claims']}
    for claim_id, (tier, refs, broken) in classifications.items():
        if claim_id in by_id:
            by_id[claim_id]['evidence_tier'] = tier
            if refs.get('T1'):
                by_id[claim_id]['evidence_t1'] = refs['T1']
            if refs.get('T2'):
                by_id[claim_id]['evidence_t2'] = refs['T2']
            if refs.get('T3'):
                by_id[claim_id]['evidence_t3'] = refs['T3']
            if broken:
                by_id[claim_id]['broken_refs'] = '; '.join(broken)
    return catalog


def emit_catalog(catalog, output_path):
    """Emit updated YAML catalog."""
    lines = []
    lines.append('# Claim Catalog — classified by infra/scripts/governance/classify-evidence.py')
    lines.append(f'# Updated: {date.today().isoformat()}')
    lines.append(f'meta:')
    lines.append(f'  generated: {catalog["meta"].get("generated")}')
    lines.append(f'  classified: {date.today().isoformat()}')
    lines.append(f'  files_scanned: {catalog["meta"].get("files_scanned")}')
    lines.append(f'  claims_count: {len(catalog["claims"])}')
    lines.append('')
    lines.append('claims:')
    text_fields = {'source', 'text', 'broken_refs', 'evidence_t1', 'evidence_t2', 'evidence_t3'}
    for c in catalog['claims']:
        lines.append(f"  - id: {c['id']}")
        for k in ('source', 'text', 'type', 'dimension', 'polarity', 'extracted_at',
                  'evidence_tier', 'ttl_default_days', 'evidence_t1', 'evidence_t2',
                  'evidence_t3', 'broken_refs'):
            if k in c:
                v = c[k]
                if k in text_fields or (isinstance(v, str) and any(ch in v for ch in ('"', ':', '\n', '|', '*', '&', '#', '>', '%'))):
                    safe_v = str(v).replace('\\', '\\\\').replace('"', '\\"').replace('\n', ' ')
                    lines.append(f"    {k}: \"{safe_v}\"")
                else:
                    lines.append(f"    {k}: {v}")
    with open(output_path, 'w') as f:
        f.write('\n'.join(lines) + '\n')


def emit_gaps_report(catalog, classifications, gaps_path):
    """Emit markdown report of UNEVIDENCED + BROKEN claims by dimension."""
    by_dim = {}
    for c in catalog['claims']:
        tier = c.get('evidence_tier', 'UNKNOWN')
        if tier in ('UNEVIDENCED', 'BROKEN'):
            by_dim.setdefault(c.get('dimension', 'unknown'), []).append(c)

    tier_counts = Counter(c.get('evidence_tier', 'UNKNOWN') for c in catalog['claims'])

    lines = []
    lines.append(f"# Evidence Gaps Report — {date.today().isoformat()}")
    lines.append('')
    lines.append(f"**Total claims:** {len(catalog['claims'])}")
    lines.append('')
    lines.append('## Distribution by Evidence Tier')
    lines.append('')
    lines.append('| Tier | Count | %% |')
    lines.append('|------|------:|---:|')
    total = len(catalog['claims'])
    for tier in ('EVIDENCED', 'PARTIAL_CONFIDENCE', 'UNEVIDENCED', 'BROKEN', 'UNKNOWN'):
        n = tier_counts.get(tier, 0)
        pct = (n / total * 100) if total else 0
        lines.append(f"| {tier} | {n} | {pct:.1f} |")
    lines.append('')
    lines.append('## Gaps by Dimension')
    lines.append('')
    for dim in sorted(by_dim.keys()):
        gaps = by_dim[dim]
        broken = [c for c in gaps if c.get('evidence_tier') == 'BROKEN']
        unevid = [c for c in gaps if c.get('evidence_tier') == 'UNEVIDENCED']
        lines.append(f"### {dim} — {len(gaps)} gaps")
        lines.append('')
        if broken:
            lines.append(f"**BROKEN ({len(broken)}):**")
            for c in broken[:10]:
                lines.append(f"- `{c.get('source', '')}` — {c.get('text', '')[:120]}")
                if 'broken_refs' in c:
                    lines.append(f"  - Broken: {c['broken_refs']}")
            if len(broken) > 10:
                lines.append(f"- _...{len(broken) - 10} more_")
            lines.append('')
        if unevid:
            lines.append(f"**UNEVIDENCED ({len(unevid)}):**")
            for c in unevid[:10]:
                lines.append(f"- `{c.get('source', '')}` — {c.get('text', '')[:120]}")
            if len(unevid) > 10:
                lines.append(f"- _...{len(unevid) - 10} more_")
            lines.append('')

    lines.append('## Phase 2 Priority Queue')
    lines.append('')
    lines.append('Claims marked BROKEN should be addressed FIRST (refs point to nothing).')
    lines.append('Then UNEVIDENCED needing T1/T2/T3 — split by dimension.')

    with open(gaps_path, 'w') as f:
        f.write('\n'.join(lines) + '\n')


def main():
    parser = argparse.ArgumentParser(description='Classify claims by evidence tier')
    parser.add_argument('--catalog', required=True, help='Input claim-catalog.yaml')
    parser.add_argument('--gaps-report', required=True, help='Output gaps report path')
    parser.add_argument('--update-catalog', action='store_true',
                        help='Write classifications back to catalog in-place')
    args = parser.parse_args()

    catalog = load_catalog(args.catalog)
    classifications = {}

    for claim in catalog['claims']:
        tier, refs, broken = classify_claim(claim)
        classifications[claim['id']] = (tier, refs, broken)
        claim['evidence_tier'] = tier
        if refs.get('T1'):
            claim['evidence_t1'] = refs['T1']
        if refs.get('T2'):
            claim['evidence_t2'] = refs['T2']
        if refs.get('T3'):
            claim['evidence_t3'] = refs['T3']
        if broken:
            claim['broken_refs'] = '; '.join(broken)

    # Always emit gaps report
    emit_gaps_report(catalog, classifications, args.gaps_report)
    print(f"Gaps report written to {args.gaps_report}", file=sys.stderr)

    # Optionally update catalog
    if args.update_catalog:
        emit_catalog(catalog, args.catalog)
        print(f"Catalog updated in-place: {args.catalog}", file=sys.stderr)

    # Summary
    tier_counts = Counter(c.get('evidence_tier', 'UNKNOWN') for c in catalog['claims'])
    print('Classification summary:', file=sys.stderr)
    for tier, n in tier_counts.most_common():
        print(f'  {tier}: {n}', file=sys.stderr)


if __name__ == '__main__':
    main()
