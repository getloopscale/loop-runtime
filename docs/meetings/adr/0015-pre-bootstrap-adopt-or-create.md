# ADR-0015: Pre-bootstrap Adopt-or-Create Policy + Sprint 8 Phase -1 Scope

**Status:** ACCEPTED
**Date:** 2026-04-27
**Origin:** MEET-2026-04-27-060 — 9-reviewer unanimous approval of Option B (Bootstrap-only CHG-001)
**Deciders:** Principal Architect, Platform Architect (Sponsor), GitOps, K8s Controller, Platform Security (veto-conditional), SRE (challenger, veto-conditional), Change Validation Planner, Production Design Reviewer, DX

---

## Context

LoopZero Cloud DigitalOcean infrastructure is currently empty (`doctl kubernetes cluster list` and `doctl databases list` both return no results). Pulumi backend `loopzero-homolog-pulumi-state` carries a zombie `homolog` stack with 41 outputs claiming live infrastructure but only 7 managed resources. The VPC `loopzero-infra-dev-sfo3-net` (10.20.0.0/16) and DigitalOcean Container Registry `loopzero-system` are live and not under Pulumi management.

Sprint 8 v3.1 (`docs/plans/sprint8-hardening-chaos-plan.md`) proposes 23 tasks bundling bootstrap (Phase -1), infra scale-up (Phase 0), HA hardening (Phase 2), L3/L4 stabilization (Phase 1/3), chaos validation (Phase 5), and observability (Phase 6) under a single change ticket.

ADR-0010 mandates seven Phase 2a deliverables before cell-2 activation. Zero are currently met because no infrastructure exists to measure them on. The first `pulumi preview` of stack `dev` failed with two errors, the load-bearing one being `infra/pulumi/src/components/infra.ts:282` constructing the registry name as `${config.project}-registry` = `loopzero-infra-registry`, while the live DOCR is `loopzero-system`.

A meeting was convened (MEET-2026-04-27-060) under the pre-release governance T3 protocol with 9 reviewers and explicit veto rights for Security, SRE, and DX.

## Decision

### 1. Sprint 8 Scope Split

Split Sprint 8 v3.1 into two sequential change tickets:

- **CHG-001 — Phase -1 Bootstrap.** Five files only (see §Implementation). Acceptance is a 16-item validation matrix codified in MEET-2026-04-27-060 §Stage 2 (Change Validation Planner). Phases 0-6 are explicitly out of scope.
- **CHG-002 — Phase 0-6 Hardening.** Requires a new technical meeting after CHG-001 acceptance. Scope, validation, and risk mitigations to be re-derived from telemetry observed on the freshly bootstrapped infrastructure.

The original "big-bang" Option A is rejected as an epic-CHG anti-pattern that MEET-2026-04-17-051 already rejected when scoping Phase 2 itself. The redesign Option C (application sharding instead of cell architecture) is rejected because it discards `runtimeCluster2` wiring already committed in `infra/pulumi/src/components/infra.ts:160-175` and rewinds the architectural commitment of ADR-0008.

### 2. Adopt-or-Create as Default Policy for Live Stateful Resources

When a `pulumi up` of a fresh stack encounters a pre-existing live resource that holds state or carries downstream identity (registry name, VPC CIDR, DNS records, DO Spaces buckets, managed databases), the default policy is **adopt via `import` ID lookup**, never recreate or rename.

This policy already exists in `infra/pulumi/src/core/adopt-or-create.ts` for VPC and Cloudflare resources. It is now extended to DigitalOcean Container Registry. Future stateful resource types follow the same pattern: a `lookupExisting<Resource>(name)` function returns an import ID that the Pulumi component consumes via `{ import: <id> }`.

**Renaming live resources is prohibited** unless an explicit superseding ADR justifies it on a per-resource basis. The rationale: every live stateful resource has consumers (image pulls, DNS resolution, secret references) whose configuration cannot be atomically updated with the rename. Adoption preserves consumer integrity.

### 3. Operational Hardening for Phase -1

CHG-001 incorporates six operational mitigations vetoed-into-existence by Security and SRE:

| Concern | Mitigation |
|---|---|
| Secret-bearing Pulumi exports in world-readable `/tmp` | Backup to `${HOME}/.loopzero/backups/` with `chmod 600` on creation; retention until Phase -1 declared green |
| Four cluster admin kubeconfigs aggregated in default `~/.kube/config` | Per-cluster file `${HOME}/.kube/loopzero-${cluster}.yaml`, `chmod 600`, `KUBECONFIG` exported per session |
| `pulumi up` 45-90min unattended | Foreground-only execution; operator stays present |
| DO droplet quota silent exhaustion | Pre-flight `doctl account get` validates ≥ 12 droplet headroom |
| Mid-run partial failure with no recovery path | Runbook `docs/runbooks/sprint8-bootstrap.md` published before T-1.3 executes |
| Smoke E2E project residue | T-1.5 acceptance includes `loopzero project delete --force` + Gitea repo absence verification |

## Alternatives Rejected

### Option A — Big-bang Sprint 8 v3.1 under one CHG

Rejected unanimously by 9 reviewers. SRE veto: epic-CHG bundles bootstrap (one-shot, externally observable) with hardening (multi-week, telemetry-driven) under a single approval. Failure of `pulumi up` would block unrelated observability and Helm changes that have no causal dependency on bootstrap. Production Design noted the same anti-pattern was rejected for Phase 2 scope in MEET-2026-04-17-051.

### Option B — Bootstrap-only CHG-001 + Hardening CHG-002 (chosen)

Approved unanimously. Trade-off: doubles approval ceremony. Compensating benefit: blast radius narrowed to a single externally-verifiable acceptance test; CHG-002 scope can be refined using telemetry from a real bootstrapped stack.

### Option C — Redesign to application-level sharding

Rejected by Principal Architect. Discards ADR-0008 cell architecture and the `runtimeCluster2` IaC commitment. No reviewer endorsed C on technical grounds.

### Option D — Rename live DOCR `loopzero-system` to match Pulumi default

Considered and rejected during meeting Conflict 2. Security objection: every Helm values reference and image-pull configuration in the cluster pulls from `registry.digitalocean.com/loopzero-system/...`. A rename triggers cascade of value-file edits and image republishes with no operator-visible benefit. Adoption is the strictly safer path.

### Option E — Defer registry to Helm-only (skip Pulumi management)

Rejected by Platform Architect. The registry is a stateful primitive other Pulumi resources reference (Container Registry binding for DOKS pull credentials). Removing it from Pulumi management creates a bootstrap order problem.

## Consequences

### Positive

1. **Narrow blast radius.** Phase -1 succeeds or fails as one observable unit with 16 enumerated checks.
2. **Honest sequencing.** Phase 0-6 work is gated on the existence of telemetry, not on optimism about scope.
3. **Adoption policy is explicit.** Future drift cases (registry rename, VPC CIDR change, bucket name divergence) have a default answer and a referenceable ADR.
4. **Security posture preserved.** Six operational mitigations land before the first `pulumi up` rather than after the first incident.
5. **Runbook precedent.** `docs/runbooks/sprint8-bootstrap.md` is the first canonical bootstrap runbook for LoopZero Cloud.

### Negative

1. **Two CHGs, two approvals.** Approximately one extra day of governance lead time before Phase 0 work starts.
2. **Phase -1 success ≠ Gate 2a closer in calendar.** The plan must communicate that bootstrap unblocks observation, not deliverables.
3. **Adoption locks naming.** Once `loopzero-system` is adopted, renaming becomes a two-meeting decision (this ADR + a superseding ADR). Acceptable.

### Risks Accepted with Mitigation

| Severity | Risk | Mitigation | Owner |
|---|---|---|---|
| HIGH | `pulumi up` partial failure mid-T-1.3 | Runbook published before T-1.3 | SRE |
| HIGH | DO droplet quota exhaustion | Pre-flight `doctl account get` (T-1.0d) | Infra |
| MEDIUM | Pulumi state corruption | `${HOME}/.loopzero/backups/` with retention | SRE |
| MEDIUM | Kubeconfig aggregation surface | Per-cluster file with chmod 600 | Security |
| MEDIUM | Stale `lokiPushUrl` IP in Pulumi.dev.yaml | CHG-002 follow-up; not blocking Phase -1 | SRE |
| LOW | Smoke project residue | T-1.5 cleanup acceptance criteria | GitOps |

### Risks NOT Accepted (Blockers)

- Secrets/state files in `/tmp` — Security VETO, absolute.
- Kubeconfigs merged into `~/.kube/config` — Security VETO, absolute.
- Renaming live DOCR — Security + Production Design VETO, absolute.
- T-1.3 without runbook — SRE VETO, absolute.
- T-1.3 without DO quota check — SRE VETO, absolute.
- `pulumi up` in background — SRE VETO, absolute.

## Implementation

### CHG-001 Approved Files (narrow scope)

```
docs/plans/sprint8-hardening-chaos-plan.md          (plan v3.2 reflecting mandatory actions)
docs/meetings/adr/0015-pre-bootstrap-adopt-or-create.md   (this ADR)
docs/runbooks/sprint8-bootstrap.md                  (NEW — operational runbook)
infra/pulumi/src/core/adopt-or-create.ts            (add lookupExistingRegistry)
infra/pulumi/src/components/infra.ts                (line 282 → consumes orphans.registryImportId)
```

### Out of Scope for CHG-001 (deferred to CHG-002)

```
infra/pulumi/Pulumi.dev.yaml
infra/helm/gitea/**, infra/helm/harbor/**, infra/helm/minio/**
infra/helm/monitoring/**
api/internal/metrics/metrics.go
infra/pulumi/src/components/mgmt-gitea.ts, harbor-bootstrap.ts
docs/slo.md
docs/runbooks/alerts/**
```

### Mandatory Actions Before CHG-001 Implementation

1. ADR-0015 merged (this document).
2. Plan v3.2 reflects file-mode, kubeconfig, runbook, quota check, and smoke cleanup updates.
3. Runbook published.
4. `lookupExistingRegistry` added to `core/adopt-or-create.ts`.
5. `infra.ts:282` consumes import ID via `{ import: orphans.registryImportId }`.
6. `pulumi up --stack dev --yes` executes in foreground with backup pre-created.
7. Validation matrix (16 items) all green.
8. Smoke project deleted; Gitea repo verified empty.

### Definition of Done — CHG-001

CHG-001 is complete when all 16 validation-matrix items pass, the smoke project is cleaned up, `pulumi preview` post-up shows no drift, and the operator log of `pulumi up` is archived to `${HOME}/.loopzero/artifacts/` for post-mortem reference.

CHG-002 may not open until CHG-001 is closed.

## References

- MEET-2026-04-27-060 — Meeting record producing this ADR
- ADR-0008 — Cell-Based Architecture
- ADR-0010 — Phase 2 Multi-Cell Activation Rescope
- MEET-2026-04-17-051 — Phase 2 rescope (precedent for splitting epic-CHGs)
- `docs/plans/sprint8-hardening-chaos-plan.md`
- `infra/pulumi/src/index.ts`
- `infra/pulumi/src/components/infra.ts`
- `infra/pulumi/src/core/adopt-or-create.ts`
- CLAUDE.md Rules 1, 3, 6, 10
