#!/usr/bin/env python3
"""
cross-validate-plan.py — Compare plan's stated artifacts vs filesystem reality.

Per T2.1 of status-validation-and-doc-consolidation-plan v1.2.

Lightweight version of /cross-validation skill: extracts every "Files to edit"
mention from a plan, checks if the file exists in the repo, and reports gaps.

This catches the "Implemented but not wired" pattern at the plan level.

Usage:
  cross-validate-plan.py <plan-file.md>
  cross-validate-plan.py --all docs/plans/

Output: Markdown report (CRITICO/GRAVE classification).

Exit codes:
  0 — no CRITICO/GRAVE findings
  1 — findings present
"""

import os
import re
import sys
import argparse
from collections import defaultdict


# Patterns for extracting file references from plan
RE_FILE_PATH = re.compile(
    r'`?([a-z][\w./-]*/[\w./-]+\.[a-z]{2,6})`?',
    re.IGNORECASE
)
RE_FILES_TO_EDIT = re.compile(r'^####\s+Files to edit', re.MULTILINE)
RE_TASK_HEADER = re.compile(r'^###\s+T(\d+\.\d+\w?)\s+—\s+(.+)$', re.MULTILINE)


def extract_file_refs_per_task(plan_path):
    """Per task, extract files mentioned in 'Files to edit' sections."""
    with open(plan_path, 'r') as f:
        content = f.read()

    # Split into task blocks
    task_blocks = []
    matches = list(RE_TASK_HEADER.finditer(content))
    for i, m in enumerate(matches):
        task_id = m.group(1)
        task_title = m.group(2).strip()
        start = m.end()
        end = matches[i+1].start() if i+1 < len(matches) else len(content)
        task_blocks.append((task_id, task_title, content[start:end]))

    task_refs = {}
    for task_id, task_title, block in task_blocks:
        # Find "Files to edit" section
        m = RE_FILES_TO_EDIT.search(block)
        if not m:
            continue
        # Take next ~30 lines after "Files to edit" header
        after = block[m.end():m.end()+3000]
        # Extract file paths from this region
        # Stop at next #### or ---
        stop = re.search(r'\n####\s|\n---\n', after)
        if stop:
            after = after[:stop.start()]

        refs = []
        for line in after.split('\n'):
            # Skip comments and section markers
            if line.startswith('#') or line.startswith('-->'):
                continue
            for match in RE_FILE_PATH.finditer(line):
                path = match.group(1)
                # Filter false positives
                if path.startswith(('http', 'github.com', 'arxiv.org')):
                    continue
                if path.endswith(('.com', '.org', '.io')):
                    continue
                refs.append(path)

        task_refs[task_id] = {
            'title': task_title,
            'files': refs,
        }

    return task_refs


def check_files(refs, repo_root):
    """For each task, classify file references."""
    results = []
    for task_id, info in refs.items():
        existing = []
        missing = []
        for path in info['files']:
            # Handle (NEW) marker — file is supposed to be created
            full_path = os.path.join(repo_root, path)
            if os.path.exists(full_path):
                existing.append(path)
            else:
                missing.append(path)

        results.append({
            'task_id': task_id,
            'title': info['title'],
            'files_total': len(info['files']),
            'files_existing': existing,
            'files_missing': missing,
        })

    return results


def classify_severity(result):
    """CRITICO / GRAVE / MEDIO / OK."""
    missing = len(result['files_missing'])
    total = result['files_total']

    if total == 0:
        return 'OK'  # No files to validate

    pct_missing = missing / total

    if pct_missing >= 0.8:
        return 'CRITICO'
    elif pct_missing >= 0.4:
        return 'GRAVE'
    elif pct_missing > 0:
        return 'MEDIO'
    else:
        return 'OK'


def emit_report(plan_name, results, output_path):
    """Write Markdown report."""
    lines = []
    lines.append(f"# Cross-Validation Report — {plan_name}")
    lines.append('')
    lines.append(f"**Date:** {os.popen('date +%Y-%m-%d').read().strip()}")
    lines.append(f"**Tasks scanned:** {len(results)}")
    lines.append('')

    severity_counts = defaultdict(int)
    for r in results:
        severity_counts[classify_severity(r)] += 1

    lines.append('## Summary')
    lines.append('')
    lines.append('| Severity | Count |')
    lines.append('|----------|------:|')
    for sev in ('CRITICO', 'GRAVE', 'MEDIO', 'OK'):
        lines.append(f"| {sev} | {severity_counts.get(sev, 0)} |")
    lines.append('')

    for sev in ('CRITICO', 'GRAVE', 'MEDIO', 'OK'):
        bucket = [r for r in results if classify_severity(r) == sev]
        if not bucket:
            continue
        lines.append(f"## {sev} — {len(bucket)} tasks")
        lines.append('')
        for r in bucket:
            lines.append(f"### T{r['task_id']} — {r['title']}")
            lines.append('')
            lines.append(f"- **Files total:** {r['files_total']}")
            lines.append(f"- **Existing:** {len(r['files_existing'])}")
            lines.append(f"- **Missing:** {len(r['files_missing'])}")
            if r['files_missing']:
                lines.append('')
                lines.append('  Missing files:')
                for f in r['files_missing'][:10]:
                    lines.append(f"  - `{f}`")
                if len(r['files_missing']) > 10:
                    lines.append(f"  - _...{len(r['files_missing']) - 10} more_")
            lines.append('')

    with open(output_path, 'w') as f:
        f.write('\n'.join(lines) + '\n')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('paths', nargs='+', help='Plan files or directories')
    parser.add_argument('--output-dir', default='docs/governance/cross-validation-reports/',
                        help='Directory for output reports')
    parser.add_argument('--repo-root', default='.', help='Repo root for file existence checks')
    args = parser.parse_args()

    os.makedirs(args.output_dir, exist_ok=True)

    all_plans = []
    for p in args.paths:
        if os.path.isfile(p) and p.endswith('.md'):
            all_plans.append(p)
        elif os.path.isdir(p):
            for f in os.listdir(p):
                if f.endswith('-plan.md'):
                    all_plans.append(os.path.join(p, f))

    print(f"Scanning {len(all_plans)} plans...", file=sys.stderr)

    overall_severity = defaultdict(int)
    overall_findings = 0

    for plan in all_plans:
        plan_name = os.path.basename(plan).replace('-plan.md', '')
        refs = extract_file_refs_per_task(plan)
        results = check_files(refs, args.repo_root)

        output_file = os.path.join(args.output_dir, f"{plan_name}.md")
        emit_report(plan_name, results, output_file)

        for r in results:
            sev = classify_severity(r)
            overall_severity[sev] += 1
            if sev in ('CRITICO', 'GRAVE'):
                overall_findings += 1

        print(f"  {plan_name}: {len(results)} tasks, "
              f"{sum(1 for r in results if classify_severity(r) == 'CRITICO')} CRITICO, "
              f"{sum(1 for r in results if classify_severity(r) == 'GRAVE')} GRAVE",
              file=sys.stderr)

    # Aggregate summary
    summary_file = os.path.join(args.output_dir, 'SUMMARY.md')
    with open(summary_file, 'w') as f:
        f.write(f"# Cross-Validation Summary — {os.popen('date +%Y-%m-%d').read().strip()}\n\n")
        f.write(f"**Plans scanned:** {len(all_plans)}\n")
        f.write(f"**Total tasks:** {sum(overall_severity.values())}\n\n")
        f.write('## Severity Distribution\n\n')
        f.write('| Severity | Count |\n')
        f.write('|----------|------:|\n')
        for sev in ('CRITICO', 'GRAVE', 'MEDIO', 'OK'):
            f.write(f"| {sev} | {overall_severity.get(sev, 0)} |\n")
        f.write('\n')
        f.write('## Plans Validated\n\n')
        for plan in all_plans:
            name = os.path.basename(plan).replace('-plan.md', '')
            f.write(f"- [{name}]({name}.md)\n")

    print(f"\nSummary written to {summary_file}", file=sys.stderr)
    print(f"Total findings: {overall_findings}", file=sys.stderr)

    sys.exit(0 if overall_findings == 0 else 1)


if __name__ == '__main__':
    main()
