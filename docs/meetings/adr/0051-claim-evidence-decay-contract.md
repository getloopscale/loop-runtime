# ADR-0051 — Claim-Evidence-Decay Contract for Maturity Status

**Status:** ACCEPTED
**Date:** 2026-05-11
**Author:** governance team
**Replaces:** part of ad-hoc maturity reporting in `docs/maturity-assessment.md` (now redirect)
**Related plans:** `docs/plans/status-validation-and-doc-consolidation-plan.md` v1.2 (T6.1)
**Related ADRs:** ADR D1-D8 of the plan; consolidates here

---

## Context

The 2026-05-11 working session demonstrated empirically that the existing maturity assessment pipeline is **epistemically vulnerable to bias**:

- The same Claude session (with full context) drifted **+0.4 maturity points in <30 minutes** during normal scoring discussion
- A plan task claimed "19 files reference X" — actual grep returned 6 (3.16× inflation by the planning author)
- 87 of 297 status-related markdown files reference "score / maturity / GREEN / DONE / ✅" with **zero TTL** declared anywhere
- Material inventory: 0/348 claims meet a 3-source EVIDENCED bar (92.5% UNEVIDENCED, 3.2% BROKEN refs to non-existent artifacts)

Six bias patterns were taxonomized (counting / aggregation / recency / self-attestation / lag / "implemented but not wired"). All six were observed in the same baseline.

Existing defenses (review meetings, peer review, CLAUDE.md rules, ADR-0016 §"Resolution" clause) were insufficient — bias retorna ao prose mesmo com awareness explícita.

## Decision

Adopt a formal **Claim-Evidence-Decay Contract** for all maturity-affecting status claims:

### D1 — Evidence Triangulation
- Each score-affecting claim requires **3 evidence sources**: T1 (commit + test), T2 (LIVE log), T3 (CI run).
- All refs MUST be distinct (EC-6: same ref in T1/T2/T3 violates independence).
- Confidence cap **95-97%** (mathematical ceiling considering correlation residual; not 99% — same-model-family adversarial agents have correlated errors).

### D2 — Single Source of Truth
- `docs/STATUS.md` is canonical. All other surfaces redirect: `docs/maturity-assessment.md` is a 2-line pointer; `CLAUDE.md` status section is ≤500 chars referencing STATUS.md.
- History snapshots in `docs/maturity-history/{YYYY-MM-DD}-snapshot.md` (read-only).

### D3 — Decay TTL Tiers
- Operational claims: **30 days** TTL
- Architectural claims: **60 days** TTL
- Strategic claims (ADRs): **90 days** TTL
- Override allowed via `ttl_override: <days>` with mandatory `ttl_override_adr:` link.

### D4 — Adversarial Personas (Independence-Verified)
- 3 personas score blind: Vercel DD Engineer, Hostile VC Auditor, Paranoid SRE.
- **Independence requirement (T3.0):** ≥2 of 3 dimensions varied (model / temperature / seed) OR cross-vendor (codex-rescue for 1 of 3).
- Convergence: median per dim; std-dev > 0.3 triggers reconciliation meeting.

### D5 — Evidence Files In Repo
- `docs/evidence/{dimension}/{claim-slug}.md` per schema in `docs/GOVERNANCE.md (§1 Evidence Bar)`.
- Logs >1MB stored in `${HOME}/.loopzero/artifacts/` with SHA256 checksum in frontmatter.

### D6 — Pre-Merge Gate
- PR touching `docs/STATUS.md` with score-affecting changes MUST also touch ≥1 `docs/evidence/**` file.
- Override label `evidence-waiver` requires PR description justification + approver different from author.

### D7 — Bias Taxonomy as Active Defense
- 6 patterns formalized in `docs/GOVERNANCE.md (§3 Bias Taxonomy)`.
- `bias-check.sh` runs on every plan creation (via `to-plan` skill integration).
- FP rate target <20% (current ~30% calibrated against 4 existing plans; meta-discussion plans are expected FP source).

### D8 — Diagram Tool Dual-Track
- `c4-*.puml` → C4 formal (structure)
- `concept-*.excalidraw` → conceptual arguments (decisions, lifecycles, flows)
- Naming convention enforces disjunction.

## Rationale

**Why 3-source triangulation specifically:**
Math: each source has p≥0.9 of being correct individually. Under full independence, conjunction reliability = 1−(0.1)³ = 99.9%. Real-world cap 95-97% accounts for correlation residual:
- Tests and LIVE runs may share fixtures (~0.2 correlation)
- CI invokes same tests in different env (~0.3 correlation)
- Same-model-family adversarial reviewers share training-data bias (~0.1-0.2 correlation)

Even at 95% real, this is 2-3 orders of magnitude better than single-source claims (which are typically 60-80% reliable empirically).

**Why TTL tiers (30/60/90):**
Empirical observation: test counts change weekly; architecture changes per-sprint; strategic decisions change per-quarter. TTL tiers calibrated to natural drift rates.

**Why adversarial personas (3) with independence verification:**
Self-attestation bias is the root cause (see bias taxonomy). 1 adversarial reviewer can disagree; 3 with diverse viewpoints triangulate the truth. Independence verification prevents the silent "same model in 3 wrappers" failure mode.

**Why pre-merge gate (not just CI):**
CI runs detect drift after merge. Pre-merge gate prevents drift from landing. The fricção is intentional — every score-affecting change requires explicit evidence-attached PR.

## Alternatives Considered

| Alternative | Rejected because |
|-------------|------------------|
| Manual quarterly audits (no automation) | Doesn't scale; audits become folklore; lag bias persists |
| External audit firm (~$50k/year) | Cost prohibitive for pre-release; gap-of-context with internal team |
| Single canonical source without evidence requirement | Drift returns within weeks (per Event 2 in `docs/architecture/governance/README.md (§1 AS-IS)`) |
| 99% confidence claim | Mathematically dishonest without cross-vendor (codex/GPT) mandatory; rejected per EC-1 MUST FIX in edge-case review |

## Consequences

### Positive
- Status claims become **auditable in <2 minutes** by external party (cite evidence file → 3 refs → verify each)
- Bias drift detected automatically (decay scanner + adversarial spotcheck + pre-merge gate)
- Architecture domain `governance/` introduced (new C4 + Excalidraw + deep-dive)
- 4 new scripts in `infra/scripts/governance/` (validator, scanner, bias-check, triangulator)
- 4 new CI workflows (decay-scan, status-refresh, cross-validation-weekly, adversarial-spotcheck)

### Negative
- Pre-release engineering velocity reduced ~5-10% (every score-affecting PR now requires evidence file)
- Operational cost: ~$30-60 setup + $15-25/month for adversarial agent invocations
- Coordination cost: 3 personas × time per recalibration (currently quarterly target)

### Risks Mitigated
- Self-attestation bias (D4 + D6 + D7)
- Documentation drift (D3 + decay scanner + auto-refresh)
- Aggregation bias (D2 — per-dim contract over single number)
- Counting bias (D1 — 3-source bar instead of ✅ checkboxes)

### Risks Remaining
- Same-model-family correlation persists if cross-vendor unavailable in CI (mitigated: 95-97% cap, not 99%)
- bias-check.sh FP rate ~30% (calibration target <20% — improvement deferred)

## Implementation Status (2026-05-11)

Per `docs/plans/status-validation-and-doc-consolidation-plan.md` v1.2:

| Phase | Status |
|-------|--------|
| Phase 0 (Foundation) | ✅ DONE (6/6 tasks) |
| Phase 1 (Inventory) | ✅ DONE (4/4 tasks) — 348 claims cataloged, 0 EVIDENCED |
| Phase 2 (Code-Truth) | Pending — requires `/cross-validation`, `/dead-code`, `/code-audit` skill invocations |
| Phase 3 (Adversarial) | Pending — requires LIVE agent runs with diverse models |
| Phase 4 (Single SoT) | ✅ DONE (7/7 tasks) — STATUS.md + migration + auto-gen + 2 Excalidraw |
| Phase 5 (CI Loop) | ✅ DONE (4/4 tasks) — 4 workflows + notify action |
| Phase 6 (Governance) | In progress — this ADR is T6.1 |

## References

- Plan: `docs/plans/status-validation-and-doc-consolidation-plan.md` v1.2
- Plan ADRs D1-D8 (consolidated above)
- Edge cases: `docs/plans/status-validation-and-doc-consolidation-edge-cases.md`
- AS-IS architecture: `docs/architecture/governance/README.md (§1 AS-IS)`
- Material evidence file (proof-of-concept): `docs/evidence/tests-confidence/2026-05-11-validate-evidence-script.md`
- Bias taxonomy: `docs/GOVERNANCE.md (§3 Bias Taxonomy)`
- Evidence bar: `docs/GOVERNANCE.md (§1 Evidence Bar)`
- Decay policy: `docs/GOVERNANCE.md (§2 Decay Policy)`
- Personas README: `docs/GOVERNANCE.md (§4 Adversarial Personas)`
