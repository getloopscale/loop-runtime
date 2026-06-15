// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import type { AppName, AppSpec, ValidatorResult } from './types';

/**
 * Validate `type` + `port` + `render` interplay.
 *
 * - `type: server` requires `port > 0`
 * - `type: worker` rejects `port > 0` (workers don't receive HTTP traffic)
 * - `type: frontend` rejects `render: server` (frontends are always static
 *   CDN-delivered with zero pods)
 */
export function validateAppRuntime(name: AppName, app: AppSpec): ValidatorResult {
  const errors: ValidatorResult['errors'] = [];
  const appType = app.type ?? 'server';

  if (appType === 'server' && (app.port == null || app.port <= 0)) {
    errors.push({
      field: `apps.${name}.port`,
      message: `App '${name}': type 'server' requires port > 0`,
    });
  }
  if (appType === 'worker' && app.port != null && app.port > 0) {
    errors.push({
      field: `apps.${name}.port`,
      message: `App '${name}': type 'worker' should not have a port`,
    });
  }
  if (appType === 'frontend' && app.render === 'server') {
    errors.push({
      field: `apps.${name}.render`,
      message: `App '${name}': frontend apps are always static (CDN delivery, zero pods). Remove 'render: server'. If you need a server process, use 'type: server' instead.`,
    });
  }

  return { errors, warnings: [] };
}
