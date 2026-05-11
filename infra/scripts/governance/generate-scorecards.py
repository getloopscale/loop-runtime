#!/usr/bin/env python3
"""
generate-scorecards.py — Generates 7 final scorecards from triangulation output.

Per T3.4 of status-validation-and-doc-consolidation-plan.

Reads:
  - scoring-YYYY-MM-DD/p1-vercel-dd.yaml
  - scoring-YYYY-MM-DD/p2-hostile-vc.yaml
  - scoring-YYYY-MM-DD/p3-paranoid-sre.yaml
  - scoring-YYYY-MM-DD/triangulation.yaml

Writes:
  - scoring-YYYY-MM-DD/scorecard-{dim}.md  (one per 7 dimensions)

Usage:
  generate-scorecards.py --scoring-dir docs/governance/scoring-2026-05-11/
"""

import os
import sys
import argparse
from datetime import date, timedelta

import yaml


DIMS = [
    ("core_deploy_flow", "Core Deploy Flow"),
    ("dx_cli", "DX / CLI"),
    ("infra_ops", "Infrastructure / Operations"),
    ("security", "Security"),
    ("scale_multitenancy", "Scale / Multi-tenancy"),
    ("product_completeness", "Product Completeness"),
    ("tests_confidence", "Tests / Confidence"),
]

DIM_TTL_DAYS = {
    "core_deploy_flow": 30,
    "dx_cli": 30,
    "infra_ops": 60,
    "security": 60,
    "scale_multitenancy": 60,
    "product_completeness": 30,
    "tests_confidence": 30,
}


def emit_scorecard(dim_key, dim_title, scoring_dir, p1, p2, p3, triang):
    today = date.today()
    ttl = DIM_TTL_DAYS.get(dim_key, 30)
    expires = (today + timedelta(days=ttl)).isoformat()

    dim_triang = triang.get('dimensions', {}).get(dim_key, {})

    lines = []
    lines.append(f"# Scorecard — {dim_title}")
    lines.append("")
    lines.append(f"**Generated:** {today.isoformat()}")
    lines.append(f"**TTL:** {ttl} days")
    lines.append(f"**Expires:** {expires}")
    lines.append("")
    lines.append("## Final Score (Triangulated)")
    lines.append("")

    if dim_triang.get('error') == 'incomplete_data':
        lines.append("**ERROR:** Incomplete data — not all 3 personas provided scores for this dim.")
        lines.append("")
    else:
        median = dim_triang.get('median', '?')
        stdev = dim_triang.get('std_dev', '?')
        flagged = dim_triang.get('flagged', '?')
        final = dim_triang.get('final_score', None)

        lines.append(f"| Field | Value |")
        lines.append(f"|-------|------:|")
        lines.append(f"| **Median** | {median} |")
        lines.append(f"| **Std-dev** | {stdev} |")
        lines.append(f"| **Flagged (>0.3)?** | {flagged} |")
        if final is not None:
            lines.append(f"| **Final score** | **{final}** |")
        else:
            lines.append(f"| **Final score** | _Pending T3.3 reconciliation (flagged)_ |")
        lines.append("")

    lines.append("## Persona Scores")
    lines.append("")
    lines.append("| Persona | Model | Score | Justification |")
    lines.append("|---------|-------|------:|---------------|")

    for label, persona in [("P1 Vercel DD", p1), ("P2 Hostile VC", p2), ("P3 Paranoid SRE", p3)]:
        dim_data = persona.get('scores', {}).get(dim_key, {})
        score = dim_data.get('score', '?')
        justifications = dim_data.get('justification', [])
        joined = "<br>".join("• " + str(j).replace('|', '\\|') for j in justifications[:3])
        model = persona.get('model', '?')
        lines.append(f"| {label} | {model} | {score} | {joined} |")

    lines.append("")
    lines.append("## Evidence Cited")
    lines.append("")
    lines.append("Cross-reference with `docs/evidence/" + dim_key.replace('_', '-') + "/`.")
    lines.append("")

    lines.append("## Open Concerns (synthesis)")
    lines.append("")
    seen = set()
    for persona in (p1, p2, p3):
        overall = persona.get('overall_concerns', [])
        for c in overall:
            cs = str(c).strip().lower()
            if cs not in seen:
                seen.add(cs)
                lines.append(f"- {c}")
    lines.append("")

    lines.append("## TTL")
    lines.append("")
    lines.append(f"- **Last validated:** {today.isoformat()} (T3.1 LIVE adversarial run)")
    lines.append(f"- **Expires:** {expires} ({ttl}d TTL per `docs/governance/decay-policy.md`)")
    lines.append(f"- **Revalidation:** rerun T3.1 with new dated `scoring-YYYY-MM-DD/`")
    lines.append("")

    lines.append("## Cross-References")
    lines.append("")
    lines.append(f"- Plan task: T3.4 of `docs/plans/status-validation-and-doc-consolidation-plan.md`")
    lines.append(f"- Triangulation: `{os.path.basename(scoring_dir)}/triangulation.yaml`")
    lines.append(f"- Raw runs: `{os.path.basename(scoring_dir)}/raw-runs.md`")
    lines.append(f"- Linked from: `docs/STATUS.md` §2 row `{dim_key}`")

    out_path = os.path.join(scoring_dir, f"scorecard-{dim_key.replace('_', '-')}.md")
    with open(out_path, 'w') as f:
        f.write('\n'.join(lines) + '\n')

    return out_path


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--scoring-dir', required=True)
    args = parser.parse_args()

    scoring_dir = args.scoring_dir
    if not os.path.isdir(scoring_dir):
        print(f"Not a dir: {scoring_dir}", file=sys.stderr)
        sys.exit(1)

    p1_path = os.path.join(scoring_dir, 'p1-vercel-dd.yaml')
    p2_path = os.path.join(scoring_dir, 'p2-hostile-vc.yaml')
    p3_path = os.path.join(scoring_dir, 'p3-paranoid-sre.yaml')
    triang_path = os.path.join(scoring_dir, 'triangulation.yaml')

    for p in (p1_path, p2_path, p3_path, triang_path):
        if not os.path.isfile(p):
            print(f"Missing: {p}", file=sys.stderr)
            sys.exit(1)

    with open(p1_path) as f:
        p1 = yaml.safe_load(f)
    with open(p2_path) as f:
        p2 = yaml.safe_load(f)
    with open(p3_path) as f:
        p3 = yaml.safe_load(f)
    with open(triang_path) as f:
        triang = yaml.safe_load(f)

    print(f"Generating 7 scorecards in {scoring_dir}", file=sys.stderr)
    for dim_key, dim_title in DIMS:
        out = emit_scorecard(dim_key, dim_title, scoring_dir, p1, p2, p3, triang)
        print(f"  {out}", file=sys.stderr)

    print(f"\nDone. 7 scorecards generated.", file=sys.stderr)


if __name__ == '__main__':
    main()
