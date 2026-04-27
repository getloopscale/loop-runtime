# ADR-0016: Sprint 8 Phase 0-6 — Three-CHG Hardening Strategy

**Status:** ACCEPTED
**Date:** 2026-04-27
**Origin:** MEET-2026-04-27-061 — 9-reviewer T3 governance, unanimous APPROVED-WITH-CONDITIONS for Option B
**Predecessor:** ADR-0015 (Pre-bootstrap Adopt-or-Create Policy) + MEET-2026-04-27-060 (CHG-001 Bootstrap)
**Deciders:** Principal Architect, Platform Architect (Sponsor), GitOps Engineer, K8s Controller Engineer, Platform Security Engineer (VETO holder), SRE Engineer (VETO holder, Challenger), Change Validation Planner, Production Design Reviewer, DX Engineer (Conditional VETO)

---

## Context

CHG-001 closed GREEN on 2026-04-27 13:40 with the 14/14 validation matrix verified (`/home/paulo/.loopzero/artifacts/validation-20260427-134037.log`). LoopZero dev infrastructure exists end-to-end on DigitalOcean: 4 DOKS clusters (mgmt, build, runtime, runtime-2 — the latter physically provisioned but not yet routed), managed PostgreSQL `loopzero-infra-dev-sfo3-db`, Container Registry `loopzero-system` (adopted, not recreated), Spaces buckets, Cloudflare Tunnel `loopzero-dev`, all Helm releases bootstrapped, `https://api.dev.loopzero.example/api/v1/health` HTTP 200.

ADR-0010 mandates seven Phase 2a deliverables before cell-2 activation (Phase 2b in Sprint 9):
1. L3 (100 tenants) ≥95/100 ten times consecutive
2. PG `max_connections ≥ 100`
3. Gitea HA (2 replicas + shared storage + 200 concurrent clones)
4. Harbor HA (2+ core + Redis HA + S3 shared)
5. MinIO distributed migration
6. mgmt cluster memory <50% idle
7. Unified `tenant-health` Grafana dashboard

Plus a chaos test proving each shared service kill recovers <2min and 5 consecutive L3 ≥98/100. Today, **0/7 are met** — every one requires hardening or migration of a running primitive.

Sprint 8 v3.2 plan (`docs/plans/sprint8-hardening-chaos-plan.md`) covers all of this in 18 tasks across Phase 0 (infra scale-up), Phase 1 (L3 stabilization), Phase 2 (shared-service HA), Phase 3 (L4 validation), Phase 4 (Gate 2a Exit), Phase 5 (chaos), Phase 6 (observability). Phase -1 (Bootstrap) shipped under CHG-001. Phase 0-6 is what CHG-002 must execute.

The orchestrator's initial proposal was Option A: a single CHG-002 covering all 18 tasks under one approval. MEET-2026-04-27-061 unanimously rejected this on the same grounds MEET-051 and MEET-060 rejected analogous proposals: epic-CHG anti-pattern, blast radius unbounded, three live-data mutations (PG resize, MinIO migration, chaos injection) cannot share one acceptance gate.

Phase -1 telemetry surfaced five additional gaps — `Pulumi.staging.yaml`/`prod.yaml` k8s versions stale (`1.32.13-do.2` vs dev `1.33.9-do.3`), hardcoded `lokiPushUrl: 10.20.0.26:30100` in dev.yaml, 5 Helm charts missing upstream-repo mapping, T6.3 Promtail validation correctly de-prioritized in v3.2, four `mgmt-*.ts` Pulumi components without PSA compliance audit. These are housekeeping that must be reconciled in CHG-002.

---

## Decision

**Split CHG-002 into three sequential sub-CHGs, each with its own validation matrix, each gated on its predecessor closing GREEN.**

### CHG-002a — Infrastructure Scale-Up + Shared-Service HA + Housekeeping

Scope: Phase 0 (T0.1 PG bump, T0.2 mgmt scale-up, T0.3 Chaos Mesh install) + Phase 2 (T2.1 Gitea HA + NFS, T2.2 Harbor HA + Redis externo, T2.3 MinIO standalone→distributed migration, T2.4 tenant-health Grafana dashboard) + Phase -1-derived housekeeping (k8s version sync staging/prod, lokiPushUrl Service-DNS, helm-chart upstream mappings, PSA audit of 4 mgmt components, MAINTENANCE_MODE flag).

This is the largest sub-CHG (~22 files) but the most predictable: every change is a Pulumi config edit, Helm value change, or new chart wrapper consumed by the existing 3-phase pipeline (`createInfra → bootstrapCluster → exposePlatform`).

Pre-conditions for opening:
- CHG-001 GREEN.
- `docs/runbooks/minio-distributed-migration.md` published (BLOCKING — not "before T2.3", but "before CHG-002a opens").
- DO billing forecast attached (delta vs Phase -1).
- ADR-0016 (this) merged.
- PSA audit of `mgmt-cloud-service.ts`, `mgmt-tenant-controller.ts`, `mgmt-gitea.ts`, `mgmt-api.ts` complete.

### CHG-002b — Load Validation (L3 Stabilization + L4 Capacity)

Scope: Phase 1 (T1.1 L3 diagnostic, T1.2-T1.4 root-cause fixes + 5x consecutive L3 ≥95/100 — Gate #1 partial progress) + Phase 3 (T3.1 L4 500-tenant run with capacity extrapolation).

This sub-CHG is empirical and bounded by *time*, not by code. Each L3 run produces metrics; iteration may surface architectural bugs (BuildKit concurrency ceiling, ArgoCD ApplicationSet single-threading per CNOE benchmark) requiring separate ADRs. CHG-002b is time-boxed at 5 calendar days. If L3 stays <95/100 after iteration, CHG-002b returns "INSUFFICIENT" — a re-meeting decides next phase.

Pre-conditions for opening:
- CHG-002a GREEN (validation matrix all-green).
- Phase 2 HA stable >24h continuous.
- L3 cost/budget approved by SRE.

### CHG-002c — Gate 2a Exit + Chaos + Observability

Scope: Phase 4 (T4.1 Gate 2a Exit checklist with independent reviewer sign-off) + Phase 5 (T5.1 9-scenario chaos against DO, T5.2 chaos failure fixes) + Phase 6 (T6.1 5 critical metrics + 12 orphan alert deletion, T6.2 `docs/slo.md` + 13 missing runbooks, T6.3 Promtail validation across 3 clusters).

This sub-CHG closes Sprint 8. It re-runs L3 to validate Gate 2a Exit threshold (5x ≥98/100, distinct from CHG-002b's Gate #1 5x ≥95/100). It records the chaos video that ADR-0010 §F mandates. It signs Gate 2a Exit with independent reviewers per line item.

Pre-conditions for opening:
- CHG-002b GREEN.
- Phase 2 HA stable >24h continuous since CHG-002a closure.
- Sprint 6 policy-controller in dry-run for chaos namespaces (alignment with the Audit→Deny observation window).

---

## What Changes vs ADR-0010

| Dimension | ADR-0010 (rescope) | ADR-0016 (this) |
|---|---|---|
| Phase 2a delivery shape | "3 sprints, 7 deliverables" | Three sequential sub-CHGs (002a + 002b + 002c) within Sprint 8 |
| Gate criteria | 7 deliverables + chaos + 5x L3 ≥98/100 | Same gates, but split: 002b targets 5x ≥95/100 (Gate #1 partial); 002c re-runs for 5x ≥98/100 (Gate 2a Exit) |
| Big-bang option | Implicit single-CHG path | Explicitly rejected (epic-CHG anti-pattern) |
| Validation contract | Per-deliverable acceptance | Per-CHG enumerated matrix (18 + 8 + 12 items) |
| Cell-2 activation | Phase 2b (Sprint 9) | Unchanged — explicitly out of scope |

ADR-0010's high-level architectural choices (cell-based, PG as routing authority, central ArgoCD through Phase 2, 5k apps/cell) are **not** changed. This ADR sharpens execution scope only.

---

## Decisions

### D-1 — Three CHGs, not one (epic-CHG anti-pattern)

**Decision:** Phase 0-6 splits into CHG-002a + CHG-002b + CHG-002c, sequential, each gated on its predecessor.
**Rationale:** Three qualitatively different uncertainty cones (mechanical, empirical, evaluative) cannot share one acceptance gate. Security (VETO on A) + SRE (VETO on A) + Production Design (REJECT on C) + 6 other reviewers concur. Big-bang means partial-CHG that takes weeks to disentangle. MEET-051 and MEET-060 establish the precedent.
**Consequences:** ~3 days of additional approval ceremony. Three independent acceptance windows.

### D-2 — Adopt-or-create policy extends to all stateful primitives in CHG-002

**Decision:** ADR-0015's adopt-or-create primitive applies to any stateful resource touched in CHG-002 (PG resize, MinIO migration, Gitea PVC, Harbor backend). New stateful resources (DO managed Redis for Harbor, NFS provisioner, MinIO distributed cluster) follow create-not-rename pattern.
**Rationale:** Prevents zombie state recurrence. Renaming live primitives breaks Helm values referencing endpoint strings.
**Consequences:** New components must support `import` directives in Pulumi for future re-bootstrap.

### D-3 — MinIO migration uses blue/green with 48h overlap and atomic credential rotation

**Decision:** T2.3 keeps old MinIO running for 48h post-cutover; endpoint flip is a Service rename, not a chart uninstall; new MinIO root credentials issued BEFORE flip; old credentials revoked AFTER ESO consumer namespaces refresh.
**Rationale:** SRE VETO threshold (data loss) + Security VETO threshold (credential rotation atomicity). `mc mirror` is best-effort — blue/green with `mc diff` zero-loss check is the only safe pattern.
**Consequences:** ~$40/month additional storage during 48h overlap. Migration runbook is a CHG-002a pre-condition deliverable.

### D-4 — Chaos Mesh installed with hardened RBAC; Audit/Deny alignment with Sprint 6

**Decision:** Chaos Mesh Helm values: `dashboard.enabled: false`, `controllerManager.replicaCount: 1`, RBAC scoped to `chaos-mesh` + 6 explicit allowed-target namespaces (`gitea`, `harbor`, `argocd`, `loopzero-api`, `loopzero-system`, `minio`). NO `cluster-admin` ClusterRoleBinding. T5.1 chaos namespaces enter policy-controller dry-run during chaos; revert to enforcement post-T5.2.
**Rationale:** Security VETO on default RBAC. Sprint 6 (cosign + policy-controller Audit) Deny-flip is pending observation; chaos with policy-controller live in Deny conflates failure attribution.
**Consequences:** Chaos blast bounded to platform namespaces only. Operator-supervised window for Audit/Deny coordination.

### D-5 — Gitea HA via NFSv4 only; StorageClass scoped to `gitea` namespace

**Decision:** T2.1 NFS PVC mounts via NFSv4 (`vers=4` validated pre-cutover). StorageClass scoped to `gitea` namespace via `allowedTopologies` or `csi.storage.k8s.io/provisioner-secret-namespace`. NetworkPolicy on `gitea` namespace allows ingress only from `argocd`, `loopzero-api`, internal `gitea` pods.
**Rationale:** Security VETO on horizontal escape via shared filesystem. GitOps engineer's NFSv3 silent-corruption risk on git ref locks (`HEAD.lock` requires POSIX `O_EXCL` on NFSv4).
**Consequences:** RWX is contained. Concurrent push under load must be load-tested explicitly.

### D-6 — Harbor Redis credential via ESO ExternalSecret, never Helm values literal

**Decision:** T2.2 DO managed Redis password lives in K8s Secret managed by ExternalSecret; Harbor chart consumes Secret reference, not literal value.
**Rationale:** Security VETO on credential exposure surface.
**Consequences:** ESO `ClusterSecretStore` for DO must be configured before T2.2 executes.

### D-7 — MAINTENANCE_MODE flag is a CHG-002a pre-flight, not Phase 0 task

**Decision:** API exposes `MAINTENANCE_MODE` env; `withEnvironmentLock` returns 503 with `Retry-After: 600` when set. CLI translates to user-friendly message respecting Rule 3 (no internal terms).
**Rationale:** T0.1 PG resize 5min downtime is unavoidable (DO managed PG resize is failover). Without MAINTENANCE_MODE, in-flight `loopzero deploy` sees generic 503. DX + SRE conditional approval depends on this.
**Consequences:** Small API + CLI delta. Sets precedent for future maintenance windows (Sprint 9 cell-2 cutover, future PG sharding).

### D-8 — Gate threshold disambiguation (Gate #1 vs Gate 2a Exit)

**Decision:** ADR-0010 defines two distinct L3 thresholds. CHG-002b targets 5 consecutive ≥95/100 (Gate #1 partial — full Gate #1 of 10x deferred to Sprint 9 if still gapped). CHG-002c re-runs L3 to satisfy Gate 2a Exit threshold of 5 consecutive ≥98/100. Conflating the two is the principal failure mode of self-attestation.
**Rationale:** Production Design Reviewer + Validation Planner concur. ADR-0010's two thresholds exist for a reason; collapsing them undermines the rescope.
**Consequences:** CHG-002c L3 re-run is non-optional — even if CHG-002b ends with 5x ≥98/100 already, the post-chaos run is a separate validation.

### D-9 — T4.1 Gate 2a Exit checklist requires independent reviewer per line item

**Decision:** Each of the 7 ADR-0010 deliverables in T4.1 is signed off by a reviewer who did not implement that task. Self-attestation is banned for Gate 2a Exit.
**Rationale:** Production Design Reviewer's framing: ADR-0010 was written *because* the prior framing allowed self-asserted "Phase 2 done" claims. Repeating the pattern here defeats the rescope.
**Consequences:** Sign-off coordination overhead. Acceptable cost.

### D-10 — Cell-2 activation is OUT OF SCOPE for CHG-002

**Decision:** CHG-002 unblocks Phase 2b; it does not deliver it. `tenants.cell_id = 2` placement, `ClusterCredential` CR for runtime-2, per-cell ArgoCD topology decisions all live in Sprint 9 (Phase 2b).
**Rationale:** ADR-0010 boundary explicit. Conflating "Gate 2a passed" with "cell-2 live" is the investor-narrative drift this ADR exists to prevent.
**Consequences:** Sprint 9 opens fresh meeting referencing ADR-0010 + ADR-0016.

### D-11 — Telemetry-derived housekeeping lands in CHG-002a

**Decision:** k8s version sync (`Pulumi.staging.yaml` + `Pulumi.prod.yaml` to `1.33.9-do.3`), `lokiPushUrl` Service-DNS replacement, helm-chart upstream-repo mappings (5 charts), PSA audit of 4 `mgmt-*.ts` components — all inside CHG-002a.
**Rationale:** Same files, same approval window, same Pulumi pipeline. Splitting housekeeping into a 4th CHG produces approval-thrash without blast-radius reduction.
**Consequences:** CHG-002a is the largest sub-CHG; manageable because changes are mechanical.

### D-12 — Chaos video as deliverable, not demo

**Decision:** T5.1 records `mp4` per scenario, ≥5min duration, with `kubectl get pods -A --watch` overlay for evidentiary value. Archived to `${HOME}/.loopzero/artifacts/`.
**Rationale:** ADR-0010 §F: "the chaos video IS the deliverable for 'cell failure isolated', not a slide."
**Consequences:** Recording overhead. Cost minimal vs evidentiary value.

### D-13 — CHG-002b time-boxed at 5 calendar days; INSUFFICIENT return is honest outcome

**Decision:** CHG-002b explicitly time-boxed. If L3 stays <95/100 after 5 days of iteration, CHG-002b returns "INSUFFICIENT". A re-meeting (not silent extension) decides whether to advance to CHG-002c with documented Gate #1 gap, or extend Phase 1 in a new CHG.
**Rationale:** L3 stabilization is empirical; uncertainty must be respected. Silent extension produces the same self-attestation failure that drove ADR-0010.
**Consequences:** Possible re-meeting for L3-extension or scope redirection.

---

## Alternatives Rejected

### Option A — Big-bang single CHG-002

Unanimous rejection. Three live-data mutations (PG resize, MinIO migration, chaos injection) under one approval = unbounded blast radius. Security VETO + SRE VETO + Production Design "epic-CHG anti-pattern". MEET-051 and MEET-060 precedent.

### Option C — Defer Phase 4 + 5 + 6 to Sprint 9

Production Design Reviewer rejection: deferring chaos validation repeats the exact failure mode ADR-0010 was written to prevent. "Phase 2a done" without chaos video is unproven Gate 2a. Sprint 9 would inherit the same "is this actually done?" ambiguity.

### Option D (proposed mid-meeting) — Split CHG-002a further into Phase 0 alone + Phase 2 alone

Discussed informally. Rejected: Phase 0 (PG bump, mgmt scale-up, Chaos Mesh install) and Phase 2 (HA migrations) overlap on Pulumi.dev.yaml + Helm chart files; splitting them produces two CHGs that touch the same files within the same approval-cycle — net negative blast-radius gain.

---

## Consequences

### Positive

1. **Three independent acceptance gates.** Each CHG closes (or returns) on a yes/no contract. No partial-CHG ambiguity.
2. **MinIO migration runbook becomes a contract, not aspirational.** Pre-condition deliverable forces it to exist before any code touches MinIO data.
3. **MAINTENANCE_MODE flag generalizes beyond CHG-002.** Sprint 9 cell-2 cutover, future PG sharding, any other live-data operation inherits the primitive.
4. **Chaos video as evidence.** ADR-0010 §F deliverable shipped with mechanical sign-off, not slide-asserted.
5. **Independent reviewer on T4.1.** Self-attestation is structurally banned for Gate 2a Exit.
6. **Housekeeping reconciled.** k8s version drift + lokiPushUrl + PSA audit + helm-chart mappings closed in CHG-002a, not deferred.

### Negative

1. **Approval ceremony cost.** Three sub-CHGs × ~1 day each = 3 days of governance overhead.
2. **CHG-002a is large.** ~22 files. Manageable because mechanical, but reviewer attention budget must not split.
3. **MinIO 48h blue/green overlap = ~$40/mo extra DO storage during transition.** Negligible vs data-loss risk.
4. **Recording chaos video adds operational overhead.** Acceptable cost.
5. **CHG-002b INSUFFICIENT return possible.** Architectural bugs may surface (BuildKit ceiling, ArgoCD bottleneck per CNOE benchmark) requiring separate ADRs. Honest outcome; better than silent extension.

### Risks Accepted with Mitigation

| Severity | Risk | Mitigation | Owner |
|---|---|---|---|
| HIGH | T0.1 PG resize 5min API blackout | MAINTENANCE_MODE flag + scheduled window + Retry-After header | SRE |
| HIGH | T2.3 MinIO migration data loss | Blue/green 48h, `mc diff` zero-loss check, atomic credential rotation, runbook pre-published | SRE+Security |
| HIGH | T0.2 mgmt rolling upgrade pod cascade | Pre-flight kubectl-wait template; controller leader-re-election validation post-upgrade | SRE |
| HIGH | Chaos Mesh over-broad blast | Hardened Helm values: dashboard off, RBAC scoped, no cluster-admin, 6 allowed targets | Security |
| HIGH | NFS RWX horizontal escape | NetworkPolicy + scoped StorageClass + NFSv4 explicit | Security+GitOps |
| HIGH | L3 stays <95/100 after iteration | CHG-002b time-boxed 5 days; INSUFFICIENT return triggers re-meeting | Architect |
| MEDIUM | Self-attestation on Gate 2a Exit | T4.1 independent reviewer per line item | Production Design |
| MEDIUM | T5.1 chaos vs Sprint 6 Deny-flip | Chaos namespaces in policy-controller dry-run during T5.1 | Security |
| MEDIUM | Harbor Redis password leak | ESO ExternalSecret, never Helm values literal | Security |
| MEDIUM | `lokiPushUrl: 10.20.0.26:30100` brittle | Service DNS replacement in CHG-002a | SRE |
| MEDIUM | k8s version drift staging/prod | Sync to `1.33.9-do.3` in CHG-002a | Platform Architect |
| LOW | DO billing variance | SRE-approved forecast attached at CHG-002a opening | SRE |
| LOW | First-user MAINTENANCE 503 ergonomics | CLI translation to friendly message | DX |

### Risks NOT Accepted (Blockers)

- T2.3 MinIO migration without published runbook — SRE VETO, absolute.
- Chaos Mesh with default cluster-admin RBAC — Security VETO, absolute.
- NFS RWX with default StorageClass — Security VETO, absolute.
- Harbor Redis password literal in Helm values — Security VETO, absolute.
- T5.1 chaos with policy-controller live in Deny mode — Security VETO, absolute.
- Gate 2a Exit checklist self-signed — Production Design VETO, absolute.
- T0.1 PG resize without MAINTENANCE_MODE — DX + SRE VETO.
- L3 runs without per-run telemetry capture — Validation Planner VETO.

---

## Implementation Phases

### CHG-002a (~2 weeks)

See `docs/plans/sprint8-hardening-chaos-plan.md` v3.3 §Phase 0 + §Phase 2 + §Housekeeping. 22-file scope, 18-item validation matrix.

### CHG-002b (5-day time-box)

See `docs/plans/sprint8-hardening-chaos-plan.md` v3.3 §Phase 1 + §Phase 3. 4-file scope, 8-item validation matrix.

### CHG-002c (~1 week)

See `docs/plans/sprint8-hardening-chaos-plan.md` v3.3 §Phase 4 + §Phase 5 + §Phase 6. 10-file scope, 12-item validation matrix.

### Downstream (Sprint 9, separate meeting required)

- Phase 2b (cell-2 routing activation): `ClusterCredential` for runtime-2, `tenants.cell_id` placement, capacity-gated sticky placement, kubeconfig Secret rotation, per-tenant MinIO STS, per-cell ArgoCD migration plan.
- ADR-0011 (Gitea Phase 3 scaling decision) if CHG-002b surfaces central-Gitea bottleneck under L4.

---

## Execution Contract

| Role | Owner | Deliverable |
|---|---|---|
| Sponsor | Platform Architect | This ADR + runbook + plan v3.3 |
| Technical lead CHG-002a | Infra Engineer | 22-file scope, 18-item matrix GREEN |
| Technical lead CHG-002b | Infra Engineer + SRE | L3+L4 logs, 8-item matrix GREEN |
| Technical lead CHG-002c | Infra Engineer + Production Design | Gate 2a Exit + chaos video + 12-item matrix GREEN |
| Security gate | Platform Security Engineer | RBAC + ESO + NFS scope + chaos namespace policy alignment |
| SRE gate | SRE Engineer | Runbook published + MAINTENANCE_MODE + blue/green MinIO + billing forecast |
| Validation planner | Change Validation Planner | Three matrices in MEET-061 §Stage 2 referenced from plan v3.3 |
| Production design gate | Production Design Reviewer | Independent reviewer sign-off on T4.1 + chaos video format |
| DX gate | DX Engineer | MAINTENANCE_MODE CLI message respects Rule 3 |
| Architect gate | Principal Architect | Time-box CHG-002b; INSUFFICIENT return path; D-8 thresholds |

### Definition of Done — ADR-0016 (the rescope)

The CHG-002 strategy is COMPLETE when:
1. This ADR is merged.
2. Plan v3.3 published reflecting three sub-CHG envelopes + per-CHG validation matrices.
3. Runbook `docs/runbooks/minio-distributed-migration.md` published.
4. CHG-002a opened (after pre-conditions met) and closed GREEN against 18-item matrix.
5. CHG-002b opened (after CHG-002a GREEN) and closed GREEN or INSUFFICIENT against 8-item matrix.
6. CHG-002c opened (after CHG-002b GREEN) and closed GREEN against 12-item matrix.
7. Chaos video archived to `${HOME}/.loopzero/artifacts/`.
8. Maturity assessment updated (target ≥4.0).
9. CLAUDE.md sprint status reflects Sprint 8 closeout.
10. Sprint 9 (Phase 2b cell-2 activation) opens with a fresh meeting referencing ADR-0010 + ADR-0016.

Any subsequent proposal to re-fuse the sub-CHGs, skip chaos video, or self-attest Gate 2a Exit requires a superseding ADR with new MEET record.

---

## References

- ADR-0008 — Cell-Based Architecture (50k apps, 5k apps/cell)
- ADR-0010 — Phase 2 Multi-Cell Activation Rescope (the gates this ADR's CHGs satisfy)
- ADR-0015 — Pre-bootstrap Adopt-or-Create Policy (CHG-001 product)
- MEET-2026-04-17-051 — Phase 2 rescope (epic-CHG anti-pattern precedent)
- MEET-2026-04-27-060 — CHG-001 Bootstrap (predecessor)
- MEET-2026-04-27-061 — This ADR's source meeting
- `docs/plans/sprint8-hardening-chaos-plan.md` v3.2 (to be revised to v3.3 reflecting this ADR)
- `/home/paulo/.loopzero/artifacts/validation-20260427-134037.log` (CHG-001 GREEN evidence)
- `infra/pulumi/Pulumi.dev.yaml`, `Pulumi.staging.yaml`, `Pulumi.prod.yaml`
- `infra/pulumi/src/index.ts`, `src/components/{infra,mgmt-gitea,harbor-bootstrap}.ts`
- CLAUDE.md Rules 1, 3, 6, 10
- `.claude/rules/pre-release-governance.md`
- CNOE Argo CD 50k Benchmark — https://cnoe.io/blog/argo-cd-application-scalability
- GitHub argo-cd #19113 ApplicationSet bottleneck — https://github.com/argoproj/argo-cd/discussions/19113
