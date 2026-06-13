# ADR D3 — `pkg/domain.DefaultPermanentEnv` canonical constant

**Status:** Accepted
**Date:** 2026-06-11
**Plan:** v1.2 § ADR D3 + Task T1.2
**Tier:** T2 (shared library `pkg/`) per `rules/pre-release-governance.md`

## Context

Completeness-auditor finding H2 surfaced: `"production"` string literal duplicated across **6 sites** without canonical constant:
- `api/internal/routes/environments.go:32` (reserved)
- `:490` (delete protection)
- `:558` (stub list)
- `:579` (stub detail)
- `api/internal/routes/projects.go:269` (auto-create)
- `cloud/dashboard/src/pages/secrets.tsx:25,52` (frontend env list)

Finding H6 (J-22 silently breaks J-10) is downstream of the same root: secrets.tsx hardcodes `['all', 'production', 'staging']` instead of consuming the canonical constant + dynamic env list.

Changing the canonical permanent-env name (e.g., `"production"` → `"prod"` for shorter URLs) would require editing 6 places; any miss is a silent J-10/J-22 regression.

## Decision

NEW canonical constants (Go + TypeScript mirror, parity-tested):

**Go:** `pkg/domain/constants.go`
```go
package domain

// DefaultPermanentEnv names the always-present permanent environment auto-created
// per tenant. Single source of truth across api/, cloud/, operators/.
const DefaultPermanentEnv = "production"
```

**TypeScript:** `cloud/dashboard/src/lib/domain.ts` (frontend mirror)
```typescript
// Mirror of pkg/domain.DefaultPermanentEnv (Go).
// Parity enforced by infra/tests/check-default-permanent-env-parity_test.sh
export const DEFAULT_PERMANENT_ENV = 'production';
```

**EC-3 absorbed — Go↔TS parity gate:** Hermetic test `infra/tests/check-default-permanent-env-parity_test.sh` (NEW, allowlisted in `rules/shell-scripts-allowlist.md`) greps both constants, asserts equal value. Wired in `task quality:gates`. Drift between backend Go constant and frontend TS constant fails CI before merge.

T1.2 migration: replace all 6 hardcoded sites. Backend imports `domain.DefaultPermanentEnv`. Frontend uses `DEFAULT_PERMANENT_ENV` only as loading-state fallback (T1.4 wires dynamic `useEnvironments(name)` hook for the main flow).

## Alternatives

### (a) `pkg/naming.DefaultEnv` — REJECTED

Naming package's concern is computing K8s namespaces from inputs, not declaring canonical names. Wrong package responsibility — violates SRP per `rules/design-principles.md`. The constant is a DOMAIN concept ("what is the canonical first environment for every tenant?"), not a NAMING concept ("how do I encode an env as a namespace string?").

### (b) DB-driven (query `default_env` from a `tenant_settings` table) — REJECTED

Requires DB migration + adds query to hot paths (every project auto-create reads the table). YAGNI per `rules/design-principles.md` — current product offers ONE canonical permanent env per tenant; future multi-tenant variation can layer on top of the constant via override (constant becomes fallback).

### (c) Environment variable (`LOOPZERO_DEFAULT_PERMANENT_ENV`) — REJECTED

Adds 3-layer drift surface (config.Go + values.yaml + helmfile per `feedback_drift_3layer_canonical_pattern`) for a value that is part of the PRODUCT contract, not deployment-specific. Customers who self-host don't pick their "canonical permanent env name" — it's "production" because that's the product's vocabulary.

## Rationale

Cited principles:
- **DRY** per `rules/design-principles.md` § DRY — canonical knowledge has single source
- **YAGNI** — defer multi-tenant variation until first concrete use case
- **Twelve-Factor App § X (Dev/prod parity)** — env name is part of the product contract, not deploy-time config

Memory: `feedback_no_hardcode_gate_2026_06_09` (closes 6 hardcode sites); `feedback_drift_3layer_canonical_pattern` (avoids adding new drift via env-var alternative).

## Consequences

### Positive
- 6 hardcode-class findings closed (H2 + H5 backend; H6/EC-5 frontend uses as fallback)
- Future canonical rename = single-file edit propagated by compile errors (Go) + grep (TS, validated by parity gate)
- Establishes precedent: every canonical product vocabulary term gets a `pkg/domain` constant + frontend mirror

### Negative
- Frontend mirror requires manual sync IF an operator forgets the parity test; mitigated by CI gate (`task quality:gates` blocks merge on drift)

### Neutral
- Adds `pkg/domain` import to 5 backend files (was 0); import cycle audit passes (`pkg/domain` is leaf node — depends on no other pkg/)

## References

- Plan v1.2 § ADR D3 + T1.2 + edge case EC-3
- Completeness-auditor findings H2, H5, H6 (`code-review-output/findings/phase2-completeness.md`)
- ADR D2 (`EnvRef` Value Object) — sibling, uses `DefaultPermanentEnv` as default `EnvName` for new tenants
