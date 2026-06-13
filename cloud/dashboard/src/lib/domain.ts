/**
 * Canonical product-level constants — frontend mirror of pkg/domain (Go).
 *
 * Per ADR D3 (decisions/2026-06-11-pkg-domain-default-permanent-env.md):
 * single source of truth for domain vocabulary that crosses the backend/
 * frontend boundary. Parity enforced by
 * infra/tests/check-default-permanent-env-parity_test.sh (CI gate in
 * task quality:gates).
 */

/**
 * DEFAULT_PERMANENT_ENV names the always-present permanent environment
 * auto-created for every tenant. Frontend uses this ONLY as a
 * loading-state fallback when the dynamic useEnvironments(name) hook
 * (J-10 pattern) has not yet resolved — never as the primary source
 * of env list. Closes H6 (J-22 silently breaks J-10).
 *
 * Backend source: pkg/domain.DefaultPermanentEnv (Go).
 */
export const DEFAULT_PERMANENT_ENV = 'production';
