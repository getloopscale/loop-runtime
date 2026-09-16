# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/) and this project adheres
to [Semantic Versioning](https://semver.org/).

> **History before 2026-09-11 is in git, not in this file.** The previous `CHANGELOG.md`
> held 4,802 lines and was removed in `4f728eb7a`. Recover it with
> `git show 4f728eb7a~1:CHANGELOG.md`, or read the release history with
> `git log --oneline`.

## [Unreleased]

### Fixed

- The local development edge no longer hardcodes one machine's home directory. The dashboard
  root in `infra/local/Caddyfile` reads `{$LOOPZERO_DASHBOARD_DIST:../loopzero-cloud/dashboard/dist}`,
  so the file works from any checkout instead of exactly one, and the runbook naming that path
  was corrected in the same change.
- Four tests named documentation paths that the six-document consolidation retired. Three were
  repointed at where the content actually lives; the fourth is retired, because its subject left
  with the dated audit trail and there is nothing to repoint at.
- Thirteen test helpers located the repository root by looking for a file this repository
  deleted, and reported the search rather than the cause.
- The Tiltfile named the pre-rename organisation and a four-minor-versions-old release of the
  contracts module that `api/go.mod` and `pkg/go.mod` already require.

### Fixed

- The shell-script allowlist is versioned again, at `infra/quality/shell-scripts-allowlist.md`.
  It lived under `.claude/`, which is an installed dependency and is not tracked, so six
  quality gates read a file that existed on one machine and in no clone — four of them
  failed for every contributor and every CI run. The 768-line record was recovered from the
  commit before the one that untracked the tooling tree; the content is unchanged.
- A missing allowlist now means *no exemptions declared* rather than a fatal read error, in
  the two gates whose question is "may this be exempt?". A gate whose default exempts
  nothing cannot be weakened by losing its file; a file that is present and unreadable is a
  different claim and stays fatal.
- Thirteen test helpers located the repository root by looking for `ROADMAP.md`, a file this
  repository deleted. They use `go.work` — which defines the nine-module workspace and
  exists by construction.
- Seven documentation citations pointed at `pkg/quality/`, relocated at M9. Four were in
  sqlc-generated files and were fixed in the `.sql` sources so regeneration preserves them.
- A guard against stale `pkg/quality/` citations reported itself, because a guard for a token
  must spell the token. Its sibling exempted itself by hardcoded filename and that exemption
  did not grow when the second guard was written; both now key on the class.

### Changed

- Twenty-six tests over `.claude/hooks/delivery-gate.sh` are retired. `hooks.json` declares
  nine hooks and none of them is it — the mechanism was removed from the kit, and its
  replacement (`.git/hooks/pre-push`) has an interface nothing like it. What they asserted,
  and what their retirement costs, is recorded in `cmd/loopzero-ops/delivery_gate_history.go`.
- Two RED suites for registered-but-unimplemented items (B-001, B-048) skip by default and
  name the item and the measurement in the skip message. `LOOPZERO_RUN_B001_RED=1` and
  `LOOPZERO_RUN_B048_RED=1` arm them; both still fail when armed, so the skip hides nothing.

### Added

- `MAINTAINERS.md` and `RELEASES.md`, both declared by `.github/CODEOWNERS` and both absent.
- `docs/operations.md` gains a Troubleshooting section, written from failures this repository
  has actually produced: a root-level `go build` that covers almost nothing, the absent
  sibling module, an e2e suite that reports success having skipped all 132 specs, Ginkgo's
  own timeout, and the seed's legacy database URL.

### Changed

- The delivery-gate premise check asks which regime the repository is in before it
  asserts anything. Its three premises defend a specific trade — CI may run post-merge
  because a local hook blocks a bad push first — and that trade cannot hold for a public
  repository, where a contributor has no such hook and a fork's workflow receives no
  secrets. With `ci.yml` now triggering on `pull_request`, the guard is CI itself, which
  is verifiable by GitHub as a required check and covers every contributor. The local-hook
  premises remain in the file for a repository that still makes the post-merge trade.

### Added

- Continuous integration now runs on pull requests, not only after a merge. A contribution
  is checked before it reaches `develop` — previously the suite ran only on push to
  `develop`/`main`, so the first automated check on any change happened after it landed.
- A `public-checks` job that needs no repository secrets, so a pull request opened from a
  fork is gated like any other: it builds, vets and tests the six modules that do not
  depend on the private `loopzero-contracts` sibling, and runs the diff-scoped quality gates.
- An aggregated `CI gate` check. Branch protection can require one check instead of five,
  and it fails when an upstream job fails OR is skipped — a skipped required check
  otherwise reports as passing.

### Changed

- Every job runs on GitHub-hosted runners (`ubuntu-latest`). The third-party runner fleet
  is no longer the default anywhere, including in the reusable publish workflow, whose
  `runner` input still accepts a custom label for anyone who has a fleet of their own.
- Jobs that need the private `loopzero-contracts` module are skipped on pull requests from
  forks instead of failing there. A fork receives no secrets, so those jobs could never
  have succeeded; they now report as skipped and the aggregated gate accepts that by name.

### Security

- Workflow permissions are declared per job rather than repository-wide. `packages: write`
  and `id-token: write` were granted to every job in the release workflow, including one
  that only reads the repository.
- The release workflow names the four secrets it passes to the workflow it calls, instead
  of `secrets: inherit`. A secret added later is no longer forwarded by omission.
- `persist-credentials: false` on all nine checkouts: the credential no longer stays in
  `.git/config` on the runner where later third-party steps can read it.
- A branch name is no longer interpolated into a shell command in the quality-gate step.
  On a fork pull request the branch name is chosen by the contributor.

### Added

- `BACKLOG.md` — the repository's maintenance registry, created at the root of the governed
  scope (`single-repo`). Opened with 33 items, each carrying a measured evidence pointer from
  the 2026-09-11 system-cartography run (finding id in `cartography-output/cartography.db`
  plus the `file:line` it cites); since grown by DISCOVER sweeps, which register their own
  findings directly. Items are grouped by the quality pillar their
  Definition of Done moves — nomenclature, clarity, organisation, cohesion, domain
  separation — and six of them are blocked on the gate that must fail before they can be
  verified.
- `.squad/domain-routing.txt` — the project's domain routing table, derived from disk:
  7 domains (`loopzero`, `api`, `cmd`, `infra`, `operators`, `pkg`, `tools`), all resolving.

### Fixed

- Domain routing was FATAL for every repository in this project. `rules/domain-routing.txt`
  carried no domain rows at all — only comments plus a stray `grep` command that had landed
  in the file — so `route_domain.py` exited `UNROUTED` for all 7 repos and no backlog item
  could have been assigned to anyone. The table is now derived and verified.

### Changed

- `docs/` consolidated from 1,374 versioned markdown files across 17 subtrees into **six living
  documents** — `architecture.md`, `loopzerokit-loopzerocloud-seam.md`, `api-contracts.md`,
  `operations.md`, `runbooks.md`, `decisions.md` — plus `README.md` as the single index.
  Everything removed stays in git at `4f728eb7a`.
- `CLAUDE.md` repointed at the consolidated paths: the locked LoopZeroKit ↔ LoopZeroCloud rule now
  cites `docs/loopzerokit-loopzerocloud-seam.md`, and the paragraph describing `docs/` as a mix of
  living reference and audit trail now describes the six-document tree.
- Code-quality auditing is enabled for the `api` module — 97 of the workspace's 321 packages.
  It audited nothing at all before this: no language was enabled, so every plan scored in this
  repository was capped `INVALID` regardless of its own quality, and `/implement` was unreachable.
  Pre-existing dead-code findings are recorded as a baseline (14 rows) so they stay countable
  without failing changes that did not cause them, and Go mutation testing — which the toolchain
  defers unconditionally for every Go project, so the finding says nothing about this codebase —
  is exempted **until 2026-12-12**. On that date the exemption lapses, the finding returns at full
  severity and every plan drops back below the band `/implement` requires; renew the row in
  `.claude/rules/code-quality-allowlist.txt` or adopt a Go mutation runner (#B-034, #B-088)

### Removed

- The dated audit trail under `docs/` — completed plans, baselines, meeting records,
  roadmap runs, per-iteration implementation logs and review reports. Two thirds of the
  tree. Still readable as history via `git show 4f728eb7a:docs/<path>`.
- The two competing indexes (`docs/README.md` curated in English and `index`
  generated in Portuguese, the latter also breaking the English-only rule for versioned
  artifacts), replaced by one index.
- The duplicate audit trails `docs/wiki/` (934 files) and `docs/knowledge-base/` (126),
  which overlapped in period with zero byte-identical files between them.
- `docs/operations/loopzero-capsim-mcp/` — a 26 MB Node checkout with its own `node_modules`
  living inside the documentation tree, and its now-dangling `.gitignore` entry.
- `docs/api-contracts.md`, which documented `memory:*` scopes belonging to `../loopzero-memory`
  rather than to this engine.

### Fixed

- A deploy stuck in release activation no longer creates a duplicate release each time the
  platform fails to read the release it already has. The read failure was being mistaken for
  "this build has no release yet", which is the one condition that legitimately creates one;
  a database blip during activation could therefore leave up to ten releases behind for a
  single deploy, with nothing recorded to explain them. The failure is now reported and the
  attempt retried on the next pass, and a deploy whose read keeps failing still ends as a
  clear failure on the same schedule as before (#B-069)
- A failed Cloudflare activation check on a custom domain is now reported instead of being
  discarded silently. Domain activation was unaffected either way — the check only speeds up
  detection, and the next pass re-reads the zone regardless — but a rate-limited or failing
  Cloudflare account produced no trace at any log level, leaving slow activations with no
  explanation (#B-069)

### Known regressions

- **The consolidation broke the 82 inbound `docs/*.md` references that still resolved.** Of the
  154 distinct `docs/*.md` paths cited from code, configuration and ADR bodies, 72 were already
  dangling before the cut; all 154 dangle now. No content was lost — every target is at
  `git show 4f728eb7a:<path>` — but a reader following a source comment no longer lands on a
  file. Repointing them means editing hundreds of comments across the tree, which is a separate
  change.
- Links *between* the six consolidated documents all resolve; the regression is inbound only.
