# Raw Runs Provenance — 2026-05-11

T3.1 LIVE adversarial run + T3.2 triangulation per status-validation-and-doc-consolidation-plan v1.2.

## Persona Invocations

| Persona | Model | Temp | Seed | Avg Score | Output |
|---------|-------|-----:|-----:|----------:|--------|
| P1 Vercel DD | claude-opus-4-7 | 0.7 | 42 | 2.57 | p1-vercel-dd.yaml |
| P2 Hostile VC | claude-sonnet-4-6 | 0.3 | 137 | 2.73 | p2-hostile-vc.yaml |
| P3 Paranoid SRE | claude-haiku-4-5 | 0.5 | 99 | 3.04 | p3-paranoid-sre.yaml |

## Independence Dimensions Varied

- **Models:** 3 distinct (opus / sonnet / haiku) — **fallback Claude-only family** (codex unavailable in Ralph context)
- **Temperatures:** 3 distinct (0.7 / 0.3 / 0.5)
- **Seeds:** 3 distinct (42 / 137 / 99)
- **Persona prompts:** textually distinct (different bias lens per role)

**Verification:** `bash infra/scripts/governance/verify-independence.sh` PASS (3/3 dims varied)

## Confidence Cap

Per ADR D1 — **90-95%** (not 95-97%) because fallback mode (no cross-vendor codex). For 95-97% would need cross-vendor diversity.

## Triangulation Result (T3.2)

**Median average across 7 dims: 2.84/5.0**

**4 of 7 dims FLAGGED** (std-dev > 0.3 — require T3.3 reconciliation meeting):
- DX/CLI: median 3.1 (P1=2.5 to P3=3.8, spread 1.3)
- Security: median 3.0 (P2=2.5 to P3=3.2, spread 0.7)
- Product Completeness: median 2.2 (P1=1.5 to P3=3.1, spread 1.6 — widest)
- Tests/Confidence: median 2.9 (P1=2.0 to P3=3.0, spread 1.0)

**3 of 7 dims CONVERGENT** (std-dev ≤ 0.3 — final):
- Core Deploy Flow: 3.0 (std-dev 0.289)
- Infra/Ops: 2.8 (std-dev 0.115)
- Scale/MT: 2.9 (std-dev 0.208)

## Material Finding — Delta from Documented Baseline

| Source | Avg Score | Delta |
|--------|----------:|------:|
| `docs/maturity-history/2026-05-03-snapshot.md` | 3.9 | — |
| Arithmetic mean of 7 dims pre-Phase-3 (from STATUS.md placeholders) | 3.69 | -0.21 |
| **P1+P2+P3 triangulation median (LIVE Phase 3)** | **2.84** | **-1.06** |

**This is the empirical evidence the plan was designed to produce.** Self-attested 3.9 vs adversarial-triangulated 2.84 = 1.06-point overclaim revealed.

Confidence: 90-95% (fallback mode). Cross-vendor codex run would tighten to 95-97% but not invalidate the directional finding (3 independent personas converge significantly below 3.9).

## Convergent Themes (All 3 Personas Agreed)

1. **0/348 EVIDENCED claims** — pipeline proves claims are unverified
2. **11 BROKEN commit refs** — load-bearing citations point to missing commits
3. **Zero external users** — Product Completeness penalty across all 3 personas
4. **Sprint 8 Phase 1+3+4+5+6 deferred** — chaos coverage absent, capped Scale
5. **Self-attestation bias documented** — 19→6 file count inflation, +0.4 agent drift in 30min

## Next Steps (T3.3)

Per ADR D4: 4 flagged dims require reconciliation meeting. Operator invokes:
1. `/meeting` orchestrator per flagged dim, OR
2. Manual arbiter analysis using `docs/governance/scoring-templates/reconciliation-meeting.template.md`

Until T3.3 completes, flagged dims show `final_score: null` in triangulation.yaml.

## T3.4 Generated Files

7 scorecards (one per dim) under `scoring-2026-05-11/scorecard-{dim}.md`. Convergent dims have final scores; flagged dims show `_pending T3.3 reconciliation_`.
