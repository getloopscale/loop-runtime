# ADR-0021 — Infra Normalize (CHG-2026-04-30): D1-D10

| | |
|---|---|
| **Status** | APPROVED (governance via 3 T3 meetings; implementation iterative) |
| **Date** | 2026-04-30 |
| **Plan** | docs/plans/infra-normalize-multicell-plan.md (v2) |
| **Discovery** | PR #51 review + deep `infra/` review uncovered 8 structural debts |
| **Reviewers** | platform-architect, platform-security-engineer, sre-engineer, production-design-reviewer, distributed-systems-engineer, k8s-controller-engineer, gitops-engineer, change-validation-planner, dx-engineer, verification-architect (across 3 T3 meetings) |
| **Supersedes** | parts of CLAUDE.md "Helm Chart Dual System" section (operator monolith claim no longer current; see D1) |
| **Related decision records** | docs/decisions/MEET-INFRA-NORMALIZE-T3-{operator-split,pulumi-consolidation,multi-cell}.md |

## Problem

The 2026-04-30 deep review of `infra/` (run on `fix/parity-on-refactorhelms` post-PR #51) surfaced eight structural issues plus an unfinished feature (multi-cell). v1 of the plan addressed coverage but failed FAANG rigor — a self-audit caught 3 workarounds and 10 gaps. v2 rewrote with mandatory governance meetings before code, forward-only migration (no flag debt), source-verified multi-cell, cluster-real integration tests, and per-phase Security + SRE veto.

Specific S0/S1 findings:
1. Tilt deploys 4 split operators; Helmfile deploys 1 monolith → Rule 10 break.
2. 4 Pulumi project dirs; 3 unused; 2 import `@pulumi/kubernetes` violating ADR-0020 D7; one collides on project name.
3. 3 orphan `infra/tilt/*-deployment.yaml` not referenced by Tiltfile.
4. CLAUDE.md "Cell architecture Phase 0+1+1.5 complete; runtime-2 not routed" never live-verified — `dynamic_router.go` exists but no ClusterCredential CR with `cellID: cell-2` exists anywhere in repo.
5. 56 of 59 CLI subcommands lack any integration test (T0.3 audit gap matrix).
6. `regression_test.go` asserts on string-literals (broken 4× in earlier session by semantic-equivalent refactors).
7. `charts/loopzero-platform` umbrella requires `helm dependency build` not in `task validate:manifests`.
8. Sloth SLO files bypass kubeconform validation.

## Decisions

### D1 — Forward-only operator topology migration (no feature flag)

**Decision.** Helmfile drops `tenant-controller` + `loopzero-controller` (monolith) in the same PR that adds `loopzero-{tenant,app,preview,platform}-operator`. Tilt also migrates atomically. Cutover is one PR, one revert away from rollback.

**Rationale.** Rule 10 violation (Local ≠ Production) is unacceptable. Pre-release governance ("we CAN break things and fix them properly") favors forward-only. A `splitOperators` flag was rejected (option A) because it would empilhe more dual-chart debt on top of the existing one documented in CLAUDE.md.

**Vetoes captured (from MEET-INFRA-NORMALIZE-T3-operator-split):**
- Security: 4 split ClusterRoles must be least-privilege subset of monolith (5 rule widenings flagged in tenant-operator; tracked as conditions C10/C20).
- SRE: 4 leases must coexist without thrashing (verified unique LeaderElectionIDs); replicaCount=1 violates atomicity (fixed in this commit batch).
- Production-design: cutover must be atomic (no ≥30s reconciler gap) — requires explicit migration script (C1) not yet shipped.

**Consequences.** Phase 2 cutover is BLOCKED until 24 conditions (C1-C24) satisfied. T2.1 (chart hardening, replicaCount=2) shipped this session.

### D2 — Single Pulumi project (delete 3 paralelas dirs + state archive)

**Decision.** Delete `infra/pulumi-{infra,platform,control-plane,shared}/` and `provision-homolog.sh`. Before deletion: archive state via `pulumi stack export` to encrypted S3 + verify against live DO resources.

**Rationale.** YAGNI; 3 paralelas dirs only referenced by homolog-script (homolog isn't an active environment); state collision risk from duplicate project name; ADR-0020 D7 violations (`@pulumi/kubernetes` in 5+1 source files).

**Vetoes captured (from MEET-INFRA-NORMALIZE-T3-pulumi-consolidation):**
- Platform-architect: zero-live-resources gate before any rm.
- Security: archive S3 bucket private + KMS-encrypted + 90d retention.
- GitOps: rm reproducible via runbook, no ad-hoc destruction.
- SRE: deletion impacts no live monitoring/alerts.

**Consequences.** Phase 3 BLOCKED until T0.2 inventory + 14-test validation matrix GREEN + 14d replay drill.

### D3 — Multi-cell tiered canary with error-budget gates

**Decision.** Cell-2 activation via 5 stages: 1 synthetic tenant 1h, 10% dev 24h, (staging if exists), 1 customer-opt-in 7d, prod 10/50/100% with 7d holds. Halt on cell-1 EB consumption > 1%.

**Rationale.** Real customer traffic; blast radius must be progressively contained.

**Vetoes captured (from MEET-INFRA-NORMALIZE-T3-multi-cell):**
- SRE: rollback measured under load (P50 ≤ 5min, P95 ≤ 8min) before stage 4; capacity gate ≥8 nodes before stage 1; manual halt + PagerDuty + `--proceed` flag.
- Security: cred revocation (not just rotation) DOKS-side; cross-cell isolation verified at stage 1.
- Distributed-systems: cell-unavailable returns 503 not 500; routing determinism invariant + test (shipped via T5.4 metrics).
- DX: customer comms template + opt-in/opt-out + status page note.
- Production-design: cell-2-independent rollback; explicit dev-GREEN-prod-fails runbook.

**Consequences.** T5.4 (observability) + T5.5 (admin pin) + T5.3b (CC bootstrap) + cells migration shipped offline. T5.6/T5.7 cloud-pendentes.

### D4 — Cluster-real CLI integration tests (no nock)

**Decision.** `tests/integration/_fixtures/spawn-helper.ts` (async `spawn()` against real `bin/loopzero`) + `mock-api-server.ts` (in-process pure-Node HTTP on loopback). At least one success + one error test per register group.

**Rationale.** F-02 audit found `bin/loopzero` exit-code wiring only catchable by fork-based tests. Pure mocks miss plumbing bugs.

**Critical bug surfaced during T4.1.** Initial harness used `spawnSync()` — blocked the parent event loop, in-process mock could not serve, child hung, SIGKILL → exit 143. **Async fix is mandatory** when in-process mock interacts with child spawn.

**Consequences.** 11 register integration suites covering 47 cases; closes T0.3 audit gaps for 11 of 12 register groups. cli-commands.yml CI gate.

### D5 — Per-phase Security + SRE review with veto power

**Decision.** Each implementation phase gates on written Security Review + SRE Review answers with explicit veto criteria. Reviewers: `platform-security-engineer` (absolute veto), `sre-engineer` (absolute veto on production readiness).

**Rationale.** Pre-release does not exempt the platform from rigour. v1 plan omitted these entirely.

**Consequences.** Plan v2 has 17 Security Review + 17 SRE Review + 17 Rollback Plan + 17 Observability Plan sections (one per implementation task).

### D6 — Per-phase Observability Plan baked-in

**Decision.** Each phase that adds a feature also adds: ≥1 Prometheus metric, ≥1 alert with runbook annotation, dashboard panel update.

**Rationale.** Multi-cell without metrics = blackbox. Observability is part of the deliverable, not a follow-up.

**Implementation.** T5.4 shipped 4 metrics + 7 alerts + runbook (`docs/runbooks/multi-cell-alerts.md`) BEFORE any cell-2 traffic flips.

### D7 — Pre-release governance per the project's own rules

**Decision.** Phase −1 invokes `/meeting` (Meeting Orchestrator) for each of the three T3 changes BEFORE implementation begins. Records committed in `docs/decisions/MEET-INFRA-NORMALIZE-T3-*`.

**Rationale.** `.claude/rules/pre-release-governance.md` mandates meeting + ADR + change-governor for T2/T3 BEFORE code. v1 violated this; v2 corrects.

**Implementation.** 3 records committed (commits 8ba6c2f2, 3b7de128, 63772b48). 82 conditions captured.

### D8 — Refactorhelms merge through proper PR review

**Decision.** Phase 1 opens PR `fix/parity-on-refactorhelms → develop`, normal review, merge per repo policy. No direct push.

**Rationale.** Verbal "merge foi feito" ≠ git artifact. v1 proposed direct push (workaround W1); v2 corrects.

**Status.** Branch pushed; PR opening pending (T1.1 not yet executed in this session).

### D9 — Production rollout is a separate phase

**Decision.** Phase 5 = dev activation. Phase 7 = production rollout (separate phase, separate CI gate, separate SRE review).

**Rationale.** Pre-release status is for this codebase but production exists (4 DOKS clusters per CLAUDE.md). Treat production as production.

### D10 — Zero anonymous DEBT

**Decision.** Every DEBT row has owner + horizon + stop-the-line condition.

**Implementation.** Single tracked DEBT in matrix coverage row #5: regression_test.go behavioural refactor, owner `agent:test`, horizon 2026-06-30, stop-the-line "≥3 broken-by-string-change PRs".

## Consequences

**Positive:**
- Forward-only operator migration prevents dual-chart debt growth.
- Single Pulumi project removes 3 dead-code dirs + state collision risk.
- Multi-cell observability + admin pinning shipped BEFORE any cell-2 traffic.
- CLI fork-based integration tests catch plumbing bugs unit tests miss.
- Per-phase Security/SRE/Rollback/Observability sections force rigour at every step.

**Negative:**
- Phase 2 cutover blocked on 24 conditions (5-7 days dedicated work).
- Phase 3 blocked on Pulumi backend access offline-unavailable.
- Phase 5 live stages (T5.6/T5.7) blocked on cloud + soak time (1h+24h).
- Phase 7 blocked on customer opt-in + 28+ days wall-clock.

**Open questions:**
- Sprint to attack Phase 2 conditions (likely 1 sprint dedicated).
- Customer comms timing for Phase 7 stage 4.

## Verification

| Decision | Test / artefact | Status |
|---|---|---|
| D1 | 4 split charts replicaCount=2 + helm lint GREEN; conditions C1-C24 tracked | Phase 2 cutover BLOCKED on conditions |
| D2 | T0.2 + T3.1 archive + T3.2 stack rm + T3.3 code rm | BLOCKED on Pulumi backend access |
| D3 | T5.4 obs + T5.5 admin pin + T5.3b CC bootstrap + cells migration | Code GREEN; live stages cloud-pendentes |
| D4 | 11 integration suites + spawn-helper + mock-api-server | GREEN (1243 CLI tests) |
| D5 | 17 Security + 17 SRE Review sections in plan v2 | Documented; reviewers conveyed at meetings |
| D6 | 4 metrics + 7 alerts + runbook | GREEN |
| D7 | 3 decision records committed | GREEN |
| D8 | PR fix/parity-on-refactorhelms → develop | PENDING (T1.1 next iteration) |
| D9 | Phase 7 separate from Phase 5 in plan v2 | Documented |
| D10 | Coverage matrix row #5 with owner+horizon+stop-the-line | GREEN |

| Live validation | Status |
|---|---|
| `task validate:matrix` (10 offline gates) | GREEN 10/10 |
| `task parity:k3d-virgin` (degrau 1) | GREEN (verified earlier infra-parity sprint) |
| Cell-2 stage 1 canary (1h soak) | Cloud-pendente |
| Cell-2 stage 2 canary (24h soak) | Cloud-pendente |
| Production stage 4 canary (7d) | Cloud-pendente + customer opt-in |
| Production stages 5a/b/c (10/50/100%) | Cloud-pendente |

## References

- Plan: `docs/plans/infra-normalize-multicell-plan.md`
- 3 governance records: `docs/decisions/MEET-INFRA-NORMALIZE-T3-{operator-split,pulumi-consolidation,multi-cell}.md`
- Audits: `docs/audits/{multi-cell-source-audit,cli-commands-audit}.md`
- Runbooks: `docs/runbooks/multi-cell-alerts.md`
- Memory: `~/.claude/projects/-home-paulo-loopzero/memory/reference_infra_normalize_2026_04_30.md`
- Predecessor ADR: `docs/decisions/ADR-0020-infra-parity-decisions.md`
