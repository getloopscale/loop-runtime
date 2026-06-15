// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import type { AppName, AppSpec, ValidatorResult } from './types';

/**
 * Recognized top-level app fields. Anything else generates an
 * "Unknown field" warning (non-fatal — preserves forward compat).
 *
 * B15 (CHG-2026-04-23-B15 / MEET-2026-04-23-059): replicas + autoscaling
 * are now known.
 */
const KNOWN_APP_FIELDS = new Set([
  'path', 'framework', 'type', 'port', 'build', 'dockerfile', 'render',
  'env', 'secrets', 'commands',
  'replicas', 'autoscaling',
]);

/**
 * Warn on unknown top-level app fields. Non-fatal — caller is expected to
 * concat warnings into the parent ValidatorResult.
 */
export function validateAppUnknownFields(name: AppName, app: AppSpec): ValidatorResult {
  const warnings: string[] = [];
  if (typeof app === 'object' && app !== null) {
    for (const key of Object.keys(app)) {
      if (!KNOWN_APP_FIELDS.has(key)) {
        warnings.push(`Unknown field 'apps.${name}.${key}' -- ignored`);
      }
    }
  }
  return { errors: [], warnings };
}
