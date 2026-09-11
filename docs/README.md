# LoopZero docs

Engineering reference for the engine. **Six documents, all current truth** — fix one when it
drifts rather than adding a seventh beside it.

| Document | Read it for |
|---|---|
| [`architecture.md`](architecture.md) | the shape of the system — four planes, the Go workspace, deploy flow, CRDs, state machines, multi-cell, the chart dual system, cross-repo contracts |
| [`loopzerokit-loopzerocloud-seam.md`](loopzerokit-loopzerocloud-seam.md) | **mandatory pre-read** before touching `services_json`, the upload contract, the validators or the manifest renderer |
| [`api-contracts.md`](api-contracts.md) | wire protocols, error codes, the stability rules that bind them |
| [`operations.md`](operations.md) | getting the stack up, running the tests, the quality gates, which credentials an environment needs |
| [`runbooks.md`](runbooks.md) | rotating, recovering, upgrading, draining, and responding to an alert |
| [`decisions.md`](decisions.md) | what was decided, when, and whether it is ratified |

## Where else truth lives

`docs/` does not hold all of it, and pretending otherwise is how the tree rotted the first
time.

| | |
|---|---|
| **`CLAUDE.md`** at the repo root | the authoritative operating contract — the rules, the common mistakes, the known systemic patterns. Read it before `docs/`. |
| **The code** | authoritative over both. Where a document and the code disagree, the code wins. |
| **`CHANGELOG.md`** | the dated history of what changed. `docs/` deliberately does not restate it. |
| **`pkg/statemachine/`** | the 11 state machines. The code is the spec — read it for the full state set. The `.dot` diagram sources were removed in this consolidation; recover them with `git show 4f728eb7a:docs/state-machines/`. |

## Conventions

- **Every claim that carries a number states how it was measured**, or says it is an
  estimate.
- **A path in prose must resolve.** If a referenced artifact was deleted, say so inline —
  a reader cannot tell "moved" from "gone".
- **Runbooks state whether a step is automated or manual.** A runbook that describes a
  retired automation as if it still runs is worse than no runbook.
- **English only.** Everything the repository versions is written in English.

## What this tree used to be

Until 2026-09-11 `docs/` held **1,523 markdown files across 19 subtrees** — roughly 303,000
lines. Two thirds were dated audit trail: completed plans, roadmap runs, meeting records,
per-iteration implementation logs, review reports.

It was consolidated into the six documents above, and the rest was deleted. Three
measurements taken before the cut, which are the reason for it:

- **72 of the 154 `docs/*.md` paths cited from code and configuration no longer resolved.**
  Nearly half the tree's inbound references were already broken.
- **Two competing indexes** described the same tree — a curated English `README.md` and a
  generated Portuguese `index.md` claiming 1,199 concepts. The second violated the
  English-only rule for versioned artifacts.
- **Two parallel audit trails** with identical subdirectory names — `wiki/` (934 files) and
  `knowledge-base/` (126) — overlapping in period, with **zero byte-identical files**
  between them.

**Nothing was lost, but it did stop being navigable.** Every deleted file is in git.
Recover one by path:

```bash
git show 4f728eb7a:docs/operations/openbao-init-runbook.md
git log --diff-filter=D --name-only -- 'docs/**'   # what was removed
```

The audit trail is history and remains readable as history. What it stopped doing is
competing with current truth for the reader's attention.
