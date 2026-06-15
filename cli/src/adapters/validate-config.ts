// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// LoopZero.yaml strict validator.
//
// Plan: 2026-06-15-code-review-49-findings-remediation T2.1 (R3-F2 BLOCKER).
// Before T2.1 this file held a monolithic `validateConfig` at CC=95 (worst
// in the entire codebase, 6.3× the FAANG threshold of 15). Per ADR D2 the
// function is decomposed via the Strategy pattern into 9 specialized
// validators under ./validators/, with this file reduced to a thin
// orchestrator that delegates + concatenates ValidatorResult.
//
// Behavior is preserved verbatim — every existing test in
// `cli/tests/unit/adapters/validate-config.test.ts` (69 cases) MUST pass
// without modification.

import type { LoopZeroYaml } from './loopzero-yaml';
import {
  validateAppBuild,
  validateAppEnv,
  validateAppName,
  validateAppPath,
  validateAppRuntime,
  validateAppScaling,
  validateAppUnknownFields,
  validateProjectConstraints,
  validateVersion,
  type ValidationError,
  type ValidatorResult,
} from './validators';

// Re-export the canonical type so consumers (commands/deploy.ts +
// adapters/loopzero-yaml.ts) don't need to change their import path.
export type { ValidationError };

export interface ValidateConfigOptions {
  /** Skip filesystem checks (path existence, Dockerfile existence). Useful when
   *  the source will be validated server-side after upload (e.g., during deploy). */
  skipFilesystemChecks?: boolean;
}

/**
 * Validate a parsed LoopZeroYaml config strictly. Returns an array of errors
 * (empty = valid) + an array of warnings (non-fatal advisories).
 *
 * Per ADR D2 of plan 2026-06-15-code-review-49-findings-remediation, this
 * function is a thin orchestrator delegating to specialized validators in
 * ./validators/. Each validator returns a `ValidatorResult` ({errors,
 * warnings}) which the orchestrator concatenates in order. The order
 * matters for one specific case: when `validateAppPath` detects path
 * traversal it asks downstream validators to skip via
 * `shouldSkipDownstream` — matching the original `continue` in the loop.
 *
 * Pre-T2.1 CC: 95. Post-T2.1 CC: ≤ 12 (each specialized validator ≤ 15,
 * orchestrator ≤ 8).
 */
export function validateConfig(
  config: LoopZeroYaml,
  projectRoot: string,
  opts?: ValidateConfigOptions,
): { errors: ValidationError[]; warnings: string[] } {
  const skipFs = opts?.skipFilesystemChecks ?? false;
  const errors: ValidationError[] = [];
  const warnings: string[] = [];

  appendResult(validateVersion(config), errors, warnings);

  for (const [name, app] of Object.entries(config.apps)) {
    appendResult(validateAppName(name), errors, warnings);

    const pathResult = validateAppPath(name, app, projectRoot, skipFs);
    appendResult(pathResult, errors, warnings);
    if (pathResult.shouldSkipDownstream) continue;

    appendResult(validateAppRuntime(name, app), errors, warnings);
    appendResult(validateAppBuild(name, app, projectRoot, skipFs), errors, warnings);
    appendResult(validateAppEnv(name, app), errors, warnings);
    appendResult(validateAppScaling(name, app), errors, warnings);
    appendResult(validateAppUnknownFields(name, app), errors, warnings);
  }

  appendResult(validateProjectConstraints(config), errors, warnings);

  return { errors, warnings };
}

function appendResult(
  result: ValidatorResult,
  errors: ValidationError[],
  warnings: string[],
): void {
  errors.push(...result.errors);
  warnings.push(...result.warnings);
}
