# Releases

How a change reaches a version, and what a version number means here.

## The branch flow

```
workspace ──PR──> develop ──PR + semver tag──> main
 (work)          (integration)                 (release)
```

Work is born on `workspace` — every change, including a one-line fix. `develop`
integrates and never originates. `main` receives only release merges, each carrying a
semantic-version tag.

Measured 2026-09-16: **80 semver tags**, the most recent `v0.76.6`.

## Two cuts, and they answer different questions

| Cut | Version | Says |
|---|---|---|
| pre-release | `X.Y.Z-rc.N` | this batch is installable; the scope is not finished |
| final | `X.Y.Z` | a milestone closed and was validated against the released artifact |

An `rc` never becomes a release — it is superseded by one. The first `rc` bumps the
core version and every `rc` after it only advances the counter, so `0.2.0` goes
`0.3.0-rc.1` → `0.3.0-rc.2` → `0.3.0`. Bumping again at the final would publish a
number nobody testing the candidates recognises.

## What decides the bump

Read from `CHANGELOG.md`'s `[Unreleased]` section, not from anyone's judgement:

| Bump | Trigger |
|---|---|
| `major` | a `### Removed` entry, or a `### Changed` entry opening with `BREAKING:` |
| `minor` | a `### Added` entry, and no major trigger |
| `minor` | only `### Changed`, with at least one entry — see below |
| `patch` | only `### Fixed` or `### Security` |

**Why a `Changed`-only release is `minor`.** Under `0.x` the fact that decides it —
does this change a behaviour a caller depends on? — is not in the CHANGELOG, and the
two possible errors are not symmetric. Guessing `patch` ships a break silently to
everyone on a caret range, which is the exact failure semantic versioning exists to
prevent. Guessing `minor` makes a version number larger than it needed to be. The rule
takes the recoverable error.

## Before a version is cut

`CHANGELOG.md` is the contract with whoever consumes this, so the entry comes first and
the tag follows it. Entries are written for the consumer: *"a deploy no longer creates
a duplicate release"*, not *"changed the error handling in build_reconcile_retry.go"*.

## Support

There is no long-term-support branch and no backport policy, because with one
maintainer promising either would be a promise nobody can keep. Fixes land on
`workspace` and reach you in the next release.
