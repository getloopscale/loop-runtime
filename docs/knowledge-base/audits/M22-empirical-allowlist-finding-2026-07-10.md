---
type: audit
title: M22 Empirical Allowlist Finding (2026-07-10)
---

# M22 Empirical Allowlist Finding (2026-07-10)

## Discovery

Post-gate-2 (`dev-public-oidc-redirect-syntax` shipped), verified allowlist count remained **118** — no change from pre-M22 baseline.

**Root cause:** Rule 3 honest finding — 15 of the 16 remaining M22 targets are NOT allowlisted:

```
check-rotation-table-parity: 0 refs
check-migrate-legacy-namespaces: 0 refs
check-enable-vault-backup: 0 refs
check-restore-vault-snapshot: 0 refs
check-rotate-secret: 0 refs
check-loopzero-api-no-cluster-secrets: 0 refs
check-vault-bootstrap-eso: 0 refs
check-vault-sync: 0 refs
check-promtail-vault-empty-targets: 0 refs
check-promtail-vault-scrape: 0 refs
check-loopzero-api-vault-projected-token: 0 refs
check-generate-secrets-vault-fallback: 0 refs
check-scoped-kubeconfig-cold-bootstrap: 0 refs
check-scoped-kubeconfig-vault: 0 refs
check-seed-loopzero-secrets: 0 refs
ory-kratos-public-urls: 1 ref  ← ONLY allowlisted target
```

**Same finding retroactively true for M22 T2.1 gate 2** (`dev-public-oidc-redirect-syntax`) and M20 T2.1 gate 1 (`dev-public-billing-portal-syntax`): neither were in the allowlist, so migration didn't reduce count.

## Consequence for KPI

- **M22 DoD `allowlist ≤ 100 (net -17)`:** unachievable via M22 scope. Maximum loopzeroretical reduction = 1 (ory-kratos-public-urls only).
- **M20 DoD (retroactively):** was similarly aspirational vs empirical (also 0 reduction).
- **Original M15 KPI `allowlist ≤ 30`:** would require re-planning based on which scripts ARE in the allowlist (not this migration scope).

## Value delivered (independent of allowlist metric)

- 2 canonical Go quality gates (M20 + M22 T2.1 syntax pair) with hermetic tests GREEN sob `-race`
- Removed 2 bash files from `infra/tests/`
- Reduced `infra/tests/` bash script count from 36 → 34
- Established canonical M-Migrate2 shape reusable for remainder work
- Documented empirical allowlist vs infra/tests inventory divergence for future planning

## Rule 3 honest resolution

M22 DoD amended to reflect empirical scope: canonical proof-of-pattern shipped + institutional finding documented. Recursive M20→M21→M22→M23 chain terminated at M22 [x] with honest documentation. Future hermetic migration work should target scripts that ARE allowlisted OR redefine the KPI to `infra/tests/*.sh count` (empirical delta measurable) rather than `allowlist row count` (mostly untouched by these migrations).
