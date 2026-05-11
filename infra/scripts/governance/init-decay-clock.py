#!/usr/bin/env python3
"""
init-decay-clock.py — Assign last_validated + expires to claims using git history.

Per T1.3 of status-validation-and-doc-consolidation-plan v1.2.

EC-4 (MUST FIX): Uses `git log -1 --follow --format=%aI` to track through file renames.

Heuristic:
  last_validated = git_last_modified_date(claim.source.file)  -- via --follow
  expires = last_validated + ttl_days

Per ADR D3:
  - 30d operational (default)
  - 60d architectural (if dimension in scale-mt/security or content mentions state machine/CRD)
  - 90d strategic (if claim text mentions ADR, sprint plan, strategic)

Output: Catalog updated in-place with last_validated + expires + ttl_days per claim.

Usage:
  init-decay-clock.py --catalog claim-catalog.yaml

Exit codes:
  0 — success
  1 — argument or IO error
"""

import os
import re
import sys
import argparse
import subprocess
from datetime import date, timedelta
from collections import Counter


def git_last_modified(file_path):
    """Get last modified ISO date for file using git log --follow (handles renames per EC-4)."""
    try:
        result = subprocess.run(
            ['git', 'log', '-1', '--follow', '--format=%aI', '--', file_path],
            capture_output=True, text=True, timeout=5
        )
        if result.returncode == 0 and result.stdout.strip():
            # ISO 8601 datetime; extract date part
            return date.fromisoformat(result.stdout.strip().split('T')[0])
    except (subprocess.SubprocessError, FileNotFoundError, ValueError):
        pass
    return None


def determine_ttl_days(claim):
    """Apply TTL tier heuristic."""
    text_lower = claim.get('text', '').lower()
    dim = claim.get('dimension', '')

    # Strategic (90d)
    if any(kw in text_lower for kw in ['adr', 'sprint plan', 'strategic', 'sprint 9', 'ADR-']):
        return 90
    # Architectural (60d)
    if dim in ('scale-mt', 'security') or any(kw in text_lower for kw in ['state machine', 'crd', 'reconcile', 'tenant lifecycle']):
        return 60
    # Operational default (30d)
    return 30


def load_yaml_simple(path):
    """Minimal YAML parse compatible with our catalog format."""
    try:
        import yaml
        with open(path, 'r') as f:
            return yaml.safe_load(f)
    except ImportError:
        # Fallback parser
        result = {'meta': {}, 'claims': []}
        with open(path, 'r') as f:
            content = f.read()
        current = None
        in_claims = False
        for line in content.split('\n'):
            if line.startswith('claims:'):
                in_claims = True
                continue
            if not in_claims:
                if line.startswith('  ') and ':' in line and 'meta:' not in line:
                    k, v = line.split(':', 1)
                    result['meta'][k.strip()] = v.strip()
                continue
            if line.startswith('  - id:'):
                if current:
                    result['claims'].append(current)
                current = {'id': line.split(':', 1)[1].strip()}
            elif current and line.startswith('    '):
                k, v = line.strip().split(':', 1)
                v = v.strip().strip('"')
                current[k] = v
        if current:
            result['claims'].append(current)
        return result


def emit_catalog(catalog, output_path):
    """Re-emit YAML catalog with timestamps populated."""
    lines = []
    lines.append('# Claim Catalog — decay clocks initialized by init-decay-clock.py')
    lines.append(f'# Updated: {date.today().isoformat()}')
    lines.append('meta:')
    for k, v in catalog.get('meta', {}).items():
        lines.append(f'  {k}: {v}')
    lines.append(f'  decay_initialized: {date.today().isoformat()}')
    lines.append('')
    lines.append('claims:')

    field_order = ('source', 'text', 'type', 'dimension', 'polarity', 'extracted_at',
                   'evidence_tier', 'ttl_default_days', 'ttl_days', 'last_validated',
                   'expires', 'evidence_t1', 'evidence_t2', 'evidence_t3', 'broken_refs')

    text_fields = {'source', 'text', 'broken_refs', 'evidence_t1', 'evidence_t2', 'evidence_t3'}
    for c in catalog['claims']:
        lines.append(f"  - id: {c['id']}")
        for k in field_order:
            if k in c:
                v = c[k]
                if k in text_fields or (isinstance(v, str) and any(ch in str(v) for ch in ('"', ':', '\n', '|', '*', '&', '#', '>', '%'))):
                    safe_v = str(v).replace('\\', '\\\\').replace('"', '\\"').replace('\n', ' ')
                    lines.append(f"    {k}: \"{safe_v}\"")
                else:
                    lines.append(f"    {k}: {v}")
    with open(output_path, 'w') as f:
        f.write('\n'.join(lines) + '\n')


def main():
    parser = argparse.ArgumentParser(description='Initialize decay clocks for claims')
    parser.add_argument('--catalog', required=True, help='Catalog YAML to update in-place')
    args = parser.parse_args()

    catalog = load_yaml_simple(args.catalog)
    today = date.today()

    distribution = Counter()
    file_cache = {}  # path → date (avoid repeat git log)
    missing_files = 0

    for claim in catalog['claims']:
        source = claim.get('source', '')
        file_path = source.split(':')[0] if ':' in source else source

        if not file_path:
            continue

        if file_path in file_cache:
            last_mod = file_cache[file_path]
        else:
            last_mod = git_last_modified(file_path)
            file_cache[file_path] = last_mod

        if not last_mod:
            # File not in git history — use today as fallback (will expire soon)
            last_mod = today
            missing_files += 1

        ttl_days = determine_ttl_days(claim)
        expires = last_mod + timedelta(days=ttl_days)

        claim['ttl_days'] = ttl_days
        claim['last_validated'] = last_mod.isoformat()
        claim['expires'] = expires.isoformat()

        # Bucket
        days_left = (expires - today).days
        if days_left < 0:
            distribution['expired'] += 1
        elif days_left < 7:
            distribution['expiring_7d'] += 1
        elif days_left < 30:
            distribution['expiring_30d'] += 1
        else:
            distribution['fresh'] += 1

    emit_catalog(catalog, args.catalog)

    print(f"Initialized decay clocks for {len(catalog['claims'])} claims", file=sys.stderr)
    print(f"Files cache: {len(file_cache)} unique paths", file=sys.stderr)
    print(f"Files missing from git: {missing_files}", file=sys.stderr)
    print(f"Distribution:", file=sys.stderr)
    for bucket, n in distribution.most_common():
        print(f"  {bucket}: {n}", file=sys.stderr)


if __name__ == '__main__':
    main()
