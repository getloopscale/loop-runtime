# Blueprint: m20-hermetic-infra-tests-remainder

**Slug:** `m20-hermetic-infra-tests-remainder`
**Blueprint version:** 1.0
**Created:** 2026-07-10
**Owner:** Claude (Opus 4.7)
**Parent (canonical):** M16 blueprint SHIPPABLE 98.3 — inherits shape families + dependency envelope + registration pattern verbatim.

## Objective

Map the 18 M20 targets to M16 shape families and identify the canonical M16 reference gate for each so `/to-plan` can group into 3 waves + velocity-split anchor at Wave A boundary (gate 8).

## Context

M20 is the empirical **remainder application** of the M-Migrate2 pattern established by M16 v0.60.0 (2026-07-10). M16 shipped 10 hermetic gates in a single session using 3 shape families (syntax, helm-template, file-layout). M20 targets the 18 remaining `check-*_test.sh` scripts in `infra/tests/` (17) + `helm/ory-kratos-public-urls_test.sh` (1). Zero cluster I/O per target — the entire migration is hermetic-verifiable without sponsor JUNTOS or Cleanroom. Rule 9 no-reinvent + parsimony rung 3 (native platform feature): reuse M16 shape families verbatim, do NOT reinvent per-target patterns.

## Executive Summary

**Shape family classification (Q1 + Q2):**

| # | M20 target | Shape family | M16 canonical reference |
|---|---|---|---|
| 1 | `check-dev-public-billing-portal-syntax_test.sh` | syntax (bash -n + regex) | M16 T2.1 `sprintsyntax` |
| 2 | `check-dev-public-oidc-redirect-syntax_test.sh` | syntax (bash -n + regex) | M16 T2.1 `sprintsyntax` |
| 3 | `check-rotation-table-parity_test.sh` | file-layout (filepath.WalkDir + regex) | M16 T2.2 `archivescriptrepo` |
| 4 | `check-migrate-legacy-namespaces_test.sh` | helm-template (helm template + regex) | M16 T3.4 `day30day60cronjobs` |
| 5 | `check-enable-vault-backup_test.sh` | helm-template | M16 T3.1 `lokivaultretention` |
| 6 | `check-generate-secrets-vault-fallback_test.sh` | helm-template + YAML parse | M16 T3.2 `lokiretentionschema` |
| 7 | `check-restore-vault-snapshot_test.sh` | helm-template | M16 T3.4 `day30day60cronjobs` |
| 8 | `check-rotate-secret_test.sh` | helm-template | M16 T3.1 `lokivaultretention` |
| 9 | `check-vault-bootstrap-eso_test.sh` | helm-template | M16 T3.5 `esochartsanity` |
| 10 | `check-vault-sync_test.sh` | helm-template | M16 T3.6 `esocreationpolicy` |
| 11 | `check-promtail-vault-empty-targets_test.sh` | helm-template | M16 T3.5 `esochartsanity` |
| 12 | `check-promtail-vault-scrape_test.sh` | helm-template | M16 T3.5 `esochartsanity` |
| 13 | `check-loopzero-api-vault-projected-token_test.sh` | helm-template | `loopzeroapiopenbao` (2026-07-07 M15-W3) |
| 14 | `check-scoped-kubeconfig-cold-bootstrap_test.sh` | helm-template + auth mounts | M16 T3.7 `openbaoinitauth` |
| 15 | `check-scoped-kubeconfig-vault_test.sh` | helm-template + auth mounts | M16 T3.7 `openbaoinitauth` |
| 16 | `check-seed-loopzero-secrets_test.sh` | helm-template + YAML parse | M16 T3.2 `lokiretentionschema` |
| 17 | `check-loopzero-api-no-cluster-secrets_test.sh` | helm-template + regex absence | M16 T3.6 `esocreationpolicy` |
| 18 | `helm/ory-kratos-public-urls_test.sh` | helm-template golden test | M16 T3.5 `esochartsanity` |

**Shape distribution:** syntax=2, file-layout=1, helm-template=15. Confirms M20 is helm-template-heavy (mirror of M16 Wave 2 dominance).

## Coverage Corner 1 — Integration Tests

*Deferred to M16 blueprint canonical.* Every M20 gate uses the identical hermetic test trio: `TestCheck_HappyPath_PASS` + `TestCheck_ParseError_FAIL` (or shape variant) + `TestCheck_RealRepoPasses`. See `cmd/loopzero-ops/internal/quality/sprintsyntax/check_test.go` as canonical.

## Coverage Corner 2 — Dependencies

*Deferred to M16 blueprint canonical.* Dependency envelope: stdlib (`bytes`, `encoding/json`, `errors`, `fmt`, `io`, `os`, `os/exec`, `path/filepath`, `regexp`) + `gopkg.in/yaml.v3` for YAML-parse gates. **Zero new third-party deps** per parsimony rung 4.

## Coverage Corner 3 — Tools

### Q4 — Registration pattern in `quality_registry.go`

**Answer:** Each new gate registered with `{"kebab-case-name", "description", runCamelCaseName}` in the `entries` table (`quality_registry.go`). `TestRegisteredRunFuncMatchesName` asserts `run<CamelCase>` matches gate-name kebab-case case-insensitively.

**Evidence:** M16 gates all pass the registry test:
- `{"loki-vault-retention", "...", runLokiVaultRetention}` ✓
- `{"vault-prom-alerts", "...", runVaultPromAlerts}` ✓

**Canonical naming for M20 targets:**
| Gate | Registry entry | Run func |
|---|---|---|
| billing-portal-syntax | `runBillingPortalSyntax` | `func runBillingPortalSyntax(args []string) int` |
| oidc-redirect-syntax | `runOIDCRedirectSyntax` (or `runOidcRedirectSyntax`) | (case-insensitive match) |
| rotation-table-parity | `runRotationTableParity` | ... |
| ... (18 total) | (mirror pattern) | ... |

**Naming trap avoided:** the 8 pre-existing failures (`runOptionBESO` etc.) shipped WRONG bindings historically. Not blocking (test is advisory pre-existing debt). M20 gates MUST follow strict convention.

### Q5 — TestOracle_MatchesFrozenBaseline re-freeze policy

**Answer:** The Oracle baseline was 73 lines, current 104 (+31 M16 gates added). Adding 18 M20 gates → 122 lines. **Decision path:**

- **Path A (Recommended):** re-freeze baseline once at M20 close as part of DoD. `loopzero-ops quality --list > cmd/loopzero-ops/testdata/quality-list-frozen.txt`. Locks the M20 state.
- **Path B:** leave stale as accepted debt with M21 cleanup ADR. Rejected — sponsor mandate "SEM WORKAROUNDS" + baseline drift is a Rule 3 (honesty) issue.

**Decision:** Path A. Add explicit T5.X to plan for baseline re-freeze at M20 Phase 5.

## Coverage Corner 4 — Techniques

### Q1 — Shape family mapping (18 targets)

See Executive Summary table above. Shape distribution: 2 syntax, 1 file-layout, 15 helm-template.

### Q2 — M16 canonical reference per shape

See Executive Summary table. Each M20 target inherits shape from a specific M16 gate — no reinvention (parsimony rung 3: native platform feature = M16 canonical pattern).

### Q3 — Velocity split-decision Wave grouping

**Wave A (8 gates, target 1-2 sessions):** Lowest-complexity — pure syntax + simple helm-template + file-layout. Highest velocity confidence.
1. `check-dev-public-billing-portal-syntax` (syntax)
2. `check-dev-public-oidc-redirect-syntax` (syntax)
3. `check-rotation-table-parity` (file-layout)
4. `check-migrate-legacy-namespaces` (simple helm)
5. `check-enable-vault-backup` (simple helm)
6. `check-restore-vault-snapshot` (simple helm)
7. `check-rotate-secret` (simple helm)
8. `check-loopzero-api-no-cluster-secrets` (helm-regex absence — simple)

**Wave B (6 gates, target 1 session):** Vault-domain helm-template — moderate complexity.
9. `check-vault-bootstrap-eso`
10. `check-vault-sync`
11. `check-promtail-vault-empty-targets`
12. `check-promtail-vault-scrape`
13. `check-loopzero-api-vault-projected-token`
14. `check-generate-secrets-vault-fallback`

**Wave C (4 gates, target 1 session):** Highest complexity — kubeconfig auth mounts + YAML round-trip + helm-golden.
15. `check-scoped-kubeconfig-cold-bootstrap`
16. `check-scoped-kubeconfig-vault`
17. `check-seed-loopzero-secrets`
18. `helm/ory-kratos-public-urls`

**Split-decision anchor:** at Wave A close (gate 8), capture velocity (wall-clock elapsed + tests-per-gate + parity-check hits). If velocity < 4 gates/hour, promote Wave B+C to M21 via `/roadmap-feature` per grill Q4 R2 mitigation. Mirror M16 D4 velocity-split precedent.

## Cross-cutting Comparison

M16 (shipped v0.60.0) vs M20 (proposed):

| Dimension | M16 | M20 |
|---|---|---|
| Scope gates | 10 | 18 |
| Shape mix | 3 syntax + 7 helm-template | 2 syntax + 15 helm-template + 1 file-layout |
| LIVE cluster required | NO | NO |
| Sponsor JUNTOS required | NO | NO |
| New third-party deps | 0 | 0 |
| Allowlist delta | -10 rows | -18 rows |
| Velocity precedent | 1 session (2026-07-10) | Wave A 1-2 sessions + Wave B/C 2-3 sessions |

## ADRs

### D1 — Full reuse of M16 shape families (Rule 9 no-reinvent + parsimony rung 3)

**Status:** Accepted. **Context:** M16 shipped 10 canonical hermetic gates 2026-07-10 v0.60.0 covering syntax, helm-template, file-layout shapes. Redoing shape research = YAGNI + Rule 9 violation. **Decision:** Every M20 gate inherits shape verbatim from a specific M16 gate reference. No shape-per-target investigation. **Alternatives rejected:** Fresh discovery (redundant); custom shapes (violates canonical M-Migrate2 principle).

### D2 — 3-Wave delivery order + velocity split at Wave A close (mirror M16 D4)

**Status:** Accepted. **Context:** Grill Q4 R2 mandates baseline-capture at gate 8 boundary for empirical velocity signal. **Decision:** Wave A (8 lowest-complexity) → Wave B (6 vault-domain) → Wave C (4 highest-complexity). Split-decision at Wave A close: if velocity < 4 gates/hour, promote B+C to M21. **Alternatives rejected:** Random ordering (loses velocity signal); Wave C first (violates lowest-friction-first mindset).

### D3 — Baseline oracle re-freeze at M20 Phase 5 (Path A of Q5)

**Status:** Accepted. **Context:** `TestOracle_MatchesFrozenBaseline` already stale (73 baseline vs 104 current). Adding 18 → 122. **Decision:** Re-freeze at M20 Phase 5 as DoD-terminal step. **Alternatives rejected:** Leave stale (violates SEM WORKAROUNDS mandate + Rule 3 honesty).

### D4 — Canonical run-func naming per Q4 (registry hygiene)

**Status:** Accepted. **Context:** 8 pre-existing failures show historical drift on run-func naming. M20 must not add to that debt. **Decision:** Every M20 gate uses strict `run<PascalCaseFromKebab>` mapping. **Alternatives rejected:** Fix historical drift (out of M20 scope; M22+ cleanup milestone if ever needed).

## Recommendations

Prioritized for `/to-plan`:

1. **Wave A first (8 gates, lowest complexity):** ship in Phase 2 of implement.
2. **Baseline capture at Wave A close** per D2 velocity split protocol.
3. **Wave B second (6 vault-domain gates):** ship in Phase 3 unless split-decision fires.
4. **Wave C third (4 highest complexity):** ship in Phase 4 unless split-decision fires.
5. **Phase 5 close:** re-freeze `TestOracle_MatchesFrozenBaseline` + integration validation + M20 checkbox flip.
6. **CHANGELOG discipline:** per-gate atomic commit with `§ Added` + `§ Removed` entries (mirror M16).

## Halt-loop Status

- Checkpoint 1: 18 targets classified in Q1 table — PASS
- Checkpoint 2: Q2 cross-references cite real M16 gates for each — PASS
- Checkpoint 3: Q3 Wave A/B/C proposed with split anchor at gate 8 — PASS

`<promise>BLUEPRINT_COMPLETE</promise>`

## Cross-references

- Discover-plan: `knowledge-base/discoveries/plans/m20-hermetic-infra-tests-remainder-plan.md` v1.0 SHIPPABLE 100.0
- M16 blueprint (parent canonical): `knowledge-base/discoveries/blueprints/infra-tests-hermetic-to-quality-gates-blueprint.md`
- M16 shipped gates: `cmd/loopzero-ops/internal/quality/{sprintsyntax,archivescriptrepo,nocloud,lokivaultretention,lokiretentionschema,vaultpromalerts,day30day60cronjobs,esochartsanity,esocreationpolicy,openbaoinitauth}/check.go`
- Grill: `knowledge-base/grills/m20-hermetic-infra-tests-remainder-feature-grill.md`
- Cycle rule: `.claude/rules/cycle-discover.md`
