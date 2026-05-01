# Pulumi Consolidation Audit Log

> Plan reference: `docs/plans/infra-normalize-multicell-plan.md` Phase 3.
> Source decision: `docs/decisions/MEET-INFRA-NORMALIZE-T3-pulumi-consolidation.md` (condition C15).
> Append-only.

This file is an **append-only audit trail** of every action taken during Phase 3 (Pulumi consolidation). Per condition C15, no entry is rewritten or deleted; corrections are appended as new entries with explicit references to the entry being corrected.

---

## 2026-04-30 — Phase 3 deliverables shipped (offline)

**Author.** Claude Code (Ralph loop iter 12).
**Action.** Shipped offline-feasible Phase 3 deliverables ahead of execution:
- `infra/scripts/pulumi-archive.sh` (C14) — archive script.
- `infra/scripts/pulumi-orphan-rm.sh` (C14) — stack rm script.
- `docs/runbooks/pulumi-consolidation.md` (C10) — runbook covering T3.1+T3.2+T3.3.
- `docs/runbooks/pulumi-state-restore.md` (C16) — URN-rewrite restore procedure.
- `infra/helm/monitoring/manifests/prometheusrule-pulumi-state.yaml` (C20) — `PulumiArchiveStaleness` + `PulumiOrphanStackDetected` alerts.
- `api/internal/metrics/metrics.go` (C20) — `loopzero_pulumi_archives_total` + `loopzero_pulumi_stacks_total` gauges.
- This audit log file (C15).

**State transitions.** None — no Pulumi state mutated; no S3 archives created; no `pulumi stack rm` executed. Phase 3 EXECUTION (T3.1/T3.2/T3.3) remains BLOCKED on:
- Pulumi backend access from a privileged operator (cloud cred).
- 14-day spacing between commits per C13.
- Replay drill within 14 days of T3.2 per C22 / C26.

**Verification.** `task validate:matrix --quick` — 9/9 PASS. Build clean for api + operators with new metrics. Bash syntax check passes for both scripts.

**Next entry.** Will record T3.1 archive run (commit 1 of 3 per C13). Operator must:
1. Set `PULUMI_CONFIG_PASSPHRASE`, `AWS_*` env.
2. Run `bash infra/scripts/pulumi-archive.sh --dry-run` first.
3. If dry-run output looks correct, `bash infra/scripts/pulumi-archive.sh` live.
4. Append entry here with archive count, S3 prefix, snapshot SHAs.

---

## 2026-04-30 — C28 finding: seed_e2e.go is canonical, KEEP

**Author.** Claude Code (Ralph loop iter 13).

**Action.** Read `infra/scripts/seed_e2e.go` to determine scope per condition C28 ("scope verified by reading; relocated or deleted per finding").

**Finding.** The script is a 60-line wrapper around `infra/tests/seed/seed.go`. Per its file header: "thin wrapper around the seed package for standalone use ... delegates all seeding logic to infra/tests/seed and adds an optional API health check". It seeds E2E test data (Postgres rows + Tenant CR + namespace reconcile) — none of which is paralelas-specific.

`grep -rn 'scripts/seed_e2e'` returns:
- `Taskfile.yml:517` — `task` invocation
- `Taskfile.yml:1145` — `task` invocation
- `CHANGELOG.md:1706` — release note from previous refactor

NO references in `infra/pulumi-{infra,platform,control-plane}/` or `provision-homolog.sh`. `awk '/(homolog|pulumi-platform|pulumi-infra|pulumi-control-plane)/' seed_e2e.go` returns zero matches.

**Decision.** Per C28 ("relocated or deleted per finding"), **KEEP** at its current path `infra/scripts/seed_e2e.go`. Plan T3.3 must NOT include this file in the delete list. The plan was wrong; this commit patches the plan to align with the C28 finding.

**State transitions.** None — file is unchanged.

**Verification.** Cross-referenced with `infra/tests/seed/seed.go` (the package it delegates to) — same finding. Both Taskfile call sites (`task: dev:seed:e2e` at 517, and the `task: e2e:run` at 1145) remain functional.

**Plan amendment.** `docs/plans/infra-normalize-multicell-plan.md` patched to remove `seed_e2e.go` from the T3.3 delete list. Reference: this audit entry.

**Next entry.** Operator-driven T3.1 archive run (per first entry of this log).

---

## Schema for future entries

Each entry MUST contain:

```
## YYYY-MM-DD — <one-line summary>

**Author.** <agent or human name>.
**Action.** <what was done; reference scripts + commits>.
**State transitions.** <what cloud / Pulumi state changed; resource counts before/after>.
**Verification.** <evidence the action succeeded; SHAs, exit codes, alert state>.
**Next entry.** <what's expected next, who's the next actor>.
```

If correcting an earlier entry: prefix the corrected entry with `> CORRECTS entry of YYYY-MM-DD: <reason>` and reference the original by date in the body.
