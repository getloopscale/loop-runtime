# ADR — Exempt the Go mutation deferral with a dated allowlist row

- **Date:** 2026-09-14
- **Item:** B-034
- **Status:** ACCEPTED
- **Decided by:** the autonomous chain, under `rules/autonomy-envelope.md`
  § *A structural decision the contract wants recorded*
- **Governs:** `.claude/rules/code-quality-allowlist.txt`, one row, sunset 2026-12-12

## Decision

Exempt `soft_cap_mutation_deferred_go` with a single six-field row in the project's
code-quality allowlist:

```
go | . | mutation_low | d4 | <reason> | 2026-12-12
```

Edit no plan. Do not touch the baseline to achieve the same number.

## Context — what forced a decision

`code-quality-golden-rule.md` § 5 declares detector D4 (mutation testing) as **Python and
TypeScript only, with Rust and Go deferred**. `detectors/_mutation.py` returns
`_deferred("go", "gremlins")` before any handler lookup, so the finding fires
**unconditionally on every Go project** and is a statement about the toolchain, not about
this repository's tests.

Measured on this tree 2026-09-14, offline path, with the enablement row and the 14-row
baseline already in place:

| allowlist | verdict | cap | soft caps | baselined |
|---|---|---|---|---|
| empty | `FAIL_SOFT` | 70 | `soft_cap_mutation_deferred_go` | 14 |
| this row | `PASS_WITH_CAVEATS` | **89** | — | 14 |

`cq_invoke.merge_verdict_into_plan_confidence` carries a `FAIL_SOFT` into every plan as
`NON_SHIPPABLE`/70 — below the `>= SHIPPABLE_WITH_CAVEATS` band `rules/cycle-plan.md`
requires for `/implement`. Composed across the registry: **0 of 14** plans reach the band
without this row, **11 of 14** with it.

So the cap is unpayable here and it blocks the chain. That is the condition the § 4
allowlist exists for.

## Who is affected — found by searching, not recalled

`grep -rln 'code-quality-languages\|code-quality-baseline\|code-quality-allowlist'
.claude/skills/ --include='*.py'` returns 8 files. The consumers that matter:

- **`run_structural.py`** — calls `cq_invoke.invoke()` for **every plan scored**. This is the
  blast radius: every plan in this repository, present and future, takes its cap from this row.
- **`run_validation.py`** — calls the same gate as a hard gate of `IMPLEMENTATION_COMPLETE`.
- **`mini_review.py`** — delta-audit coverage at phase boundaries.

**Outside this repository: nobody.** `.claude/` is gitignored here (`.gitignore:311`, 0 tracked
files), so the row reaches no consumer and no sibling repo. The only durable trace is the
`CHANGELOG.md` entry, which is why one was written.

## What would break, and what would not

**Would not break.** No Go source, chart, manifest, migration or exported symbol is touched.
Nothing here is compiled, imported or served. `go build`, the charts, the Taskfiles and the
clusters are untouched. The 14 baselined dead-code findings stay **listed in the report at full
count** — the exemption downgrades exactly one finding by exactly one severity level, and a
**new** dead symbol still fires `dead_code_unallowlisted_go` and caps the plan at `INVALID`.

**Would break, on 2026-12-12.** The row expires. `soft_cap_mutation_deferred_go` returns at full
severity, the gate goes back to `FAIL_SOFT`/70, and **every plan in this repository silently drops
back to `NON_SHIPPABLE`** — the exact state B-034 existed to end. The symptom a person meets is
identical to the half-fixed state, so the natural misdiagnosis will be "the baseline broke" rather
than "a date passed". Two mitigations: the expiry is written as prose beside the row and in the
CHANGELOG entry, and the kit now names expired entries on stderr (`run_code_quality.py`), a fix
landed since this item's plan was written.

## What was rejected, and why

1. **Per-plan `ADR-DISMISS-SOFT-CAP` markers.** Works, and reaches only **70** — permanently,
   for every plan ever written here — against this row's **89**. It also requires a marker in
   each of 14 plans, and the markers live in `.squad/`, which is untracked: 14 invisible
   assertions against 1 visible CHANGELOG line. Its one genuine advantage is that it survives
   expiry, so it remains available to any plan author who wants it.
2. **A hand-typed baseline row** `go|.|mutation_low|soft_cap_mutation_deferred_go`. Reaches
   `PASS` at cap **100** — a better number — while the report still prints all 14 HARD findings.
   **Rejected as the dishonest close.** § 4.1 forbids a baseline recording a finding about the
   GATE: it "silences the warnings that say the gate is not working — worse than a gate that
   fails, because it looks like one that passed". `--write-baseline` refuses to emit such a row;
   only the reader accepts it, which is the hole B-086 exists to close. Reaching for it is
   `autonomy-envelope.md` floor 3.
3. **Paying the cap down by adopting `gremlins` for Go.** The right long-term answer and not
   available here: it requires an ADR extending the § 5 table of a LOCKED golden rule, which is
   its own item (**B-088**) and crosses intake gate G3 if folded in.
4. **Raising a threshold or passing a bypass flag.** Forbidden — `autonomy-envelope.md` floor 3.
5. **Doing nothing.** The status quo the item exists to end: 11 plans capped, `/implement`
   unreachable.

## Why the SYMBOL column is `d4`

`is_allowlisted` matches the entry's SYMBOL against `finding.symbol_or_line`, which for this
finding is the literal `d4`. The gate **publishes** a different key
(`go|.|mutation_low|soft_cap_mutation_deferred_go`), and copying it used to produce a well-formed
row that matched nothing, silently. The kit has since added the published key as a *second*
haystack, so both forms now match — verified by measurement. `d4` is kept because it matches the
**primary** haystack and therefore works on every kit version, patched or not.

## Consequences

- 11 of 14 plans reach an implementable band with **no plan file edited**.
- The repository carries a dated obligation expiring 2026-12-12, owned by whoever renews it.
- The exemption is a recorded decision with a reason and a date; the debt stays countable.
