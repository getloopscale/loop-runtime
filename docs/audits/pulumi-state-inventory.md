# Pulumi State Inventory — T0.2

> Plan reference: `docs/plans/infra-normalize-multicell-plan.md` Phase 0 / T0.2.
> Decision driver: `docs/decisions/MEET-INFRA-NORMALIZE-T3-pulumi-consolidation.md` (verdict: APPROVED_WITH_CONDITIONS, 29 conditions).
> Sibling artifacts: `docs/audits/pulumi-consolidation-log.md` (append-only execution log).

This document inventories the **offline-discoverable state** of every Pulumi directory in the repo. Live state (`pulumi stack ls`, exported snapshots, DO resource cross-reference) is **deferred** until backend access is restored — see "Live verification — pending" section at the bottom for the exact commands and expected outputs.

---

## Filesystem inventory — 4 paralelas + 1 shared lib

| Dir | Pulumi.yaml `name` | Stacks declared | @pulumi/kubernetes? | LOC (TS) | Verdict (offline) |
|---|---|---|---|---|---|
| `infra/pulumi` | `loopzero-infra` | dev, prod, staging | ❌ none | 13 269 | **CANONICAL** — preserve |
| `infra/pulumi-infra` | `loopzero-infra` ⚠️ | dev, homolog | ✅ in `package.json` only | 2 231 | **REMOVE** — name collision + violates ADR-0020 D7 |
| `infra/pulumi-platform` | `loopzero-platform` | dev | ✅ 8 source files + package.json | 3 882 | **REMOVE** — paralela superseded by helmfile L2 (ADR-0021) |
| `infra/pulumi-control-plane` | `loopzero-control-plane` | dev | ✅ 8 source files + package.json | 1 735 | **REMOVE** — paralela superseded by Helmfile L3 / ArgoCD |
| `infra/pulumi-shared` | _(no Pulumi.yaml)_ | _(none)_ | — | 60 | **REVIEW** — utility lib (`stack-outputs.ts`), check if any non-paralela imports it |

**Total LOC slated for removal:** 7 848 lines across 3 paralelas dirs (after `pulumi-shared` review).

### Critical finding: project-name collision

`infra/pulumi/Pulumi.yaml` and `infra/pulumi-infra/Pulumi.yaml` both declare `name: loopzero-infra`. Running `pulumi up` from either dir against the same backend would write into the SAME namespace → state corruption. This is precisely the failure mode flagged by ADR-0020 D7 (`@pulumi/kubernetes` ban + single-project rule).

Mitigation: any operator command on `infra/pulumi-infra` MUST use a distinct `--stack` to avoid cross-write. Long term: code deletion (T3.3) eliminates the risk.

---

## Stack-level inventory (offline-discoverable)

### infra/pulumi (CANONICAL — preserve)

| Stack | Pulumi.{stack}.yaml present? | Recommendation |
|---|---|---|
| `dev` | ✅ | KEEP — active dev environment |
| `prod` | ✅ | KEEP — production state |
| `staging` | ✅ | KEEP — staging environment |

### infra/pulumi-infra (PARALELA — remove)

| Stack | Pulumi.{stack}.yaml present? | Recommendation (subject to live verification) |
|---|---|---|
| `dev` | ✅ | `pulumi stack rm` after archive (T3.2) — assume orphan unless live state proves resources |
| `homolog` | ✅ | `pulumi stack rm` after archive — `homolog` is legacy provision-homolog naming, no longer in CI |

### infra/pulumi-platform (PARALELA — remove)

| Stack | Pulumi.{stack}.yaml present? | Recommendation |
|---|---|---|
| `dev` | ✅ | `pulumi stack rm` after archive |

### infra/pulumi-control-plane (PARALELA — remove)

| Stack | Pulumi.{stack}.yaml present? | Recommendation |
|---|---|---|
| `dev` | ✅ | `pulumi stack rm` after archive |

**Total stacks to archive + remove (subject to live verification):** 4 (`pulumi-infra/{dev,homolog}` + `pulumi-platform/dev` + `pulumi-control-plane/dev`).

---

## Source-code references — `@pulumi/kubernetes` violations

Per ADR-0020 D7 (Fix #6/#7 in `infra/pulumi/src/__tests__/sprint8-fixes.test.ts`), the canonical `infra/pulumi/` is K8s-clean. The 3 paralelas violate this:

| Dir | Source files importing @pulumi/kubernetes | Why it matters |
|---|---|---|
| `pulumi-infra` | `package.json` declares dep but src/*.ts does not import (transitively wired via stack export) | Removing the dir removes the dep entirely |
| `pulumi-platform` | `src/index.ts` (1 file) | Direct K8s resource creation = duplicates Helmfile L2 |
| `pulumi-control-plane` | `src/cp-ingress.ts`, `src/cp-ory-kratos.ts`, `src/cp-cloud-service.ts`, `src/cp-ory-hydra.ts`, `src/cp-network.ts`, `src/cp-api-service.ts`, `src/cp-tenant-controller.ts`, `src/index.ts` (8 files) | Most-divergent paralela; reimplements helmfile-managed services |

After T3.3 code deletion: `sprint8-fixes.test.ts` Fix #6 expands from "infra/pulumi clean" to "no @pulumi/kubernetes anywhere in repo".

---

## Live verification — pending (cloud + backend access)

The following data CANNOT be collected offline because it requires `pulumi` CLI authenticated against the S3 backend (`s3://loopzero-infra-pulumi-state` per CLAUDE.md). Once an operator with `PULUMI_CONFIG_PASSPHRASE` + `AWS_ACCESS_KEY_ID/SECRET_ACCESS_KEY` is available, the following commands close the inventory:

```bash
# For each paralela dir, list stacks and export state.
for d in pulumi-infra pulumi-platform pulumi-control-plane; do
  cd /home/paulo/loopzero/infra/$d
  pulumi stack ls --json > ~/.loopzero/archive/pulumi-state-archive/${d}-stacks.json
  for stack in $(pulumi stack ls --json | jq -r '.[].name'); do
    pulumi stack export --stack "$stack" > ~/.loopzero/archive/pulumi-state-archive/${d}-${stack}.json
  done
done

# Cross-reference with live DO resources.
doctl compute droplet list --format ID,Name,Region,Tags --no-header > ~/.loopzero/archive/do-droplets.txt
doctl kubernetes cluster list --format ID,Name,Region --no-header > ~/.loopzero/archive/do-clusters.txt
doctl databases list --format ID,Name,Engine --no-header > ~/.loopzero/archive/do-databases.txt
doctl spaces ls > ~/.loopzero/archive/do-spaces.txt 2>/dev/null
```

**Expected outcome (per existing audit `pulumi-consolidation-log.md` 2026-04-30 entry):** all 4 paralela stacks return zero or stale resources. Any stack returning live DO resources halts T3.2 stack rm and escalates to a separate migration runbook (per condition C9).

**If live verification surfaces resources:** append a new entry to `pulumi-consolidation-log.md` (per condition C15 append-only audit trail).

---

## Recommendation summary

| Recommendation | Stacks | Action |
|---|---|---|
| **rm** (after archive) | `pulumi-infra/dev`, `pulumi-infra/homolog`, `pulumi-platform/dev`, `pulumi-control-plane/dev` | Phase 3 T3.2 — gated on live verification |
| **preserve** | `pulumi/dev`, `pulumi/prod`, `pulumi/staging` | Canonical — no action |
| **escalate** | _(none expected; pending live verification)_ | Halt T3.2 + RCA if any |

---

## T0.2 Acceptance Criteria — current state

| Criterion | Status |
|---|---|
| Inventory doc committed | ✅ this file |
| Archives present per stack with resources | ⏸️ pending — `pulumi stack export` blocked on backend access |
| Recommendation per stack (rm \| preserve \| escalate) | ✅ provisional offline; live verification will confirm or escalate |

---

## References

- Plan: `docs/plans/infra-normalize-multicell-plan.md` Phase 0 / T0.2 + Phase 3 (T3.1, T3.2, T3.3).
- Decision: `docs/decisions/MEET-INFRA-NORMALIZE-T3-pulumi-consolidation.md` (29 conditions, especially C9 / C13 / C14 / C15 / C22 / C26).
- Execution log: `docs/audits/pulumi-consolidation-log.md` (append-only).
- Runbooks: `docs/runbooks/pulumi-consolidation.md`, `docs/runbooks/pulumi-state-restore.md`.
- Scripts: `infra/scripts/pulumi-archive.sh`, `infra/scripts/pulumi-orphan-rm.sh`.
- ADRs: ADR-0020 D7 (`@pulumi/kubernetes` ban), ADR-0021 (Helmfile L2 supersedes pulumi-platform).
- Test gate: `infra/pulumi/src/__tests__/sprint8-fixes.test.ts` Fix #6 + Fix #7.
