# Pulumi state inventory (T0.2)

> **CHG-2026-04-30-INFRA-NORMALIZE Phase 0 deliverable.** Inventory of Pulumi
> stacks + paralelas directories before the consolidation to a single project.
> Phase 3 deletes the paralelas; this doc is the audit trail.

## Executive summary

Three "paralelas" Pulumi directories existed alongside the canonical
`infra/pulumi`: `pulumi-infra`, `pulumi-platform`, `pulumi-control-plane`.
They were deprecated drafts from Sprint 7 layered-bootstrap exploration.
Critical finding: project-name collision between paralelas and canonical
caused stack list ambiguity. Phase 3 removes the paralelas; this inventory
preserves the audit trail.

## Filesystem inventory

| Directory | Status | Recommendation |
|---|---|---|
| `infra/pulumi` | CANONICAL | preserve — single Pulumi project (post-consolidation) |
| `infra/pulumi-infra` | paralela (deprecated) | rm — Phase 3 deletion |
| `infra/pulumi-platform` | paralela (deprecated) | rm — Phase 3 deletion |
| `infra/pulumi-control-plane` | paralela (deprecated) | rm — Phase 3 deletion |
| `infra/pulumi-shared` | shared TS modules | preserve — referenced by canonical |

## Stack-level inventory

| Stack | Project | Backend | Status |
|---|---|---|---|
| `dev` | `infra/pulumi` (canonical) | s3://loopzero-infra-pulumi-state | ACTIVE |
| `staging` | `infra/pulumi` (canonical) | s3://loopzero-infra-pulumi-state | ACTIVE |
| `prod` | `infra/pulumi` (canonical) | s3://loopzero-infra-pulumi-state | ACTIVE |
| (paralelas) | `infra/pulumi-{infra,platform,control-plane}` | s3 (orphan stacks) | escalate to archive |

## Critical findings

### Critical finding #1: project-name collision

Two of the paralelas declared `name: loopzero-platform` in their `Pulumi.yaml`,
colliding with each other (and conflicting with operator/sprint expectations).
A `pulumi stack ls --all` returns ambiguous results until paralelas are
removed. This is the load-bearing reason Phase 3 deletes them entirely
rather than just excluding from CI.

### Critical finding #2: orphaned cloud resources

Some paralelas-created resources may persist in DigitalOcean (unmanaged
DOKS clusters from early sprints). `infra/scripts/pulumi-orphan-rm.sh
--dry-run` enumerates them; operator decides per-resource: rm / preserve /
escalate.

## Live verification gate

Backend access (s3://loopzero-infra-pulumi-state) is required to enumerate
authoritative state. Live commands:

```bash
# Stack-level enumeration (requires AWS_PROFILE with read access to s3 backend)
pulumi stack ls --all

# Per-stack export (idempotent — read-only)
pulumi stack export --stack dev --file /tmp/dev-state.json

# Orphan detection (compares state to declared resources in code)
bash infra/scripts/pulumi-orphan-rm.sh --dry-run

# Archive (irreversible — Phase 3)
bash infra/scripts/pulumi-archive.sh --stack <name>
```

These commands are gated by backend access. Without it, this inventory is
filesystem-only; Phase 3 closure requires backend confirmation.

## References

- ADR-0021 D2
- `infra/scripts/pulumi-archive.sh` — archive automation
- `infra/scripts/pulumi-orphan-rm.sh` — orphan detection + removal
- `docs/runbooks/pulumi-state-cleanup.md` — operational runbook
- `docs/runbooks/pulumi-state-repair.md` — repair under failure
- `docs/runbooks/pulumi-state-restore.md` — disaster recovery
