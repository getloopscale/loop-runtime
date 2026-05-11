# LoopZero PaaS — Maturity Assessment (REDIRECT)

> **This file is now a redirect.** The canonical source of maturity status is **`docs/STATUS.md`**.
>
> Per ADR D2 of `docs/plans/status-validation-and-doc-consolidation-plan.md` v1.2 (T4.2), the maturity assessment is no longer a duplicated narrative surface. The dated history snapshot lives in `docs/maturity-history/`.

## Where to find current status

| Goal | Location |
|------|----------|
| **Current overall + per-dimension scores** | [`docs/STATUS.md`](STATUS.md) §1, §2 |
| **Open gaps** | [`docs/STATUS.md`](STATUS.md) §3 |
| **Last 30 days activity (auto-generated)** | [`docs/STATUS.md`](STATUS.md) §4 |
| **Decay status (auto-generated)** | [`docs/STATUS.md`](STATUS.md) §5 |
| **Evidence files** | [`docs/STATUS.md`](STATUS.md) §6 + `docs/evidence/` |
| **Adversarial validation log** | [`docs/STATUS.md`](STATUS.md) §7 |

## Methodology (briefly)

Maturity scoring uses 3 adversarial personas (Vercel DD / Hostile VC / Paranoid SRE) blind-scoring 7 dimensions, with median + std-dev triangulation. Confidence cap **95-97%** per ADR D1 (correlation residual from same-model-family agents; not 99%).

See:
- `docs/governance/evidence-bar.md` — 3-tier evidence schema (T1 code+test, T2 LIVE log, T3 CI run)
- `docs/governance/personas/README.md` — adversarial persona invocation
- `docs/governance/decay-policy.md` — 30/60/90 day TTL policy
- `docs/governance/bias-taxonomy.md` — 6 bias patterns + counter-defenses

## Dated history

Previous maturity assessment snapshots (read-only after migration):

| Date | Score | File |
|------|-------|------|
| 2026-05-03 | 3.9 / 5.0 | [`docs/maturity-history/2026-05-03-snapshot.md`](maturity-history/2026-05-03-snapshot.md) |

New snapshots are NOT written to `maturity-assessment.md` — they go to `docs/maturity-history/{YYYY-MM-DD}-snapshot.md` and the current state lives in `docs/STATUS.md` which is updated continuously (with TTL-driven decay scans and adversarial spot-checks).

## Why this redirect exists

The 2026-05-11 plan (`docs/plans/status-validation-and-doc-consolidation-plan.md`) identified that scattered status surfaces caused drift inevitable. Material evidence: the same Claude session viesou +0.4 maturity points in <30 minutes (documented in `docs/architecture/governance/deep-dive-current-state.md`).

The fix: single source of truth (`docs/STATUS.md`) with material defenses (TTL, evidence files, validators, adversarial review, pre-merge gate, CI workflows). This file becomes a redirect to preserve incoming links while the canonical surface lives elsewhere.
