# MEET-INFRA-NORMALIZE-T3 — Pulumi Consolidation (T3 Governance Review)

**Date:** 2026-04-30
**Topic:** Pulumi project consolidation — archive + rm of 3 paralelas dirs (`docs/plans/infra-normalize-multicell-plan.md` ADR D2)
**Type:** `architecture` + `gitops` + `security` + `pipeline` (cross-cutting, state-lifecycle)
**Tier:** T3 (Pulumi state lifecycle + cloud billing exposure + IaC topology)
**Scope:** Plan Phase 0 / T0.2 (inventory) → Phase 3 / T3.1–T3.3 (archive + stack rm + code delete)
**Out of scope:** Operator topology cutover (D1 — see MEET-INFRA-NORMALIZE-T3-operator-split.md), multi-cell activation (D3), CLI cluster-real tests (D4)

**Agents convened:** platform-architect (Sponsor) · platform-security-engineer (Veto) · gitops-engineer (Veto) · sre-engineer (Veto) · change-validation-planner

**Owner (post-approval):** infra-engineer (lead) + sre-engineer (co-signs runbook) + platform-security-engineer (co-signs archive ACL)

**Verdict:** **APPROVED_WITH_CONDITIONS**

---

## Stage 1 — Context

The repository carries 4 distinct Pulumi project directories rooted under `infra/`:

| Dir | `Pulumi.yaml` name | Wired by | Imports `@pulumi/kubernetes`? | Status |
|-----|---------------------|----------|-------------------------------|--------|
| `infra/pulumi/` | `loopzero-infra` | `cli/src/installer/pulumi-runner.ts:resolvePulumiProjectDir` (ADR-0020 D2 canonical) | No (per Fix #6+#7 in `infra/pulumi/src/__tests__/sprint8-fixes.test.ts`) | **CANONICAL** |
| `infra/pulumi-infra/` | `loopzero-infra` (**collision**) | `infra/scripts/provision-homolog.sh` only | n/a (cloud-only) | orphan |
| `infra/pulumi-platform/` | `loopzero-platform` | `infra/scripts/provision-homolog.sh` only | YES — 5+ files (`services/{gitea,argocd,cert-manager,prometheus,grafana,tempo}.ts`, `components/{runtime-bootstrap,mgmt-gitea-bootstrap,mgmt-platform-services,bootstrap-shared}.ts`) | **violates ADR-0020 D7** |
| `infra/pulumi-control-plane/` | `loopzero-control-plane` | `infra/scripts/provision-homolog.sh` only | YES — 1 file | **violates ADR-0020 D7** |
| `infra/pulumi-shared/` | n/a (no `Pulumi.yaml` — single `stack-outputs.ts`) | imported by `pulumi-platform/src/contracts.ts` and `pulumi-control-plane/src/contracts.ts` only | n/a | orphan |

Two structural problems:

1. **Project-name collision.** `infra/pulumi/Pulumi.yaml` and `infra/pulumi-infra/Pulumi.yaml` both declare `name: loopzero-infra`. Pulumi state on `s3://loopzero-infra-pulumi-state` is partitioned by `<project-name>/<stack-name>/`. A `pulumi up` from either dir against the same stack name (`dev`, `homolog`, etc.) would write to the **same backend prefix** — silent state collision risk. CHANGELOG line 364 (declarative multi-cluster topology) references `pulumi-infra/src/topology/` directly, indicating real history of state writes from that dir, not just dead code.
2. **ADR-0020 D7 violation.** `pulumi-platform` and `pulumi-control-plane` import `@pulumi/kubernetes`, mixing L1 (cloud) and L2 (Kubernetes) concerns the canonical `infra/pulumi/` has been deliberately purged of (Sprint 8 v3.2 / CHG-2026-04-28-LAYERED-BOOTSTRAP T1.1).

The 3 paralelas dirs are referenced **only** by `infra/scripts/provision-homolog.sh` (which provisions a `homolog` env not named in CLAUDE.md "Production Infrastructure" section) and by `docs/runbooks/alerts/LoopZeroCFTunnelDisconnected.md:48` (an outdated runbook line still pointing to `infra/pulumi-platform`).

### Decision required

Adopt **Option C** of plan ADR D2 (line 94): for each paralelas stack, perform a state-lifecycle dance — `pulumi stack export` (archive) → cross-reference archived URNs against live DO/CF resources (verify) → `pulumi stack rm --yes` only if confirmed empty → `git rm -r` the code dirs. **NOT** "just delete the code".

The "what if a stack has live resources we don't know about?" question is the load-bearing reason this is T3 and not T1 — a forgotten DOKS, Spaces bucket, PostgreSQL cluster, or CF Tunnel could continue to bill indefinitely after the dir is deleted, with no `pulumi destroy` path remaining (because the dir is gone). DO list price for a single 3-node DOKS s-4vcpu-8gb-amd is ~$108/mo; a forgotten managed PG db-s-2vcpu-4gb is ~$60/mo. Plausible undetected exposure: $200–$500/mo per orphan stack.

### Veto criteria from plan ADR D2 (lines 99–101)

- **Platform-architect (line 99):** "at least one stack must show zero live DO resources before any rm". Otherwise we don't know what we're orphaning.
- **Security (line 100):** "archived state contains secrets — must encrypt at rest + restrict S3 ACL". Pulumi state holds: `digitaloceanToken`, `cfApiToken`, `spacesAccessKeyId`, `spacesSecretAccessKey`, `tunnelSecret`, `kubeconfig` for each cluster. Plaintext leak = takeover.
- **GitOps (line 101):** "any rm must be reproducible via runbook step (no ad-hoc destruction)". The decision must produce a runbook executable in a single CI job, not slack-coordinated `pulumi` invocations.
- **SRE (implicit, supplied by this meeting):** deletion must impact zero live monitoring/alerting paths; rollback path must restore both **state** and **resources** within a defined SLO.

### Pre-conditions inherited from sibling decisions

- **ADR-0020 D2** (`infra/pulumi/` is the only canonical wired Pulumi root) — already in force.
- **ADR-0020 D7** (zero `@pulumi/kubernetes` imports in canonical) — already in force per `sprint8-fixes.test.ts` Fix #6+#7. The paralelas dirs visibly violate D7; consolidation closes the violation by removing the violators.
- **ADR-0015** (`docs/meetings/adr/0015-pre-bootstrap-adopt-or-create.md`) — adopt-or-create policy treats Pulumi state and live cloud resources as two distinct lifecycles that must be reconciled. This decision applies the **inverse** of adopt-or-create: an *abandon-or-archive* lifecycle that must be equally rigorous.

---

## Stage 2 — Analyses

### platform-architect (Sponsor)

Option C is architecturally correct. Option A (delete code only) is a non-starter: it produces orphan billing risk plus a broken `pulumi destroy` recovery path (the dir holding the destroy entrypoint is gone). Option B (delete code + `stack rm` without archive) is recoverable only via `git restore` of deleted code from history — and `.claude/rules/workflow.md` line 33 forbids the destructive variants entirely; it also discards forensic evidence of what was provisioned.

**Risks I flag:**

1. **Project-name collision risk during inventory.** `pulumi stack ls` from `infra/pulumi-infra/` and from `infra/pulumi/` may return overlapping stack names mapped to the same backend prefix. T0.2 must enumerate stacks **per dir, with full backend URI** (`pulumi stack ls --json --backend-url s3://...`) to disambiguate.
2. **`pulumi-shared/stack-outputs.ts` is a type-only export.** Deleting it does not affect runtime state but the two consumers (`pulumi-platform/src/contracts.ts:5`, `pulumi-control-plane/src/contracts.ts:5`) must also be deleted. Ordering matters: paralelas dirs deleted first, then `pulumi-shared/` (or all in one commit; either way, the "delete pulumi-shared first" path leaves transient TypeScript compile errors on `develop`).
3. **`docs/runbooks/alerts/LoopZeroCFTunnelDisconnected.md:48`** still points to `infra/pulumi-platform`. T3.3 must update or delete that runbook reference.

**Recommendation: APPROVED with C1 (per-dir backend-explicit `stack ls`), C2 (atomic single-commit dir delete + runbook ref update), C3 (verification step covers ALL cloud providers, not just DO — Cloudflare resources too).**

### platform-security-engineer (Veto holder — secrets + archive ACL)

I traced what each stack's state actually contains:

- `pulumi-infra/` state (homolog): `digitaloceanToken`, `spacesAccessKeyId`, `spacesSecretAccessKey`, output `clusterRegistry` containing kubeconfigs (3 clusters per topology config), output `cellRegistry` mapping cells → cluster credentials. **HIGH SENSITIVITY.**
- `pulumi-platform/` state: `cfApiToken`, `tunnelSecret` (from `provision-homolog.sh:66`), Gitea admin password (created in `services/gitea.ts`), Argo Workflows S3 creds, ArgoCD admin creds. **HIGH SENSITIVITY.**
- `pulumi-control-plane/` state: API JWT signing keys, Kratos/Hydra config, IngressRoutes (low sensitivity but still PII paths).

`pulumi stack export` produces JSON containing `secrets` blocks encrypted with the **stack passphrase** (`PULUMI_CONFIG_PASSPHRASE`). The encrypted ciphertext is safe at rest in S3 only if (a) S3 ACL is private; (b) the passphrase is itself stored in a separate, encrypted secret store, not in the same S3 bucket; (c) bucket has SSE-S3 or SSE-KMS enabled.

**Veto criteria (each MUST be met before any `stack rm`):**

1. **Archive S3 bucket.** `s3://loopzero-archive/pulumi-state/` (NEW — not the existing state backend `s3://loopzero-infra-pulumi-state`, to enforce blast-radius separation). Bucket policy:
   - Private ACL (no public-read, no public-read-write).
   - SSE-KMS with a customer-managed key in the `loopzero-infra` AWS account.
   - Versioning enabled.
   - Object Lock (governance mode, 90-day retention) to prevent accidental destruction.
   - Bucket-level deny on `s3:DeleteObject` for everyone except the `loopzero-archive-rotator` IAM role.
2. **Archive object key format.** `pulumi-state/<dir-name>/<stack-name>/<ISO-8601-timestamp>.json.enc` (timestamped, immutable).
3. **Encryption-at-rest layered.** Pulumi's own passphrase-based encryption + SSE-KMS = belt and suspenders. The `PULUMI_CONFIG_PASSPHRASE` used to export must be re-entered or fetched from `mgmt-platform-secrets` at archive time, NOT stored alongside the archive.
4. **Rotation policy: 90 days.** Archives older than 90 days deleted by `loopzero-archive-rotator` Lambda unless `legal-hold: true` tag set. Documented in runbook D4.
5. **Access policy.** Only `infra-engineer` + `platform-security-engineer` IAM roles can read. Read access logged to CloudTrail with 1-year retention.
6. **No archive contains plaintext secrets.** Run `jq '.. | strings | select(test("token|password|secret|key"; "i"))' archive.json` post-export; expect zero matches outside encrypted `ciphertext` blocks. If any match → ABORT, escalate to me before proceeding.

**Additional vetoes:**

- **V-S1.** If T0.2 inventory reveals **any** stack with `cfApiToken` or `digitaloceanToken` in plaintext (legacy Pulumi version regression), I issue a HARD VETO until those tokens are rotated in DO/CF console and confirmed unused, regardless of whether the stack has live resources.
- **V-S2.** Archive bucket cannot share the same AWS account as production data. Cross-account is preferred. If single-account, separate IAM boundary policy.
- **V-S3.** Code deletion must include `infra/scripts/provision-homolog.sh` (which encodes how secrets flow into the orphan stacks); leaving the script alive after stack rm leaves a misleading recipe.

**Veto position: PARTIAL VETO** lifted CONDITIONAL on **C4** (S3 bucket spec + ACL committed as Pulumi resource in canonical `infra/pulumi/` BEFORE T3.1 archive runs), **C5** (passphrase handling per V-S1), **C6** (90-day rotation + Object Lock per veto criterion 1+4), **C7** (`provision-homolog.sh` deleted in same commit as paralelas dirs per V-S3), **C8** (CloudTrail audit logging on archive bucket for 1y), **C9** (post-export plaintext-secret regex scan).

### gitops-engineer (Veto holder — reproducibility + runbook)

Plan T3.1 / T3.2 / T3.3 in current form are an outline. The veto criterion (line 101) demands "reproducible via runbook step (no ad-hoc destruction)". My acceptance bar:

1. **Single executable runbook.** `docs/runbooks/pulumi-consolidation.md` with explicit shell blocks. Every `pulumi stack rm` must be preceded by a preflight check (Pulumi has no `--dry-run` for stack rm; use `pulumi stack export | jq '.deployment.resources | length'` and refuse rm if > 0).
2. **Runbook must be idempotent.** Re-running after partial completion converges (already-archived stacks skipped; already-removed stacks skipped). No "did you remember to do step 7" failure modes.
3. **Runbook must be CI-runnable.** A scheduled GitHub Action `.github/workflows/pulumi-archive.yml` invokes the runbook in `--archive-only` mode quarterly to re-archive any surviving stacks (defense in depth against forgotten state).
4. **Single git commit per phase.** Phase 3.1 (archive) = 1 commit producing `docs/audits/pulumi-state-inventory.md` + the archive S3 objects (verified via post-commit script, NOT committed to git themselves). Phase 3.2 (stack rm) = 1 commit producing audit log entry. Phase 3.3 (code delete) = 1 commit deleting dirs + `provision-homolog.sh` + updating runbook references. **Three commits, three approvals, three CI runs.**
5. **No ad-hoc `pulumi` invocations.** All shelled-out commands routed through `infra/scripts/pulumi-archive.sh` (NEW) and `infra/scripts/pulumi-orphan-rm.sh` (NEW), exit codes 0 (ok) / 2 (preflight failed) / 3 (live resources detected — abort) / 4 (archive failed). The canonical `loopzero platform` CLI does NOT need to learn about archive — this is an admin op.
6. **GitOps audit trail.** Each script run appends to `docs/audits/pulumi-consolidation-log.md` with `<timestamp> <user> <dir> <stack> <action> <result>` rows. ArgoCD does not manage this; it's pure operator runbook. But the audit MD must be checked into git so the trail is auditable post-merge.
7. **Rollback runbook.** A separate `docs/runbooks/pulumi-state-restore.md` documents `pulumi stack import < archive.json.enc` (after KMS decrypt + passphrase restore) into the canonical `infra/pulumi/`. The plan ADR D2 line 105 mentions this but does NOT define what "restore" actually produces: importing state into a different project name (`loopzero-infra` canonical vs. `loopzero-platform` archived) requires URN rewriting. Runbook must include the URN-rewrite step, with a worked example.

**Veto position: LIFTED CONDITIONAL on C10 (single runbook D4), C11 (idempotency test T-04), C12 (CI archive job D7), C13 (3-commit phasing C2 expansion), C14 (`pulumi-archive.sh` + `pulumi-orphan-rm.sh` with declared exit codes — D2/D3), C15 (audit log D5), C16 (URN-rewrite restore runbook D6).**

### sre-engineer (Veto holder — live-impact + rollback SLO)

The plan claims T3.1 is "read-only" (line 1010). I challenge that:

1. **`pulumi stack export` may mutate state.** Pulumi's exporter can rewrite snapshot v1→v3 schema versions on read in some CLI versions. T0.2 + T3.1 must pin `pulumi version` (e.g., 3.140.0) and run a snapshot diff `pulumi stack export | sha256sum` BEFORE and AFTER any modifying op. If sha changes during what should be a read-only op, ABORT.
2. **Live-resource verification is the load-bearing safety check.** Plan line 96 says "verify by listing live DO resources tagged with stack URN — if any return non-zero results, ABORT". I need this written as concrete commands, per provider:
   - **DigitalOcean:** `doctl compute droplet list --tag-name pulumi:Stack=<stack> --format ID,Name`; `doctl databases list --format ID,Name,Tags`; `doctl kubernetes cluster list --format ID,Name,Tags`; `doctl spaces ls`. Pulumi tagging convention: `pulumi:Stack`, `pulumi:Project`. NB: not all DO resource types support tags (managed PG does, Spaces does NOT — Spaces verification requires bucket-name pattern match `loopzero-*-homolog-*`).
   - **Cloudflare:** `cf zones list --account-id <id>`, `cf tunnel list --account-id <id>`, `cf workers-kv namespaces list`. Cloudflare does not propagate Pulumi tags; verification is name-pattern + cross-reference vs canonical `infra/pulumi/` outputs to identify "anything not owned by canonical".
   - **AWS (S3 state backend, AWS provider stub):** `aws s3 ls`, `aws iam list-users`. Stub provider may not have provisioned anything but verify regardless.
3. **Rollback SLO.** Plan ADR D2 line 105 admits "rollback ≠ instant for live resources". I need the rollback SLO quantified:
   - **State-only rollback** (re-import archive into canonical `infra/pulumi/`): ≤ 15 min wall-clock from decision-to-rollback to verified-restored state. Test this in dev.
   - **Resource rollback** (re-provision destroyed cloud resources): NOT in scope of this decision — if we accidentally `pulumi destroy` a live cluster, that's a P1 incident, not a rollback. Defense: T0.2 / T3.1 verification is rigorous so this never happens.
4. **Live-monitoring impact.** `loopzero_pulumi_archives_total` (plan line 1016) and `loopzero_pulumi_stacks_total` (plan line 1066) are NEW metrics. SRE must confirm:
   - Metrics scraped by Prometheus (mgmt cluster).
   - Alert rules: `PulumiArchiveStaleness` (no archive in 90 days, MEDIUM); NEW `PulumiOrphanStackDetected` (canonical scan reveals an unexpected stack name in any backend prefix, HIGH — fires if someone re-creates `pulumi-platform/`).
   - Dashboard panel in `infra/helm/monitoring/dashboards/` for archive lifecycle.
5. **No live alerts reference deleted resources.** Plan line 1058 says "T0.2 confirmed no". I require this confirmation be machine-checkable: `grep -rn "pulumi-infra\|pulumi-platform\|pulumi-control-plane" infra/helm/monitoring/ docs/runbooks/alerts/` returns 0. Today it returns 1 hit (`docs/runbooks/alerts/LoopZeroCFTunnelDisconnected.md:48`) — must be fixed.
6. **Archive replay drill.** Within 14 days of T3.2 completion, run a full restore of one archived stack into a sandbox project (NOT canonical) and verify the imported state diff against canonical resources is exactly `0 to update, 0 to create, 0 to delete`. This proves the archive is recoverable. Without this drill, the archive is hope, not insurance.

**Veto position: LIFTED CONDITIONAL on C17 (pulumi version pinned + snapshot-sha diff), C18 (per-provider verification commands D8), C19 (rollback SLO ≤ 15 min state-only, written + tested), C20 (`PulumiOrphanStackDetected` alert + scrape config D9), C21 (alert-doc grep clean, fixes `LoopZeroCFTunnelDisconnected.md:48`), C22 (archive replay drill within 14 days post-T3.2).**

### change-validation-planner

Plan T3.x has 3 unit-style TDD assertions per task. Bar for a T3 with cloud-billing exposure is materially higher. I propose this 14-test matrix:

| ID | Layer | What it validates | Owner |
|----|-------|-------------------|-------|
| T-01 | static | Per-dir `pulumi stack ls --json` enumerates ≥ 1 stack OR confirms 0 stacks | infra |
| T-02 | static | Project-name collision detected: `infra/pulumi-infra/Pulumi.yaml` name == canonical name (if true, log + warn; rm proceeds only after verification) | infra |
| T-03 | static | `infra/pulumi-shared/stack-outputs.ts` consumers enumerated; deletion order verified compile-clean | infra |
| T-04 | runbook | `infra/scripts/pulumi-archive.sh --dry-run` is idempotent: 2× run produces identical S3 archive object set | infra |
| T-05 | security | Archive S3 bucket ACL denies public; SSE-KMS enabled; Object Lock 90d; CloudTrail logs (C4 + C8) | security |
| T-06 | security | Post-export plaintext-secret regex scan returns 0 matches outside encrypted blocks (C9) | security |
| T-07 | security | Pulumi passphrase NOT colocated with archive (separate secret store, separate IAM boundary) (C5) | security |
| T-08 | verification | `doctl compute droplet list --tag-name pulumi:Stack=<stack>` returns 0 rows for each orphan stack (C18) | sre + infra |
| T-09 | verification | `doctl databases list` cross-reference: no DBs tagged with orphan stacks; manual review of untagged candidates | sre + infra |
| T-10 | verification | CF zones + tunnels + KV cross-reference vs canonical outputs returns no unaccounted entries (C18) | sre + infra |
| T-11 | rollback | State-only rollback drill: `pulumi stack import < archive.json.enc` into sandbox project, diff against canonical = 0/0/0 (C22 partial) | sre + infra |
| T-12 | gitops | `infra/scripts/pulumi-orphan-rm.sh` exit codes 0/2/3/4 enumerated by test cases (C14) | infra |
| T-13 | observability | `loopzero_pulumi_archives_total` + `loopzero_pulumi_stacks_total` + `PulumiOrphanStackDetected` alert all wired and scraped (C20) | sre |
| T-14 | regression | `find infra -name 'Pulumi.yaml' \| wc -l == 1` post-T3.3 (matches plan T3.3 RED test) + `grep -rn "pulumi-platform\|pulumi-control-plane\|pulumi-shared\|provision-homolog" .` returns only this decision record + plan + CHANGELOG `[Removed]` entry | infra |

**Recommendation: APPROVED CONDITIONAL on C23 (T-01..T-12 GREEN before T3.2 stack rm), C24 (T-13 GREEN within 7 days post-T3.3), C25 (T-14 GREEN before merge of T3.3), C26 (archive replay drill T-11 GREEN within 14 days post-T3.2 — blocks any subsequent paralelas-style project re-introduction).**

---

## Stage 3 — Confrontation

### Conflict 1 — "Read-only" claim of T3.1 archive vs. SRE pulumi-snapshot-mutation risk

Plan claims `pulumi stack export` is read-only. SRE flags that some Pulumi CLI versions auto-migrate snapshot schema on read. **Resolution:** **C17 mandatory** — pin `pulumi` version + snapshot-sha verification before/after every export. Plan T3.1 amended.

### Conflict 2 — Project-name collision: stack-rm target ambiguity

Architect flagged that `infra/pulumi-infra/Pulumi.yaml` and canonical share `name: loopzero-infra`. If T0.2 inventory uses backend-naive `pulumi stack ls`, results include canonical stacks too, which we must NEVER rm. **Resolution:** **C1 mandatory** — `pulumi stack ls` must be invoked with explicit `--cwd <dir>` and the resulting stack URI compared against the canonical's URIs; any URI shared between paralelas and canonical is an architectural anomaly that gets escalated, not auto-rm'd. Plan T0.2 amended.

### Conflict 3 — Archive bucket separation vs. operational simplicity

GitOps suggests reusing `s3://loopzero-infra-pulumi-state` (existing backend) with a `pulumi-state-archive/` prefix — fewer buckets, simpler IAM. Security counter-points: blast-radius separation is non-negotiable (if state-bucket is compromised, archive bucket must remain intact). **Resolution:** **C4 mandatory** — separate bucket `s3://loopzero-archive/pulumi-state/`, distinct IAM, distinct KMS key. Operational complexity accepted.

### Conflict 4 — Archive replay drill scheduling: pre-rm or post-rm?

SRE wants drill before T3.2 stack rm (highest insurance). Verification-planner accepts post-rm within 14 days (faster path; archive is already verified by sha256 + decrypt test). Architect: drill must use the **archived encrypted** state, not the live-state pre-rm — therefore drill is naturally post-archive. **Resolution:** **C22 + C26 mandatory** — drill runs within 14 days of T3.2; if drill FAILS, decision is RETROACTIVELY downgraded to REJECTED and rollback SLO C19 is invoked. This is the strongest "archive is recoverable" proof we can offer without delaying T3.2 indefinitely.

### Conflict 5 — Spaces (DO Object Storage) verification gap

SRE flagged Spaces does NOT support DO tags, so `doctl compute tag` cannot enumerate Spaces buckets. Architect proposes name-pattern fallback (`loopzero-*-homolog-*`) plus cross-reference vs. canonical `cellRegistry`/`spacesBuckets` outputs. **Resolution:** **C18 expanded** — verification per provider is a tabulated matrix, with explicit fallback methods declared per resource type (tag → name → URN → none). For "none", manual review with platform-architect signoff required.

### Conflict 6 — `infra/pulumi-shared/` deletion ordering

`pulumi-shared/stack-outputs.ts` is imported by paralelas dirs only. Canonical does not depend on it. Three orderings possible: (a) delete shared first → paralelas dirs have transient broken imports (CI red); (b) delete shared with paralelas in the same commit → 1 atomic commit, CI green; (c) delete shared last → paralelas dirs gone, shared has 0 importers, dead code briefly preserved. **Resolution:** **C2 — option (b)** atomic single-commit delete. T3.3 plan amended.

### Self-challenge (orchestrator) — three additional gaps

1. **Pulumi backend stack-history retention.** S3 backend stores stack history (`v1`, `v2`, ... per stack). `pulumi stack rm --yes` deletes the latest pointer but may leave historical snapshots in the bucket prefix. C27: post-rm cleanup script removes `<project>/<stack>/` prefix from `s3://loopzero-infra-pulumi-state/`.
2. **`infra/scripts/seed_e2e.go`** — plan T3.3 line 1086 includes deletion. This Go file's purpose is undocumented; it may seed test data for the paralelas dirs (if so, deletion is correct) OR it may seed for canonical (if so, deletion breaks E2E). C28: read `seed_e2e.go`, document its scope, delete only if paralelas-specific; otherwise relocate to `infra/pulumi/` or delete with explicit migration.
3. **CHANGELOG `[Removed]` entry must use product language (CLAUDE.md Rule 3).** "Removed orphan Pulumi projects" is fine; "Removed Gitea-bootstrapping pulumi-platform" mentions a forbidden internal term. C29: CHANGELOG language reviewed pre-commit.

---

## Stage 4 — Decision

**Chosen approach.** **Option C — archive + verify + rm** with mandatory conditions C1–C29.

**Alternatives considered & rejected.**

| Alternative | Rejection reason |
|-------------|------------------|
| **Option A — Delete code only** | Orphan billing exposure ($200–500/mo per stack uncertainty); no `pulumi destroy` recovery path remains; no forensic record of what was provisioned. Forbidden by veto criteria (architect line 99, security line 100, gitops line 101). **REJECTED.** |
| **Option B — Delete code + `pulumi stack rm`, no archive** | Closes billing exposure but discards forensic evidence; rollback only via destructive history operations forbidden by `.claude/rules/workflow.md` line 33; fails security veto criterion (no encrypted archive). **REJECTED.** |
| **Option C — Archive + verify + stack rm + code delete** (CHOSEN) | Closes billing exposure; preserves forensic evidence (90-day retention); recoverable via `pulumi stack import` + reverse-patch commits over the canonical history (NOT destructive history rewrites); satisfies all 4 veto criteria. Operational cost: 3 commits + 1 runbook + 1 archive bucket + 1 replay drill. Acceptable. |
| **Option D — Defer to post-launch** | Leaves project-name collision risk active (silent state corruption possible on accidental `pulumi up` from paralelas dir); leaves ADR-0020 D7 violation active; technical debt deepens daily. Pre-release governance principle (`.claude/rules/pre-release-governance.md` line 96): "We CAN break things and fix them properly". **REJECTED.** |

**Trade-offs consciously accepted.**

1. **Operational complexity: +1 S3 bucket, +1 KMS key, +1 IAM role, +2 scripts, +2 runbooks, +3 commits.** Mitigated by C12 (CI archive job runs unattended); pre-release acceptable burden.
2. **90-day retention is a window, not forever.** After 90 days, archives gone. Acceptable: by then T-11 replay drill has confirmed archive validity; any unanticipated need beyond 90 days reaches via `git log` of canonical commits + cloud provider audit logs. Legal-hold tag covers exceptional cases.
3. **`provision-homolog.sh` deletion eliminates the `homolog` env path entirely.** CLAUDE.md "Production Infrastructure" section names dev/prod only; homolog is not an active environment. Acceptable.
4. **`pulumi-shared/stack-outputs.ts` type definitions discarded.** If a future ADR re-introduces split projects, types must be re-declared. Acceptable: re-introduction requires its own T3 meeting per pre-release governance.

**Risks accepted (with mitigation + owner).**

| Risk | Sev | Prob | Mitigation | Owner |
|------|-----|------|------------|-------|
| Forgotten DOKS / managed PG / Spaces continues billing post-rm | HIGH | confirmed-possible | C18 per-provider verification (T-08/T-09/T-10) before any rm | sre + infra |
| Project-name collision causes accidental rm of canonical stack | CRITICAL | LOW | C1 backend-explicit `stack ls` + URI compare; C17 snapshot-sha diff | infra |
| Archive S3 bucket compromise leaks credentials | HIGH | LOW | C4 SSE-KMS + Object Lock + C5 separate-passphrase + C8 CloudTrail | security |
| Pulumi snapshot schema migration during "read-only" export mutates state | MED | LOW (version-dependent) | C17 pinned version + sha diff | sre |
| Archive recoverability untested → archive is hope not insurance | HIGH | confirmed-without-drill | C22 replay drill within 14d post-T3.2 | sre + infra |
| Plaintext token in legacy state reveals takeover-ready creds | CRITICAL | LOW (legacy CLI versions only) | C9 regex scan post-export; V-S1 hard-veto if found | security |
| `pulumi-shared/` consumers leave broken TS compile if order wrong | LOW | LOW | C2 single-commit atomic delete | infra |
| `seed_e2e.go` deletion breaks E2E suite | MED | UNKNOWN until C28 reads file | C28 scoped read + relocate-or-delete decision | infra |
| `LoopZeroCFTunnelDisconnected.md:48` runbook reference goes stale | LOW | confirmed | C21 grep clean before merge | sre |
| State-history blob remains in `s3://loopzero-infra-pulumi-state` after stack rm | LOW | confirmed | C27 post-rm prefix cleanup | infra |
| `loopzero_pulumi_*` metrics not actually scraped | MED | LOW | C20 + C24 (T-13) verified | sre |
| Recovery requires URN rewrite from `loopzero-platform` → canonical project | MED | confirmed | C16 documented restore procedure | gitops |
| Archive rotation Lambda fails silently → archives age out unnoticed | MED | LOW | C20 `PulumiArchiveStaleness` alert | sre |
| New paralelas project re-introduced by future contributor | LOW | LOW | C20 `PulumiOrphanStackDetected` alert | sre |
| CHANGELOG language leaks internal term | LOW | LOW | C29 pre-commit review | doc |

**Risks NOT accepted (block until mitigated):** none — every risk has mitigation, owner, validation test.

**Mandatory actions before implementation merge.**

1. **C1.** `pulumi stack ls` invoked with explicit `--cwd <dir>` per dir; resulting URIs compared against canonical to detect collision (T-01, T-02).
2. **C2.** `infra/pulumi-shared/` deleted in same commit as paralelas dirs (T3.3 atomic).
3. **C3.** Verification covers DO + Cloudflare + AWS; per-provider commands tabulated in runbook D8.
4. **C4.** Archive S3 bucket `s3://loopzero-archive/pulumi-state/`: private ACL, SSE-KMS (customer-managed key), versioning, Object Lock 90d governance, deny-delete except `loopzero-archive-rotator` role. Provisioned in canonical `infra/pulumi/` BEFORE T3.1.
5. **C5.** `PULUMI_CONFIG_PASSPHRASE` fetched at archive time from `mgmt-platform-secrets`, NOT colocated with archive.
6. **C6.** 90-day rotation policy + Object Lock (governance mode); `legal-hold: true` tag for exceptions.
7. **C7.** `infra/scripts/provision-homolog.sh` deleted in same commit as paralelas dirs.
8. **C8.** CloudTrail audit logging on archive bucket, 1-year retention.
9. **C9.** Post-export plaintext-secret regex scan (jq pattern `token|password|secret|key`); 0 matches outside encrypted blocks (T-06).
10. **C10.** `docs/runbooks/pulumi-consolidation.md` (NEW) — single executable runbook covering T3.1/T3.2/T3.3.
11. **C11.** Idempotency test (T-04): runbook re-run produces identical S3 archive set.
12. **C12.** `.github/workflows/pulumi-archive.yml` (NEW) — quarterly scheduled archive scan in `--archive-only` mode.
13. **C13.** Three commits: (a) T3.1 archive run + inventory doc, (b) T3.2 stack rm + audit log, (c) T3.3 code delete + CHANGELOG + ADR-0021.
14. **C14.** `infra/scripts/pulumi-archive.sh` + `infra/scripts/pulumi-orphan-rm.sh` (NEW) — exit codes 0 (ok) / 2 (preflight failed) / 3 (live resources detected) / 4 (archive failed). Test matrix T-12.
15. **C15.** `docs/audits/pulumi-consolidation-log.md` (NEW) — append-only audit trail.
16. **C16.** `docs/runbooks/pulumi-state-restore.md` (NEW) — URN-rewrite restore procedure with worked example.
17. **C17.** `pulumi version` pinned in scripts (e.g., 3.140.0); snapshot-sha verified before/after every export.
18. **C18.** Per-provider verification commands documented (DO: tag query + name-pattern; CF: name + cross-ref canonical outputs; AWS: list + cross-ref).
19. **C19.** State-only rollback SLO ≤ 15 min wall-clock; tested in dev (T-11).
20. **C20.** `loopzero_pulumi_archives_total` (gauge) + `loopzero_pulumi_stacks_total` (gauge) + `PulumiArchiveStaleness` (no archive in 90d, MEDIUM) + `PulumiOrphanStackDetected` (canonical scan reveals unexpected stack name, HIGH) — all wired (T-13).
21. **C21.** `grep -rn "pulumi-infra\|pulumi-platform\|pulumi-control-plane" infra/helm/monitoring/ docs/runbooks/alerts/` returns 0 (fixes `LoopZeroCFTunnelDisconnected.md:48`).
22. **C22.** Archive replay drill (T-11) within 14 days of T3.2; FAILS → retroactive REJECTED, C19 rollback invoked.
23. **C23.** Test matrix T-01..T-12 GREEN before T3.2 stack rm.
24. **C24.** T-13 (observability) GREEN within 7 days post-T3.3.
25. **C25.** T-14 (regression: `find infra -name 'Pulumi.yaml' \| wc -l == 1` + grep clean) GREEN before T3.3 merge.
26. **C26.** Archive replay drill T-11 GREEN within 14 days post-T3.2; blocks any future paralelas-style re-introduction.
27. **C27.** Post-rm cleanup: `s3://loopzero-infra-pulumi-state/<project>/<stack>/` prefix removed.
28. **C28.** `infra/scripts/seed_e2e.go` scope verified by reading; relocated or deleted per finding.
29. **C29.** CHANGELOG `[Removed]` entry uses product language only (CLAUDE.md Rule 3 review).

---

## Stage 5 — Execution Contract

**Owner (joint).** infra-engineer (lead) + sre-engineer (co-signs runbook + replay drill) + platform-security-engineer (co-signs archive ACL + KMS + plaintext-secret scan).

**Deliverables.**

| # | Artifact | Path | Owner |
|---|----------|------|-------|
| D1 | This decision record | `docs/decisions/MEET-INFRA-NORMALIZE-T3-pulumi-consolidation.md` | orchestrator |
| D2 | Archive script | `infra/scripts/pulumi-archive.sh` (exit codes 0/2/3/4) | infra |
| D3 | Orphan rm script | `infra/scripts/pulumi-orphan-rm.sh` (exit codes 0/2/3/4) | infra |
| D4 | Consolidation runbook | `docs/runbooks/pulumi-consolidation.md` | sre + infra |
| D5 | Audit log | `docs/audits/pulumi-consolidation-log.md` | infra |
| D6 | Restore runbook | `docs/runbooks/pulumi-state-restore.md` | gitops |
| D7 | CI archive workflow | `.github/workflows/pulumi-archive.yml` | infra |
| D8 | Per-provider verification matrix | embedded in D4 | sre |
| D9 | Prometheus rules + dashboard | `infra/helm/monitoring/values-prometheus.yaml` (`PulumiArchiveStaleness`, `PulumiOrphanStackDetected`) + `infra/helm/monitoring/dashboards/pulumi-lifecycle.json` | sre |
| D10 | State inventory | `docs/audits/pulumi-state-inventory.md` (T0.2 deliverable, gates this decision's T3.1) | infra |
| D11 | Archive S3 bucket | new resource in `infra/pulumi/src/components/archive-bucket.ts` | infra |
| D12 | ADR-0021 prescriptive | `docs/decisions/ADR-0021-pulumi-single-stack.md` | architect |
| D13 | Test matrix T-01..T-14 | spread across `infra/pulumi/src/__tests__/` + `infra/scripts/__tests__/` | per matrix |
| D14 | CHANGELOG | `CHANGELOG.md` `[Unreleased]` `[Removed]` entry (product language) | doc |
| D15 | Updated runbook reference | `docs/runbooks/alerts/LoopZeroCFTunnelDisconnected.md` line 48 | sre |

**Validations.**

- `task test` GREEN (T-01..T-04, T-12).
- `task lint` + `task validate:manifests` GREEN.
- Archive bucket policy validated by `aws s3api get-bucket-policy` + `get-bucket-encryption` + `get-object-lock-configuration` (T-05).
- Plaintext-secret regex scan returns 0 matches outside encrypted blocks (T-06).
- `doctl` + `cf` + `aws` cross-reference returns 0 unaccounted resources per orphan stack (T-08, T-09, T-10).
- Replay drill: `pulumi stack import` of archived state into sandbox project → diff vs canonical = 0/0/0 (T-11).
- Post-T3.3: `find infra -name 'Pulumi.yaml' \| wc -l` returns 1 (T-14).
- 7 days post-T3.3: `loopzero_pulumi_archives_total` scraped; `PulumiArchiveStaleness` not firing; `PulumiOrphanStackDetected` not firing (T-13).

**Definition of Done.**

- [ ] All 29 conditions C1–C29 satisfied with committed evidence.
- [ ] Test matrix T-01..T-12 GREEN before T3.2 (stack rm).
- [ ] T-13 GREEN within 7 days post-T3.3.
- [ ] T-11 replay drill GREEN within 14 days post-T3.2.
- [ ] T-14 GREEN before T3.3 merge.
- [ ] CHANGELOG `[Unreleased]` `[Removed]` updated (product language).
- [ ] PR descriptions for the 3 commits reference this decision record.
- [ ] ADR-0021 (prescriptive single-stack policy) committed.
- [ ] No alert references deleted dirs (C21).
- [ ] Archive bucket provisioned BEFORE T3.1.
- [ ] `provision-homolog.sh` + `pulumi-shared/` + paralelas dirs deleted atomically in T3.3.
- [ ] State-only rollback exercised in dev within 15-min SLO (C19).

---

## Migration Playbook (summary; full procedure in D4 runbook)

Sequence. Each step gated on prior step's exit 0.

1. **Pre-flight (Phase 0 / T0.2).**
   - Archive S3 bucket provisioned (D11) per C4.
   - Run `infra/scripts/pulumi-archive.sh --inventory-only` per dir → produces `docs/audits/pulumi-state-inventory.md` (D10).
   - Per-provider verification commands run; results tabulated.
   - Decision: per stack, recommendation ∈ {rm, preserve, escalate}.
   - C1 backend-explicit `stack ls` + URI collision check.
   - **Gate:** if any stack has `recommendation: preserve` or `escalate`, this decision halts and a supplementary T3 meeting is required for that stack only.

2. **Phase 3.1 — Archive (commit 1).**
   - Run `infra/scripts/pulumi-archive.sh --apply` per (dir, stack):
     - `pulumi version` matches pinned (C17).
     - `sha256sum` of pre-export snapshot recorded.
     - `pulumi stack export --stack <stack>` → JSON.
     - `sha256sum` of post-export snapshot — must match (C17).
     - Plaintext-secret regex scan (C9) — 0 matches outside encrypted blocks.
     - Encrypt with `PULUMI_CONFIG_PASSPHRASE` from `mgmt-platform-secrets` (C5).
     - Upload to `s3://loopzero-archive/pulumi-state/<dir>/<stack>/<ISO-ts>.json.enc` (Object Lock auto-applies).
     - Append to `docs/audits/pulumi-consolidation-log.md` (D5).
   - Commit: `feat(infra): archive Pulumi state of orphan paralelas stacks (T3.1)`.

3. **Phase 3.2 — Stack rm (commit 2).**
   - Run `infra/scripts/pulumi-orphan-rm.sh --apply` per stack with `recommendation: rm`:
     - Re-verify zero live resources (C18) — exit 3 if any found.
     - `pulumi stack rm --yes <stack>`.
     - Cleanup `s3://loopzero-infra-pulumi-state/<project>/<stack>/` prefix (C27).
     - Append to audit log.
   - Run T-01..T-12. All GREEN.
   - Commit: `feat(infra): remove orphan Pulumi stacks post-archive (T3.2)`.

4. **Phase 3.3 — Code delete (commit 3, atomic).**
   - `git rm -r infra/pulumi-infra infra/pulumi-platform infra/pulumi-control-plane infra/pulumi-shared infra/scripts/provision-homolog.sh` (C2, C7).
   - `git rm infra/scripts/seed_e2e.go` (C28, conditional on read).
   - Update `docs/runbooks/alerts/LoopZeroCFTunnelDisconnected.md:48` (C21).
   - Add `docs/decisions/ADR-0021-pulumi-single-stack.md` (D12) — prescriptive (Options, Vetoes, Migration, Rollback).
   - Add Fix #10 test case (T-14) in `infra/pulumi/src/__tests__/sprint8-fixes.test.ts`: `find infra -name 'Pulumi.yaml' | wc -l === 1`.
   - CHANGELOG `[Removed]` entry (C29 product-language reviewed).
   - Run T-14. GREEN.
   - Commit: `feat(infra): consolidate to single Pulumi project (T3.3)`.

5. **Post-merge — Replay drill (within 14 days).**
   - Pick one archived stack.
   - Provision sandbox project name (e.g., `loopzero-archive-replay-test`).
   - Decrypt archive with passphrase + KMS.
   - URN-rewrite per D6 runbook.
   - `pulumi stack import < restored.json`.
   - `pulumi preview` — expect 0 to update / 0 to create / 0 to delete.
   - Document result in `docs/audits/pulumi-consolidation-log.md`.
   - **Gate:** if drill FAILS, this decision is RETROACTIVELY REJECTED; rollback C19 invoked (state re-imported into canonical via D6 procedure).

6. **Post-merge — Observability burn-in (7 days).**
   - `loopzero_pulumi_archives_total` scraping confirmed.
   - `PulumiArchiveStaleness` alert not firing.
   - `PulumiOrphanStackDetected` alert not firing.

---

## Rollback Procedure (≤ 15 min state-only, per C19)

Rollback is feasible at three points:

1. **Mid-archive (Phase 3.1 partial completion).** No state changed; introduce a NEW reverse-patch commit on top of the archive script changes; cancel run. Wall-clock: ≤ 5 min.

2. **Post-`stack rm` (Phase 3.2 completed, before code delete).** Archive exists. Restore procedure (D6):
   - Decrypt archive (`PULUMI_CONFIG_PASSPHRASE` + KMS).
   - URN-rewrite project name `loopzero-platform` (or whichever) → `loopzero-infra` canonical.
   - `cd infra/pulumi && pulumi stack init <recovered-stack>`.
   - `pulumi stack import < restored.json`.
   - `pulumi preview` — verify 0/0/0 against live cloud (live resources unchanged because `stack rm` does not destroy resources, only state).
   - Wall-clock: ≤ 15 min (C19).
   - **NB:** state restored, but the `infra/pulumi-platform/` *code* is gone (only relevant if the rollback motivation is "restore the dir"; otherwise canonical absorbs the state).

3. **Post-code-delete (Phase 3.3 merged).** Code is gone; restoring code requires a NEW reverse-patch commit (NOT destructive history rewrites — forbidden by `.claude/rules/workflow.md` line 33). Procedure:
   - From clean tree at the cutover SHA, produce reverse-patch: `git diff <T3.3-sha>~1 <T3.3-sha> -- infra/pulumi-platform/ infra/pulumi-control-plane/ infra/pulumi-infra/ infra/pulumi-shared/ infra/scripts/provision-homolog.sh | git apply -R`.
   - `git add` + `git commit -m "rollback: T3.3 paralelas dirs restored"` as a NEW commit on a fresh branch (not main).
   - State restore via step 2 above.
   - Total wall-clock: ≤ 30 min.
   - **NB:** This is the absolute-worst-case rollback. The replay drill (C22, T-11) is designed to make it never necessary. If it IS necessary, that signals the decision was wrong; a new T3 supplementary meeting is required.

In ALL cases: **cloud resources** (DOKS, PG, Spaces, CF Tunnel) are NEVER destroyed by this decision's procedure (`pulumi stack rm` removes state pointer, not resources). If a rollback is needed because cloud resources WERE destroyed, that is a P1 incident — not a planned rollback — and is out of scope here.

---

## Verdict — Final

**APPROVED_WITH_CONDITIONS.**

- **Platform-architect veto:** lifted CONDITIONAL on **C1 + C2 + C3**. Per-dir backend-explicit verification + atomic single-commit code delete + multi-cloud verification.
- **Security veto (PARTIAL):** lifted CONDITIONAL on **C4 + C5 + C6 + C7 + C8 + C9**. Without C9 plaintext-secret scan returning 0, security veto REINSTATED. V-S1 hard-veto remains active (plaintext token in legacy state → rotate first).
- **GitOps veto:** lifted CONDITIONAL on **C10 + C11 + C12 + C13 + C14 + C15 + C16**. Single executable runbook + idempotency + CI archive + 3-commit phasing + scripts with declared exit codes + audit log + URN-rewrite restore.
- **SRE veto:** lifted CONDITIONAL on **C17 + C18 + C19 + C20 + C21 + C22**. Pinned pulumi version + per-provider verification + ≤15-min state-only rollback SLO + observability + alert-doc grep clean + replay drill within 14 days post-T3.2.
- **Verification veto:** lifted CONDITIONAL on **C23 + C24 + C25 + C26**. T-01..T-12 GREEN before T3.2; T-13 within 7 days; T-14 before T3.3 merge; T-11 within 14 days.
- **Orchestrator self-challenge:** **C27 + C28 + C29** added.

**Phase 3 implementation may begin only after:**

1. This decision record committed to `docs/decisions/`.
2. Plan `infra-normalize-multicell-plan.md` updated to reference C1–C29 in T0.2, T3.1, T3.2, T3.3 task lists.
3. Test matrix T-01..T-14 written in failing state (TDD red).
4. Archive S3 bucket (D11) provisioned in canonical `infra/pulumi/`.
5. `pulumi-archive.sh` + `pulumi-orphan-rm.sh` drafted with declared exit codes.
6. `docs/runbooks/pulumi-consolidation.md` + `docs/runbooks/pulumi-state-restore.md` drafted.
7. T0.2 inventory completed and committed; per-stack recommendation ∈ {rm, preserve, escalate} declared. Any stack with `preserve` or `escalate` triggers a supplementary T3 meeting before this decision proceeds.

**This decision record is binding. Any deviation requires a new T3 supplementary meeting.**

---

## References

- **Plan:** `/home/paulo/loopzero/docs/plans/infra-normalize-multicell-plan.md` — ADR D2 (lines 87–106), Phase 0 / T0.2 (lines 457–504), Phase 3 / T3.1–T3.3 (lines 963–1129).
- **Sibling decision:** `/home/paulo/loopzero/docs/decisions/MEET-INFRA-NORMALIZE-T3-operator-split.md` (format reference; D1 cross-cutting context).
- **Pulumi project files:**
  - `/home/paulo/loopzero/infra/pulumi/Pulumi.yaml` (canonical, `name: loopzero-infra`).
  - `/home/paulo/loopzero/infra/pulumi-infra/Pulumi.yaml` (orphan, `name: loopzero-infra` — collision).
  - `/home/paulo/loopzero/infra/pulumi-platform/Pulumi.yaml` (orphan, `name: loopzero-platform`, violates ADR-0020 D7).
  - `/home/paulo/loopzero/infra/pulumi-control-plane/Pulumi.yaml` (orphan, `name: loopzero-control-plane`, violates ADR-0020 D7).
  - `/home/paulo/loopzero/infra/pulumi-shared/stack-outputs.ts` (type-only, consumed by paralelas only).
- **Provisioning script:** `/home/paulo/loopzero/infra/scripts/provision-homolog.sh` (sole external reference to the 3 paralelas dirs).
- **D7 enforcement test:** `/home/paulo/loopzero/infra/pulumi/src/__tests__/sprint8-fixes.test.ts` (Fix #6 + Fix #7).
- **CLI wiring:** `/home/paulo/loopzero/cli/src/installer/pulumi-runner.ts:resolvePulumiProjectDir` (ADR-0020 D2 canonical).
- **State backend:** `s3://loopzero-infra-pulumi-state` (per CLAUDE.md "Production Infrastructure").
- **Sibling ADRs:**
  - `/home/paulo/loopzero/docs/meetings/adr/0015-pre-bootstrap-adopt-or-create.md` — adopt-or-create policy precedent for state lifecycle.
  - `/home/paulo/loopzero/docs/decisions/ADR-0020-infra-parity-decisions.md` — D2 (canonical Pulumi root) + D7 (no @pulumi/kubernetes in canonical).
  - `/home/paulo/loopzero/docs/decisions/ADR-0017-argocd-auto-sync-evidence.md` — sibling reproducibility precedent.
  - `/home/paulo/loopzero/docs/decisions/ADR-0018-tenant-namespace-sot.md` — sibling cross-decision-invariants pattern.
- **Project rules:**
  - `/home/paulo/loopzero/CLAUDE.md` (Rule 3 product language, Rule 5 JSON contract, Rule 10 local equals production).
  - `/home/paulo/loopzero/.claude/rules/workflow.md` (Git Rules — Inviolable; line 33 forbids destructive history operations).
  - `/home/paulo/loopzero/.claude/rules/pre-release-governance.md` (T3 trigger; pre-release bias).
- **Stale runbook reference (to fix per C21):** `/home/paulo/loopzero/docs/runbooks/alerts/LoopZeroCFTunnelDisconnected.md:48`.
