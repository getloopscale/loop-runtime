---
type: plan
title: "Plan: Maturity 3.62→4.0 Roadmap — All 12 Modules to Excellence"
status: deprecated
---

# Plan: Maturity 3.62→4.0 Roadmap — All 12 Modules to Excellence

> **Version 1.1** — Synthesize all "Caminho para X+1" sections from the 2026-05-26 modular audit into a single phased executable plan that lifts the entire scorecard from average 3.62 → 4.0 (+0.38 portfolio lift). Address 8 HIGH + 8 MEDIUM + 10 LOW cross-module gaps via 6 phases: operator/sponsor decisions (Phase 0), mechanical high-value (Phase 1), architectural medium (Phase 2), operational runbooks (Phase 3), test debt + polish (Phase 4), re-audit + close-out (Phase 5). Document trade-offs that cap pkg/ + State Machines at 3.9 (M11 #25 typed-enum migration intractable per DB CHECK + JSON marshaling + K8s status conversions). Pre-release governance respected — all tasks tier-classified; operator HARD STOPS preserved (T5.1 Op B 7-day soak NOT in scope here).
>
> **v1.1 changes (2026-05-26):** Absorved 8 MUST FIX edge cases from `/edge-case-plan` review (see [`docs/plans/2026-05-26-maturity-3-62-to-4-0-roadmap-edge-case-review.md`](./2026-05-26-maturity-3-62-to-4-0-roadmap-edge-case-review.md)) — clarifications em T1.1 (correlation pkg path), T2.2 (Fulcio/Rekor chart pinning + TLS issuer per env), T2.3 (values.schema.json oneOf + ClusterImagePolicy progressive rollout), T3.3 (ConfigMap path + emergency override), T4.6 (Promtail templating mechanism). 6 SHOULD TEST + 5 DOCUMENT edges noted in review doc for awareness; not blocking.

## Context

The 2026-05-26 modular audit (`.claude/knowledge-base/audits/2026-05-26/SUMMARY.md`) measured 12 modules at average **3.62 / 4.0** (range 3.4-3.9):

```
4.0  ┃                                                             0 módulos
3.9  ┃ ██ pkg/                                                     1 módulo
3.8  ┃ ████ Multi-Cell, State Machines                             2 módulos
3.7  ┃ ████ API, Auth/Billing                                      2 módulos
3.6  ┃ ██████ CLI, Operators, Observability                        3 módulos
3.5  ┃ ██ Helm                                                     1 módulo
3.4  ┃ ██████ IaC, Build Pipeline, CI/CD                           3 módulos
```

Movement vs 2026-05-11 baseline (897 commits in 15 days): **+0.13** (11 modules ↑, 1 module ↓ intentional). Floor lifted 3.0 → 3.4 (IaC graduated from bottom via pulumi-control-plane deletion). Ceiling lifted 3.8 → 3.9 (pkg/ via zero deprecated symbols).

**Why this plan now:**
- 8 HIGH gaps identified across 7 modules (production canary, scheduler gap, charts overlap drift, correlation-ID, cosign keyless infra, withEnvironmentLock opacity, drill GREEN gate, errors.New residuals)
- 8 MEDIUM + 10 LOW gaps cumulative
- CTO Rule 0 (local↔prod parity) requires real CI platform decision (post GitHub Actions removal 2026-05-23)
- CTO Rule 5 (50k tenant target) requires production canary completion
- Sponsor-direct approval pattern (3 invocations in May 2026) demonstrates governance can move HIGH items fast

**Evidence baseline:**
- All 12 module docs in `.claude/knowledge-base/audits/2026-05-26/{01-cli,02-api,...,12-cicd-testing}.md`
- SUMMARY § 3 "Delta vs Audit Anterior" — delta tracking per module
- SUMMARY § 4 "Top Gaps Cross-Module" — prioritized HIGH/MEDIUM/LOW
- SUMMARY § 5 "Caminho Consolidado para 4.0" — strategic groupings

## Objective

**Done = all 12 modules at ≥ 3.9/4.0; portfolio average ≥ 3.95/4.0; 8 HIGH gaps closed or formally deferred via MEET-ASYNC-AMENDMENT; production canary stages 4-5 LIVE + 1 real customer day-trial completed.**

Specific measurable goals:
1. CLI 3.6 → 3.8 (wire `doctor` + standardize exit pattern + CLAUDE.md drift)
2. API 3.7 → 3.9 (lock visibility audit + errors.New triage + fuzz tests)
3. Operators 3.6 → 3.8 (correlation-ID em 7 reconcilers + property tests)
4. pkg/ 3.9 → **3.9 capped** (shellrun coverage + property extension; M11 #25 typed enum intractable)
5. IaC 3.4 → 3.6 (pulumi-platform component tests + dry-run em orchestrators)
6. Helm 3.5 → 3.7 (overlap parity gate + schema tightening + digest enforcement)
7. Build Pipeline 3.4 → 3.6 (cosign keyless infra + check-cutover-deadline script + Trivy/TruffleHog re-wire)
8. Observability 3.6 → 3.8 (correlation-ID propagation + Loki federation)
9. Multi-Cell 3.8 → 4.0 (cell drain runbook + production canary Stage 4-5)
10. Auth/Billing 3.7 → 3.9 (session rotation runbook + Stripe Portal SDK)
11. State Machines 3.8 → **3.9 capped** (helper consistency + ARCHIVED integration; M11 #25 capped)
12. CI/CD 3.4 → 3.6 (real CI platform integration + Polaris/kubeconform re-wire)

## ADRs

### D1 — Phased delivery prioritizing operator-decisions first

**Decision:** Phase 0 (operator/sponsor decisions) gates Phases 1-4. No engineering work on items blocked by sponsor decision until decision made.

**Rationale:** 3 modules (M09 Multi-Cell, M12 CI/CD, partial M03 Operators) are blocked by sponsor decisions (real CI platform choice, production canary, customer trial). Pre-committing engineering effort wastes work if decision goes different direction. Phase 0 documents what decision needs to be made + drives MEET process via `meeting-orchestrator`.

**Consequences:**
- Phases 1-4 can start in parallel for non-blocked items
- Phase 0 may take longer in calendar time but minimizes wasted engineering
- Forces explicit governance ratification before engineering commitment

### D2 — Architecture docs Pre/Post-Plan required for affected domains

**Decision:** Invoke `/architecture-docs` Mode 2 (Pre-Plan AS-IS) + Mode 3 (Post-Plan PROPOSED) for 5 domains lacking current architecture docs: observability, build-pipeline, ci-cd, infra, auth. Platform + multi-cell already exist (per 2026-05-25 Op B plan).

**Rationale:** Per `/to-plan` skill mandatory steps (sections 3 + 5). Forces explicit AS-IS snapshot before changes + PROPOSED diff post-plan. Operator can compare visually before approving large changes.

**Consequences:**
- ~3h additional effort for 5 new architecture domain docs
- Operator promotion decision required post-implementation (`/architecture-docs` Mode 4)
- Provides traceable architectural diff for future audits

### D3 — Defer M11 #25 typed-enum migration (string aliases) — formally documented cap at 3.9

**Decision:** pkg/statemachine string aliases (M11 #25) remain mitigated via MustParseState/IsKnownState trust-boundary validators — NOT migrated to typed enums.

**Rationale:** Typed enum migration would break:
- DB CHECK constraints (require string values em 6+ tables)
- JSON marshaling (API responses, CR status fields across 8 CRDs)
- K8s status field conversions cross-operator (4 split binaries)

Estimated effort 16+ hours with HIGH regression risk. Documented mitigation via trust-boundary validators is pragmatic + active adoption (application_reaper.go:127-129).

**Consequences:**
- pkg/ + State Machines modules permanently capped at 3.9/4.0 (not 4.0)
- Average ceiling becomes 11×4.0 + 2×3.9 / 12 = **3.983** instead of perfect 4.0
- Acceptable trade-off; formal ADR via MEET-ASYNC-AMENDMENT this plan

### D4 — Real CI platform decision = T3 critical sponsor meeting

**Decision:** Real CI platform choice (post GitHub Actions removal 2026-05-23) is T3 sponsor meeting via `meeting-orchestrator`. Options to evaluate: GitLab CI · Jenkins · Drone CI · BuildKite · Argo Events + Workflows + scheduled CronWorkflows · self-hosted GitHub Actions runners.

**Rationale:** Choice affects:
- All quality gates (re-wire required)
- Drill orchestration cron (currently operator-run)
- Coverage reporting cadence
- Production canary automation
- Security scanning (Trivy auto-delete)

Cannot be decided unilaterally; needs Production Design Reviewer + Security & Blast Radius Reviewer + SRE Engineer + Sponsor.

**Consequences:**
- Phase 0 includes meeting orchestration as deliverable
- Phase 1 mechanical work continues in parallel (does not depend on CI choice)
- Phase 2 + Phase 5 re-audit depend on Phase 0 completion

### D5 — Production canary stages 4-5 via Argo Rollouts (existing infrastructure)

**Decision:** Stage 4 (canary traffic split 1% → 10% → 50% → 100%) implemented via existing Argo Rollouts deployment (per CLAUDE.md "progressive delivery" — shipped 2026-05-07 com per-plan steps + TraefikService weighted routing + AnalysisTemplate). Stage 5 (1 customer day-trial) requires sponsor + customer onboarding.

**Rationale:** Infrastructure já existe (Argo Rollouts + TraefikService + AnalysisTemplate). Não precisa nova engenharia — apenas operator configuration + customer trial coordination.

**Consequences:**
- Phase 0 documents Stage 4 traffic split procedure (mostly operator runbook, low engineering)
- Customer trial gated by sponsor onboarding (calendar time, not engineering)
- Once complete, Multi-Cell saltos para 4.0 (último gate)

### D6 — Cosign keyless infra provisioning via Pulumi component (NOT manual)

**Decision:** Fulcio + Rekor instances provisioned via novo Pulumi component em pulumi-platform, NOT via manual Helm install. Following ADR-0023 declarative pattern.

**Rationale:** Manual Helm install creates configuration drift; Pulumi component is declarative + idempotent + auditable. ADR 2026-05-12 cosign keyless cutover commits T0+90d deadline — Pulumi enables faster phased rollout (Phase A T0+30d ready).

**Consequences:**
- ~16h engineering for Pulumi component (vs ~8h manual Helm)
- Future operability higher (one-command deploy/destroy/upgrade)
- Phase 2 includes Pulumi component as deliverable

## Dependency Graph

```
Phase 0 (Sponsor/Operator decisions, calendar-blocking)
├─ T0.1 Architecture docs Pre-Plan (5 domains)  ──────────┐
├─ T0.2 Real CI platform decision (T3 meeting)             │
├─ T0.3 Production canary Stage 4 procedure documented     │
└─ T0.4 Customer trial onboarding plan                     │
                                                            │
                ▼                                           │
Phase 1 (Mechanical high-value, parallel)                  │
├─ T1.1 Correlation-ID propagation × 39 reconcilers       │
├─ T1.2 Charts overlap parity CI gate                      │
├─ T1.3 withEnvironmentLock visibility audit               │
├─ T1.4 CLAUDE.md numerical drift cleanup                  │
└─ T1.5 Architecture docs Post-Plan (5 domains diff)  ◄────┘
                │
                ▼
Phase 2 (Architectural medium, mostly parallel)
├─ T2.1 pulumi-platform component tests
├─ T2.2 Cosign keyless infra (Pulumi component)
├─ T2.3 Cosign digest enforcement em charts
├─ T2.4 30 errors.New() triage + conversion
├─ T2.5 Multi-K8s-version testing (chart-testing matrix)
├─ T2.6 CLI: wire doctor + process.exit standardization
└─ T2.7 Auth: Stripe Portal SDK + token rotation runbook
                │
                ▼
Phase 3 (Operational runbooks, sequential)
├─ T3.1 Cell drain operational runbook
├─ T3.2 Session token rotation runbook
├─ T3.3 Signing key rotation procedure + check-cutover-deadline script
└─ T3.4 CLAUDE.md comprehensive refresh
                │
                ▼
Phase 4 (Test debt + polish, parallel)
├─ T4.1 Property-based tests extension (errors, jwt, enforcement)
├─ T4.2 shellrun test coverage (signal/exit corners)
├─ T4.3 Dashboard semantic validation (promtool query check)
├─ T4.4 M11 helper consistency
├─ T4.5 Release ARCHIVED state machine integration
├─ T4.6 Loki cross-cluster federation
└─ T4.7 Trivy/TruffleHog re-wire em task quality:*
                │
                ▼
Phase 5 (Re-audit + close-out, sequential)
├─ T5.1 /audit-modular full re-execute
├─ T5.2 Score lift validation per module
├─ T5.3 Cross-validation final pass
└─ T5.4 SUMMARY.md delta update + CHANGELOG.md
```

**Parallelism:**
- Phase 1 can run all 5 tasks in parallel after T0.1
- Phase 2 mostly parallel (7 tasks; only T2.5 needs T2.3 first)
- Phase 4 fully parallel (independent items)

**Sequential blockers:**
- Phase 0 → Phase 1 (T0.1 architecture docs needed for context)
- Phase 1 → Phase 2 (mechanical fixes establish patterns reused em architectural)
- Phase 2 → Phase 3 (runbooks document what was built)
- Phase 3 → Phase 4 (polish after critical paths done)
- Phase 4 → Phase 5 (validation)

---

## Phase 0: Operator/Sponsor Decisions (Calendar-Blocking)

**Objective:** Unblock 3 modules (M09 Multi-Cell, M12 CI/CD, partial M03/M07) via formal governance decisions. Establish architecture docs baseline for 5 new domains.

### T0.1 — Architecture Docs Pre-Plan (AS-IS) for 5 New Domains

#### Objective
Generate `docs/architecture/{observability,build-pipeline,ci-cd,infra,auth}/` AS-IS state docs. Platform + multi-cell already exist (per 2026-05-25 Op B plan).

#### Evidence
Per `/to-plan` skill mandatory step 3 (Pre-Plan AS-IS) + ADR D2 this plan. Operator + reviewers need traceable "before" state to evaluate proposed changes. 5 domains lack canonical arch docs:
- observability (M08 audit references metrics + tracing + Loki but no C4 diagram)
- build-pipeline (M07 audit references Argo Workflow + cosign + syft but no canonical doc)
- ci-cd (M12 audit references task quality:* + loopzero-ops but post-GitHub-Actions reality undocumented)
- infra (M05 audit references 5 Pulumi projects + Helmfile L2 but no canonical layered diagram)
- auth (M10 audit references Hydra/Kratos/Stripe flows but no end-to-end diagram)

#### Files to edit
```
docs/architecture/observability/c4-container.md          — (NEW) AS-IS metrics + logs + traces flow
docs/architecture/observability/operator-correlation.md  — (NEW) AS-IS correlation-ID propagation
docs/architecture/build-pipeline/c4-container.md         — (NEW) AS-IS Argo Workflow DAG + cosign + syft
docs/architecture/build-pipeline/keyless-cutover.md      — (NEW) AS-IS cosign keyed → keyless deadline
docs/architecture/ci-cd/c4-container.md                  — (NEW) AS-IS task quality:* + loopzero-ops post GitHub Actions removal
docs/architecture/ci-cd/scheduler-gap.md                 — (NEW) AS-IS operator-only execution
docs/architecture/infra/c4-container.md                  — (NEW) AS-IS L1+L2+L3 (Pulumi + Helmfile + ArgoCD)
docs/architecture/infra/multi-cluster-topology.md        — (NEW) AS-IS 4-cluster k3d + prod-identical drill
docs/architecture/auth/c4-container.md                   — (NEW) AS-IS Hydra device flow + Kratos session + Stripe
docs/architecture/auth/ed25519-dual-control.md           — (NEW) AS-IS Phase 1 accept-both
docs/architecture/index.md                                — (UPDATE) Add 10 new entries
```

#### Deep file dependency analysis
- Each doc é standalone reference; downstream readers só plan executors (Phase 1-4 task implementers) + auditors
- No code dependencies — pure documentation
- Frontmatter follows `.claude/rules/auto-docs.md` schema (type, dates, evidence_class)
- Cross-references via `[[name]]` per knowledge-base convention

#### Deep Dives
- Each doc MUST include `<!-- AS-IS STATE -->` header
- C4 Container view (Mermaid OR ASCII) showing component boundaries + data flows
- Evidence classified [MEASURED]/[REPORTED]/[ESTIMATED]
- File:line references onde apropriado

#### Tasks
1. Run `/architecture-docs` Mode 2 (Pre-Plan) com domains = "observability,build-pipeline,ci-cd,infra,auth"
2. Review generated docs against actual codebase (sample 3 file:line refs per doc)
3. Update `docs/architecture/index.md` com 10 new entries
4. Commit em PR separado (pre-implementation per CLAUDE.md auto-docs Rule 1)

#### TDD
```
RED:     N/A — documentation task
VERIFY:  All 10 files exist + each contains "AS-IS STATE" header + valid frontmatter
VERIFY:  docs/architecture/index.md updated com 10 new entries
VERIFY:  No emojis (CLAUDE.md style)
```

#### Acceptance Criteria
- [ ] All 10 architecture docs exist com valid frontmatter (`type`, `created_at`, `updated_at`, `updated_by`)
- [ ] Each contém `<!-- AS-IS STATE -->` header
- [ ] Evidence classified per `.claude/rules/auto-docs.md`
- [ ] Index updated em `docs/architecture/index.md`
- [ ] No emojis (CLAUDE.md style rule)
- [ ] Cross-references valid (`[[name]]` resolves)

#### DoD
- [ ] Files committed em PR separado
- [ ] No drift warnings from `.claude/hooks/doc-drift-check.sh`
- [ ] Tier: T0 (documentation only, no code change)

---

### T0.2 — Real CI Platform Decision (T3 Sponsor Meeting)

#### Objective
Formal T3 meeting via `meeting-orchestrator` to choose real CI platform replacing GitHub Actions (removed 2026-05-23). Produce ADR + execution contract.

#### Evidence
- CLAUDE.md "GitHub Actions was removed 2026-05-23 (operator decision: not the CI/CD platform). Quality gates run via `task quality:*`"
- M12 audit doc: "GitHub Actions removed 2026-05-23 sem scheduler replacement = HIGH gap"
- Multiple modules cite "T3 (CI run): N/A — gates re-homed em `task quality:*`"
- 56 pkg/quality/* gates + 132 Ginkgo specs + drill cycles all need CI orchestration

#### Files to edit
```
docs/decisions/2026-XX-XX-real-ci-platform-adr.md         — (NEW) ADR D1-D5 ratified
.claude/knowledge-base/meetings/MEET-2026-XX-XX-ci-platform.md — (NEW) Meeting record
.claude/governance-gate.local.md                          — (UPDATE) Append APPROVED status
```

#### Deep file dependency analysis
- ADR + MEET docs are governance artifacts; downstream are all Phase 2+ engineering tasks
- governance-gate.local.md is gitignored sponsor-only file

#### Deep Dives
Options to evaluate:
- **GitLab CI** — pros: integrated, mature; cons: requires GitLab SaaS or self-hosted
- **Jenkins** — pros: most flexible; cons: high operational overhead, security concerns
- **Drone CI** — pros: K8s-native, simple; cons: smaller community
- **BuildKite** — pros: hybrid model (cloud orchestrator + on-prem runners); cons: cost
- **Argo Events + Workflows + CronWorkflows** — pros: re-uses existing Argo infra; cons: less mature for CI than Argo Workflows for build pipelines
- **Self-hosted GitHub Actions runners** — pros: re-uses workflows (could restore); cons: undoes 2026-05-23 decision

Decision criteria (D4):
- Cost (hosted vs self-hosted)
- Operational overhead
- Multi-cluster awareness (must work with k3d local + DOKS prod-identical drill)
- Secret management integration (ESO compatibility)
- Cron equivalent (drill watchdog every 6h, soaks weekly, coverage report per-PR + weekly)

#### Tasks
1. Invoke `meeting-orchestrator` com Production Design Reviewer + Security & Blast Radius Reviewer + SRE Engineer + Sponsor
2. Evaluate 6 options against decision criteria
3. Produce decision record (ADR D1-D5)
4. Document scheduler equivalents (cron jobs that need migration)
5. Produce execution contract (timeline, owner, integration scope)

#### TDD
```
RED:     N/A — governance decision task
VERIFY:  ADR file exists com all 5 decisions (D1-D5) ratified
VERIFY:  Meeting record lists participants + positions + vetoes
VERIFY:  Governance gate APPROVED status committed
```

#### Acceptance Criteria
- [ ] T3 meeting executed com required specialists (PDR + Security + SRE + Sponsor)
- [ ] ADR file exists with all decisions ratified
- [ ] Meeting record committed em `.claude/knowledge-base/meetings/`
- [ ] CI platform choice explicitly named
- [ ] Cron job migration plan documented (drill watchdog · soaks · coverage · drift audit · cross-validation)
- [ ] Security review APPROVED (secret storage + runner isolation evaluated)
- [ ] SRE review APPROVED (operational readiness criteria defined)

#### DoD
- [ ] Sponsor APPROVED status em governance-gate.local.md
- [ ] ADR file committed
- [ ] Meeting record committed
- [ ] Tier: T3 (architecture, critical flow) — requires meeting

---

### T0.3 — Production Canary Stage 4 Procedure Documented

#### Objective
Document Stage 4 procedure (traffic split via Argo Rollouts) for cell-2 production canary. Infrastructure exists (Argo Rollouts shipped 2026-05-07); this task produces operational runbook.

#### Evidence
- CLAUDE.md "Path to maturity 4.0: (a) operator-run prod-identical drill GREEN ✓ + (b) 1 prod customer day-trial sem incidentes"
- M09 Multi-Cell audit: "production canary stages 4-5 pendentes"
- Argo Rollouts deployed em runtime cluster (per CLAUDE.md "Argo Rollouts Progressive Delivery COMPLETED 2026-05-07")
- Drill L2 LIVE PASS 2026-05-18 validates infra ready

#### Files to edit
```
docs/operations/production-canary-stage4-runbook.md   — (NEW) Stage 4 traffic split procedure
docs/operations/production-canary-stage5-runbook.md   — (NEW) Stage 5 customer trial gate
.claude/knowledge-base/concepts/operations/canary-progressive-rollout.md — (NEW) Concept page
```

#### Deep file dependency analysis
- Runbooks são operator-facing reference docs
- Concept page links to existing `concepts/progressive-delivery/` (Argo Rollouts)
- No code changes

#### Deep Dives
Stage 4 traffic split phases:
- Phase A: 1% canary (manual approval, watch 1h)
- Phase B: 10% canary (manual approval, watch 4h)
- Phase C: 50% canary (manual approval, watch 8h)
- Phase D: 100% (promote stable)

Rollback criteria:
- AnalysisTemplate failure (success_rate < 0.95 in 5min window)
- Manual operator abort
- LoopZero SLO burn rate alert fires

#### Tasks
1. Read existing Argo Rollouts config em runtime cluster
2. Document 4-phase progressive rollout procedure
3. Document rollback procedure
4. Document Stage 5 customer trial onboarding (separate runbook)
5. Cross-reference em `docs/architecture/multi-cell/`

#### TDD
```
RED:     N/A — documentation task
VERIFY:  Runbook contains 4 phases com explicit approval gates
VERIFY:  Rollback procedure documented com Loki query examples
VERIFY:  Stage 5 customer onboarding checklist exists
```

#### Acceptance Criteria
- [ ] Stage 4 runbook contém 4 phases (1%/10%/50%/100%) com approval gates
- [ ] Rollback procedure documented
- [ ] Stage 5 customer trial checklist (consent, scope, SLO definition, escalation)
- [ ] Concept page linked from CLAUDE.md "Path to 4.0" section

#### DoD
- [ ] Runbooks committed
- [ ] CLAUDE.md updated com link to runbooks
- [ ] Tier: T0 (documentation only)

---

### T0.4 — Customer Trial Onboarding Plan

#### Objective
Produce customer trial onboarding plan (Stage 5 prerequisite). Sponsor decision required — this task documents what needs to be decided.

#### Evidence
- CLAUDE.md "1 prod customer day-trial sem incidentes" é gate explícito for 4.0
- No customer onboarded as of 2026-05-26
- Pricing locked per MEET-2026-05-12 (Starter + Pro public; Team Early Access)

#### Files to edit
```
docs/operations/customer-trial-onboarding-plan.md       — (NEW) Operator + sponsor checklist
docs/operations/customer-trial-sla-template.md          — (NEW) SLA template para trial
docs/operations/customer-trial-success-criteria.md      — (NEW) Day-trial success definition
```

#### Deep file dependency analysis
- Customer trial onboarding requires sponsor + sales coordination
- This task delivers planning documents only

#### Deep Dives
Success criteria for "no incidents" day:
- Zero S0 (Blocker) incidents in 24h window
- Zero S1 (Severity 1) incidents in 24h window
- All customer-initiated deploys succeed within deploy_latency SLO (95% within 5min)
- All customer-initiated rollbacks succeed within rollback_success SLO (99.9%)
- Custom domain TLS provisioning successful (domain_provisioning SLO 99%)

#### Tasks
1. Document sponsor decision required (who is the customer, consent process)
2. Define SLA template (24h customer-facing commitment)
3. Define success criteria (zero S0/S1, all SLOs met)
4. Document escalation path (operator → SRE → sponsor)
5. Reference em CLAUDE.md "Path to 4.0"

#### TDD
```
RED:     N/A — documentation + sponsor decision task
VERIFY:  Plan exists com explicit "what needs to be decided" section
```

#### Acceptance Criteria
- [ ] Onboarding plan documents sponsor decisions needed
- [ ] SLA template exists (24h commitment)
- [ ] Success criteria explicitly defined (zero S0/S1, SLO conformance)
- [ ] Escalation path documented

#### DoD
- [ ] Plans committed
- [ ] Sponsor + operator review (this task delivers planning, sponsor commits separately)
- [ ] Tier: T2 (operations impact)

---

## Phase 1: Mechanical High-Value (Parallel Execution)

**Objective:** Lift 4 modules (M01 CLI · M02 API · M03 Operators · M08 Observability · M12 CI/CD partial) via mechanical fixes with established patterns. All 5 tasks can run in parallel.

### T1.1 — Correlation-ID Propagation in 39 Reconcilers

#### Objective
Inject correlation-ID via `correlation.WithID(ctx, cr.GetAnnotations()["loopzero.io/correlation-id"])` em 39 operator reconcilers + reconcile flows (ApplicationReconciler já implementa pattern at line 104). Closes M03 + M08 HIGH gap.

#### Evidence
- M03 Operators audit: "Correlation-ID propagation missing em 7/8 reconcilers"
- M08 Observability audit: "40 reconciler files across 4 operator binaries usam base logger from init() sem context enrichment. Apenas ApplicationReconciler (1/40) invoca correlation injection"
- Audit trail break em tenant/preview/clustercredential/exposedroute/gitrepositorybinding/platformbootstrap flows

#### Files to edit
```
operators/internal/tenant/tenant_controller.go           — Inject correlation_id em Reconcile entry
operators/internal/tenant/environment_controller.go      — Inject correlation_id
operators/internal/preview/preview_controller.go         — Inject correlation_id
operators/internal/platform/clustercredential_controller.go — Inject correlation_id
operators/internal/platform/gitrepositorybinding_controller.go — Inject correlation_id
operators/internal/platform/platformbootstrap_controller.go — Inject correlation_id
operators/internal/app/exposedroute_controller.go        — Inject correlation_id (if separate from ApplicationReconciler)
operators/internal/app/application_reaper.go             — Inject correlation_id em reaper loop
pkg/correlation/context.go                                — (Reference only) correlation.WithID pattern (CONFIRMED via grep 2026-05-26 — EC-1 edge-case-review)
```

#### Deep file dependency analysis
- Each reconciler entry point (Reconcile method) receives `ctx context.Context` and CR object
- Pattern (per ApplicationReconciler line 104):
  ```go
  ctx = correlation.WithID(ctx, cr.GetAnnotations()["loopzero.io/correlation-id"])
  ```
- Downstream: all logger calls within Reconcile inherit context; Loki indexes correlation_id label
- Risk: if annotation missing, correlation.WithID handles nil → no-op (safe default)

#### Deep Dives
- Annotation key: `"loopzero.io/correlation-id"` (canonical per pkg/correlation)
- Empty value handling: correlation.WithID skips injection if value empty
- Pattern is idempotent (safe to invoke múltiplas vezes per reconcile)
- Test pattern: assert log entry contains correlation_id field when annotation present

#### Tasks
1. Implement injection em each of 7 reconcilers (3-line addition)
2. Add envtest spec validating correlation_id in log output when annotation present
3. Add envtest spec validating no-op when annotation absent
4. Update `operators/cmd/*/main.go` ContextLogger invocation if needed
5. Document pattern em `docs/architecture/observability/operator-correlation.md`

#### TDD
```
RED:     test_tenant_reconciler_injects_correlation_id_from_annotation() — fails before injection
RED:     test_tenant_reconciler_noop_when_annotation_absent() — fails if implementation panics on nil
RED:     test_preview_reconciler_injects_correlation_id() — same pattern
RED:     test_clustercredential_reconciler_injects_correlation_id() — same pattern
(... 4 more tests for other reconcilers)
GREEN:   Add 3-line injection em each reconciler entry point
REFACTOR: Extract common helper to operatortest if pattern repeated
VERIFY:  go test -count=1 ./operators/internal/... GREEN
```

#### Acceptance Criteria
- [ ] All 7 reconcilers inject correlation_id from CR annotation
- [ ] envtest specs PASS (8 new tests: 7 reconcilers × {present, absent} - shared helper)
- [ ] Existing reconciler tests PASS (zero regression)
- [ ] Pattern documented em architecture doc
- [ ] Pass: complexity check (each addition < 5 lines added)
- [ ] Pass: lint check (zero golangci-lint warnings)

#### DoD
- [ ] All 7 reconcilers committed com tests
- [ ] envtest suite GREEN
- [ ] `task lint` zero warnings
- [ ] Tier: T1 (local implementation, single-component minor refactor)

---

### T1.2 — Charts Overlap Parity CI Gate

#### Objective
Implement CI gate validating `infra/helm/{loopzero-api,loopzero-cloud,admission-policies}` Chart.yaml versions never exceed `charts/{loopzero-api,loopzero-cloud,loopzero-admission-policies}` published versions. Closes M06 HIGH gap (3-way overlap drift).

#### Evidence
- M06 Helm audit: "loopzero-api 0.1.0/0.2.0 · loopzero-cloud 0.1.0/0.2.0 · loopzero-admission-policies 0.1.0/0.3.0. Sem CI gate de parity. Risk: Helmfile deploys bootstrap antigo enquanto published evolui breaking"
- 3 overlapping charts (loopzero-api, loopzero-cloud, loopzero-admission-policies) mantidos independentes — no automated sync

#### Files to edit
```
pkg/quality/helmoverlapparity/check.go                    — (NEW) Gate implementation
pkg/quality/helmoverlapparity/check_test.go               — (NEW) Test coverage
cmd/loopzero-ops/quality_subcmd.go                            — Wire new gate command "helm-overlap-parity"
taskfiles/quality.yml                                     — Add gate to quality:gates task
.claude/knowledge-base/concepts/operations/helm-chart-dual-system.md — (UPDATE) Document parity invariant
```

#### Deep file dependency analysis
- New gate package isolated em pkg/quality/ (no cross-deps)
- loopzero-ops binary needs rebuild after wire
- taskfiles/quality.yml integration ensures gate runs com `task quality:gates`

#### Deep Dives
Gate logic:
1. Read `infra/helm/loopzero-api/Chart.yaml` version
2. Read `charts/loopzero-api/Chart.yaml` version
3. Assert: bootstrap_version is semver ≤ published_version
4. Repeat for loopzero-cloud + loopzero-admission-policies
5. Exit 1 if any violation

Semver parsing via standard library + golang.org/x/mod/semver if needed.

#### Tasks
1. Implement `helmoverlapparity.Check()` reading both Chart.yaml files via yaml.Unmarshal
2. Implement semver comparison
3. Wire CLI command `loopzero-ops quality helm-overlap-parity --root DIR`
4. Add to `taskfiles/quality.yml` quality:gates task
5. Run gate manually to validate current state (drift expected!)
6. Update concept page documenting invariant

#### TDD
```
RED:     test_helm_overlap_parity_pass_when_versions_equal() — fails before implementation
RED:     test_helm_overlap_parity_pass_when_bootstrap_lower() — passes (allowed)
RED:     test_helm_overlap_parity_fail_when_bootstrap_higher() — fails before implementation
RED:     test_helm_overlap_parity_fail_when_chart_yaml_missing() — handles missing file
RED:     test_helm_overlap_parity_fail_when_version_invalid_semver() — handles malformed
GREEN:   Implement gate
REFACTOR: Extract semver compare helper
VERIFY:  go test -count=1 ./pkg/quality/helmoverlapparity/... GREEN
VERIFY:  loopzero-ops quality helm-overlap-parity --root . exits 0 OR documents current drift
```

#### Acceptance Criteria
- [ ] Gate detects bootstrap_version > published_version (FAIL)
- [ ] Gate passes when versions equal OR bootstrap ≤ published
- [ ] Wired em `task quality:gates`
- [ ] 5 unit tests PASS
- [ ] Documents current drift (may FAIL until charts realigned — separate fix)
- [ ] Pass: complexity check (< 10 cyclomatic)

#### DoD
- [ ] Gate committed
- [ ] `task quality:gates` includes new gate
- [ ] Tier: T1 (single gate addition)
- [ ] Cross-reference: documents drift; chart realignment is separate T1 task

---

### T1.3 — withEnvironmentLock Visibility Audit (Documentation + Static Analysis)

#### Objective
Grep all mutating API routes; document onde `withEnvironmentLock` é applied (handler closure vs middleware). Produce findings + recommendations. Closes M02 HIGH gap (opacity).

#### Evidence
- M02 API audit: "withEnvironmentLock opacity em routes — Only 2 grep matches em services/release_activator.go + services/domain_provisioner.go. Mutating routes (POST builds/secrets/teams) sem visibility de lock em middleware chain"
- Risk: race condition se lock outside critical section

#### Files to edit
```
docs/architecture/api/withenvironmentlock-coverage.md     — (NEW) Audit findings doc
api/internal/services/withenvironmentlock_audit_test.go   — (NEW) Static test enumerating lock call sites
.claude/knowledge-base/concepts/api/lock-discipline.md    — (NEW) Concept page
```

#### Deep file dependency analysis
- Audit doc é reference for future PR reviewers
- Static test fails se new mutating route added without lock
- Concept page anchors discussion

#### Deep Dives
Audit methodology:
1. Grep `r\.Post\|r\.Put\|r\.Delete\|r\.Patch` em api/internal/routes
2. For each match, trace handler → service → identify lock acquisition site
3. Tag each route as: LOCKED-HANDLER, LOCKED-SERVICE, NO-LOCK-FOUND
4. NO-LOCK-FOUND = HIGH severity finding

Static test: Go AST walker counts `withEnvironmentLock` invocations + mutating route count; asserts ratio.

#### Tasks
1. Grep all mutating routes em api/internal/routes
2. Trace each to lock acquisition site
3. Document findings com classification
4. Implement static test asserting lock coverage
5. Add new lock acquisitions where NO-LOCK-FOUND identified (separate small commits)
6. Update concept page

#### TDD
```
RED:     test_all_mutating_routes_have_lock_in_call_chain() — enumerates routes + asserts lock
RED:     test_concurrent_build_check_inside_lock() — race-detector enabled
GREEN:   Audit + document existing; add missing locks if any
VERIFY:  go test -race -count=1 ./api/internal/... GREEN
```

#### Acceptance Criteria
- [ ] Audit doc lists every mutating route + lock acquisition site
- [ ] Static test enumerates routes (FAILS quando new mutating route added without lock)
- [ ] All NO-LOCK-FOUND findings fixed OR explicitly documented as exempt
- [ ] Concept page links from CLAUDE.md Rule 6

#### DoD
- [ ] Audit doc committed
- [ ] Static test committed + GREEN
- [ ] Tier: T1 (documentation + tests; no behavior change OR T2 if locks added — escalate via Change Governor)

---

### T1.4 — CLAUDE.md Numerical Drift Cleanup

#### Objective
Sync CLAUDE.md numerical claims com 2026-05-26 audit measurements. Fixes documentation integrity gap detected em multiple audit docs.

#### Evidence
- M01 CLI: "65 commands" claimed → 66 registered actual
- M02 API: "35 subpackages" claimed → 33 actual
- M06 Helm: "34 dirs em infra/helm" claimed → 35 actual
- M08 Observability: "109+8 cell metrics = 117" claimed → 110 actual
- M12 CI/CD: "48 workflows" claimed → 0 actual (post-removal)

#### Files to edit
```
CLAUDE.md                                                  — Update numerical claims em multiple sections
docs/architecture/index.md                                 — Sync any drifts
```

#### Deep file dependency analysis
- CLAUDE.md é root source-of-truth doc; updates here propagate to all skills that reference it
- No code dependency

#### Deep Dives
Updates needed:
- CLI section: "65 commands" → "40 command files / 66 registered commands (1 unwired: doctor — T2.6)"
- API section: "35 subpacotes" → "33 subpackages"
- Helm section: "34 bootstrap charts" → "35 distinct bootstrap charts (36 subdirs com 1 disambig)"
- Observability section: "109 loopzero_* metrics + 8 cell" → "110 unique loopzero_* metrics (85 API + 15 operators + 10 cloud)"
- CI/CD section: "48 GitHub workflows" → "0 GitHub workflows (removed 2026-05-23); quality gates via `task quality:*`"

#### Tasks
1. Apply 5 numerical updates em CLAUDE.md
2. Add reference to 2026-05-26 audit em "Maturity" section
3. Update "Path to maturity 4.0" com current state (3.62)
4. Cross-reference T0.2 (CI platform decision) em CI/CD mention

#### TDD
```
RED:     N/A — documentation task
VERIFY:  grep "65 commands\|35 subpacotes\|34 charts\|109 metrics\|48 workflows" CLAUDE.md returns 0 matches
VERIFY:  Maturity section shows 3.62 (linked to 2026-05-26 audit)
```

#### Acceptance Criteria
- [ ] 5 numerical drifts corrected
- [ ] Audit 2026-05-26 referenced em CLAUDE.md
- [ ] No emojis introduced (CLAUDE.md style)
- [ ] No breaking changes em downstream skills

#### DoD
- [ ] CLAUDE.md committed
- [ ] Tier: T0 (documentation)

---

### T1.5 — Architecture Docs Post-Plan (PROPOSED) for 5 New Domains

#### Objective
Generate `docs/architecture/{observability,build-pipeline,ci-cd,infra,auth}/diff/` PROPOSED state docs reflecting Phase 1-4 changes. Per `/to-plan` skill mandatory step 5.

#### Evidence
Mandatory step per skill definition + ADR D2 this plan.

#### Files to edit
```
docs/architecture/observability/diff/proposed.md          — (NEW) Post-T1.1 correlation propagation
docs/architecture/observability/diff/loki-federation.md   — (NEW) Post-T4.6 cross-cluster
docs/architecture/build-pipeline/diff/keyless-active.md   — (NEW) Post-T2.2 keyless infra active
docs/architecture/ci-cd/diff/real-ci-active.md            — (NEW) Post-T0.2 real CI platform
docs/architecture/infra/diff/cosign-digest-enforcement.md — (NEW) Post-T2.3 digest pinning
docs/architecture/auth/diff/stripe-portal-sdk.md          — (NEW) Post-T2.7 SDK integration
```

#### Tasks
1. Run `/architecture-docs` Mode 3 (Post-Plan PROPOSED) after Phase 1-2 tasks scoped
2. Each doc includes `<!-- PROPOSED STATE -->` header
3. Inform operator: "Documentação arquitetural proposta gerada em docs/architecture/{domain}/diff/. Após implementação aprovada, perguntarei se deseja promover."

#### TDD
```
RED:     N/A — documentation task
VERIFY:  6 diff docs exist com PROPOSED header
```

#### Acceptance Criteria
- [ ] 6 PROPOSED docs em docs/architecture/{domain}/diff/
- [ ] Each contém PROPOSED STATE header
- [ ] Operator informed para post-implementation promotion decision

#### DoD
- [ ] PROPOSED docs committed
- [ ] Tier: T0

---

## Phase 2: Architectural Medium (Mostly Parallel)

**Objective:** Lift 6 modules (M01 CLI · M02 API · M05 IaC · M06 Helm · M07 Build · M10 Auth) via medium-effort architectural changes. 7 tasks; T2.5 depends on T2.3.

### T2.1 — pulumi-platform Component Tests

#### Objective
Add component-level unit tests para CertManager, Traefik, ArgoCD, Gitea services em pulumi-platform (currently 1 test / 4,655 LOC). Closes M05 MEDIUM gap.

#### Evidence
- M05 IaC audit: "pulumi-platform test coverage = 1 source test (schema.test.ts) vs 4,655 LOC. Risk: integration-only coverage; drift undetected até Helm sync failure"

#### Files to edit
```
infra/pulumi-platform/src/__tests__/cert-manager.test.ts  — (NEW) 3-5 component tests
infra/pulumi-platform/src/__tests__/traefik.test.ts       — (NEW) 3-5 tests
infra/pulumi-platform/src/__tests__/argocd.test.ts        — (NEW) 3-5 tests
infra/pulumi-platform/src/__tests__/gitea.test.ts         — (NEW) 3-5 tests
infra/pulumi-platform/src/__tests__/loki.test.ts          — (NEW) 3-5 tests (already has 1 schema test)
infra/pulumi-platform/src/__tests__/prometheus.test.ts    — (NEW) 3-5 tests
infra/pulumi-platform/package.json                         — Verify vitest config
```

#### Deep file dependency analysis
- New test files isolated em __tests__/; no production code change
- vitest already configured
- Tests use Pulumi automation API mocks (NOT full provisioning)

#### Deep Dives
Per-service test scope:
- Input contract: required config keys present + types valid
- Output contract: declared exports match consumer expectations (StackReference pattern)
- Dependency wiring: explicit `dependsOn` chains correct
- Helm values: schema-conforming defaults

#### Tasks
1. Implement cert-manager component tests (3-5 cases)
2. Implement Traefik tests
3. Implement ArgoCD tests
4. Implement Gitea tests (includes bootstrap job validation)
5. Implement Loki tests (extend existing schema test)
6. Implement Prometheus tests
7. Run full suite + assert coverage ≥ 50% (lifted from ~0.02%)

#### TDD
```
RED:     test_cert_manager_emits_cluster_issuer() — fails before tests written
RED:     test_traefik_uses_cert_manager_for_tls() — depends on cert-manager component
RED:     test_argocd_dependson_cert_manager() — validates dependsOn wiring
(... 30+ more tests)
GREEN:   Tests pass against existing implementation (component code unchanged)
VERIFY:  cd infra/pulumi-platform && npx vitest run
VERIFY:  Coverage report shows ≥ 50% (vs 0.02% baseline)
```

#### Acceptance Criteria
- [ ] 18-30 new tests (3-5 per 6 services)
- [ ] Coverage ≥ 50%
- [ ] Tests run em < 30s (vitest unit tests)
- [ ] Each service has at minimum: input validation + dependsOn validation + output assertion

#### DoD
- [ ] All test files committed
- [ ] `cd infra/pulumi-platform && npx vitest run` PASS
- [ ] Tier: T1 (test addition only)

---

### T2.2 — Cosign Keyless Infrastructure (Pulumi Component)

#### Objective
Provision Fulcio + Rekor instances via novo Pulumi component em pulumi-platform. Activates ADR 2026-05-12 Phase A (T0+30d). Closes M07 HIGH gap.

#### Evidence
- M07 Build Pipeline audit: "Cosign keyless infrastructure NÃO provisioned. InfraParams.CosignKeyless default=false. Fulcio + Rekor NOT yet deployed em staging. ADR 2026-05-12 commits hard T0+90d deadline; sem provisioning started"

#### Files to edit
```
infra/pulumi-platform/src/services/fulcio.ts              — (NEW) Fulcio ComponentResource
infra/pulumi-platform/src/services/rekor.ts               — (NEW) Rekor ComponentResource
infra/pulumi-platform/src/services/__tests__/fulcio.test.ts — (NEW) Fulcio tests
infra/pulumi-platform/src/services/__tests__/rekor.test.ts — (NEW) Rekor tests
infra/pulumi-platform/src/index.ts                        — Wire Fulcio + Rekor após Traefik
infra/pulumi-platform/src/config.ts                       — Add Fulcio/Rekor config
infra/pulumi-platform/Pulumi.dev.yaml                     — Add Fulcio/Rekor settings
api/internal/argo/workflow_builder.go                     — Set InfraParams.CosignKeyless config wiring
api/internal/argo/cosign.go                                — Verify keyless template ready
```

#### Deep file dependency analysis
- New Fulcio + Rekor components added to pulumi-platform DAG após cert-manager + Traefik (require TLS)
- api/internal/argo/workflow_builder.go reads infra config to set CosignKeyless flag
- cosign.go already has cosignSignTemplateKeyless implementation (line 223-326)
- Workflow templates emit COSIGN_EXPERIMENTAL=1 + SA OIDC token mount when flag enabled

#### Deep Dives
Provisioning approach:
- Fulcio Helm chart `oci://ghcr.io/sigstore/charts/fulcio` (pin version no Pulumi component; sigstore official, NOT chainguard fork) — EC-2 edge-case-review
- Rekor Helm chart `oci://ghcr.io/sigstore/charts/rekor` (pin version no Pulumi component)
- Both installed via Pulumi @pulumi/kubernetes/helm
- **TLS issuer per environment (EC-3 edge-case-review):**
  - Local k3d: `loopzero-local-ca-issuer` (self-signed, letsencrypt-prod nao funciona em k3d sem public DNS)
  - Drill: `letsencrypt-staging` ClusterIssuer (existente)
  - Prod: `letsencrypt-prod` ClusterIssuer (existente)
  - Issuer name lido de Pulumi config key `cosign.tlsIssuer` per stack
- ConfigMap with Fulcio + Rekor public URLs (operators read)
- SA OIDC token projection (per cosign.go:236)
- TLS via existing cert-manager ClusterIssuer

Per ADR 2026-05-12 timeline:
- Phase A (Infra): T0+30d — this task
- Phase B (Staging): T0+30→T0+45d — separate task post-soak
- Phase C (Production): T0+75→T0+90d — operator-led

#### Tasks
1. Implement Fulcio ComponentResource (~80 LOC)
2. Implement Rekor ComponentResource (~60 LOC)
3. Wire em pulumi-platform/src/index.ts (após Traefik)
4. Add config keys em Pulumi.dev.yaml + Pulumi.drill.yaml
5. Verify api/internal/argo/cosign.go keyless template ready
6. Wire InfraParams.CosignKeyless config read
7. Component tests (4-6 per component)
8. Manual `pulumi preview --stack drill` validates resources

#### TDD
```
RED:     test_fulcio_component_emits_deployment() — fails before implementation
RED:     test_fulcio_uses_cert_manager_tls() — dependsOn validation
RED:     test_rekor_component_emits_deployment()
RED:     test_rekor_dependson_fulcio() — TLS chain
RED:     test_workflow_builder_passes_cosign_keyless_when_enabled() — Go test for API integration
GREEN:   Implement components + wire em workflow_builder
VERIFY:  cd infra/pulumi-platform && npx vitest run
VERIFY:  go test -count=1 ./api/internal/argo/... GREEN
```

#### Acceptance Criteria
- [ ] Fulcio + Rekor components implemented + tested
- [ ] Wired em pulumi-platform DAG após cert-manager + Traefik
- [ ] CosignKeyless config flag wired API ↔ infra
- [ ] Component tests PASS (8-12 new)
- [ ] `pulumi preview --stack drill` shows new resources
- [ ] Operator HARD STOP: `pulumi up --stack drill` to LIVE-validate (manual step)

#### DoD
- [ ] Components committed
- [ ] Tests GREEN
- [ ] Tier: T3 (architecture, critical flow — supply chain) — requires meeting
- [ ] Sponsor sign-off em governance-gate.local.md

---

### T2.3 — Cosign Digest Enforcement em Charts

#### Objective
Extend published charts to embed image digest references (`<registry>@sha256:<digest>`) instead of mutable tags. Add ClusterImagePolicy enforcing digest. Closes M06 + M07 MEDIUM gap.

#### Evidence
- M06 Helm audit: "Cosign digest enforcement em charts NÃO ativo — charts referenciam `${LOOPZERO_REGISTRY}:${TAG}` (mutable tag), não digest. Vulnerable tag-reuse attacks"
- M07 Build Pipeline audit: "SBOM-to-image attestation linking missing"

#### Files to edit
```
charts/loopzero-api/templates/deployment.yaml                  — Use image digest ref
charts/loopzero-cloud/templates/deployment.yaml                — Use image digest ref
charts/loopzero-app-operator/templates/deployment.yaml         — Same
charts/loopzero-platform-operator/templates/deployment.yaml    — Same
charts/loopzero-preview-operator/templates/deployment.yaml     — Same
charts/loopzero-tenant-operator/templates/deployment.yaml      — Same
charts/loopzero-dashboard/templates/deployment.yaml            — Same
charts/loopzero-admission-policies/templates/cluster-image-policy-digest.yaml — (NEW) Enforce digest
infra/helm/policy-controller/values.yaml                   — Enable digest enforcement
api/internal/argo/buildkit.go                              — Emit digest output for chart consumption
```

#### Deep file dependency analysis
- Each chart template change: image field from `{{ .Values.image }}:{{ .Values.tag }}` to `{{ .Values.image }}@{{ .Values.digest }}`
- values.yaml + values.schema.json need `digest` field added (required when tag absent)
- Build pipeline already emits digest (cosign.go line 95: extract digest from metadata-{app}.json)
- ClusterImagePolicy adds digest matching rule

#### Deep Dives
Migration approach:
1. Add `digest` field opcional em values.schema.json (validation: regex `^sha256:[a-f0-9]{64}$`)
2. **values.schema.json constraint (EC-4 edge-case-review):**
   ```json
   "oneOf": [{"required": ["tag"]}, {"required": ["digest"]}]
   ```
   Sem oneOf, tag+digest ambos vazios renderiza `image: ""` → CrashLoopBackOff silent.
3. Template logic: prefer digest over tag if both present
4. Build pipeline reads digest from cosign step output + injects em CD step
5. **ClusterImagePolicy modo PROGRESSIVE (EC-5 edge-case-review):**
   - Phase 1 (este task): `mode: warn` + `namespaceSelector: matchLabels loopzero.io/tenant=true` (escopo tenants only)
   - Phase 2 (separate task pós-soak ≥ 7 dias): flip para `mode: enforce` E expand namespaceSelector para incluir loopzero-system
   - Sem progressive rollout, enforce-mode rejeita admission-policies/monitoring/operator pods pre-existing usando `:latest`/`:vN.Y.Z` → cluster wide CrashLoop.

Backward compatibility: pre-release, no constraint — break tag references after migration.

#### Tasks
1. Update 7 chart deployment templates
2. Update values.schema.json + values.yaml (add digest field)
3. Implement ClusterImagePolicy digest constraint
4. Update buildkit.go to output digest for downstream consumption
5. Render tests validating digest field present
6. Manual integration test: build app → verify chart deploys com digest ref

#### TDD
```
RED:     test_loopzero_api_template_uses_digest_when_present() — fails before template change
RED:     test_loopzero_api_template_falls_back_to_tag() — backward compat
RED:     test_cluster_image_policy_rejects_tag_only_references() — admission test
RED:     test_buildkit_emits_digest_output_parameter() — output param
GREEN:   Update templates + ClusterImagePolicy + buildkit
VERIFY:  task helm:test PASS (24 render packages)
VERIFY:  go test -count=1 ./api/internal/argo/... GREEN
```

#### Acceptance Criteria
- [ ] 7 charts emit digest reference
- [ ] values.schema.json includes digest field validation
- [ ] ClusterImagePolicy enforces digest
- [ ] Build pipeline emits digest output
- [ ] Render tests PASS
- [ ] LIVE: built image deploys via chart com digest (operator manual validation)

#### DoD
- [ ] All chart changes committed
- [ ] Render tests GREEN
- [ ] Tier: T2 (Helm modifications + admission policy)
- [ ] Change Governor meeting required

---

### T2.4 — 30 errors.New() Residuais Triage + Conversion

#### Objective
Triage 30 remaining `errors.New()` calls em api/internal/. Classify each as: (a) internal-only (acceptable), (b) user-facing (must convert to LoopZeroError). Convert user-facing. Closes M02 MEDIUM gap.

#### Evidence
- M02 API audit: "errors.New() 30 instances residuais. Down de 72 baseline. Top: services/sync_callback_activator.go:74 + middleware/auth_failure_audit.go:143 + 27 outras em argocd/k8s/manifests"

#### Files to edit
```
api/internal/services/sync_callback_activator.go:74        — Convert if user-facing
api/internal/middleware/auth_failure_audit.go:143          — Convert if user-facing
(... 27 more files identified during triage)
pkg/errors/errors.go                                        — Add new LoopZeroError sentinels if needed
pkg/errors/http/mapping.go                                  — Register new codes
```

#### Deep file dependency analysis
- Each errors.New triage requires understanding caller chain
- LoopZeroError additions need HTTP status code registration (pkg/errors/http/mapping.go)
- Test coverage: each new LoopZeroError needs unit test em pkg/errors/errors_test.go

#### Deep Dives
Triage criteria:
- **User-facing:** Error reaches HTTP response OR CLI output → MUST be LoopZeroError
- **Internal-only:** Error stays em service-to-service OR DB query rollback → acceptable as errors.New
- **Sentinel pattern:** If error compared via errors.Is → MUST be LoopZeroError sentinel for stable identity

Conversion pattern:
```go
// Before
return errors.New("sync operation in progress")

// After
return loopzeroerrors.ErrSyncOperationInProgress
// Where ErrSyncOperationInProgress is defined em pkg/errors/errors.go
```

#### Tasks
1. Triage 30 errors.New calls (file:line list + classification)
2. Define new LoopZeroError sentinels para user-facing cases (~10-15 new)
3. Register HTTP codes em pkg/errors/http/mapping.go
4. Convert call sites
5. Update unit tests para new sentinels
6. Document triage results em audit doc

#### TDD
```
RED:     test_sync_callback_activator_returns_typed_error() — fails before conversion
RED:     test_auth_failure_audit_returns_typed_error() — fails before conversion
(... per converted call site)
GREEN:   Convert + register
VERIFY:  go test -count=1 ./api/... GREEN
VERIFY:  grep -c "errors.New(" api/internal/ | should be 0 OR justified
```

#### Acceptance Criteria
- [ ] Triage doc lists 30 errors.New calls com classification
- [ ] All user-facing converted to LoopZeroError
- [ ] Internal-only acceptable (documented)
- [ ] New LoopZeroError sentinels registered em HTTP mapping
- [ ] Zero regressions em existing tests

#### DoD
- [ ] All conversions committed
- [ ] Triage doc em .claude/knowledge-base/reviews/
- [ ] Tier: T1 (per-file refactor; T2 se >10 user-facing)

---

### T2.5 — Multi-K8s-Version Testing (Chart-Testing Matrix)

#### Objective
Integrate chart-testing (ct) com matrix testing across K8s 1.30, 1.31, 1.32 versions. Closes M06 MEDIUM gap.

#### Evidence
- M06 Helm audit: "Multi-K8s-version testing missing. Sem chart-testing matrix integration. 16 grep hits v1beta1/v1alpha API versions (deprecation risk K8s 1.30+)"

#### Files to edit
```
taskfiles/helm.yml                                         — Add ct-lint + ct-install tasks
infra/tests/ct.yaml                                        — (NEW) chart-testing config
.chart-testing/ct.yaml                                     — (NEW) Repo config
docs/operations/helm-chart-testing-runbook.md              — (NEW) Operator runbook
```

#### Deep file dependency analysis
- chart-testing binary needs install (Go binary, can be pinned via go install)
- Multi-version testing requires kind clusters (separate from k3d) OR mock K8s API
- CI integration depends on T0.2 (real CI platform decision)

#### Deep Dives
chart-testing scope:
- `ct lint --target-branch main` — validates Chart.yaml + values.yaml + templates
- `ct install --target-branch main --kubernetes-version 1.32.0` — installs em kind cluster
- Matrix: 3 versions (1.30, 1.31, 1.32) × all 13 published charts

This task delivers infrastructure; full matrix execution depends on T0.2 + Phase 5 re-audit.

#### Tasks
1. Install chart-testing binary (go install version pinned)
2. Configure .chart-testing/ct.yaml
3. Add taskfiles/helm.yml tasks (ct-lint + ct-install)
4. Document operator runbook
5. Local validation: `task helm:ct-lint` PASS
6. Document matrix integration deferred to T0.2 platform choice

#### TDD
```
RED:     N/A — tooling integration, not application code
VERIFY:  task helm:ct-lint exits 0
VERIFY:  task helm:ct-install --kubernetes-version 1.32.0 PASS (single version local)
```

#### Acceptance Criteria
- [ ] chart-testing binary installed via go install
- [ ] Configuration files committed
- [ ] taskfiles/helm.yml tasks added
- [ ] Operator runbook documented
- [ ] Local single-version PASS validated

#### DoD
- [ ] Configs committed
- [ ] Local validation PASS
- [ ] Tier: T1 (tooling addition)
- [ ] Full matrix integration deferred to T0.2 platform choice

---

### T2.6 — CLI: Wire `doctor` Command + process.exit Standardization

#### Objective
(a) Wire `doctor` command em CLI program (276 LOC + tests existem mas unwired); (b) Standardize `process.exit` vs `process.exitCode` em register/*.ts. Closes M01 LOW gaps.

#### Evidence
- M01 CLI audit: "`doctor` command unwired — fully implemented DoctorCommand, exported commands/doctor.ts:46, NOT imported em qualquer register/*.ts"
- M01 CLI audit: "`process.exit` vs `process.exitCode` inconsistência — register/platform.ts usa process.exit() direto; outros usam process.exitCode"

#### Files to edit
```
cli/src/register/diagnostics.ts                            — (NEW) Register doctor command
cli/src/index.ts                                           — Add registerDiagnosticsCommands()
cli/src/register/platform.ts                               — Refactor process.exit → process.exitCode
cli/src/register/deploy.ts                                  — Verify already uses exitCode (no change)
cli/tests/integration/doctor-integration.test.ts           — (NEW) Integration test doctor command
```

#### Deep file dependency analysis
- New register/diagnostics.ts follows existing pattern (register/auth.ts, register/deploy.ts, etc.)
- index.ts already invokes 11 register functions; adding 12th is trivial
- platform.ts has 7 process.exit calls (lines 50, 63, 73, 86, 102, 111, 119)

#### Deep Dives
doctor command scope (per commands/doctor.ts):
- "T1.11 — `loopzero doctor` end-user health check"
- Tests existem em tests/unit/doctor.test.ts
- Fully implemented DoctorCommand class

process.exit standardization rationale:
- process.exitCode allows async cleanup before exit
- process.exit() aborts immediately (may skip cleanup)
- Per-register consistency improves debuggability

#### Tasks
1. Create register/diagnostics.ts following auth.ts pattern
2. Wire em index.ts
3. Refactor platform.ts 7 process.exit calls
4. Integration test invoking `loopzero doctor`
5. Verify exit code contract test still passes (cli/tests/integration/exit-code-contract.test.ts)

#### TDD
```
RED:     test_doctor_command_registered() — fails before wire
RED:     test_doctor_command_exits_0_on_healthy() — fails before integration
RED:     test_platform_uses_process_exitCode_not_exit() — fails before refactor
GREEN:   Wire doctor + refactor platform.ts
REFACTOR: Extract pattern if helpful
VERIFY:  cd cli && npm test
VERIFY:  cli/tests/integration/exit-code-contract.test.ts PASS
```

#### Acceptance Criteria
- [ ] `loopzero doctor --help` shows command help
- [ ] Integration test PASS
- [ ] platform.ts uses process.exitCode consistently
- [ ] Exit code contract test PASS (no regression)

#### DoD
- [ ] Files committed
- [ ] `cd cli && npm test` PASS
- [ ] Tier: T1 (CLI command wire + minor refactor)

---

### T2.7 — Auth: Stripe Portal SDK Integration

#### Objective
Replace `POST /api/v1/billing/portal` placeholder com actual Stripe CreatePortalSession SDK call. Closes M10 MEDIUM gap (Sprint 11 deferral).

#### Evidence
- M10 Auth/Billing audit: "Stripe Portal SDK em Sprint 11 placeholder — POST /api/v1/billing/portal retorna hardcoded Stripe dashboard URL; actual CreatePortalSession() SDK call commented com 'Sprint 11' (billing.go:180-215). Users veem usage mas não acessam Customer Portal"

#### Files to edit
```
api/internal/routes/billing.go:180-215                     — Replace placeholder com SDK call
cloud/internal/billing/stripe.go                            — Add CreatePortalSession method
cloud/internal/billing/webhook.go                           — Add idempotency key para portal session (mention M10 gap)
api/internal/routes/billing_test.go                         — Add test coverage
```

#### Deep file dependency analysis
- Existing Stripe client em cloud/internal/billing/stripe.go (185 LOC) — extend
- POST /api/v1/billing/portal handler currently returns hardcoded URL
- Idempotency: customer-id + hourBucket prevents double-click within 1h

#### Deep Dives
Stripe SDK call:
```go
sess, err := stripeClient.BillingPortalSession.New(&stripe.BillingPortalSessionParams{
    Customer:  stripe.String(stripeCustomerID),
    ReturnURL: stripe.String(returnURL),
})
```

Error handling:
- No stripe_customer_id (free tier) → 400 NO_STRIPE_CUSTOMER
- Stripe API error → 5xx com retry-after
- Idempotency key collision → 422 IDEMPOTENCY_MISMATCH

#### Tasks
1. Add CreatePortalSession method em cloud/internal/billing/stripe.go
2. Replace placeholder em billing.go:180-215
3. Add idempotency key (customer-id-portal-{hourBucket})
4. Unit tests para new method
5. Integration test em test mode (Stripe test key)

#### TDD
```
RED:     test_billing_portal_returns_stripe_url() — fails before SDK integration
RED:     test_billing_portal_returns_400_when_no_customer() — error path
RED:     test_billing_portal_idempotent_within_hour() — repeated call same URL
GREEN:   Implement SDK call
VERIFY:  go test -count=1 ./api/internal/routes/... GREEN
VERIFY:  Stripe test mode integration PASS
```

#### Acceptance Criteria
- [ ] POST /api/v1/billing/portal returns valid Stripe Customer Portal URL
- [ ] Error paths (no customer, Stripe API error) return appropriate HTTP codes
- [ ] Idempotency key prevents double-charge
- [ ] Integration test PASS (Stripe test mode)

#### DoD
- [ ] Files committed
- [ ] Tests GREEN
- [ ] Tier: T2 (API contract change, billing flow)
- [ ] Sponsor review for Stripe production key usage

---

## Phase 3: Operational Runbooks (Sequential)

**Objective:** Document operational procedures missing — required for production readiness. 4 runbooks.

### T3.1 — Cell Drain Operational Runbook

#### Objective
Document procedure for transitioning cell to draining → maintenance → retired states. Closes M09 MEDIUM gap.

#### Evidence
- M09 Multi-Cell audit: "Cell drain operational runbook ausente — cells.status field supporta draining/maintenance/retired mas sem procedure documentada (evacuation SLO, rollback)"

#### Files to edit
```
docs/operations/cell-drain-runbook.md                     — (NEW) Operator procedure
.claude/knowledge-base/concepts/multi-cell/cell-lifecycle.md — (NEW) Concept page
docs/architecture/multi-cell/cell-drain-flow.md           — (NEW) Sequence diagram
```

#### Deep Dives
Drain procedure:
1. `loopzero admin cells update --cell cell-1 --status draining` — DB UPDATE
2. CellPinner rejects new pins to cell-1 (existing code)
3. Operator manually pins existing tenants: `loopzero admin tenants pin --tenant X --to cell-2`
4. Monitor `loopzero_cell_tenant_count{cell="cell-1"}` until 0
5. SLO: evacuation completes within N days (operator choice based on plan)
6. Transition to maintenance OR retired

Rollback procedure:
- `loopzero admin cells update --cell cell-1 --status active` (within 24h)
- New tenants resume binning to cell-1
- No data movement needed (sticky placement)

#### Tasks
1. Document drain procedure
2. Document rollback procedure
3. Define evacuation SLO (operator-recommended: 7 days)
4. Document monitoring queries (Loki + Prometheus)
5. Create concept page + sequence diagram

#### TDD
```
RED:     N/A — documentation task
VERIFY:  Runbook contains evacuation SLO, rollback procedure, monitoring queries
```

#### Acceptance Criteria
- [ ] Runbook documents drain → maintenance → retired flow
- [ ] Rollback procedure documented
- [ ] Evacuation SLO defined
- [ ] Monitoring queries provided
- [ ] Concept page + sequence diagram em knowledge-base

#### DoD
- [ ] Runbook committed
- [ ] Concept page committed
- [ ] Tier: T2 (operations impact)

---

### T3.2 — Session Token Rotation Runbook

#### Objective
Document session token rotation policy + refresh procedure. Closes M10 MEDIUM gap.

#### Evidence
- M10 Auth/Billing audit: "Session token rotation procedure UNDOCUMENTED. Code tem refresh_token support (HydraClient.RefreshToken) + CLI pode chamar DeviceToken repeat + API token cache TTL 5min. GAP: Sem docs respondendo: when CLI triggers refresh? proactively at N% lifetime? server-side ou client-driven? Redis session TTL e GC policy?"

#### Files to edit
```
docs/operations/session-token-rotation-runbook.md         — (NEW) Operator runbook
.claude/knowledge-base/concepts/auth/session-token-rotation.md — (NEW) Concept page
docs/architecture/auth/diff/token-rotation-flow.md        — (UPDATE) Add rotation to AS-IS
```

#### Deep Dives
Rotation policy decisions:
- CLI refresh trigger: proactive at 80% of token lifetime (default Hydra 1h TTL → refresh at 48min)
- Server-side rotation: NO (client-driven; preserves stateless API)
- Redis session storage TTL: 24h (per CLAUDE.md MEET-2026-04-09-043)
- GC policy: ttl-based eviction; orphaned sessions cleared on user logout

Refresh procedure:
1. CLI detects expiry approaching (configurable threshold)
2. CLI invokes HydraClient.RefreshToken(refresh_token)
3. Hydra returns new access_token + new refresh_token
4. CLI persists em ~/.loopzero/config.json (atomic write)
5. Subsequent requests use new access_token

Rollback: if refresh fails, fall back to device flow (loopzero login)

#### Tasks
1. Document rotation policy
2. Document refresh procedure
3. Document Redis TTL + GC
4. Document fallback (loopzero login)
5. Create concept page

#### TDD
```
RED:     N/A — documentation task
VERIFY:  Runbook answers 4 questions: when trigger, server vs client, Redis TTL, fallback
```

#### Acceptance Criteria
- [ ] Runbook documents 4 questions explicitly
- [ ] Rotation policy explicit
- [ ] Concept page links architecture diagram

#### DoD
- [ ] Runbook committed
- [ ] Tier: T0 (documentation)

---

### T3.3 — Signing Key Rotation Procedure + check-cutover-deadline Script

#### Objective
(a) Document signing key rotation procedure; (b) Implement `check-cosign-cutover-deadline.sh` script (referenced em CLAUDE.md mas missing). Closes M07 HIGH gap.

#### Evidence
- M07 Build Pipeline audit: "Signing key rotation enforcement — ADR Phase C.5 + cosign-key-rotation.md runbook documented mas enforcement gate (cutover-deadline script) NÃO wired. `check-cosign-cutover-deadline.sh` script not found em repo"

#### Files to edit
```
infra/scripts/security/check-cosign-cutover-deadline.sh   — (NEW) Cutover deadline gate
.claude/rules/shell-scripts-allowlist.md                  — Register new script
taskfiles/quality.yml                                     — Wire script
docs/operations/cosign-signing-key-rotation-runbook.md    — (NEW) Operator runbook
.claude/knowledge-base/concepts/supply-chain/cosign-key-rotation.md — (EXISTS — extend)
```

#### Deep Dives
Script logic:
- Read ADR 2026-05-12 hard deadline (T0 + 90 days)
- **Read current cosign mode (EC-6 edge-case-review):**
  - ConfigMap: namespace=`loopzero-system`, name=`loopzero-cosign-config`, key=`mode`
  - Valores válidos: `keyed` | `keyless`
  - Owner: pulumi-platform (Fulcio component sets/updates em T2.2)
- If date > deadline AND mode == keyed → exit 1 (block)
- If date > deadline AND mode == keyless → exit 0
- Else → exit 0 (still within window)
- **Emergency override (EC-7 edge-case-review):** flag `--emergency-override=<reason>` que:
  - Emite WARN log + escreve `audit_log` entry com reason + actor + timestamp
  - Exit 0
  - Doc menciona: uso requer post-hoc Change Governor review em 24h

Rotation runbook:
1. Generate new cosign keypair offline
2. Store private key em ESO Secret
3. Add new public key to ConfigMap (verification)
4. Phase: dual-sign 7 days (both old + new)
5. Phase: cut over to new key only
6. Destroy old private key (offline)

#### Tasks
1. Implement check-cutover-deadline.sh script
2. Hermetic test for script (mock date + ConfigMap reads)
3. Register em scripts allowlist
4. Wire em taskfiles/quality.yml
5. Document rotation runbook
6. Extend concept page

#### TDD
```
RED:     test_cutover_deadline_blocks_after_deadline_in_keyed_mode() — script test
RED:     test_cutover_deadline_passes_in_keyless_mode() — script test
RED:     test_cutover_deadline_passes_within_window() — script test
GREEN:   Implement script
VERIFY:  bash infra/scripts/security/check-cosign-cutover-deadline.sh (mock mode) PASS
```

#### Acceptance Criteria
- [ ] Script implements 3 conditions correctly
- [ ] Hermetic tests PASS
- [ ] Registered em allowlist + taskfiles
- [ ] Rotation runbook documented
- [ ] Concept page extended

#### DoD
- [ ] Script + tests + runbook committed
- [ ] `task quality:gates` includes check
- [ ] Tier: T2 (supply chain)

---

### T3.4 — CLAUDE.md Comprehensive Refresh

#### Objective
Comprehensive rewrite of CLAUDE.md sections affected by 2026-05-23 GitHub Actions removal + 2026-05-26 audit. Update CI section + task quality:* documentation + drill public tasks. Closes M12 LOW gap.

#### Evidence
- M12 CI/CD audit: "Documentation drift CLAUDE.md — per commit 37a58cec 'de-stale CLAUDE.md after GitHub Actions removal' — CLAUDE.md ainda documenta 48-workflow CI (legacy) despite all deleted. Stale: CI platform TBD · task quality:* invocation pattern new · drill tasks public agora"

#### Files to edit
```
CLAUDE.md                                                  — Comprehensive refresh CI/Testing sections
```

#### Deep Dives
Sections to refresh:
- "Development Commands" — add task quality:* invocations
- "Stack > CI/CD" — remove "GitHub Actions" references, add "task quality:* + loopzero-ops quality <gate>"
- "Tool Inventory > Supply Chain Security" — note GitHub Actions removal impact on "Where" columns
- "Sprint Plan > Sprint 9" — update with current state (post-Op-B)
- "Common Mistakes" — add learnings from 2026-05-26 audit
- Add reference to 2026-05-26 audit em "Maturity" section

#### Tasks
1. Refresh CI/Testing sections
2. Add task quality:* invocation examples
3. Document drill public tasks (drill:up, drill:l2-scale, etc.)
4. Reference 2026-05-26 audit + 2026-05-26 plan
5. Update "Path to maturity 4.0" with current state

#### TDD
```
RED:     N/A — documentation task
VERIFY:  grep "48 workflows\|GitHub Actions" CLAUDE.md returns 0 active references (only historical context)
```

#### Acceptance Criteria
- [ ] CI/Testing sections refreshed
- [ ] task quality:* documented
- [ ] Drill tasks documented
- [ ] 2026-05-26 audit referenced

#### DoD
- [ ] CLAUDE.md committed
- [ ] Tier: T0 (documentation)

---

## Phase 4: Test Debt + Polish (Parallel)

**Objective:** Address LOW gaps + remaining test debt. 7 tasks, fully parallel.

### T4.1 — Property-Based Tests Extension (errors, jwt, enforcement)

#### Objective
Extend property-based testing (testing/quick) to pkg/errors, pkg/jwt, pkg/enforcement packages. Closes M04 LOW gap.

#### Evidence
- M04 pkg/ audit: "Property-based testing limited a 2 packages (naming, statemachine). Recomendado para errors wrap, jwt validator inputs, enforcement edge cases"

#### Files to edit
```
pkg/errors/properties_test.go                              — (NEW) LoopZeroError fuzz tests
pkg/jwt/properties_test.go                                 — (NEW) JWT validator fuzz
pkg/enforcement/properties_test.go                         — (NEW) Plan limiter fuzz
```

#### Deep Dives
- errors: WrapWithStack random call depths + concurrent wraps
- jwt: Adversarial token inputs (malformed typ, algo confusion, expired)
- enforcement: Plan caps com random tenant configs

#### Tasks
1. Implement 3 property test files (5-8 tests each)
2. Run em `go test -count=1 ./pkg/...`

#### TDD
```
RED:     TestProperty_LoopZeroErrorWrapDeepCallChain — random depths
RED:     TestProperty_JWTValidatorRejectsAlgoConfusion — adversarial
RED:     TestProperty_EnforcementCapsRespectPlanLimits — random
GREEN:   Tests pass (implementation already correct)
VERIFY:  go test -count=1 ./pkg/... GREEN
```

#### Acceptance Criteria
- [ ] 3 new property test files com 5-8 tests each
- [ ] All tests PASS
- [ ] Property assertions ~10k per file (testing/quick MaxCount=1000)

#### DoD
- [ ] Files committed · Tier: T1

---

### T4.2 — shellrun Test Coverage (Signal/Exit Corners)

#### Objective
Add test coverage para pkg/shellrun signal handling + exit corner cases. Closes M04 LOW gap.

#### Evidence
- M04 pkg/ audit: "shellrun test coverage 18% — Basic smoke tests; sem chaos injection (exit code corners, signal handling under resource pressure)"

#### Files to edit
```
pkg/shellrun/signal_test.go                                — (NEW) Signal handling tests
pkg/shellrun/exit_corners_test.go                          — (NEW) Exit code edge cases
```

#### Tasks
1. Implement signal tests (SIGINT, SIGTERM, SIGKILL)
2. Implement exit code tests (124 timeout, 130 SIGINT, 143 SIGTERM)
3. Resource pressure scenarios

#### TDD
```
RED:     test_shellrun_propagates_sigint() — fails before test
RED:     test_shellrun_exit_code_124_on_timeout() — timeout corner
GREEN:   Tests pass (existing implementation likely correct)
VERIFY:  go test -count=1 ./pkg/shellrun/... GREEN
```

#### Acceptance Criteria
- [ ] Signal + exit tests PASS
- [ ] Coverage ≥ 60% (vs 18% baseline)

#### DoD
- [ ] Files committed · Tier: T1

---

### T4.3 — Dashboard Semantic Validation (promtool query check)

#### Objective
Add promtool query validation em CI for 21 Grafana dashboards. Closes M08 LOW gap.

#### Evidence
- M08 Observability audit: "Dashboard semantic validation missing — 21 dashboards JSON syntax only validated; sem query validation contra Prometheus schema"

#### Files to edit
```
pkg/quality/dashboardpromtool/check.go                     — (NEW) Gate parsing JSON + promtool check
pkg/quality/dashboardpromtool/check_test.go                — (NEW) Tests
cmd/loopzero-ops/quality_subcmd.go                              — Wire command "dashboard-promtool"
taskfiles/quality.yml                                       — Add gate
```

#### Tasks
1. Implement gate parsing dashboard JSON, extracting Prometheus queries
2. Invoke promtool check rules em each query
3. Test coverage

#### TDD
```
RED:     test_dashboard_promtool_passes_valid_queries()
RED:     test_dashboard_promtool_fails_invalid_query()
GREEN:   Implement
VERIFY:  loopzero-ops quality dashboard-promtool --root . PASS
```

#### Acceptance Criteria
- [ ] Gate validates 21 dashboards
- [ ] Tests PASS · Wired em task quality:*

#### DoD
- [ ] Files committed · Tier: T1

---

### T4.4 — M11 Helper Consistency

#### Objective
Standardize state machine helper naming OR consolidate to canonical `machine.IsTerminal()`. Closes M11 LOW gap.

#### Evidence
- M11 State Machines audit: "M11 #26 helper consistency — IsBuildTerminal/IsReleaseTerminal/IsDomainTerminal em definition files vs sem helper para ExposedRoute/Application. Acceptable mas viola single-style"

#### Files to edit
```
pkg/statemachine/build.go                                  — Remove IsBuildTerminal duplicate
pkg/statemachine/release.go                                — Remove IsReleaseTerminal duplicate
pkg/statemachine/domain.go                                 — Remove IsDomainTerminal duplicate
(... callers updated to use machine.IsTerminal())
```

#### Tasks
1. Audit callers of IsXxxTerminal helpers
2. Update callers to use machine.IsTerminal()
3. Remove duplicate helpers

#### TDD
```
RED:     N/A — refactor task
VERIFY:  go test -count=1 ./pkg/statemachine/... GREEN
VERIFY:  grep -c "IsBuildTerminal\|IsReleaseTerminal\|IsDomainTerminal" pkg/ should be 0
```

#### Acceptance Criteria
- [ ] Duplicates removed · Callers use canonical · Tests GREEN

#### DoD
- [ ] Files committed · Tier: T1

---

### T4.5 — Release ARCHIVED State Machine Integration

#### Objective
Integrate Release.ARCHIVED como state machine state (currently DB-only). Define ReleaseArchived event. Closes M11 LOW gap.

#### Evidence
- M11 State Machines audit: "Release.ARCHIVED arquiteturalmente disconnected — DB-only state (migration 000008), NOT em state machine definition"

#### Files to edit
```
pkg/statemachine/release.go                                — Add ARCHIVED state + ReleaseArchived event
pkg/statemachine/release_typed.go                          — Typed wrapper
pkg/statemachine/release_test.go                           — New tests
api/internal/services/release_archiver.go                  — Use new event
```

#### Tasks
1. Add ARCHIVED state to Release machine
2. Add ReleaseArchived event (CANCELLED|SUPERSEDED → ARCHIVED)
3. Update release_archiver service to use Transition()
4. Tests

#### TDD
```
RED:     test_release_archive_event_transitions_from_cancelled() — fails before
RED:     test_release_archive_event_transitions_from_superseded() — fails before
GREEN:   Add state + event
VERIFY:  go test -count=1 ./pkg/statemachine/... GREEN
```

#### Acceptance Criteria
- [ ] ARCHIVED state em machine · Event defined · Service uses Transition()
- [ ] DB CHECK constraint unchanged · Tests GREEN

#### DoD
- [ ] Files committed · Tier: T2 (state machine change)

---

### T4.6 — Loki Cross-Cluster Federation

#### Objective
Implement Loki cross-cluster federation (multi-cluster log aggregation). Closes M08 MEDIUM gap (Phase 3 ADR-0009).

#### Evidence
- M08 Observability audit: "Loki cross-cluster siloed — Single Loki em build cluster. Runtime/workload Promtail push to build Loki via hardcoded IP. Phase 3 ADR-0009 sem implementation"

#### Files to edit
```
infra/helm/loki/values.yaml                                — Federation config
infra/helmfile/mgmt.helmfile.yaml.gotmpl                   — Loki em mgmt cluster (federate)
infra/helm/promtail/values.yaml                            — Templated push URL
docs/architecture/observability/diff/loki-federation.md    — Architecture update
```

#### Tasks
1. Configure Loki em mgmt cluster (federation primary)
2. Configure Loki em build/runtime as federated backends
3. **Templated Promtail push URLs per environment (EC-8 edge-case-review):**
   - Mechanism: extend `infra/helm/promtail/values.gotmpl.yaml` com env-conditional push URL
   - Pattern já existe (cell-default → mgmt Loki internal LB) per commit `ef7a7c16` — extend para drill+prod
   - Matrix: local/drill/prod cada com push URL templated via helmfile env block
4. Validate cross-cluster grep works

#### TDD
```
RED:     N/A — infrastructure task
VERIFY:  Manual cross-cluster grep: logs from build appear em mgmt Loki
```

#### Acceptance Criteria
- [ ] Loki em mgmt federated · Promtail URLs templated · Cross-cluster grep works

#### DoD
- [ ] Configs committed · Tier: T2 (infra change)

---

### T4.7 — Trivy/TruffleHog Re-wire em task quality:*

#### Objective
Re-wire Trivy (CVE scan) + TruffleHog (secret scan) gates em task quality:* post-GitHub-Actions removal. Closes M07 + M12 MEDIUM gap.

#### Evidence
- M07 + M12: "Trivy + TruffleHog re-wiring em task quality:* TBD"

#### Files to edit
```
pkg/quality/trivyscan/check.go                             — (NEW) Trivy gate
pkg/quality/trufflehogscan/check.go                        — (NEW) TruffleHog gate
cmd/loopzero-ops/quality_subcmd.go                             — Wire commands
taskfiles/quality.yml                                       — Add to quality:gates
```

#### Tasks
1. Implement trivyscan gate (severity=CRITICAL,HIGH exit-code:1)
2. Implement trufflehogscan gate (--only-verified)
3. Wire CLI commands
4. Test coverage

#### TDD
```
RED:     test_trivy_scan_passes_clean_image()
RED:     test_trivy_scan_fails_high_severity_cve()
RED:     test_trufflehog_passes_no_secrets()
RED:     test_trufflehog_fails_verified_secret()
GREEN:   Implement
```

#### Acceptance Criteria
- [ ] Both gates implemented + tested · Wired em task quality:gates

#### DoD
- [ ] Files committed · Tier: T2 (supply chain)

---

## Phase 5: Re-Audit + Close-Out (Sequential)

**Objective:** Validate score lift via fresh audit. 4 tasks.

### T5.1 — /audit-modular full Re-Execute

#### Objective
Re-run `/audit-modular` skill com fresh 12-module assessment post-Phase 0-4 implementation.

#### Files to edit
```
.claude/knowledge-base/audits/YYYY-MM-DD/                  — (NEW dated dir) 12 module docs + SUMMARY
```

#### Tasks
1. Invoke `/audit-modular full`
2. Validate all 12 module docs generated
3. SUMMARY captures delta vs 2026-05-26

#### Acceptance Criteria
- [ ] New audit dir created · 12 module docs + SUMMARY exist
- [ ] Delta tracking vs 2026-05-26 shows expected lifts

#### DoD
- [ ] All artifacts committed · Tier: T0

---

### T5.2 — Score Lift Validation per Module

#### Objective
Validate each module reached target score. If any module missed target, identify root cause + create follow-up task.

#### Tasks
1. Compare 2026-05-26 scores vs new audit
2. For each missed target: root cause analysis
3. Document follow-ups em audit doc

#### Acceptance Criteria
- [ ] All 12 modules ≥ 3.9 OR root cause documented
- [ ] Portfolio average ≥ 3.95

#### DoD
- [ ] Validation doc committed · Tier: T0

---

### T5.3 — Cross-Validation Final Pass

#### Objective
Invoke `/cross-validation 2026-05-26-maturity-3-62-to-4-0-roadmap` to validate plan vs implementation.

#### Tasks
1. Run cross-validation skill
2. Address any CRITICO/GRAVE findings

#### Acceptance Criteria
- [ ] Zero CRITICO · Zero GRAVE

#### DoD
- [ ] Cross-validation report committed · Tier: T0

---

### T5.4 — SUMMARY.md Delta Update + CHANGELOG.md

#### Objective
Update CLAUDE.md "Maturity" section with new portfolio score + commit CHANGELOG entry.

#### Files to edit
```
CLAUDE.md                                                  — Update Maturity section
CHANGELOG.md                                               — Add Unreleased entry
```

#### Tasks
1. Update CLAUDE.md com new score + audit reference
2. CHANGELOG.md entry under "Changed"

#### Acceptance Criteria
- [ ] CLAUDE.md reflects new score
- [ ] CHANGELOG.md entry follows Keep a Changelog format

#### DoD
- [ ] Files committed · Tier: T0

---

## Coverage Matrix

| # | Gap / Requirement | Severity | Module | Task(s) | Resolution |
|---|---|---|---|---|---|
| 1 | Production canary stages 4-5 | HIGH | M09 + M12 | T0.3, T0.4 | Runbooks + customer trial onboarding plan |
| 2 | GitHub Actions removal sem scheduler replacement | HIGH | M12 | T0.2 | Real CI platform decision via T3 meeting |
| 3 | 3-way overlapping charts drift | HIGH | M06 | T1.2 | CI parity gate |
| 4 | Correlation-ID propagation 7/8 reconcilers | HIGH | M03 + M08 | T1.1 | Mechanical injection across 39 reconcilers |
| 5 | Cosign keyless infrastructure não provisioned | HIGH | M07 | T2.2 | Pulumi component for Fulcio + Rekor |
| 6 | withEnvironmentLock opacity em route handlers | HIGH | M02 | T1.3 | Audit + static test |
| 7 | Operator-run prod-identical drill GREEN gate | HIGH | M12 | T0.2 + T0.3 | CI platform integration + canary |
| 8 | 30 errors.New() residuais | MEDIUM | M02 | T2.4 | Triage + LoopZeroError conversion |
| 9 | Cell drain operational runbook ausente | MEDIUM | M09 | T3.1 | Runbook |
| 10 | Session token rotation undocumented | MEDIUM | M10 | T3.2 | Runbook |
| 11 | 10 permissive Helm schemas | MEDIUM | M06 | (Deferred to T3.2 schema tightening follow-up — separate plan) | Acknowledge gap |
| 12 | Multi-K8s-version testing missing | MEDIUM | M06 | T2.5 | chart-testing matrix |
| 13 | Cosign digest enforcement em charts não-ativo | MEDIUM | M06 + M07 | T2.3 | Chart template + ClusterImagePolicy |
| 14 | pulumi-platform test coverage | MEDIUM | M05 | T2.1 | Component tests |
| 15 | Loki cross-cluster siloed | MEDIUM | M08 | T4.6 | Federation |
| 16 | Trivy + TruffleHog status post-removal | MEDIUM | M07 + M12 | T4.7 | task quality:* gates |
| 17 | CLAUDE.md numerical drift | LOW | M01/M02/M06/M08/M12 | T1.4 | Cleanup |
| 18 | helm-chartsnap + chart-testing referenced not active | LOW | M06 | T2.5 | chart-testing integration |
| 19 | `doctor` CLI command unwired | LOW | M01 | T2.6 | Wire em register/ |
| 20 | process.exit vs exitCode inconsistência | LOW | M01 | T2.6 | Refactor platform.ts |
| 21 | M11 #26 helper consistency | LOW | M11 | T4.4 | Standardize OR consolidate |
| 22 | Release.ARCHIVED architectural disconnect | LOW | M11 | T4.5 | Integrate em state machine |
| 23 | SBOM-to-image attestation linking missing | LOW | M07 | (Deferred to T2.3 follow-up — separate plan) | Acknowledge |
| 24 | Harbor mirror failures silent | LOW | M07 | (Deferred — operational LOW) | Acknowledge |
| 25 | Dashboard semantic validation missing | LOW | M08 | T4.3 | promtool gate |
| 26 | shellrun test coverage 18% | LOW | M04 | T4.2 | Signal/exit tests |
| 27 | Property-based tests escopo limitado | LOW | M04 | T4.1 | Extension to errors/jwt/enforcement |
| 28 | check-cosign-cutover-deadline script ausente | HIGH | M07 | T3.3 | Implement script |
| 29 | Signing key rotation enforcement | HIGH | M07 | T3.3 | Runbook + script |
| 30 | Stripe Portal SDK em Sprint 11 placeholder | MEDIUM | M10 | T2.7 | SDK integration |
| 31 | Architecture docs Pre/Post-Plan | T0 mandatory | All | T0.1 + T1.5 | Mode 2 + Mode 3 invocation |
| 32 | Re-audit validation | T5 mandatory | All | T5.1-T5.4 | Fresh audit + cross-validation |

**Coverage: 32/32 gaps covered (100%) — 2 LOW deferred to separate follow-up plans explicitly noted**

## Global Definition of Done

- [ ] All phases completed (Phase 0-5)
- [ ] All tests passing (Go + TypeScript)
- [ ] Zero lint warnings (golangci-lint + ESLint)
- [ ] Backward compatibility — pre-release; breaks acceptable when justified
- [ ] code-audit checks passing
- [ ] **`/cross-validation 2026-05-26-maturity-3-62-to-4-0-roadmap` PASSED** — zero CRITICO, zero GRAVE
- [ ] Plan-specific criteria:
  - [ ] All 12 modules audited com new scores
  - [ ] Portfolio average ≥ 3.95
  - [ ] 8 HIGH gaps closed OR formally deferred via MEET-ASYNC-AMENDMENT
- [ ] **Evidence** (CONDITIONAL — score-affecting plan):
  - [ ] Evidence documented em `.claude/knowledge-base/audits/YYYY-MM-DD/` with file:line references (3 distinct sources T1/T2/T3)
- [ ] **`/dogfood full` PASSED** (see Dogfood Gate below)

---

## Dogfood Gate (MANDATORY — NON-NEGOTIABLE)

> **No plan is considered DONE without passing `/dogfood full`.** Unit tests, integration tests, and lint are necessary but NOT sufficient. Only real end-to-end validation via dogfood proves the system works.

After ALL phases are complete and ALL tests pass, the final validation is a full dogfood audit.

### Execution

```bash
# 1. Fresh infrastructure
task clean:all && task setup && tilt up
task doctor   # PASS=41 / FAIL=0 expected

# 2. Mint JWT
HYDRA_NAMESPACE=loopzero-auth bash infra/helmfile/scripts/seed-local-user.sh

# 3. Run dogfood full
export LOOPZERO_E2E=1
export LOOPZERO_API_URL=http://localhost:13001
export LOOPZERO_TOKEN=$(cat ~/.loopzero/local-dogfood-token.txt)
export LOOPZERO_E2E_DATABASE_URL='postgres://loopzero:loopzero-dev-password@127.0.0.1:15432/loopzero?sslmode=disable'
go run github.com/onsi/ginkgo/v2/ginkgo --procs=1 -v --label-filter="journey" --timeout=120m ./infra/tests/e2e/

# 4. Run chaos full
TARGET_CTX=k3d-loopzero-mgmt bash infra/scripts/test/run-full.sh
```

### Pass Criteria

- [ ] Cleanroom phases 1-4 PASS (clean:all → setup → tilt up → doctor)
- [ ] **112/0 journey specs PASS** (procs=1 serial — established baseline 2026-05-26)
- [ ] **10/10 chaos scenarios PASS**
- [ ] Zero S0 (Blocker) findings
- [ ] Zero S1 (Severity 1) findings related to plan scope
- [ ] Prova de Vida passes for every deploy
- [ ] Prova de Morte passes for every delete
- [ ] No regressions in previously passing flows
- [ ] Observability check passes (logs, metrics, CRs em correct state)
- [ ] Rule 3 clean: zero internal terms em CLI output
- [ ] Op B counter visible (loopzero_clustercredential_ca_fetch_total)
- [ ] All correlation_id flows traceable em Loki (new in this plan)

### On Failure

If `/dogfood full` finds S0 or S1 issues:
1. Fix ALL S0/S1 findings
2. Re-run `/dogfood full` on fresh infrastructure
3. Repeat until zero S0/S1

S2-S4 findings are documented but do not block plan completion.

---

## Notes on M04 + M11 Capped at 3.9

Per ADR D3, pkg/ + State Machines modules are formally capped at 3.9/4.0 due to M11 #25 string-aliases tradeoff. This means:

- **Portfolio ceiling = 11×4.0 + 2×3.9 / 12 ≈ 3.983** (not perfect 4.0)
- Documented em MEET-ASYNC-AMENDMENT post-implementation
- Future plan may revisit if DB CHECK refactor + JSON marshaling compat + K8s status conversion proves tractable (high-risk, ~16h+)

Acceptable trade-off; cap is conscious design decision, not unresolved gap.
