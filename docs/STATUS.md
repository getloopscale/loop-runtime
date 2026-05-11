# LoopZero Platform — Status

**Canonical source of truth.** All maturity claims live here. Every claim has a TTL. Every score-affecting claim links to evidence in `docs/evidence/`. Per `docs/plans/status-validation-and-doc-consolidation-plan.md` v1.2.

> **Note for readers:** if you're looking for `docs/maturity-assessment.md` — that file is now a redirect; the dated snapshot is in `docs/maturity-history/`. CLAUDE.md status block also redirects here.
>
> **Visual reference:** `docs/architecture/governance/concept-evidence-triangulation-pipeline.excalidraw`
>
> **How scores are computed:** see `docs/GOVERNANCE.md (§4 Adversarial Personas)` (3 adversarial personas + triangulation + std-dev gate) and `docs/GOVERNANCE.md (§1 Evidence Bar)` (3-tier evidence requirement). Confidence cap: **95-97%** per ADR D1 (correlation residual; not 99%).

---

## 1. Overall Maturity

| Field | Value |
|-------|-------|
| **Score (median of 7 dims, post-T3.1 LIVE)** | **2.84 / 5.0** (3 convergent + 4 flagged pending T3.3 reconciliation) |
| **Confidence cap** | 90-95% (ADR D1 fallback mode — Claude-only family; 95-97% needs cross-vendor codex) |
| **Last validated** | 2026-05-11 (Phase 3 LIVE adversarial run) |
| **Expires** | 2026-06-10 (30d TTL operational) |
| **Methodology** | 3 adversarial personas (P1 Vercel DD opus / P2 Hostile VC sonnet / P3 Paranoid SRE haiku) blind-score 7 dimensions; median + std-dev triangulation per `docs/governance/scoring-2026-05-11/` |
| **Open gates for 4.0+** | (a) first CI Sunday cron GREEN producing real `cell2-soaks.yml` evidence; (b) 1 prod customer day-trial with no incidents; (c) T3.3 reconciliation of 4 flagged dims; (d) Dogfood Gate (cleanroom) |

**Reading this number:** the overall score is *informative* (median of medians), not canonical. Per-dimension scores below are the actual contract. See §2.

**Material delta from prior documentation:** `docs/maturity-history/2026-05-03-snapshot.md` claimed 3.9/5.0; LIVE triangulation (3 adversarial personas blind, independence-verified) converged at **2.84** — **1.06-point overclaim revealed**. This is the empirical evidence the plan was designed to produce.

---

## 2. Per-Dimension Scores

Each dimension: score 0-5, TTL, evidence-file-count, scorecard link. Final scores populated by Phase 3 (T3.4 scorecards) — currently showing pre-Phase-3 placeholders from 2026-05-03 baseline + 2026-05-11 conversation deltas.

| # | Dimension | LIVE Score | Std-dev | TTL | Scorecard | Status |
|---|-----------|-----------:|--------:|----:|-----------|--------|
| 1 | Core Deploy Flow | **3.0** | 0.289 | 30d | [scorecard-core-deploy-flow.md](governance/scoring-2026-05-11/scorecard-core-deploy-flow.md) | ✅ CONVERGENT |
| 2 | DX / CLI | _3.1_ | **0.651** | 30d | [scorecard-dx-cli.md](governance/scoring-2026-05-11/scorecard-dx-cli.md) | 🔴 FLAGGED — [reconciliation pending](governance/scoring-2026-05-11/reconciliation-dx-cli.md) |
| 3 | Infrastructure / Operations | **2.8** | 0.115 | 60d | [scorecard-infra-ops.md](governance/scoring-2026-05-11/scorecard-infra-ops.md) | ✅ CONVERGENT |
| 4 | Security | _3.0_ | **0.361** | 60d | [scorecard-security.md](governance/scoring-2026-05-11/scorecard-security.md) | 🔴 FLAGGED — [reconciliation pending](governance/scoring-2026-05-11/reconciliation-security.md) |
| 5 | Scale / Multi-tenancy | **2.9** | 0.208 | 60d | [scorecard-scale-multitenancy.md](governance/scoring-2026-05-11/scorecard-scale-multitenancy.md) | ✅ CONVERGENT |
| 6 | Product Completeness | _2.2_ | **0.802** | 30d | [scorecard-product-completeness.md](governance/scoring-2026-05-11/scorecard-product-completeness.md) | 🔴 FLAGGED — [reconciliation pending](governance/scoring-2026-05-11/reconciliation-product-completeness.md) |
| 7 | Tests / Confidence | _2.9_ | **0.551** | 30d | [scorecard-tests-confidence.md](governance/scoring-2026-05-11/scorecard-tests-confidence.md) | 🔴 FLAGGED — [reconciliation pending](governance/scoring-2026-05-11/reconciliation-tests-confidence.md) |

**Median of medians (pre-reconciliation):** **2.84** — 3 convergent (median final) + 4 flagged (pending T3.3 arbiter input).

**Score interpretation:**
- **Bold** = CONVERGENT (std-dev ≤ 0.3; final)
- _Italic_ = FLAGGED median (std-dev > 0.3; needs reconciliation meeting per ADR D4)

**Per-persona detail (T3.1 LIVE):** `docs/governance/scoring-2026-05-11/{p1-vercel-dd,p2-hostile-vc,p3-paranoid-sre}.yaml`

---

## 3. Open Gaps (Enumerated)

Material gaps that hold back launch. Listed deliberately — not buried in prose.

### Critical (block customer traffic)
- **Zero external users have ever deployed LoopZero.** All validation is team-internal. (Sprint 9)
- **Sprint 8 Phase 1+3+4+5+6 deferred** — L3 stabilization, L4 capacity, Gate 2a Exit, Chaos validation, Observability hardening. Require CHG-002b/c (not opened).
- **Phase 3 adversarial run not executed** — scores in §2 are pre-validation placeholders.

### High (degrade confidence)
- 10 failing tests in `api/internal/k8s` cataloged but not fixed.
- `platform.ts` (495 LOC installer) has zero tests.
- AppSecret CRD defined but controller not implemented.
- cosign keyless Fulcio pending Sprint 9.
- Local cert-manager letsencrypt-prod cannot issue for `*.loopzero.local` (no public DNS for ACME).
- 0/348 claims meet EVIDENCED bar per `docs/governance/evidence-gaps-report.md` (92.5% UNEVIDENCED, 3.2% BROKEN).

### Medium (worth fixing pre-launch)
- 7 stale claims (operational decay) per `docs/governance/stale-claims-2026-05-11.md`.
- Bias-check.sh FP rate ~30% — calibration target was <20% (T0.4 accepted with this gap documented).
- CLAUDE.md known issues table has 39+ bug references (B1-B39 + F-15..F-22) without consolidation.

---

## 4. Last 30 Days Activity

<!-- AUTO:START activity-30d -->
_Auto-regenerated by infra/scripts/governance/regenerate-status.sh on 2026-05-11_

Recent commits (last 30 days):

```
bdccf463 feat(governance): Phase 1 — claim extraction + evidence classification + decay clocks
e5c25a3c feat(governance): Phase 0 — evidence bar + bias taxonomy + decay policy + adversarial personas
6df4eb40 Merge branch 'develop' of github-loopzero:getloopscale/loopzero into develop
6bfd6faf Add Excalidraw diagram skill and supporting files
99b23044 chore(gitignore): ignore .test-reports-cleanroom/ artifacts
341181ae docs(deep-review-2026-05-10): plan body — 29/30 ✅ + 1 explicit Sprint-9 deferral
fd4bf4e5 feat(deep-review-2026-05-10): T4.2 + T5.7 — error shape migration + sentinels deferral note
ae9e4abc feat(deep-review-2026-05-10): T5.3 + T5.5 — TenantCRPatcher iface + atomic retrigger counter
6d84efe7 feat(deep-review-2026-05-10): T2.2 partial — bundle SHA256 digest schema (find_sec_no_bundle_integrity HIGH)
f1e9c004 feat(deep-review-2026-05-10): T1.2 + T5.1 + T5.2 + T5.6 — preview lock + polish
0ec54698 docs(changelog): deep-review-2026-05-10 P0+P1+P2+P3+P4 — 19/30 tasks (63% complete)
88c23d4f feat(deep-review-2026-05-10): T5.4 — framework allowlist single source (find_arch_framework_dup MEDIUM)
679096f6 fix(deep-review-2026-05-10): T3.4 — remove ArgoCD dual-owner of admission-policies (find_admission_dual_owner MEDIUM)
c41862dd feat(deep-review-2026-05-10): P4 T4.1 + T4.3 — CLI delivery_failed handler + metric placement
4e0bcecf feat(deep-review-2026-05-10): P3 T3.1+T3.2+T3.3 — dead alerts cleared + FK index + startup probes
633bd1df feat(deep-review-2026-05-10): P2 T2.1 + T2.3 — admin secret consolidation + per-user rate limit
fc15cb89 feat(deep-review-2026-05-10): P1 T1.3-T1.7 — concurrency & state fixes
242670eb feat(deep-review-2026-05-10): T1.1 — SecretHandler wraps mutations in environment lock (find_conc_secrets_no_lock HIGH)
4087e91a feat(deep-review-2026-05-10): T0.5 — flip ClusterImagePolicy default warn→enforce (ADR-0050)
178fcb3a feat(deep-review-2026-05-10): P0 T0.1-T0.4 — preview cell-aware + pgDedup defer + dead env + IDOR fix
(git not available)
```
<!-- AUTO:END activity-30d -->

---

## 5. Decay Status

<!-- AUTO:START decay-summary -->
_Auto-regenerated by infra/scripts/governance/regenerate-status.sh on 2026-05-11_

| Bucket | Count | % |
|--------|------:|--:|
| Fresh (>=50% TTL remaining) | 323 | 92% |
| Aging (50%-80% TTL elapsed) | 16 | 4% |
| Expiring (80%-100% TTL elapsed) | 2 | 0% |
| **Expired (>=TTL elapsed)** | **7** | **2%** |

**Total claims:** 348

Run `task decay:scan` for full report by severity.
<!-- AUTO:END decay-summary -->

---

## 6. Evidence Index

Per dimension → links to `docs/evidence/{dim}/`. Each evidence file follows schema in `docs/GOVERNANCE.md (§1 Evidence Bar)`.

| Dimension | Evidence Directory | File Count | Pass Rate (≥3 sources) |
|-----------|---------------------|-----------:|------------------------:|
| Core Deploy Flow | `docs/evidence/core-deploy/` | _0_ | _pending T4.5_ |
| DX / CLI | `docs/evidence/dx-cli/` | _0_ | _pending_ |
| Infra / Ops | `docs/evidence/infra-ops/` | _0_ | _pending_ |
| Security | `docs/evidence/security/` | _0_ | _pending_ |
| Scale / MT | `docs/evidence/scale-mt/` | _0_ | _pending_ |
| Product Completeness | `docs/evidence/product-completeness/` | _0_ | _pending_ |
| Tests / Confidence | `docs/evidence/tests-confidence/` | _0_ | _pending_ |

**Evidence creation flow (T4.5):**
1. Extract claim from `docs/governance/claim-catalog.yaml`
2. Find 3 distinct refs (T1 commit+test, T2 LIVE log, T3 CI run)
3. Create `docs/evidence/{dim}/{claim-slug}.md` per schema
4. Run `task validate:evidence` — must exit 0

---

## 7. Adversarial Validation Log

History of Phase 3 triangulated scorings.

| Date | P1 model | P2 model | P3 model | Max std-dev | Flagged | Median | Scoring directory |
|------|----------|----------|----------|------------:|--------:|-------:|--------------------|
| 2026-05-11 | opus | sonnet | haiku | 0.802 (Product Completeness) | 4/7 | **2.84** | [scoring-2026-05-11/](governance/scoring-2026-05-11/) |

**Convergent dims (final):** Core Deploy 3.0, Infra/Ops 2.8, Scale/MT 2.9
**Flagged dims (median pre-reconciliation):** DX/CLI 3.1, Security 3.0, Product Completeness 2.2, Tests/Confidence 2.9

**Material finding:** Documented baseline 3.9 vs adversarial median 2.84 = **1.06-point overclaim** revealed. See `docs/governance/scoring-2026-05-11/raw-runs.md`.

**Next scheduled:** Nightly spotcheck via `.github/workflows/adversarial-spotcheck.yml`. Full re-triangulation when TTL expires (2026-06-10 for operational dims).

---

## How This Page Works

```
       Claim                  Evidence files            Adversarial scoring
   (in code/spec)            (3 distinct sources)        (3 personas blind)
        │                          │                          │
        ▼                          ▼                          ▼
   docs/governance/   ┌──►  docs/evidence/{dim}/ ─►  scoring-YYYY-MM-DD/
   claim-catalog.yaml │             │                  triangulation.yaml
                     │             ▼                          │
                     │      validate-evidence.sh              ▼
                     │             │                  reconciled.yaml
                     │             └──────────┬──────────────┘
                     │                        ▼
                     │                  THIS FILE (docs/STATUS.md)
                     │                        │
                     │                        ▼
                     │              ┌─ decay-scan (weekly CI)
                     │              ├─ cross-validation (weekly CI)
                     │              ├─ nightly spotcheck
                     │              └─ pre-merge gate
                     │                        │
                     └────────────────────────┘  (decay triggers revalidation)
```

**Anti-pattern (the OLD way):** status spread across CLAUDE.md + maturity-assessment.md + plans/ + meetings/ + runbooks/ + MEMORY.md with zero TTL and zero evidence linkage. See `docs/architecture/governance/concept-status-md-information-architecture.excalidraw` for visual.

---

## Cross-References

- **Plan:** `docs/plans/status-validation-and-doc-consolidation-plan.md` v1.2
- **Edge cases:** `docs/plans/status-validation-and-doc-consolidation-edge-cases.md`
- **Architecture (AS-IS):** `docs/architecture/governance/c4-context.puml`
- **Visual pipeline:** `docs/architecture/governance/concept-evidence-triangulation-pipeline.excalidraw`
- **Visual bias:** `docs/architecture/governance/concept-bias-taxonomy.excalidraw`
- **Visual decay:** `docs/architecture/governance/concept-decay-lifecycle.excalidraw`
- **Evidence policy:** `docs/GOVERNANCE.md (§1 Evidence Bar)`
- **Decay policy:** `docs/GOVERNANCE.md (§2 Decay Policy)`
- **Bias taxonomy:** `docs/GOVERNANCE.md (§3 Bias Taxonomy)`
- **Adversarial personas:** `docs/GOVERNANCE.md (§4 Adversarial Personas)`
- **Claim catalog:** `docs/governance/claim-catalog.yaml` (348 claims, Phase 1 baseline)

---

**Last manual edit:** 2026-05-11 (T4.1 of plan — initial scaffolding with placeholders pending Phase 3 adversarial run + Phase 4.5 evidence file population).
