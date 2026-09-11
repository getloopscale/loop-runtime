---
type: design-review
title: What a person must decide before the backlog is filed
date: 2026-09-10
status: draft
---

# What is waiting for your signature

Nine documents. Grouped by **what you would actually be asserting**, because the
three groups need different amounts of your attention.

---

## 1 · Seven domain specialists — `.claude/agents/`

Each was drafted from measurement. The table below is the part that is
**re-runnable** — disagree with it by re-running it.

| Agent | Files | Lines | Commits | Tests | Build |
|---|---|---|---|---|---|
| `api` | 879 | 183 145 | 1063 | 505 | 32s |
| `cmd` | 495 | 102 546 | 341 | 260 | 32s |
| `infra` | 222 | 37 184 | 955 | 206 | 3s + 6s |
| `operators` | 179 | 39 707 | 371 | 84 | 22s + 1s |
| `pkg` | 133 | 22 077 | 138 | 68 | 9s |
| `tools` | 2 | 228 | 4 | 1 | 3s |
| `loopzero` | — | — | — | — | no build at root — `go build ./...` refuses |

**What needs you:** items 3 and 4 of each file — the domain's invariants and the
shape of a real finding. Those I inferred from reading code. They are the file's
whole value and the part measurement cannot supply.

The three I am least sure of:

- **`infra`** — I wrote that `infra/scripts` has 190 commits and zero tests, and
  called it "the module most changed and least protected". True as a count; whether
  it is a *problem* depends on what those scripts do, which I did not establish.
- **`cmd`** — I made the destructive-operation invariant the highest severity, based
  on the `teardown` incident your own Taskfile records. If that incident is not
  representative, the severity is wrong.
- **`loopzero`** — the routing table in it says "hand this item to a specialist". If the
  split is wrong, every item mis-routes.

---

## 2 · The design sign-off — `.squad/wiki/design/sign-off.md`

Five drawings. One question of the seven is now **resolved**; six remain.

| # | Question | Status |
|---|---|---|
| D4 | Does agent state survive a pod restart? | **RESOLVED** — yes. `loopzero-memory`, Postgres, 34 migrations, **17 tables** (corrected 2026-09-11; this said 9). It was never in this repo, which is why the charts did not show it |
| D1 | Is there a suspended state for an idle agent? | open — **backlog** |
| D1 | What happens to a running app during a platform upgrade? No `Draining` phase | open — **backlog** |
| D2 | Egress allowlist for the customer's app? | open — **backlog** |
| D2 | Isolation: account separates BYOC customers, namespace separates projects — **what separates two solo developers on a shared cell?** | open — **BLOCKS the backlog**. Restated 2026-09-11; two thirds of it were already decided in code |
| D3 | Build runs in the customer's cell or on platform infra? | **RESOLVED** — the customer's, by C1 of the extraction RFC, before this drawing asked |
| D4 | Is an in-flight turn checkpointed before it completes? | open — new, from resolving the one above. **Backlog, not a pre-condition** |

**Why D4 looked unresolved from inside `loopzero`:** the renderer emits `Deployment` or
`Rollout`, never `StatefulSet`, and puts no `PersistentVolumeClaim` on the user's path.
Read alone that says the state does not survive. It survives because it lives in
`loopzero-memory` — part of this ecosystem, **treated as ready and working** (decided
2026-09-11). The drawing's contribution is WHERE, not a risk.

---

## 3 · The brainstorm alignment — `.squad/wiki/product/alignment.md`

Predates this session. `score_product_alignment.py` computes the 90% and refuses
to sign; `PRODUCT_ALIGNED` needs a person and a judge may not stand in.

---

## How to sign

```bash
python3 .claude/skills/sign/scripts/sign_document.py --list
python3 .claude/skills/sign/scripts/sign_document.py <doc> --as paulo          # preview
python3 .claude/skills/sign/scripts/sign_document.py <doc> --as paulo --confirm
```

You authored none of these — I did — so the authorship refusal will not fire, and
`--despite-authorship` is not needed.

**A signature asserts that you read it and are willing to say it holds.** It does
not assert that a machine checked it; that is a separate claim, computed separately.

## Related

- [`sign-off.md`](sign-off.md) — the document these questions gate
- [`durability.md`](durability.md) — D4, the one that resolved and took a number with it
