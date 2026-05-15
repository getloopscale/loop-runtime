// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

/**
 * Plan T1.2 — CLI Exit Code contract per CLAUDE.md Rule 4.
 *
 * Exit codes are permanent — never change. Only 6 values are allowed.
 */

export const EXIT_CODES = {
  /** Success */
  SUCCESS: 0,
  /** Usage error (bad arguments, missing flags) */
  USAGE: 1,
  /** Build failure (Argo Workflow / BuildKit / Cosign sign / SBOM / etc.) */
  BUILD_FAILED: 2,
  /** Deploy failure (ArgoCD sync, manifest delivery, health probe, etc.) */
  DEPLOY_FAILED: 3,
  /** Auth error (token expired, missing, invalid) */
  AUTH_ERROR: 4,
  /** Partial failure (multi-app deploy: some apps OK, some failed) */
  PARTIAL: 5,
} as const;

export type ExitCode = typeof EXIT_CODES[keyof typeof EXIT_CODES];

/**
 * Map a terminal release state to its Rule 4 exit code.
 *
 * Plan T1.2 ADR D3 — single source of truth for state → exit mapping.
 */
export function releaseStateToExitCode(releaseState: string | undefined): ExitCode {
  switch (releaseState) {
    case 'ACTIVE':
    case 'SUPERSEDED':
      return EXIT_CODES.SUCCESS;
    case 'SYNC_FAILED':
    case 'GIT_MIRROR_FAILED':
    case 'ROLLBACK_FAILED':
    case 'CANCELLED':
      return EXIT_CODES.DEPLOY_FAILED;
    default:
      // Unknown state — treat as deploy failure (safe default)
      return EXIT_CODES.DEPLOY_FAILED;
  }
}

/**
 * Map a terminal build state to its Rule 4 exit code.
 */
export function buildStateToExitCode(buildState: string | undefined): ExitCode {
  switch (buildState) {
    case 'SUCCEEDED':
      return EXIT_CODES.SUCCESS;
    case 'FAILED':
    case 'CANCELLED':
      return EXIT_CODES.BUILD_FAILED;
    default:
      // Non-terminal or unknown — caller decides; default to build failure
      return EXIT_CODES.BUILD_FAILED;
  }
}

/**
 * Map a terminal state combination (build + release) to the appropriate exit
 * code. Plan T1.2 — releaseState wins when set (deploy failure semantically
 * happens AFTER build success).
 */
export function mapTerminalStateToExitCode(
  buildState: string | undefined,
  releaseState: string | undefined,
): ExitCode {
  // If release reached a terminal state, that wins (deploy-level concern)
  if (releaseState) {
    return releaseStateToExitCode(releaseState);
  }
  // Otherwise fall through to build state
  return buildStateToExitCode(buildState);
}

/**
 * Guard: verify a given number is a valid Rule 4 exit code.
 * Used in tests to ensure CLI never exits with 6+, negative, etc.
 */
export function isValidExitCode(n: number): n is ExitCode {
  return n === 0 || n === 1 || n === 2 || n === 3 || n === 4 || n === 5;
}
