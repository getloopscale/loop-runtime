// Package domain holds canonical product-level constants and Value Objects
// for the LoopZero platform. This package is the single source of truth for
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
