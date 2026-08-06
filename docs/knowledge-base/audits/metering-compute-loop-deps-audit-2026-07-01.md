# Deps Audit: metering-compute-loop

**Date:** 2026-07-01
**Mode:** plan-bound:metering-compute-loop (plan v1.3)
**Verdict:** **PASS_WITH_CAVEATS** (declared plan deps clean; caveats are repo-wide, pre-existing, NOT introduced by this plan)
**Hard caps triggered:** none (after adding the `## Dependencies` section — see § note)

## Summary
- Ecosystems detected (feature scope): **Go** (`api/go.mod`, `pkg/go.mod`). The repo also has npm + Python surfaces, but this feature is Go-only — audited the Go surface it touches.
- Total declared plan deps: 5 existing (reuse) + 1 NEW (`k8s.io/component-helpers/resource`).
- Vulnerabilities on **declared plan deps**: **0** (pgx, prometheus/client_golang, client-go, k8s.io/api, component-helpers — osv-scanner found none).
- Repo-wide finding (NOT a plan dep): **Go stdlib advisories at go1.26.1** — GO-2026-4864/4865/4866/4869/4870/4918/4946/4947/4971/4976/4977/4980/4981/4982/4986/5037/5038/5039 (18 IDs across api+pkg go.mod). These are the Go standard library on the newest toolchain (1.26.1), awaiting a 1.26.2 patch — pre-existing, repo-wide, not this plan's responsibility.
- Outdated: none relevant to the feature (deps are current).
- Allowlist hits: 0.
- **Auditor coverage:** `osv-scanner`: **ran** (via `go.mod` extractor). `govulncheck`: **UNAVAILABLE** — the local binary at `~/go/bin/govulncheck` is built with go1.25; the repo requires go1.26.1, so it emitted "package requires newer Go version" for every package and could NOT run its call-graph reachability analysis. `auditor_unavailable_govulncheck`.

## Vulnerabilities (declared plan deps)

**None.** osv-scanner reported no CVE on `jackc/pgx/v5@v5.9.0`, `prometheus/client_golang@v1.23.2`, `k8s.io/client-go@v0.35.2`, `k8s.io/api@v0.35.2`, or the NEW `k8s.io/component-helpers/resource@v0.35.2` (same k8s.io family; no advisory).

## Repo-wide caveat (NOT introduced by this plan)

18 Go **stdlib** advisories flagged by osv-scanner against `go 1.26.1` (declared in `api/go.mod` + `pkg/go.mod`). Honest handling:
- **Scope:** the Go toolchain, applied to the ENTIRE monorepo — not a dependency this plan declares, adds, or upgrades.
- **Severity/reachability:** osv-scanner did not print severity scores for these, and **`govulncheck` (the reachability-aware authority) could not run** — so it is UNCONFIRMED which (if any) of the vulnerable stdlib symbols are actually reachable from called code. Per anti-pattern #5, I do NOT claim these are harmless; I report the gap.
- **Fix path:** bump the Go toolchain to `1.26.2`+ when released (a repo-wide `go.mod`/`go.work` change), tracked separately from this plan.

## Plan validation (Mode 2)

| Plan dep | Section | Manifest match | Audit clean? | Rule 9 OK? | Verdict |
|---|---|---|---|---|---|
| `jackc/pgx/v5` | Existing | yes (`v5.9.0`) | yes | n/a | OK |
| `prometheus/client_golang` | Existing | yes (`v1.23.2`) | yes | n/a | OK |
| `k8s.io/client-go` | Existing | yes (`v0.35.2`) | yes | n/a | OK |
| `k8s.io/api` | Existing | yes (`v0.35.2`) | yes | n/a | OK |
| `k8s.io/component-helpers/resource` | NEW | to add (`v0.35.2`) | yes (no CVE) | **yes** — rejected (a) hand-roll pod-request math (Rule 9 anti-pattern; subtle sidecar/resize rules), (b) copy the formula in-repo | OK |

## Recommended next steps

1. **No blocking dep action** for this plan — declared deps are clean and the NEW dep is a zero-CVE, same-family k8s.io helper with a valid Rule 9 rationale.
2. **Reinstall `govulncheck` with go1.26** (`go install golang.org/x/vuln/cmd/govulncheck@latest` under the 1.26 toolchain) and re-run `govulncheck ./...` to confirm the stdlib advisories' reachability — a **repo-wide** action, not gated on this plan.
3. **Track a repo-wide Go 1.26.2 bump** when released (separate hotfix), to clear the 18 stdlib advisories.
4. Proceed to `/plan-confidence metering-compute-loop`.

## Note on verdict derivation

- The initial Mode-2 read hit `plan_dependencies_section_missing` (the plan lacked a `## Dependencies` section). That section was authored (deps verified against `api/go.mod`), clearing the hard cap.
- The remaining caveats (stdlib toolchain advisories + `govulncheck` unavailable) are **repo-wide and pre-existing**, not introduced by this plan, so they do not cap the plan's dep verdict to INVALID — but they are surfaced honestly and warrant the repo-wide follow-ups above. Verdict: **PASS_WITH_CAVEATS** (cap 89 for plan-confidence, on the `auditor_unavailable_govulncheck` basis — the honest ceiling while the reachability layer can't run).
