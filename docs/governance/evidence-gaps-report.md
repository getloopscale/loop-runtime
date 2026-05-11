# Evidence Gaps Report — 2026-05-11

**Total claims:** 348

## Distribution by Evidence Tier

| Tier | Count | %% |
|------|------:|---:|
| EVIDENCED | 0 | 0.0 |
| PARTIAL_CONFIDENCE | 15 | 4.3 |
| UNEVIDENCED | 322 | 92.5 |
| BROKEN | 11 | 3.2 |
| UNKNOWN | 0 | 0.0 |

## Gaps by Dimension

### core-deploy — 65 gaps

**BROKEN (5):**
- `docs/maturity-assessment.md:6` — **Score: 3.9 / 5.0** (was 3.8 post-cell-2-activation; +0.1 from fix-all-platform-issues-plan iter 1-7. **Empirical evide
  - Broken: commit_missing:b04e0905c1
- `CLAUDE.md:19` — **`loopzero scale` — fully functional E2E** (B40 fixed 2026-05-06, tag `scale-b40-converged-20260506`): chain DB→deploy→K8s 
  - Broken: commit_missing:20260506
- `CLAUDE.md:19` — **`loopzero scale` — fully functional E2E** (B40 fixed 2026-05-06, tag `scale-b40-converged-20260506`): chain DB→deploy→K8s 
  - Broken: commit_missing:20260506
- `CLAUDE.md:19` — **`loopzero scale` — fully functional E2E** (B40 fixed 2026-05-06, tag `scale-b40-converged-20260506`): chain DB→deploy→K8s 
  - Broken: commit_missing:20260506
- `README.md:127` — | Cleanroom dogfood (E2E) | preflight + 11 journeys | **12/12 GREEN** end-to-end on fresh `k3d` destroy/setup cycle (202
  - Broken: commit_missing:20260505

**UNEVIDENCED (60):**
- `docs/maturity-assessment.md:12` — LoopZero has a strong technical foundation -- 4-plane architecture, cell-based scaling, strict state machines, advisory lock
- `docs/maturity-assessment.md:48` — 5. **GitHub App auto-deploy** works end-to-end -- this is the experience that sells
- `docs/maturity-assessment.md:93` — | Deploy happy path | Done | E2E validated on DO |
- `docs/maturity-assessment.md:94` — | Rollback | Done | ~3s, E2E validated |
- `docs/meetings/records/MEET-2026-05-03-066-ASYNC-AMENDMENT.md:76` — > `loopzero scale` config persistence shipped (Plan T2.1+T2.2+T2.4); operator actuation pending follow-up plan. In intermedi
- `docs/meetings/records/MEET-2026-05-03-063.md:62` — **Position.** APPROVE. **Condition:** Track-2 must include either (a) a private Fulcio deployment OR (b) a documented de
- `docs/meetings/records/MEET-2026-04-09-044.md:21` — - GitHub App auto-deploy E2E validated (happy path only)
- `docs/meetings/records/MEET-2026-04-22-056.md:60` — **Mutation testing is DEFERRED to Phase 3.** The immediate priority is filling the catastrophic test gaps in the deploy 
- `docs/meetings/records/MEET-2026-04-23-058.md:118` — **Impact if ignored.** If Pulumi is deferred to a later PR, prod is guaranteed to break on next deploy of API. This is n
- `docs/meetings/records/MEET-2026-04-23-059.md:83` — - If we ever support `scale` out-of-band (the `loopzero scale` CLI exists at `cli/src/commands/scale.ts`), we must decide wh
- _...50 more_

### dx-cli — 27 gaps

**BROKEN (1):**
- `docs/runbooks/action-sha-pinning.md:26` — | `docker/setup-buildx-action` | `b5ca514318bd6ebac0fb2aedd5d36ec1b5c232a2` | n/a |
  - Broken: commit_missing:b5ca514318

**UNEVIDENCED (26):**
- `docs/cli-reference.md:17` — | ❌ | Backend não implementado (CLI retorna mensagem UX-friendly via AUDIT-27/28) |
- `docs/meetings/records/MEET-2026-04-27-060.md:392` — | DX Engineer | APPROVE (Option B) | No veto; `task bootstrap:dev` deferred to backlog |
- `docs/meetings/adr/0040-sbom-attestation-parity-charts.md:9` — Sprint 6 (CHG-2026-04-26-SPRINT6-IMPL) shipped cosign image signing + syft SBOM (CycloneDX) + policy-controller deny-by-
- `docs/meetings/adr/MEET-INFRA-NORMALIZE-T3-multi-cell.md:8` — **Out of scope:** Operator topology cutover (D1, separate decision), Pulumi consolidation (D2, separate decision), CLI c
- `docs/meetings/adr/MEET-INFRA-NORMALIZE-T3-multi-cell.md:8` — **Out of scope:** Operator topology cutover (D1, separate decision), Pulumi consolidation (D2, separate decision), CLI c
- `docs/meetings/adr/MEET-INFRA-NORMALIZE-T3-operator-split.md:8` — **Out of scope:** Pulumi consolidation (D2), multi-cell activation (D3), CLI cluster-real tests (D4)
- `docs/meetings/adr/MEET-INFRA-NORMALIZE-T3-pulumi-consolidation.md:8` — **Out of scope:** Operator topology cutover (D1 — see MEET-INFRA-NORMALIZE-T3-operator-split.md), multi-cell activation 
- `docs/meetings/adr/0011-replicas-autoscaling-precedence-and-plan-caps.md:154` — 4. `task lint` — CLI B15 files clean; pre-existing auth.ts/infra.ts errors out of scope.
- `docs/governance/evidence-gaps-report.md:51` — - `docs/cli-reference.md:17` — | ❌ | Backend não implementado (CLI retorna mensagem UX-friendly via AUDIT-27/28) |
- `docs/governance/evidence-gaps-report.md:52` — - `docs/meetings/records/MEET-2026-04-27-060.md:392` — | DX Engineer | APPROVE (Option B) | No veto; `task bootstrap:dev
- _...16 more_

### infra-ops — 68 gaps

**BROKEN (2):**
- `docs/meetings/records/MEET-2026-04-27-061.md:7` — **Pre-condition:** CHG-001 CLOSED 2026-04-27 13:40 — validation matrix 14/14 GREEN (`/home/paulo/.loopzero/artifacts/validat
  - Broken: commit_missing:20260427
- `CLAUDE.md:176` — | Cleanroom Dogfood Loop | Real PaaS test: destroy infra + setup + 12 journeys end-to-end | ✅ 12/12 GREEN (2026-05-05, t
  - Broken: commit_missing:20260505

**UNEVIDENCED (66):**
- `docs/maturity-assessment.md:27` — | **Product Completeness** | 3.4/5 | Sprint 5 / Preview Environments — **ALL 14 plan tasks complete** with ACs + DoDs va
- `docs/maturity-assessment.md:107` — | Preview environments | Done | Sprint 5 / CHG-2026-04-26-PREVIEW-IMPL: all 14 plan tasks complete + E2E validated again
- `docs/meetings/records/MEET-2026-04-15-049.md:38` — - Single Pulumi stack for now (accepted: split deferred to post-launch)
- `docs/meetings/records/MEET-2026-04-15-050.md:67` — - Single Pulumi stack for now (accepted: split deferred to post-launch)
- `docs/meetings/records/MEET-2026-05-03-063.md:115` — 1. **Two-track explicit split.** T4.1 splits into T4.1a (GHA, in-scope) and T4.1b (in-cluster, deferred to Sprint 9+ unl
- `docs/meetings/records/MEET-2026-04-27-060.md:77` — **Conditions.** Add T-1.0c sub-task: implement `lookupExistingRegistry(name)` in `core/adopt-or-create.ts` mirroring the
- `docs/meetings/records/MEET-2026-03-30-022.md:247` — **Resolution:** Consensus with nuance. Phase 4.2 scope: (a) Install Kyverno on workload cluster (required for verifyImag
- `docs/meetings/records/MEET-2026-04-23-058.md:247` — - **Option B' (Option B but Pulumi deferred to a follow-up PR).** Rejected by all five reviewers: guarantees prod outage
- `docs/meetings/records/MEET-2026-04-27-061.md:139` — - Cell-2 routing remains explicitly OUT OF SCOPE for CHG-002 (per ADR-0010 §Phase 2b). `runtimeCluster2` is provisioned 
- `docs/meetings/records/MEET-2026-05-03-064.md:54` — **Assessment.** Schema validation is a formal correctness gate. helm-unittest is a unit test layer that's currently abse
- _...56 more_

### product-completeness — 29 gaps

**UNEVIDENCED (29):**
- `docs/meetings/records/MEET-2026-03-29-019.md:120` — 1. **Alerts deferred to PRE-HARBOR phase** — `LoopZeroRegistryUnreachable` and `LoopZeroRegistryCredentialStale` alerts are trac
- `docs/meetings/records/MEET-2026-04-04-030.md:16` — **Decision Required:** What is the minimum viable set of changes, in what order, with what Definition of Done, before we
- `docs/meetings/records/MEET-2026-04-04-030.md:82` — - **SRE CONDITIONAL VETO on declaring production-ready without Sprint 7 tasks 7.1 + 7.2.** RunAsRoot and no PSA enforcem
- `docs/meetings/records/MEET-2026-04-04-030.md:437` — | **Total to production-ready** | **10-14h** | — |
- `docs/meetings/records/MEET-2026-04-27-060.md:39` — - Sprint 5 (CHG-2026-04-26-PREVIEW-IMPL) and Sprint 6 (CHG-2026-04-26-SPRINT6-IMPL) shipped; governance hook still scope
- `docs/meetings/records/MEET-2026-04-06-038.md:61` — **Recommendation:** 6 phases, DB-first. Free tier = DB-only, no Stripe subscription. Stripe customer creation deferred t
- `docs/meetings/records/MEET-2026-04-27-061.md:53` — - Sprint 5 / preview envs shipped 2026-04-26.
- `docs/meetings/adr/ADR-0021-infra-normalize-decisions.md:38` — - Production-design: cutover must be atomic (no ≥30s reconciler gap) — requires explicit migration script (C1) not yet s
- `docs/meetings/adr/ADR-0020-infra-parity-decisions.md:64` — **Rationale:** Without this constraint, customers self-hosting LoopZero build an emergent "third platform" that nobody tests
- `docs/meetings/adr/MEET-ADR-0018-decision.md:263` — | production-design-reviewer | Out of scope for T3.1+T3.2. Defer. |
- _...19 more_

### scale-mt — 64 gaps

**UNEVIDENCED (64):**
- `docs/maturity-assessment.md:26` — | **Scale / Multi-tenancy** | **3.5/5** | Cell architecture code 96% complete + **cell-2 LIVE-validated 2026-05-02** (ce
- `docs/maturity-assessment.md:26` — | **Scale / Multi-tenancy** | **3.5/5** | Cell architecture code 96% complete + **cell-2 LIVE-validated 2026-05-02** (ce
- `docs/meetings/MEET-ASYNC-AMENDMENT-002-e2e-ginkgo-rewrite-governance.md:101` — | 20 | Multi-cell routing | ❌ | ✅ |
- `docs/meetings/records/MEET-2026-04-09-042.md:49` — The ArgoCD blast radius incident was caused by Certificate CRs in the gitops tree. CF Zone Management v2 eliminates Cert
- `docs/meetings/records/MEET-2026-05-03-066-ASYNC-AMENDMENT.md:51` — **Reasoning:** Without T2.3, `loopzero scale` end-to-end is half-implemented: config persists but nothing actuates. The plan
- `docs/meetings/records/MEET-2026-04-09-044.md:129` — - Per-project ArgoCD Applications as near-term: deferred to P2/P3 — architectural cost not justified before 100 tenants
- `docs/meetings/records/MEET-2026-04-21-055.md:41` — | **A2: ApplicationSet Enforcement** | DEFER to Phase 4 | Dual ownership with TenantController. ConfigMap race condition
- `docs/meetings/records/MEET-2026-04-01-027.md:109` — - Scale: N/A (single-tenant dev pipeline).
- `docs/meetings/records/MEET-2026-04-17-051.md:212` — | **Migration** | Explicitly out of scope. New tenants → cell-2 (capacity-aware). Existing 100 stay on `cell-default`. M
- `docs/meetings/records/MEET-2026-04-23-059.md:278` — | `loopzero scale` command diverges from loopzero.yaml | Out of scope; follow-up ticket. | tracked as follow-up |
- _...54 more_

### security — 32 gaps

**BROKEN (1):**
- `docs/runbooks/action-sha-pinning.md:27` — | `sigstore/cosign-installer` | `cad07c2e89fa2edd6e2d7bab4c1aa38e53f76003` | n/a |
  - Broken: commit_missing:cad07c2e89

**UNEVIDENCED (31):**
- `docs/meetings/records/MEET-2026-05-03-062.md:138` — 4. Future: add OIDC for DigitalOcean (DO_TOKEN replacement) — out of scope for MEET-A; covered in MEET-B (cosign keyless
- `docs/meetings/records/MEET-2026-05-03-063.md:72` — **Position.** APPROVE. **Conditions:** (a) admission webhook timeout configured ≤ 3s; (b) policy in `warn` for ≥ 7 days 
- `docs/meetings/records/MEET-2026-04-04-030.md:14` — **Problem:** The LoopZero platform is at approximately 85% production readiness. Today's session (MEET-028, MEET-029) resolv
- `docs/meetings/records/MEET-2026-04-04-030.md:223` — **Resolution:** Security veto is ABSOLUTE per governance rules. Tasks 7.1, 7.2, 7.3, and 7.4 are MANDATORY before produc
- `docs/meetings/records/MEET-2026-04-17-052.md:248` — | 4 | Bulk evict authn/authz | `LOOPZERO_ADMIN_API_KEY` with dual-key rotation | No current RBAC role covers cross-tenant op
- `docs/meetings/records/MEET-2026-04-27-061.md:52` — - Sprint 6 / supply chain shipped 2026-04-26 (cosign + SBOM + policy-controller Audit). Deny-mode flip pending 2-week ob
- `docs/meetings/records/MEET-2026-04-05-032.md:200` — - [ ] Login flow works end-to-end: dashboard -> authenticate -> dashboard (token received)
- `docs/meetings/adr/ADR-0018-tenant-namespace-sot.md:11` — > **Follow-up:** ADR-0019 (RBAC tightening, deferred to Sprint 9)
- `docs/meetings/adr/0040-sbom-attestation-parity-charts.md:16` — - ❌ NO SBOM
- `docs/meetings/adr/MEET-ADR-0018-decision.md:172` — - C12: RBAC tightening is **out of scope** of this decision — defer to a follow-up ADR-0019. Remove "RBAC simplification
- _...21 more_

### tests-confidence — 48 gaps

**BROKEN (2):**
- `docs/meetings/MEET-ASYNC-AMENDMENT-002-e2e-ginkgo-rewrite-governance.md:95` — - Cleanroom dogfood 12/12 GREEN remains valid: tag `dogfood-cleanroom-converged-20260505` covers the bash-journey path; 
  - Broken: commit_missing:20260505
- `docs/governance/evidence-gaps-report.md:136` — - `docs/meetings/MEET-ASYNC-AMENDMENT-002-e2e-ginkgo-rewrite-governance.md:95` — - Cleanroom dogfood 12/12 GREEN remains
  - Broken: commit_missing:20260505

**UNEVIDENCED (46):**
- `docs/meetings/records/MEET-2026-04-21-055.md:120` — - [ ] `task loadtest:run LEVEL=l1` works end-to-end
- `docs/meetings/adr/0040-sbom-attestation-parity-charts.md:17` — - ❌ NO attestation
- `docs/dogfood/2026-04-29-real-e2e.md:36` — - Audit assertions added to all 9 mutating journeys (J02, J03, J04, J05, J07, J08, J09 strict; J01, J06 already had; J10
- `docs/dogfood/2026-04-29-real-e2e.md:42` — | Phase 2 full-loop.sh harness | ✅ GREEN | state machine + journeys/ | — |
- `docs/dogfood/2026-04-29-real-e2e.md:44` — | Journey 02 multi-server | ✅ GREEN | 2 apps (server+worker) project-scoped | Achado #17 validado runtime |
- `docs/dogfood/2026-04-29-real-e2e.md:45` — | Journey 03 secrets lifecycle | ✅ GREEN | set/list/delete + Rule 9/fail-fast | **Achado #19 (S0 prod) ESO CSS bootstrap
- `docs/dogfood/2026-04-29-real-e2e.md:49` — | Journey 07 custom domain | ✅ GREEN (parcial) | register/list/status/remove (4/5 verbs) | **Achado #22 (Rule 8 fail-cle
- `docs/dogfood/2026-04-29-real-e2e.md:52` — | Journey 10 failure modes (§12) | ✅ GREEN | F1/F3/F5; F2 GAP documentado | — |
- `docs/dogfood/2026-04-29-real-e2e.md:55` — **Cobertura total**: 11/11 journeys GREEN. **9 bugs sistêmicos shipped + validados** vs estado pré-iter 13.
- `docs/governance/evidence-gaps-report.md:140` — - `docs/meetings/records/MEET-2026-04-21-055.md:120` — - [ ] `task loadtest:run LEVEL=l1` works end-to-end
- _...36 more_

## Phase 2 Priority Queue

Claims marked BROKEN should be addressed FIRST (refs point to nothing).
Then UNEVIDENCED needing T1/T2/T3 — split by dimension.
