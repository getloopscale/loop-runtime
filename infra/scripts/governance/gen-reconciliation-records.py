#!/usr/bin/env python3
"""
gen-reconciliation-records.py — Auto-generate T3.3 meeting record skeletons for flagged dims.

Per T3.3 of status-validation-and-doc-consolidation-plan.

Reads triangulation.yaml + persona YAMLs, generates one meeting record per flagged dim
(std-dev > 0.3) ready for operator/arbiter discussion.

Usage:
  gen-reconciliation-records.py --scoring-dir docs/governance/scoring-YYYY-MM-DD/
"""

import os
import sys
import argparse
from datetime import date

import yaml


def gen_record(scoring_dir, dim_key, dim_data, personas):
    """Generate one reconciliation record."""
    today = date.today().isoformat()
    safe_dim = dim_key.replace('_', '-')

    p1, p2, p3 = personas

    p1_dim = p1.get('scores', {}).get(dim_key, {})
    p2_dim = p2.get('scores', {}).get(dim_key, {})
    p3_dim = p3.get('scores', {}).get(dim_key, {})

    lines = []
    lines.append(f"# Reconciliation Meeting — {dim_key.replace('_', ' ').title()}")
    lines.append("")
    lines.append(f"**Date:** {today}")
    lines.append(f"**Trigger:** std-dev = {dim_data['std_dev']} > 0.3 in T3.2 triangulation")
    lines.append(f"**Plan task:** T3.3 of `docs/plans/status-validation-and-doc-consolidation-plan.md`")
    lines.append(f"**Status:** AWAITING OPERATOR — auto-generated from T3.1 LIVE run")
    lines.append("")
    lines.append("## Pre-Meeting Inputs")
    lines.append("")
    lines.append("| Persona | Model | Score |")
    lines.append("|---------|-------|------:|")
    lines.append(f"| P1 Vercel DD | {p1.get('model', '?')} | {p1_dim.get('score', '?')} |")
    lines.append(f"| P2 Hostile VC | {p2.get('model', '?')} | {p2_dim.get('score', '?')} |")
    lines.append(f"| P3 Paranoid SRE | {p3.get('model', '?')} | {p3_dim.get('score', '?')} |")
    lines.append(f"| **Median** | — | **{dim_data['median']}** |")
    lines.append(f"| **Std-dev** | — | **{dim_data['std_dev']}** |")
    lines.append("")
    lines.append(f"**Spread:** {abs(max(p1_dim.get('score', 0), p2_dim.get('score', 0), p3_dim.get('score', 0)) - min(p1_dim.get('score', 0), p2_dim.get('score', 0), p3_dim.get('score', 0))):.1f}")
    lines.append("")

    lines.append("## Persona Justifications (raw bullets)")
    lines.append("")
    for label, persona_dim in [("P1 Vercel DD", p1_dim), ("P2 Hostile VC", p2_dim), ("P3 Paranoid SRE", p3_dim)]:
        lines.append(f"### {label}")
        lines.append("")
        for b in persona_dim.get('justification', [])[:3]:
            lines.append(f"- {b}")
        lines.append("")

    lines.append("## Arbiter Discussion (TO BE COMPLETED BY OPERATOR)")
    lines.append("")
    lines.append("**Why the delta?**")
    lines.append("")
    lines.append("_[Manual analysis: which persona is over/under-scoring? Different criteria? Different evidence weighting?]_")
    lines.append("")
    lines.append("### Arbiter Questions")
    lines.append("")
    lines.append("1. Are P1/P2/P3 looking at the same evidence?")
    lines.append("2. Is the delta semantic (different lens) or factual (one persona has wrong data)?")
    lines.append("3. What concrete artifact would shift the lowest-scoring persona up, or vice versa?")
    lines.append("")
    lines.append("### Convergence Path")
    lines.append("")
    lines.append("_[Document the final agreed score or note dissent]_")
    lines.append("")
    lines.append("## Final Decision (TO BE COMPLETED)")
    lines.append("")
    lines.append("| Field | Value |")
    lines.append("|-------|------:|")
    lines.append("| **Reconciled score** | _N.N_ |")
    lines.append("| **Decision method** | _consensus / arbiter-decided / dissent-recorded_ |")
    lines.append("| **Dissenting opinions** | _if any, document them_ |")
    lines.append("")
    lines.append("## Action Items")
    lines.append("")
    lines.append(f"- [ ] Update `docs/audits/2026-05-11/SUMMARY.md` with reconciled score")
    lines.append(f"- [ ] Update `docs/governance/scoring-2026-05-11/scorecard-{safe_dim}.md` with final")
    lines.append("- [ ] Document open work items if dim has actionable gaps")
    lines.append("")
    lines.append("## Cross-References")
    lines.append("")
    lines.append("- T3.2 output: `triangulation.yaml`")
    lines.append(f"- T3.4 scorecard: `scorecard-{safe_dim}.md`")
    lines.append("- Per-persona files: `p1-vercel-dd.yaml`, `p2-hostile-vc.yaml`, `p3-paranoid-sre.yaml`")
    lines.append("- Plan ADR D4 (adversarial personas) + ADR-0051 (canonical contract)")

    out_path = os.path.join(scoring_dir, f"reconciliation-{safe_dim}.md")
    with open(out_path, 'w') as f:
        f.write('\n'.join(lines) + '\n')

    return out_path


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--scoring-dir', required=True)
    args = parser.parse_args()

    triang_path = os.path.join(args.scoring_dir, 'triangulation.yaml')
    with open(triang_path) as f:
        triang = yaml.safe_load(f)

    personas = []
    for f_name in ('p1-vercel-dd.yaml', 'p2-hostile-vc.yaml', 'p3-paranoid-sre.yaml'):
        with open(os.path.join(args.scoring_dir, f_name)) as fp:
            personas.append(yaml.safe_load(fp))

    flagged = [
        (dim_key, dim_data) for dim_key, dim_data in triang.get('dimensions', {}).items()
        if dim_data.get('flagged')
    ]

    print(f"Generating {len(flagged)} reconciliation record(s) in {args.scoring_dir}", file=sys.stderr)
    for dim_key, dim_data in flagged:
        out = gen_record(args.scoring_dir, dim_key, dim_data, personas)
        print(f"  {out}", file=sys.stderr)

    print(f"\nDone. {len(flagged)} reconciliation records — awaiting operator/arbiter input.", file=sys.stderr)


if __name__ == '__main__':
    main()
