// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import type { ValidatorResult } from './types';

/**
 * RFC 1123 app name validation. Must start with a lowercase letter,
 * contain only lowercase letters / digits / hyphens, not end with a
 * hyphen, and be at most 63 characters.
 */
export function validateAppName(name: string): ValidatorResult {
  const errors: ValidatorResult['errors'] = [];
  if (!/^[a-z][a-z0-9-]*$/.test(name) || name.length > 63 || name.endsWith('-')) {
    errors.push({
      field: `apps.${name}`,
      message: `App name '${name}' is invalid. Must start with a lowercase letter, contain only lowercase letters, digits, and hyphens, not end with a hyphen, and be at most 63 characters.`,
    });
  }
  return { errors, warnings: [] };
}
