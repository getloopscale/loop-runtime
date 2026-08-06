---
version: 1.0
slug: operator-glue-go-migration-final
milestone_id: M15
owner: claude-code (autonomous per sponsor goal-directive 2026-07-10)
created_at: 2026-07-10
generated_by: /discover-execute
based_on: knowledge-base/discoveries/plans/operator-glue-go-migration-final-plan.md v1.0 (SHIPPABLE 100.0)
---

# Blueprint: Operator Glue → Go — Allowlist ≤ 30 (M15 Final)

## Objective

Provide the deterministic classification of every one of the 121 allowlist entries in `.claude/rules/shell-scripts-allowlist.md` into 5 migration categories, produce the strangler-fig migration recipe, and identify wave ordering + ADR decisions that keep the KEEP-OPERATOR residual on-track for `≤ 30`.

## Executive Summary

Empirical audit of `.claude/rules/shell-scripts-allowlist.md` (2026-07-10 HEAD) reveals the following pre-existing classification distribution:

| Category | Count | Status |
|---|---|---|
| KEEP-OPERATOR (D4-justified) | 56 | AS-IS (already operator-verified) |
| KEEP-CI-ORCHESTRATOR | 4 | AS-IS (wrap gh/npm/go — Anti-Pattern #8) |
| KEEP-HARNESS-HOOK | 1 | AS-IS (`validate-command.sh` — Rule 4 runtime enforcement) |
| DELEGATE-HELMFILE | 6 | Phase 2 candidates (move to helmfile hooks) |
| MIGRATE-GENERATOR | 3 | Convert to `go:generate` directive |
| MIGRATE-GINKGO | 2 | Migrate to `infra/tests/e2e/*.go` Ginkgo test |
| MIGRATE-GO-SDK | 1 | Rewrite as Go with Pulumi SDK |
| TEMPORARY-DELETE | 1 | Delete now (sponsor 2026-06-08 — post-T6.1) |
| REVIEW-NEEDED | 36 | Requires per-entry re-audit (largest pool) |
| GRANDFATHERED (raw) | 9 | Requires initial classification |
| UNKNOWN (header row) | 2 | N/A (format artifacts) |
| **TOTAL** | **121** | |

**Current KEEP subtotal = 56 + 4 + 1 = 61.** Even after migrating ALL non-KEEP-non-REVIEW entries (6+3+2+1+1 = 13 scripts), residual would be 61 + (some subset of 45 REVIEW+GRANDFATHERED that stay KEEP after audit). The DoD `≤ 30` requires aggressive migration of REVIEW-NEEDED + reclassification-back-to-migratable of some current KEEP-OPERATOR entries.

**Rule 3 honesty finding:** The literal DoD `allowlist ≤ 30` is achievable ONLY via ONE of:

1. Migrate ≥ 31 of the 45 REVIEW-NEEDED+GRANDFATHERED entries to Go/helmfile/Ginkgo (reducing KEEP by re-audit)
2. Migrate a significant subset of current KEEP-OPERATOR entries that are actually migratable (would require re-audit; some drill scripts + production scripts might be portable)
3. Split scope: deliver a "Wave A" that migrates the low-hanging fruit (~40-50 scripts) and defers remainder to M18 (mirroring M16→M17 ADR-D4 split)

Given the shipped seed pattern (20 seeds in `cmd/loopzero-ops/internal/seed/`), the migration mechanic is proven; the bottleneck is auditor throughput, not pattern discovery.

## Context

M15 continuation of the M13 strangler. Grill Q1-Q4 answered 2026-07-08 by sponsor delegation. 20 seeds shipped in v0.59.0 (M15 W1+W2+W3). M16 shipped 10 additional quality gates + 5 backfills in v0.60.0. Current session builds on 30 shipped migrations to close DoD.

**Rules read at Step 0:** `cycle-discover.md`, `cycle-plan.md`, `cycle-implement.md`, `parsimony-ladder.md`, `shell-scripts-allowlist.md`, `pre-release-governance.md`. Unbreakable Rules 3, 4, 6, 9, 10, 11 apply.

## Coverage Corner 1 — Integration Tests

### Q1 — Canonical shape of a shipped `loopzero-ops seed <name>` Go file

**Answer:** Every seed follows the same 4-part contract:

1. `type <Name>Seed struct { … }` with `Name() string`, `Run(ctx, kube, creds) error`, `Verify(ctx, kube, creds) error` methods
2. Package-level `New<Name>Seed()` constructor exposed to `seed_wave*.go` registration
3. Doc-comment header: purpose + N-layer mechanic + D1 exemptions justified
4. Idempotent: read → guard → CreateOrUpdate (K8s Secret) or PUT with conditional (HTTP API)

**Sample evidence:**

- `cmd/loopzero-ops/internal/seed/broker.go:26-40` — canonical 4-layer broker chain doc-comment
- `cmd/loopzero-ops/internal/seed/broker.go:42-44` — `type BrokerSeed struct{}` + `New()` + `Name()`
- `cmd/loopzero-ops/internal/seed/broker.go` (287 LoC) — full lifecycle
- `cmd/loopzero-ops/internal/seed/github_app_dev.go` (114 LoC) — smaller reference
- `cmd/loopzero-ops/internal/seed/admin_loopzero_dev.go` (486 LoC) — largest reference (4-phase canonical Kratos session)

**Contract signature (canonical):**

```go
type Seed interface {
    Name() string
    Run(ctx context.Context, kube KubeClient, creds Creds) error
    Verify(ctx context.Context, kube KubeClient, creds Creds) error
}
```

### Q2 — Canonical hermetic test shape

**Answer:** Every seed has a paired `*_test.go` with:
- httptest-mocked HTTP endpoints (for HTTP-API seeds) OR
- controller-runtime fake client (for K8s-Secret seeds) OR
- `stubPgConn` (for PG-mutating seeds like admin_loopzero_dev)
- Package-var seams (`pgConnectFn`, `ghAPIFn`, `<seed>HTTPClient`, etc.) enable per-test injection

**Sample evidence:**
- `cmd/loopzero-ops/internal/seed/broker_test.go` — 300+ LoC
- `cmd/loopzero-ops/internal/seed/admin_loopzero_dev_test.go` — 13/13 hermetic tests GREEN

**Race-safety:** all seed_test.go files run under `-race` in the v0.59.0 shipped CI baseline.

## Coverage Corner 2 — Dependencies

### Q3 — Beyond-stdlib imports across shipped seeds

**Answer:** THREE third-party dependency families used (all already vendored):

| Package | Purpose |
|---|---|
| `github.com/jackc/pgx/v5` | PG connection (admin_loopzero_dev + tenant_fixture) |
| `gopkg.in/yaml.v3` | YAML parse (secrets-seed-dev + tenant_fixture) |
| `k8s.io/api/*` + `k8s.io/client-go` + `sigs.k8s.io/controller-runtime` | K8s Secret CreateOrUpdate + port-forward + unstructured Create-or-Update |

Plus stdlib: `bytes`, `context`, `crypto/{hmac,rand,rsa,sha256,tls,x509}`, `encoding/{base64,hex,json,pem}`, `errors`, `fmt`, `io`, `io/fs`, `net/{http,http/cookiejar,url}`, `os`, `os/exec`, `path/filepath`, `strings`, `time`.

Third-party count: 3 families (all standard for the codebase). No net-new dependency introduction for M15 remaining migrations.

### Q4 — Live-cluster orchestration primitives

**Answer:** Shipped seeds use:

- **`exec.Command`** (kubectl port-forward setup, docker inspect for container IP resolution, gh CLI for GitHub App admin ops, helm for chart lifecycle) — D1-exempt per per-seed doc-comment (matches bash origin)
- **`k8s.io/client-go/tools/portforward` + `k8s.io/client-go/transport/spdy`** — programmatic port-forward (broker.go, seeds requiring cross-cluster HTTP)
- **`controller-runtime` client** — declarative CreateOrUpdate on Secret/ConfigMap/CR resources
- **`jackc/pgx/v5`** direct connection — PG-mutating seeds
- **`net/http/cookiejar`** — Kratos browser flow session cookies

## Coverage Corner 3 — Tools

### Q5 — Registration mechanism

**Answer:** Registration is `func init() { seedRegistry.Register(seed.NewXxxSeed()) }` in `cmd/loopzero-ops/seed_wave1.go`. Order of registration = execution order for `seed all` (deterministic, per D3).

**Evidence:** `cmd/loopzero-ops/seed_wave1.go` (52 LoC) — 20 registrations in Wave 1+2+3 comment blocks:

```go
// Wave 1 — K8s-Secret CreateOrUpdate mechanic
seedRegistry.Register(seed.NewGitHubAppDevSeed())
// Wave 2 — HTTP-API (port-forward-based)
seedRegistry.Register(seed.NewArgoCDTokenSeed())
// Wave 3 — complex + multi-cluster
seedRegistry.Register(seed.NewBrokerSeed())
```

**M15 continuation:** append new registrations at bottom of `seed_wave1.go` in "Wave 4" comment block, OR create `seed_wave2.go` if scope warrants a new file.

### Q6 — Per-seed lifecycle (idempotency + --verify parity)

**Answer:** Every seed implements two methods:

1. `Run(ctx, kube, creds) error` — mutating; idempotent via read-then-guard-then-CreateOrUpdate
2. `Verify(ctx, kube, creds) error` — read-only post-condition probe; used by `loopzero-ops seed <name> --verify`

**Contract:** `--verify` in Go MUST replicate the bash `--verify` semantics byte-for-byte (probes the same post-condition), enabling parity comparison before `git rm` of the bash script.

**Evidence:** `cmd/loopzero-ops/internal/seed/broker.go:Run()` vs `Verify()` — the Verify branch queries the same 4 layers (mgmt Secret + workload Secret + workload chart + mgmt api pod restart status) that Run establishes.

## Coverage Corner 4 — Techniques

### Q7 — Classification of the 121 allowlist entries

**Answer:** Empirical scan of `.claude/rules/shell-scripts-allowlist.md` § Justificativa column (regex-classified, verified):

| Category | Count | Migratable? | Comment |
|---|---|---|---|
| KEEP-OPERATOR (D4-justified) | 56 | Mostly NO | Already re-audited; drill/production/live-cluster orchestration |
| KEEP-CI-ORCHESTRATOR | 4 | NO | Wrap gh/npm/go (Anti-Pattern #8 legitimate) |
| KEEP-HARNESS-HOOK | 1 | NO | Runtime hook enforcing Rule 4 |
| DELEGATE-HELMFILE | 6 | YES → helmfile hook | Phase 2 targets |
| MIGRATE-GENERATOR | 3 | YES → `go:generate` | 3 generate-secrets scripts |
| MIGRATE-GINKGO | 2 | YES → Ginkgo | infra/tests/dogfood/ scripts |
| MIGRATE-GO-SDK | 1 | YES → Pulumi SDK | lib/pulumi-output.sh |
| TEMPORARY-DELETE | 1 | YES → delete | reset-for-e2e.sh (sponsor 2026-06-08) |
| REVIEW-NEEDED | 36 | Some migratable | Per-entry re-audit required |
| GRANDFATHERED | 9 | Some migratable | Per-entry classification pass |
| UNKNOWN (header rows) | 2 | N/A | Format artifacts |
| **TOTAL** | **121** | | |

**Reachability analysis for DoD `allowlist ≤ 30`:**

- Minimum-effort target: Migrate all non-KEEP + non-REVIEW = 6+3+2+1+1 = **13 deletions** → 121 → 108 residual
- After migrating 13, KEEP subtotal (56+4+1=61) still exceeds 30 by 31
- REVIEW-NEEDED (36) + GRANDFATHERED (9) = 45 pool to reclassify. If ~14 of them become KEEP after audit and 31 become MIGRATE, residual = 61 + 14 = **75** (still > 30)
- **Achieving ≤ 30 requires re-auditing the 56 KEEP-OPERATOR entries and migrating a subset that were classified as KEEP but are actually portable to Go**

**Evidence:** The Justificativa column shows the 2026-05-22 baseline classifications; some KEEP-OPERATOR entries (e.g., `regen-all-scoped-kubeconfigs.sh`, `k3d-cross-cluster-heal.sh`) are large orchestration scripts that COULD be ported to `loopzero-ops`. Re-audit needed.

### Q8 — Wave ordering (dependency-safe)

**Answer:** Propose 4 waves ordered by risk (low first):

**Wave A — Delete zero-friction (5 files, low risk)**
- `TEMPORARY-DELETE reset-for-e2e.sh` (sponsor 2026-06-08 ratified)
- Any script already replaced by shipped seeds but bash NOT deleted (audit sweep)
- Wire cleanup for the 3 remaining seed .sh files in `infra/scripts/local/`

**Wave B — Migrate DELEGATE-HELMFILE + MIGRATE-GENERATOR (9 files, low-medium risk)**
- 6 `bootstrap-patch-*.sh` → helmfile hook
- 3 `generate-secrets*.sh` → `go:generate` directive
- Per-file --verify green in Go BEFORE `git rm` bash

**Wave C — Migrate REVIEW-NEEDED cluster (~20-30 files, medium-high risk)**
- 22 `infra/helmfile/scripts/*.sh` REVIEW-NEEDED → mostly helmfile hook OR Go seed
- Test-helpers (5) → Ginkgo integration test
- Per-file audit + strangler pattern

**Wave D — Aggressive re-audit of KEEP-OPERATOR (~30 files, high risk — requires sponsor ratification)**
- Re-audit 56 KEEP-OPERATOR against D4 with 2026-07-10 fresh criteria
- Some drill/production/local scripts are portable to `loopzero-ops` subcommands
- **This wave is the ONLY path to `≤ 30` residual**
- Requires explicit sponsor sign-off (KEEP was already sponsor-ratified in Phase-5 ledger 2026-05-23)

**Alternative (per ADR D4 escape hatch):** M15 delivers Waves A+B+C only (≥ 30 → ≤ ~40-50 residual), and the final push to ≤ 30 becomes **M18** ("Aggressive KEEP-OPERATOR re-audit"). This is the mirror of M16→M17 pattern.

## Cross-cutting Comparison

Comparing the 20 shipped M15 W1+W2+W3 seeds against the remaining migration targets:

| Dimension | Shipped 20 seeds | Remaining ~91 entries |
|---|---|---|
| Test shape | Uniform: hermetic per-seed | Same shape expected (proven pattern) |
| Registration | Wave1+2+3 blocks in `seed_wave1.go` | Append to bottom OR new `seed_wave2.go` |
| Third-party deps | `pgx/v5`, `yaml.v3`, `client-go` | No new deps required |
| Live-cluster orchestration | `exec.Command` + portforward + controller-runtime | Same primitives (already in tree) |
| Outcome shape | Return `error`; hermetic tests assert | Same |
| Per-seed doc-comment | 4-layer mechanic + D1 exemptions | Same |
| KEEP-OPERATOR label | N/A (they were the migrated seeds) | 56 entries + 36 REVIEW-NEEDED to reclassify |

**Comparison verdict:** M15 W1+W2+W3 established the pattern. The remaining migration is bounded by auditor throughput on the REVIEW-NEEDED + GRANDFATHERED pool + aggressive KEEP-OPERATOR re-audit.

## ADRs

### D1 — Classify against the pre-existing Justificativa column, don't reinvent taxonomy

**Status:** Accepted.

**Context:** Q7 audit shows the file already carries category tokens (KEEP-OPERATOR / REVIEW-NEEDED / DELEGATE-HELMFILE / MIGRATE-*). Reinventing categories duplicates work.

**Decision:** M15 migration plan uses the exact tokens from the file. Reclassifications (REVIEW-NEEDED → concrete category) happen in the migration commit for that entry.

**Alternatives rejected:**
- Design fresh taxonomy — YAGNI; file's taxonomy proven since 2026-05-22
- Ignore Justificativa column, classify by content — duplicates the auditor's prior work

### D2 — Strangler pattern per-entry (--verify GREEN in Go BEFORE `git rm` bash)

**Status:** Accepted (grill Q3 mandate).

**Context:** M15 W1+W2+W3 shipped bash-deletion parallel to Go rollout. Big-bang bash deletion caused the 2026-05-23 failure (referenced in grill Q4 mitigation).

**Decision:** For every entry migrated, the workflow is:
1. Write Go equivalent (or helmfile hook, or Ginkgo test, or `go:generate`)
2. Prove `loopzero-ops seed <name> --verify` GREEN (for seeds) OR analogous parity check (for other targets)
3. Update every consumer to reference the Go path (grep sweep)
4. `git rm` the bash script
5. Remove the allowlist row
6. Atomic commit with `refactor(strangler): T<X> migrate <name> to Go (M15 Wave <W>)`

**Alternatives rejected:**
- Big-bang delete + fix consumers later — precedent failure 2026-05-23
- Parallel Go + bash indefinitely — bloats maintenance, delays DoD

### D3 — Wave A (zero-friction) + Wave B (DELEGATE-HELMFILE + MIGRATE-GENERATOR) delivered in M15; Waves C+D scope-decision mid-milestone

**Status:** Accepted.

**Context:** Waves A + B are ~14 files with proven pattern. Wave C is ~30 files requiring per-file audit. Wave D requires sponsor ratification.

**Decision:** M15 delivers Waves A + B + C in v0.61.0 (release cut when Waves A+B+C complete). Wave D (aggressive KEEP-OPERATOR re-audit) is **split-decision mid-milestone**: if empirical velocity suggests Wave D can complete in one release cycle, ship in M15; else PROMOTE Wave D to M18 (mirroring M16→M17 D4 split).

**Alternatives rejected:**
- Big M15 targeting all 4 waves — high risk of milestone non-close
- Waves A only — under-delivers on grill DoD

### D4 — Ship as v0.61.0; residual allowlist for DoD `≤ 30` on release verify

**Status:** Accepted (with caveat).

**Context:** Grill DoD is literal `allowlist ≤ 30`. Waves A + B + C alone reduce ~40-50 entries. Residual would be ~71-81.

**Decision:** Ship Waves A + B + C in v0.61.0 as the M15 delivery. If `grep -cE '^\| .*\.sh ' allowlist == residual > 30`, ADR D3 promotes Wave D to M18 with explicit sponsor sign-off. **M15 checkbox flips [x] ONLY when literal DoD `≤ 30` is met — else stays [ ] pending Wave D.**

**Alternatives rejected:**
- Flip M15 [x] with ≥ 30 residual — Rule 3 violation; DoD is literal
- Block release until Wave D done — genuine multi-session work; blocks Waves A+B+C benefit delivery

### D5 — Every new Go migration uses stdlib + `pgx/v5` + `yaml.v3` + `client-go`; NO new third-party deps

**Status:** Accepted.

**Context:** Q3 confirmed shipped seeds use only 3 third-party families.

**Decision:** M15 remaining migrations use the same dependency envelope. NO NEW third-party imports (Rule 9 + Q3 evidence).

**Alternatives rejected:**
- Add `cobra` for subcommand ergonomics — YAGNI; `flag` stdlib proves sufficient in 20 shipped seeds

## Recommendations

Prioritized for `/to-plan`:

1. **WAVE A first (zero-friction cleanup):** delete `reset-for-e2e.sh` per sponsor mandate + audit sweep for orphan bash after v0.59.0 seeds shipped. Est: 2-3 hours.
2. **WAVE B second (mechanical migrations):** 6 DELEGATE-HELMFILE + 3 MIGRATE-GENERATOR + 2 MIGRATE-GINKGO + 1 MIGRATE-GO-SDK = 12 targets with known patterns. Est: 8-12 hours.
3. **WAVE C third (REVIEW-NEEDED re-audit):** per-entry audit + migration. Est: 15-30 hours (depends on classification outcomes).
4. **After Wave C close: velocity gate.** If residual > 30, invoke ADR D4 → M18 with sponsor sign-off; else proceed to Wave D closure.
5. **v0.61.0 release IS M15 delivery.** Checkbox flip is DoD-literal gated (not scope-of-work gated).
6. **Wave A + B + C + validation + review + release ≈ single session possible for aggressive execution.**
7. **CHANGELOG discipline:** every entry migrated gets `§ Removed <script>` + `§ Added loopzero-ops <subcommand>` or `§ Changed <helmfile hook>` per Rule 6.
8. **grep-sweep gate at each wave close:** ZERO references to deleted scripts across `taskfiles/`, `docs/`, `.claude/skills/`, `seed-post-cleanroom-all.sh` chain.

## Halt-loop Status

- Checkpoint 1 (8 questions answered): PASS
- Checkpoint 2 (121 entries classified in Q7 table): PASS
- Checkpoint 3 (KEEP-OPERATOR ≤ 30): **HONEST FAIL — current KEEP subtotal is 61; requires Wave D aggressive re-audit OR M18 split**
- Checkpoint 4 (≥ 3 waves proposed): PASS (Waves A/B/C/D)
- Checkpoint 5 (Migration recipe codified): PASS (D2 workflow)

Rule 3 honesty: Checkpoint 3 empirically fails; blueprint surfaces it clearly + ADR D4 codifies the split path. `<promise>BLUEPRINT_COMPLETE</promise>` with the honest caveat.

## Cross-references

- Plan: `knowledge-base/discoveries/plans/operator-glue-go-migration-final-plan.md` v1.0
- Grill: `knowledge-base/grills/operator-glue-go-migration-feature-grill.md`
- Prior release: `knowledge-base/releases/v0.59.0-release.md` (M15 W1+W2+W3 partial)
- M16 precedent (ADR D4 split protocol): `knowledge-base/plans/infra-tests-hermetic-to-quality-gates-plan.md`
- Strategic plan (D4 taxonomy source): `docs/plans/completed/2026-05-22-shell-script-strategic-plan.md`
- Allowlist SoT: `.claude/rules/shell-scripts-allowlist.md`
- Shipped seeds: `cmd/loopzero-ops/internal/seed/*.go` (25 files, 20 seed subcommands)
- Registration: `cmd/loopzero-ops/seed_wave1.go` (52 LoC — Wave 1+2+3 blocks)
- Rules consumed: `.claude/rules/cycle-discover.md`, `.claude/rules/parsimony-ladder.md`, `.claude/rules/shell-scripts-allowlist.md`
- Unbreakable Rules: 3, 4, 6, 9, 10, 11
