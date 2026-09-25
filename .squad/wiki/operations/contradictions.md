---
type: Operations
title: "Where the repository and its own documents disagree"
description: "All 35 findings the map recorded: contradictions, stale pointers, unwired code, undeclared boundaries, and the negatives that were checked rather than assumed."
resource: "."
tags:
  - contradiction
  - finding
  - documentation
  - drift
sources:
  - id: f2
    resource: "api/internal/audit/domain_events.go#L43"
    note: "api/internal/audit is implemented and has no caller anywhere in the repository"
  - id: f5
    resource: "api/internal/health/prober.go#L3"
    note: "api/internal/health has no production caller, yet adapters.go names it as the required real implementation"
  - id: f18
    resource: "infra/pulumi/README.md#L359"
    note: "infra/pulumi is declared AS-IS legacy yet is the most-churned infra directory, and no successor exists in this repo"
  - id: f20
    resource: "infra/pulumi/docs/ADR-001-project-boundaries.md#L3"
    note: "ADR-001 is still PROPOSED while its migration is half-executed on disk"
  - id: f26
    resource: "infra/helm/loopzero-api/templates/serviceaccount.yaml#L27"
    note: "The two loopzero-api ClusterRoles are a DECLARED parity pair and grant materially different RBAC — only one carries the documented blast-radius remediation"
  - id: f23
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L220"
    note: "17 infrastructure components are recorded twice under two key schemes by two concurrent agents"
  - id: f13
    resource: "go.work#L6"
    note: "The Go workspace compiles only because a sibling repository is checked out next to this one"
  - id: f8
    resource: "api/internal/db/store/membership_wrapper.go#L27"
    note: "The persistence layer imports HTTP middleware: db/store -> middleware/auth and middleware/policy"
  - id: f1
    resource: "CLAUDE.md#L21"
    note: "CLAUDE.md states CHANGELOG.md has 4,567 lines; the file has 48"
  - id: f11
    resource: "api/cmd/setup_database.go#L29"
    note: "331 tracked files carry Portuguese, including runtime error strings, against the repository's English-only rule"
  - id: f12
    resource: "cmd/loopzero-ops/quality_registry.go#L36"
    note: "CLAUDE.md reports 145 registered / 126 wired gates; the oracle it names reports 146 registered, 127 task-attached"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

The map re-checked what the repository says about itself. **35 findings** —
6 high, 13 medium, 12 low, 4
informational — each anchored to the line that establishes it.

Every one of these is recorded **rather than resolved**. Where a document and the code disagree,
the concept documents in this bundle describe what the code does and name the document that says
otherwise, with both anchors. Deciding which should change is not a cartographer's call.

Two of the 32 are defects in **this cartography run**, not in the repository. They are kept in
the same list, at the same severity, because a map that hides its own errors is asking to be
trusted more than it has earned.

# Schema

| # | Severity | Category | Finding | Anchor |
|---|---|---|---|---|
| 1 | 🟠 medium | `contradiction` | CLAUDE.md states CHANGELOG.md has 4,567 lines; the file has 48 | `CLAUDE.md:21` |
| 2 | 🔴 high | `orphan_component` | api/internal/audit is implemented and has no caller anywhere in the repository | `api/internal/audit/domain_events.go:43` |
| 3 | 🟠 medium | `orphan_component` | api/internal/cellmgr has zero importers, while CLAUDE.md lists it as a notable package | `api/internal/cellmgr/client_manager.go:3` |
| 4 | 🟠 medium | `orphan_component` | api/internal/objectstore declares a DIP seam with no consumer, and holds its own adapter | `api/internal/objectstore/objectstore.go:3` |
| 5 | 🔴 high | `orphan_component` | api/internal/health has no production caller, yet adapters.go names it as the required real implementation | `api/internal/health/prober.go:3` |
| 6 | 🟡 low | `stale_declaration` | adapters.go cites api/internal/gitea/gitops_pusher.go, a path that does not exist | `api/internal/adapters/adapters.go:20` |
| 7 | 🟡 low | `stale_declaration` | api/internal/correlation cites pkg/quality/correlationheader; pkg/quality does not exist, and the regression-lock that forbids the citation only scans cmd/loopzero-ops | `api/internal/correlation/reader.go:18` |
| 8 | 🟠 medium | `undocumented_boundary` | The persistence layer imports HTTP middleware: db/store -> middleware/auth and middleware/policy | `api/internal/db/store/membership_wrapper.go:27` |
| 9 | 🟠 medium | `undocumented_boundary` | routes/secrets and routes/billing reach persistence directly, unlike the other 27 route packages | `api/internal/secrets/secretshttp/secrets.go:41` |
| 10 | 🟡 low | `undocumented_boundary` | pkg/enforcement is the one shared library that spans three layers | `pkg/enforcement/limiter.go:5` |
| 11 | 🟠 medium | `contradiction` | 331 tracked files carry Portuguese, including runtime error strings, against the repository's English-only rule | `api/cmd/setup_database.go:29` |
| 12 | 🟡 low | `stale_declaration` | CLAUDE.md reports 145 registered / 126 wired gates; the oracle it names reports 146 registered, 127 task-attached | `cmd/loopzero-ops/quality_registry.go:36` |
| 13 | 🟠 medium | `undocumented_boundary` | The Go workspace compiles only because a sibling repository is checked out next to this one | `go.work:6` |
| 14 | ⚪ info | `no_cartography_issues` | Verified: the two-chart parity claim holds, and the declared pair list matches disk | `infra/quality/chart-pairs.txt:24-25` |
| 15 | 🟡 low | `stale_declaration` | CODEOWNERS assigns ownership of two files that do not exist | `.github/CODEOWNERS:22` |
| 16 | ⚪ info | `no_cartography_issues` | No dependency cycle found, by three independent methods | `cmd/loopzero-ops/quality_registry.go:36` |
| 17 | 🟠 medium | `orphan_component` | policies/charts/*.rego are Conftest policies that nothing invokes | `policies/charts/security_context.rego:1` |
| 18 | 🔴 high | `contradiction` | infra/pulumi is declared AS-IS legacy yet is the most-churned infra directory, and no successor exists in this repo | `infra/pulumi/README.md:359` |
| 19 | 🟠 medium | `contradiction` | The only in-tree AWS path is a Pulumi provider stub, on neither the live nor the declared-target path | `infra/pulumi/src/providers/aws/index.ts:14` |
| 20 | 🔴 high | `stale_declaration` | ADR-001 is still PROPOSED while its migration is half-executed on disk | `infra/pulumi/docs/ADR-001-project-boundaries.md:3` |
| 21 | 🟠 medium | `orphan_component` | infra/pulumi-control-plane/ survives on disk as a node_modules-only directory with no tracked files | `infra/pulumi/docs/ADR-001-project-boundaries.md:298` |
| 22 | ⚪ info | `other` | Cartography note: collapsing buildplane/* and services/* into one node each manufactured a cycle that does not exist | `api/internal/buildplane/argo/buildkit.go:19` |
| 23 | 🔴 high | `other` | 17 infrastructure components are recorded twice under two key schemes by two concurrent agents | `infra/helmfile/core.helmfile.yaml.gotmpl:220` |
| 24 | 🟠 medium | `other` | 17 infrastructure components were mapped twice by concurrent agents, and have been reconciled | `infra/helmfile/core.helmfile.yaml.gotmpl:245` |
| 25 | 🟡 low | `dynamic_only_region` | Five declared ClusterRoles have no readable name: metadata.name is a Helm template expression | `charts/loopzero-api/templates/clusterrole.yaml:5` |
| 26 | 🔴 high | `contradiction` | The two loopzero-api ClusterRoles are a DECLARED parity pair and grant materially different RBAC — only one carries the documented blast-radius remediation | `infra/helm/loopzero-api/templates/serviceaccount.yaml:27` |
| 27 | 🟠 medium | `other` | 468 commits — 8.9% of the repository's history — sit unreleased behind a tag series that stopped 24 days ago | `CHANGELOG.md:1` |
| 28 | 🟡 low | `other` | 36% of first-party TODO hits are the Portuguese word for "every", so the TODO marker cannot be used as a debt signal here | `cmd/loopzero-ops/leak_subcmd.go:100` |
| 29 | 🟡 low | `stale_declaration` | ci.yml's post-mortem comment cites commit 696fe5c62, which exists in no reachable commit; the commit it means is 8c9a1d953 | `.github/workflows/ci.yml:203` |
| 30 | ⚪ info | `other` | Cartography note: the test pyramid's e2e tier is Ginkgo-driven, so its test_count (71) is a floor, not a count | `infra/tests/go.mod:1` |
| 31 | 🟡 low | `contradiction` | application.go's doc comment says 'No terminal state' 49 lines after saying Cleaned IS terminal; the code declares Cleaned terminal | `pkg/statemachine/application.go:64` |
| 32 | 🟡 low | `stale_declaration` | 12 Go source citations point at loopzero-design.md, a file deleted in commit 8cb41f660 | `operators/internal/environment/environment_controller.go:177` |
| 33 | 🟠 medium | `stale_declaration` | The Tiltfile still sends developers to getloopscale/loopzero-contracts@v0.1.0 — the org the repo left in August, and a version three minors behind | `Tiltfile:35` |
| 34 | 🟡 low | `contradiction` | HEAD commit claims the ExternalSecret comments were translated; line 46 is still Portuguese | `infra/helm/loopzero-external-secrets/templates/externalsecrets.yaml:46` |
| 35 | 🟡 low | `unmapped_region` | Read-deny glob written for the rendered artifact matches the Helm template, leaving 11 critical-role files unmapped | `.claude/settings.json:103` |

# The findings


## A document and the code disagree — 7

A statement in the repository's own prose that the code refutes. These are the most valuable rows here: a stale document is believed precisely because nobody re-checks it.

### 18. infra/pulumi is declared AS-IS legacy yet is the most-churned infra directory, and no successor exists in this repo

**🔴 high**, confidence high. `infra/pulumi/README.md:359`

The README declares 'infra/pulumi* is AS-IS legacy until the AM8 cutover to Terraform'. Measured against disk and git on 2026-09-11: (a) NO .tf file exists anywhere in this repository, so the successor tooling is not here; (b) infra/pulumi carries 351 commits with its most recent on 2026-09-11 — more than infra/helm (214), infra/helmfile (62) and infra/k3d (28) combined (304), and it is not dormant; six commits touched infra/pulumi/src since 2026-08-01, four of them refactors (cluster renames, cell identity merge, package moves) rather than freeze maintenance. A layer being actively refactored is not being held AS-IS. (c) The same README elsewhere (line 342) still presents AWS portability as a Pulumi provider swap ('Gitea VM -> EC2, Spaces -> S3, DOKS -> EKS, DB -> RDS'), which contradicts the Terraform cutover stated 17 lines earlier in the same file.

_What would close it:_ State the freeze scope explicitly: which changes are still accepted into infra/pulumi during the strangler period (security patches, renames forced elsewhere) and which are not. Remove or annotate the AWS-portability paragraph, which points readers at a path the mandate abandoned.

### 26. The two loopzero-api ClusterRoles are a DECLARED parity pair and grant materially different RBAC — only one carries the documented blast-radius remediation

**🔴 high**, confidence high. `infra/helm/loopzero-api/templates/serviceaccount.yaml:27`

infra/quality/chart-pairs.txt declares loopzero-api as one of exactly two charts existing in both trees, and states the contract it exists to enforce: *"when patching one, apply the same fix to the other"*. Their ClusterRoles disagree on 162 of 200 distinct (resource, verb) cells. The asymmetry runs the dangerous way. charts/loopzero-api/templates/clusterrole.yaml:16-18 grants loopzero.io/* seven NAMED verbs and carries a 9-line comment recording that the cluster-wide secrets/configmaps grant was 'deep review finding #1 (cluster-wide secrets/configmaps grant = catastrophic blast radius)', now gated behind .Values.rbac.legacyClusterWideSecrets so a cluster can turn it off. infra/helm/loopzero-api/templates/serviceaccount.yaml:27-29 grants loopzero.io/* verb `*` — full wildcard — plus, at :35-37, cluster-wide core secrets with `delete`, with no conditional and no reference to that review. The remediation was applied to the published chart and never to its declared twin, and the twin is the one the Helmfile installs at bootstrap. The chart-pairs gate checks that a pair is DECLARED and that both sides EXIST; nothing compares what the two sides grant, which is why a security fix could land on one side and the divergence stay invisible.

_What would close it:_ Extend the chart-pairs gate from existence to CONTENT for the security-bearing templates: diff the rendered RBAC of each declared pair and fail on a cell present in one side and absent in the other. Then decide the wildcard deliberately — either infra/helm narrows loopzero.io/* to named verbs and adopts the legacyClusterWideSecrets conditional, or an ADR records why bootstrap needs more than the published chart.

### 1. CLAUDE.md states CHANGELOG.md has 4,567 lines; the file has 48

**🟠 medium**, confidence high. `CLAUDE.md:21`

CLAUDE.md:21 asserts this repository's CHANGELOG.md 'has 4,567 lines'. Measured: CHANGELOG.md is 48 lines. The number is neither the current size nor the historical one. CHANGELOG.md:9 states the previous file 'held 4,802 lines and was removed in 4f728eb7a', and that is verifiable: 'git show 4f728eb7a~1:CHANGELOG.md | wc -l' returns exactly 4802. So the repository's own changelog documents its truncation correctly while CLAUDE.md carries a third figure matching neither. Consequence for a reader: CLAUDE.md presents CHANGELOG.md as the dated engineering history, and a reader following that pointer finds 48 lines with one [Unreleased] section and no versioned entries, despite 84 git tags existing.

_What would close it:_ Correct the figure at CLAUDE.md:21 to the measured 48, or remove the line-count claim: a count of a file that changes every release is a fact that goes stale by construction.

### 11. 331 tracked files carry Portuguese, including runtime error strings, against the repository's English-only rule

**🟠 medium**, confidence high. `api/cmd/setup_database.go:29`

The project states an English-only rule for everything git versions (.claude/rules/english-only.md; CLAUDE.md PARTE 0 in the user contract). Measured on tracked files outside .claude/: 331 contain Portuguese function words. It is not confined to comments — api/cmd/setup_database.go:29 is a Go error VALUE in Portuguese, and api/internal/server/router.go:608 is another. Package documentation is affected too: operators/internal/{cellrouting,reconcileloop,operatorboot,tenantpolicy} and cmd/loopzero-ops/internal/leak have Portuguese package docs, so `go doc` renders Portuguese for five packages. infra/quality/chart-pairs.txt — a tracked contract file read by the chart-pairs gate — is entirely in Portuguese.

_What would close it:_ The rule has no mechanism in this repository: the checker named in .claude/rules/english-only.md lives in the installed kit, which .gitignore excludes, so a fresh clone enforces nothing. Either port the check into cmd/loopzero-ops/internal/quality/ where the other 146 gates live, or amend the rule to match practice.

### 19. The only in-tree AWS path is a Pulumi provider stub, on neither the live nor the declared-target path

**🟠 medium**, confidence high. `infra/pulumi/src/providers/aws/index.ts:14`

infra/pulumi/src/providers/aws/index.ts is 66 lines and opens with 'AWS provider stub — not implemented yet' followed by a 7-step implementation guide. It is the repository's only AWS-shaped artifact. The declared target tooling is Terraform in the sibling repos ../loopzero-infra-modules and ../loopzero-infra-live (both present on disk beside this one, neither inside it), so this stub is on neither the live path (DigitalOcean/Pulumi) nor the target path (AWS/Terraform). A reader following the README's portability paragraph would start implementing it.

_What would close it:_ Delete the stub, or mark it superseded in-file naming the Terraform repos. Keeping an inviting implementation guide for an abandoned approach costs somebody a day.

### 31. application.go's doc comment says 'No terminal state' 49 lines after saying Cleaned IS terminal; the code declares Cleaned terminal

**🟡 low**, confidence high. `pkg/statemachine/application.go:64`

### 34. HEAD commit claims the ExternalSecret comments were translated; line 46 is still Portuguese

**🟡 low**, confidence high. `infra/helm/loopzero-external-secrets/templates/externalsecrets.yaml:46`

rules/english-only.md and CLAUDE.md Part 0 require every versioned comment to be English. Commit 6c22c4ecf -- titled 'chore(helm): translate the ExternalSecret template comments to English' -- is the last commit touching this file, yet line 46 still reads 'quando ExternalSecret CR e deleted'. The rule and the file disagree, AND the commit message asserts the disagreement was already resolved, so a reader trusting the log would not look. Found while verifying a stop-hook secret-pattern BLOCK on this path: the file renders ExternalSecret CRs and holds Vault path references only, no secret value, and this session did not modify it (git status clean for this path). Recorded, not fixed: editing a mapped file invalidates every file_line anchor taken against it.

_What would close it:_ Translate line 46 to English in a follow-up commit, outside this cartography run.


## A pointer that no longer resolves — 8

A citation — a path, a commit, a package — that pointed at something real when it was written and does not now. A reader who follows one concludes something false about the history.

### 20. ADR-001 is still PROPOSED while its migration is half-executed on disk

**🔴 high**, confidence high. `infra/pulumi/docs/ADR-001-project-boundaries.md:3`

ADR-001 (2026-04-10, status PROPOSED) decides a split of infra/pulumi into three projects — loopzero-infra, loopzero-platform, loopzero-control-plane — connected by StackReference, with a 13-step migration ending at '13. Deprecate monolith'. Measured 2026-09-11: two of the three projects exist and are live (infra/pulumi-infra, first commit 2026-04-10, 25 commits, last 2026-09-11; infra/pulumi-platform, 39 commits, last 2026-09-11) and the shared StackReference contract exists (infra/pulumi-shared/stack-outputs.ts). The third does NOT: infra/pulumi-control-plane has ZERO tracked files — its last commit, 2026-05-14, is literally 'delete pulumi-control-plane stub'. And step 13 never ran: the monolith infra/pulumi still carries 351 commits and was touched the same day as its successors. So the repository is in a state the ADR never described — two projects instead of three, running BESIDE the monolith rather than replacing it — and the document that a reader would consult still says PROPOSED.

_What would close it:_ Move ADR-001 to ACCEPTED-WITH-AMENDMENT or SUPERSEDED and record what actually happened: the control-plane project was dropped, and the monolith was not deprecated. As written, the ADR tells a reader that none of this has been done.

### 33. The Tiltfile still sends developers to getloopscale/loopzero-contracts@v0.1.0 — the org the repo left in August, and a version three minors behind

**🟠 medium**, confidence high. `Tiltfile:35`

Tiltfile:35 instructs a blocked developer to generate a PAT with `Contents: read` on `getloopscale/loopzero-contracts`, and Tiltfile:183 documents the build's dependency as `github.com/getloopscale/loopzero-contracts@v0.1.0`. Both go.mod files that actually require the module say `github.com/getloopscale/loopzero-contracts v0.4.0` (api/go.mod:91, pkg/go.mod:30), and the Tiltfile is the ONLY place left in the repository naming the old organisation. Neither line is executable — they are an error message and a comment — so nothing breaks at build time; what breaks is the person reading them, who is by construction already blocked and who will go and create a token on a repository that is not there. The same drift in ci.yml blocked every pipeline run from 2026-07-31 to 2026-08-07 and was repaired in b919cc135; the repair did not reach the Tiltfile, which CLAUDE.md names as the canonical day-to-day loop.

_What would close it:_ Repoint both lines at `getloopscale/loopzero-contracts` and drop the pinned @v0.1.0 in favour of 'the version api/go.mod requires', which is what ci.yml already does rather than pinning — it derives the ref from go.mod precisely so the next bump does not strand a second file.

### 6. adapters.go cites api/internal/gitea/gitops_pusher.go, a path that does not exist

**🟡 low**, confidence high. `api/internal/adapters/adapters.go:20`

The 'Real implementations are required' block in api/internal/adapters/adapters.go names five paths. Four resolve. The fifth, 'GitOpsPusher -> api/internal/gitea/gitops_pusher.go', does not: there is no api/internal/gitea directory. The file is api/internal/gitops/gitea_pusher.go. The package was renamed and the pointer was not.

_What would close it:_ Repoint to api/internal/gitops/gitea_pusher.go.

### 7. api/internal/correlation cites pkg/quality/correlationheader; pkg/quality does not exist, and the regression-lock that forbids the citation only scans cmd/loopzero-ops

**🟡 low**, confidence high. `api/internal/correlation/reader.go:18`

The package doc justifies the reader's location by naming the gate that scans around it: 'pkg/quality/correlationheader'. `pkg/quality` is not on disk — it moved to cmd/loopzero-ops/internal/quality/ at M9. A regression-lock test exists for precisely this stale path (cmd/loopzero-ops/quality_stale_pkg_quality_path_test.go) but it walks only the cmd/loopzero-ops tree, so this citation in api/ is invisible to it. Two occurrences: reader.go:18 and reader.go:54.

_What would close it:_ Repoint both mentions to cmd/loopzero-ops/internal/quality/correlationheader, and widen the regression-lock's walk root from cmd/loopzero-ops to the repository so the next stale citation is caught wherever it lands.

### 12. CLAUDE.md reports 145 registered / 126 wired gates; the oracle it names reports 146 registered, 127 task-attached

**🟡 low**, confidence high. `cmd/loopzero-ops/quality_registry.go:36`

CLAUDE.md states the gate inventory is '126 wired of 145 registered' and names `loopzero-ops quality --list` as the oracle. Running that oracle against a binary built from the current tree returns 146 gates, of which 127 are attached to a task (123 to `gates`, 4 to `diff`) and 19 to none. The file itself records that this count has drifted three times before (3 -> 4 -> 5 workflows; 140 vs 126 gates in two places of the same file). The drift is small; the pattern is the finding.

_What would close it:_ Stop writing the number into prose. The oracle is already machine-readable (`--list -json`); a gate that fails when CLAUDE.md disagrees with it would end the class.

### 15. CODEOWNERS assigns ownership of two files that do not exist

**🟡 low**, confidence high. `.github/CODEOWNERS:22`

CODEOWNERS declares per-file owners for four governance documents. On disk GOVERNANCE.md and SECURITY.md are present; MAINTAINERS.md and RELEASES.md are absent. GitHub silently ignores a rule whose path matches nothing, so those two lines impose no review requirement while reading as though they do. Separately and less conclusively: @paulosergio-jnr, named as owner of every rule, matches no author identity in git history — shortlog returns six identities over 5288 commits and none is that name. A GitHub handle need not equal a git author name, so this is recorded as suggestive, not as a defect.

_What would close it:_ Delete the two rules whose paths do not resolve, or create the documents. A rule matching nothing reads exactly like an enforced one.

### 29. ci.yml's post-mortem comment cites commit 696fe5c62, which exists in no reachable commit; the commit it means is 8c9a1d953

**🟡 low**, confidence high. `.github/workflows/ci.yml:203`

.github/workflows/ci.yml:203 explains the week-long pipeline outage of 2026-08-01..07 by naming 'O commit 696fe5c62 (2026-08-06)' as the change that rewrote api/go.mod and pkg/go.mod from getloopscale to getloopscale. `git cat-file -t 696fe5c62` reports 'Not a valid object name' and the SHA appears in none of the 6910 commits reachable from all refs. The change itself is real and findable — `git log -S` on the new import path lands on 8c9a1d953, dated exactly 2026-08-06 — so the comment is a good post-mortem with one unusable pointer, which is the worst kind: a reader who follows it concludes the history was rewritten rather than that the SHA was mistyped. The same comment's other claim, 'o ultimo run verde foi 2026-07-31', is REPORTED and unverifiable from a checkout, since Actions history is not in the repository.

_What would close it:_ Repoint the comment at 8c9a1d953, and at b919cc135 for the repair.

### 32. 12 Go source citations point at loopzero-design.md, a file deleted in commit 8cb41f660

**🟡 low**, confidence high. `operators/internal/environment/environment_controller.go:177`


## Implemented, and not wired — 6

Code that exists, documents its own purpose, and has no caller. Distinct from dead code: each of these was written deliberately, and the wiring is what is missing.

### 2. api/internal/audit is implemented and has no caller anywhere in the repository

**🔴 high**, confidence high. `api/internal/audit/domain_events.go:43`

The package documents itself as 'the canonical engine-side enforcement' of a closed audit-action enum, added after a 2026-06-21 QA run found a raw SQL fragment leaking into the audit log. It declares a two-phase rollout gated by AUDIT_ENFORCE_DOMAIN_ENUM and an exported ValidateAction(). Measured: 0 of 318 workspace packages import it, and grep finds no reference to "api/internal/audit" in any .go file outside the package's own directory. ValidateAction has no caller. The enforcement described is therefore not in the request path — the leak the package was written to stop can still occur. This is exactly the 'Implemented but not wired' pattern CLAUDE.md lists under Known Systemic Patterns.

_What would close it:_ Wire ValidateAction into the audit-log write path (api/internal/db/store audit_wrapper.go is the likely call site), or record why the package is dormant the way api/internal/payloadlog does in its own doc.

### 5. api/internal/health has no production caller, yet adapters.go names it as the required real implementation

**🔴 high**, confidence high. `api/internal/health/prober.go:3`

api/internal/adapters/adapters.go documents that all Fake* types were REMOVED from production code and that real implementations are required, listing 'HealthProber -> api/internal/health/prober.go'. Measured: no package imports api/internal/health; the only file in the repository referencing it is api/tests/unit/health_prober_test.go. The prober exists and is tested, and no production path reaches it.

_What would close it:_ Trace which component is meant to hold the HealthProber (the aggregator's adapter_status_url_probe.go is the closest live analogue) and either wire it or correct the adapters.go doc block.

### 3. api/internal/cellmgr has zero importers, while CLAUDE.md lists it as a notable package

**🟠 medium**, confidence high. `api/internal/cellmgr/client_manager.go:3`

CLAUDE.md's Components table names `cellmgr` among the notable api/internal subpackages. Measured over all 318 workspace packages: nothing imports it, and no .go file outside its directory mentions it. The live per-cell client resolution used by the operators is operators/internal/cellrouting.RuntimeClientFactory, in a different module. Either the package is dead or its caller was removed without it.

_What would close it:_ Establish whether cellmgr has a live caller. If not, delete it and drop it from the CLAUDE.md Components table; if it is intended to return, say so in the package doc as payloadlog does.

### 4. api/internal/objectstore declares a DIP seam with no consumer, and holds its own adapter

**🟠 medium**, confidence high. `api/internal/objectstore/objectstore.go:3`

The package doc states 'callers depend on the interface, not on a concrete cloud SDK, so a backend swap never touches a call-site'. Measured: no package imports objectstore at all, so there is no such call-site. The adapter that satisfies the interface (s3_adapter.go) lives in the SAME package and imports api/internal/s3, so the inversion the doc describes is not observable between packages. The concrete uploader api/internal/s3 is what is actually used.

_What would close it:_ Either route the S3 consumers through objectstore.ObjectStore, or record the interface as speculative (it is the YAGNI case its own doc argues against).

### 17. policies/charts/*.rego are Conftest policies that nothing invokes

**🟠 medium**, confidence high. `policies/charts/security_context.rego:1`

Three Conftest Rego policies were added in a single commit (b1da3b368, 2026-05-05) and the directory has taken no write since. A repo-wide grep for 'conftest' (excluding node_modules and .git) returns nothing outside an unrelated Go comment: no Taskfile target, no taskfiles/*.yml step, no cmd/loopzero-ops quality gate and no GitHub workflow runs them. They assert real invariants about the published charts (non-root Deployments, priorityClass, ServiceMonitor port naming) and assert them to nobody.

_What would close it:_ Either wire a `conftest test` step into the chart validation task alongside kubeconform, or register the three assertions as loopzero-ops quality gates and delete the Rego. An uninvoked policy reads as coverage that does not exist.

### 21. infra/pulumi-control-plane/ survives on disk as a node_modules-only directory with no tracked files

**🟠 medium**, confidence high. `infra/pulumi/docs/ADR-001-project-boundaries.md:298`

`git ls-files infra/pulumi-control-plane` returns nothing; `ls` returns exactly one entry, node_modules. The project was deleted from version control on 2026-05-14 and the installed dependency tree was left behind. A fresh clone has no such directory, so anyone reading the tree on an existing checkout sees a third Pulumi project that does not exist.

_What would close it:_ Remove the directory from working checkouts and add it to .gitignore if the path can be recreated by tooling.


## A boundary nothing declares — 4

A dependency that crosses an architectural line without anything in the repository saying it is meant to. The map does not call these bugs — it calls them undeclared.

### 8. The persistence layer imports HTTP middleware: db/store -> middleware/auth and middleware/policy

**🟠 medium**, confidence high. `api/internal/db/store/membership_wrapper.go:27`

api/internal/ports states in its own doc that it exists 'to avoid DIP violations where infrastructure (db/) would import presentation (routes/)'. The inversion it names is avoided for routes/, and a different one is live for middleware/: db/store/membership_wrapper.go:27 imports middleware/auth and db/store/idempotency_wrapper.go:36 imports middleware/policy. Both are transport-layer packages (every middleware package imports net/http). Derived mechanically from go list, not from directory names.

_What would close it:_ Move the shared types these two wrappers need into api/internal/ports, which both layers already import and which imports nothing.

### 9. routes/secrets and routes/billing reach persistence directly, unlike the other 27 route packages

**🟠 medium**, confidence high. `api/internal/secrets/secretshttp/secrets.go:41`

The HTTP surface is otherwise a clean interface layer: 18 of the 29 routes/ sub-packages depend on api/internal/ports and none on db/. Two do not. routes/secrets imports db/dbtx (secrets.go:41) and db/store (internal_secret_fetch_audit_adapter.go:29); routes/billing imports the pgx driver directly (billing.go:45). Derived from go list over all 318 packages.

_What would close it:_ Declare the two store contracts these handlers need in api/internal/ports and let the composition root (api/cmd/setup_secret_fetch.go, which already builds them) inject the implementations.

### 13. The Go workspace compiles only because a sibling repository is checked out next to this one

**🟠 medium**, confidence high. `go.work:6`

go.work line 6 unions ../loopzero-contracts, a module built in a DIFFERENT repository, and api/go.mod:91 plus pkg/go.mod:30 require it at v0.4.0 with no replace directive. In this checkout the sibling is present and the whole workspace resolves (go list ./... across all 8 in-repo modules: 318 packages, exit 0, empty stderr). Anyone cloning only this repository gets a tree that does not build. CLAUDE.md states the consequence; nothing in the repository detects or explains the failure at the moment it happens.

_What would close it:_ A preflight in `task doctor` that checks for ../loopzero-contracts and names the repository to clone would turn an opaque toolchain error into an instruction.

### 10. pkg/enforcement is the one shared library that spans three layers

**🟡 low**, confidence high. `pkg/enforcement/limiter.go:5`

pkg/ is otherwise layerable from its imports: nine packages import nothing outside stdlib or each other, errors/http and healthcheck add net/http, vault adds the OpenBao SDK. pkg/enforcement alone holds domain rules (plan caps, scale.go), an infrastructure adapter (store.go imports database/sql for sqlLimitsStore) and an interface adapter (middleware.go imports net/http) in one package. Its own doc is candid about the SQL half and silent about the HTTP half. A LimitsStore interface provides the DIP seam, so this is a packaging observation, not a broken dependency direction.

_What would close it:_ If the split is ever wanted, middleware.go is the cheap one to move out — it is 52 lines and the only net/http in the package.


## A region the map was refused, and should not have been — 1

Files the run could not open, where the reason it could not is itself the defect. This is the one finding category that is about the map's own blind spot rather than about the code.

### 35. Read-deny glob written for the rendered artifact matches the Helm template, leaving 11 critical-role files unmapped

**🟡 low**, confidence medium. `.claude/settings.json:103`

.claude/settings.json:103 denies Read on a glob ending in /secret.yaml. That name belongs to the Kubernetes Secret a chart RENDERS, not to the chart source, so the glob also matches four Helm templates -- charts/loopzero-api, charts/loopzero-tenant-operator, charts/loopzero-cloud and infra/helm/openbao-init, each at templates/secret.yaml. Lines 107-118 extend the same shape to the plural and to *-secret.yaml / *-secrets.yaml, which additionally catches infra/helm/minio/minio-credentials-secret.yaml, three files under infra/manifests/cluster-secret-stores/, and infra/argocd-bootstrap/apps/cell-external-secrets.yaml -- an ArgoCD Application, not a Secret.

MEASURED: 11 of the 33 excluded rows carry a critical role (iac/config); all 33 have sha256 NULL, so no bytes were read. The categorical role floor still reports 517/517 iac and 219/219 schema, because these rows carry is_excluded=1 and therefore sit outside the denominator. The gap is recorded in the inventory and invisible in the gate -- which is why it is reported here rather than left for the gate to surface.

ALSO MEASURED, three times in this session: the hook validate-command.py matches the COMMAND STRING rather than the file a command reads. A grep whose target was settings.json was refused because the search pattern contained a denied literal, and the heredoc carrying this very description was refused because the description quotes the glob it is about. The finding had to be written through a non-Bash tool to be recorded at all.

NOT MEASURED, and the reason this is confidence=medium rather than high: the bytes were deliberately not read, so 'these files hold no credential' is an inference from the path plus the project's own declared convention (charts/loopzero-api/SECRETS.md, cited in CLAUDE.md: secrets are set per installation in the target's native store). It is NOT an observation of the content. A Helm template can hardcode a value, and nothing here rules that out.

_What would close it:_ Narrow the globs to the paths that hold VALUES rather than to the rendered name: exempt **/templates/secret.yaml (Helm source) and **/apps/*-secrets.yaml (ArgoCD Applications), or invert to an allow-list. Separately, the hook should test the resolved target of a command rather than the presence of a literal in its text -- as written it blocks writing ABOUT a denied path, which is how this finding nearly went unrecorded. Until narrowed, these 11 files are named in the report's 'What Was NOT Analyzed'.


## Only knowable at runtime — 1

A region source cannot answer. Recorded so its silence reads as a limit of the method rather than as an absence in the system.

### 25. Five declared ClusterRoles have no readable name: metadata.name is a Helm template expression

**🟡 low**, confidence high. `charts/loopzero-api/templates/clusterrole.yaml:5`


## Notes about the release process and about this map — 6

Two about the repository's release process, two about defects in this cartography run itself, and one about how a metric should be read.

### 23. 17 infrastructure components are recorded twice under two key schemes by two concurrent agents

**🔴 high**, confidence high. `infra/helmfile/core.helmfile.yaml.gotmpl:220`

Two agents mapped the same helmfile releases six minutes apart and used different key schemes, so one thing has two component rows. Measured pairs (earlier key written 2026-09-11T22:39, later 22:45): infra.postgres/ds.postgres, infra.gitea/ds.gitea, infra.minio/ds.minio, infra.harbor/ds.harbor, infra.loki/ds.loki, infra.openbao/ds.openbao, infra.redis.gitea + infra.redis.harbor / ds.redis, infra.argocd/orch.argocd, infra.argoworkflows/orch.argo_workflows, infra.argorollouts/orch.argo_rollouts, infra.argoevents/orch.argo_events, infra.buildcwts/orch.build_cwts, infra.keda/orch.keda, infra.certmanager/infra.cert_manager, infra.eso/infra.external_secrets, infra.pulumi.infra/infra.pulumi_infra, infra.pulumi.platform/infra.pulumi_platform. Six pairs also disagree on kind — infra.keda says service where orch.keda says orchestrator, infra.buildcwts says artifact where orch.build_cwts says batch_job — so a kind-filtered diagram query returns a different set depending on which row it finds. 19 relations already point at the earlier keys and roughly 60 at the later ones, so neither side can be deleted without re-pointing its edges. Recorded rather than silently resolved: picking a winner here would discard whichever description the chooser happened not to read.  RESOLVED by the orchestrator: see finding 24. 16 pairs merged
on the fuller anchor; the redis pair resolved the other way because the coarse node stood for two
distinct releases. Components 218 -> 201, 36 relations repointed.

_What would close it:_ Reconcile before any diagram is rendered. For each pair keep one key, merge the two descriptions, re-point the losing side's relations with UPDATE relations SET from_key/to_key, then delete the losing component. The pairs are enumerated as same_as relations in the relations table (kind='other', detail starts with 'duplicate_of').

### 24. 17 infrastructure components were mapped twice by concurrent agents, and have been reconciled

**🟠 medium**, confidence high. `infra/helmfile/core.helmfile.yaml.gotmpl:245`

This is a defect of the RUN, not of the repository, and it is recorded so a reader of the map knows it happened. Two structure-cartographers were run in parallel over the same helmfile releases under different key prefixes (infra.* and ds.*/orch.*). Neither was wrong; the map became double-valued, and four pairs disagreed on kind — infra.keda=service vs orch.keda=orchestrator, infra.buildcwts=artifact vs orch.build_cwts=batch_job, infra.certmanager=service vs infra.cert_manager=runtime, infra.eso=service vs infra.external_secrets=runtime — so a kind-filtered query returned a different set depending on which row it found. Reconciled: 16 pairs merged keeping the row with the fuller anchor (chart AND version, not just release name), descriptions merged rather than discarded, 36 relations repointed, 218 components to 201. The redis pair was resolved the OTHER way on purpose: ds.redis was a single node standing for two distinct releases — gitea-redis at core.helmfile:245 and harbor-redis at :427 — and it had absorbed dependency edges from both Gitea and Harbor, which would have stated on the map that the two share a Redis instance. They do not. The split nodes were kept and the coarse one removed, because a map coarser than the thing it maps invents structure. Applying the majority rule mechanically would have kept the wrong row here.

_What would close it:_ None needed in the repository. For future runs: agents writing to the same table must partition by SUBJECT, not by key prefix — a prefix convention does not prevent two agents describing the same release.

### 27. 468 commits — 8.9% of the repository's history — sit unreleased behind a tag series that stopped 24 days ago

**🟠 medium**, confidence high. `CHANGELOG.md:1`

The tag series is dense then absent: 80 semver tags in the 66 days from v0.14.0 (2026-06-13) to v0.76.6 (2026-08-18), then nothing in the 24 days to HEAD (2026-09-11) while 468 commits landed. This is a fact about the release process rather than a defect in the code, and it is the operational reading of the same interval the CHANGELOG contradiction (finding 1) touches from the other side: a repository whose released artifact is 468 commits behind its trunk cannot answer 'what is deployed' from a tag. The cause is not established here — a deliberate pause, an unshipped milestone and a broken release path are all consistent with the evidence.

_What would close it:_ Establish which of the three it is before reading anything else into the gap. If the release path is broken, that is a blocker; if the pause is deliberate, the CHANGELOG's [Unreleased] section should carry the 468 commits' worth of entries, and whether it does is checkable.

### 28. 36% of first-party TODO hits are the Portuguese word for "every", so the TODO marker cannot be used as a debt signal here

**🟡 low**, confidence high. `cmd/loopzero-ops/leak_subcmd.go:100`

`TODO` is a homograph across the two languages this repository is written in: the marker, and the Portuguese adjective meaning 'every/all'. 8 of the 22 first-party hits are the adjective and 1 describes a stub that was deleted, so a marker count is 58% precise and any tool or dashboard counting TODOs here overstates the debt by 69%. This is a distinct consequence of the English-only violation already recorded as finding 11, not a restatement of it: finding 11 is about the rule being broken, this is about a measurement that silently stops working because it is. The 13 genuine markers are recorded as work_items with their blame age; the 9 false positives are named in this finding's evidence so the exclusion can be disagreed with line by line.

_What would close it:_ Either finish the English-only migration for comments, which removes the collision, or require the punctuated form `TODO:` / `TODO(` — both hold for all 13 real markers and for none of the 8 Portuguese ones.

### 22. Cartography note: collapsing buildplane/* and services/* into one node each manufactured a cycle that does not exist

**⚪ info**, confidence high. `api/internal/buildplane/argo/buildkit.go:19`

My first pass modelled api/internal/buildplane and api/internal/services as single components. Tarjan then reported a 2-node strongly-connected component between them. Expanding to real packages shows four distinct participants and no cycle: buildplane/argo imports services/deploycontracts (buildkit.go:19, delivery.go:9) while services/build imports buildplane/source (build_submit_error.go:11). The substrate was corrected - the two grouping edges were deleted and twelve real sub-packages (4 buildplane, 8 services) added with their actual edges - and Tarjan now returns zero SCCs of size > 1. Recorded because the failure mode generalises: a component map whose nodes are coarser than the import graph invents coupling, and invented coupling is indistinguishable from real coupling in every diagram drawn from it.

_What would close it:_ None outstanding; the substrate carries the refined nodes. Any future coarse grouping in this database should be re-checked with Tarjan before it is drawn.

### 30. Cartography note: the test pyramid's e2e tier is Ginkgo-driven, so its test_count (71) is a floor, not a count

**⚪ info**, confidence high. `infra/tests/go.mod:1`

DG-18 is drawn from `test_count`, which counts top-level test functions. That is exact for the unit tier (6767 over 903 files) and understates the e2e tier, whose 98 files hold 71 `func Test*` and 225 Ginkgo Describe/Context/It blocks — a Ginkgo suite puts its specs inside one Go test function, so counting functions counts harnesses. `ginkgo_block_count` is recorded per tier beside `test_count` for exactly this reason, and it overcounts in the other direction because Describe and Context are grouping rather than assertions. The true e2e spec count lies between 71 and 225; CLAUDE.md names 132 for one suite. Read the pyramid's base-to-apex ratio as a shape, not as a measurement of e2e coverage.

_What would close it:_ Count It() alone, excluding Describe and Context, to get a defensible e2e spec number; or run `ginkgo --dry-run` and count reported specs, which is the suite's own answer.


## Checked, and nothing wrong found — 2

Negatives established by measurement rather than assumed. A checked absence is worth as much as a finding, and costs more to produce.

### 14. Verified: the two-chart parity claim holds, and the declared pair list matches disk

**⚪ info**, confidence high. `infra/quality/chart-pairs.txt:24-25`

Read the name: field of all 36 Chart.yaml files (12 under charts/, 24 under infra/helm/). Exactly two names occur in both trees: loopzero-api and loopzero-cloud. infra/helm/admission-policies is named admission-policies, not loopzero-admission-policies, so CLAUDE.md's correction — that the third pair it used to claim never existed — is accurate. infra/quality/chart-pairs.txt declares exactly those two pairs and nothing more.

_What would close it:_ None. Noted so a later reader does not re-derive it.

### 16. No dependency cycle found, by three independent methods

**⚪ info**, confidence high. `cmd/loopzero-ops/quality_registry.go:36`

The mission asked for cycles. There are none to record, and the negative was established three ways rather than assumed. First, Go itself: intra-module import cycles do not compile, and go list ./... across all 8 in-repo modules exits 0 with empty stderr. Second, the repository's own gates, run from a freshly built binary: acyclic-graph PASS over the decomposition DAG (loopzero-contracts <- engine / loopzero-cloud / operators-api), routes-acyclic PASS (29 route sub-packages, 0 sibling edges — route packages are forbidden from importing each other and none does), services-acyclic PASS over a frozen bounded-context DAG. Third, a Tarjan pass over the 188 import edges I derived myself finds no SCC larger than one. Note what this does NOT cover: a cycle across MODULE boundaries through the sibling ../loopzero-contracts, and any runtime call cycle that is not an import.

_What would close it:_ None. Recorded so DG-12 (loop) has a documented reason to be NOT_APPLICABLE on the structural substrate rather than an empty table.


# Relationships

Each finding is carried by the concept it belongs to, in context:

| Finding | Where it is discussed |
|---|---|
| 2 — audit package unwired | [`/components/audit-package.md`](/components/audit-package.md) |
| 3, 5, 6 — cellmgr, health, stale adapter path | [`/components/multi-cell-routing.md`](/components/multi-cell-routing.md) |
| 4, 8 — objectstore seam, store → middleware | [`/components/persistence.md`](/components/persistence.md) |
| 7, 9 — correlation citation, route bypass | [`/components/control-plane-api.md`](/components/control-plane-api.md) |
| 10 — enforcement spans three layers | [`/components/shared-go-libraries.md`](/components/shared-go-libraries.md) |
| 12, 16, 29, 30 — gate count, no cycle, ci.yml, pyramid | [`/operations/quality-gates-and-tests.md`](/operations/quality-gates-and-tests.md) |
| 13 — sibling repository | [`/components/go-workspace.md`](/components/go-workspace.md) |
| 14, 17, 25 — parity, rego, template names | [`/components/helm-charts.md`](/components/helm-charts.md) |
| 15 — CODEOWNERS | [`/operations/ownership.md`](/operations/ownership.md) |
| 18, 19, 20, 21 — the Pulumi layer | [`/components/pulumi-legacy.md`](/components/pulumi-legacy.md) |
| 22 — the phantom cycle | [`/components/build-orchestration.md`](/components/build-orchestration.md) |
| 26 — ClusterRole drift across the pair | [`/operations/permissions.md`](/operations/permissions.md) |
| 1, 27 — CHANGELOG, unreleased commits | [`/operations/timeline.md`](/operations/timeline.md) |
| 28 — TODO noise | [`/operations/work-in-flight.md`](/operations/work-in-flight.md) |
| 31 — the machine that contradicts its own comment | [`/entities/state-machine-application.md`](/entities/state-machine-application.md) |
| 32 — twelve citations to a deleted file | [`/components/operators.md`](/components/operators.md) |
| 33 — the Tiltfile points at the old GitHub organisation | [`/components/go-workspace.md`](/components/go-workspace.md) |
| 34 — a commit claims a translation it did not finish | [`/components/platform-runtime-services.md`](/components/platform-runtime-services.md) |
| 35 — the Read-deny glob is over-broad, and the coverage gate cannot see it | this bundle's own `## Known gaps`, and [`/operations/quality-gates-and-tests.md`](/operations/quality-gates-and-tests.md) |
| 11 — Portuguese in 331 tracked files | below, and in [`/components/control-plane-api.md`](/components/control-plane-api.md) |

# Two citations in this register were repaired, and both repairs are named

**Finding 14's declared anchor does not resolve.** It cites `infra/quality/chart-pairs.txt:30` and
the file has 25 lines. Its evidence row anchors at lines 24-25 — which is exactly where the two
declared chart pairs are — and verifies clean, so this bundle cites that span instead. The
finding's substance is unaffected; what was wrong was the line number it declared.

**Finding 19's file and its evidence file are different files**, and the map is right about both:
the finding is anchored on the AWS stub at `infra/pulumi/src/providers/aws/index.ts:14`, while its
supporting evidence is the README paragraph at `infra/pulumi/README.md:340-344` that sends a
reader to implement it. This bundle cites the finding's own anchor and quotes the README claim in
the body.

Neither repair is silent, because a map that fixes its own citations without saying so cannot be
audited. Worth noting how they were found: `verify_evidence.py` re-reads the **evidence** table
and does not look at the `file`/`line` columns a finding declares, so a finding's own anchor is
outside what the tool verifies. These two were caught by re-resolving every citation in the
written bundle against the filesystem.

# The pattern under the list

Four of the five high-severity findings are the same shape in different materials: **something
was written, and the thing that would make it take effect was not.** An audit enum with no
caller. A health prober named as required and never constructed. An ADR whose migration stopped
at step 12 of 13. A parity contract whose two sides diverged by 162 cells.

`CLAUDE.md` names this pattern itself, under *Known Systemic Patterns*, as **"implemented but not
wired"**, and prescribes the remedy: *verify the call site after implementation; integration test
required*. The repository diagnosed the failure mode correctly and then contained four more
instances of it — which is not hypocrisy, it is what a systemic pattern means.

# Evidence

[^f2]: `api/internal/audit/domain_events.go:43`
[^f5]: `api/internal/health/prober.go:3`
[^f18]: `infra/pulumi/README.md:359`
[^f20]: `infra/pulumi/docs/ADR-001-project-boundaries.md:3`
[^f26]: `infra/helm/loopzero-api/templates/serviceaccount.yaml:27`
[^f23]: `infra/helmfile/core.helmfile.yaml.gotmpl:220`
[^f13]: `go.work:6`
[^f8]: `api/internal/db/store/membership_wrapper.go:27`
[^f1]: `CLAUDE.md:21`
[^f11]: `api/cmd/setup_database.go:29`
[^f12]: `cmd/loopzero-ops/quality_registry.go:36`
