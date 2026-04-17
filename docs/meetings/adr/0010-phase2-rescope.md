# ADR-0010: Phase 2 Multi-Cell Activation — Rescope

**Status:** ACCEPTED
**Date:** 2026-04-17
**Supersedes:** Phase 2 scope as framed in ADR-0008 §Implementation Phases (single monolithic phase)
**Origin:** MEET-2026-04-17-051 — unanimous 6-specialist rescope with triple VETO on original framing
**Deciders:** Principal Architect, Distributed Systems, SRE, Platform Security, GitOps, Production Design Reviewer

---

## Context

### The Problem

ADR-0008 (2026-03-31) approved cell-based architecture for 50k-app scale. Phase 0 was deployed (`cell_id`, `PlacementEngine`, `DynamicRouter`, `LOOPZERO_DYNAMIC_ROUTER=true`). Phase 1 reached ~75% code completion: `clustercredentials.loopzero.io` CRD installed, `ClusterCredentialReconciler` written, Pulumi `cell.ts` scaffolded.

The platform stalled with 100 tenants all in `cell-default` and a single workload cluster. User authorized DO quota + $400-800/mo for a 2nd cell, framing the urgency as investor-facing scalability proof.

Orchestrator proposed activating Phase 2 as a single milestone: provision cell-2, extend CF Worker + KV for cross-cell routing, go live.

### The Problem With That Proposal

The MEET-051 meeting with 6 Staff/Principal specialists surfaced that this framing was architecturally and operationally unsound on three independent dimensions:

1. **Shared services are SPOFs that multi-cell amplifies, not isolates.**
   Production Design Reviewer: "2 cells sharing 1 Gitea/Harbor/MinIO/PG is an architecture diagram, not a cell. ADR-0008's promise 'cell failure affects only tenants in that cell' is architecturally false in Phase 2 as proposed."

2. **Single cell not yet stable.** L3 best = 75/100, v14 = 62/100. Gate #1 (10× ≥95/100 consecutive) unmet. Scaling an unstable primitive compounds risk.

3. **CF Worker + KV cross-cell is wrong data-path design.**
   - Architect: creates second control plane with its own state + deploy cycle + failure mode outside the cluster; debuggability collapses.
   - DistSys: KV is eventually consistent (60s+ p99); stitching CP authority to AP cache without fallback = 404 on first request after tenant creation.
   - Named failure: "Day 2, 15:32 UTC tenant created → PG commit → KV write queued → user deploy hits FRA edge, KV miss, 404. Retry hits IAD, KV hit, works. User files 'flaky deploys.' Unfixable without PG fallback."

### Evidence Base

| Source | Finding |
|---|---|
| MEET-051 (this ADR's predecessor) | 6-way unanimous veto on original framing; 34 veto conditions across specialists |
| Production Design analysis | PG connection pool at 72% idle saturation; mgmt cluster at 74% memory; single Gitea/Harbor/MinIO |
| CNOE ArgoCD benchmark | Central ArgoCD survives ≤500 tenants; per-cell mandatory at 500+ |
| GitHub argo-cd #19113 | ApplicationSet controller single-threaded, reconcile degrades linearly with app count |
| L3 iterations v1-v14 | Build success 66% → 96% via software fixes; app-live capped at 62-75/100 by shared-service + runtime capacity |
| Industry DNS routing patterns | Fly.io (BGP Anycast, infeasible DO); Vercel (Worker + KV for static, App Runner proxy for dynamic); Heroku/Render (regional subdomains, breaks custom domains) |

---

## Decision

**Rescope Phase 2 into two sequential milestones: Phase 2a (Shared-Service Hardening) and Phase 2b (Cell-2 Activation).** Phase 2b is gated on Phase 2a Exit. Neither may be skipped or fused.

Additionally, **reject CF Worker + KV as the dynamic-routing authority.** The canonical routing decision is made from PostgreSQL (`tenants.cell_id`) via an API data-path proxy in the mgmt cluster. CF Worker retains static-delivery responsibility only. KV, if used at all, is a cache with PG-backed fallback on miss — never authority.

### Phase 2a — Shared-Service Hardening (prerequisite)

Seven concrete deliverables before any cell-2 work:

1. Close Gate #1 — L3 10× consecutive ≥ 95/100
2. PG bump — `max_connections ≥ 100` (db-s-2vcpu-4gb minimum)
3. Gitea HA — complete beyond bfbe818 Redis partial: shared storage + 2 replicas + load test 200 concurrent clones
4. Harbor HA — 2+ core replicas + Redis HA + S3 shared
5. MinIO distributed migration — per existing `docs/plans/minio-distributed-migration.md`
6. mgmt cluster memory headroom — below 50% idle
7. Unified `tenant-health` Grafana dashboard — answer "tenant X at which cell, with what status at each stage" from one panel

**Gate 2a Exit:** all seven done + chaos test proving each shared service can be killed independently with recovery < 2min + 5 consecutive L3 ≥ 98/100.

### Phase 2b — Cell-2 Activation

Seventeen tasks (see `docs/plans/cell-architecture-phase2.md` §Phase 2b for full breakdown). Key architectural commitments:

- **Cell source of truth:** `ClusterCredential` CR (Pulumi produces, controller reconciles, API reads). `tenants.cell_id` in PostgreSQL is operational pointer only, not declarative.
- **Routing authority:** PostgreSQL. API data-path proxy reads with 30s cache. CF Worker stays for static delivery. KV, if used, is cache with PG fallback on miss.
- **Placement algorithm:** sticky-at-creation + capacity-gated + `pg_advisory_xact_lock(hash('placement'), hash(cell_id))` around capacity-check and assignment.
- **Kubeconfig:** Secret-stored with ≤24h rotation. **Never** in CRD spec or status.
- **MinIO credentials:** per-tenant STS with 15-min TTL issued at build submission.
- **ArgoCD:** central (single instance) through Phase 2b. Path-level cell encoding in Gitea: `gitops/cells/{cell-id}/{tenant}/{env}/`. Per-cell ArgoCD is Phase 3.
- **Migration:** explicitly out of scope for Phase 2. New tenants → cell-2 via capacity-aware sticky. Existing 100 tenants stay on `cell-default`. Migration mechanism = Phase 3.
- **Observability:** per-cell Prometheus with remote-write to central tier. Alertmanager `group_by: [cell]` + `cell:degraded` inhibit rules.
- **Security:** cosign verification enforced at each cell's admission controller; per-cell pull-only Harbor robot; per-cell ESO with scoped SA (no mgmt-proxy pattern); NetworkPolicy default-deny cross-cluster.

**Gate 2b Exit:** all 34 veto conditions across the 5 specialists validated + chaos suite (6 scenarios) passed + chaos video recorded showing cell-2 down while cell-1 apps serve 200 for 10 minutes (and reverse) + this ADR committed before implementation begins.

### Migration to Phase 3

Migration (tenant rebalancing across cells) is deferred to Phase 3 with an explicit state machine: `ACTIVE(cell-A) → PREPARING(cell-B) → DUAL → DRAINING(cell-A) → ACTIVE(cell-B)`. Finalizers prevent prune during DUAL/DRAINING. Phase 2b ships the types-only skeleton (`pkg/statemachine/migration.go`) — wiring is Phase 3.

---

## What Changes vs ADR-0008

| Dimension | ADR-0008 (original) | ADR-0010 (this) |
|---|---|---|
| Phase 2 shape | Single monolithic milestone | Two sequential sub-phases (2a + 2b) |
| Routing data-plane | Undefined | PG-as-authority + API proxy; CF Worker rejected for dynamic routing |
| Placement algorithm | "PlacementEngine" unspecified algorithm | Sticky-at-creation + capacity-gated + advisory lock |
| Kubeconfig storage | Unspecified | Secret-stored, ≤24h rotation, never in CRD spec |
| MinIO credentials | Implied shared | Per-tenant STS at build time |
| Migration | In Phase 1-2 scope | Deferred to Phase 3 |
| ArgoCD topology | "1 instance current → 2 → N" | Central through Phase 2; per-cell starts Phase 3 |
| Shared-service state | "Central, evaluate at Phase 3" | HA required before Phase 2b (explicit prerequisite) |

ADR-0008 high-level architectural choice (cell-based, 5k apps/cell, linear scaling) is **not** changed. Phase boundaries and gate criteria are sharpened.

---

## Alternatives Rejected

### A — Proceed as originally framed (monolithic Phase 2)

Unanimous rejection by 6 specialists. Core defect: cell failure isolation is not deliverable while Gitea/Harbor/MinIO/PG are single-instance. Shipping would produce a demo that lies about the architecture.

### B — CF Worker + KV as routing authority

Rejected by Architect (creates second control plane, debuggability collapse) and DistSys (KV is AP, binding is CP — no fallback path = 404s on fresh tenants). KV retained as optional cache with PG-backed fallback, never as authority.

### C — Per-cell Gitea replicas

Rejected by GitOps. Dual-write split-brain during concurrent gitops push across cells is unresolvable without substantial additional design. Central Gitea with HA (Phase 2a) is acceptable; per-cell or clustering decision is Phase 3 via ADR-0011.

### D — Auto-sync on any cell (e.g., "canary cell = auto-sync")

Rejected by GitOps. Violates ADR-0007 (manual sync is the intent/execution boundary). Build/release decisions belong to the API, not ArgoCD. Any cell with auto-sync would contaminate that boundary.

### E — Include tenant migration in Phase 2

Rejected unanimously. Migration is a cold-move problem (drain builds, re-push gitops, re-sync ArgoCD, re-issue certs, update DNS, coordinated state transfer). It deserves its own phase with explicit state machine + finalizer protocol. Phase 2b ships new tenants to cell-2 only.

### F — Proceed without chaos proof as DoD

Rejected by SRE and Production Design. A "production-ready" claim with no failure-injection evidence is not production-ready. The chaos video (cell-2 down, cell-1 apps serve 200 for 10 min) IS the deliverable for "cell failure isolated", not a slide.

---

## Consequences

### Positive

1. **Honest scalability story.** Investor demo shows chaos video of actual failure isolation, not a topology diagram.
2. **Blast radius truly contracts.** With Phase 2a, shared-service failure stops being universal; with Phase 2b, cell failure is cell-local.
3. **Routing is debuggable.** PG as single authority means `SELECT cell_id FROM tenants WHERE id = X;` answers "which cell?" with one query; no CF dashboard + KV replica topology required.
4. **Migration path is explicit.** State machine + finalizers defined before Phase 3 needs it.
5. **Security controls are non-optional.** 8 security veto conditions bind Phase 2b from the start — no retrofit.

### Negative

1. **Longer calendar time.** Phase 2a adds 3 sprints before cell-2 is live. Investor narrative must lead with "Phase 2a is the prerequisite for the real cell-2 demo", not "we're slower than expected."
2. **Two gates, two sign-offs.** More ceremony. Acceptable trade for safety.
3. **CF Worker team loses scope.** Static delivery only. No dynamic-route use case on edge (at least in Phase 2).
4. **Higher baseline cost before cell-2.** Gitea HA + Harbor HA + MinIO distributed + PG bump add infra spend before cell-2 adds capacity.

### Risks Accepted with Mitigation

| Severity | Risk | Mitigation | Owner |
|---|---|---|---|
| HIGH | Central Gitea even with HA remains choke point at 5k+ tenants | ADR-0011 in Phase 3 decides clustering vs per-cell vs bare-git+S3 | GitOps |
| HIGH | Central PG shared across cells | Phase 2a bump to 100 connections; Phase 3 adds read replicas; Phase 4 ADR-0012 partition strategy | SRE + DistSys |
| MEDIUM | Central Harbor as supply-chain SPOF | Cosign verification per cell from Phase 2b; geo-replication in Phase 4 | Security |
| MEDIUM | Single mgmt cluster as control plane for all cells | Phase 3 T-3-06 adds mgmt HA | Architect |
| LOW | KV-as-cache still has stale-data window | PG fallback path mandatory on every Worker read; reconciliation cron every 5 min | DistSys |

### Risks NOT Accepted (Blockers)

- Kubeconfig in CRD spec — Security VETO, absolute.
- Placement without capacity advisory lock — DistSys VETO, absolute (TOCTOU).
- KV-as-authority without PG fallback — DistSys VETO.
- Shipping without chaos video — SRE + Production Design VETO.
- Migration in Phase 2 scope — unanimous VETO.

---

## Implementation Phases

### Phase 2a (3 sprints)

See `docs/plans/cell-architecture-phase2.md` §Phase 2a for the 7-task breakdown, sequence, and Gate Exit criteria.

### Phase 2b (5 sprints)

See `docs/plans/cell-architecture-phase2.md` §Phase 2b for the 17-task breakdown, sequence, and the 34 veto conditions mapped to specific tasks.

### Downstream (not in this ADR's scope)

- Phase 3 Automation — `docs/plans/cell-architecture-phase3.md`; will produce ADR-0011 (Gitea scaling decision).
- Phase 4 Optimization → 50k — `docs/plans/cell-architecture-phase4.md`; will produce ADR-0012 (PG sharding decision).

---

## Execution Contract

| Role | Owner | Deliverable |
|---|---|---|
| Sponsor (of rescope) | Production Design Reviewer | This ADR |
| Technical lead Phase 2a | Infra Engineer | 7 tasks + Gate 2a Exit |
| Technical lead Phase 2b | API + Operator Engineers | 17 tasks + Gate 2b Exit |
| Security gate | Platform Security Engineer | 8 veto conditions validated |
| SRE gate | SRE Engineer | 7 veto conditions + chaos video |
| Distsys gate | Distributed Systems Engineer | 8 invariants validated in envtest + staging |
| Architect gate | Principal Architect | 4 conditions (source of truth, routing degrade, state machine) |

### Definition of Done

The rescope is COMPLETE when:
1. This ADR is merged and referenced from `cell-architecture-overview.md`, `cell-architecture-phase2.md`, `50k-immediate-focus.md`, and MEET-051 record.
2. Phase 2a and Phase 2b plans (`docs/plans/cell-architecture-phase2.md`) cite this ADR by number.
3. Any subsequent proposal to re-fuse the phases or reintroduce CF Worker as dynamic-routing authority requires a superseding ADR with new MEET record.

---

## References

- ADR-0008: Cell-Based Architecture — `docs/meetings/adr/0008-cell-based-architecture.md`
- ADR-0007: ArgoCD Application Model (manual sync) — `docs/meetings/adr/0007-argocd-application-model.md`
- MEET-2026-04-17-051: Phase 2 rescope meeting — `docs/meetings/records/MEET-2026-04-17-051.md`
- MEET-2026-03-31-024: ArgoCD scalability — `docs/meetings/records/MEET-2026-03-31-024.md`
- Plan — Phase 2 executable: `docs/plans/cell-architecture-phase2.md`
- Plan — overview: `docs/plans/cell-architecture-overview.md`
- Plan — 50k load-test progression: `docs/plans/scale-50k-plan.md`
- CNOE Argo CD 50k Benchmark — https://cnoe.io/blog/argo-cd-application-scalability
- GitHub argo-cd #19113 ApplicationSet bottleneck — https://github.com/argoproj/argo-cd/discussions/19113
- Fly.io Cell-Based Architecture — https://fly.io/docs/blueprints/cell-based/
