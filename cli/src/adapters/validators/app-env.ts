// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import type { AppName, AppSpec, ValidatorResult } from './types';

const ENV_KEY_REGEX = /^[A-Z_][A-Z0-9_]*$/;

/**
 * Validate env keys follow shell convention (uppercase + underscores).
 * Reject snake_case / mixed-case / leading-digit / hyphen-separated keys.
 */
export function validateAppEnv(name: AppName, app: AppSpec): ValidatorResult {
  const errors: ValidatorResult['errors'] = [];
  if (!app.env) {
    return { errors, warnings: [] };
  }
  for (const key of Object.keys(app.env)) {
    if (!ENV_KEY_REGEX.test(key)) {
      errors.push({
        field: `apps.${name}.env.${key}`,
        message: `Env key '${key}' must be uppercase with underscores (e.g., NODE_ENV)`,
      });
    }
  }
  return { errors, warnings: [] };
}
