# Scorecard — DX / CLI

**Generated:** 2026-05-11
**TTL:** 30 days
**Expires:** 2026-06-10

## Final Score (Triangulated)

| Field | Value |
|-------|------:|
| **Median** | 3.1 |
| **Std-dev** | 0.651 |
| **Flagged (>0.3)?** | True |
| **Final score** | _Pending T3.3 reconciliation (flagged)_ |

## Persona Scores

| Persona | Model | Score | Justification |
|---------|-------|------:|---------------|
| P1 Vercel DD | claude-opus-4-7 | 2.5 | • `docs/cli-reference.md:17` is UNEVIDENCED and contains an INTERNAL TERM in user-facing copy: 'Backend não implementado (CLI retorna mensagem UX-friendly via AUDIT-27/28)' — AUDIT-27/28 is internal bug taxonomy that has zero meaning to a customer reading the spec. AUTO -0.5 per scoring rules (internal terms leak).<br>• 27 dx-cli gaps in `evidence-gaps-report.md` §dx-cli — 26 UNEVIDENCED, 1 BROKEN (`action-sha-pinning.md:26` commit_missing:b5ca514318). When 100% of CLI claims are unverified except one POC fixture (`2026-05-11-validate-evidence-script.md`), I cannot distinguish 'works' from 'tested by author once'. Vercel CI snapshots every `--json` shape per command per PR.<br>• Per `deep-dive-current-state.md` §'6 Bias Patterns Observed' #6 'Implemented but Not Wired' — CLAUDE.md self-admits AppSecret CRD defined without controller and pre-B40 ScalingResolver existed but wasn't invoked. These are the exact 'demo works / production silently doesn't' traps Vercel DD penalizes — and they're acknowledged in the codebase's own known-issues table. |
| P2 Hostile VC | claude-sonnet-4-6 | 3.1 | • evidence-gaps-report.md §dx-cli: 26 UNEVIDENCED + 1 BROKEN. Not a single claim about CLI commands, exit codes, or --json shape stability has a triangulated evidence file (T1+T2+T3). 'Vercel-quality DX' is the product positioning but zero outsiders have used it without support channels. Self-team dogfood counts as internal, not external validation.<br>• cli-reference.md:17 has an explicit '❌ Backend not implemented' marker classified UNEVIDENCED last_validated 2026-04-26 — now 15 days stale. Spec itself documents gaps; the evidence system cannot tell me whether those gaps closed. If the spec says ❌ and no one updated it, I have to treat the feature as absent.<br>• 1210+ CLI tests claimed, zero coverage percentage provided. Auto-penalty per scoring rules: -0.3 for test count without coverage %. I need to know: what % of CLI paths are exercised? Are the 1210 tests covering all 36 commands, or are 900 testing the same 5 happy paths? |
| P3 Paranoid SRE | claude-haiku-4-5 | 3.8 | • Exit codes (0/1/2/3/4/5) are documented and tested. No internal terms found in CLI output spot-check (Rule 3 compliance). --json output shape is a stability contract but no mutation testing validates compatibility across CLI versions. If a field is accidentally renamed in a patch release, how would we know before 50 customers report broken CI scripts?<br>• CLI commands exist for happy path (deploy, promote, rollback, scale, logs, status, cancel). No documented failure-recovery for 'loopzero deploy' when build cluster is unreachable (HTTP 503 for 30s — what's the behavior?). CLI code suggests STUB patterns (comment: 'TODO lock') for some mutations, meaning error handling may be incomplete. Need failure-mode tests for every CLI command's error paths.<br>• 360+ tests in cli/ (jest) are passing locally. No integration tests verify CLI against real API instance under intentional failures (e.g., API database connection pool exhausted, no runners available, quota exceeded). CLI is tested in isolation from reality. |

## Evidence Cited

Cross-reference with `docs/evidence/dx-cli/`.

## Open Concerns (synthesis)

- 11 BROKEN evidence refs in headline claims (maturity score, cleanroom 12/12, loopzero scale E2E, cosign pinning) — these are the FOUR proofs a Vercel-tier customer DD would open first, and all four 404. The remediation queue in evidence-gaps-report.md should treat BROKEN refs as P0 blockers; UNEVIDENCED is fixable, BROKEN is actively misleading.
- Zero external user signal compounds with documented +0.4 maturity drift in same agent session (deep-dive §Event 2). Self-attestation by 1M-context agent fails empirically; introducing the 3-persona triangulation pipeline (this scorecard) is the right material defense — but the pipeline itself has 1 proof-of-concept evidence file. Until LIVE dogfood replays under new governance prove a stable score across personas + reviews, the platform is 'demo-tier convincing, DD-tier exposed'.
- Internal terms (AUDIT-27/28 in cli-reference.md:17, ArgoCD/Gitea/CRD acknowledged via CLAUDE.md Rule 3 enforcement) keep leaking into customer-facing surfaces. Bias #6 'Implemented but Not Wired' is self-admitted in CLAUDE.md's own Systemic Patterns table — that auto-awareness without material defense is the exact pattern that ships AppSecret CRDs without controllers. A customer hitting `loopzero secrets rotate` discovers it isn't wired AFTER paying for Pro.
- SYSTEMIC SELF-ATTESTATION: 0 of 348 claims are EVIDENCED per the governance system's own classifier. 92.5% are UNEVIDENCED. The team built a sophisticated evidence framework (claim catalog, decay clocks, bias taxonomy) and then used it to prove that every single claim they make is unverified. This is the most honest form of overclaim I have encountered: the founders know their claims are unevidenced and built a system to track it. The system is not the evidence.
- AGGREGATION BIAS CONFIRMED BY OWN TOOLING: deep-dive-current-state.md §Aggregation Bias documents that the reported 3.9 maturity score is a round-up of the actual arithmetic mean 3.79. The same document records that the authoring agent drifted +0.4 points in a single 30-minute session under self-attestation pressure. This is not a loopzeroretical risk; it is a measured, documented instance of the bias the governance system was designed to prevent.
- BROKEN COMMIT REFERENCES ON KEY CLAIMS: 11 BROKEN claims across 5 dimensions cite commits that cannot be resolved. The maturity score anchor (b04e0905c1), the cleanroom dogfood tag (20260505), and the scale B40 fix (20260506) are all BROKEN. In a $10M due diligence, broken evidence is not a minor documentation issue — it is a data integrity failure that invalidates the claims downstream.
- SPRINT 9 NOT STARTED = PRE-RELEASE PRODUCT: The team's own sprint table marks Sprint 9 (Polish + Launch) as NOT STARTED. The product is pre-release. Yet the pitch language ('shipped,' 'LIVE-validated,' 'production-ready,' 'E2E validated on DO') reads as a launched product. The gap between pitch language and sprint status is the clearest signal of overclaim in this audit.
- SECURITY POSTURE STRUCTURALLY INCOMPLETE: Policy-controller in warn mode, cosign keyless pending Sprint 9, RBAC tightening deferred to ADR-0019/Sprint 9, DO_TOKEN (static credential) replacing OIDC deferred to future. An investor in a platform-as-a-service handling multi-tenant workloads must price the probability of a security incident before Sprint 9 is completed — because the product will be launched into Sprint 9 still incomplete.

## TTL

- **Last validated:** 2026-05-11 (T3.1 LIVE adversarial run)
- **Expires:** 2026-06-10 (30d TTL per `docs/GOVERNANCE.md (§2 Decay Policy)`)
- **Revalidation:** rerun T3.1 with new dated `scoring-YYYY-MM-DD/`

## Cross-References

- Plan task: T3.4 of `docs/plans/status-validation-and-doc-consolidation-plan.md`
- Triangulation: `/triangulation.yaml`
- Raw runs: `/raw-runs.md`
- Linked from: `docs/STATUS.md` §2 row `dx_cli`
