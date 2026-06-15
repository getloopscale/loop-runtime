// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import type { LoopZeroYaml } from './types';
import { ValidatorResult } from './types';

/**
 * Validate the top-level `version` field. Only version 1 is currently
 * supported; undefined means "default to 1" (no error).
 *
 * Preserves verbatim error message + field path from the monolithic
 * validateConfig pre-T2.1 refactor.
 */
export function validateVersion(config: LoopZeroYaml): ValidatorResult {
  const errors: ValidatorResult['errors'] = [];
  if (config.version !== undefined && config.version !== 1) {
    errors.push({
      field: 'version',
      message: `Unsupported version ${config.version} (current: 1)`,
    });
  }
  return { errors, warnings: [] };
}
