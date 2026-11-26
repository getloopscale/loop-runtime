// Package domain holds canonical product-level constants and Value Objects
// for the LoopRuntime platform. This package is the single source of truth for
// domain vocabulary — names that are part of the customer-facing contract,
// not deployment-specific configuration.
//
// Per ADR D3 (decisions/2026-06-11-pkg-domain-default-permanent-env.md),
// closes H2 + H5 (6 hardcoded "production" literal sites) and unblocks
// T1.4 (J-22 secrets.tsx useEnvironments hook).
package domain

// DefaultPermanentEnv names the always-present permanent environment
// auto-created for every tenant. Single source of truth across the
// monorepo (api/, cloud/, operators/).
//
// Frontend mirror: cloud/dashboard/src/lib/domain.ts DEFAULT_PERMANENT_ENV.
// Parity enforced by infra/tests/check-default-permanent-env-parity_test.sh
// (CI gate in task quality:gates).
const DefaultPermanentEnv = "production"

// DefaultPreviewEnv names the second permanent canonical environment
// per the PaaS model lock (sponsor verbatim 2026-06-11 — see memory
// project_paas_envs_preview_production_only).
//
// Per ADR-Q5 of qa-dogfood-cleanup-2026-06-21-plan.md, every project
// has EXACTLY two permanent environments — `production` and `preview` —
// seeded at project.created. The `preview` environment is the
// production-target for PR/branch builds; the per-PR ephemeral slots
// (e.g., `preview-pr-42`) are derived from this canonical permanent
// preview environment.
//
// Surfaced by 2026-06-21 QA dogfooding (#124): the `/environments`
// view listed only `production` for projects without a preview row,
// while the canonical model + QA doc J-10 + add-secret dialog (#127)
// all assume both are PaaS-permanent.
const DefaultPreviewEnv = "preview"

// PermanentEnvs is the canonical ordered list of permanent environments
// auto-created per project. Iteration order matches the lifecycle:
// production is created FIRST (lowest-risk + legacy compat) followed by
// preview. Order is stable so the dashboard /environments view renders
// deterministically.
//
// Cross-reference: ADR-Q5 of qa-dogfood-cleanup-2026-06-21.
var PermanentEnvs = []string{DefaultPermanentEnv, DefaultPreviewEnv}
