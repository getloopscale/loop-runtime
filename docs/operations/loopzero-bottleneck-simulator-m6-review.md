# Review — LoopZero Bottleneck Simulator M6 (AWS region-posture decision memo)

> Cycle verdict record. Slug: `loopzero-bottleneck-simulator-m6`.
> **VERDICT: READY_TO_MERGE** (after remediation). Independent review returned READY_TO_MERGE
> *conditional* (1 HIGH + 1 MEDIUM + 2 LOW — all derived-prose-stat labels, not capsim outputs);
> all four closed with evidence; the 7 capsim-sourced headline numbers reproduced exactly.

## DoD → evidence (all satisfied)

| DoD | Evidence | Status |
|---|---|---|
| Memo exists, MADR-shaped | `docs/architecture/aws-region-posture-decision.md` — context/drivers/options/outcome/consequences/risks/revisit/reproduction/confirmation | ✅ |
| **No fabricated number** | reviewer independently re-ran the engine and reproduced ALL 7 capsim headline numbers exactly; the 2 wrong stats were derived prose (not capsim) — fixed | ✅ |
| Options compared on objective criteria | one `capsim_compare` table: cost/mo · blast radius · SLA · review score · cost-of-resilience | ✅ |
| Actionable recommendation | 4 ordered phases (levers → sharded → defer aa → request tier) with the $ deltas | ✅ |
| Numeric revisit triggers | 100k → $126,403; ≥99.9% SLA contract → active-active (+$49,564 post-lever) | ✅ |
| Linked from index | `docs/architecture/index.md` (Capacity & cost tooling) | ✅ |
| No regression | engine 45/45 + MCP 8/8 green at decision time | ✅ |

## The decision (LIVE-sourced)

**Levers-first, then sharded multi-region, defer active-active.**
1. Raise `maxPerCell` 500→2000 + prefix delegation → $72,566→$38,892 (−46%), review 32→52, cell_count bottleneck resolved.
2. Sharded multi-region → +$5,778/mo (scale-flat), blast radius 100%→33%, SLA 95%→98.32%.
3. Defer active-active → +$49,564/mo post-levers (2.36× cheaper than pre-levers), gated on a ≥99.9% SLA contract.
4. Add BFF/Engine-API replicas (review's next bottleneck).

## Findings raised by independent review — disposition

| Sev | Finding | Disposition |
|---|---|---|
| HIGH-1 | "≈2% of the post-lever bill" — actual 5,778/38,892 = 14.86% | **FIXED** → "≈15% of the post-lever single-region bill of $38,892" |
| MEDIUM-1 | "$14k CloudFront/NAT traffic" — actual CF/NAT traffic ≈ $6k; $14k is the whole global bucket | **FIXED** → relabeled ("~$14k global bucket, of which CF/NAT ≈ $6k, rest ECR/WAF/storage") + "global cancels in CoR Δ" |
| LOW-1 | "2.4×" is 2.36× | **FIXED** → 2.36× (both occurrences) |
| LOW-2 | SLA column conflates nominal (single/aa) vs blended per-tenant (sharded) | **FIXED** → footnote distinguishing them + noting review score doesn't discriminate sharded/aa |

## Reviewer's reproduction ledger (quoted)

> All 7 capsim-sourced headline numbers reproduce exactly. Levers-first justified (maxPerCell drops
> compute $55,567→$24,142 = the entire −$31,425, dominates by ~14×). Sharded scale-flat CoR +$5,778
> at 50k AND 100k. Estimate-sensitivity SOUND — verified two ways (global cancels in the Δ;
> re-running with muRps 1000 left the ranking byte-identical). MADR complete. 45/45 + 8/8 green.

## Deliverables (M6)

- `docs/architecture/aws-region-posture-decision.md` — the MADR decision memo (LIVE-sourced, reproducible).
- `docs/architecture/index.md` — memo linked.
- `loopzero-bottleneck-simulator-m6-{plan,review}.md`.
